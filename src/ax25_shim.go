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
	AX25_MAX_REPEATERS             = ax25.MaxRepeaters
	AX25_MIN_ADDRS                 = ax25.MinAddrs
	AX25_MAX_ADDRS                 = ax25.MaxAddrs
	AX25_DESTINATION               = ax25.Destination
	AX25_SOURCE                    = ax25.Source
	AX25_REPEATER_1                = ax25.Repeater1
	AX25_REPEATER_2                = ax25.Repeater2
	AX25_MAX_INFO_LEN              = ax25.MaxInfoLen
	AX25_MIN_PACKET_LEN            = ax25.MinPacketLen
	AX25_MAX_PACKET_LEN            = ax25.MaxPacketLen
	AX25_PID_NO_LAYER_3            = ax25.PIDNoLayer3
	AX25_PID_SEGMENTATION_FRAGMENT = ax25.PIDSegmentationFragment
	cr_cmd                         = ax25.CRCmd
	cr_res                         = ax25.CRRes
	cr_11                          = ax25.CR11
	modulo_unknown                 = ax25.ModuloUnknown
	modulo_8                       = ax25.Modulo8
	modulo_128                     = ax25.Modulo128
	frame_type_I                   = ax25.FrameTypeI
	frame_type_S_RR                = ax25.FrameTypeSRR
	frame_type_S_RNR               = ax25.FrameTypeSRNR
	frame_type_S_REJ               = ax25.FrameTypeSREJ
	frame_type_S_SREJ              = ax25.FrameTypeSSREJ
	frame_type_U_SABME             = ax25.FrameTypeUSABME
	frame_type_U_SABM              = ax25.FrameTypeUSABM
	frame_type_U_DISC              = ax25.FrameTypeUDISC
	frame_type_U_DM                = ax25.FrameTypeUDM
	frame_type_U_UA                = ax25.FrameTypeUUA
	frame_type_U_FRMR              = ax25.FrameTypeUFRMR
	frame_type_U_UI                = ax25.FrameTypeUUI
	frame_type_U_XID               = ax25.FrameTypeUXID
	frame_type_U_TEST              = ax25.FrameTypeUTEST
	frame_type_U                   = ax25.FrameTypeU
	frame_not_AX25                 = ax25.FrameNotAX25
	AddrLenient                    = ax25.AddrLenient
	AddrStrict                     = ax25.AddrStrict
	AddrStrictNoStar               = ax25.AddrStrictNoStar
	AddrStrictLowerCaseWarning     = ax25.AddrStrictLowerCaseWarning
)

func ax25_is_null_frame(this_p *packet_t) bool {
	return this_p.IsNullFrame()
}

func ax25_set_pid(this_p *packet_t, pid byte) {
	this_p.SetPID(pid)
}

func ax25_get_pid(this_p *packet_t) int {
	return this_p.PID()
}

func ax25_get_frame_len(this_p *packet_t) int {
	return this_p.FrameLen()
}

func ax25_get_frame_data(this_p *packet_t) []byte {
	return this_p.FrameData()
}

func ax25_dedupe_crc(pp *packet_t) uint16 {
	return pp.DedupeCRC()
}

func ax25_m_m_crc(pp *packet_t) uint16 {
	return pp.MultiModemCRC()
}

func AX25SafePrint(info []byte, ascii_only bool) {
	ax25.SafePrint(info, ascii_only)
}

func NoteSafePrintTruncation(length int) {
	ax25.NoteSafePrintTruncation(length)
}

func ax25_alevel_to_text(alevel ALevel) string {
	return alevel.Text()
}

func ax25_get_control_offset(this_p *packet_t) int {
	return this_p.ControlOffset()
}

func ax25_get_info_offset(this_p *packet_t) int {
	return this_p.InfoOffset()
}

func ax25_u_frame(addrs [AX25_MAX_ADDRS]string, num_addr int, cr cmdres_t, ftype ax25_frame_type_t, pf int, pid int, info []byte) *packet_t {
	return ax25.UFrame(addrs, num_addr, cr, ftype, pf, pid, info)
}

func ax25_s_frame(addrs [AX25_MAX_ADDRS]string, num_addr int, cr cmdres_t, ftype ax25_frame_type_t, modulo ax25_modulo_t, nr int, pf int, info []byte) *packet_t {
	return ax25.SFrame(addrs, num_addr, cr, ftype, modulo, nr, pf, info)
}

func ax25_i_frame(addrs [AX25_MAX_ADDRS]string, num_addr int, cr cmdres_t, modulo ax25_modulo_t, nr int, ns int, pf int, pid int, info []byte) *packet_t {
	return ax25.IFrame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, info)
}
