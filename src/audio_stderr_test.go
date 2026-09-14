//go:build unix

// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// alsaNoise is the sort of thing libasound writes straight to file descriptor
// 2, from underneath PortAudio, on a machine whose ALSA configuration mentions
// a card that isn't there.
const alsaNoise = "ALSA lib confmisc.c:855:(parse_card) cannot find card '0'\n" +
	"ALSA lib pcm.c:2722:(snd_pcm_open_noupdate) Unknown PCM sysdefault\n"

// stderrIdentity identifies whatever file descriptor 2 currently refers to, so
// a test can check it was put back.  A string because the types of the fields
// vary between platforms.
func stderrIdentity(t *testing.T) string {
	t.Helper()

	var stat unix.Stat_t

	require.NoError(t, unix.Fstat(unix.Stderr, &stat))

	return fmt.Sprintf("%v:%v", stat.Dev, stat.Ino)
}

// captureStdout collects what fn prints via dw_printf, which goes to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	var oldStdout = os.Stdout

	defer func() {
		os.Stdout = oldStdout
	}()

	var reader, writer, err = os.Pipe()

	require.NoError(t, err)

	os.Stdout = writer

	fn()

	os.Stdout = oldStdout

	require.NoError(t, writer.Close())

	var out, readErr = io.ReadAll(reader)

	require.NoError(t, readErr)
	require.NoError(t, reader.Close())

	return string(out)
}

func TestCaptureStderrFDCapturesNativeWrites(t *testing.T) {
	var before = stderrIdentity(t)

	var captured = captureStderrFD(func() {
		var _, err = unix.Write(unix.Stderr, []byte(alsaNoise))
		require.NoError(t, err)
	})

	assert.Equal(t, alsaNoise, captured)
	assert.Equal(t, before, stderrIdentity(t), "stderr should have been restored")
}

func TestCaptureStderrFDDrainsMoreThanItKeeps(t *testing.T) {
	// More than a pipe will buffer, so this deadlocks if the reader stops
	// reading once it has all it intends to keep.
	var flood = strings.Repeat(alsaNoise, (maxCapturedStderr/len(alsaNoise))+1000)

	var captured = captureStderrFD(func() {
		var _, err = unix.Write(unix.Stderr, []byte(flood))
		require.NoError(t, err)
	})

	assert.LessOrEqual(t, len(captured), maxCapturedStderr)
	assert.True(t, strings.HasPrefix(flood, captured))
}

// TestCaptureStderrFDGivesUpWhenSomethingElseHoldsThePipe covers the case
// where our copy of the pipe's write end isn't the only one, as happens when
// something is forked while file descriptor 2 is the pipe: the end of the pipe
// never arrives, and waiting for it unconditionally would hang the process.
func TestCaptureStderrFDGivesUpWhenSomethingElseHoldsThePipe(t *testing.T) {
	var realTimeout = captureDrainTimeout

	captureDrainTimeout = 100 * time.Millisecond

	defer func() {
		captureDrainTimeout = realTimeout
	}()

	var before = stderrIdentity(t)

	var leaked int

	// Checked once the capture is done: a failed assertion in the goroutine
	// below would stop only the goroutine.
	var leakErr, writeErr error

	var done = make(chan string, 1)

	go func() {
		done <- captureStderrFD(func() {
			// Stand in for the forked process: a copy of the write end that
			// outlives the call.
			leaked, leakErr = unix.Dup(unix.Stderr)
			if leakErr != nil {
				return
			}

			_, writeErr = unix.Write(unix.Stderr, []byte(alsaNoise))
		})
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("captureStderrFD did not give up on a pipe that stayed open")
	}

	require.NoError(t, leakErr)
	require.NoError(t, writeErr)
	assert.Equal(t, before, stderrIdentity(t), "stderr should have been restored")

	require.NoError(t, unix.Close(leaked))
}

// TestCaptureStderrFDSurvivesAFailedRestore checks the case where putting
// stderr back fails: file descriptor 2 is then still the pipe's write end, and
// unless that is released the reader never reaches the end of the pipe and the
// capture waits for it forever.
func TestCaptureStderrFDSurvivesAFailedRestore(t *testing.T) {
	var before = stderrIdentity(t)

	// A failed restore leaves file descriptor 2 closed, so keep a copy of the
	// real stderr to put back afterwards.
	var savedStderr, dupErr = unix.Dup(unix.Stderr)

	require.NoError(t, dupErr)

	var realDupOntoStderr = dupOntoStderrFn

	defer func() {
		dupOntoStderrFn = realDupOntoStderr
	}()

	// Fail only the restore, so the capture itself is set up as usual.
	var redirected bool

	dupOntoStderrFn = func(fd int) error {
		if redirected {
			return unix.EBADF
		}

		redirected = true

		return realDupOntoStderr(fd)
	}

	var done = make(chan string, 1)

	go func() {
		done <- captureStderrFD(func() {
			var _, err = unix.Write(unix.Stderr, []byte(alsaNoise))
			assert.NoError(t, err)
		})
	}()

	select {
	case captured := <-done:
		assert.Equal(t, alsaNoise, captured)
	case <-time.After(10 * time.Second):
		t.Fatal("captureStderrFD did not return after a failed restore")
	}

	// Whatever else happened, file descriptor 2 must still be open: were it
	// left closed, the next file opened would be given it and everything aimed
	// at stderr would land in that file.
	var stat unix.Stat_t

	assert.NoError(t, unix.Fstat(unix.Stderr, &stat), "stderr should not be left closed")

	// It is /dev/null rather than the real stderr at this point, so put the
	// real one back for the rest of the tests.
	dupOntoStderrFn = realDupOntoStderr

	require.NoError(t, realDupOntoStderr(savedStderr))
	require.NoError(t, unix.Close(savedStderr))
	assert.Equal(t, before, stderrIdentity(t))
}

func TestCaptureStderrFDRestoresStderrAfterPanic(t *testing.T) {
	var before = stderrIdentity(t)

	assert.Panics(t, func() {
		captureStderrFD(func() {
			panic("native code went wrong")
		})
	})

	assert.Equal(t, before, stderrIdentity(t), "stderr should have been restored")
}

func TestQuietPortAudioHoldsBackNoiseUntilSomethingIsReported(t *testing.T) {
	var noisy = func() error {
		var _, err = unix.Write(unix.Stderr, []byte(alsaNoise))

		return err
	}

	// The noise itself goes nowhere, whether the call worked...
	var out = captureStdout(t, func() {
		require.NoError(t, quietPortAudio(noisy))
	})

	assert.NotContains(t, out, "cannot find card")

	// ...or not: printing is the caller's, after its own error message, so the
	// explanation follows the failure rather than preceding it.
	out = captureStdout(t, func() {
		require.Error(t, quietPortAudio(func() error {
			_ = noisy()

			return assert.AnError
		}))
	})

	assert.NotContains(t, out, "cannot find card")

	// What was kept is there for that caller...
	out = captureStdout(t, printAudioBackendNoise)

	assert.Contains(t, out, "cannot find card")

	// ...but only once, so a later unrelated failure doesn't repeat it.
	out = captureStdout(t, printAudioBackendNoise)

	assert.NotContains(t, out, "cannot find card")
}

func TestQuietPortAudioKeepsTheMostRecentNoise(t *testing.T) {
	// Each call captures and remembers under one lock, so the noise left for
	// printing is the latest, rather than whichever call happened to finish
	// publishing last.
	for _, message := range []string{"first\n", "second\n"} {
		require.NoError(t, quietPortAudio(func() error {
			var _, err = unix.Write(unix.Stderr, []byte(message))

			return err
		}))
	}

	var out = captureStdout(t, printAudioBackendNoise)

	assert.Contains(t, out, "second")
	assert.NotContains(t, out, "first")
}
