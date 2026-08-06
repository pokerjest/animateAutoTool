package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadLocalPosterFileUsesKnownPosterNames(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "cover.jpg"), []byte("\xff\xd8\xff\xe0local"), 0o600))
	require.Equal(t, []byte("\xff\xd8\xff\xe0local"), loadLocalPosterFile(directory))
}

func TestLoadLocalPosterFileIgnoresNonImages(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "poster.jpg"), []byte("not an image"), 0o600))
	require.Empty(t, loadLocalPosterFile(directory))
}
