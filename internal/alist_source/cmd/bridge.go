package cmd

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/alist-org/alist/v3/drivers/pikpak"
	alistdb "github.com/alist-org/alist/v3/internal/db"
	alistfs "github.com/alist-org/alist/v3/internal/fs"
	alistmodel "github.com/alist-org/alist/v3/internal/model"
	offlinetool "github.com/alist-org/alist/v3/internal/offline_download/tool"
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

	storage := drv.GetStorage()
	return storage.Status, nil
}

// AddOfflineDownload adds a magnet or URL to the specified path using the offline download tool
func AddOfflineDownload(url, targetDir string) error {
	ctx := context.Background()

	// Use PikPak as the tool
	// We might need to ensure PikPak tool is initialized or available.
	// AddURL takes "Tool" string.
	// Assuming "PikPak" is the tool name registered in AList.

	args := &offlinetool.AddURLArgs{
		URL:          url,
		DstDirPath:   targetDir, // e.g. /PikPak/InstantPlay
		Tool:         "PikPak",
		DeletePolicy: offlinetool.DeleteOnUploadSucceed, // Or DeleteNever? Usually we want to keep it in cloud
	}

	// Wait, DeletePolicy refers to the temporary file?
	// For "PikPak" tool, it adds task to cloud.
	// If the storage is also PikPak, it's optimized path.

	_, err := offlinetool.AddURL(ctx, args)
	if err != nil {
		return fmt.Errorf("failed to add offline download task: %w", err)
	}

	return nil
}

// ListFiles lists files in a directory
func ListFiles(path string) ([]alistmodel.Obj, error) {
	ctx := context.Background()
	// Create list args
	args := &alistfs.ListArgs{
		NoLog: true,
	}
	return alistfs.List(ctx, path, args)
}

// GetSignUrl gets the direct link for a file
func GetSignUrl(path string) (*alistmodel.Link, error) {
	ctx := context.Background()
	// Create link args
	args := alistmodel.LinkArgs{
		IP: "127.0.0.1",
	}
	// fs.Link returns (link, file, error)
	link, _, err := alistfs.Link(ctx, path, args)
	return link, err
}
