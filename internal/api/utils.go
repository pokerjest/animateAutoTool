package api

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

// IsHTMX checks if the request is from HTMX
func IsHTMX(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true"
}

// FetchQBConfig reliably fetches QB config without GORM scope issues
func FetchQBConfig() (string, string, string) {
	var configs []model.GlobalConfig
	// Fetch all to avoid scope pollution from sequential First() calls
	if err := db.DB.Find(&configs).Error; err != nil {
		log.Printf("Error fetching configs: %v", err)
		return "http://localhost:8080", "", ""
	}

	cfgMap := make(map[string]string)
	for _, c := range configs {
		cfgMap[c.Key] = c.Value
	}

	url := cfgMap[model.ConfigKeyQBUrl]
	if url == "" {
		url = "http://localhost:8080"
	}
	return url, cfgMap[model.ConfigKeyQBUsername], cfgMap[model.ConfigKeyQBPassword]
}
