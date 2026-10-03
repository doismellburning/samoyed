// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package ax25

/*------------------------------------------------------------------
 *
 * Purpose:	Packet assembler and disasembler, part 2.
 *
 * Description:
 *
 *	The original ax25_pad.c was written with APRS in mind.
 *	It handles UI frames and transparency for a KISS TNC.
 *	Here we add new functions that can handle the
 *	more general cases of AX.25 frames.
 *
 *
 *	* Destination Address  (note: opposite order in printed format)
 *
 *	* Source Address
 *
 *	* 0-8 Digipeater Addresses
 *				(The AX.25 v2.2 spec reduced this number to
 *				a maximum of 2 but I allow the original 8.)
 *
 *	Each address is composed of:
 *
 *	* 6 upper case letters or digits, blank padded.
 *		These are shifted left one bit, leaving the LSB always 0.
 *
 *	* a 7th octet containing the SSID and flags.
 *		The LSB is always 0 except for the last octet of the address field.
 *
 *	The final octet of the Destination has the form:
 *
 *		C R R SSID 0, where,
 *
 *			C = command/response.   Set to 1 for command.
 *			R R = Reserved = 1 1	(See RR note, below)
 *			SSID = substation ID
 *			0 = zero
 *
 *	The final octet of the Source has the form:
 *
 *		C R R SSID 0, where,
 *
 *			C = command/response.   Must be inverse of destination C bit.
 *			R R = Reserved = 1 1	(See RR note, below)
 *			SSID = substation ID
 *			0 = zero (or 1 if no repeaters)
 *
 *	The final octet of each repeater has the form:
 *
 *		H R R SSID 0, where,
 *
 *			H = has-been-repeated = 0 initially.
 *				Set to 1 after this address has been used.
 *			R R = Reserved = 1 1
 *			SSID = substation ID
 *			0 = zero (or 1 if last repeater in list)
 *
 *		A digipeater would repeat this frame if it finds its address
 *		with the "H" bit set to 0 and all earlier repeater addresses
 *		have the "H" bit set to 1.
 *		The "H" bit would be set to 1 in the repeated frame.
 *
 *	In standard monitoring format, an asterisk is displayed after the last
 *	digipeater with the "H" bit set.  That indicates who you are hearing
 *	over the radio.
 *
 *
 *	Next we have:
 *
 *	* One or two byte Control Field - A U frame always has one control byte.
 *					When using modulo 128 sequence numbers, the
 *					I and S frames can have a second byte allowing
 *					7 bit fields instead of 3 bit fields.
 *					Unfortunately, we can't tell which we have by looking
 *					at a frame out of context.  :-(
 *					If we are one end of the link, we would know this
 *					from SABM/SABME and possible later negotiation
 *					with XID.  But if we start monitoring two other
 *					stations that are already conversing, we don't know.
 *
 *			RR note:	It seems that some implementations put a hint
 *					in the "RR" reserved bits.
 *					http://www.tapr.org/pipermail/ax25-layer2/2005-October/000297.html (now broken)
 *					https://elixir.bootlin.com/linux/latest/source/net/ax25/ax25_addr.c#L237
 *
 *					The RR bits can also be used for "DAMA" which is
 *					some sort of channel access coordination scheme.
 *					http://internet.freepage.de/cgi-bin/feets/freepage_ext/41030x030A/rewrite/hennig/afu/afudoc/afudama.html
 *					Neither is part of the official protocol spec.
 *
 *	* One byte Protocol ID 		- Only for I and UI frames.
 *					Normally we would use 0xf0 for no layer 3.
 *
 *	Finally the Information Field. The initial max size is 256 but it
 *	can be negotiated higher if both ends agree.
 *
 *	Only these types of frames can have an information part:
 *		- I
 *		- UI
 *		- XID
 *		- TEST
 *		- FRMR
 *
 *	The 2 byte CRC is not stored here.
 *
 *
 * Constructors:
 *		UFrame		- Construct a U frame.
 *		SFrame		- Construct a S frame.
 *		IFrame		- Construct a I frame.
 *
 * Get methods:	....			???
 *
 *------------------------------------------------------------------*/

import (
	"bytes"
	"fmt"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/sirupsen/logrus"
)

/*------------------------------------------------------------------------------
 *
 * Name:	UFrame
 *
 * Purpose:	Construct a U frame.
 *
 * Input:	addrs		- Array of addresses.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- CRCmd command frame, CRRes for a response frame.
 *
 *		ftype		- One of:
 *				        FrameTypeUSABME     // Set Async Balanced Mode, Extended
 *				        FrameTypeUSABM      // Set Async Balanced Mode
 *				        FrameTypeUDISC      // Disconnect
 *				        FrameTypeUDM        // Disconnect Mode
 *				        FrameTypeUUA        // Unnumbered Acknowledge
 *				        FrameTypeUFRMR      // Frame Reject
 *				        FrameTypeUUI        // Unnumbered Information
 *				        FrameTypeUXID       // Exchange Identification
 *				        FrameTypeUTEST      // Test
 *
 *		pf		- Poll/Final flag.
 *
 *		pid		- Protocol ID.  >>> Used ONLY for the UI type. <<<
 *				  Normally 0xf0 meaning no level 3.
 *				  Could be other values for NET/ROM, etc.
 *
 *		info		- Info field.  Allowed only for UI, XID, TEST, FRMR.
 *
 *
 * Returns:	Pointer to new packet object.
 *
 *------------------------------------------------------------------------------*/

func UFrame(addrs [MaxAddrs]string, num_addr int, cr CmdRes, ftype FrameType, pf int, pid int, info []byte) *Packet {
	var this_p = New()

	if this_p == nil {
		return (nil)
	}

	this_p.modulo = 0

	if this_p.setAddrs(addrs, num_addr, cr) == 0 {
		logrus.Error("Internal error: UFrame: could not set addresses")

		return (nil)
	}

	var ctrl int
	var t CmdRes  // 1 = must be cmd, 0 = must be response, 2 = can be either.
	var i = false // Is Info part allowed?

	switch ftype {
	// 1 = cmd only, 0 = res only, 2 = either
	case FrameTypeUSABME:
		ctrl = 0x6f
		t = 1
	case FrameTypeUSABM:
		ctrl = 0x2f
		t = 1
	case FrameTypeUDISC:
		ctrl = 0x43
		t = 1
	case FrameTypeUDM:
		ctrl = 0x0f
		t = 0
	case FrameTypeUUA:
		ctrl = 0x63
		t = 0
	case FrameTypeUFRMR:
		ctrl = 0x87
		t = 0
		i = true
	case FrameTypeUUI:
		ctrl = 0x03
		t = 2
		i = true
	case FrameTypeUXID:
		ctrl = 0xaf
		t = 2
		i = true
	case FrameTypeUTEST:
		ctrl = 0xe3
		t = 2
		i = true
	default:
		logrus.WithField("ftype", ftype).Error("Internal error: UFrame: invalid frame type for U frame")

		return (nil)
	}

	if pf != 0 {
		ctrl |= 0x10
	}

	if t != 2 {
		if cr != t {
			logrus.WithFields(logrus.Fields{
				"cr":          cr,
				"expected_cr": t,
				"ftype":       ftype,
			}).Error("Internal error: UFrame: wrong command/response for U frame")
		}
	}

	this_p.frame_data[this_p.frame_len] = byte(ctrl)
	this_p.frame_len++

	if ftype == FrameTypeUUI {
		// Definitely don't want pid value of 0 (not in valid list)
		// or 0xff (which means more bytes follow).
		if pid < 0 || pid == 0 || pid == 0xff {
			logrus.WithField("pid", fmt.Sprintf("0x%02x", pid)).Error("Internal error: UFrame: invalid PID for U frame")
			pid = PIDNoLayer3
		}

		this_p.frame_data[this_p.frame_len] = byte(pid)
		this_p.frame_len++
	}

	if i {
		if len(info) > 0 {
			if len(info) > MaxInfoLen {
				logrus.WithField("length", len(info)).Error("Internal error: UFrame: invalid information field length for U frame")
				info = info[:MaxInfoLen]
			}

			copy(this_p.frame_data[this_p.frame_len:], info)
			this_p.frame_len += len(info)
		}
	} else {
		if len(info) > 0 {
			logrus.Error("Internal error: UFrame: info part not allowed for this U frame type")
		}
	}

	return (this_p)
} /* end UFrame */

/*------------------------------------------------------------------------------
 *
 * Name:	SFrame
 *
 * Purpose:	Construct an S frame.
 *
 * Input:	addrs		- Array of addresses.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- CRCmd command frame, CRRes for a response frame.
 *
 *		ftype		- One of:
 *				        FrameTypeSRR,        // Receive Ready - System Ready To Receive
 *				        FrameTypeSRNR,       // Receive Not Ready - TNC Buffer Full
 *				        FrameTypeSREJ,       // Reject Frame - Out of Sequence or Duplicate
 *				        FrameTypeSSREJ,      // Selective Reject - Request single frame repeat
 *
 *		modulo		- 8 or 128.  Determines if we have 1 or 2 control bytes.
 *
 *		nr		- N(R) field --- describe.
 *
 *		pf		- Poll/Final flag.
 *
 *		info		- Info field.  Allowed only for SREJ.
 *
 *
 * Returns:	Pointer to new packet object.
 *
 *------------------------------------------------------------------------------*/

func SFrame(
	addrs [MaxAddrs]string,
	num_addr int,
	cr CmdRes,
	ftype FrameType,
	modulo Modulo,
	nr int,
	pf int,
	info []byte,
) *Packet {
	var this_p = New()

	if this_p == nil {
		return (nil)
	}

	if this_p.setAddrs(addrs, num_addr, cr) == 0 {
		logrus.Error("Internal error: SFrame: could not set addresses")

		return (nil)
	}

	if modulo != 8 && modulo != 128 {
		logrus.WithField("modulo", modulo).Error("Internal error: SFrame: invalid modulo for S frame")
		modulo = 8
	}

	this_p.modulo = modulo

	if nr < 0 || nr >= int(modulo) {
		logrus.WithField("nr", nr).Error("Internal error: SFrame: invalid N(R) for S frame")
		nr &= int(modulo - 1)
	}

	// Erratum: The AX.25 spec is not clear about whether SREJ should be command, response, or both.
	// The underlying X.25 spec clearly says it is response only.  Let's go with that.

	if ftype == FrameTypeSSREJ && cr != CRRes {
		logrus.Error("Internal error: SFrame: SREJ must be response")
	}

	var ctrl int

	switch ftype {
	case FrameTypeSRR:
		ctrl = 0x01
	case FrameTypeSRNR:
		ctrl = 0x05
	case FrameTypeSREJ:
		ctrl = 0x09
	case FrameTypeSSREJ:
		ctrl = 0x0d
	default:
		logrus.WithField("ftype", ftype).Error("Internal error: SFrame: invalid frame type for S frame")

		return (nil)
	}

	if modulo == 8 {
		if pf != 0 {
			ctrl |= 0x10
		}

		ctrl |= nr << 5
		this_p.frame_data[this_p.frame_len] = byte(ctrl) //nolint:gosec // G115: unchecked narrowing conversion, see #294
		this_p.frame_len++
	} else {
		this_p.frame_data[this_p.frame_len] = byte(ctrl)
		this_p.frame_len++

		ctrl = pf & 1
		ctrl |= nr << 1
		this_p.frame_data[this_p.frame_len] = byte(ctrl) //nolint:gosec // G115: unchecked narrowing conversion, see #294
		this_p.frame_len++
	}

	if ftype == FrameTypeSSREJ {
		if len(info) > 0 {
			if len(info) > MaxInfoLen {
				logrus.WithField("length", len(info)).Error("Internal error: SFrame: invalid information field length for SREJ frame")
				info = info[:MaxInfoLen]
			}

			copy(this_p.frame_data[this_p.frame_len:], info)
			this_p.frame_len += len(info)
		}
	} else {
		if len(info) > 0 {
			logrus.Error("Internal error: SFrame: info part not allowed for RR, RNR, REJ frame")
		}
	}

	return (this_p)
} /* end SFrame */

/*------------------------------------------------------------------------------
 *
 * Name:	IFrame
 *
 * Purpose:	Construct an I frame.
 *
 * Input:	addrs		- Array of addresses.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- CRCmd command frame, CRRes for a response frame.
 *
 *		modulo		- 8 or 128.
 *
 *		nr		- N(R) field --- describe.
 *
 *		ns		- N(S) field --- describe.
 *
 *		pf		- Poll/Final flag.
 *
 *		pid		- Protocol ID.
 *				  Normally 0xf0 meaning no level 3.
 *				  Could be other values for NET/ROM, etc.
 *
 *		info		- Info field.
 *
 *
 * Returns:	Pointer to new packet object.
 *
 *------------------------------------------------------------------------------*/

func IFrame(
	addrs [MaxAddrs]string,
	num_addr int,
	cr CmdRes,
	modulo Modulo,
	nr int,
	ns int,
	pf int,
	pid int,
	info []byte,
) *Packet {
	var this_p = New()

	if this_p == nil {
		return (nil)
	}

	if this_p.setAddrs(addrs, num_addr, cr) == 0 {
		logrus.Error("Internal error: IFrame: could not set addresses")

		return (nil)
	}

	if modulo != 8 && modulo != 128 {
		logrus.WithField("modulo", modulo).Error("Internal error: IFrame: invalid modulo for I frame")
		modulo = 8
	}

	this_p.modulo = modulo

	if nr < 0 || nr >= int(modulo) {
		logrus.WithField("nr", nr).Error("Internal error: IFrame: invalid N(R) for I frame")
		nr &= int(modulo - 1)
	}

	if ns < 0 || ns >= int(modulo) {
		logrus.WithField("ns", ns).Error("Internal error: IFrame: invalid N(S) for I frame")
		ns &= int(modulo - 1)
	}

	var ctrl int
	if modulo == 8 {
		ctrl = (nr << 5) | (ns << 1)
		if pf != 0 {
			ctrl |= 0x10
		}

		this_p.frame_data[this_p.frame_len] = byte(ctrl) //nolint:gosec // G115: unchecked narrowing conversion, see #294
		this_p.frame_len++
	} else {
		ctrl = ns << 1
		this_p.frame_data[this_p.frame_len] = byte(ctrl) //nolint:gosec // G115: unchecked narrowing conversion, see #294
		this_p.frame_len++

		ctrl = nr << 1
		if pf != 0 {
			ctrl |= 0x01
		}

		this_p.frame_data[this_p.frame_len] = byte(ctrl) //nolint:gosec // G115: unchecked narrowing conversion, see #294
		this_p.frame_len++
	}

	// Definitely don't want pid value of 0 (not in valid list)
	// or 0xff (which means more bytes follow).

	if pid < 0 || pid == 0 || pid == 0xff {
		logrus.WithField("pid", fmt.Sprintf("0x%02x", pid)).Warn("Client application provided invalid PID for I frame")
		pid = PIDNoLayer3
	}

	this_p.frame_data[this_p.frame_len] = byte(pid)
	this_p.frame_len++

	if len(info) > 0 {
		if len(info) > MaxInfoLen {
			logrus.WithField("length", len(info)).Error("Internal error: IFrame: invalid information field length for I frame")
			info = info[:MaxInfoLen]
		}

		copy(this_p.frame_data[this_p.frame_len:], info)
		this_p.frame_len += len(info)
	}

	return (this_p)
} /* end IFrame */

/*------------------------------------------------------------------------------
 *
 * Name:	setAddrs
 *
 * Purpose:	Set address fields
 *
 * Input:	pp		- Packet object.
 *
 *		addrs		- Array of addresses.  Same order as in frame.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- CRCmd command frame, CRRes for a response frame.
 *
 * Output:	pp.frame_data 	- 7 bytes for each address.
 *
 *		pp.frame_len	- num_addr * 7
 *
 *		p.num_addr	- num_addr
 *
 * Returns:	1 for success.  0 for failure.
 *
 *------------------------------------------------------------------------------*/

func (this_p *Packet) setAddrs(addrs [MaxAddrs]string, num_addr int, cr CmdRes) int {
	dwutil.Assert(this_p.frame_len == 0)
	dwutil.Assert(cr == CRCmd || cr == CRRes)

	if num_addr < MinAddrs || num_addr > MaxAddrs {
		logrus.WithField("num_addr", num_addr).Error("Internal error: setAddrs: bad number of addresses")

		return (0)
	}

	for n := range num_addr {
		var oaddr, ssid, _, ok = ParseAddr(n, addrs[n], AddrStrict)

		if !ok {
			return (0)
		}

		// Fill in address.

		copy(this_p.frame_data[n*7:], bytes.Repeat([]byte{' ' << 1}, 6))

		for i, c := range oaddr {
			this_p.frame_data[n*7+i] = byte(c << 1) //nolint:gosec // G115: unchecked narrowing conversion, see #294
		}

		// Fill in SSID.

		this_p.frame_data[n*7+6] = byte(0x60 | ((ssid & 0xf) << 1)) //nolint:gosec // G115: unchecked narrowing conversion, see #294

		// Command / response flag.

		switch n {
		case Destination:
			if cr == CRCmd {
				this_p.frame_data[n*7+6] |= 0x80
			}
		case Source:
			if cr == CRRes {
				this_p.frame_data[n*7+6] |= 0x80
			}
		default:
		}

		// Is this the end of address field?

		if n == num_addr-1 {
			this_p.frame_data[n*7+6] |= 1
		}

		this_p.frame_len += 7
	}

	this_p.num_addr = num_addr

	return (1)
} /* end setAddrs */
