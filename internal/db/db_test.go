package db

import (
	"errors"
	"os"
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

func TestOpenReadOnlyDoesNotCreateMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")

	_, _, err := OpenReadOnly(path)
	if err == nil {
		t.Fatal("OpenReadOnly should reject a missing database")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenReadOnly error = %v, want os.ErrNotExist", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("read-only open created %s: stat error = %v", path, statErr)
	}
}

func TestOpenReadOnlyReadsExistingDatabaseAndRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	writable, err := gorm.Open(sqlite.Open(sqliteDriverPath(path)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open writable database: %v", err)
	}
	sqlWritable, err := writable.DB()
	if err != nil {
		t.Fatalf("get writable sql handle: %v", err)
	}
	if err := writable.Exec("CREATE TABLE checks (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatalf("create test table: %v", err)
	}
	if err := sqlWritable.Close(); err != nil {
		t.Fatalf("close writable database: %v", err)
	}

	readOnly, sqlReadOnly, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("open read-only database: %v", err)
	}
	t.Cleanup(func() { _ = sqlReadOnly.Close() })

	var count int
	if err := readOnly.Raw("SELECT COUNT(*) FROM checks").Scan(&count).Error; err != nil {
		t.Fatalf("read from read-only database: %v", err)
	}
	if count != 0 {
		t.Fatalf("row count = %d, want 0", count)
	}
	if err := readOnly.Exec("INSERT INTO checks (value) VALUES (?)", "blocked").Error; err == nil {
		t.Fatal("read-only database accepted an INSERT")
	}
}
