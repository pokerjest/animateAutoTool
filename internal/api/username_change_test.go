package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV1ChangeUsernameKeepsCurrentSessionAndRotatesLogin(t *testing.T) {
	resetAuthFixtures(t)
	resetAuditFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	body, err := json.Marshal(map[string]string{
		"current_password": "admin",
		"new_username":     "动画管理员",
	})
	require.NoError(t, err)
	changed := authedRequest(t, r, cookie, http.MethodPost, "/api/v1/session/change-username", body)
	require.Equal(t, http.StatusOK, changed.Code, changed.Body.String())
	assert.Contains(t, changed.Body.String(), "动画管理员")

	// The existing cookie remains valid and resolves the updated name because
	// sessions store the stable user ID, not the username.
	session := authedRequest(t, r, cookie, http.MethodGet, "/api/v1/session", nil)
	require.Equal(t, http.StatusOK, session.Code, session.Body.String())
	assert.Contains(t, session.Body.String(), `"username":"动画管理员"`)
	assert.Contains(t, session.Body.String(), `"authenticated":true`)

	oldLogin := httptest.NewRecorder()
	oldReq := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", bytes.NewBufferString(`{"username":"admin","password":"admin"}`))
	oldReq.Header.Set("Content-Type", "application/json")
	markLocalRequest(oldReq)
	r.ServeHTTP(oldLogin, oldReq)
	assert.Equal(t, http.StatusUnauthorized, oldLogin.Code)

	newLogin := httptest.NewRecorder()
	newReq := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", bytes.NewBufferString(`{"username":"动画管理员","password":"admin"}`))
	newReq.Header.Set("Content-Type", "application/json")
	markLocalRequest(newReq)
	r.ServeHTTP(newLogin, newReq)
	require.Equal(t, http.StatusOK, newLogin.Code, newLogin.Body.String())

	var rows []model.AuditLog
	require.NoError(t, db.DB.Where("action = ?", service.AuditActionUsernameChange).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, service.AuditOutcomeSuccess, rows[0].Outcome)
	assert.Equal(t, "admin", rows[0].Username)
	assert.Contains(t, rows[0].Details, `"old_username":"admin"`)
	assert.Contains(t, rows[0].Details, `"new_username":"动画管理员"`)
}

func TestV1ChangeUsernameRejectsInvalidPasswordNameAndDuplicate(t *testing.T) {
	tests := []struct {
		name     string
		password string
		username string
		seed     string
		message  string
	}{
		{name: "wrong password", password: "wrong", username: "new-admin", message: "当前密码不正确"},
		{name: "too short", password: "admin", username: "ab", message: "至少需要 3 个字符"},
		{name: "too long", password: "admin", username: strings.Repeat("a", 65), message: "不能超过 64 个字符"},
		{name: "control character", password: "admin", username: "new\nadmin", message: "不能包含换行或控制字符"},
		{name: "duplicate", password: "admin", username: "other-admin", seed: "other-admin", message: "用户名已被使用"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAuthFixtures(t)
			if tt.seed != "" {
				_, err := service.NewAuthService().CreateUser(tt.seed, "other-password")
				require.NoError(t, err)
			}
			r := setupRouter()
			cookie, _ := loginCookie(t, r, "admin")
			body, err := json.Marshal(map[string]string{
				"current_password": tt.password,
				"new_username":     tt.username,
			})
			require.NoError(t, err)

			response := authedRequest(t, r, cookie, http.MethodPost, "/api/v1/session/change-username", body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), tt.message)

			var admin model.User
			require.NoError(t, db.DB.Where("username = ?", "admin").First(&admin).Error)
		})
	}
}

func TestLegacyChangeUsernameRouteRemainsDeprecatedAndFunctional(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	body := []byte(`{"current_password":"admin","new_username":"legacy-admin"}`)

	response := authedRequest(t, r, cookie, http.MethodPost, "/api/change-username", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "true", response.Header().Get("Deprecation"))
	assert.Equal(t, `</api/v1>; rel="successor-version"`, response.Header().Get("Link"))
}
