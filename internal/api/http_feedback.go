package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func htmlStringError(c *gin.Context, status int, message string) {
	c.String(status, message)
}

func htmlBadRequest(c *gin.Context, message string) {
	htmlStringError(c, http.StatusBadRequest, message)
}

func htmlNotFound(c *gin.Context, message string) {
	htmlStringError(c, http.StatusNotFound, message)
}

func htmlServerError(c *gin.Context, action string, err error) {
	message := action + "失败"
	if err != nil {
		message += ": " + err.Error()
	}
	htmlStringError(c, http.StatusInternalServerError, message)
}

func jsonBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": message})
}

func jsonNotFound(c *gin.Context, message string) {
	c.JSON(http.StatusNotFound, gin.H{"error": message})
}

func jsonServerError(c *gin.Context, action string, err error) {
	message := action + "失败"
	if err != nil {
		message += ": " + err.Error()
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": message})
}
