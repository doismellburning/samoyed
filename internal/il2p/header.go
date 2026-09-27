// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/sirupsen/logrus"
)

/*--------------------------------------------------------------------------------
 *
 * Purpose:	Functions to deal with the IL2P header.
 *
 * Reference:	https://tarpn.net/t/il2p/il2p-specification_draft_v0-6.pdf
 *
 *--------------------------------------------------------------------------------*/

// Convert ASCII to/from DEC SIXBIT as defined here:
// https://en.wikipedia.org/wiki/Six-bit_character_code#DEC_six-bit_code

func ascii_to_sixbit(a rune) byte {
	if a >= ' ' && a <= '_' {
		return byte(a - ' ')
	}

	return (31) // '?' for any invalid.
}

func sixbit_to_ascii(s byte) rune {
	return rune(s + ' ')
}

// Functions for setting the various header fields.
// It is assumed that it was zeroed first so only the '1' bits are set.

func set_il2p_field(hdr []byte, bit_num int, lsb_index int, width int, value int) {
	for width > 0 && value != 0 {
		dwutil.Assert(lsb_index >= 0 && lsb_index <= 11)

		if value&1 != 0 {
			hdr[lsb_index] |= byte(1 << bit_num)
		}

		value >>= 1
		lsb_index--
		width--
	}

	dwutil.Assert(value == 0)
}

func setUI(hdr []byte, val int) {
	set_il2p_field(hdr, 6, 0, 1, val)
}

func setPID(hdr []byte, val int) {
	set_il2p_field(hdr, 6, 4, 4, val)
}

func setControl(hdr []byte, val int) {
	set_il2p_field(hdr, 6, 11, 7, val)
}

func setFECLevel(hdr []byte, val int) {
	set_il2p_field(hdr, 7, 0, 1, val)
}

func setHdrType(hdr []byte, val int) {
	set_il2p_field(hdr, 7, 1, 1, val)
}

func setPayloadByteCount(hdr []byte, val int) {
	set_il2p_field(hdr, 7, 11, 10, val)
}

// Extracting the fields.

func get_il2p_field(hdr []byte, bit_num int, lsb_index int, width int) int {
	var result = 0

	lsb_index -= width - 1
	for width > 0 {
		result <<= 1

		dwutil.Assert(lsb_index >= 0 && lsb_index <= 11)

		var x = hdr[lsb_index]
		if x&(1<<bit_num) != 0 {
			result |= 1
		}

		lsb_index++
		width--
	}

	return (result)
}

func getUI(hdr []byte) int {
	return get_il2p_field(hdr, 6, 0, 1)
}

func getPID(hdr []byte) int {
	return get_il2p_field(hdr, 6, 4, 4)
}

func getControl(hdr []byte) int {
	return get_il2p_field(hdr, 6, 11, 7)
}

func getFECLevel(hdr []byte) int {
	return get_il2p_field(hdr, 7, 0, 1)
}

func getHdrType(hdr []byte) int {
	return get_il2p_field(hdr, 7, 1, 1)
}

func getPayloadByteCount(hdr []byte) int {
	return get_il2p_field(hdr, 7, 11, 10)
}

// AX.25 'I' and 'UI' frames have a protocol ID which determines how the
// information part should be interpreted.
// Here we squeeze the most common cases down to 4 bits.
// Return -1 if translation is not possible.  Fall back to type 0 header in this case.

func encode_pid(pp *ax25.Packet) int {
	var pid = pp.PID()

	if (pid & 0x30) == 0x20 {
		return (0x2) // AX.25 Layer 3
	}

	if (pid & 0x30) == 0x10 {
		return (0x2) // AX.25 Layer 3
	}

	if pid == 0x01 {
		return (0x3) // ISO 8208 / CCIT X.25 PLP
	}

	if pid == 0x06 {
		return (0x4) // Compressed TCP/IP
	}

	if pid == 0x07 {
		return (0x5) // Uncompressed TCP/IP
	}

	if pid == 0x08 {
		return (0x6) // Segmentation fragmen
	}

	if pid == 0xcc {
		return (0xb) // ARPA Internet Protocol
	}

	if pid == 0xcd {
		return (0xc) // ARPA Address Resolution
	}

	if pid == 0xce {
		return (0xd) // FlexNet
	}

	if pid == 0xcf {
		return (0xe) // TheNET
	}

	if pid == 0xf0 {
		return (0xf) // No L3
	}

	return (-1)
}

// Convert IL2P 4 bit PID to AX.25 8 bit PID.

func decode_pid(pid int) int {
	var axpid = [16]int{
		0xf0, // Should not happen. 0 is for 'S' frames.
		0xf0, // Should not happen. 1 is for 'U' frames (but not UI).
		0x20, // AX.25 Layer 3
		0x01, // ISO 8208 / CCIT X.25 PLP
		0x06, // Compressed TCP/IP
		0x07, // Uncompressed TCP/IP
		0x08, // Segmentation fragment
		0xf0, // Future
		0xf0, // Future
		0xf0, // Future
		0xf0, // Future
		0xcc, // ARPA Internet Protocol
		0xcd, // ARPA Address Resolution
		0xce, // FlexNet
		0xcf, // TheNET
		0xf0, // No L3
	}

	dwutil.Assert(pid >= 0 && pid <= 15)

	return (axpid[pid])
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_type_1_header
 *
 * Purpose:	Attempt to create type 1 header from packet object.
 *
 * Inputs:	pp	- Packet object.
 *
 *		fec_level - Value for the header bit which is the FEC Level in
 *			  v0.4 and RESERVED in v0.6.  See il2p_tx_fec.
 *
 * Returns:	hdr	- IL2P header with no scrambling or parity symbols.
 *			  Must be large enough to hold HeaderSize unsigned bytes.
 *
 * Returns:	Number of bytes for information part or -1 for failure.
 *		In case of failure, fall back to type 0 transparent encapsulation.
 *
 * Description:	Type 1 Headers do not support AX.25 repeater callsign addressing,
 *		Modulo-128 extended mode window sequence numbers, nor any callsign
 *		characters that cannot translate to DEC SIXBIT.
 *		If these cases are encountered during IL2P packet encoding,
 *		the encoder switches to Type 0 Transparent Encapsulation.
 *		SABME can't be handled by type 1.
 *
 *--------------------------------------------------------------------------------*/

func il2p_type_1_header(pp *ax25.Packet, fec_level int) ([]byte, int) {
	var hdr = make([]byte, HeaderSize)

	if pp.NumAddr() != 2 {
		// Only two addresses are allowed for type 1 header.
		return nil, -1
	}

	// Check does not apply for 'U' frames but put in one place rather than two.

	if pp.Modulo() == ax25.Modulo128 {
		return nil, -1
	}

	// Destination and source addresses go into low bits 0-5 for bytes 0-11.

	var dst_addr = pp.AddrNoSSID(ax25.Destination)
	var dst_ssid = pp.SSID(ax25.Destination)

	var src_addr = pp.AddrNoSSID(ax25.Source)
	var src_ssid = pp.SSID(ax25.Source)

	for i, b := range dst_addr {
		if b < ' ' || b > '_' {
			// Shouldn't happen but follow the rule.
			return nil, -1
		}

		hdr[i] = ascii_to_sixbit(b)
	}

	for i, b := range src_addr {
		if b < ' ' || b > '_' {
			// Shouldn't happen but follow the rule.
			return nil, -1
		}

		hdr[6+i] = ascii_to_sixbit(b)
	}

	// Byte 12 has DEST SSID in upper nybble and SRC SSID in lower nybble and
	hdr[12] = byte((dst_ssid << 4) | src_ssid)

	var cr, _, pf, nr, ns, frame_type = pp.FrameType()

	//dw_printf ("%s(): %s-%d>%s-%d: %s\n", __func__, src_addr, src_ssid, dst_addr, dst_ssid, description);

	switch frame_type {
	case ax25.FrameTypeSRR, ax25.FrameTypeSRNR, ax25.FrameTypeSREJ, ax25.FrameTypeSSREJ:
		// Receive Ready - System Ready To Receive
		// Receive Not Ready - TNC Buffer Full
		// Reject Frame - Out of Sequence or Duplicate
		// Selective Reject - Request single frame repeat
		// S frames (RR, RNR, REJ, SREJ), mod 8, have control N(R) P/F S S 0 1
		// These are mapped into    P/F N(R) C S S
		// Bit 6 is not mentioned in documentation but it is used for P/F for the other frame types.
		// C is copied from the C bit in the destination addr.
		// C from source is not used here.  Reception assumes it is the opposite.
		// PID is set to 0, meaning none, for S frames.
		setUI(hdr, 0)
		setPID(hdr, 0)
		setControl(hdr, (pf<<6)|(nr<<3)|(((dwutil.IfThenElse((cr == ax25.CRCmd), 1, 0))|(dwutil.IfThenElse((cr == ax25.CR11), 1, 0)))<<2))

		// This gets OR'ed into the above.
		switch frame_type {
		case ax25.FrameTypeSRR:
			setControl(hdr, 0)
		case ax25.FrameTypeSRNR:
			setControl(hdr, 1)
		case ax25.FrameTypeSREJ:
			setControl(hdr, 2)
		case ax25.FrameTypeSSREJ:
			setControl(hdr, 3)
		default:
		}

	case ax25.FrameTypeUSABM, ax25.FrameTypeUDISC, ax25.FrameTypeUDM, ax25.FrameTypeUUA, ax25.FrameTypeUFRMR, ax25.FrameTypeUUI, ax25.FrameTypeUXID, ax25.FrameTypeUTEST:
		// Set Async Balanced Mode
		// Disconnect
		// Disconnect Mode
		// Unnumbered Acknowledge
		// Frame Reject
		// Unnumbered Information
		// Exchange Identification
		// Test
		// The encoding allows only 3 bits for frame type and SABME got left out.
		// Control format:  P/F opcode[3] C n/a n/a
		// The grayed out n/a bits are observed as 00 in the example.
		// The header UI field must also be set for UI frames.
		// PID is set to 1 for all U frames other than UI.
		if frame_type == ax25.FrameTypeUUI {
			setUI(hdr, 1) // I guess this is how we distinguish 'I' and 'UI'
			// on the receiving end.
			var pid = encode_pid(pp)
			if pid < 0 {
				return nil, -1
			}

			setPID(hdr, pid)
		} else {
			setPID(hdr, 1) // 1 for 'U' other than 'UI'.
		}

		// Each of the destination and source addresses has a "C" bit.
		// They should normally have the opposite setting.
		// IL2P has only a single bit to represent 4 possbilities.
		//
		//	dst	src	il2p	meaning
		//	---	---	----	-------
		//	0	0	0	Not valid (earlier protocol version)
		//	1	0	1	Command (v2)
		//	0	1	0	Response (v2)
		//	1	1	1	Not valid (earlier protocol version)
		//
		// APRS does not mention how to set these bits and all 4 combinations
		// are seen in the wild.  Apparently these are ignored on receive and no
		// one cares.  Here we copy from the C bit in the destination address.
		// It should be noted that the case of both C bits being the same can't
		// be represented so the il2p encode/decode bit not produce exactly the
		// same bits.  We see this in the second example in the protocol spec.
		// The original UI frame has both C bits of 0 so it is received as a response.

		setControl(hdr, (pf<<6)|(((dwutil.IfThenElse((cr == ax25.CRCmd), 1, 0))|(dwutil.IfThenElse((cr == ax25.CR11), 1, 0)))<<2))

		// This gets OR'ed into the above.
		switch frame_type {
		case ax25.FrameTypeUSABM:
			setControl(hdr, 0<<3)
		case ax25.FrameTypeUDISC:
			setControl(hdr, 1<<3)
		case ax25.FrameTypeUDM:
			setControl(hdr, 2<<3)
		case ax25.FrameTypeUUA:
			setControl(hdr, 3<<3)
		case ax25.FrameTypeUFRMR:
			setControl(hdr, 4<<3)
		case ax25.FrameTypeUUI:
			setControl(hdr, 5<<3)
		case ax25.FrameTypeUXID:
			setControl(hdr, 6<<3)
		case ax25.FrameTypeUTEST:
			setControl(hdr, 7<<3)
		default:
		}

	case ax25.FrameTypeI: // Information
		// I frames (mod 8 only)
		// encoded control: P/F N(R) N(S)
		setUI(hdr, 0)

		var pid2 = encode_pid(pp)
		if pid2 < 0 {
			return nil, -1
		}

		setPID(hdr, pid2)

		setControl(hdr, (pf<<6)|(nr<<3)|ns)

	default:
		// case frame_type_U_SABME:		// Set Async Balanced Mode, Extended
		// case frame_type_U:			// other Unnumbered, not used by AX.25.
		// case frame_not_AX25:		// Could not get control byte from frame.

		// Fall back to the header type 0 for these.
		return nil, -1
	}

	// Common for all header type 1.

	// Bit 7 has [FEC Level:1], [HDR Type:1], [Payload byte Count:10]

	setFECLevel(hdr, fec_level)
	setHdrType(hdr, 1)

	var pinfo = pp.Info()
	if len(pinfo) > maxPayloadSize {
		return nil, -2
	}

	setPayloadByteCount(hdr, len(pinfo))

	return hdr, len(pinfo)
}

// This should create a packet from the IL2P header.
// The information part will not be filled in.

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_decode_header_type_1
 *
 * Purpose:	Attempt to convert type 1 header to a packet object.
 *
 * Inputs:	hdr - IL2P header with no scrambling or parity symbols.
 *
 *		num_sym_changed - Number of symbols changed by FEC in the header.
 *				Should be 0 or 1.
 *
 * Returns:	Packet Object or nil for failure.
 *
 * Description:	A later step will process the payload for the information part.
 *
 *--------------------------------------------------------------------------------*/

func il2p_decode_header_type_1(hdr []byte, num_sym_changed int) *ax25.Packet {
	if getHdrType(hdr) != 1 {
		logrus.Error("IL2P internal error: Should not be here: il2p_decode_header_type_1, when header type is 0")

		return (nil)
	}

	// First get the addresses including SSID.

	var addrs [ax25.MaxAddrs]string
	var num_addr = 2

	// The IL2P header uses 2 parity symbols which means a single corrupted symbol (byte)
	// can always be corrected.
	// However, I have seen cases, where the error rate is very high, where the RS decoder
	// thinks it found a valid code block by changing one symbol but it was the wrong one.
	// The result is trash.  This shows up as address fields like 'R&G4"A' and 'TEW\ !'.
	// I added a sanity check here to catch characters other than upper case letters and digits.
	// The frame should be rejected in this case.  The question is whether to discard it
	// silently or print a message so the user can see that something strange is happening?
	// My current thinking is that it should be silently ignored if the header has been
	// modified (correctee or more likely, made worse in this cases).
	// If no changes were made, something weird is happening.  We should mention it for
	// troubleshooting rather than sweeping it under the rug.

	// The same thing has been observed with the payload, under very high error conditions,
	// and max_fec==0.  Here I don't see a good solution.  AX.25 information can contain
	// "binary" data so I'm not sure what sort of sanity check could be added.
	// This was not observed with max_fec==1.  If we make that the default, same as Nino TNC,
	// it would be extremely extremely unlikely unless someone explicitly selects weaker FEC.

	// TODO: We could do something similar for header type 0.
	// The address fields should be all binary zero values.
	// Someone overly ambitious might check the addresses found in the first payload block.

	var byteBuf []byte
	for i := range 6 {
		byteBuf = append(byteBuf, byte(sixbit_to_ascii(hdr[i]&0x3f)))
	}

	addrs[ax25.Destination] = strings.TrimSpace(string(byteBuf))

	for _, c := range addrs[ax25.Destination] {
		if !unicode.IsUpper(c) && !unicode.IsDigit(c) { // TODO KG How can this be true?
			if num_sym_changed == 0 { //nolint:staticcheck
				// This can pop up sporadically when receiving random noise.
				// Would be better to show only when debug is enabled but variable not available here.
				// TODO: For now we will just suppress it.
				//text_color_set(DW_COLOR_ERROR);
				//dw_printf ("IL2P: Invalid character '%c' in destination address '%s'\n", addrs[AX25_DESTINATION][i], addrs[AX25_DESTINATION]);
			}

			return (nil)
		}
	}
	var destSSID = int((hdr[12] >> 4) & 0xf)
	addrs[ax25.Destination] += fmt.Sprintf("-%d", destSSID)

	byteBuf = []byte{}
	for i := range 6 {
		byteBuf = append(byteBuf, byte(sixbit_to_ascii(hdr[i+6]&0x3f)))
	}

	addrs[ax25.Source] = strings.TrimSpace(string(byteBuf))

	for _, c := range addrs[ax25.Source] {
		if !unicode.IsUpper(c) && !unicode.IsDigit(c) {
			if num_sym_changed == 0 { //nolint:staticcheck
				// This can pop up sporadically when receiving random noise.
				// Would be better to show only when debug is enabled but variable not available here.
				// TODO: For now we will just suppress it.
				//text_color_set(DW_COLOR_ERROR);
				//dw_printf ("IL2P: Invalid character '%c' in source address '%s'\n", addrs[AX25_SOURCE][i], addrs[AX25_SOURCE]);
			}

			return (nil)
		}
	}
	var srcSSID = int(hdr[12] & 0xf)
	addrs[ax25.Source] += fmt.Sprintf("-%d", srcSSID)

	// The PID field gives us the general type.
	// 0 = 'S' frame.
	// 1 = 'U' frame other than UI.
	// others are either 'UI' or 'I' depending on the UI field.

	var pid = getPID(hdr)
	var ui = getUI(hdr)

	if pid == 0 {
		// 'S' frame.
		// The control field contains: P/F N(R) C S S
		var control = getControl(hdr)
		var cr = dwutil.IfThenElse((control&0x04) != 0, ax25.CRCmd, ax25.CRRes)
		var ftype ax25.FrameType

		switch control & 0x03 {
		case 0:
			ftype = ax25.FrameTypeSRR
		case 1:
			ftype = ax25.FrameTypeSRNR
		case 2:
			ftype = ax25.FrameTypeSREJ
		default:
			ftype = ax25.FrameTypeSSREJ
		}
		var modulo = ax25.Modulo8
		var nr = (control >> 3) & 0x07
		var pf = (control >> 6) & 0x01
		var pinfo []byte // Any info for SREJ will be added later.

		return (ax25.SFrame(addrs, num_addr, cr, ftype, modulo, nr, pf, pinfo))
	} else if pid == 1 {
		// 'U' frame other than 'UI'.
		// The control field contains: P/F OPCODE{3) C x x
		var control = getControl(hdr)
		var cr = dwutil.IfThenElse((control&0x04) != 0, ax25.CRCmd, ax25.CRRes)
		var axpid = 0 // unused for U other than UI.
		var ftype ax25.FrameType

		switch (control >> 3) & 0x7 {
		case 0:
			ftype = ax25.FrameTypeUSABM
		case 1:
			ftype = ax25.FrameTypeUDISC
		case 2:
			ftype = ax25.FrameTypeUDM
		case 3:
			ftype = ax25.FrameTypeUUA
		case 4:
			ftype = ax25.FrameTypeUFRMR
		case 5:
			ftype = ax25.FrameTypeUUI
			axpid = 0xf0
			// Should not happen with IL2P pid == 1.
		case 6:
			ftype = ax25.FrameTypeUXID
		default:
			ftype = ax25.FrameTypeUTEST
		}
		var pf = (control >> 6) & 0x01
		var pinfo []byte // Any info for UI, XID, TEST will be added later.

		return (ax25.UFrame(addrs, num_addr, cr, ftype, pf, axpid, pinfo))
	} else if ui != 0 {
		// 'UI' frame.
		// The control field contains: P/F OPCODE{3) C x x
		var control = getControl(hdr)
		var cr = dwutil.IfThenElse((control&0x04) != 0, ax25.CRCmd, ax25.CRRes)
		var ftype = ax25.FrameTypeUUI
		var pf = (control >> 6) & 0x01
		var axpid = decode_pid(getPID(hdr))
		var pinfo []byte // Any info for UI, XID, TEST will be added later.

		return (ax25.UFrame(addrs, num_addr, cr, ftype, pf, axpid, pinfo))
	} else {
		// 'I' frame.
		// The control field contains: P/F N(R) N(S)
		var control = getControl(hdr)
		var cr = ax25.CRCmd // Always command.
		var pf = (control >> 6) & 0x01
		var nr = (control >> 3) & 0x7
		var ns = (control & 0x7)
		var modulo = ax25.Modulo8
		var axpid = decode_pid(getPID(hdr))
		var pinfo []byte // Any info for UI, XID, TEST will be added later.

		return (ax25.IFrame(addrs, num_addr, cr, modulo, nr, ns, pf, axpid, pinfo))
	}
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_type_0_header
 *
 * Purpose:	Attempt to create type 0 header from packet object.
 *
 * Inputs:	pp	- Packet object.
 *
 *		fec_level - Value for the header bit which is the FEC Level in
 *			  v0.4 and RESERVED in v0.6.  See il2p_tx_fec.
 *
 * Returns:	hdr	- IL2P header with no scrambling or parity symbols.
 *			  Must be large enough to hold HeaderSize unsigned bytes.
 *
 * Returns:	Number of bytes for information part or -1 for failure.
 *		In case of failure, fall back to type 0 transparent encapsulation.
 *
 * Description:	The type 0 header is used when it is not one of the restricted cases
 *		covered by the type 1 header.
 *		The AX.25 frame is put in the payload.
 *		This will cover: more than one address, mod 128 sequences, etc.
 *
 *--------------------------------------------------------------------------------*/

func il2p_type_0_header(pp *ax25.Packet, fec_level int) ([]byte, int) {
	var hdr = make([]byte, HeaderSize)

	// Bit 7 has [FEC Level:1], [HDR Type:1], [Payload byte Count:10]

	setFECLevel(hdr, fec_level)
	setHdrType(hdr, 0)

	var frame_len = pp.FrameLen()

	if frame_len < 14 || frame_len > maxPayloadSize {
		return nil, -2
	}

	setPayloadByteCount(hdr, frame_len)

	return hdr, frame_len
}

/***********************************************************************************
 *
 * Name:        HeaderAttributes
 *
 * Purpose:     Extract a few attributes from an IL2p header.
 *
 * Inputs:      hdr	- IL2P header structure.
 *
 * Returns:     hdr_type - 0 or 1.
 *
 *		fec_level - The header bit which is the FEC Level in v0.4 and
 *			  RESERVED in v0.6.  See RxMaxFEC.
 *
 * Returns:	Payload byte count.   (actual payload size, not the larger encoded format)
 *
 ***********************************************************************************/

func HeaderAttributes(hdr []byte) (int, int, int) {
	return getHdrType(hdr), getFECLevel(hdr), getPayloadByteCount(hdr)
}

/***********************************************************************************
 *
 * Name:        ClarifyHeader
 *
 * Purpose:     Convert received header to usable form.
 *		This involves RS FEC then descrambling.
 *
 * Inputs:      rec_hdr	- Header as received over the radio.
 *
 * Returns:     corrected_descrambled_hdr - After RS FEC and unscrambling.
 *
 * Returns:	Number of symbols that were corrected:
 *		 0 = No errors
 *		 1 = Single symbol corrected.
 *		 <0 = Unable to obtain good header.
 *
 ***********************************************************************************/

func ClarifyHeader(rec_hdr []byte) ([]byte, int) {
	var corrected, e = il2p_decode_rs(rec_hdr, HeaderParity)

	var corrected_descrambled_hdr = il2p_descramble_block(corrected)

	return corrected_descrambled_hdr, e
}
