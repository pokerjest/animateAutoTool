package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetAuditFixtures clears prior audit rows so each test starts from a
// known empty table.
func resetAuditFixtures(t *testing.T) {
	t.Helper()
	require.NoError(t, db.DB.Exec("DELETE FROM audit_logs").Error)
	t.Cleanup(func() {
		_ = db.DB.Exec("DELETE FROM audit_logs").Error
	})
}

func TestLoginSuccessAndFailureAreAudited(t *testing.T) {
	resetAuthFixtures(t)
	resetAuditFixtures(t)
	r := setupRouter()

	// 1) Wrong password → expect a login.failure entry.
	bad, _ := json.Marshal(map[string]string{"username": "admin", "password": "definitely-wrong"})
	wBad := httptest.NewRecorder()
	reqBad, _ := http.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(bad))
	reqBad.Header.Set("Content-Type", "application/json")
	markLocalRequest(reqBad)
	r.ServeHTTP(wBad, reqBad)
	require.Equal(t, http.StatusUnauthorized, wBad.Code)

	// 2) Right password → expect a login.success entry alongside the failure.
	_, _ = loginCookie(t, r, "admin")

	var rows []model.AuditLog
	require.NoError(t, db.DB.Order("id asc").Find(&rows).Error)
	require.GreaterOrEqual(t, len(rows), 2)

	var sawSuccess, sawFailure bool
	for _, row := range rows {
		switch row.Action {
		case "login.success":
			sawSuccess = true
			assert.Equal(t, "success", row.Outcome)
			assert.NotZero(t, row.UserID, "login.success should attribute to a real user")
			assert.Equal(t, "admin", row.Username)
		case "login.failure":
			sawFailure = true
			assert.Equal(t, "failure", row.Outcome)
			assert.Contains(t, row.Details, "invalid_credentials")
		}
	}
	assert.True(t, sawSuccess, "expected at least one login.success row")
	assert.True(t, sawFailure, "expected at least one login.failure row")
}

func TestPasswordChangeIsAudited(t *testing.T) {
	resetAuthFixtures(t)
	resetAuditFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	// Wrong old password → failure row
	bad, _ := json.Marshal(map[string]string{"old_password": "nope", "new_password": "long-enough-password-1"})
	wBad := authedRequest(t, r, cookie, http.MethodPost, "/api/change-password", bad)
	require.Equal(t, http.StatusBadRequest, wBad.Code)

	// Correct → success row
	ok, _ := json.Marshal(map[string]string{"old_password": "admin", "new_password": "long-enough-password-1"})
	wOK := authedRequest(t, r, cookie, http.MethodPost, "/api/change-password", ok)
	require.Equal(t, http.StatusOK, wOK.Code)

	var rows []model.AuditLog
	require.NoError(t, db.DB.Where("action = ?", "password.change").Order("id asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, "failure", rows[0].Outcome)
	assert.Equal(t, "success", rows[1].Outcome)
	for _, row := range rows {
		assert.Equal(t, "admin", row.Username)
		assert.NotZero(t, row.UserID)
	}
}

func TestAuditLogsEndpointReturnsRecentEntries(t *testing.T) {
	resetAuthFixtures(t)
	resetAuditFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	// The login above already wrote one success row. Confirm the list
	// endpoint surfaces it and honours the action filter.
	w := authedRequest(t, r, cookie, http.MethodGet, "/api/audit-logs?action=login.success", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Entries []model.AuditLog `json:"entries"`
		Count   int              `json:"count"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Entries)
	for _, e := range resp.Entries {
		assert.Equal(t, "login.success", e.Action)
		assert.Equal(t, "admin", e.Username)
	}
}
