// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package il2p

const Preamble = 0x55

const SyncWord = 0xF15E48

const SyncWordSize = 3
const IL2P_HEADER_SIZE = 13 // Does not include 2 parity.
const IL2P_HEADER_PARITY = 2

const IL2P_MAX_PAYLOAD_SIZE = 1023
const IL2P_MAX_PAYLOAD_BLOCKS = 5
const IL2P_MAX_PARITY_SYMBOLS = 16 // For payload only.
const IL2P_MAX_ENCODED_PAYLOAD_SIZE = (IL2P_MAX_PAYLOAD_SIZE + IL2P_MAX_PAYLOAD_BLOCKS*IL2P_MAX_PARITY_SYMBOLS)

const IL2P_CRC_ENCODED_SIZE = 4 // 16-bit CRC → 4 Hamming-encoded bytes

// IL2P_RS_BLOCK_SIZE is the size of a Reed-Solomon codeblock with 8 bit
// symbols, which is what IL2P uses, as FX.25 does.
const IL2P_RS_BLOCK_SIZE = 255

const IL2P_MAX_PACKET_SIZE = (SyncWordSize + IL2P_HEADER_SIZE + IL2P_HEADER_PARITY + IL2P_MAX_ENCODED_PAYLOAD_SIZE + IL2P_CRC_ENCODED_SIZE)
