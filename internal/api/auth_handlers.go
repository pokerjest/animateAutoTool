package api

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
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
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.HTML(http.StatusOK, "login.html", gin.H{})
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

	if req.RememberMe {
		// 30 days in seconds
		session.Options(sessions.Options{
			MaxAge: 3600 * 24 * 30,
			Path:   "/",
		})
	} else {
		// Browser session (cleared when browser closes)
		// Note: Depending on cookie store implementation, 0 or -1 might be used, or just omitting MaxAge.
		// If explicit setting is needed to reset previous persistent session:
		session.Options(sessions.Options{
			MaxAge: 0,
			Path:   "/",
		})
	}

	session.Save()

	c.JSON(http.StatusOK, gin.H{"message": "Login successful"})
}

func LogoutHandler(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
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

	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	authService := service.NewAuthService()
	// userID from session is usually generic interface{}, needs assertion
	uid, ok := userID.(uint)
	if !ok {
		// Try casting to standard int types if uint fails (depends on serialization)
		// Usually session stores numbers as int or float64 in JSON/Gob
		// For cookie store with gob, it preserves type if registered, but typically just cast carefully
		// Gorm IDs are uint. Let's assume standard behavior or try safe cast.
		// For simplicity, let's try direct assertion or check your session serialization.
		// If using `gob.Register`, it should be fine.
		// Let's safe check:
		if num, ok := userID.(int); ok {
			uid = uint(num) // Convert int to uint
		} else if num, ok := userID.(float64); ok {
			uid = uint(num)
		} else if num, ok := userID.(int64); ok {
			uid = uint(num)
		} else {
			// Fallback or error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Session error"})
			return
		}
	}

	if err := authService.ChangePassword(uid, req.OldPassword, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}
