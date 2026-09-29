// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
)

// FuzzUnwrap covers the KISS framing, which any client on the KISS TCP
// port or the serial KISS device can feed.
func FuzzUnwrap(f *testing.F) {
	var saved = logrus.StandardLogger().Out

	logrus.SetOutput(io.Discard)
	f.Cleanup(func() { logrus.SetOutput(saved) })

	f.Add([]byte{0x00, 'h', 'e', 'l', 'l', 'o', FEND})
	f.Add([]byte{0x00, FESC, TFEND, FESC, TFESC, FEND})
	f.Add([]byte{FEND})

	f.Fuzz(func(t *testing.T, in []byte) {
		Unwrap(in)
	})
}
