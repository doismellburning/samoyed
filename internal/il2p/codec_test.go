// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIL2PDecodeFrameShortInputReturnsNil(t *testing.T) {
	Init(0)
	// Input shorter than IL2P_HEADER_SIZE+IL2P_HEADER_PARITY must not panic.
	var pp = il2p_decode_frame([]byte{0x01, 0x02, 0x03}, Version0_4)
	assert.Nil(t, pp)
}

func TestIL2PDecodeFrameHeaderFECFailureReturnsNil(t *testing.T) {
	Init(0)
	// Two symbol errors exceed the correction capacity of the 2-parity header (e < 0).
	// il2p_decode_frame must return nil rather than proceeding with a corrupt header.
	var twoErrors = make([]byte, headerSize+headerParity)
	twoErrors[0] = 0x01
	twoErrors[1] = 0x01
	var pp = il2p_decode_frame(twoErrors, Version0_4)
	assert.Nil(t, pp)
}

func TestIL2PDecodeFrameTruncatedPayloadReturnsNil(t *testing.T) {
	Init(0)

	// Build a real frame and encode it, then truncate the payload portion.
	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"
	var pinfo = []byte("hello world")
	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)

	var encoded, elen = EncodeFrame(pp, Version0_4, 0)
	require.Positive(t, elen)

	// Keep only the header bytes plus 1 byte of payload — far less than encoded_payload_size.
	var truncated = encoded[:headerSize+headerParity+1]
	var pp2 = il2p_decode_frame(truncated, Version0_4)
	assert.Nil(t, pp2)
}

func TestIL2PDecodeFrameJunkTrailingBytesReturnsNil(t *testing.T) {
	Init(0)

	// A frame with 1–3 trailing bytes beyond encoded_payload_size is malformed:
	// not enough to be a CRC, not exactly the right payload length. Must return nil.
	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"
	var pinfo = []byte("hello world")
	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)

	var encoded, elen = EncodeFrame(pp, Version0_4, 0)
	require.Positive(t, elen)

	for junk := 1; junk < CRCEncodedSize; junk++ {
		var padded = append(encoded, make([]byte, junk)...)
		var pp2 = il2p_decode_frame(padded, Version0_4)
		assert.Nil(t, pp2, "expected nil for %d trailing junk byte(s)", junk)
	}
}
