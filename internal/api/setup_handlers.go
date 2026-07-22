package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/alist"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/downloader"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

type BootstrapSetupRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
	Confirm     string `json:"confirm_password" binding:"required"`
	QBMode      string `json:"qb_mode"`
	QBURL       string `json:"qb_url"`
	QBUsername  string `json:"qb_username"`
	QBPassword  string `json:"qb_password"`
	BaseDir     string `json:"base_download_dir"`
}

type SetupReadinessStatus struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	State    string `json:"state"`
	Headline string `json:"headline"`
	Detail   string `json:"detail"`
	Action   string `json:"action,omitempty"`
}

const (
	setupStateReady   = "ready"
	setupStatePending = "pending"
	setupStateWarning = "warning"
)

type SetupReadinessResponse struct {
	Services []SetupReadinessStatus `json:"services"`
}

func SetupPageHandler(c *gin.Context) {
	bootstrapInfo, pending := bootstrap.PendingAdminBootstrapInfo()
	if !pending {
		c.Redirect(http.StatusFound, "/")
		return
	}

	currentUser, err := currentSessionUser(c)
	canComplete := err == nil && currentUser.Username == bootstrapInfo.Username

	qbCfg := qbutil.LoadConfig()
	managedDownloadsOff := true
	if config.AppConfig != nil {
		managedDownloadsOff = !config.AppConfig.Managed.DownloadMissing
	}
	c.HTML(http.StatusOK, "setup.html", gin.H{
		"BootstrapAdmin":      bootstrapInfo,
		"CanCompleteSetup":    canComplete,
		"ConfigPath":          config.ConfigFilePath(),
		"DataDir":             config.DataDir(),
		"ManagedDownloadsOff": managedDownloadsOff,
		"IsWindows":           runtime.GOOS == goosWindows,
		"QBMode":              qbCfg.Mode,
		"QBURL":               qbCfg.URL,
		"QBUsername":          qbCfg.Username,
		"QBPassword":          qbCfg.Password,
		"BaseDir":             loadGlobalConfigValue(model.ConfigKeyBaseDir),
		"ManagedQBMissing":    qbutil.ManagedBinaryMissing(qbCfg, config.BinDir()),
	})
}

func SetupReadinessHandler(c *gin.Context) {
	c.JSON(http.StatusOK, SetupReadinessResponse{
		Services: collectSetupReadinessStatuses(),
	})
}

func CompleteBootstrapSetupHandler(c *gin.Context) {
	bootstrapInfo, pending := bootstrap.PendingAdminBootstrapInfo()
	if !pending {
		c.JSON(http.StatusOK, gin.H{
			"message":  "初始化已经完成",
			"redirect": "/",
		})
		return
	}

	currentUser, err := currentSessionUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前登录状态已失效，请重新登录"})
		return
	}
	if currentUser.Username != bootstrapInfo.Username {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "请使用初始化生成的管理员账号登录后，再完成首次设置",
		})
		return
	}

	var req BootstrapSetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonBadRequest(c, "初始化请求格式不正确")
		return
	}

	req.NewPassword = strings.TrimSpace(req.NewPassword)
	req.Confirm = strings.TrimSpace(req.Confirm)

	switch {
	case len(req.NewPassword) < 8:
		jsonBadRequest(c, "新密码至少需要 8 个字符")
		return
	case req.NewPassword != req.Confirm:
		jsonBadRequest(c, "两次输入的新密码不一致")
		return
	case req.NewPassword == bootstrapInfo.Password:
		jsonBadRequest(c, "请不要继续使用初始化密码，换一个新的密码吧")
		return
	}

	qbValues := normalizedQBValues(req.QBMode, req.QBURL, req.QBUsername, req.QBPassword)
	if qbValues[model.ConfigKeyQBMode] == qbutil.ModeExternal && qbValues[model.ConfigKeyQBUrl] == "" {
		jsonBadRequest(c, "外部 qBittorrent 模式需要填写 WebUI 地址")
		return
	}

	authService := service.NewAuthService()
	if err := authService.SetPassword(currentUser.ID, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := persistGlobalConfigs(map[string]string{
		model.ConfigKeyQBMode:     qbValues[model.ConfigKeyQBMode],
		model.ConfigKeyQBUrl:      qbValues[model.ConfigKeyQBUrl],
		model.ConfigKeyQBUsername: qbValues[model.ConfigKeyQBUsername],
		model.ConfigKeyQBPassword: qbValues[model.ConfigKeyQBPassword],
		model.ConfigKeyBaseDir:    strings.TrimSpace(req.BaseDir),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存初始化下载配置失败: %v", err)})
		return
	}
	statusCache.Delete("qb")

	auditCtx := buildAuditContext(c)
	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:  service.AuditActionBootstrapComplete,
		Outcome: service.AuditOutcomeSuccess,
		Details: map[string]string{
			"qb_mode":  qbValues[model.ConfigKeyQBMode],
			"base_dir": strings.TrimSpace(req.BaseDir),
		},
	})

	c.JSON(http.StatusOK, gin.H{
		"message":  "初始化完成",
		"redirect": "/",
	})
}

func loadGlobalConfigValue(key string) string {
	if db.DB == nil {
		return ""
	}
	return store.NewConfigStore(db.DB).GetDefault(key, "")
}

func collectSetupReadinessStatuses() []SetupReadinessStatus {
	return []SetupReadinessStatus{
		buildAppReadinessStatus(),
		buildQBReadinessStatus(),
		buildTMDBReadinessStatus(),
		buildJellyfinReadinessStatus(),
		buildAListReadinessStatus(),
	}
}

func buildAppReadinessStatus() SetupReadinessStatus {
	detail := fmt.Sprintf("配置: %s | 数据: %s", config.ConfigFilePath(), config.DataDir())
	action := ""
	state := setupStateReady
	headline := "配置文件和数据目录已经就绪"

	if runtime.GOOS == "windows" {
		root := strings.ToLower(filepath.Clean(config.RootDir()))
		switch {
		case strings.Contains(root, `\program files`):
			state = setupStateWarning
			headline = "建议把应用移到可写目录后再长期使用"
			action = "Windows 下不要长期放在 Program Files。建议移动到 D:\\Apps\\AnimateAutoTool 或 %USERPROFILE%\\Apps\\AnimateAutoTool。"
		case strings.TrimSpace(loadGlobalConfigValue(model.ConfigKeyBaseDir)) == "":
			action = "如果准备在 Windows 本机下载，建议顺手选一个默认下载目录，例如 D:\\Anime\\Downloads。"
		}
	}

	return SetupReadinessStatus{
		Key:      "app",
		Label:    "应用目录",
		State:    state,
		Headline: headline,
		Detail:   detail,
		Action:   action,
	}
}

func buildQBReadinessStatus() SetupReadinessStatus {
	cfg := qbutil.LoadConfig()
	modeLabel := "托管模式"
	if qbutil.NormalizeMode(cfg.Mode) == qbutil.ModeExternal {
		modeLabel = "外部 WebUI"
	}

	if qbutil.ManagedBinaryMissing(cfg, config.BinDir()) {
		return SetupReadinessStatus{
			Key:      "qb",
			Label:    "qBittorrent",
			State:    "warning",
			Headline: "当前选择了托管模式，但还没发现本地 qB 二进制",
			Detail:   fmt.Sprintf("%s 会在完成初始化后继续保留。当前 bin 目录: %s", modeLabel, config.BinDir()),
			Action:   "安装 qB 后重启，或者改成外部 WebUI 模式。",
		}
	}
	if qbutil.MissingExternalURL(cfg) {
		return SetupReadinessStatus{
			Key:      "qb",
			Label:    "qBittorrent",
			State:    setupStatePending,
			Headline: "外部 WebUI 模式还没填地址",
			Detail:   "如果你准备连接已有的 qB 实例，需要补一个可访问的 WebUI URL。",
			Action:   "继续填写 WebUI 地址、用户名和密码后再保存。",
		}
	}

	client := downloader.NewQBittorrentClient(cfg.URL)
	if err := client.Login(cfg.Username, cfg.Password); err != nil {
		return SetupReadinessStatus{
			Key:      "qb",
			Label:    "qBittorrent",
			State:    "warning",
			Headline: "配置已经保存，但当前还没有连通 qB",
			Detail:   fmt.Sprintf("%s 下的目标地址是 %s，当前返回: %v", modeLabel, cfg.URL, err),
			Action:   "确认 WebUI 已启动，或稍后到设置页继续测试连接。",
		}
	}

	if version, err := client.GetVersion(); err == nil {
		return SetupReadinessStatus{
			Key:      "qb",
			Label:    "qBittorrent",
			State:    setupStateReady,
			Headline: "qBittorrent 已经可以连通",
			Detail:   fmt.Sprintf("%s 已连上 %s，版本 %s。", modeLabel, cfg.URL, version),
		}
	}

	return SetupReadinessStatus{
		Key:      "qb",
		Label:    "qBittorrent",
		State:    "warning",
		Headline: "qB 登录成功，但版本检测没有通过",
		Detail:   fmt.Sprintf("%s 已完成登录，后续仍建议到设置页再做一次完整检查。", modeLabel),
		Action:   "如果下载任务异常，再到设置页执行“保存并测试连接”。",
	}
}

func buildTMDBReadinessStatus() SetupReadinessStatus {
	if strings.TrimSpace(loadGlobalConfigValue(model.ConfigKeyTMDBToken)) == "" {
		return SetupReadinessStatus{
			Key:      "tmdb",
			Label:    "TMDB",
			State:    setupStatePending,
			Headline: "还没有配置 TMDB Token",
			Detail:   "不影响进入系统，但封面、简介和季集增强会少一部分数据源。",
			Action:   "需要更完整的元数据时，再到设置页补一个 TMDB Token。",
		}
	}

	connected, errStr := CheckTMDBConnection()
	if connected {
		return SetupReadinessStatus{
			Key:      "tmdb",
			Label:    "TMDB",
			State:    setupStateReady,
			Headline: "TMDB 已连通",
			Detail:   "元数据抓取和海报补全已经具备完整外部数据源。",
		}
	}

	return SetupReadinessStatus{
		Key:      "tmdb",
		Label:    "TMDB",
		State:    "warning",
		Headline: "TMDB Token 已填写，但当前检测没有通过",
		Detail:   errStr,
		Action:   "确认 Token 是否有效，或检查代理设置。",
	}
}

func buildJellyfinReadinessStatus() SetupReadinessStatus {
	url := strings.TrimSpace(loadGlobalConfigValue(model.ConfigKeyJellyfinUrl))
	apiKey := strings.TrimSpace(loadGlobalConfigValue(model.ConfigKeyJellyfinApiKey))
	if url == "" || apiKey == "" {
		return SetupReadinessStatus{
			Key:      "jellyfin",
			Label:    "Jellyfin",
			State:    setupStatePending,
			Headline: "还没有配置 Jellyfin 地址和 API Key",
			Detail:   "播放器和播放进度同步会先保持未启用，不影响其它功能。",
			Action:   "等媒体库稳定后，再到设置页补 Jellyfin 连接。",
		}
	}

	connected, errStr := CheckJellyfinConnection()
	if connected {
		return SetupReadinessStatus{
			Key:      "jellyfin",
			Label:    "Jellyfin",
			State:    setupStateReady,
			Headline: "Jellyfin 已连通",
			Detail:   fmt.Sprintf("当前服务器地址: %s", url),
		}
	}

	return SetupReadinessStatus{
		Key:      "jellyfin",
		Label:    "Jellyfin",
		State:    "warning",
		Headline: "Jellyfin 已配置，但检测还没通过",
		Detail:   errStr,
		Action:   "确认地址、API Key 和代理配置是否正确。",
	}
}

func buildAListReadinessStatus() SetupReadinessStatus {
	url := strings.TrimSpace(loadGlobalConfigValue(model.ConfigKeyAListUrl))
	token := strings.TrimSpace(loadGlobalConfigValue(model.ConfigKeyAListToken))
	creds, err := bootstrap.LoadAListCredentials()
	hasBootstrapCreds := err == nil && (strings.TrimSpace(creds.Token) != "" || strings.TrimSpace(creds.Password) != "")

	if url == "" && token == "" && !hasBootstrapCreds {
		headline := "AList 还没有准备连接信息"
		detail := "文件浏览和 PikPak 挂载会先保持未启用。"
		action := "需要时再到设置页补 AList 地址与 Token。"
		if managedDownloadsDisabled() {
			headline = "AList 自动下载安装默认已关闭"
			detail = "首次启动不会自动拉取 AList，主流程会优先保证 Web 管理台先可用。"
			action = "需要 AList 时，再手动打开下载缺失组件或自行安装。"
		}
		return SetupReadinessStatus{
			Key:      "alist",
			Label:    "AList",
			State:    setupStatePending,
			Headline: headline,
			Detail:   detail,
			Action:   action,
		}
	}

	connected, errStr := alist.CheckConnection()
	if connected {
		return SetupReadinessStatus{
			Key:      "alist",
			Label:    "AList",
			State:    setupStateReady,
			Headline: "AList 已连通",
			Detail:   fmt.Sprintf("当前服务地址: %s", alist.BaseURL()),
		}
	}

	state := setupStateWarning
	headline := "AList 已配置，但当前没有连通"
	action := "确认 AList 是否启动，以及当前凭据是否仍然有效。"
	switch errStr {
	case "Credentials missing":
		state = "pending"
		headline = "AList 服务地址已知，但还没有可用凭据"
		action = "先完成主流程，后面再到设置页同步 AList / PikPak。"
	case "Authentication failed":
		headline = "AList 可以访问，但凭据认证没有通过"
	}

	return SetupReadinessStatus{
		Key:      "alist",
		Label:    "AList",
		State:    state,
		Headline: headline,
		Detail:   errStr,
		Action:   action,
	}
}

func managedDownloadsDisabled() bool {
	if config.AppConfig == nil {
		return true
	}
	return !config.AppConfig.Managed.DownloadMissing
}
