package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type LocalRecoveryRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Confirm  string `json:"confirm_password" binding:"required"`
}

func RecoveryPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "recover.html", gin.H{
		"ConfigPath":        config.ConfigFilePath(),
		"DataDir":           config.DataDir(),
		"BootstrapPending":  bootstrap.BootstrapSetupPending(),
		"BootstrapInfoPath": bootstrap.AdminBootstrapInfoPath(),
	})
}

func LocalResetAdminPasswordHandler(c *gin.Context) {
	if bootstrap.BootstrapSetupPending() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "首次初始化尚未完成，请先在本机登录并完成初始化向导。",
		})
		return
	}

	var req LocalRecoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	req.Confirm = strings.TrimSpace(req.Confirm)

	switch {
	case req.Username == "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
		return
	case len(req.Password) < 8:
		c.JSON(http.StatusBadRequest, gin.H{"error": "New password must be at least 8 characters long"})
		return
	case req.Password != req.Confirm:
		c.JSON(http.StatusBadRequest, gin.H{"error": "The new passwords do not match"})
		return
	}

	authService := service.NewAuthService()
	if err := authService.ResetPasswordByUsername(req.Username, req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	clearFailedLoginAttempts(requestClientIP(c))

	c.JSON(http.StatusOK, gin.H{
		"message":  "Password reset successfully",
		"redirect": "/login",
	})
}
