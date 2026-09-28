// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package fx25

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Busy is what holds off duplicate removal while a codeblock is coming in, so
// it has to be true from the correlation tag until the frame is delivered.
func TestReceiverBusy(t *testing.T) {
	var ctag_num, data, check = EncodeFrame(0, slices.Clone(fxTestFrame), 16, 0)
	require.Positive(t, ctag_num)

	var block = fxTestBlock(ctag_num, data, check)

	var busyAtDelivery bool

	var rx *Receiver
	rx = NewReceiver(0, 0, 0, 0, func(int, int, int, []byte, int) {
		busyAtDelivery = rx.Busy()
	})

	assert.False(t, rx.Busy())

	for _, b := range block {
		for imask := byte(0x01); imask != 0; imask <<= 1 {
			rx.RecBit(int(b & imask))
		}
	}

	assert.True(t, busyAtDelivery, "Busy should still be true while the frame is delivered")
	assert.False(t, rx.Busy(), "Busy should be false once the codeblock is done")
}

// FuzzReceiver feeds the receiver arbitrary bits, least significant first, as
// anyone on frequency can.  The failure looked for is a panic.
func FuzzReceiver(f *testing.F) {
	for _, fx_mode := range []int{16, 32, 64} {
		var ctag_num, data, check = EncodeFrame(0, slices.Clone(fxTestFrame), fx_mode, 0)
		require.Positive(f, ctag_num)
		f.Add(fxTestBlock(ctag_num, data, check))
	}

	f.Fuzz(func(t *testing.T, in []byte) {
		var rx = NewReceiver(0, 0, 0, 0, func(int, int, int, []byte, int) {})

		for _, b := range in {
			for imask := byte(0x01); imask != 0; imask <<= 1 {
				rx.RecBit(int(b & imask))
			}
		}
	})
}
