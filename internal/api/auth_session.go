package api

import (
	"errors"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

var errNoActiveSession = errors.New("no active session")

func currentSessionUserID(c *gin.Context) (uint, error) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		return 0, errNoActiveSession
	}

	switch value := userID.(type) {
	case uint:
		return value, nil
	case int:
		return uint(value), nil
	case int64:
		return uint(value), nil
	case float64:
		return uint(value), nil
	default:
		return 0, errors.New("invalid session user id")
	}
}

func currentSessionUser(c *gin.Context) (*model.User, error) {
	userID, err := currentSessionUserID(c)
	if err != nil {
		return nil, err
	}

	var user model.User
	if err := db.DB.First(&user, userID).Error; err != nil {
		return nil, err
	}

	return &user, nil
}
