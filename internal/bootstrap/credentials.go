package bootstrap

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/pokerjest/animateAutoTool/internal/config"
)

func credentialsDir() string {
	return config.DataPath("bootstrap")
}

type AListCredentials struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token,omitempty"`
}

type JellyfinCredentials struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	APIKey   string `json:"api_key,omitempty"`
}

func SaveAListCredentials(creds AListCredentials) error {
	return save("alist.json", creds)
}

func LoadAListCredentials() (AListCredentials, error) {
	var creds AListCredentials
	err := load("alist.json", &creds)
	return creds, err
}

func SaveJellyfinCredentials(creds JellyfinCredentials) error {
	return save("jellyfin.json", creds)
}

func LoadJellyfinCredentials() (JellyfinCredentials, error) {
	var creds JellyfinCredentials
	err := load("jellyfin.json", &creds)
	return creds, err
}

func save(filename string, payload interface{}) error {
	dir := credentialsDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, filename), data, 0600)
}

func load(filename string, dst interface{}) error {
	credentialPath := filepath.Clean(filepath.Join(credentialsDir(), filename))
	data, err := os.ReadFile(credentialPath) //nolint:gosec // filename is fixed by the bootstrap credential helpers.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		return err
	}

	return json.Unmarshal(data, dst)
}

func remove(filename string) error {
	err := os.Remove(filepath.Join(credentialsDir(), filename))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
