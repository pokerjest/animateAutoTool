package api

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

type LoginRequest struct {
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required"`
	RememberMe bool   `json:"remember_me"`
}

func LoginPageHandler(c *gin.Context) {
	// If already logged in, redirect to dashboard
	session := sessions.Default(c)
	if session.Get("user_id") != nil {
		if bootstrap.BootstrapSetupPending() {
			c.Redirect(http.StatusFound, "/setup")
			return
		}
		c.Redirect(http.StatusFound, "/")
		return
	}

	bootstrapInfo, _ := bootstrap.PendingAdminBootstrapInfo()

	c.HTML(http.StatusOK, "login.html", gin.H{
		"BootstrapAdmin":      bootstrapInfo,
		"ConfigPath":          config.ConfigFilePath(),
		"DataDir":             config.DataDir(),
		"ManagedDownloadsOff": !config.AppConfig.Managed.DownloadMissing,
		"ConfigAutoCreated":   config.ConfigAutoCreated,
	})
}

func LoginPostHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	authService := service.NewAuthService()
	user, err := authService.Login(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	session := sessions.Default(c)
	session.Set("user_id", user.ID)

	maxAge := 0
	if req.RememberMe {
		maxAge = 3600 * 24 * 30
	}
	session.Options(sessionCookieOptions(c, maxAge))

	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	redirect := "/"
	if bootstrap.BootstrapSetupPending() {
		redirect = "/setup"
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Login successful",
		"redirect": redirect,
	})
}

func LogoutHandler(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Options(sessionCookieOptions(c, -1))
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}
	c.Redirect(http.StatusFound, "/login")
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

func ChangePasswordHandler(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	req.NewPassword = strings.TrimSpace(req.NewPassword)
	if len(req.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "New password must be at least 8 characters long"})
		return
	}

	uid, err := currentSessionUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	authService := service.NewAuthService()
	if err := authService.ChangePassword(uid, req.OldPassword, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}
