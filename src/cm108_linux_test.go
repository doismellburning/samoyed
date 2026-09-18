package direwolf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A regular file stands in for a HID: it can be opened and written, but it does
// not answer HIDIOCGRAWINFO, so it exercises the ioctl failure path.
func notAHID(t *testing.T) string {
	t.Helper()

	var name = filepath.Join(t.TempDir(), "hidraw0")

	require.NoError(t, os.WriteFile(name, nil, 0o600))

	return name
}

func TestCM108WriteReportsIoctlFailure(t *testing.T) {
	// Regression test: the ioctl failure was tested for under "err == nil", so
	// a device that could not be interrogated was accepted silently, while a
	// device that answered with an unknown vid/pid was reported as an ioctl
	// failure - with a nil errno at that.
	var name = notAHID(t)

	var result int

	var output = captureStdout(t, func() {
		result = cm108_write(name, 0x04, 0x04)
	})

	assert.Equal(t, 0, result)
	assert.Contains(t, output, "HIDIOCGRAWINFO")
	assert.NotContains(t, output, "nil")
}
