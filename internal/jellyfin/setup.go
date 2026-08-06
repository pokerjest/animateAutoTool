package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

var ErrAlreadyConfigured = errors.New("jellyfin server already configured")

var (
	serverReadyAttempts      = 30
	serverReadyPollDelay     = 2 * time.Second
	authRetryAttempts        = 5
	authRetryDelay           = 2 * time.Second
	startupUserRetryAttempts = 5
	startupUserRetryDelay    = 2 * time.Second
)

func startupWizardCompleted(info *PublicSystemInfo) bool {
	return info != nil && info.StartupWizardCompleted != nil && *info.StartupWizardCompleted
}

func shouldRetryStartupUser(err error) bool {
	return !HasStatus(err, http.StatusUnauthorized, http.StatusForbidden)
}

// AttemptZeroConfig tries to perform a zero-config setup if Jellyfin is brand new.
// It returns the generated API key if successful, empty string if skipped or failed.
func AttemptZeroConfig(url, username, password string) (string, error) {
	return AttemptZeroConfigContext(context.Background(), url, username, password)
}

func AttemptZeroConfigContext(ctx context.Context, url, username, password string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client := NewClient(url, "")
	var publicInfo *PublicSystemInfo

	log.Println("Jellyfin: Waiting for server to be ready...")
	up := false
	for i := 0; i < serverReadyAttempts; i++ {
		if info, err := client.GetPublicInfoContext(ctx); err == nil {
			publicInfo = info
			up = true
			break
		}
		if !waitForSetupRetry(ctx, serverReadyPollDelay) {
			return "", ctx.Err()
		}
	}
	if !up {
		return "", fmt.Errorf("timeout waiting for jellyfin to start")
	}

	log.Println("Jellyfin: Checking status...")
	for i := 0; i < authRetryAttempts; i++ {
		if authResp, err := client.AuthenticateContext(ctx, username, password); err == nil {
			log.Println("Jellyfin: Already configured (Auth successful). Using session token as API Key.")
			return authResp.AccessToken, nil
		}
		if !waitForSetupRetry(ctx, authRetryDelay) {
			return "", ctx.Err()
		}
	}
	if startupWizardCompleted(publicInfo) {
		return "", fmt.Errorf("%w: stored bootstrap credentials were rejected", ErrAlreadyConfigured)
	}

	log.Println("Jellyfin: Attempting Zero-Config Setup...")
	var err error
	for i := 0; i < startupUserRetryAttempts; i++ {
		err = client.UpdateStartupUserContext(ctx, username, password)
		if err == nil {
			break
		}
		if !shouldRetryStartupUser(err) {
			return "", fmt.Errorf("%w: startup wizard is unavailable for the current server", ErrAlreadyConfigured)
		}
		log.Printf("Jellyfin: UpdateStartupUser failed (attempt %d/%d): %v. Retrying...", i+1, startupUserRetryAttempts, err)
		if !waitForSetupRetry(ctx, startupUserRetryDelay) {
			return "", ctx.Err()
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to set startup user (maybe already set up?): %v", err)
	}

	if err := client.CompleteStartupWizardContext(ctx); err != nil {
		return "", fmt.Errorf("failed to complete wizard: %v", err)
	}
	authResp, err := client.AuthenticateContext(ctx, username, password)
	if err != nil {
		return "", fmt.Errorf("failed to authenticate after setup: %v", err)
	}

	log.Printf("Jellyfin: Zero-Config successful; session API key generated.")
	return authResp.AccessToken, nil
}

func waitForSetupRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
func (c *Client) UpdateStartupUser(username, password string) error {
	return c.UpdateStartupUserContext(context.Background(), username, password)
}

func (c *Client) UpdateStartupUserContext(ctx context.Context, username, password string) error {
	req := map[string]string{
		"Name":     username,
		"Password": password,
	}
	_, err := c.doContext(ctx, "POST", "/Startup/User", req)
	return err
}

func (c *Client) CompleteStartupWizard() error {
	return c.CompleteStartupWizardContext(context.Background())
}

func (c *Client) CompleteStartupWizardContext(ctx context.Context) error {
	_, err := c.doContext(ctx, "POST", "/Startup/Complete", nil)
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
