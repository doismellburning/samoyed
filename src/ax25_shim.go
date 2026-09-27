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
	frame_type_I               = ax25.FrameTypeI
	frame_type_S_RR            = ax25.FrameTypeSRR
	frame_type_S_RNR           = ax25.FrameTypeSRNR
	frame_type_S_REJ           = ax25.FrameTypeSREJ
	frame_type_S_SREJ          = ax25.FrameTypeSSREJ
	frame_type_U_SABME         = ax25.FrameTypeUSABME
	frame_type_U_SABM          = ax25.FrameTypeUSABM
	frame_type_U_DISC          = ax25.FrameTypeUDISC
	frame_type_U_DM            = ax25.FrameTypeUDM
	frame_type_U_UA            = ax25.FrameTypeUUA
	frame_type_U_FRMR          = ax25.FrameTypeUFRMR
	frame_type_U_UI            = ax25.FrameTypeUUI
	frame_type_U_XID           = ax25.FrameTypeUXID
	frame_type_U_TEST          = ax25.FrameTypeUTEST
	frame_type_U               = ax25.FrameTypeU
	frame_not_AX25             = ax25.FrameNotAX25
	AddrLenient                = ax25.AddrLenient
	AddrStrict                 = ax25.AddrStrict
	AddrStrictNoStar           = ax25.AddrStrictNoStar
	AddrStrictLowerCaseWarning = ax25.AddrStrictLowerCaseWarning
)
