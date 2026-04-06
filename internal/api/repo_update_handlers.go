package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/updater"
)

func GetRepoUpdateStatusHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "repo_update_status.html", updater.Snapshot())
}

func RepoUpdateCheckNowHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "repo_update_status.html", updater.CheckNow("manual-check"))
}

func RepoUpdatePullNowHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "repo_update_status.html", updater.CheckAndPullNow("manual-pull"))
}
