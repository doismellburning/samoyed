// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package cm108

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

// TestDeviceWarning pins down which message goes with which outcome.  Dire
// Wolf's cm108.c, and this code before it was fixed, had the two the wrong way
// round: a recognised device was told it was unsupported, and an unrecognised
// one got an ioctl failure message reporting a nil errno.
func TestDeviceWarning(t *testing.T) {
	var cmedia = &unix.HIDRawDevInfo{Bustype: 3, Vendor: CMEDIA_VID, Product: CMEDIA_PID_CM108B}
	var mouse = &unix.HIDRawDevInfo{Bustype: 3, Vendor: 0x0461, Product: 0x4d15}

	var testCases = []struct {
		name     string
		info     *unix.HIDRawDevInfo
		ioctlErr error
		expected string
	}{
		{
			name:     "supported device says nothing",
			info:     cmedia,
			ioctlErr: nil,
			expected: "",
		},
		{
			name:     "unsupported device warns about the device",
			info:     mouse,
			ioctlErr: nil,
			expected: "/dev/hidraw0 is not a supported device type.  Proceed at your own risk.  vid=0461 pid=4d15\n",
		},
		{
			name:     "failed ioctl reports the ioctl, not the uninitialised device info",
			info:     new(unix.HIDRawDevInfo),
			ioctlErr: unix.ENOTTY,
			expected: "ioctl HIDIOCGRAWINFO failed for /dev/hidraw0. errno = inappropriate ioctl for device.\n",
		},
		{
			// A failed ioctl wins even when the device info happens to hold
			// something we would otherwise have been happy with.
			name:     "failed ioctl beats plausible-looking device info",
			info:     cmedia,
			ioctlErr: errors.New("boom"),
			expected: "ioctl HIDIOCGRAWINFO failed for /dev/hidraw0. errno = boom.\n",
		},
		{
			name:     "missing device info says nothing",
			info:     nil,
			ioctlErr: nil,
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, deviceWarning("/dev/hidraw0", tc.info, tc.ioctlErr))
		})
	}
}

// TestDeviceWarningHighVendorID checks that a vendor or product id with the top
// bit set, which the kernel's signed hidraw_devinfo fields render as a negative
// number, still prints as the four hexadecimal digits the message promises.
func TestDeviceWarningHighVendorID(t *testing.T) {
	var info = &unix.HIDRawDevInfo{Bustype: 3, Vendor: -1, Product: -2}

	assert.Equal(t,
		"/dev/hidraw0 is not a supported device type.  Proceed at your own risk.  vid=ffff pid=fffe\n",
		deviceWarning("/dev/hidraw0", info, nil))
}
