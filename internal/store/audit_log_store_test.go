package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	return db
}

func TestAuditLogStoreCreateAndList(t *testing.T) {
	t.Parallel()
	db := newAuditTestDB(t)
	s := NewAuditLogStore(db)

	rows := []model.AuditLog{
		{Username: "alice", Action: "login.success", Outcome: "success", IP: "127.0.0.1"},
		{Username: "bob", Action: "login.failure", Outcome: "failure", IP: "127.0.0.1"},
		{Username: "alice", Action: "password.change", Outcome: "success", IP: "10.0.0.5"},
	}
	for i := range rows {
		require.NoError(t, s.Create(&rows[i]))
		require.NotZero(t, rows[i].ID, "Create should fill in primary key")
		require.False(t, rows[i].CreatedAt.IsZero(), "Create should fill in CreatedAt")
	}

	all, err := s.ListRecent(AuditLogQuery{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	// Newest first — last inserted has the largest ID.
	assert.Equal(t, "password.change", all[0].Action)
}

func TestAuditLogStoreFilters(t *testing.T) {
	t.Parallel()
	db := newAuditTestDB(t)
	s := NewAuditLogStore(db)

	for _, entry := range []model.AuditLog{
		{Username: "alice", Action: "login.success", Outcome: "success"},
		{Username: "alice", Action: "login.failure", Outcome: "failure"},
		{Username: "bob", Action: "login.success", Outcome: "success"},
		{Username: "bob", Action: "subscription.delete", Outcome: "success"},
	} {
		e := entry
		require.NoError(t, s.Create(&e))
	}

	byAction, err := s.ListRecent(AuditLogQuery{Action: "login.success"})
	require.NoError(t, err)
	assert.Len(t, byAction, 2)
	for _, r := range byAction {
		assert.Equal(t, "login.success", r.Action)
	}

	byUser, err := s.ListRecent(AuditLogQuery{Username: "bob"})
	require.NoError(t, err)
	assert.Len(t, byUser, 2)
	for _, r := range byUser {
		assert.Equal(t, "bob", r.Username)
	}

	byOutcome, err := s.ListRecent(AuditLogQuery{Outcome: "failure"})
	require.NoError(t, err)
	assert.Len(t, byOutcome, 1)
	assert.Equal(t, "alice", byOutcome[0].Username)

	count, err := s.Count(AuditLogQuery{Username: "alice"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}

func TestAuditLogStoreLimitDefaultsAndCaps(t *testing.T) {
	t.Parallel()
	db := newAuditTestDB(t)
	s := NewAuditLogStore(db)

	// Insert 12 rows; default limit (100) returns all, custom limit 5 returns 5.
	for i := 0; i < 12; i++ {
		e := model.AuditLog{Username: "x", Action: "noop", Outcome: "success"}
		require.NoError(t, s.Create(&e))
	}

	all, err := s.ListRecent(AuditLogQuery{})
	require.NoError(t, err)
	assert.Len(t, all, 12)

	limited, err := s.ListRecent(AuditLogQuery{Limit: 5})
	require.NoError(t, err)
	assert.Len(t, limited, 5)

	// Negative / zero limit falls back to the default; impossible to exceed
	// 500 from 12 inserted rows, but ListRecent should still cap silently.
	capped, err := s.ListRecent(AuditLogQuery{Limit: 100000})
	require.NoError(t, err)
	assert.Len(t, capped, 12, "cap should not invent rows")
}

func TestAuditLogStoreNilSafety(t *testing.T) {
	t.Parallel()
	var nilStore *AuditLogStore
	assert.ErrorIs(t, nilStore.Create(&model.AuditLog{}), gorm.ErrInvalidDB)

	_, err := nilStore.ListRecent(AuditLogQuery{})
	assert.ErrorIs(t, err, gorm.ErrInvalidDB)

	_, err = nilStore.Count(AuditLogQuery{})
	assert.ErrorIs(t, err, gorm.ErrInvalidDB)

	// Create with a real store but nil entry is a no-op.
	db := newAuditTestDB(t)
	require.NoError(t, NewAuditLogStore(db).Create(nil))
}
