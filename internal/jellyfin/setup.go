package jellyfin

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// AttemptZeroConfig tries to perform a zero-config setup if Jellyfin is brand new.
// It returns the generated API key if successful, empty string if skipped or failed.
func AttemptZeroConfig(url, username, password string) (string, error) {
	client := NewClient(url, "")

	// 1. Wait for Server to be Up
	log.Println("Jellyfin: Waiting for server to be ready...")
	up := false
	for i := 0; i < 30; i++ {
		if _, err := client.GetPublicInfo(); err == nil {
			up = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !up {
		return "", fmt.Errorf("timeout waiting for jellyfin to start")
	}

	// 2. Check if Startup Wizard is needed
	// The API endpoint /Startup/Configuration returns the state
	// NOTE: This endpoint might not be perfectly documented or version-dependent.
	// A simpler check: Try to create a user via startup wizard API. If it works, we were in wizard mode.
	// Or check /System/Info/Public -> If "StartupWizardCompleted" is false?
	// Actually, best bet is try to perform the first step of wizard.

	log.Println("Jellyfin: Checking status...")
	// Try to authenticate with retries. If it works, we are already set up.
	// We retry because sometimes the server is "Publicly" up but User Manager is still loading.
	for i := 0; i < 5; i++ {
		if authResp, err := client.Authenticate(username, password); err == nil {
			log.Println("Jellyfin: Already configured (Auth successful). Using session token as API Key.")
			return authResp.AccessToken, nil
		}
		time.Sleep(2 * time.Second)
	}

	// 3. Perform Startup Wizard Steps
	log.Println("Jellyfin: Attempting Zero-Config Setup...")

	// Step 1: Update Startup User
	// Keep trying in case DB migrations aren't done (which causes 500 error "Sequence contains no elements")
	var err error
	for i := 0; i < 5; i++ {
		err = client.UpdateStartupUser(username, password)
		if err == nil {
			break
		}
		log.Printf("Jellyfin: UpdateStartupUser failed (attempt %d/5): %v. Retrying...", i+1, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		// If this fails, maybe it's already set up or not in wizard mode
		// We assume failure here means "Not Fresh Install" or "Already Done"
		return "", fmt.Errorf("failed to set startup user (maybe already set up?): %v", err)
	}

	// Step 2: Complete Wizard
	// POST /Startup/Complete
	if err := client.CompleteStartupWizard(); err != nil {
		return "", fmt.Errorf("failed to complete wizard: %v", err)
	}

	// 4. Authenticate to get Token
	authResp, err := client.Authenticate(username, password)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate after setup: %v", err)
	}

	// Use the session AccessToken as the API Key.
	// This is more reliable than /Auth/Keys which seems to create inactive keys in some versions.
	apiKey := authResp.AccessToken

	log.Printf("Jellyfin: Zero-Config Successful! API Key (Session): %s", apiKey)
	return apiKey, nil
}

func (c *Client) UpdateStartupUser(username, password string) error {
	req := map[string]string{
		"Name":     username,
		"Password": password,
	}
	_, err := c.do("POST", "/Startup/User", req)
	return err
}

func (c *Client) CompleteStartupWizard() error {
	_, err := c.do("POST", "/Startup/Complete", nil)
	return err
}

func (c *Client) CreateApiKey(appName string) (string, error) {
	// POST /Auth/Keys
	req := map[string]string{
		"App": appName,
	}
	data, err := c.do("POST", "/Auth/Keys", req)
	if err != nil {
		return "", err
	}

	// Response: {"AccessToken":"...", "Id": ...}
	// Note: In older Emby/JF versions, this might return just the key string?
	// Checking docs: Returns ApiKey object.
	var resp struct {
		AccessToken string `json:"AccessToken"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	return resp.AccessToken, nil
}
