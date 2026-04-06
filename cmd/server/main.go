package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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

	sch := scheduler.NewManager()
	sch.Start()
	defer sch.Stop()

	port := fmt.Sprintf("%d", config.AppConfig.Server.Port)
	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
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
