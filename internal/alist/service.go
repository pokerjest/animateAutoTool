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
