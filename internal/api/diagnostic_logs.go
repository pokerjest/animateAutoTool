package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	applogging "github.com/pokerjest/animateAutoTool/internal/logging"
)

const diagnosticLogFileCount = 3

// V1ExportDiagnosticLogsHandler downloads the newest three hourly server logs
// as a ZIP without exposing their absolute paths.
func V1ExportDiagnosticLogsHandler(c *gin.Context) {
	path, filename, included, err := applogging.CreateRecentArchive(config.LogsDir(), "server", diagnosticLogFileCount, time.Now())
	if err != nil {
		if errors.Is(err, applogging.ErrNoHourlyLogs) {
			v1Error(c, http.StatusNotFound, "diagnostic_logs_not_found", "还没有可导出的服务日志，请先运行一段时间后再试")
			return
		}
		v1Error(c, http.StatusInternalServerError, "diagnostic_logs_export_failed", "打包诊断日志失败")
		return
	}
	defer func() { _ = os.Remove(path) }()

	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/zip")
	c.Header("X-Log-File-Count", strconv.Itoa(len(included)))
	c.FileAttachment(path, filename)
}
