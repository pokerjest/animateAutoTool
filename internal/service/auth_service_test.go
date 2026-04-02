package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"golang.org/x/crypto/bcrypt"
)

const bootstrapAdminUsername = "admin"

func withBootstrapDataDir(t *testing.T) {
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

func TestEnsureDefaultUserCreatesRandomBootstrapAdmin(t *testing.T) {
	withBootstrapDataDir(t)
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	svc := NewAuthService()
	svc.EnsureDefaultUser()

	var users []model.User
	if err := db.DB.Find(&users).Error; err != nil {
		t.Fatalf("failed to fetch users: %v", err)
	}

	if len(users) != 1 {
		t.Fatalf("expected exactly one bootstrap user, got %d", len(users))
	}
	if users[0].Username != bootstrapAdminUsername {
		t.Fatalf("expected bootstrap username admin, got %s", users[0].Username)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(users[0].PasswordHash), []byte(bootstrapAdminUsername)); err == nil {
		t.Fatal("bootstrap user should not use the legacy admin/admin password")
	}

	info, err := bootstrap.LoadAdminBootstrapInfo()
	if err != nil {
		t.Fatalf("expected bootstrap admin info to be persisted: %v", err)
	}
	if info.Username != bootstrapAdminUsername {
		t.Fatalf("expected bootstrap admin username admin, got %s", info.Username)
	}
	if info.Password == "" || info.Password == bootstrapAdminUsername {
		t.Fatalf("expected random bootstrap password, got %q", info.Password)
	}
}

func TestEnsureDefaultUserRemovesLegacyRecoveryAccount(t *testing.T) {
	withBootstrapDataDir(t)
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	if _, err := NewAuthService().CreateUser("backup_admin", "legacy"); err != nil {
		t.Fatalf("failed to seed legacy recovery account: %v", err)
	}

	svc := NewAuthService()
	svc.EnsureDefaultUser()

	var count int64
	if err := db.DB.Model(&model.User{}).Where("username = ?", "backup_admin").Count(&count).Error; err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected legacy recovery account to be removed, got %d rows", count)
	}
}

func TestChangePasswordClearsBootstrapAdminInfo(t *testing.T) {
	withBootstrapDataDir(t)
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	svc := NewAuthService()
	svc.EnsureDefaultUser()

	info, err := bootstrap.LoadAdminBootstrapInfo()
	if err != nil {
		t.Fatalf("expected bootstrap admin info: %v", err)
	}

	var admin model.User
	if err := db.DB.Where("username = ?", bootstrapAdminUsername).First(&admin).Error; err != nil {
		t.Fatalf("failed to fetch admin user: %v", err)
	}

	if err := svc.ChangePassword(admin.ID, info.Password, "new-password-123"); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	if _, err := bootstrap.LoadAdminBootstrapInfo(); !os.IsNotExist(err) {
		t.Fatalf("expected bootstrap admin info to be cleared, got %v", err)
	}
}
