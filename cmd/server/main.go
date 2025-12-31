package main

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/api"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/launcher"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
)

func main() {
	// 0. Initialize Sidecar Launcher
	// We initialize this early to ensure we have the environment even if config fails?
	// But let's load logic order: Config -> Launcher -> App

	// 1. Load Config
	if err := config.LoadConfig("."); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Launcher: Ensure Binaries & Start Services
	mgr := launcher.NewManager()

	// Ensure Env (downloads if missing)
	log.Println("Initializing environment (Checking Alist & qBittorrent)...")
	if err := mgr.EnsureBinaries(); err != nil {
		log.Fatalf("Failed to initialize environment: %v", err)
	}

	// Start Sidecars
	log.Println("Starting background services...")
	if err := mgr.StartAll(); err != nil {
		log.Fatalf("Failed to start services: %v", err)
	}
	defer mgr.StopAll()

	// 3. Setup Gin Mode
	gin.SetMode(config.AppConfig.Server.Mode)

	// 转换为绝对路径日志一下
	absPath, _ := filepath.Abs(config.AppConfig.Database.Path)
	log.Printf("Initializing database at: %s", absPath)

	db.InitDB(config.AppConfig.Database.Path)

	r := gin.Default()

	// 初始化路由
	api.InitRoutes(r)

	// Pre-fetch R2 Cache
	api.InitR2Cache()

	// Start Scheduler
	sch := scheduler.NewManager()
	sch.Start()
	defer sch.Stop()

	// Alist Server is now managed by mgr.StartAll(), so we don't call alist.StartAlistServer()

	port := fmt.Sprintf("%d", config.AppConfig.Server.Port)
	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
