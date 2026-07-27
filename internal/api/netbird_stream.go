package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

const netBirdStreamTokenTTL = 12 * time.Hour

type netBirdStreamClaims struct {
	EpisodeID uint   `json:"episode_id"`
	Provider  string `json:"provider,omitempty"`
	ItemID    string `json:"item_id,omitempty"`
	UserID    uint   `json:"user_id"`
	ExpiresAt int64  `json:"expires_at"`
}

func signNetBirdStreamToken(episodeID, userID uint, expiresAt time.Time) (string, error) {
	if episodeID == 0 || userID == 0 {
		return "", errors.New("missing netbird stream token subject")
	}
	claims, err := json.Marshal(netBirdStreamClaims{
		EpisodeID: episodeID,
		UserID:    userID,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claims)
	return payload + "." + netBirdStreamSignature(payload), nil
}

func signMediaNetBirdStreamToken(provider, itemID string, userID uint, expiresAt time.Time) (string, error) {
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(itemID) == "" || userID == 0 {
		return "", errors.New("missing netbird media stream token subject")
	}
	claims, err := json.Marshal(netBirdStreamClaims{
		Provider:  strings.TrimSpace(provider),
		ItemID:    strings.TrimSpace(itemID),
		UserID:    userID,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claims)
	return payload + "." + netBirdStreamSignature(payload), nil
}

func netBirdStreamSignature(payload string) string {
	secret := ""
	if config.AppConfig != nil {
		secret = config.AppConfig.Auth.SecretKey
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyNetBirdStreamToken(token string, episodeID uint, now time.Time) error {
	claims, err := decodeNetBirdStreamToken(token, now)
	if err != nil {
		return err
	}
	if claims.EpisodeID == 0 || claims.EpisodeID != episodeID {
		return errors.New("netbird stream token subject mismatch")
	}
	return nil
}

func verifyMediaNetBirdStreamToken(token, provider, itemID string, now time.Time) error {
	claims, err := decodeNetBirdStreamToken(token, now)
	if err != nil {
		return err
	}
	if claims.Provider == "" || claims.ItemID == "" || !strings.EqualFold(claims.Provider, provider) || claims.ItemID != itemID {
		return errors.New("netbird media stream token subject mismatch")
	}
	return nil
}

func decodeNetBirdStreamToken(token string, now time.Time) (netBirdStreamClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return netBirdStreamClaims{}, errors.New("invalid netbird stream token")
	}
	// Compare the canonical encoded signatures directly. Raw base64 decoding
	// ignores non-zero padding bits, which could otherwise let a tampered final
	// character decode to the same signature bytes.
	expected := netBirdStreamSignature(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return netBirdStreamClaims{}, errors.New("invalid netbird stream signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return netBirdStreamClaims{}, errors.New("invalid netbird stream claims")
	}
	var claims netBirdStreamClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return netBirdStreamClaims{}, errors.New("invalid netbird stream claims")
	}
	if claims.UserID == 0 || claims.ExpiresAt <= now.Unix() {
		return netBirdStreamClaims{}, errors.New("netbird stream token expired")
	}
	return claims, nil
}

func NetBirdProxyVideoHandler(c *gin.Context) {
	episodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || episodeID == 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	if err := verifyNetBirdStreamToken(c.Query("token"), uint(episodeID), time.Now()); err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	proxyVideoForEpisode(c, uint(episodeID))
}

func NetBirdProxyMediaHandler(c *gin.Context) {
	provider := strings.TrimSpace(c.Param("provider"))
	itemID := strings.TrimSpace(c.Param("id"))
	if provider == "" || itemID == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(provider, "jellyfin") {
		c.Status(http.StatusNotFound)
		return
	}
	if err := verifyMediaNetBirdStreamToken(c.Query("token"), provider, itemID, time.Now()); err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	proxyVideoForJellyfinItem(c, itemID)
}
