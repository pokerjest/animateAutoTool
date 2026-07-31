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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/api"
	"github.com/pokerjest/animateAutoTool/internal/appidentity"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/launcher"
	applogging "github.com/pokerjest/animateAutoTool/internal/logging"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/startup"
	"github.com/pokerjest/animateAutoTool/internal/tray"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
)

const (
	serverLogPrefix   = "server"
	healthLogPrefix   = "health"
	serverLogMaxFiles = 24 * 7
	healthLogMaxFiles = 24 * 7
)

func main() {
	launcherMigration, launcherMigrationErr := appidentity.PrepareLocalLauncher()
	if err := config.LoadConfig(""); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	logCleanup := configureLogging()
	defer logCleanup()
	if launcherMigrationErr != nil {
		log.Printf("Failed to prepare canonical launcher name: %v", launcherMigrationErr)
	} else if err := launcherMigration.Complete(); err != nil {
		log.Printf("Failed to finish launcher name migration: %v", err)
	}

	if config.AppConfig.Server.Headless {
		log.Println("Tray integration disabled; starting in headless mode.")
		runServer()
		return
	}

	tray.Run(runServer)
}

func runServer() {
	log.Printf("AnimateAutoTool version: %s", appversion.AppVersion)

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// Managed services must remain available while the HTTP server drains
	// in-flight requests. StopAll cancels this independent context after the
	// shutdown sequence completes.
	mgr := launcher.NewManager(context.Background())

	log.Println("Initializing environment (Checking Alist & qBittorrent)...")
	if err := mgr.EnsureBinaries(); err != nil {
		log.Fatalf("Failed to initialize environment: %v", err)
	}

	log.Println("Starting background services...")
	if err := mgr.StartAll(); err != nil {
		log.Fatalf("Failed to start services: %v", err)
	}
	defer mgr.StopAll()

	gin.SetMode(config.AppConfig.Server.Mode)

	absPath, _ := filepath.Abs(config.AppConfig.Database.Path)
	log.Printf("Initializing database at: %s", absPath)

	db.InitDB(config.AppConfig.Database.Path)
	if err := db.SyncGlobalConfigsWithConfigFile(); err != nil {
		log.Printf("Failed to synchronize system settings with %s: %v", config.ConfigFilePath(), err)
	}
	cleanupStartup := startup.Run(rootCtx)
	defer func() {
		cleanupStartup()
		if err := db.CloseDB(); err != nil {
			log.Printf("WARN: database close failed during shutdown: %v", err)
		}
	}()

	r := gin.Default()
	if err := r.SetTrustedProxies(config.AppConfig.Server.TrustedProxies); err != nil {
		log.Fatalf("Failed to set trusted proxies: %v", err)
	}
	api.InitRoutes(r)
	api.InitR2Cache()

	sch := scheduler.NewManagerWithContext(rootCtx)
	sch.Start()
	defer sch.Stop()

	port := fmt.Sprintf("%d", config.AppConfig.Server.Port)
	log.Printf("Server starting on port %s", port)
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-rootCtx.Done():
		log.Println("Shutdown signal received, stopping services...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful shutdown failed: %v", err)
		}
		// Drain in-flight async event handlers before the process exits.
		event.GlobalBus.Wait()
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			return
		}
		log.Fatal(err)
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
