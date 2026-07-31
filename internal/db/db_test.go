package db

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSQLiteDriverPathAddsBusyTimeoutOnNonWindows(t *testing.T) {
	prev := currentDBGOOS
	currentDBGOOS = func() string { return "darwin" }
	t.Cleanup(func() { currentDBGOOS = prev })

	input := "/tmp/animate.db"
	want := "/tmp/animate.db?_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_pragma=foreign_keys(ON)"
	if got := sqliteDriverPath(input); got != want {
		t.Fatalf("sqliteDriverPath(%q) = %q, want %q", input, got, want)
	}
}

func TestSQLiteDriverPathAppliesBusyTimeout(t *testing.T) {
	prev := currentDBGOOS
	currentDBGOOS = func() string { return "darwin" }
	t.Cleanup(func() { currentDBGOOS = prev })

	target, err := gorm.Open(sqlite.Open(sqliteDriverPath(filepath.Join(t.TempDir(), "app.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	sqlDB, err := target.DB()
	if err != nil {
		t.Fatalf("read sql handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var timeout int
	if err := target.Raw("PRAGMA busy_timeout").Scan(&timeout).Error; err != nil {
		t.Fatalf("read busy timeout: %v", err)
	}
	if timeout != 5000 {
		t.Fatalf("busy timeout = %d, want 5000", timeout)
	}
	var synchronous int
	if err := target.Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil {
		t.Fatalf("read synchronous mode: %v", err)
	}
	if synchronous != 2 {
		t.Fatalf("synchronous = %d, want FULL (2)", synchronous)
	}
	var foreignKeys int
	if err := target.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read foreign keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	if err := CheckIntegrity(target); err != nil {
		t.Fatalf("quick check: %v", err)
	}
}

func TestSQLiteDriverPathUsesBusyTimeoutAndWALOnWindows(t *testing.T) {
	prev := currentDBGOOS
	currentDBGOOS = func() string { return "windows" }
	t.Cleanup(func() { currentDBGOOS = prev })

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain path",
			input: `C:\AnimateAutoTool\data\app.db`,
			want:  `C:\AnimateAutoTool\data\app.db?_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)`,
		},
		{
			name:  "existing query",
			input: `C:\AnimateAutoTool\data\app.db?cache=shared`,
			want:  `C:\AnimateAutoTool\data\app.db?cache=shared&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteDriverPath(tc.input); got != tc.want {
				t.Fatalf("sqliteDriverPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
