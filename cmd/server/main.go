package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/api"
	"github.com/pokerjest/animateAutoTool/internal/appidentity"
	"github.com/pokerjest/animateAutoTool/internal/appshutdown"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/launcher"
	applogging "github.com/pokerjest/animateAutoTool/internal/logging"
	"github.com/pokerjest/animateAutoTool/internal/runtimejournal"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/startup"
	"github.com/pokerjest/animateAutoTool/internal/tray"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
)

const (
	serverLogPrefix     = "server"
	healthLogPrefix     = "health"
	serverLogMaxFiles   = 24 * 7
	healthLogMaxFiles   = 24 * 7
	shutdownTaskTimeout = 45 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Printf("Server stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	launcherMigration, launcherMigrationErr := appidentity.PrepareLocalLauncher()
	if err := config.LoadConfig(""); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logCleanup := configureLogging()
	defer logCleanup()
	if launcherMigrationErr != nil {
		log.Printf("Failed to prepare canonical launcher name: %v", launcherMigrationErr)
	} else if err := launcherMigration.Complete(); err != nil {
		log.Printf("Failed to finish launcher name migration: %v", err)
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	controlCleanup, err := appshutdown.StartPlatformListener(signalCtx)
	if err != nil {
		log.Printf("WARN: failed to initialize platform shutdown control: %v", err)
	} else {
		defer controlCleanup()
	}
	if config.AppConfig.Server.Headless {
		log.Println("Tray integration disabled; starting in headless mode.")
		return runServer(signalCtx)
	}

	return tray.Run(signalCtx, runServer)
}

func runServer(parent context.Context) (runErr error) {
	log.Printf("AnimateAutoTool version: %s", appversion.AppVersion)

	if parent == nil {
		parent = context.Background()
	}
	appCtx, cancelApp := context.WithCancel(context.Background())

	// Managed services remain available while the HTTP server drains requests.
	mgr := launcher.NewManager(context.Background())

	var (
		databaseOpen bool
		startupLife  *startup.Lifecycle
		sch          *scheduler.Manager
		srv          *http.Server
	)
	defer func() {
		if srv != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			if err := srv.Shutdown(shutdownCtx); err != nil {
				if closeErr := srv.Close(); closeErr != nil {
					log.Printf("WARN: forced HTTP close failed: %v", closeErr)
				}
				if runErr == nil {
					runErr = fmt.Errorf("graceful HTTP shutdown: %w", err)
				}
			}
			cancel()
		}
		if sch != nil {
			sch.Stop()
		}
		api.StopBackgroundTasks()
		cancelApp()
		if startupLife != nil {
			startupLife.Stop()
		}

		waits := []func(){api.WaitBackgroundTasks, event.GlobalBus.Wait, mgr.StopAll}
		if sch != nil {
			waits = append(waits, sch.Wait)
		}
		if startupLife != nil {
			waits = append(waits, startupLife.Wait)
		}
		if err := waitForShutdownTasks(shutdownTaskTimeout, waits...); err != nil {
			log.Printf("ERROR: %v; leaving SQLite open for process termination", err)
			databaseOpen = false
			if runErr == nil {
				runErr = err
			}
			return
		}
		if err := runtimejournal.EndSession(); err != nil {
			log.Printf("WARN: failed to clear runtime session marker during shutdown: %v", err)
		}
		if databaseOpen {
			if err := db.CloseDB(); err != nil {
				log.Printf("WARN: database close failed during shutdown: %v", err)
			}
		}
	}()

	log.Println("Initializing environment (Checking Alist & qBittorrent)...")
	if err := mgr.EnsureBinaries(); err != nil {
		return fmt.Errorf("initialize environment: %w", err)
	}

	log.Println("Starting background services...")
	if err := mgr.StartAll(); err != nil {
		return fmt.Errorf("start managed services: %w", err)
	}

	gin.SetMode(config.AppConfig.Server.Mode)

	absPath, _ := filepath.Abs(config.AppConfig.Database.Path)
	log.Printf("Initializing database at: %s", absPath)
	if err := db.InitDBWithError(config.AppConfig.Database.Path); err != nil {
		return err
	}
	databaseOpen = true
	mgr.NotifyDatabaseReady()
	if err := db.SyncGlobalConfigsWithConfigFile(); err != nil {
		log.Printf("Failed to synchronize system settings with %s: %v", config.ConfigFilePath(), err)
	}

	api.StartBackgroundTasks(appCtx)
	startupLife = startup.Run(appCtx)

	r := gin.Default()
	if err := r.SetTrustedProxies(config.AppConfig.Server.TrustedProxies); err != nil {
		return fmt.Errorf("set trusted proxies: %w", err)
	}
	api.InitRoutes(r)
	api.InitR2Cache()

	sch = scheduler.NewManagerWithContext(appCtx)
	sch.Start()

	port := fmt.Sprintf("%d", config.AppConfig.Server.Port)
	log.Printf("Server starting on port %s", port)
	srv = &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-parent.Done():
		log.Println("Shutdown signal received, stopping services...")
		return nil
	case reason := <-appshutdown.Requests():
		log.Printf("Shutdown requested (%s), stopping services...", reason)
		return nil
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	}
}
func waitForShutdownTasks(timeout time.Duration, waits ...func()) error {
	var wg sync.WaitGroup
	for _, wait := range waits {
		if wait == nil {
			continue
		}
		wg.Add(1)
		go func(wait func()) {
			defer wg.Done()
			wait()
		}(wait)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("background shutdown exceeded %s", timeout)
	}
}

func configureLogging() func() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if config.AppConfig == nil {
		return func() {}
	}

	logDir := config.LogsDir()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		log.Printf("Failed to create log directory %s: %v", logDir, err)
		return func() {}
	}

	file, err := applogging.NewHourlyWriter(logDir, serverLogPrefix, serverLogMaxFiles)
	if err != nil {
		log.Printf("Failed to initialize hourly logs in %s: %v", logDir, err)
		return func() {}
	}
	healthFile, err := applogging.NewHealthWriter(logDir, healthLogPrefix, healthLogMaxFiles)
	if err != nil {
		log.Printf("Failed to initialize health diagnostics in %s: %v", logDir, err)
		healthFile = nil
	}

	releaseMode := strings.EqualFold(strings.TrimSpace(config.AppConfig.Server.Mode), "release")
	if runtime.GOOS == "windows" && releaseMode {
		output := io.Writer(file)
		if healthFile != nil {
			output = io.MultiWriter(file, healthFile)
		}
		log.SetOutput(output)
		gin.DefaultWriter = output
		gin.DefaultErrorWriter = output
		return func() {
			_ = file.Close()
			if healthFile != nil {
				_ = healthFile.Close()
			}
		}
	}

	stdoutWriters := []io.Writer{os.Stdout, file}
	stderrWriters := []io.Writer{os.Stderr, file}
	if healthFile != nil {
		stdoutWriters = append(stdoutWriters, healthFile)
		stderrWriters = append(stderrWriters, healthFile)
	}
	stdout := io.MultiWriter(stdoutWriters...)
	stderr := io.MultiWriter(stderrWriters...)
	log.SetOutput(stderr)
	gin.DefaultWriter = stdout
	gin.DefaultErrorWriter = stderr
	return func() {
		_ = file.Close()
		if healthFile != nil {
			_ = healthFile.Close()
		}
	}
}
