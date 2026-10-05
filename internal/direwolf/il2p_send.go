package direwolf

import (
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/sirupsen/logrus"
)

// IL2PSender sends IL2P frames, and IL2P's preamble, on a channel's line.
// IL2P is not HDLC and skips NRZI: its bits go out as they are, most
// significant first, inverted if asked.
type IL2PSender struct {
	line    *linecode.Encoder
	channel int // For logging.

	bitsSent int // Count number of bits sent by SendFrame or SendPreamble.
}

// NewIL2PSender makes an IL2PSender for channel that sends on line.
func NewIL2PSender(line *linecode.Encoder, channel int) *IL2PSender {
	var s = new(IL2PSender)
	s.line = line
	s.channel = channel

	return s
}

/*-------------------------------------------------------------
 *
 * Name:	SendFrame (il2p_send_frame in Dire Wolf)
 *
 * Purpose:	Convert frames to a stream of bits in IL2P format.
 *
 * Inputs:	pp	- Pointer to packet object.
 *
 *		version	- IL2P version to speak.
 *
 *		max_fec	- 1 to force 16 parity symbols for each payload block.
 *			  0 for automatic depending on block size.
 *			  Only consulted for IL2P_VERSION_0_4.
 *
 *		crc	- true to append the trailing CRC.
 *
 *		polarity - 0 for normal.  1 to invert signal.
 *			   2 special case for testing - introduce some errors to test FEC.
 *
 * Outputs:	Bits are shipped out to the sender's tone generator.
 *
 * Returns:	Number of bits sent including
 *		- Preamble   (01010101...)
 *		- 3 byte Sync Word.
 *		- 15 bytes for Header.
 *		- Optional payload.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *		-1 is returned for failure.
 *
 * Description:	Generate an IL2P encoded frame.
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		the tone generator are processed correctly.
 *
 * Errors:	Return -1 for error.  Probably frame too large.
 *
 * Note:	Inconsistency here. ax25 version has just a byte array
 *		and length going in.  Here we need the full packet object.
 *
 *--------------------------------------------------------------*/

func (s *IL2PSender) SendFrame(pp *ax25.Packet, version il2p_version_t, max_fec int, crc bool, polarity int) int {
	var syncWordBytes = []byte{
		(IL2P_SYNC_WORD >> 16) & 0xff,
		(IL2P_SYNC_WORD >> 8) & 0xff,
		(IL2P_SYNC_WORD) & 0xff,
	}

	var encoded, elen = il2p_encode_frame(pp, version, max_fec, crc)
	if elen <= 0 {
		logrus.WithField("channel", s.channel).Warn("IL2P: Unable to encode frame into IL2P")

		return (-1)
	}

	var data = append(syncWordBytes, encoded...)

	s.bitsSent = 0

	if il2p_get_debug() >= 1 {
		var logEntry = logrus.WithFields(logrus.Fields{
			"channel": s.channel,
			"version": version.String(),
			"max_fec": max_fec,
			"bytes":   len(data),
		})
		logEntry.Debug("IL2P: Sending frame")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, data)
	}

	// Clobber some bytes for testing.
	if polarity >= 2 {
		for j := 10; j < len(data); j += 100 {
			data[j] = ^data[j]
		}
	}

	// Send bits to modulator.

	s.sendByteMSBFirst(IL2P_PREAMBLE, polarity)

	for _, x := range data {
		s.sendByteMSBFirst(int(x), polarity)
	}

	return s.bitsSent
}

// SendPreamble sends nbytes of IL2P's filler pattern, 01010101, for before
// and after a frame, and returns the number of bits sent.  polarity is as for
// SendFrame.
func (s *IL2PSender) SendPreamble(nbytes int, polarity int) int {
	s.bitsSent = 0

	for range nbytes {
		s.sendByteMSBFirst(IL2P_PREAMBLE, polarity)
	}

	return s.bitsSent
}

// The next one is only for IL2P.  No NRZI.
// MSB first, opposite of AX.25.
// NRZI would be applied for AX.25 but IL2P does not use it.
// However we do have an option to invert the signal.
// The direwolf receive implementation will automatically compensate
// for either polarity but other implementations might not.

func (s *IL2PSender) sendByteMSBFirst(x int, polarity int) {
	for range 8 {
		s.line.Write((x&0x80) != 0, polarity&1 != 0)

		x <<= 1
		s.bitsSent++
	}
}

// end il2p_send.c
