package cmd

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/alist-org/alist/v3/drivers/pikpak"
	alistdb "github.com/alist-org/alist/v3/internal/db"
	alistmodel "github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
)

// GetAdminPassword retrieves the admin password from the internal database.
func GetAdminPassword() (string, error) {
	user, err := op.GetAdmin()
	if err != nil {
		return "", err
	}
	return user.Password, nil
}

// AddPikPakStorage adds or updates the PikPak storage in AList
func AddPikPakStorage(username, password, refreshToken string) error {
	const mountPath = "/PikPak"
	ctx := context.Background()

	// Check if exists in DB directly (to catch failed/unloaded storages)
	existing, err := alistdb.GetStorageByMountPath(mountPath)

	var existingDeviceID, existingRefreshToken, existingCaptchaToken string

	if err == nil && existing != nil {
		// Found existing, try to extract tokens and device ID for reuse
		var existingAdd pikpak.Addition
		if jsonErr := json.Unmarshal([]byte(existing.Addition), &existingAdd); jsonErr == nil {
			// Only reuse tokens/device if username matches
			if existingAdd.Username == username {
				existingDeviceID = existingAdd.DeviceID
				existingRefreshToken = existingAdd.RefreshToken
				existingCaptchaToken = existingAdd.CaptchaToken
			}

			// Check if we can skip update entirely
			if existing.Status == "work" {
				if existingAdd.Username == username &&
					existingAdd.Password == password &&
					existingAdd.Platform == "web" {
					return nil
				}
			}
		}

		// Found in DB but needs update/fix, delete it to ensure clean state
		if err := op.DeleteStorageById(ctx, existing.ID); err != nil {
			return fmt.Errorf("failed to delete existing pikpak storage (id: %d): %w", existing.ID, err)
		}
	}

	// Generate deterministic Device ID if missing to prevent "Too frequent" error on re-login
	if existingDeviceID == "" {
		hash := md5.Sum([]byte(username + "animate_pikpak_web_device_v1"))
		existingDeviceID = hex.EncodeToString(hash[:])
	}

	// Use provided refresh token, or reuse existing if not provided
	if refreshToken != "" {
		existingRefreshToken = refreshToken
	}

	addition := pikpak.Addition{
		Username:     username,
		Password:     password,
		Platform:     "web",                // Switch to Web platform to match browser token
		DeviceID:     existingDeviceID,     // Reuse or Deterministic ID
		RefreshToken: existingRefreshToken, // Reuse token to skip login if valid
		CaptchaToken: existingCaptchaToken, // Reuse captcha token if valid
	}

	additionJson, err := json.Marshal(addition)
	if err != nil {
		return fmt.Errorf("failed to marshal pikpak addition: %w", err)
	}

	// Create new storage
	newStorage := alistmodel.Storage{
		MountPath: mountPath,
		Driver:    "PikPak",
		Addition:  string(additionJson),
		Status:    "work",
	}

	if _, err := op.CreateStorage(ctx, newStorage); err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	return nil
}

// GetPikPakStatus returns the status of the PikPak storage
func GetPikPakStatus() (string, error) {
	const mountPath = "/PikPak"
	drv, err := op.GetStorageByMountPath(mountPath)
	if err != nil {
		return "未配置", nil // Not configured
	}
	if drv == nil {
		return "未找到驱动", nil
	}

	// Driver interface has GetStorage() which has Status field?
	// No, model.Storage has Status (string).
	// Let's check model definition if needed, but storage.go indicates storage.Status is used.
	// Also driverStorage.SetStatus(WORK/err) is used.

	storage := drv.GetStorage()
	return storage.Status, nil
}
