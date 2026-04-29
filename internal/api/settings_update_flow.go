package api

import (
	"fmt"
	"html"
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/alist"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/jellyfin"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/pokerjest/animateAutoTool/internal/updater"
)

type settingsScopeSpec struct {
	keys       []string
	checkboxes []string
}

func resolveSettingsScopeSpec(scope string) settingsScopeSpec {
	switch scope {
	case "download":
		return settingsScopeSpec{
			keys: []string{
				model.ConfigKeyQBMode,
				model.ConfigKeyQBUrl,
				model.ConfigKeyQBUsername,
				model.ConfigKeyQBPassword,
				model.ConfigKeyBaseDir,
			},
		}
	case "data-sources":
		return settingsScopeSpec{
			keys: []string{
				model.ConfigKeyBangumiRefreshToken,
				model.ConfigKeyBangumiAccessToken,
				model.ConfigKeyBangumiAppID,
				model.ConfigKeyBangumiAppSecret,
				model.ConfigKeyTMDBToken,
				model.ConfigKeyAniListToken,
			},
		}
	case "network":
		return settingsScopeSpec{
			keys: []string{
				model.ConfigKeyProxyURL,
				model.ConfigKeyRepoUpdateIntervalMinutes,
				model.ConfigKeyRepoUpdateOwner,
				model.ConfigKeyRepoUpdateName,
			},
			checkboxes: []string{
				model.ConfigKeyProxyBangumi,
				model.ConfigKeyProxyTMDB,
				model.ConfigKeyProxyAniList,
				model.ConfigKeyProxyJellyfin,
				model.ConfigKeyRepoUpdateEnabled,
				model.ConfigKeyRepoAutoPullEnabled,
				model.ConfigKeyRepoRequireChecksum,
			},
		}
	case "media":
		return settingsScopeSpec{
			keys: []string{
				model.ConfigKeyJellyfinUrl,
				model.ConfigKeyJellyfinApiKey,
				model.ConfigKeyJellyfinUsername,
				model.ConfigKeyJellyfinPassword,
			},
		}
	case "pikpak":
		return settingsScopeSpec{
			keys: []string{
				model.ConfigKeyPikPakUsername,
				model.ConfigKeyPikPakPassword,
				model.ConfigKeyPikPakRefreshToken,
			},
		}
	default:
		return settingsScopeSpec{
			keys: []string{
				model.ConfigKeyQBMode,
				model.ConfigKeyQBUrl,
				model.ConfigKeyQBUsername,
				model.ConfigKeyQBPassword,
				model.ConfigKeyBaseDir,
				model.ConfigKeyBangumiRefreshToken,
				model.ConfigKeyBangumiAccessToken,
				model.ConfigKeyTMDBToken,
				model.ConfigKeyAniListToken,
				model.ConfigKeyProxyURL,
				model.ConfigKeyProxyBangumi,
				model.ConfigKeyProxyTMDB,
				model.ConfigKeyProxyAniList,
				model.ConfigKeyRepoUpdateEnabled,
				model.ConfigKeyRepoAutoPullEnabled,
				model.ConfigKeyRepoUpdateIntervalMinutes,
				model.ConfigKeyRepoUpdateOwner,
				model.ConfigKeyRepoUpdateName,
				model.ConfigKeyRepoRequireChecksum,
				model.ConfigKeyJellyfinUrl,
				model.ConfigKeyJellyfinApiKey,
				model.ConfigKeyJellyfinUsername,
				model.ConfigKeyJellyfinPassword,
				model.ConfigKeyProxyJellyfin,
				model.ConfigKeyPikPakUsername,
				model.ConfigKeyPikPakPassword,
				model.ConfigKeyPikPakRefreshToken,
			},
			checkboxes: []string{
				model.ConfigKeyProxyBangumi,
				model.ConfigKeyProxyTMDB,
				model.ConfigKeyProxyAniList,
				model.ConfigKeyProxyJellyfin,
				model.ConfigKeyRepoUpdateEnabled,
				model.ConfigKeyRepoAutoPullEnabled,
				model.ConfigKeyRepoRequireChecksum,
			},
		}
	}
}

func persistSettingsScope(c *gin.Context, scope string) error {
	spec := resolveSettingsScopeSpec(scope)
	qbOverrides := map[string]string{}
	if scope == "download" {
		qbOverrides = normalizedQBFormValues(c)
	} else if _, hasQBMode := c.GetPostForm(model.ConfigKeyQBMode); hasQBMode {
		qbOverrides = normalizedQBFormValues(c)
	}

	for _, key := range spec.keys {
		if val, ok := qbOverrides[key]; ok {
			if err := persistGlobalConfig(key, val); err != nil {
				return err
			}
			continue
		}
		if val, exists := c.GetPostForm(key); exists {
			if err := persistGlobalConfig(key, val); err != nil {
				return err
			}
		}
	}

	for _, key := range spec.checkboxes {
		if _, exists := c.GetPostForm(key); !exists {
			if err := persistGlobalConfig(key, "false"); err != nil {
				return err
			}
			continue
		}
		if err := persistGlobalConfig(key, "true"); err != nil {
			return err
		}
	}
	return nil
}

func maybeAutoAuthJellyfin(c *gin.Context) []string {
	jfURL := c.PostForm(model.ConfigKeyJellyfinUrl)
	jfUser := c.PostForm(model.ConfigKeyJellyfinUsername)
	jfPass := c.PostForm(model.ConfigKeyJellyfinPassword)

	if jfURL == "" || jfUser == "" || jfPass == "" {
		return nil
	}

	client := jellyfin.NewClient(jfURL, "")
	authResp, err := client.AuthenticateContext(c.Request.Context(), jfUser, jfPass)
	if err == nil && authResp.AccessToken != "" {
		log.Printf("Jellyfin Auto-Auth Successful for user: %s", jfUser)
		if err := persistGlobalConfig(model.ConfigKeyJellyfinApiKey, authResp.AccessToken); err != nil {
			log.Printf("Jellyfin Auto-Auth token persist failed: %v", err)
			return []string{fmt.Sprintf("Jellyfin 自动登录成功，但保存 API Key 失败: %v", err)}
		}
		statusCache.Delete("jellyfin")
		return nil
	}

	log.Printf("Jellyfin Auto-Auth Failed: %v", err)
	if err != nil {
		return []string{fmt.Sprintf("Jellyfin 自动登录失败: %v", err)}
	}
	return []string{"Jellyfin 自动登录未返回可用的 API Key"}
}

func maybeAutoSyncPikPak(c *gin.Context) []string {
	username := c.PostForm(model.ConfigKeyPikPakUsername)
	password := c.PostForm(model.ConfigKeyPikPakPassword)
	refreshToken := c.PostForm(model.ConfigKeyPikPakRefreshToken)
	captchaToken := c.PostForm(model.ConfigKeyPikPakCaptchaToken)

	if username == "" || password == "" {
		return nil
	}

	if err := alist.AddPikPakStorage(username, password, refreshToken, captchaToken); err != nil {
		log.Printf("Failed to sync PikPak storage during settings save: %v", err)
		if msg := strings.TrimSpace(err.Error()); msg != "" {
			return []string{fmt.Sprintf("PikPak 自动挂载失败: %s", msg)}
		}
		return []string{"PikPak 自动挂载失败"}
	}

	log.Printf("PikPak storage synced successfully during settings save for user: %s", username)
	return nil
}

func buildSettingsSaveResponse(c *gin.Context, scope string, warnings []string) string {
	statusCache.Delete("qb")
	statusCache.Delete("jellyfin")
	qbStatusHTML := fmt.Sprintf(`<div id="qb-status" hx-swap-oob="innerHTML">%s</div>`, renderQBStatusMessage(qbConfigFromForm(c)))
	bangumiStatusOOB := `<div hx-swap-oob="true" id="bangumi-refresh-trigger" hx-get="/api/bangumi/profile" hx-target="#bangumi-status" hx-trigger="load" class="hidden"></div>`
	tmdbStatusOOB := RenderTMDBStatusOOB()
	aniListStatusOOB := RenderAniListStatusOOB()
	jellyfinStatusOOB := RenderJellyfinStatusOOB()

	successMsg := fmt.Sprintf(`
		<div class="text-emerald-600 font-bold flex items-center gap-2 animate-pulse">✅ 所有配置已保存</div>
		%s
		%s
		%s
		%s
		%s
	`, qbStatusHTML, bangumiStatusOOB, tmdbStatusOOB, aniListStatusOOB, jellyfinStatusOOB)

	if scope == "data-sources" {
		successMsg += RenderBangumiStatusOOB()
		successMsg += RenderTMDBStatusOOB()
		successMsg += RenderAniListStatusOOB()
	}
	if scope == "network" {
		go updater.CheckNow("settings-save")
		successMsg += `<div hx-swap-oob="true" id="repo-update-refresh-trigger" hx-get="/api/settings/repo-update-status" hx-target="#repo-update-container" hx-trigger="load" class="hidden"></div>`
	}
	if len(warnings) > 0 {
		successMsg += fmt.Sprintf(`
		<div class="mt-3 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
			<div class="font-bold mb-1">配置已保存，但以下联动未完成：</div>
			<ul class="list-disc pl-5 space-y-1">%s</ul>
		</div>`, joinHTMLListItems(warnings))
	}

	return successMsg
}

func joinHTMLListItems(items []string) string {
	var b strings.Builder
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		b.WriteString("<li>")
		b.WriteString(html.EscapeString(item))
		b.WriteString("</li>")
	}
	return b.String()
}

func renderSettingsSaveError(msg string) string {
	return fmt.Sprintf(`<div class="text-red-600 font-bold flex items-center gap-2">保存未完成：%s</div>`, msg)
}

func persistBangumiSettings(c *gin.Context) error {
	keys := []string{
		model.ConfigKeyBangumiAppID,
		model.ConfigKeyBangumiAppSecret,
	}
	for _, key := range keys {
		if val, exists := c.GetPostForm(key); exists {
			if err := persistGlobalConfig(key, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadSettingsViewData() (map[string]string, string, any) {
	configMap, err := store.NewConfigStore(db.DB).ListMap()
	if err != nil {
		log.Printf("Error fetching configs: %v", err)
	}
	if configMap == nil {
		configMap = map[string]string{}
	}

	qbCfg := qbutil.LoadConfig()
	configMap[model.ConfigKeyQBMode] = qbCfg.Mode
	if qbutil.UsesManagedInstance(qbCfg) {
		configMap[model.ConfigKeyQBUrl] = ""
		configMap[model.ConfigKeyQBUsername] = ""
		configMap[model.ConfigKeyQBPassword] = ""
	}

	return configMap, "", getDBStats(db.DB, db.CurrentDBPath)
}

func renderBangumiSaveSuccess() string {
	successHTML := `<div class="text-emerald-600 font-medium flex items-center gap-2 animate-fade-in-up">✅ 保存成功</div>`
	triggerScript := `<div hx-swap-oob="true" id="bangumi-refresh-trigger" hx-get="/api/bangumi/profile" hx-target="#bangumi-status" hx-trigger="load" class="hidden"></div>`
	return successHTML + triggerScript
}
