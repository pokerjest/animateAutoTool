package api

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	restoreTokenTTL             = 30 * time.Minute
	restoreTokenCleanupInterval = 10 * time.Minute
)

type restoreArtifact struct {
	Path      string
	CreatedAt time.Time
}

var (
	restoreArtifacts            sync.Map
	restoreArtifactsJanitorOnce sync.Once
)

func registerRestoreArtifact(path string) string {
	ensureRestoreArtifactJanitor()

	token := uuid.NewString()
	restoreArtifacts.Store(token, restoreArtifact{
		Path:      path,
		CreatedAt: time.Now(),
	})
	return token
}

func resolveRestoreArtifact(token string) (string, error) {
	if token == "" {
		return "", errors.New("missing restore token")
	}

	val, ok := restoreArtifacts.Load(token)
	if !ok {
		return "", errors.New("restore file expired or not found")
	}

	artifact := val.(restoreArtifact)
	if restoreArtifactExpired(artifact, time.Now()) {
		restoreArtifacts.Delete(token)
		_ = os.Remove(artifact.Path)
		return "", errors.New("restore file expired or not found")
	}

	return artifact.Path, nil
}

func consumeRestoreArtifact(token string) (string, error) {
	path, err := resolveRestoreArtifact(token)
	if err != nil {
		return "", err
	}

	restoreArtifacts.Delete(token)
	return path, nil
}

func discardRestoreArtifact(token string) {
	path, err := consumeRestoreArtifact(token)
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

func ensureRestoreArtifactJanitor() {
	restoreArtifactsJanitorOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(restoreTokenCleanupInterval)
			defer ticker.Stop()
			for now := range ticker.C {
				cleanupExpiredRestoreArtifacts(now)
			}
		}()
	})
}

func cleanupExpiredRestoreArtifacts(now time.Time) {
	restoreArtifacts.Range(func(key, value interface{}) bool {
		token, tokenOK := key.(string)
		artifact, artifactOK := value.(restoreArtifact)
		if !tokenOK || !artifactOK {
			restoreArtifacts.Delete(key)
			return true
		}
		if restoreArtifactExpired(artifact, now) {
			restoreArtifacts.Delete(token)
			_ = os.Remove(artifact.Path)
		}
		return true
	})
}

func restoreArtifactExpired(artifact restoreArtifact, now time.Time) bool {
	return now.Sub(artifact.CreatedAt) > restoreTokenTTL
}
