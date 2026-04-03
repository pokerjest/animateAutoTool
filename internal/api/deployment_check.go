package api

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

const (
	deploymentCheckPass = "pass"
	deploymentCheckWarn = "warn"
	deploymentCheckFail = "fail"
)

type DeploymentCheckItem struct {
	Name    string
	Status  string
	Summary string
	Detail  string
	Action  string
}

type DeploymentCheckReport struct {
	PassCount int
	WarnCount int
	FailCount int
	Items     []DeploymentCheckItem
}

func GetDeploymentCheckHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "deployment_check.html", buildDeploymentCheckReport())
}

func buildDeploymentCheckReport() DeploymentCheckReport {
	items := []DeploymentCheckItem{
		checkPublicURL(),
		checkTrustedProxies(),
		checkSecureCookies(),
		checkAuthSecret(),
		checkBootstrapExposure(),
	}

	report := DeploymentCheckReport{Items: items}
	for _, item := range items {
		switch item.Status {
		case deploymentCheckPass:
			report.PassCount++
		case deploymentCheckWarn:
			report.WarnCount++
		case deploymentCheckFail:
			report.FailCount++
		}
	}
	return report
}

func checkPublicURL() DeploymentCheckItem {
	if config.AppConfig == nil {
		return DeploymentCheckItem{
			Name:    "公网访问地址",
			Status:  deploymentCheckFail,
			Summary: "配置尚未加载",
			Action:  "请先确认应用已成功加载配置文件。",
		}
	}

	raw := strings.TrimSpace(config.AppConfig.Server.PublicURL)
	if raw == "" {
		return DeploymentCheckItem{
			Name:    "公网访问地址",
			Status:  deploymentCheckWarn,
			Summary: "还没有设置 server.public_url",
			Detail:  "当前同源校验和公网链接会退回请求头推断，长期公网部署不够稳。",
			Action:  "在 config.yaml 里填写完整的 HTTPS 地址，例如 https://anime.example.com。",
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return DeploymentCheckItem{
			Name:    "公网访问地址",
			Status:  deploymentCheckFail,
			Summary: "server.public_url 格式无效",
			Detail:  raw,
			Action:  "请使用完整 URL，包含协议和主机名。",
		}
	}

	if !strings.EqualFold(parsed.Scheme, "https") {
		return DeploymentCheckItem{
			Name:    "公网访问地址",
			Status:  deploymentCheckFail,
			Summary: "server.public_url 不是 HTTPS",
			Detail:  raw,
			Action:  "公网登录建议始终走 HTTPS，否则 Secure Cookie 无法稳定生效。",
		}
	}

	return DeploymentCheckItem{
		Name:    "公网访问地址",
		Status:  deploymentCheckPass,
		Summary: "server.public_url 已正确配置为 HTTPS",
		Detail:  strings.TrimRight(raw, "/"),
	}
}

func checkTrustedProxies() DeploymentCheckItem {
	if config.AppConfig == nil {
		return DeploymentCheckItem{Name: "受信任代理", Status: deploymentCheckFail, Summary: "配置尚未加载"}
	}

	proxies := normalizedTrustedProxies()
	if len(proxies) == 0 {
		return DeploymentCheckItem{
			Name:    "受信任代理",
			Status:  deploymentCheckWarn,
			Summary: "trusted_proxies 为空",
			Detail:  "应用会忽略 X-Forwarded-* 头，反向代理场景下同源和真实来源判断会退化。",
			Action:  "如果你通过 Nginx/Caddy/Traefik 暴露公网，请把代理 IP 或 CIDR 加到 server.trusted_proxies。",
		}
	}

	for _, entry := range proxies {
		if entry == "0.0.0.0/0" || entry == "::/0" {
			return DeploymentCheckItem{
				Name:    "受信任代理",
				Status:  deploymentCheckFail,
				Summary: "trusted_proxies 过于宽泛",
				Detail:  entry,
				Action:  "不要信任所有来源，只填写你自己控制的反向代理 IP 或网段。",
			}
		}
	}

	if onlyLoopbackTrusted(proxies) && strings.TrimSpace(config.AppConfig.Server.PublicURL) != "" {
		return DeploymentCheckItem{
			Name:    "受信任代理",
			Status:  deploymentCheckWarn,
			Summary: "当前只信任本机回环地址",
			Detail:  strings.Join(proxies, ", "),
			Action:  "如果公网流量经过反向代理，请把代理 IP/CIDR 也加入 trusted_proxies。",
		}
	}

	return DeploymentCheckItem{
		Name:    "受信任代理",
		Status:  deploymentCheckPass,
		Summary: "trusted_proxies 已设置",
		Detail:  strings.Join(proxies, ", "),
	}
}

func checkSecureCookies() DeploymentCheckItem {
	if secureCookiesFromConfig() {
		return DeploymentCheckItem{
			Name:    "登录 Cookie",
			Status:  deploymentCheckPass,
			Summary: "Secure Cookie 会在公网模式下启用",
			Detail:  "session Cookie 将带 Secure / HttpOnly / SameSite=Lax。",
		}
	}

	return DeploymentCheckItem{
		Name:    "登录 Cookie",
		Status:  deploymentCheckFail,
		Summary: "当前无法稳定启用 Secure Cookie",
		Detail:  "通常是因为 server.public_url 缺失，或它不是 HTTPS。",
		Action:  "请先把 server.public_url 设为 HTTPS 地址。",
	}
}

func checkAuthSecret() DeploymentCheckItem {
	if config.AppConfig == nil {
		return DeploymentCheckItem{Name: "会话密钥", Status: deploymentCheckFail, Summary: "配置尚未加载"}
	}

	secret := strings.TrimSpace(config.AppConfig.Auth.SecretKey)
	if len(secret) < 24 {
		return DeploymentCheckItem{
			Name:    "会话密钥",
			Status:  deploymentCheckWarn,
			Summary: "auth.secret_key 看起来偏短",
			Detail:  fmt.Sprintf("当前长度 %d", len(secret)),
			Action:  "建议使用至少 24 位以上的随机字符串，避免多实例或重启后会话安全性不足。",
		}
	}

	return DeploymentCheckItem{
		Name:    "会话密钥",
		Status:  deploymentCheckPass,
		Summary: "会话密钥长度正常",
		Detail:  fmt.Sprintf("当前长度 %d", len(secret)),
	}
}

func checkBootstrapExposure() DeploymentCheckItem {
	if bootstrap.BootstrapSetupPending() {
		return DeploymentCheckItem{
			Name:    "初始化暴露面",
			Status:  deploymentCheckPass,
			Summary: "当前仍处于初始化阶段，只允许 localhost 直连",
			Detail:  "在首次改密完成前，公网访问会被拦截。",
		}
	}

	return DeploymentCheckItem{
		Name:    "初始化暴露面",
		Status:  deploymentCheckPass,
		Summary: "初始化已完成，公网入口按正式登录流程工作",
		Detail:  "如果需要重新恢复管理员密码，请使用本机专用的 /recover。",
	}
}

func normalizedTrustedProxies() []string {
	if config.AppConfig == nil {
		return nil
	}

	var result []string
	for _, entry := range config.AppConfig.Server.TrustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func onlyLoopbackTrusted(entries []string) bool {
	if len(entries) == 0 {
		return false
	}

	for _, entry := range entries {
		if _, cidr, err := net.ParseCIDR(entry); err == nil {
			if !cidr.Contains(net.ParseIP("127.0.0.1")) && !cidr.Contains(net.ParseIP("::1")) {
				return false
			}
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}
