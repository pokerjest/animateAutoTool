package alist

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

var client = resty.New().SetTimeout(10 * time.Second)

// Common response wrapper
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"message"`
	Data interface{} `json:"data"`
}

var cachedToken string

var ErrOfflineDownloadNotImplemented = errors.New("offline download via AList is not implemented yet")

const DefaultAListURL = "http://127.0.0.1:5244"

func getBaseUrl() string {
	if creds, err := bootstrap.LoadAListCredentials(); err == nil && creds.URL != "" {
		if db.DB == nil {
			return creds.URL
		}
	}

	var cfg model.GlobalConfig
	// We assume DB is initialized when this is called.
	// launcher handles binaries before DB init, but API calls happen after DB init.
	if db.DB == nil {
		if creds, err := bootstrap.LoadAListCredentials(); err == nil && creds.URL != "" {
			return creds.URL
		}
		return DefaultAListURL
	}

	if err := db.DB.Where("key = ?", model.ConfigKeyAListUrl).First(&cfg).Error; err != nil {
		return DefaultAListURL
	}
	if cfg.Value == "" {
		if creds, err := bootstrap.LoadAListCredentials(); err == nil && creds.URL != "" {
			return creds.URL
		}
		return DefaultAListURL
	}
	// Also if cfg.Value is "http://localhost:5244", we might want to replace it?
	// But let's assume user config is respected if set.
	return cfg.Value
}

func getToken() string {
	if db.DB != nil {
		var cfg model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyAListToken).First(&cfg).Error; err == nil && cfg.Value != "" {
			return cfg.Value
		}
	}

	if creds, err := bootstrap.LoadAListCredentials(); err == nil && creds.Token != "" {
		cachedToken = creds.Token
		return creds.Token
	}

	if cachedToken != "" {
		return cachedToken
	}

	// Try Login
	var res struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}

	creds, err := bootstrap.LoadAListCredentials()
	if err != nil || creds.Password == "" {
		return ""
	}
	if creds.Username == "" {
		creds.Username = "admin"
	}

	_, err = client.R().SetBody(map[string]string{
		"username": creds.Username,
		"password": creds.Password,
	}).SetResult(&res).Post(getBaseUrl() + "/api/auth/login")

	if err == nil && res.Code == 200 {
		cachedToken = res.Data.Token
		creds.Token = res.Data.Token
		_ = bootstrap.SaveAListCredentials(creds)
		if db.DB != nil {
			_ = db.SaveGlobalConfig(model.ConfigKeyAListUrl, getBaseUrl())
			_ = db.SaveGlobalConfig(model.ConfigKeyAListToken, res.Data.Token)
		}
		return res.Data.Token
	}

	return ""
}

func getStorageIdByMountPath(token, mountPath string) (int, error) {
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
		Data struct {
			Content []struct {
				Id        int    `json:"id"`
				MountPath string `json:"mount_path"`
			} `json:"content"`
		} `json:"data"`
	}

	_, err := client.R().
		SetHeader("Authorization", token).
		SetQueryParam("page", "1").
		SetQueryParam("per_page", "100").
		SetResult(&res).
		Get(getBaseUrl() + "/api/admin/storage/list")

	if err != nil {
		return 0, err
	}
	if res.Code != 200 {
		return 0, fmt.Errorf("list storage failed: %s", res.Msg)
	}

	for _, s := range res.Data.Content {
		if s.MountPath == mountPath {
			return s.Id, nil
		}
	}
	return 0, nil
}

func AddPikPakStorage(username, password, refreshToken, captchaToken string) error {
	token := getToken()

	// 1. Check if exists
	existingId, err := getStorageIdByMountPath(token, "/PikPak")
	if err != nil {
		return err
	}

	// Switch to Web platform simulation which is often more stable for "x-client-id" issues
	// when Android signatures are hard to get right.
	addition := map[string]string{
		"username":       username,
		"password":       password,
		"refresh_token":  refreshToken,
		"root_folder_id": "",
		// "platform": "web" tells Alist to use the web API flow
		"platform":      "web",
		"captcha_token": captchaToken,
	}
	additionJson, _ := json.Marshal(addition)

	payload := map[string]interface{}{
		"mount_path":       "/PikPak",
		"driver":           "PikPak",
		"cache_expiration": 30,
		"addition":         string(additionJson),
	}

	apiUrl := getBaseUrl() + "/api/admin/storage/create"
	if existingId > 0 {
		apiUrl = getBaseUrl() + "/api/admin/storage/update"
		payload["id"] = existingId
	}

	var res Response
	_, err = client.R().
		SetHeader("Authorization", token).
		SetBody(payload).
		SetResult(&res).
		Post(apiUrl)

	if err != nil {
		return err
	}
	if res.Code != 200 {
		return fmt.Errorf("alist api error: %s", res.Msg)
	}
	return nil
}

func GetPikPakStatus() (string, error) {
	token := getToken()

	var res struct {
		Code int `json:"code"`
		Data struct {
			Content []struct {
				MountPath string `json:"mount_path"`
				Status    string `json:"status"`
				Driver    string `json:"driver"`
			} `json:"content"`
		} `json:"data"`
	}

	resp, err := client.R().
		SetHeader("Authorization", token).
		SetQueryParam("page", "1").
		SetQueryParam("per_page", "100").
		SetResult(&res).
		Get(getBaseUrl() + "/api/admin/storage/list")

	if err != nil {
		return "Error", err
	}
	if resp.StatusCode() != 200 || res.Code != 200 {
		return "AuthFail", nil
	}

	for _, s := range res.Data.Content {
		if s.MountPath == "/PikPak" || s.Driver == "PikPak" {
			return s.Status, nil
		}
	}

	return "未配置", nil
}

func BaseURL() string {
	return getBaseUrl()
}

func CheckConnection() (bool, string) {
	token := ""
	hasCredentials := false

	if db.DB != nil {
		var cfg model.GlobalConfig
		if err := db.DB.Where("key = ?", model.ConfigKeyAListToken).First(&cfg).Error; err == nil && cfg.Value != "" {
			token = cfg.Value
			hasCredentials = true
		}
	}

	if creds, err := bootstrap.LoadAListCredentials(); err == nil {
		if creds.Token != "" {
			token = creds.Token
			hasCredentials = true
		}
		if creds.Password != "" {
			hasCredentials = true
		}
	}

	if token == "" {
		token = getToken()
	}
	if token != "" {
		hasCredentials = true
	}
	if !hasCredentials {
		return false, "Credentials missing"
	}
	if token == "" {
		return false, "Authentication failed"
	}

	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"message"`
	}
	resp, err := client.R().
		SetHeader("Authorization", token).
		SetQueryParam("page", "1").
		SetQueryParam("per_page", "1").
		SetResult(&res).
		Get(getBaseUrl() + "/api/admin/storage/list")

	if err != nil {
		return false, err.Error()
	}
	if resp.StatusCode() != 200 || res.Code != 200 {
		return false, "Authentication failed"
	}

	return true, ""
}

func AddOfflineDownload(url, targetDir string) error {
	return ErrOfflineDownloadNotImplemented
}

func ListFiles(path string) ([]interface{}, error) {
	token := getToken()

	payload := map[string]interface{}{
		"path":     path,
		"page":     1,
		"per_page": 0,
		"refresh":  true,
	}

	var res struct {
		Code int `json:"code"`
		Data struct {
			Content []interface{} `json:"content"`
		} `json:"data"`
	}

	_, err := client.R().
		SetHeader("Authorization", token).
		SetBody(payload).
		SetResult(&res).
		Post(getBaseUrl() + "/api/fs/list")

	if err != nil {
		return nil, err
	}
	return res.Data.Content, nil
}

func GetSignUrl(path string) (string, error) {
	token := getToken()

	payload := map[string]interface{}{
		"path": path,
	}

	var res struct {
		Code int `json:"code"`
		Data struct {
			RawUrl string `json:"raw_url"`
			Sign   string `json:"sign"`
		} `json:"data"`
	}

	_, err := client.R().
		SetHeader("Authorization", token).
		SetBody(payload).
		SetResult(&res).
		Post(getBaseUrl() + "/api/fs/get")

	if err != nil {
		return "", err
	}
	return res.Data.RawUrl, nil
}
