package alist

import (
	"github.com/alist-org/alist/v3/cmd"
	"github.com/alist-org/alist/v3/cmd/flags"
	log "github.com/sirupsen/logrus"
)

// StartAlistServer starts the embedded AList server in a goroutine
func StartAlistServer() {
	// Set Data Directory to data/alist via the public flags package
	// This ensures AList uses our isolated data directory
	flags.DataDir = "data/alist"

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("AList Server Panic: %v", r)
			}
		}()

		log.Println("Starting Embedded AList Server...")

		// Call the manual start function we exposed in internal/alist_source/cmd/server.go
		// This bypasses Cobra flag parsing and uses the flags.DataDir we set above
		cmd.StartManually("data/alist")
	}()
}

// GetAdminToken retrieves the admin token (password) from the running AList instance
func GetAdminToken() (string, error) {
	return cmd.GetAdminPassword()
}

// AddPikPakStorage adds or updates PikPak storage via the internal bridge
func AddPikPakStorage(username, password, refreshToken string) error {
	return cmd.AddPikPakStorage(username, password, refreshToken)
}

// GetPikPakStatus retrieves the current status of PikPak storage
func GetPikPakStatus() (string, error) {
	return cmd.GetPikPakStatus()
}

// AddOfflineDownload adds a magnet link to the specified path using the offline download tool
func AddOfflineDownload(url, targetDir string) error {
	return cmd.AddOfflineDownload(url, targetDir)
}

// ListFiles returns a list of files in the specified path from AList
func ListFiles(path string) ([]interface{}, error) {
	// We return interface{} because we don't want to expose internal AList models to the main app if possible,
	// but for simplicity we can return the objects here and let the handler marshal them.
	// Actually, Go's type system requires us to import the model pkg if we return []model.Obj.
	// Let's rely on the fact that `cmd` pkg is imported.
	objs, err := cmd.ListFiles(path)
	if err != nil {
		return nil, err
	}

	// Convert to []interface{} so consumers don't need to import internal AList packages?
	// Or just return as is if we import the type.
	// Since `alist-org/alist/v3/internal/model` is internal, we might have issues importing it in `api` package if Go modules are strict.
	// However, `cmd` is also internal in my fork? No, `cmd` is `github.com/alist-org/alist/v3/cmd`.
	// Wait, `alist-org/alist/v3/internal` packages are internal to alist module.
	// If I am outside that module (which I am, `github.com/pokerjest/animateAutoTool`), I technically CANNOT import `internal` packages.
	// But `internal/alist_source` IS inside my project structure now (I copied it/submodule).
	// So my module `github.com/pokerjest/animateAutoTool` owns it.
	// It's just a directory. So I CAN import `internal/alist_source/internal/model`.
	// But the import path in `bridge.go` is `github.com/alist-org/alist/v3/...`.
	// If `bridge.go` is inside `internal/alist_source/cmd`, and `go.mod` has replace directive or similar?
	// Ah, I am using the `alist_source` as a local package?
	// Actually, `service.go` imports `github.com/alist-org/alist/v3/cmd`.
	// This suggests we are using the vendored or replaced version?
	// User said `alist_source` was a submodule converted to directory.
	// So `go.mod` likely has: `replace github.com/alist-org/alist/v3 => ./internal/alist_source`.

	// Let's assume we can import `github.com/alist-org/alist/v3/internal/model`.
	// To minimize coupling, I'll return `interface{}` and marshal it in handler.

	result := make([]interface{}, len(objs))
	for i, v := range objs {
		result[i] = v
	}
	return result, nil
}

// GetSignUrl returns the direct link for a file
func GetSignUrl(path string) (string, error) {
	link, err := cmd.GetSignUrl(path)
	if err != nil {
		return "", err
	}
	return link.URL, nil
}
