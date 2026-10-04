// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package ax25

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

/*------------------------------------------------------------------------------
 *
 * Purpose:	Quick unit test for ax25_pad2.c
 *
 * Description:	Generate a variety of frames.
 *		Each function calls FrameType to verify results.
 *
 *------------------------------------------------------------------------------*/

func Test_AX25_PAD2(t *testing.T) {
	var pid = 0xf0
	var info []byte

	var addrs [MaxAddrs]string
	addrs[0] = "W2UB"
	addrs[1] = "WB2OSZ-15"
	var num_addr = 2

	/* U frame */

	for ftype := FrameTypeUSABME; ftype <= FrameTypeUTEST; ftype++ {
		for pf := range 2 {
			var cmin CmdRes = 0
			var cmax CmdRes = 0

			switch ftype {
			// 0 = response, 1 = command
			case FrameTypeUSABME:
				cmin = 1
				cmax = 1
			case FrameTypeUSABM:
				cmin = 1
				cmax = 1
			case FrameTypeUDISC:
				cmin = 1
				cmax = 1
			case FrameTypeUDM:
				cmin = 0
				cmax = 0
			case FrameTypeUUA:
				cmin = 0
				cmax = 0
			case FrameTypeUFRMR:
				cmin = 0
				cmax = 0
			case FrameTypeUUI:
				cmin = 0
				cmax = 1
			case FrameTypeUXID:
				cmin = 0
				cmax = 1
			case FrameTypeUTEST:
				cmin = 0
				cmax = 1
			default:
			}

			for cr := cmin; cr <= cmax; cr++ {
				t.Logf("Construct U frame, cr=%d, ftype=%d, pid=0x%02x", cr, ftype, pid)

				var pp = UFrame(addrs, num_addr, cr, ftype, pf, pid, nil)
				check_ax25_u_frame(t, pp, cr, ftype, pf)
				pp.HexDump()
			}
		}
	}

	/* S frame */

	addrs[2] = "DIGI1-1"
	num_addr = 3

	for ftype := FrameTypeSRR; ftype <= FrameTypeSSREJ; ftype++ {
		for pf := range 2 {
			var modulo = Modulo8
			var nr = int(modulo/2 + 1)

			for cr := CmdRes(0); cr <= 1; cr++ {
				t.Logf("Construct S frame, cmd=%d, ftype=%d, pid=0x%02x", cr, ftype, pid)

				var pp = SFrame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil)
				check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

				pp.HexDump()
			}

			modulo = Modulo128
			nr = int(modulo/2 + 1)

			for cr := CmdRes(0); cr <= 1; cr++ {
				t.Logf("Construct S frame, cmd=%d, ftype=%d, pid=0x%02x", cr, ftype, pid)

				var pp = SFrame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil)
				check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

				pp.HexDump()
			}
		}
	}

	/* SREJ is only S frame which can have information part. */

	var srej_info = []byte{1 << 1, 2 << 1, 3 << 1, 4 << 1}

	var ftype = FrameTypeSSREJ

	for pf := range 2 {
		var modulo = Modulo128
		var nr = 127
		var cr = CRRes

		t.Logf("Construct Multi-SREJ S frame, cmd=%d, ftype=%d, pid=0x%02x", cr, ftype, pid)

		var pp = SFrame(addrs, num_addr, cr, ftype, modulo, nr, pf, srej_info)
		check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

		pp.HexDump()
	}

	/* I frame */

	info = []byte("The rain in Spain stays mainly on the plain.")

	for pf := range 2 {
		var modulo = Modulo8
		var nr = 0x55 & int(modulo-1)
		var ns = 0xaa & int(modulo-1)

		for cr := CmdRes(0); cr <= 1; cr++ {
			t.Logf("Construct I frame, cmd=%d, ftype=%d, pid=0x%02x", cr, ftype, pid)

			var pp = IFrame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, info)
			check_ax25_i_frame(t, pp, cr, pf, nr, ns, info)

			pp.HexDump()
		}

		modulo = Modulo128
		nr = 0x55 & int(modulo-1)
		ns = 0xaa & int(modulo-1)

		for cr := CmdRes(0); cr <= 1; cr++ {
			t.Logf("Construct I frame, cmd=%d, ftype=%d, pid=0x%02x", cr, ftype, pid)

			var pp = IFrame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, info)
			check_ax25_i_frame(t, pp, cr, pf, nr, ns, info)

			pp.HexDump()
		}
	}
} /* end main */

func check_ax25_u_frame(t *testing.T, packet *Packet, cr CmdRes, ftype FrameType, pf int) {
	t.Helper()

	var check_cr, check_desc, check_pf, check_nr, check_ns, check_ftype = packet.FrameType()

	t.Logf("check: ftype=%d, desc=\"%s\", pf=%d", check_ftype, check_desc, check_pf)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, ftype, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, -1, check_nr)
	assert.Equal(t, -1, check_ns)
}

func check_ax25_s_frame(t *testing.T, packet *Packet, cr CmdRes, ftype FrameType, pf int, nr int) {
	t.Helper()

	// todo modulo must be input.
	var check_cr, check_desc, check_pf, check_nr, check_ns, check_ftype = packet.FrameType()

	t.Logf("check: ftype=%d, desc=\"%s\", pf=%d, nr=%d", check_ftype, check_desc, check_pf, check_nr)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, ftype, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, nr, check_nr)
	assert.Equal(t, -1, check_ns)
}

func check_ax25_i_frame(t *testing.T, packet *Packet, cr CmdRes, pf int, nr int, ns int, info []byte) {
	t.Helper()

	var check_cr, check_desc, check_pf, check_nr, check_ns, check_ftype = packet.FrameType()

	t.Logf("check: ftype=%d, desc=\"%s\", pf=%d, nr=%d, ns=%d", check_ftype, check_desc, check_pf, check_nr, check_ns)

	var check_info = packet.Info()

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, FrameTypeI, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, nr, check_nr)
	assert.Equal(t, ns, check_ns)

	assert.Equal(t, info, check_info)
}
