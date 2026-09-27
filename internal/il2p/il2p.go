// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package il2p is the codec for IL2P (Improved Layer 2 Protocol), which
// carries AX.25 frames with a compact header, scrambling and Reed-Solomon
// forward error correction.  It converts between AX.25 packets and the bytes
// that go over the air; the bit-level sending and receiving, which is tied
// into the HDLC transmit and receive paths, stays in internal/direwolf.
//
// Reference: https://tarpn.net/t/il2p/il2p-specification_draft_v0-6.pdf
package il2p

const Preamble = 0x55

const SyncWord = 0xF15E48

const SyncWordSize = 3
const HeaderSize = 13 // Does not include 2 parity.
const HeaderParity = 2

const maxPayloadSize = 1023
const maxPayloadBlocks = 5
const maxParitySymbols = 16 // For payload only.
const MaxEncodedPayloadSize = (maxPayloadSize + maxPayloadBlocks*maxParitySymbols)

const CRCEncodedSize = 4 // 16-bit CRC → 4 Hamming-encoded bytes
