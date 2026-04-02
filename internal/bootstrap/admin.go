package bootstrap

import (
	"os"
	"time"
)

type AdminBootstrapInfo struct {
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	CreatedAt time.Time `json:"created_at"`
}

func SaveAdminBootstrapInfo(info AdminBootstrapInfo) error {
	return save("admin.json", info)
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
	return &info, true
}

func BootstrapSetupPending() bool {
	_, pending := PendingAdminBootstrapInfo()
	return pending
}

func ClearAdminBootstrapInfo() error {
	return remove("admin.json")
}
