// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout runs f and returns what it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	var tmp, err = os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)

	var oldStdout = os.Stdout

	defer func() { os.Stdout = oldStdout }()

	os.Stdout = tmp

	f()

	os.Stdout = oldStdout

	require.NoError(t, tmp.Close())

	var output, readErr = os.ReadFile(tmp.Name())
	require.NoError(t, readErr)

	return string(output)
}

// buildCM108 builds the command under test.  Toggling a pin carries on until
// interrupted, and a failure exits the process, so those run it on its own.
func buildCM108(t *testing.T) string {
	t.Helper()

	var binary = filepath.Join(t.TempDir(), "samoyed-cm108")

	var build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec

	var out, err = build.CombinedOutput()
	require.NoError(t, err, "Building the command failed: %s", out)

	return binary
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

	var output = captureStdout(t, func() {
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

func Test_main_togglesThePin(t *testing.T) {
	var binary = buildCM108(t)

	// An ordinary file takes the writes a HID would, so the loop can run.
	var hid = filepath.Join(t.TempDir(), "hidraw")
	require.NoError(t, os.WriteFile(hid, nil, 0o600))

	var cmd = exec.CommandContext(t.Context(), binary, hid, "1") //nolint:gosec

	var stdout, stdoutErr = cmd.StdoutPipe()
	require.NoError(t, stdoutErr)

	require.NoError(t, cmd.Start())

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// It prints each state as it sets it, a second apart, on a line of its
	// own after any warnings about the device.
	var mu sync.Mutex

	var output []byte

	go func() {
		var buf = make([]byte, 64)

		for {
			var n, err = stdout.Read(buf)

			mu.Lock()
			output = append(output, buf[:n]...)
			mu.Unlock()

			if err != nil {
				return
			}
		}
	}()

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		var lastLine = output[bytes.LastIndexByte(output, '\n')+1:]

		return bytes.HasPrefix(lastLine, []byte("01"))
	}, 5*time.Second, 50*time.Millisecond, "the pin should go low then high")

	// A HID report was written for each.
	var written, readErr = os.ReadFile(hid) //nolint:gosec
	require.NoError(t, readErr)
	assert.NotEmpty(t, written)
}

func Test_main_fails(t *testing.T) {
	var binary = buildCM108(t)

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
			var out, err = exec.CommandContext(t.Context(), binary, tc.args...).Output() //nolint:gosec

			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 1, exitErr.ExitCode())

			for _, want := range tc.want {
				assert.Contains(t, string(out), want)
			}
		})
	}
}
