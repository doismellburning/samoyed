// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// The packet assembler/disassembler now lives in internal/ax25. These are its
// old names, kept so the rest of package direwolf needn't change all at once;
// new code should use internal/ax25 directly.
//
// Only the names package direwolf itself still uses are here. This is a
// stepping stone for our own code, not a compatibility layer for anything
// outside it: the commands under cmd, which used some of the other exported
// names (MustAX25FromText, AX25Pack, MAXSAFE, ...), now use internal/ax25.

import (
	"github.com/doismellburning/samoyed/internal/ax25"
)

type packet_t = ax25.Packet
type cmdres_t = ax25.CmdRes
type ax25_modulo_t = ax25.Modulo
type ax25_frame_type_t = ax25.FrameType
type ALevel = ax25.ALevel
type AddrStrictness = ax25.AddrStrictness

const (
	AddrLenient                = ax25.AddrLenient
	AddrStrict                 = ax25.AddrStrict
	AddrStrictNoStar           = ax25.AddrStrictNoStar
	AddrStrictLowerCaseWarning = ax25.AddrStrictLowerCaseWarning
)
