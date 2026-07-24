package api

import (
	"errors"
	"net"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
)

const (
	passwordlessUserIDContextKey = "auth.passwordless_user_id"
	maxAuthIPAllowlistEntries    = 64
)

func splitAuthIPAllowlist(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
}

func normalizeAuthIPAllowlist(raw string) (string, error) {
	entries := splitAuthIPAllowlist(raw)
	if len(entries) > maxAuthIPAllowlistEntries {
		return "", errors.New("IP 白名单最多允许 64 项")
	}

	normalized := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if ip := net.ParseIP(entry); ip != nil {
			if ip.IsUnspecified() || ip.IsMulticast() {
				return "", errors.New("IP 白名单不能包含未指定或组播地址")
			}
			normalized[ip.String()] = struct{}{}
			continue
		}

		ip, network, err := net.ParseCIDR(entry)
		if err != nil {
			return "", errors.New("IP 白名单仅支持 IP 或 CIDR，例如 192.168.1.20 或 192.168.1.0/24")
		}
		ones, _ := network.Mask.Size()
		if ones == 0 {
			return "", errors.New("IP 白名单不允许使用覆盖全网的 /0 网段")
		}
		network.IP = ip.Mask(network.Mask)
		normalized[network.String()] = struct{}{}
	}

	result := make([]string, 0, len(normalized))
	for entry := range normalized {
		result = append(result, entry)
	}
	sort.Strings(result)
	return strings.Join(result, "\n"), nil
}

func authIPAllowlistEnabled() bool {
	return strings.EqualFold(
		strings.TrimSpace(config.SystemSetting(model.ConfigKeyAuthIPAllowlistEnabled)),
		model.ConfigValueTrue,
	)
}

func requestMatchesAuthIPAllowlist(c *gin.Context) bool {
	if !authIPAllowlistEnabled() || bootstrap.BootstrapSetupPending() {
		return false
	}

	clientIP := net.ParseIP(requestClientIP(c))
	if clientIP == nil {
		return false
	}

	for _, entry := range splitAuthIPAllowlist(config.SystemSetting(model.ConfigKeyAuthIPAllowlist)) {
		if allowedIP := net.ParseIP(entry); allowedIP != nil {
			if allowedIP.Equal(clientIP) {
				return true
			}
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(clientIP) {
			return true
		}
	}
	return false
}

func passwordlessAdminForRequest(c *gin.Context) (*model.User, bool) {
	if !requestMatchesAuthIPAllowlist(c) || db.DB == nil {
		return nil, false
	}

	user, err := store.NewUserStore(db.DB).GetByUsername("admin")
	if err != nil {
		return nil, false
	}
	c.Set(passwordlessUserIDContextKey, user.ID)
	return user, true
}
