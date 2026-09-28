package direwolf

import (
	"github.com/doismellburning/samoyed/internal/dwutil"
)

// fx25_deliver_frame is the fx25.FrameSink used in normal operation, passing
// each frame the FX.25 receiver extracts on to the rest of the receive path.
func fx25_deliver_frame(channel int, subchannel int, slice int, frame []byte, derrors int) {
	var alevel = demod_get_audio_level(channel, subchannel)

	multi_modem_process_rec_frame(channel, subchannel, slice, frame, alevel, BitFixLevel(derrors), 1)
}

/***********************************************************************************
 *
 * Name:        HDLCReceiver.fx25Busy
 *
 * Purpose:     Is FX.25 reception currently in progress?
 *
 * Inputs:      channel    - Channel number.
 *
 * Returns:	True if currently in progress for the specified channel.
 *
 * Description: This is required for duplicate removal.  One channel and can have
 *		multiple demodulators (called subchannels) running in parallel.
 *		Each of them can have multiple slicers.  Duplicates need to be
 *		removed.  Normally a delay of a couple bits (or more accurately
 *		symbols) was fine because they all took about the same amount of time.
 *		Now, we can have an additional delay of up to 64 check bytes and
 *		some filler in the data portion.  We can't simply wait that long.
 *		With normal AX.25 a couple frames can come and go during that time.
 *		We want to delay the duplicate removal while FX.25 block reception
 *		is going on.
 *
 ***********************************************************************************/

func (r *HDLCReceiver) fx25Busy(channel int) bool {
	dwutil.Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	if r == nil {
		return false
	}

	// This could be a little faster if we knew number of
	// subchannels and slicers but it is probably insignificant.

	for sub := range MAX_SUBCHANS {
		for slice := range MAX_SLICERS {
			var s = r.slicer[channel][sub][slice]
			if s != nil && s.fx25.Busy() {
				return true
			}
		}
	}

	return false
}
