package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

// ListAuditLogsHandler exposes recent audit entries as JSON for the
// settings/security page. Query params:
//
//	limit    — max rows to return (default 100, capped at 500)
//	action   — exact-match filter on the action identifier
//	username — exact-match filter on the actor's username
//	outcome  — "success" or "failure"
func ListAuditLogsHandler(c *gin.Context) {
	q := store.AuditLogQuery{
		Action:   c.Query("action"),
		Username: c.Query("username"),
		Outcome:  c.Query("outcome"),
	}
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			q.Limit = n
		}
	}

	rows, err := service.ListAuditLogs(q)
	if err != nil {
		jsonServerError(c, "读取审计日志", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"entries": rows,
		"count":   len(rows),
	})
}
