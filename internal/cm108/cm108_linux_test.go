// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package cm108

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

func TestCheckDeviceIoctlFailureIsReported(t *testing.T) {
	// This replaces the coverage TestCM108WriteReportsIoctlFailure gave while
	// the check lived in cm108_write and printed rather than returning.
	var err = CheckDevice(notAHID(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HIDIOCGRAWINFO")
	assert.NotErrorIs(t, err, ErrUnknownDevice)
}

func TestCheckDeviceMissing(t *testing.T) {
	var err = CheckDevice(filepath.Join(t.TempDir(), "nonexistent"))

	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestSetGPIOPinRejectsBadArguments(t *testing.T) {
	// Note there is no device here at all - these should be rejected before we
	// go anywhere near one.
	require.Error(t, SetGPIOPin("/dev/hidraw0", 0, 1))
	require.Error(t, SetGPIOPin("/dev/hidraw0", 9, 1))
	require.Error(t, SetGPIOPin("/dev/hidraw0", 3, 2))
	require.Error(t, SetGPIOPin("/dev/hidraw0", 3, -1))
}

func TestSetGPIOPinWrites(t *testing.T) {
	var name = notAHID(t)

	require.NoError(t, SetGPIOPin(name, 3, 1))

	var written, err = os.ReadFile(name) //nolint:gosec // Test file we just created.
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0x04, 0x04, 0}, written)

	require.NoError(t, SetGPIOPin(name, 3, 0))

	written, err = os.ReadFile(name) //nolint:gosec // Test file we just created.
	require.NoError(t, err)
	assert.Equal(t, []byte{0, 0, 0x00, 0x04, 0}, written)
}

func TestSetGPIOPinMissingDevice(t *testing.T) {
	var err = SetGPIOPin(filepath.Join(t.TempDir(), "nonexistent"), 3, 1)

	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestPermissionAdviceNamesDevice(t *testing.T) {
	var advice = strings.Join(PermissionAdvice("/dev/hidraw7"), "\n")

	assert.Contains(t, advice, "/dev/hidraw7")
	assert.Contains(t, advice, "99-direwolf-cmedia.rules")
}
