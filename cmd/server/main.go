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
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/launcher"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/startup"
	"github.com/pokerjest/animateAutoTool/internal/tray"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
)

const (
	maxServerLogSize = 10 * 1024 * 1024
	maxServerBackups = 5
)

func main() {
	if err := config.LoadConfig(""); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	logCleanup := configureLogging()
	defer logCleanup()

	if config.AppConfig.Server.Headless {
		log.Println("Tray integration disabled; starting in headless mode.")
		runServer()
		return
	}

	tray.Run(runServer)
}

func runServer() {
	log.Printf("AnimateAutoTool version: %s", appversion.AppVersion)

	mgr := launcher.NewManager()

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
	startup.Run()

	r := gin.Default()
	if err := r.SetTrustedProxies(config.AppConfig.Server.TrustedProxies); err != nil {
		log.Fatalf("Failed to set trusted proxies: %v", err)
	}
	api.InitRoutes(r)
	api.InitR2Cache()

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

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

	logPath := filepath.Join(logDir, "server.log")
	if err := rotateLogFile(logPath, maxServerLogSize, maxServerBackups); err != nil {
		log.Printf("Failed to rotate log file %s: %v", logPath, err)
	}
	//nolint:gosec // log path is derived from app-controlled config directories.
	file, err := os.OpenFile(filepath.Clean(logPath), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("Failed to open log file %s: %v", logPath, err)
		return func() {}
	}

	releaseMode := strings.EqualFold(strings.TrimSpace(config.AppConfig.Server.Mode), "release")
	if runtime.GOOS == "windows" && releaseMode {
		log.SetOutput(file)
		gin.DefaultWriter = file
		gin.DefaultErrorWriter = file
		return func() {
			_ = file.Close()
		}
	}

	stdout := io.MultiWriter(os.Stdout, file)
	stderr := io.MultiWriter(os.Stderr, file)
	log.SetOutput(stderr)
	gin.DefaultWriter = stdout
	gin.DefaultErrorWriter = stderr
	return func() {
		_ = file.Close()
	}
}

func rotateLogFile(path string, maxBytes int64, backups int) error {
	if backups <= 0 || maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < maxBytes {
		return nil
	}

	for i := backups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(src); err == nil {
			_ = os.Remove(dst)
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		}
	}

	firstBackup := path + ".1"
	_ = os.Remove(firstBackup)
	return os.Rename(path, firstBackup)
}
