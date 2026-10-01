// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dwutil holds small helpers that code ported from Dire Wolf's C leans
// on - in place of C idioms Go lacks, such as assert and the ?: operator, the
// unit and angle conversions Dire Wolf defined as macros, and for letting a
// long-lived goroutine's waits be cut short by cancellation - so that packages
// split out of internal/direwolf can share them.
package dwutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Assert panics, naming the caller's file and line, when t is false - C's
// assert. It can't be named "assert" because of conflicts with
// stretchr/testify/assert, but otherwise, it's compatible enough.
func Assert(t bool) {
	if !t {
		_, file, line, _ := runtime.Caller(1)
		panic(fmt.Sprintf("Assertion failed at %s:%d", file, line))
	}
}

// IfThenElse exists because sometimes it's really convenient to have C's ternary ?:.
func IfThenElse[T any](x bool, a T, b T) T {
	if x {
		return a
	} else {
		return b
	}
}

// ByteArrayToString handles the several places where we deal with fixed-width byte arrays containing a string.
// For C this was fine, because strings are null-terminated; for Go we want to explicitly drop trailing nulls.
// This takes a slice because I didn't know how to make it take an arbitrary sized array, and didn't see the value.
func ByteArrayToString(b []byte) string {
	return string(bytes.TrimRight(b, "\x00"))
}

// HexDump prints p to stdout, 16 bytes to a line: the offset, the bytes in
// hexadecimal, and those that are printable ASCII as themselves.
func HexDump(p []byte) {
	for _, line := range HexDumpLines(p) {
		fmt.Println(line)
	}
}

// HexDumpLines formats p as HexDump prints it, one string per line and without
// the newlines, for a caller that sends each line somewhere else - a log entry,
// say.
func HexDumpLines(p []byte) []string {
	var lines []string

	for offset := 0; offset < len(p); offset += 16 {
		var chunk = p[offset:min(offset+16, len(p))]

		var line strings.Builder

		fmt.Fprintf(&line, "  %03x: ", offset)

		for _, b := range chunk {
			fmt.Fprintf(&line, " %02x", b)
		}

		line.WriteString(strings.Repeat("   ", 16-len(chunk)))
		line.WriteString("  ")

		for _, b := range chunk {
			if b >= 0x20 && b <= 0x7E {
				line.WriteByte(b)
			} else {
				line.WriteByte('.')
			}
		}

		lines = append(lines, line.String())
	}

	return lines
}

// LogHexDump logs p through entry at level, one entry per line of
// HexDumpLines in a "dump" field, so a dump sits beside the entry describing
// it rather than going to stdout on its own.  It formats nothing unless level
// is enabled.
func LogHexDump(entry *logrus.Entry, level logrus.Level, p []byte) {
	if !entry.Logger.IsLevelEnabled(level) {
		return
	}

	for _, line := range HexDumpLines(p) {
		entry.WithField("dump", line).Log(level)
	}
}

// SleepCtx sleeps for d, or until ctx is cancelled, whichever comes first.
//
// It reports whether the whole of d elapsed, so a false return says the caller
// is being shut down and should return rather than carry on with whatever it
// woke up to do.  That makes it the replacement for a time.Sleep in
// a long-lived goroutine's loop: such a sleep is where the goroutine spends
// most of its life, and nothing else in the loop can notice a cancellation
// until it finishes.
func SleepCtx(ctx context.Context, d time.Duration) bool {
	var timer = time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// SleepSecCtx is SleepCtx taking whole seconds, as the loops ported from C
// counted in whole seconds.
func SleepSecCtx(ctx context.Context, s int) bool {
	return SleepCtx(ctx, time.Duration(s)*time.Second)
}

// CloseOnDone closes c once ctx is cancelled, and returns a function that
// cancels that arrangement.
//
// A goroutine blocked in Accept or Read cannot see a cancellation at all: it
// is inside a system call, not at the top of its loop.  Closing what it is
// blocked on is what gets it back - the call returns an error, and the
// goroutine then finds ctx.Err() non-nil and returns.  Call the returned
// function, usually deferred, when finished with c, so a long-lived context
// doesn't hold on to something already closed and forgotten.
//
// The two can happen at once: the goroutine can be on its way out for its own
// reasons at the moment of cancellation, and get in before the close.  It then
// closes c itself rather than leaving a socket nobody is watching any more
// open for the rest of the process's life.
//
// That still leaves the reverse order - the call returns, and a cancellation
// arrives after the check above but before the caller looks at ctx.Err() - so
// this is how to interrupt a blocking call, not a promise about who closes c
// in the end.  A caller that owns c closes it on its own cancellation path
// too.
func CloseOnDone(ctx context.Context, c io.Closer) func() {
	var stop = context.AfterFunc(ctx, func() {
		_ = c.Close()
	})

	return func() {
		if stop() && ctx.Err() != nil {
			_ = c.Close()
		}
	}
}
