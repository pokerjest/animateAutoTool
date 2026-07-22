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
	var req LocalRecoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonBadRequest(c, "本地重置密码请求格式不正确")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	req.Confirm = strings.TrimSpace(req.Confirm)

	switch {
	case req.Username == "":
		jsonBadRequest(c, "用户名不能为空")
		return
	case len(req.Password) < 8:
		jsonBadRequest(c, "新密码至少需要 8 个字符")
		return
	case req.Password != req.Confirm:
		jsonBadRequest(c, "两次输入的新密码不一致")
		return
	}

	if bootstrapInfo, pending := bootstrap.PendingAdminBootstrapInfo(); pending && req.Username != bootstrapInfo.Username {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "首次初始化期间只能重置当前 bootstrap 管理员的密码",
		})
		return
	}

	authService := service.NewAuthService()
	auditCtx := service.AuditContext{
		Username:  req.Username,
		IP:        requestClientIP(c),
		UserAgent: c.Request.UserAgent(),
	}
	if err := authService.ResetPasswordByUsername(req.Username, req.Password); err != nil {
		service.RecordAudit(auditCtx, service.AuditEntry{
			Action:  service.AuditActionPasswordRecoveryLoc,
			Outcome: service.AuditOutcomeFailure,
			Details: map[string]string{"error": err.Error()},
		})
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	clearFailedLoginAttempts(requestClientIP(c))
	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:  service.AuditActionPasswordRecoveryLoc,
		Outcome: service.AuditOutcomeSuccess,
	})

	c.JSON(http.StatusOK, gin.H{
		"message":  "密码重置成功",
		"redirect": "/login",
	})
}
