//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Use multiple modems in parallel to increase chances
 *		of decoding less than ideal signals.
 *
 * Description:	The initial motivation was for HF SSB where mistuning
 *		causes a shift in the audio frequencies.  Here, we can
 * 		have multiple modems tuned to staggered pairs of tones
 *		in hopes that one will be close enough.
 *
 *		The overall structure opens the door to other approaches
 *		as well.  For VHF FM, the tones should always have the
 *		right frequencies but we might want to tinker with other
 *		modem parameters instead of using a single compromise.
 *
 * Originally:	The the interface application is in 3 places:
 *
 *		(a) Main program (direwolf.c or atest.c) calls
 *		    demod_init to set up modem properties and
 *		    NewLayer2Receiver for the HDLC, FX.25, IL2P and EAS
 *		    receivers.
 *
 *		(b) demod_process_sample is called for each audio sample
 *		    from the input audio stream.
 *
 *	   	(c) When a valid AX.25 frame is found, process_rec_frame,
 *		    provided by the application, in direwolf.c or atest.c,
 *		    is called.  Normally this comes from hdlc_rec.c but
 *		    there are a couple other special cases to consider.
 *		    It can be called from hdlc_rec2.c if it took a long
 *  		    time to "fix" corrupted bits.  aprs_tt.c constructs
 * 		    a fake packet when a touch tone message is received.
 *
 * New in version 0.9:
 *
 *		Put an extra layer in between which potentially uses
 *		multiple modems & HDLC decoders per channel.  The tricky
 *		part is picking the best one when there is more than one
 *		success and discarding the rest.
 *
 * New in version 1.1:
 *
 *		Several enhancements provided by Fabrice FAURE:
 *
 *		Additional types of attempts to fix a bad CRC.
 *		Optimized code to reduce execution time.
 *		Improved detection of duplicate packets from
 *		different fixup attempts.
 *		Set limit on number of packets in fix up later queue.
 *
 * New in version 1.6:
 *
 *		FX.25.  Previously a delay of a couple bits (or more accurately
 *		symbols) was fine because the decoders took about the same amount of time.
 *		Now, we can have an additional delay of up to 64 check bytes and
 *		some filler in the data portion.  We can't simply wait that long.
 *		With normal AX.25 a couple frames can come and go during that time.
 *		We want to delay the duplicate removal while FX.25 block reception
 *		is going on.
 *
 *------------------------------------------------------------------*/

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"slices"

	"github.com/doismellburning/samoyed/internal/ais"
	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/eas"
	"github.com/doismellburning/samoyed/internal/fx25"
	"github.com/doismellburning/samoyed/internal/hdlc"
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/sirupsen/logrus"
)

// Candidates for further processing.

type candidate_t struct {
	packet_p    *ax25.Packet
	alevel      ax25.ALevel
	speed_error float64     //nolint:unused
	fec_type    fec_type_t  // Type of FEC: none(0), fx25, il2p
	retries     BitFixLevel // For the old "fix bits" strategy, this is the
	// number of bits that were modified to get a good CRC.
	// It would be 0 to something around 4.
	// For FX.25, it is the number of corrected.
	// This could be from 0 thru 32.
	age   int
	crc   uint16
	score int
}

//#define PROCESS_AFTER_BITS 2		// version 1.4.  Was a little short for skew of PSK with different modem types, optional pre-filter

const PROCESS_AFTER_BITS = 3

// A MultiModem is one radio channel's layer between its demodulators and the
// rest of the program: the frames its subchannels and slicers have decoded,
// held for a few bit times so the best can be picked, and the running DC bias
// of its input.
//
// It is driven by its audio device's goroutine, which both feeds it samples
// and, by way of the HDLC, FX.25 and IL2P decoders, hands it frames.
type MultiModem struct {
	channel     int
	audioConfig *RadioConfig
	demodulator *Demodulator // nil for a channel that is not a radio.
	sink        ReceiveSink  // Where the frames it picks go.

	candidates [MAX_SUBCHANS][MAX_SLICERS]candidate_t

	// processAge is how many samples a candidate waits for others to turn up.
	processAge int

	dcAverage float64
}

// multiModems holds every channel's MultiModem, built at package
// initialisation so the decoders that hand frames on never find one nil.
var multiModems = newMultiModems()

func newMultiModems() [MAX_RADIO_CHANS]*MultiModem {
	var m [MAX_RADIO_CHANS]*MultiModem

	for channel := range m {
		m[channel] = new(MultiModem)
		m[channel].channel = channel
	}

	return m
}

// A ReceiveSink is where what the demodulators hear ends up: the frames they
// decode, and the data carrier detect state they derive from the incoming
// signal.
//
// radioSink is the sink for a radio channel being listened to for real.
// samoyed-atest, which decodes a .WAV file to report on what is in it rather
// than to act on it, has its own.
type ReceiveSink interface {
	// RecFrame hands over a frame that has been decoded successfully.
	RecFrame(channel int, subchan int, slice int, pp *ax25.Packet, alevel ax25.ALevel, fec_type fec_type_t, retries BitFixLevel, spectrum string)

	// DCDChange reports that the decoders for a channel have collectively
	// started (state 1) or stopped (state 0) seeing data.
	DCDChange(channel int, state int)
}

// radioSink is the ReceiveSink for a channel with a radio on the end of it.
type radioSink struct{}

func (s *radioSink) RecFrame(channel int, subchan int, slice int, pp *ax25.Packet, alevel ax25.ALevel, fec_type fec_type_t, retries BitFixLevel, spectrum string) {
	dataLinkQueue.RecFrame(channel, subchan, slice, pp, alevel, fec_type, retries, spectrum)
}

func (s *radioSink) DCDChange(channel int, state int) {
	pttControl.Set(OCTYPE_DCD, channel, state)
}

/*------------------------------------------------------------------------------
 *
 * Name:	multi_modem_init
 *
 * Purpose:	Called at application start up to initialize appropriate
 *		modems and HDLC decoders.
 *
 * Input:	Modem properties structure as filled in from the configuration file.
 *
 *		fx25Debug - FX.25's debug level, for the HDLC decoders.
 *
 *		sink	- Where the decoders' output goes.
 *
 * Outputs:
 *
 * Description:	Called once at application startup time.
 *
 *------------------------------------------------------------------------------*/

func multi_modem_init(pa *RadioConfig, fx25Debug int, sink ReceiveSink) {
	demod_init(pa)
	layer2Receiver = NewLayer2Receiver(pa, demodulators, fx25Debug, sink)

	for channel, m := range multiModems {
		m.audioConfig = pa
		m.demodulator = demodulators[channel]
		m.sink = sink

		// Anything still waiting to be picked came from before, e.g. the
		// previous file atest decoded, and would otherwise be handed on as
		// part of what comes next.
		m.candidates = [MAX_SUBCHANS][MAX_SLICERS]candidate_t{}

		if pa.chan_medium[channel] == MEDIUM_RADIO {
			if pa.achan[channel].baud <= 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Internal multi_modem_init error, channel=%d\n", channel)
				pa.achan[channel].baud = DEFAULT_BAUD
			}

			var real_baud = pa.achan[channel].baud
			if pa.achan[channel].modem_type == MODEM_QPSK {
				real_baud = pa.achan[channel].baud / 2
			}

			if pa.achan[channel].modem_type == MODEM_8PSK {
				real_baud = pa.achan[channel].baud / 3
			}

			m.processAge = PROCESS_AFTER_BITS * pa.adev[ACHAN2ADEV(channel)].samples_per_sec / real_baud
			//crc_queue_of_last_to_app[channel] = nil;
		}
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	multi_modem_process_sample
 *
 * Purpose:	Feed the sample into the proper modem(s) for the channel.
 *
 * Inputs:	channel	- Radio channel number
 *
 *		audio_sample
 *
 * Description:	In earlier versions we always had a one-to-one mapping with
 *		demodulators and HDLC decoders.
 *		This was added so we could have multiple modems running in
 *		parallel with different mark/space tones to compensate for
 *		mistuning of HF SSB signals.
 * 		It was also possible to run multiple filters, for the same
 *		tones, in parallel (e.g. ABC).
 *
 * Version 1.2:	Let's try something new for an experiment.
 *		We will have a single mark/space demodulator but multiple
 *		slicers, using different levels, each with its own HDLC decoder.
 *		We now have a separate variable, num_demod, which could be 1
 *		while num_subchan is larger.
 *
 * Version 1.3:	Go back to num_subchan with single meaning of number of demodulators.
 *		We now have separate independent variable, num_slicers, for the
 *		mark/space imbalance compensation.
 *		num_demod, while probably more descriptive, should not exist anymore.
 *
 *------------------------------------------------------------------------------*/

func multi_modem_get_dc_average(channel int) int { //nolint:unused
	// Scale to +- 200 so it will like the deviation measurement.
	return int(multiModems[channel].dcAverage * (200.0 / 32767.0))
}

func multi_modem_process_sample(channel int, audio_sample int) {
	multiModems[channel].ProcessSample(audio_sample)
}

// ProcessSample feeds one audio sample to each of the channel's demodulators,
// and sends on the best of the frames decoded once they have waited long
// enough for the others to catch up.
func (m *MultiModem) ProcessSample(audio_sample int) {
	var channel = m.channel

	// Accumulate an average DC bias level.
	// Shouldn't happen with a soundcard but could with mistuned SDR.
	m.dcAverage = m.dcAverage*0.999 + float64(audio_sample)*0.001

	// Issue 128.  Someone ran into this.

	var numSubchan, numSlicers = m.layout()

	if numSubchan <= 0 || numSubchan > MAX_SUBCHANS ||
		numSlicers <= 0 || numSlicers > MAX_SLICERS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR!  Something is seriously wrong in multi_modem_process_sample\n")
		dw_printf("channel = %d, num_subchan = %d [max %d], num_slicers = %d [max %d]\n", channel,
			numSubchan, MAX_SUBCHANS,
			numSlicers, MAX_SLICERS)
		dw_printf("Please report this message and include a copy of your configuration file.\n")
		os.Exit(1)
	}

	/* Formerly one loop. */
	/* 1.2: We can feed one demodulator but end up with multiple outputs. */

	/* Send same thing to all. */
	for d := range numSubchan {
		m.demodulator.ProcessSample(d, audio_sample)
	}

	for subchan := range numSubchan {
		for slice := range numSlicers {
			var c = &m.candidates[subchan][slice]
			if c.packet_p != nil {
				c.age++
				if c.age > m.processAge {
					if layer2Receiver.fx25Busy(channel) {
						c.age = 0
					} else {
						m.pickBestCandidate()
					}
				}
			}
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        multi_modem_process_rec_frame
 *
 * Purpose:     This is called when we receive a frame with a valid
 *		FCS and acceptable size.
 *
 * Inputs:	channel	- Audio channel number, 0 or 1.
 *		subchan	- Which modem found it.
 *		slice	- Which slice found it.
 *		fbuf	- Pointer to first byte in HDLC frame.
 *		flen	- Number of bytes excluding the FCS.
 *		alevel	- Audio level, range of 0 - 100.
 *				(Special case, use negative to skip
 *				 display of audio level line.
 *				 Use -2 to indicate DTMF message.)
 *		retries	- Level of correction used.
 *		fec_type	- none(0), fx25, il2p
 *
 * Description:	Add to list of candidates.  Best one will be picked later.
 *
 *--------------------------------------------------------------------*/

func multi_modem_process_rec_frame(channel int, subchan int, slice int, fbuf []byte, alevel ax25.ALevel, retries BitFixLevel, fec_type fec_type_t) {
	dwutil.Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	dwutil.Assert(subchan >= 0 && subchan < MAX_SUBCHANS)
	dwutil.Assert(slice >= 0 && slice < MAX_SLICERS)

	var pa = multiModems[channel].audioConfig

	// Special encapsulation for AIS & EAS so they can be treated normally pretty much everywhere else.

	var pp *ax25.Packet

	switch pa.achan[channel].modem_type {
	case MODEM_AIS:
		var nmea, err = ais.ToNMEA(fbuf)
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%v\n", err)

			return
		}

		// The intention is for the AIS sentences to go only to attached applications.
		// e.g. SARTrack knows how to parse the AIS sentences.

		// Put NOGATE in path so RF>IS IGates will block this.
		// TODO: Use station callsign, rather than "AIS," so we know where it is coming from,
		// if it happens to get onto RF somehow.

		var monfmt = fmt.Sprintf("AIS>%s%1d%1d,NOGATE:{%c%c%s", APP_TOCALL, MAJOR_VERSION, MINOR_VERSION, aprs.UserDefUserID, aprs.UserDefTypeAIS, string(nmea))
		pp = ax25.FromText(monfmt, true)

		// alevel gets in there somehow making me question why it is passed thru here.
	case MODEM_EAS:
		var monfmt = fmt.Sprintf("EAS>%s%1d%1d,NOGATE:{%c%c%s", APP_TOCALL, MAJOR_VERSION, MINOR_VERSION, aprs.UserDefUserID, aprs.UserDefTypeEAS, string(fbuf))
		pp = ax25.FromText(monfmt, true)

		// alevel gets in there somehow making me question why it is passed thru here.
	default:
		pp = ax25.FromFrame(fbuf, alevel)
	}

	multi_modem_process_rec_packet(channel, subchan, slice, pp, alevel, retries, fec_type)
}

// TODO: Eliminate function above and move code elsewhere?

func multi_modem_process_rec_packet_real(channel int, subchan int, slice int, pp *ax25.Packet, alevel ax25.ALevel, retries BitFixLevel, fec_type fec_type_t) {
	multiModems[channel].processRecPacket(subchan, slice, pp, alevel, retries, fec_type)
}

// processRecPacket takes a frame one of the channel's decoders found: straight
// on if there is only the one decoder, otherwise as a candidate for
// pickBestCandidate.
func (m *MultiModem) processRecPacket(subchan int, slice int, pp *ax25.Packet, alevel ax25.ALevel, retries BitFixLevel, fec_type fec_type_t) {
	var channel = m.channel
	var pa = m.audioConfig

	if pp == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unexpected internal problem in multi_modem_process_rec_packet_real\n")

		return /* oops!  why would it fail? */
	}

	/*
	 * If only one demodulator/slicer, and no FX.25 in progress,
	 * push it thru and forget about all this foolishness.
	 */
	var numSubchan, numSlicers = m.layout()

	if numSubchan == 1 &&
		numSlicers == 1 &&
		!layer2Receiver.fx25Busy(channel) {
		var drop_it = false

		if pa.recv_error_rate != 0 {
			var r = float64(rand.Int63n(1<<53)) / (1 << 53) // Random, 0.0 to 1.0

			if float64(pa.recv_error_rate)/100.0 > r {
				drop_it = true

				text_color_set(DW_COLOR_INFO)
				dw_printf("Intentionally dropping incoming frame.  Recv Error rate = %d per cent.\n", pa.recv_error_rate)
			}
		}

		if !drop_it {
			recordRadioFrame(channel, fec_type, retries)
			m.sink.RecFrame(channel, subchan, slice, pp, alevel, fec_type, retries, "")
		}

		return
	}

	/*
	 * Otherwise, save them up for a few bit times so we can pick the best.
	 */
	/* Plain old AX.25: Oops!  Didn't expect one to be there already. */
	/* FX.25: Quietly replace anything already there.  It will have priority. */
	var c = &m.candidates[subchan][slice]

	c.packet_p = pp
	c.alevel = alevel
	c.fec_type = fec_type
	c.retries = retries
	c.age = 0
	c.crc = pp.MultiModemCRC()
}

/*-------------------------------------------------------------------
 *
 * Name:        pick_best_candidate
 *
 * Purpose:     This is called when we have one or more candidates
 *		available for a certain amount of time.
 *
 * Description:	Pick the best one and send it up to the application.
 *		Discard the others.
 *
 * Rules:	We prefer one received perfectly but will settle for
 *		one where some bits had to be flipped to get a good CRC.
 *
 *--------------------------------------------------------------------*/

/* This is a suitable order for interleaved "G" demodulators. */
/* Opposite order would be suitable for multi-frequency although */
/* multiple slicers are of questionable value for HF SSB. */

// #define subchan_from_n(x) ((x) % num_subchan)
func (m *MultiModem) subchanFromN(x int) int {
	return x % m.demodulator.NumSubchan()
}

// #define slice_from_n(x)   ((x) / num_subchan)
func (m *MultiModem) sliceFromN(x int) int {
	return x / m.demodulator.NumSubchan()
}

// layout is how many subchannels, and slicers in each, the channel's
// demodulator has: none for a channel without one.
func (m *MultiModem) layout() (int, int) {
	if m.demodulator == nil {
		return 0, 0
	}

	return m.demodulator.NumSubchan(), m.demodulator.NumSlicers()
}

func (m *MultiModem) pickBestCandidate() {
	var channel = m.channel
	var pa = m.audioConfig

	var numSubchan, numSlicers = m.layout()
	var num_bars = max(numSlicers, 1) * numSubchan

	var spectrum [MAX_SUBCHANS*MAX_SLICERS + 1]byte

	for n := range num_bars {
		var j = m.subchanFromN(n)
		var k = m.sliceFromN(n)

		/* Build the spectrum display. */

		if m.candidates[j][k].packet_p == nil {
			spectrum[n] = '_'
		} else if m.candidates[j][k].fec_type != fec_type_none { // FX.25 or IL2P
			// FIXME: using retries both as an enum and later int too.
			if m.candidates[j][k].retries >= 0 && m.candidates[j][k].retries <= 9 {
				spectrum[n] = '0' + byte(m.candidates[j][k].retries)
			} else {
				spectrum[n] = '+'
			}
		} else if m.candidates[j][k].retries == RETRY_NONE { // AX.25 below
			spectrum[n] = '|'
		} else if m.candidates[j][k].retries == RETRY_INVERT_SINGLE {
			spectrum[n] = ':'
		} else {
			spectrum[n] = '.'
		}

		/* Beginning score depends on effort to get a valid frame CRC. */

		if m.candidates[j][k].packet_p == nil {
			m.candidates[j][k].score = 0
		} else {
			if m.candidates[j][k].fec_type != fec_type_none {
				m.candidates[j][k].score = 9000 - 100*int(m.candidates[j][k].retries) // has FEC
			} else {
				/* Originally, this produced 0 for the PASSALL case. */
				/* This didn't work so well when looking for the best score. */
				/* Around 1.3 dev H, we add an extra 1 in here so the minimum */
				/* score should now be 1 for anything received.  */
				m.candidates[j][k].score = int(BitFixPassall)*1000 - int(m.candidates[j][k].retries*1000) + 1
			}
		}
	}

	// FIXME: IL2p & FX.25 don't have CRC calculated. Must fill it in first.

	/* Bump it up slightly if others nearby have the same CRC. */

	for n := range num_bars {
		var j = m.subchanFromN(n)
		var k = m.sliceFromN(n)

		if m.candidates[j][k].packet_p != nil {
			for o := range num_bars {
				var oj = m.subchanFromN(o)
				var ok = m.sliceFromN(o)

				if o != n && m.candidates[oj][ok].packet_p != nil {
					if m.candidates[j][k].crc == m.candidates[oj][ok].crc {
						m.candidates[j][k].score += (num_bars + 1) - int(math.Abs(float64(o-n)))
					}
				}
			}
		}
	}

	var best_n = 0
	var best_score = 0

	for n := range num_bars {
		var j = m.subchanFromN(n)
		var k = m.sliceFromN(n)

		if m.candidates[j][k].packet_p != nil {
			if m.candidates[j][k].score > best_score {
				best_score = m.candidates[j][k].score
				best_n = n
			}
		}
	}

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithField("spectrum", spectrum).Trace("pickBestCandidate")

		for n := range num_bars {
			var j = m.subchanFromN(n)
			var k = m.sliceFromN(n)
			var c = &m.candidates[j][k]

			var logEntry = logrus.WithFields(logrus.Fields{
				"channel": channel,
				"subchan": j,
				"slice":   k,
				"best":    n == best_n,
			})

			if c.packet_p == nil {
				logEntry.Trace("candidate: no packet")
			} else {
				logEntry.WithFields(logrus.Fields{
					"fec_type": c.fec_type,
					"retries":  c.retries,
					"age":      c.age,
					"crc":      fmt.Sprintf("%04x", c.crc),
					"score":    c.score,
				}).Trace("candidate")
			}
		}
	}

	if best_score == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unexpected internal problem in pick_best_candidate.  How can best score be zero?\n")
	}

	/*
	 * send the best one along.
	 */

	/* Discard those not chosen. */

	for n := range num_bars {
		var j = m.subchanFromN(n)

		var k = m.sliceFromN(n)
		if n != best_n && m.candidates[j][k].packet_p != nil {
			m.candidates[j][k].packet_p = nil
		}
	}

	/* Pass along one. */

	var j = m.subchanFromN(best_n)
	var k = m.sliceFromN(best_n)

	var drop_it = false

	if pa.recv_error_rate != 0 {
		var r = float64(rand.Int63n(1<<53)) / (1 << 53) // Random, 0.0 to 1.0

		if float64(pa.recv_error_rate)/100.0 > r {
			drop_it = true

			text_color_set(DW_COLOR_INFO)
			dw_printf("Intentionally dropping incoming frame.  Recv Error rate = %d per cent.\n", pa.recv_error_rate)
		}
	}

	if drop_it {
		m.candidates[j][k].packet_p = nil
	} else {
		dwutil.Assert(m.candidates[j][k].packet_p != nil)
		recordRadioFrame(channel, m.candidates[j][k].fec_type, m.candidates[j][k].retries)
		m.sink.RecFrame(channel, j, k,
			m.candidates[j][k].packet_p,
			m.candidates[j][k].alevel,
			m.candidates[j][k].fec_type,
			(m.candidates[j][k].retries),
			string(spectrum[:num_bars]))

		/* Ownership has been transferred, so drop our reference. */
		m.candidates[j][k].packet_p = nil
	}

	/* Clear in preparation for next time. */

	m.candidates = [MAX_SUBCHANS][MAX_SLICERS]candidate_t{}
} /* end pickBestCandidate */

// slicerReceivers are the receivers one slicer's bits go to.  Its line
// decoder undoes NRZI, and scrambling for 9600 baud, once for all of them:
// HDLC and FX.25 take the data bits it gives, IL2P the raw bits before it.
// An EAS channel's bits go to its EAS receiver instead, and nowhere else.
type slicerReceivers struct {
	line linecode.Decoder

	hdlc *hdlc.Receiver
	fx25 *fx25.Receiver
	il2p *il2p.Receiver
	eas  *eas.Receiver // nil unless the channel is EAS.
}

// newHDLCConfig takes what the HDLC receiver needs from a channel's settings.
func newHDLCConfig(achan *achan_param_s) hdlc.Config {
	return hdlc.Config{
		FixBits:    achan.fix_bits,
		Passall:    achan.passall,
		AIS:        achan.modem_type == MODEM_AIS,
		SanityTest: achan.sanity_test,
	}
}

func newSlicerReceivers(r *Layer2Receiver, channel int, subchannel int, slice int, scrambled bool) *slicerReceivers {
	var s = new(slicerReceivers)
	s.hdlc = hdlc.NewReceiver(newHDLCConfig(&r.audio.achan[channel]), channel, subchannel, slice, scrambled, &s.line, demod_get_audio_level, multi_modem_process_rec_frame)
	s.fx25 = fx25.NewReceiver(channel, subchannel, slice, r.fx25Debug, demod_get_audio_level, multi_modem_process_rec_frame)
	s.il2p = il2p.NewReceiver(channel, subchannel, slice, r.audio.achan[channel].il2p_version, r.audio.achan[channel].il2p_crc, demod_get_audio_level, multi_modem_process_rec_packet)

	if r.audio.achan[channel].modem_type == MODEM_EAS {
		s.eas = eas.NewReceiver(channel, subchannel, slice, demod_get_audio_level, multi_modem_process_rec_frame)
	}

	return s
}

// Layer2Receiver takes the bits each radio channel's demodulators hear, and
// hands them to the layer 2 receivers - HDLC, FX.25 and IL2P - for every
// (channel, subchannel, slicer) combination.  It also keeps the aggregated
// DCD state of each channel.
type Layer2Receiver struct {
	slicer        [MAX_RADIO_CHANS][MAX_SUBCHANS][MAX_SLICERS]*slicerReceivers
	numSubchannel [MAX_RADIO_CHANS]int //TODO1.2 use ptr rather than copy.
	compositeDCD  [MAX_RADIO_CHANS][MAX_SUBCHANS + 1][MAX_SLICERS]bool
	audio         *RadioConfig
	fx25Debug     int // FX.25's debug level, for every slicer's FX.25 receiver.
	sink          ReceiveSink

	// Own copy of random number generator so we can get
	// same predictable results on different operating systems.
	// TODO: Consolidate multiple copies somewhere.
	randSeed int32
}

const hdlcRecRandMax int32 = 0x7fffffff

/***********************************************************************************
 *
 * Name:	NewLayer2Receiver
 *
 * Purpose:	Call once at the beginning to initialize.
 *
 * Inputs:	pa	- Audio configuration.
 *
 *		demods	- Each radio channel's demodulators, which say how
 *			  many subchannels it has; nil for any other channel.
 *
 *		fx25Debug - FX.25's debug level, for every slicer's FX.25
 *			  receiver.
 *
 *		sink	- Where a change in the channel's data carrier detect
 *			  state is reported.
 *
 ***********************************************************************************/

func NewLayer2Receiver(pa *RadioConfig, demods [MAX_RADIO_CHANS]*Demodulator, fx25Debug int, sink ReceiveSink) *Layer2Receiver {
	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("NewLayer2Receiver (%p) \n", pa);

	var r = new(Layer2Receiver)
	r.audio = pa
	r.fx25Debug = fx25Debug
	r.sink = sink
	r.randSeed = 1

	for ch, d := range demods {
		if d != nil {
			r.numSubchannel[ch] = d.NumSubchan()

			for sub := range r.numSubchannel[ch] {
				for slice := range MAX_SLICERS {
					r.slicer[ch][sub][slice] = newSlicerReceivers(r, ch, sub, slice, pa.achan[ch].modem_type == MODEM_SCRAMBLE)
				}
			}
		}
	}

	return r
}

/***********************************************************************************
 *
 * Name:	hdlc_rec_bit
 *
 * Purpose:	Extract HDLC frames from a stream of bits.
 *
 * Inputs:	channel	- Channel number.
 *
 *		subchannel	- This allows multiple demodulators per channel.
 *
 *		slice	- Allows multiple slicers per demodulator (subchannel).
 *
 *		raw 	- One bit from the demodulator.
 *			  should be 0 or 1.
 *
 *		is_scrambled - Is the data scrambled?
 *
 *		descram_state - Current descrambler state.  (not used - remove)
 *				Not so fast - plans to add new parameter.  PSK already provides it.
 *
 *
 * Description:	This is called once for each received bit.
 *		For each valid frame, process_rec_frame()
 *		is called for further processing.
 *
 ***********************************************************************************/

func (r *Layer2Receiver) RecBit(channel int, subchannel int, slice int, raw int, is_scrambled bool, not_used_remove int) {
	var dummyll int64
	var dummy int
	r.RecBitNew(channel, subchannel, slice, raw, is_scrambled, not_used_remove, &dummyll, &dummy)
}

func (r *Layer2Receiver) RecBitNew(channel int, subchannel int, slice int, _raw int, is_scrambled bool, not_used_remove int,
	pll_nudge_total *int64, pll_symbol_count *int) {
	var raw = _raw != 0

	// -e option can be used to artificially introduce the desired
	// Bit Error Rate (BER) for testing.

	if r.audio.recv_ber != 0 {
		var p = float64(r.rand()) / float64(hdlcRecRandMax) // calculate as double to preserve all 31 bits.
		if r.audio.recv_ber > p {
			// FIXME
			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("hdlc_rec_bit randomly clobber bit, ber = %.6f\n", r.audio.recv_ber);
			raw = !raw
		}
	}

	var s = r.slicer[channel][subchannel][slice]

	// EAS does not use HDLC.

	if r.audio.achan[channel].modem_type == MODEM_EAS {
		s.eas.RecBit(dwutil.IfThenElse(raw, 1, 0), not_used_remove)

		return
	}

	/*
	 * Using NRZI encoding,
	 *   A '0' bit is represented by an inversion since previous bit.
	 *   A '1' bit is represented by no change.
	 */

	var dbit = s.line.Decode(raw, is_scrambled) /* Data bit after undoing NRZI. */

	// After BER insertion, NRZI, and any descrambling, feed into FX.25 decoder as well.
	// Don't waste time on this if AIS.  EAS does not get this far.

	if r.audio.achan[channel].modem_type != MODEM_AIS {
		s.fx25.RecBit(dwutil.IfThenElse(dbit, 1, 0))
		s.il2p.RecBit(dwutil.IfThenElse(raw, 1, 0)) // Note: skip NRZI.
	}

	s.hdlc.RecBit(raw, dbit, is_scrambled, pll_nudge_total, pll_symbol_count)
}

/***********************************************************************************
 *
 * Name:        Layer2Receiver.fx25Busy
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

func (r *Layer2Receiver) fx25Busy(channel int) bool {
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

// TODO:  Data Carrier Detect (DCD) is now based on DPLL lock
// rather than data patterns found here.
// It would make sense to move the next 2 functions to demod.c
// because this is done at the modem level, rather than HDLC decoder.

/*-------------------------------------------------------------------
 *
 * Name:        dcd_change
 *
 * Purpose:     Combine DCD states of all subchannels/ into an overall
 *		state for the channel.
 *
 * Inputs:	channel
 *
 *		subchannel		0 to MAX_SUBCHANS-1 for HDLC.
 *				SPECIAL CASE --> MAX_SUBCHANS for DTMF decoder.
 *
 *		slice		slicer number, 0 .. MAX_SLICERS - 1.
 *
 *		state		1 for active, 0 for not.
 *
 * Returns:	None.  Use hdlc_rec_data_detect_any to retrieve result.
 *
 * Description:	DCD for the channel is active if ANY of the subchannels/slices
 *		are active.  Update the DCD indicator.
 *
 * version 1.3:	Add DTMF detection into the final result.
 *		This is now called from dtmf.c too.
 *
 *--------------------------------------------------------------------*/

func (r *Layer2Receiver) DCDChange(channel int, subchannel int, slice int, state int) {
	/*
		#if DEBUG3
			text_color_set(DW_COLOR_DEBUG);
			dw_printf ("DCD %d.%d.%d = %d \n", channel, subchannel, slice, state);
		#endif
	*/

	var old = r.DataDetectAny(channel)

	if state != 0 {
		r.compositeDCD[channel][subchannel][slice] = true
	} else {
		r.compositeDCD[channel][subchannel][slice] = false
	}

	var newVal = r.DataDetectAny(channel)

	if newVal != old {
		r.sink.DCDChange(channel, newVal)
		metrics.SetDCD(channel, newVal != 0)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        hdlc_rec_data_detect_any
 *
 * Purpose:     Determine if the radio channel is currently busy
 *		with packet data.
 *		This version doesn't care about voice or other sounds.
 *		This is used by the transmit logic to transmit only
 *		when the channel is clear.
 *
 * Inputs:	channel	- Audio channel.
 *
 * Returns:	True if channel is busy (data detected) or
 *		false if OK to transmit.
 *
 *
 * Description:	We have two different versions here.
 *
 *		hdlc_rec_data_detect_any sees if ANY of the decoders
 *		for this channel are receiving a signal.   This is
 *		used to determine whether the channel is clear and
 *		we can transmit.  This would apply to the 300 baud
 *		HF SSB case where we have multiple decoders running
 *		at the same time.  The channel is busy if ANY of them
 *		thinks the channel is busy.
 *
 * Version 1.3: New option for input signal to inhibit transmit.
 *
 *--------------------------------------------------------------------*/

func (r *Layer2Receiver) DataDetectAny(channel int) int {
	for sc := range r.numSubchannel[channel] {
		if slices.Contains(r.compositeDCD[channel][sc][:], true) {
			return (1)
		}
	}

	if pttControl.GetInput(ICTYPE_TXINH, channel) == 1 {
		return (1)
	}

	return (0)
} /* end DataDetectAny */

func (r *Layer2Receiver) rand() int32 {
	r.randSeed = (r.randSeed*1103515245 + 12345) & hdlcRecRandMax // Wraps on overflow, as intended.

	return r.randSeed
}

/* end multi_modem.c */
