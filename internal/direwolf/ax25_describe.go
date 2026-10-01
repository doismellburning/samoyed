package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Describe an AX.25 frame in human readable form.
 *
 * Description:	The pieces - AX25HexDump, AX25FormatAddrs, decode_aprs -
 *		already exist.  This puts them together the way anything
 *		inspecting frames off the wire wants them, and says what is
 *		wrong with a frame too malformed for the next step, rather
 *		than walking off the end of it.
 *
 *------------------------------------------------------------------*/

import (
	"fmt"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
)

/*------------------------------------------------------------------
 *
 * Function:	DescribeAX25Frame
 *
 * Purpose:	Print a description of one AX.25 frame: the header, the
 *		digipeater path, the control and PID fields, and the
 *		information field decoded as APRS where the frame is APRS.
 *
 * Inputs:	aprsDecoder - What to decode and print an APRS frame with.
 *
 *		frame	- The frame, as it appeared on the air, without the
 *			  FCS and without any KISS framing.
 *
 * Returns:	The number of problems found with the frame.
 *
 *------------------------------------------------------------------*/

func DescribeAX25Frame(aprsDecoder *aprs.Decoder, frame []byte) int {
	if len(frame) < ax25.MinPacketLen {
		fmt.Printf("ERROR: The frame is %d bytes, too short for an AX.25 header of at least %d.\n", len(frame), ax25.MinPacketLen)

		return 1
	}

	var alevel ax25.ALevel

	var pp = ax25.FromFrame(frame, alevel)
	if pp == nil {
		fmt.Printf("ERROR: Could not construct an AX.25 frame from those %d bytes.\n", len(frame))

		return 1
	}

	/*
	 * Establish that the frame has the fields the description is about to read
	 * before reading any of them.  AX25HexDump takes the control and PID
	 * octets on trust, so a frame that stops short of them would otherwise be
	 * described in terms of the zero padding past its end.
	 */

	if pp.NumAddr() < ax25.MinAddrs {
		/*
		 * The end of address bit is not at the end of a 7 byte address, or it
		 * marks out fewer than 2 or more than 10 addresses.  Without knowing
		 * where the address field stops there is nothing more to say.
		 */
		fmt.Printf("ERROR: The address field is malformed - the end of address bit does not mark out %d to %d addresses of 7 bytes each.\n",
			ax25.MinAddrs, ax25.MaxAddrs)
		dwutil.HexDump(frame)

		return 1
	}

	var frameLen = pp.FrameLen()

	if pp.ControlOffset() >= frameLen {
		fmt.Printf("ERROR: The frame ends after the address field - there is no control byte.\n")
		dwutil.HexDump(frame)

		return 1
	}

	if pp.InfoOffset() > frameLen {
		fmt.Printf("ERROR: The frame is %d bytes, but the address, control and PID fields need %d - it ends before the information field.\n",
			frameLen, pp.InfoOffset())
		dwutil.HexDump(frame)

		return 1
	}

	fmt.Printf("--- AX.25 frame ---\n")
	pp.HexDump()
	fmt.Printf("-------------------\n")

	var problems = 0

	fmt.Printf("%s\n", pp.FormatAddrs())

	if !pp.CheckAddresses(ax25.AddrStrict) {
		problems++
	}

	var info = pp.Info()

	if pp.IsAPRS() {
		ax25.SafePrint(info, true) // Display non-ASCII as hexadecimal.
		fmt.Printf("\n")
		ax25.NoteSafePrintTruncation(len(info))

		var A = aprsDecoder.Decode(pp, false) // Extract information into structure.

		aprsDecoder.Print(A) // Now print it in human readable format.
	} else {
		/*
		 * The control and PID octets are in the dump above, and either of them
		 * can be the reason, so don't name one of them as the culprit.
		 */
		fmt.Printf("APRS travels in a UI frame with PID 0xf0.  This is not one, so its %d byte information field is not decoded as APRS.\n", len(info))

		if len(info) > 0 {
			ax25.SafePrint(info, true)
			fmt.Printf("\n")
			ax25.NoteSafePrintTruncation(len(info))
		}
	}

	return problems
}
