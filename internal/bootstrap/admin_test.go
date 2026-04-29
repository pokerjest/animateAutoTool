package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func withBootstrapTestDataDir(t *testing.T) {
	t.Helper()

	tempRoot := t.TempDir()
	prevPaths := config.AppPaths
	config.AppPaths = config.Paths{
		RootDir: tempRoot,
		DataDir: filepath.Join(tempRoot, "data"),
	}
	t.Cleanup(func() {
		config.AppPaths = prevPaths
	})
}

func TestPendingAdminBootstrapInfoReturnsFalseWhenPasswordDrifts(t *testing.T) {
	withBootstrapTestDataDir(t)
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("current-password-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	admin := model.User{
		Username:     "admin",
		PasswordHash: string(hashedPassword),
	}
	if err := db.DB.Create(&admin).Error; err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}

	if err := SaveAdminBootstrapInfo(AdminBootstrapInfo{
		Username: "admin",
		Password: "stale-bootstrap-password",
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	if _, pending := PendingAdminBootstrapInfo(); pending {
		t.Fatal("expected stale bootstrap password to stop setup enforcement")
	}

	if _, err := LoadAdminBootstrapInfo(); !os.IsNotExist(err) {
		t.Fatalf("expected stale bootstrap file to be cleared, got %v", err)
	}
}
