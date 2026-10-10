// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ax25

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/require"
)

// The AX.25 decoders are the part of Samoyed that everything on frequency
// reaches, and they take nothing more than a []byte or a string, so they are
// cheap to fuzz.  A target's job is to make the call; the failure it is
// looking for is a panic, so there is nothing to assert.
//
// Seeds added here run as ordinary unit tests under "go test", so an input
// that once crashed a decoder stays checked even when nobody is fuzzing.
// Fuzzing proper is "go test ./internal/ax25/ -run XXX -fuzz FuzzSomething".

// FuzzAX25FromFrame covers the path every received frame takes, from the
// modem, a KISS client, a network TNC, an AGW client or IL2P: build a packet
// from the bytes off the air, then ask it the questions the receive path asks.
func FuzzAX25FromFrame(f *testing.F) {
	testutils.FuzzQuietly(f)

	// An ordinary APRS position report.
	var pp = FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)
	f.Add(pp.FrameData())

	// Addresses and a control byte, with no PID and no information part:
	// the shortest frame FromFrame accepts (issue #670).
	f.Add([]byte("000000000000010"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var pp = FromFrame(data, ALevel{Rec: 50, Mark: 50, Space: 50})
		if pp == nil {
			return
		}

		pp.FormatAddrs()
		pp.Info()
		pp.FormatViaPath()
		pp.FrameType()
		pp.IsAPRS()
		pp.DedupeCRC()
		pp.DTI()
		pp.CheckAddresses(AddrLenient)
	})
}

// FuzzAX25FromText covers the other way in: a monitor-format string, as the
// APRS-IS connection and the command line tools hand us.
func FuzzAX25FromText(f *testing.F) {
	testutils.FuzzQuietly(f)

	f.Add("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#")
	f.Add("Q1TEST>APDW17::Q2TEST   :Hello")
	f.Add(">:")

	f.Fuzz(func(t *testing.T, monitor string) {
		var pp = FromTextWithStrictness(monitor, AddrLenient)
		if pp == nil {
			return
		}

		pp.FormatAddrs()
		pp.Info()
		pp.FrameType()
	})
}
