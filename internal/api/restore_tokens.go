package api

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

const restoreTokenTTL = 30 * time.Minute

type restoreArtifact struct {
	Path      string
	CreatedAt time.Time
}

var restoreArtifacts sync.Map

func registerRestoreArtifact(path string) string {
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
	if time.Since(artifact.CreatedAt) > restoreTokenTTL {
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
