package api

import (
	"encoding/json"
	"net/url"

	"github.com/gin-gonic/gin"
)

func triggerAppToast(c *gin.Context, message, toastType string) {
	if c == nil || message == "" {
		return
	}

	payload := map[string]map[string]string{
		"app-toast": {
			"message_encoded": url.QueryEscape(message),
			"type":            toastType,
		},
	}
	if encoded, err := json.Marshal(payload); err == nil {
		c.Header("HX-Trigger", string(encoded))
	}
}
