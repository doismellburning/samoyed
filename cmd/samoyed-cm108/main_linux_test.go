// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

func Test_cm108_print_inventory(t *testing.T) {
	var adapter = new(direwolf.CM108Thing)
	adapter.VID = 0x0d8c
	adapter.PID = 0x000c
	adapter.Product = "C-Media USB Audio Device"
	adapter.DevnodeSound = "/dev/snd/pcmC1D0c"
	adapter.Plughw = "plughw:1,0"
	adapter.Plughw2 = "plughw:Device,0"
	adapter.Devpath = "/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0/sound/card1"
	adapter.DevnodeHidraw = "/dev/hidraw0"
	adapter.DevnodeUSB = "/dev/bus/usb/001/002"

	// The same adapter's other sound device shares its devpath, so is only
	// suggested a name once.
	var playback = new(direwolf.CM108Thing)
	*playback = *adapter
	playback.DevnodeSound = "/dev/snd/pcmC1D0p"

	var other = new(direwolf.CM108Thing)
	other.VID = 0x1234
	other.PID = 0x5678
	other.Product = "Something Else"
	other.Devpath = "/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/sound/card2"
	other.DevnodeUSB = "/dev/bus/usb/001/003"

	var output = testutils.CaptureOutput(t, func() {
		cm108_print_inventory([]*direwolf.CM108Thing{adapter, playback, other})
	})

	assert.Contains(t, output, "**  0d8c 000c  C-Media USB Audio Device /dev/snd/pcmC1D0c")
	assert.Contains(t, output, "    1234 5678  Something Else")
	assert.Contains(t, output, "** = Can use Audio Adapter GPIO for PTT.")
	assert.Contains(t, output,
		"DEVPATH==\"/devices/pci0000:00/0000:00:14.0/usb1/1-1/1-1:1.0/sound/card?\", ATTR{id}=\"Fred\"\n"+
			"DEVPATH==\"/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/sound/card?\", ATTR{id}=\"Wilma\"\n"+
			"LABEL=\"my_usb_audio_end\"\n")
}

// Toggling a pin carries on until interrupted, so this runs it as a process
// of its own.
func Test_main_togglesThePin(t *testing.T) {
	// An ordinary file takes the writes a HID would, so the loop can run.
	var hid = filepath.Join(t.TempDir(), "hidraw")
	require.NoError(t, os.WriteFile(hid, nil, 0o600))

	testutils.StartMain(t, hid, "1")

	// Each state goes to the HID as a report, written over the last: GPIO 1
	// is the lowest bit of the data byte, and of the mask making it an
	// output.
	var reportIs = func(want []byte) func() bool {
		return func() bool {
			var report, _ = os.ReadFile(hid) //nolint:gosec // Our own file.

			return bytes.Equal(want, report)
		}
	}

	assert.Eventually(t, reportIs([]byte{0, 0, 0, 1, 0}), 5*time.Second, 10*time.Millisecond, "the pin should go low")
	assert.Eventually(t, reportIs([]byte{0, 0, 1, 1, 0}), 5*time.Second, 10*time.Millisecond, "and then high")
}

func Test_main_fails(t *testing.T) {
	var testCases = map[string]struct {
		args []string
		want []string
	}{
		"GPIO out of range": {
			[]string{"/dev/null", "9"},
			[]string{"GPIO number must be in range of 1 - 8.", "Usage:"},
		},
		"GPIO not a number": {
			[]string{"/dev/null", "x"},
			[]string{"GPIO number must be in range of 1 - 8."},
		},
		"no such device": {
			[]string{filepath.Join(t.TempDir(), "missing")},
			[]string{"Proceed at your own risk.", "WRITE ERROR for USB Audio Adapter GPIO", "Usage:"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var result = testutils.RunMain(t, "", tc.args...)

			assert.Equal(t, 1, result.Status)

			for _, want := range tc.want {
				assert.Contains(t, result.Output(), want)
			}
		})
	}
}
