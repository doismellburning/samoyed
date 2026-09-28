// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package fx25

import (
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/sirupsen/logrus"
)

/*-------------------------------------------------------------
 *
 * Name:	EncodeFrame
 *
 * Purpose:	Wrap an AX.25 frame up as an FX.25 codeblock.
 *
 * Inputs:	channel	- For log entries.
 *
 *		fx_mode	- 16, 32 or 64 for the desired number of check bytes, the
 *			  shortest format adequate for the data being picked
 *			  automatically, or 100 + a correlation tag number.  See pickMode.
 *
 *		debug	- FX.25's debug level.
 *
 *		fbuf	- Frame buffer, without the FCS.
 *
 * Returns:	The correlation tag number, the "data" part to be transmitted,
 *		and the check bytes.
 *		The tag number is -1, and the other two are nil, for failure.
 *
 *--------------------------------------------------------------*/

func EncodeFrame(channel int, fbuf []byte, fx_mode int, debug int) (int, []byte, []byte) {
	if debug >= 3 {
		logrus.WithFields(logrus.Fields{"channel": channel, "fx_mode": fx_mode}).Debug("FX.25 send frame")
		dwutil.HexDump(fbuf)
	}

	// Append the FCS.

	var frameFCS = fcs.Calc(fbuf)
	fbuf = append(fbuf, byte(frameFCS)&0xff)
	fbuf = append(fbuf, byte(frameFCS>>8)&0xff)

	// Add bit-stuffing, filling to FX25_MAX_DATA bytes with flag patterns
	var stuffedBytes, meaningfulLen = bitStuff(fbuf, MaxData)
	var dlen = meaningfulLen // Use meaningful length, not total buffer size

	// Pick suitable correlation tag depending on
	// user's preference, for number of check bytes,
	// and the data size.
	var ctag_num = pickMode(fx_mode, dlen)

	if ctag_num < CTagMin || ctag_num > CTagMax {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"fx_mode": fx_mode,
			"dlen":    dlen,
		}).Error("FX.25: Could not find suitable format for requested mode and data length")

		return -1, nil, nil
	}

	var k_data_radio = kDataRadio(ctag_num)
	var k_data_rs = kDataRS(ctag_num)

	// Zero out part of data which won't be transmitted
	var shorten_by = MaxData - k_data_radio
	if shorten_by > 0 {
		for i := k_data_radio; i < MaxData; i++ {
			stuffedBytes[i] = 0
		}
	}

	var data = stuffedBytes

	// Compute the check bytes.

	var rs = codecFor(ctag_num)
	var nroots = rs.NRoots()

	dwutil.Assert(k_data_rs+nroots == rs.N())

	var check = rs.Encode(data[:k_data_rs])

	if debug >= 3 {
		logrus.WithFields(logrus.Fields{
			"channel":    channel,
			"data_bytes": k_data_radio,
			"ctag":       ctag_num,
		}).Debug("FX.25 transmit data bytes")
		dwutil.HexDump(data[:k_data_radio])
		logrus.WithFields(logrus.Fields{"channel": channel, "check_bytes": nroots}).Debug("FX.25 transmit check bytes")
		dwutil.HexDump(check[:nroots])
	}

	return ctag_num, data[:k_data_radio], check[:nroots]
}

/*-------------------------------------------------------------
 *
 * Name:	bitStuff
 *
 * Purpose:	Perform HDLC bit-stuffing and add "flag" octets in
 *		preparation for the RS encoding.
 *
 * Inputs:	in	- Frame, including FCS, in.
 *
 *		maxBytes - if >0, fill output to exactly this many bytes with flag patterns
 *
 * Returns:	Stuffed bytes, and meaningful length before flag padding
 *
 * Description:	Convert to stream of bits including:
 *			start flag
 *			bit stuffed data, including FCS
 *			end flag
 *
 *--------------------------------------------------------------*/

// Is it particularly time/space efficient? No.
// But it should work!
func bitStuff(in []byte, maxBytes int) ([]byte, int) {
	const flag byte = 0x7e

	var outBits []bool

	// Start flag

	for i := range 8 {
		var v = flag&(1<<i) > 0
		outBits = append(outBits, v)
	}

	// In data

	var ones = 0

	for _, b := range in {
		for i := range 8 {
			var v = b&(1<<i) > 0
			outBits = append(outBits, v)

			if v {
				ones++
				if ones == 5 {
					outBits = append(outBits, false)
					ones = 0
				}
			} else {
				ones = 0
			}
		}
	}

	// End flag

	for i := range 8 {
		var v = flag&(1<<i) > 0
		outBits = append(outBits, v)
	}

	dwutil.Assert(len(outBits) >= 16) // Start and end flags
	dwutil.Assert(len(outBits) >= 16+8*len(in))

	// Remember where meaningful data ends (before flag padding)
	meaningfulBits := len(outBits)

	// Fill remainder with flag patterns (rotating through flag bits)
	if maxBytes > 0 {
		maxBits := maxBytes * 8

		bitPos := 0 // Which bit position of flag to use (0-7)
		for len(outBits) < maxBits {
			v := flag&(1<<bitPos) > 0
			outBits = append(outBits, v)
			bitPos = (bitPos + 1) % 8
		}
	}

	// Now byte it up

	var outBytes []byte

	for len(outBits) >= 8 {
		var b byte

		for bitIdx := range 8 {
			if outBits[bitIdx] {
				b |= 1 << bitIdx
			}
		}

		outBytes = append(outBytes, b)
		outBits = outBits[8:]
	}

	// And the last 0-7 bits if present

	if len(outBits) > 0 {
		var b byte

		for bitIdx := 0; bitIdx < 8 && bitIdx < len(outBits); bitIdx++ {
			if outBits[bitIdx] {
				b |= 1 << bitIdx
			}
		}

		outBytes = append(outBytes, b)
	}

	var meaningfulLen = (meaningfulBits + 7) / 8 // Round up to bytes

	return outBytes, meaningfulLen
}
