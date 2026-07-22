package api

import (
	"math"
	"net/http"
	"runtime"
	"strconv"
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
	bootstrapInfoPath := ""
	if bootstrapInfo != nil {
		bootstrapInfoPath = bootstrap.AdminBootstrapInfoPath()
	}

	c.HTML(http.StatusOK, "login.html", gin.H{
		"BootstrapAdmin":      bootstrapInfo,
		"BootstrapInfoPath":   bootstrapInfoPath,
		"ConfigPath":          config.ConfigFilePath(),
		"DataDir":             config.DataDir(),
		"ManagedDownloadsOff": !config.AppConfig.Managed.DownloadMissing,
		"ConfigAutoCreated":   config.ConfigAutoCreated,
		"IsWindows":           runtime.GOOS == goosWindows,
	})
}

func LoginPostHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonBadRequest(c, "登录请求格式不正确")
		return
	}

	clientIP := requestClientIP(c)
	if retryAfter, blocked := checkLoginThrottle(clientIP); blocked {
		retrySeconds := int(math.Ceil(retryAfter.Seconds()))
		if retrySeconds < 1 {
			retrySeconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(retrySeconds))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":               "登录尝试过于频繁，请稍后再试",
			"retry_after_seconds": retrySeconds,
		})
		return
	}

	authService := service.NewAuthService()
	user, err := authService.Login(req.Username, req.Password)
	if err != nil {
		registerFailedLoginAttempt(clientIP)
		failureCtx := auditContextForLogin(c, req.Username)
		service.RecordAudit(failureCtx, service.AuditEntry{
			Action:  service.AuditActionLoginFailure,
			Outcome: service.AuditOutcomeFailure,
			Details: map[string]string{"reason": "invalid_credentials"},
		})
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码不正确"})
		return
	}

	clearFailedLoginAttempts(clientIP)

	session := sessions.Default(c)
	session.Set("user_id", user.ID)

	maxAge := 0
	if req.RememberMe {
		maxAge = 3600 * 24 * 30
	}
	session.Options(sessionCookieOptions(c, maxAge))

	if err := session.Save(); err != nil {
		jsonServerError(c, "保存登录状态", err)
		return
	}

	successCtx := auditContextForLogin(c, user.Username)
	successCtx.UserID = user.ID
	service.RecordAudit(successCtx, service.AuditEntry{
		Action:  service.AuditActionLoginSuccess,
		Outcome: service.AuditOutcomeSuccess,
		Details: map[string]any{"remember_me": req.RememberMe},
	})

	redirect := "/"
	if bootstrap.BootstrapSetupPending() {
		redirect = "/setup"
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "登录成功",
		"redirect": redirect,
	})
}

func LogoutHandler(c *gin.Context) {
	auditCtx := buildAuditContext(c)
	session := sessions.Default(c)
	session.Clear()
	session.Options(sessionCookieOptions(c, -1))
	if err := session.Save(); err != nil {
		jsonServerError(c, "保存退出状态", err)
		return
	}
	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:  service.AuditActionLogout,
		Outcome: service.AuditOutcomeSuccess,
	})
	c.Redirect(http.StatusFound, "/login")
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

func ChangePasswordHandler(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonBadRequest(c, "修改密码请求格式不正确")
		return
	}

	req.NewPassword = strings.TrimSpace(req.NewPassword)
	if len(req.NewPassword) < 8 {
		jsonBadRequest(c, "新密码至少需要 8 个字符")
		return
	}

	uid, err := currentSessionUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前登录状态已失效，请重新登录"})
		return
	}

	authService := service.NewAuthService()
	auditCtx := buildAuditContext(c)
	if err := authService.ChangePassword(uid, req.OldPassword, req.NewPassword); err != nil {
		service.RecordAudit(auditCtx, service.AuditEntry{
			Action:  service.AuditActionPasswordChange,
			Outcome: service.AuditOutcomeFailure,
			Details: map[string]string{"error": err.Error()},
		})
		jsonBadRequest(c, err.Error())
		return
	}

	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:  service.AuditActionPasswordChange,
		Outcome: service.AuditOutcomeSuccess,
	})
	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}
