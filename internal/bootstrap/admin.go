package bootstrap

import (
	"os"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"golang.org/x/crypto/bcrypt"
)

type AdminBootstrapInfo struct {
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	CreatedAt time.Time `json:"created_at"`
}

func SaveAdminBootstrapInfo(info AdminBootstrapInfo) error {
	return save("admin.json", info)
}

func AdminBootstrapInfoPath() string {
	return config.DataPath("bootstrap", "admin.json")
}

func LoadAdminBootstrapInfo() (AdminBootstrapInfo, error) {
	var info AdminBootstrapInfo
	err := load("admin.json", &info)
	return info, err
}

func PendingAdminBootstrapInfo() (*AdminBootstrapInfo, bool) {
	info, err := LoadAdminBootstrapInfo()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		return nil, false
	}
	if !bootstrapPasswordStillActive(info) {
		_ = ClearAdminBootstrapInfo()
		return nil, false
	}
	return &info, true
}

func BootstrapSetupPending() bool {
	_, pending := PendingAdminBootstrapInfo()
	return pending
}

func ClearAdminBootstrapInfo() error {
	return remove("admin.json")
}

func bootstrapPasswordStillActive(info AdminBootstrapInfo) bool {
	if db.DB == nil {
		return true
	}

	var user model.User
	if err := db.DB.Where("username = ?", info.Username).First(&user).Error; err != nil {
		return false
	}

	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(info.Password)) == nil
}
