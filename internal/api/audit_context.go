package api

import (
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

// buildAuditContext pulls the session user (if any), client IP and
// user-agent off the gin context. Returns a zero-valued context when
// the request has no usable session — RecordAudit will still log the
// row with whatever IP/UA we could collect.
func buildAuditContext(c *gin.Context) service.AuditContext {
	ctx := service.AuditContext{
		IP: requestClientIP(c),
	}
	if c != nil && c.Request != nil {
		ctx.UserAgent = c.Request.UserAgent()
	}
	if user, err := currentSessionUser(c); err == nil && user != nil {
		ctx.UserID = user.ID
		ctx.Username = user.Username
	}
	return ctx
}

// auditContextForLogin is the variant used inside LoginPostHandler where
// we already know which username was attempted but the session has not
// yet been established. UserID is filled in by the caller on success.
func auditContextForLogin(c *gin.Context, username string) service.AuditContext {
	ctx := service.AuditContext{
		Username: username,
		IP:       requestClientIP(c),
	}
	if c != nil && c.Request != nil {
		ctx.UserAgent = c.Request.UserAgent()
	}
	return ctx
}
