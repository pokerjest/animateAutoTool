package service

import (
	"strings"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestBackupFilenameByMode(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 20, 10, 30, 45, 0, time.UTC)

	cases := []struct {
		mode     string
		wantSub  string
		wantTail string
	}{
		{BackupModeFull, "full", "_20260520_103045.db"},
		{BackupModeSettings, "settings", "_20260520_103045.db"},
		{BackupModeCloudflare, "cloudflare", "_20260520_103045.db"},
		{"", "full", "_20260520_103045.db"}, // unknown -> full
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			got := BackupFilename(tc.mode, ts)
			assert.True(t, strings.HasPrefix(got, "animateData_"+tc.wantSub),
				"filename %q should start with mode prefix %q", got, tc.wantSub)
			assert.True(t, strings.HasSuffix(got, tc.wantTail),
				"filename %q should end with timestamp tail %q", got, tc.wantTail)
		})
	}
}

func TestR2BackupObjectKeyByMode(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	cases := []struct {
		mode    string
		wantSub string
	}{
		{BackupModeFull, "full"},
		{BackupModeSettings, "settings"},
		{BackupModeCloudflare, "cloudflare"},
		{"  ", "full"}, // whitespace normalizes to full
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			got := R2BackupObjectKey(tc.mode, ts)
			assert.True(t, strings.HasPrefix(got, "animate_backup_"+tc.wantSub),
				"object key %q should start with mode prefix %q", got, tc.wantSub)
			assert.True(t, strings.HasSuffix(got, "_20260102_030405.db"),
				"object key %q should embed timestamp", got)
		})
	}
}

func TestIsCloudflareConfigKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		model.ConfigKeyR2Endpoint,
		model.ConfigKeyR2AccessKey,
		model.ConfigKeyR2SecretKey,
		model.ConfigKeyR2Bucket,
	} {
		assert.True(t, isCloudflareConfigKey(key), "%q should be classified as cloudflare key", key)
	}
	assert.False(t, isCloudflareConfigKey("tmdb_api_key"))
	assert.False(t, isCloudflareConfigKey(""))
}

func TestBackupContainsConfigsOnly(t *testing.T) {
	t.Parallel()
	assert.True(t, BackupContainsConfigsOnly(BackupModeSettings))
	assert.True(t, BackupContainsConfigsOnly(BackupModeCloudflare))
	assert.False(t, BackupContainsConfigsOnly(BackupModeFull))
	assert.False(t, BackupContainsConfigsOnly("")) // empty normalizes to full
}

func TestCleanBackupPath(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"./foo/bar":       "foo/bar",
		"foo//bar":        "foo/bar",
		"foo/./bar/../qz": "foo/qz",
	}
	for input, want := range cases {
		assert.Equal(t, want, CleanBackupPath(input), "input=%q", input)
	}
}

func TestCloudflareConfigKeysSnapshot(t *testing.T) {
	t.Parallel()
	keys := cloudflareConfigKeys()
	assert.Len(t, keys, 4, "exactly four R2 keys are expected; if you add one, update both code and tests")
	for _, k := range keys {
		assert.True(t, isCloudflareConfigKey(k))
	}
}
