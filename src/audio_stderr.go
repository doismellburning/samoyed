//go:build unix

// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package direwolf

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

/*
 * The native audio libraries under PortAudio write their diagnostics straight
 * to file descriptor 2, where Go can't see them.  ALSA is the worst offender:
 * a system whose ALSA configuration mentions a sound card that isn't present
 * emits pages of "cannot find card" and "Unknown PCM" every time PortAudio
 * enumerates devices, which is none of the user's business unless something
 * actually failed.  The only way to get hold of that output is to point file
 * descriptor 2 somewhere else while the native code runs.
 */

// maxCapturedStderr bounds how much of the captured output is kept; anything
// beyond that is drained and discarded so the native code never blocks writing.
const maxCapturedStderr = 64 * 1024

// captureDrainTimeout bounds how long we wait for the end of the pipe once the
// native call has finished and file descriptor 2 is back.  A variable so that
// a test needn't wait it out.
var captureDrainTimeout = 5 * time.Second

// captureStderrMu serialises captures, so that two of them can't restore each
// other's saved descriptor.
var captureStderrMu sync.Mutex

// dupOntoStderrFn is dupOntoStderr, indirected so a test can make restoring
// file descriptor 2 fail, which is otherwise not something we can provoke.
var dupOntoStderrFn = dupOntoStderr

// captureStderrFD runs fn with file descriptor 2 redirected to a pipe and
// returns whatever was written to it while it ran, up to maxCapturedStderr
// bytes.  If the redirection can't be set up, fn still runs, with its output
// going to the real stderr as usual, and "" is returned.
//
// Note that this affects the whole process for as long as fn runs, so keep fn
// to the native call that needs quietening.  Samoyed's own output goes to
// stdout (see dw_printf) and so is unaffected.
func captureStderrFD(fn func()) string {
	captureStderrMu.Lock()
	defer captureStderrMu.Unlock()

	var savedFD, dupErr = unix.Dup(unix.Stderr)
	if dupErr != nil {
		fn()

		return ""
	}

	var reader, writer, pipeErr = os.Pipe()
	if pipeErr != nil {
		_ = unix.Close(savedFD)

		fn()

		return ""
	}

	defer reader.Close()

	var redirectErr = dupOntoStderrFn(int(writer.Fd()))
	if redirectErr != nil {
		_ = unix.Close(savedFD)
		_ = writer.Close()

		fn()

		return ""
	}

	var captured = make(chan string, 1)

	go func() {
		var builder strings.Builder

		_, _ = io.Copy(&builder, io.LimitReader(reader, maxCapturedStderr))

		// Keep draining, so a flood of messages can't block the writer, which
		// is the native code we're waiting on.
		_, _ = io.Copy(io.Discard, reader)

		captured <- builder.String()
	}()

	// A panic in fn must still leave the process with a working stderr to
	// report itself on, hence restoring in a defer rather than after fn.
	var restored bool

	var restore = func() {
		if restored {
			return
		}

		restored = true

		var err = dupOntoStderrFn(savedFD)
		for errors.Is(err, unix.EINTR) {
			err = dupOntoStderrFn(savedFD)
		}

		if err != nil {
			// File descriptor 2 is still the pipe's write end, and closing our
			// own copy of it below won't release that one - everything written
			// to stderr from here on would go into a pipe nobody reads.
			// Closing file descriptor 2 does release it, and leaves the way
			// clear for one more attempt at putting the real stderr back.
			_ = unix.Close(unix.Stderr)

			if dupOntoStderrFn(savedFD) != nil {
				// Leaving file descriptor 2 closed is a hazard of its own: the
				// next file opened would be given it, and everything aimed at
				// stderr would land in that file instead.  /dev/null at least
				// keeps it pointing somewhere harmless - and with 2 free, the
				// open below usually returns it directly.
				var null, nullErr = unix.Open(os.DevNull, unix.O_WRONLY, 0)
				if nullErr == nil && null != unix.Stderr {
					_ = dupOntoStderrFn(null)
					_ = unix.Close(null)
				}
			}
		}

		_ = unix.Close(savedFD)
		_ = writer.Close()
	}

	defer restore()

	fn()

	restore()

	select {
	case noise := <-captured:
		return noise
	case <-time.After(captureDrainTimeout):
		// The end of the pipe only comes once every copy of its write end is
		// closed, and ours is not necessarily the only one: anything forked
		// while file descriptor 2 was the pipe - PortAudio's JACK host API
		// starting a jackd of its own, say - inherits it and can outlive the
		// call.  Closing the read end on the way out lets the reading
		// goroutine finish; what it had collected is lost, which is a good
		// deal better than never returning.
		return ""
	}
}
