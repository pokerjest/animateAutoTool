package launcher

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetQBUrlForPlatform(t *testing.T) {
	t.Parallel()

	url, err := getQBUrlForPlatform(OSLinux, ArchAmd64)
	require.NoError(t, err)
	assert.Equal(t, QBUrlLinuxAmd64, url)

	url, err = getQBUrlForPlatform(OSLinux, ArchArm64)
	require.NoError(t, err)
	assert.Equal(t, QBUrlLinuxArm64, url)
}

func TestGetQBUrlForPlatformManualInstallPlatforms(t *testing.T) {
	t.Parallel()

	_, err := getQBUrlForPlatform(OSWindows, ArchAmd64)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrQBManualInstallRequired))

	_, err = getQBUrlForPlatform(OSDarwin, ArchArm64)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrQBManualInstallRequired))
}
