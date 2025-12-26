package main

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/alist"
	"github.com/pokerjest/animateAutoTool/internal/api"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
)

func main() {
	// 1. Load Config
	if err := config.LoadConfig("."); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Setup Gin Mode
	gin.SetMode(config.AppConfig.Server.Mode)

	// 转换为绝对路径日志一下
	absPath, _ := filepath.Abs(config.AppConfig.Database.Path)
	log.Printf("Initializing database at: %s", absPath)

	db.InitDB(config.AppConfig.Database.Path)

	r := gin.Default()

	// 初始化路由
	api.InitRoutes(r)

	// Start Scheduler
	sch := scheduler.NewManager()
	sch.Start()
	defer sch.Stop()

	// Start Embedded AList Server
	alist.StartAlistServer()

	port := fmt.Sprintf("%d", config.AppConfig.Server.Port)
	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
