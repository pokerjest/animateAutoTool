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
	"github.com/pokerjest/animateAutoTool/internal/tray"
)

func main() {
	if err := config.LoadConfig(""); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if config.AppConfig.Server.Headless {
		log.Println("Tray integration disabled; starting in headless mode.")
		runServer()
		return
	}

	tray.Run(runServer)
}

func runServer() {
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
