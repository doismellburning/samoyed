package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Transmit queued up packets when channel is clear.
 *
 * Description:	Producers of packets to be transmitted call TransmitQueue.Append and then
 *		go merrily on their way, unconcerned about when the packet might
 *		actually get transmitted.
 *
 *		This thread waits until the channel is clear and then removes
 *		packets from the queue and transmits them.
 *
 *
 * Usage:	(1) The main application calls NewXmitService.
 *
 *			This will initialize the transmit packet queue
 *			and create a thread to empty the queue when
 *			the channel is clear.
 *
 *		(2) The application queues up packets by calling TransmitQueue.Append.
 *
 *			Packets that are being digipeated should go in the
 *			high priority queue so they will go out first.
 *
 *			Other packets should go into the lower priority queue.
 *
 *		(3) xmit_thread removes packets from the queue and transmits
 *			them when other signals are not being heard.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/eas"
	"github.com/doismellburning/samoyed/internal/fx25"
	"github.com/doismellburning/samoyed/internal/hdlc"
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/lestrrat-go/strftime"
	"github.com/sirupsen/logrus"
)

const MORSE_DEFAULT_WPM = 10

// xmitTiming is one channel's transmit timing: what the configuration file
// set, unless a client application has since changed it.
type xmitTiming struct {
	slottime int /* Slot time in 10 mS units for persistence algorithm. */

	persist int /* Sets probability for transmitting after each */
	/* slot time delay.  Transmit if a random number */
	/* in range of 0 - 255 <= persist value.  */
	/* Otherwise wait another slot time and try again. */

	txdelay int /* After turning on the transmitter, */
	/* send "flags" for txdelay * 10 mS. */

	txtail int /* Amount of time to keep transmitting after we */
	/* are done sending the data.  This is to avoid */
	/* dropping PTT too soon and chopping off the end */
	/* of the frame.  Again 10 mS units. */

	fulldup bool /* Full duplex if true. */
}

/*
 * XmitService holds all transmit state.
 * Each channel can have different timing values.
 *
 * These are initialized once at application startup time
 * and some can be changed later by commands from connected applications.
 */

type XmitService struct {
	// timingMu guards timing, which a client application can change - KISS
	// has a command for each of its fields - from its listening goroutine,
	// while the channel's xmit_thread reads it.  Go through the Set methods and
	// channelTiming rather than touching it directly.
	timingMu sync.Mutex

	timing [MAX_RADIO_CHANS]xmitTiming

	bits_per_sec [MAX_RADIO_CHANS]int /* Data transmission rate. */
	/* Often called baud rate which is equivalent for */
	/* 1200 & 9600 cases but could be different with other */
	/* modulation techniques. */

	debugXmitPacket bool /* print packet in hexadecimal form for debugging. */

	/*
	 * The audio devices we transmit through.  When an audio device is in
	 * stereo mode, we can have two different channels that want to transmit
	 * at the same time.  We are not clever enough to multiplex them so hold
	 * the device's outputMu so only one is active at the same time.
	 */
	audio *AudioDevices

	/*
	 * Whether each audio device can transmit at all, sampled once the devices
	 * are open.  A station with no output device must not key a transmitter
	 * to send samples that go nowhere.
	 */
	audioOutAvailable [MAX_ADEVS]bool

	/* Whether we have said that a channel cannot transmit, so we say it once. */
	saidCannotTransmit [MAX_RADIO_CHANS]bool

	/*
	 * Each channel's layer 2 sending state, which carries over from one transmission
	 * to the next.  Only that channel's xmit_thread touches it.
	 */
	layer2Senders [MAX_RADIO_CHANS]*Layer2Sender

	/*
	 * Each radio channel's tone generator.  Only that channel's
	 * xmit_thread drives it.
	 */
	toneGenerators [MAX_RADIO_CHANS]*ToneGenerator

	// onTransmit is told each frame we send, and its channel, or is nil.
	onTransmit func(channel int, pp *ax25.Packet)

	p_modem   *RadioConfig
	fx25Debug int
	il2pDebug int
}

/*-------------------------------------------------------------------
 *
 * Name:        NewXmitService
 *
 * Purpose:     Initialize the transmit process.
 *
 * Inputs:	p_modem		- Structure with modem and timing parameters.
 *
 *		audio		- The audio devices to transmit through.
 *
 *		toneGenerators	- Each radio channel's tone generator.
 *
 *		onTransmit	- Called with each frame we send, and its
 *				  channel; nil for nobody to tell.
 *
 *		fx25Debug	- FX.25's debug level, for each channel's
 *				  Layer2Sender.
 *
 *		il2pDebug	- IL2P's debug level, likewise.
 *
 *
 * Outputs:	Returns a new XmitService with required information set up.
 *		The PTT hardware is set up beforehand, by NewPTT.
 *
 * Description:	Initialize the queue to be empty and set up other
 *		mechanisms for sharing it between different threads.
 *
 *		Start up xmit_thread(s) to actually send the packets
 *		at the appropriate time.
 *
 * Version 1.2:	We now allow multiple audio devices with one or two channels each.
 *		Each audio channel has its own thread.
 *
 *--------------------------------------------------------------------*/

func NewXmitService(
	ctx context.Context,
	p_modem *RadioConfig,
	audio *AudioDevices,
	toneGenerators [MAX_RADIO_CHANS]*ToneGenerator,
	onTransmit func(channel int, pp *ax25.Packet),
	debug_xmit_packet bool,
	fx25Debug int,
	il2pDebug int,
) *XmitService {
	logrus.Debug("xmit_init")
	var xs = &XmitService{} //nolint:exhaustruct_v5
	xs.p_modem = p_modem
	xs.audio = audio
	xs.toneGenerators = toneGenerators
	xs.onTransmit = onTransmit
	xs.fx25Debug = fx25Debug
	xs.il2pDebug = il2pDebug

	xs.debugXmitPacket = debug_xmit_packet

	/*
	 * Save parameters for later use.
	 * TODO1.2:  Any reason to use global config rather than making a copy?
	 */

	for a := range MAX_ADEVS {
		xs.audioOutAvailable[a] = audio.transmitAvailable(a)
	}

	for j := range MAX_RADIO_CHANS {
		xs.bits_per_sec[j] = p_modem.achan[j].baud
		xs.timing[j] = xmitTiming{
			slottime: p_modem.achan[j].slottime,
			persist:  p_modem.achan[j].persist,
			txdelay:  p_modem.achan[j].txdelay,
			txtail:   p_modem.achan[j].txtail,
			fulldup:  p_modem.achan[j].fulldup,
		}
	}

	logrus.Debug("xmit_init: about to call tq_init")
	transmitQueue.Init(p_modem)

	logrus.Debug("xmit_init: about to create threads")

	//TODO:  xmit thread should be higher priority to avoid
	// underrun on the audio output device.

	for j := range MAX_RADIO_CHANS {
		if p_modem.chan_medium[j] == MEDIUM_RADIO {
			go xs.xmit_thread(ctx, j)
		}
	}

	logrus.Debug("xmit_init: finished")

	return xs
}

/*-------------------------------------------------------------------
 *
 * Name:        SetTxdelay
 *		SetPersist
 *		SetSlottime
 *		SetTxtail
 *		SetFulldup
 *
 *
 * Purpose:     The KISS protocol, and maybe others, can specify
 *		transmit timing parameters.  If the application
 *		specifies these, they will override what was read
 *		from the configuration file.
 *
 * Inputs:	channel	- should be 0 or 1.
 *
 *		value	- time values are in 10 mSec units.
 *
 *
 * Outputs:	Remember required information for future use.
 *
 * Question:	Should we have an option to enable or disable the
 *		application changing these values?
 *
 * Bugs:	No validity checking other than array subscript out of bounds.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) SetTxdelay(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.timingMu.Lock()
		xs.timing[channel].txdelay = value
		xs.timingMu.Unlock()
	}
}

func (xs *XmitService) SetPersist(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.timingMu.Lock()
		xs.timing[channel].persist = value
		xs.timingMu.Unlock()
	}
}

func (xs *XmitService) SetSlottime(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.timingMu.Lock()
		xs.timing[channel].slottime = value
		xs.timingMu.Unlock()
	}
}

func (xs *XmitService) SetTxtail(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.timingMu.Lock()
		xs.timing[channel].txtail = value
		xs.timingMu.Unlock()
	}
}

func (xs *XmitService) SetFulldup(channel int, value bool) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.timingMu.Lock()
		xs.timing[channel].fulldup = value
		xs.timingMu.Unlock()
	}
}

// channelTiming returns channel's transmit timing as it stands, all of it
// under the one lock, so that a client application changing it part way
// through can't hand back a mixture that never coexisted.
func (xs *XmitService) channelTiming(channel int) xmitTiming {
	xs.timingMu.Lock()
	defer xs.timingMu.Unlock()

	return xs.timing[channel]
}

// layer2Sender is channel's Layer2Sender, made on first use.  Only the channel's
// own xmit_thread asks for it, so there is nothing to lock.
func (xs *XmitService) layer2Sender(channel int) *Layer2Sender {
	if xs.layer2Senders[channel] == nil {
		xs.layer2Senders[channel] = NewLayer2Sender(channel, xs.p_modem, xs.toneGenerators[channel], xs.fx25Debug, xs.il2pDebug)
	}

	return xs.layer2Senders[channel]
}

/*-------------------------------------------------------------------
 *
 * Name:        frame_flavor
 *
 * Purpose:     Separate frames into different flavors so we can decide
 *		which can be bundled into a single transmission and which should
 *		be sent separately.
 *
 * Inputs:	pp	- Packet object.
 *
 * Returns:	Flavor, one of:
 *
 *		FLAVOR_SPEECH		- Destination address is SPEECH.
 *		FLAVOR_MORSE		- Destination address is MORSE.
 *		FLAVOR_DTMF		- Destination address is DTMF.
 *		FLAVOR_APRS_NEW		- APRS original, i.e. not digipeating.
 *		FLAVOR_APRS_DIGI	- APRS digipeating.
 *		FLAVOR_OTHER		- Anything left over, i.e. connected mode.
 *
 *--------------------------------------------------------------------*/

type flavor_t int

const (
	FLAVOR_APRS_NEW flavor_t = iota
	FLAVOR_APRS_DIGI
	FLAVOR_SPEECH
	FLAVOR_MORSE
	FLAVOR_DTMF
	FLAVOR_OTHER
)

func frame_flavor(pp *ax25.Packet) flavor_t {
	if pp.IsAPRS() { // UI frame, PID 0xF0.
		// It's unfortunate APRS did not use its own special PID.
		var dest = pp.AddrNoSSID(ax25.Destination)

		if dest == "SPEECH" {
			return (FLAVOR_SPEECH)
		}

		if dest == "MORSE" {
			return (FLAVOR_MORSE)
		}

		if dest == "DTMF" {
			return (FLAVOR_DTMF)
		}

		/* Is there at least one digipeater AND has first one been used? */
		/* I could be the first in the list or later.  Doesn't matter. */

		if pp.NumRepeaters() >= 1 && pp.H(ax25.Repeater1) > 0 {
			return (FLAVOR_APRS_DIGI)
		}

		return (FLAVOR_APRS_NEW)
	}

	return (FLAVOR_OTHER)
} /* end frame_flavor */

/*-------------------------------------------------------------------
 *
 * Name:        xmit_thread
 *
 * Purpose:     Process transmit queue for one channel.
 *
 * Inputs:	transmit packet queue.
 *
 * Outputs:
 *
 * Description:	We have different timing rules for different types of
 *		packets so they are put into different queues.
 *
 *		High Priority -
 *
 *			Packets which are being digipeated go out first.
 *			Latest recommendations are to retransmit these
 *			immdediately (after no one else is heard, of course)
 *			rather than waiting random times to avoid collisions.
 *			The KPC-3 configuration option for this is "UIDWAIT OFF".  (?)
 *
 *			AX.25 connected mode also has a couple cases
 *			where "expedited" frames are sent.
 *
 *		Low Priority -
 *
 *			Other packets are sent after a random wait time
 *			(determined by PERSIST & SLOTTIME) to help avoid
 *			collisions.
 *
 *		If more than one audio channel is being used, a separate
 *		pair of transmit queues is used for each channel.
 *
 *
 *
 * Version 1.2:	Allow more than one audio device.
 * 		each channel has its own thread.
 *		Add speech capability.
 *
 * Version 1.4:	Rearranged logic for bundling multiple frames into a single transmission.
 *
 *		The rule is that Speech, Morse Code, DTMF, and APRS digipeated frames
 *		are all sent separately.  The rest can be bundled.
 *
 *--------------------------------------------------------------------*/

// xmit_thread runs until ctx is cancelled.  A transmission already under way
// when that happens is finished: dropping PTT part way through a frame would
// be worse than a slightly later exit.  Frames still waiting for a clear
// channel are left on the queue.
func (xs *XmitService) xmit_thread(ctx context.Context, channel int) {
	for ctx.Err() == nil {
		transmitQueue.WaitWhileEmpty(ctx, channel)
		logrus.WithField("channel", channel).Debug("xmit_thread: woke up")

		// Does this extra loop offer any benefit?
		xs.xmit_until_empty(ctx, channel)
	} /* until cancelled */
} /* end xmit_thread */

// xmit_until_empty sends everything queued for a channel, or throws it away if
// the channel has no transmit device.
//
// It is a function of its own, rather than the body of the loop in
// xmit_thread, so that the decision between the two can be tested: xmit_thread
// itself never returns.
func (xs *XmitService) xmit_until_empty(ctx context.Context, channel int) {
	for transmitQueue.Peek(channel, TQ_PRIO_0_HI) != nil || transmitQueue.Peek(channel, TQ_PRIO_1_LO) != nil {
		// xmit_next leaves the queue alone once ctx is cancelled, so without
		// this we would go straight round again, and forever.
		if ctx.Err() != nil {
			return
		}

		if !xs.audioOutAvailable[ACHAN2ADEV(channel)] {
			xs.discard_untransmittable(channel)

			continue
		}

		xs.xmit_next(ctx, channel)
	} /* while queue not empty */
}

// discard_untransmittable throws away everything queued for a channel whose
// audio device has no output.  Sending it would key PTT - putting an
// unmodulated carrier on the air, and muting the receiver for the duration of
// a half-duplex transmission - to play samples that go nowhere, so a station
// with no transmit device does not transmit at all.
//
// The null frame TransmitQueue.LMSeizeRequest queues is not a frame to send but a request
// for a transmission opportunity, so it is answered here exactly as
// send_one_frame answers it.  A connected mode session whose acknowledgement
// can never go out still has to hear that its turn came and take its normal
// course; silently dropping the request leaves it waiting for a confirmation
// that is never coming.
func (xs *XmitService) discard_untransmittable(channel int) {
	if !xs.saidCannotTransmit[channel] {
		xs.saidCannotTransmit[channel] = true

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Channel %d has no audio output device, so nothing can be transmitted on it.\n", channel)
		dw_printf("Frames queued for it are discarded, and this is said only once.\n")
	}

	var confirmed = false

	for _, prio := range []int{TQ_PRIO_0_HI, TQ_PRIO_1_LO} {
		for {
			var pp = transmitQueue.Remove(channel, prio)
			if pp == nil {
				break
			}

			if pp.IsNullFrame() {
				dataLinkQueue.SeizeConfirm(channel) // C4.2.  "This primitive indicates, to the
				// Data-link State machine, that the transmission opportunity has arrived."

				confirmed = true
			}
		}
	}

	if confirmed {
		time.Sleep(10 * time.Millisecond) // As send_one_frame does after the same confirmation: give the
		// data link state machine time to queue its response, so the caller sees it
		// on its next look at the queue rather than going back to sleep first.
	}
}

// xmit_next waits for a clear channel and then sends the next packet from the
// transmit queue, if one is still there.
//
// It is a function of its own, rather than the body of the loop in
// xmit_thread, so that the audio output device lock taken by
// wait_for_clear_channel can be released with defer: the release then happens
// however we return, including on the paths where there turns out to be
// nothing to send.  A defer in xmit_thread itself would not do, as that never
// returns, so the lock would be held for the life of the process.
func (xs *XmitService) xmit_next(ctx context.Context, channel int) {
	/*
	 * Wait for the channel to be clear.
	 * If there is something in the high priority queue, begin transmitting immediately.
	 * Otherwise, wait a random amount of time, in hopes of minimizing collisions.
	 */
	var timing = xs.channelTiming(channel)
	var ok = xs.wait_for_clear_channel(ctx, channel, timing.slottime, timing.persist, timing.fulldup)

	if ok {
		// Corresponding lock is in wait_for_clear_channel.  Releasing it with
		// defer, rather than at the end of the transmit path below, means it
		// is released however we leave this function - notably when the
		// packet we were about to send has disappeared from the queue.
		defer xs.audio.outputMu[ACHAN2ADEV(channel)].Unlock()
	} else if ctx.Err() != nil {
		// Not a timeout: we are being shut down, so leave the queue as it is
		// rather than discarding a packet as though the channel were busy.
		return
	}

	var prio = TQ_PRIO_1_LO

	var pp = transmitQueue.Remove(channel, TQ_PRIO_0_HI)
	if pp != nil {
		prio = TQ_PRIO_0_HI
	} else {
		pp = transmitQueue.Remove(channel, TQ_PRIO_1_LO)
	}

	logrus.WithFields(logrus.Fields{
		"channel": channel,
		"prio":    prio,
		"pp":      pp,
	}).Debug("xmit_thread: tq_remove returned")
	// Shouldn't have nil here but be careful.

	if pp != nil {
		if ok {
			/*
			 * Channel is clear and we have lock on output device.
			 *
			 * If destination is "SPEECH" send info part to speech synthesizer.
			 * If destination is "MORSE" send as morse code.
			 * If destination is "DTMF" send as Touch Tones.
			 */
			switch frame_flavor(pp) {
			case FLAVOR_SPEECH:
				xs.xmit_speech(ctx, channel, pp)

			case FLAVOR_MORSE:
				var ssid = pp.SSID(ax25.Destination)

				var wpm = MORSE_DEFAULT_WPM
				if ssid > 0 {
					wpm = ssid * 2
				}

				// This is a bit of a hack so we don't respond too quickly for APRStt.
				// It will be sent in high priority queue while a beacon wouldn't.
				// Add a little delay so user has time release PTT after sending #.
				// This and default txdelay would give us a second.

				if prio == TQ_PRIO_0_HI {
					//text_color_set(DW_COLOR_DEBUG);
					//dw_printf ("APRStt morse xmit delay hack...\n");
					// Not dwutil.SleepCtx: pp is already off the queue, so giving up
					// here would lose it rather than leave it for later.
					time.Sleep(700 * time.Millisecond)
				}

				xs.xmit_morse(channel, pp, wpm)

			case FLAVOR_DTMF:
				var speed = pp.SSID(ax25.Destination)
				if speed == 0 {
					speed = 5 // default half of maximum
				}

				if speed > 10 {
					speed = 10
				}

				xs.xmit_dtmf(channel, pp, speed)

			case FLAVOR_APRS_DIGI:
				xs.xmit_ax25_frames(channel, prio, pp, 1) /* 1 means don't bundle */
				// I don't know if this in some official specification
				// somewhere, but it is generally agreed that APRS digipeaters
				// should send only one frame at a time rather than
				// bundling multiple frames into a single transmission.
				// Discussion here:  http://lists.tapr.org/pipermail/aprssig_lists.tapr.org/2021-September/049034.html

			default:
				xs.xmit_ax25_frames(channel, prio, pp, 256)
			}
		} else {
			/*
			 * Timeout waiting for clear channel.
			 * Discard the packet.
			 * Display with ERROR color rather than XMIT color.
			 */
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Waited too long for clear channel.  Discarding packet below.\n")

			var stemp = pp.FormatAddrs()

			var pinfo = pp.Info()

			text_color_set(DW_COLOR_INFO)
			dw_printf("[%d%c] ", channel, priorityToRune(prio))

			dw_printf("%s", stemp) /* stations followed by : */
			ax25.SafePrint(pinfo, !pp.IsAPRS())
			dw_printf("\n")
		} /* wait for clear channel error. */
	} /* Have pp */
} /* end xmit_next */

func priorityToRune(prio int) rune {
	if prio == TQ_PRIO_0_HI {
		return 'H'
	} else {
		return 'L'
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        xmit_ax25_frames
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit one or more frames.
 *
 * Inputs:	chan	- Channel number.
 *
 *		prio	- Priority of the first frame.
 *			  Subsequent frames could be different.
 *
 *		pp	- Packet object pointer.
 *			  Ownership is transferred here so caller should
 *			  not reference it after this.
 *
 *		max_bundle - Max number of frames to bundle into one transmission.
 *
 * Description:	Turn on transmitter.
 *		Send flags for TXDELAY time.
 *		Send the first packet, given by pp.
 *		Possibly send more packets from either queue.
 *		Send flags for TXTAIL time.
 *		Turn off transmitter.
 *
 *
 * How many frames in one transmission?  (for APRS)
 *
 *		Should we send multiple frames in one transmission if we
 *		have more than one sitting in the queue?  At first I was thinking
 *		this would help reduce channel congestion.  I don't recall seeing
 *		anything in the APRS specifications allowing or disallowing multiple
 *		frames in one transmission.  I can think of some scenarios
 *		where it might help.  I can think of some where it would
 *		definitely be counter productive.
 *
 * What to others have to say about this topic?
 *
 *	"For what it is worth, the original APRSdos used a several second random
 *	generator each time any kind of packet was generated... This is to avoid
 *	bundling. Because bundling, though good for connected packet, is not good
 *	on APRS. Sometimes the digi begins digipeating the first packet in the
 *	bundle and steps all over the remainder of them. So best to make sure each
 *	packet is isolated in time from others..."
 *
 *		Bob, WB4APR
 *
 *
 * Version 0.9:	Earlier versions always sent one frame per transmission.
 *		This was fine for APRS but more and more people are now
 *		using this as a KISS TNC for connected protocols.
 *		Rather than having a configuration file item,
 *		we try setting the maximum number automatically.
 *		1 for digipeated frames, 7 for others.
 *
 * Version 1.4: Lift the limit.  We could theoretically have a window size up to 127.
 *		If another section pumps out that many quickly we shouldn't
 *		break it up here.  Empty out both queues with some exceptions.
 *
 *		Digipeated APRS, Speech, and Morse code should have
 *		their own separate transmissions.
 *		Everything else can be bundled together.
 *		Different priorities can share a single transmission.
 *		Once we have control of the channel, we might as well keep going.
 *		[High] Priority frames will always go to head of the line,
 *
 * Version 1.5:	Add full duplex option.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_ax25_frames(channel int, prio int, pp *ax25.Packet, max_bundle int) {
	/*
	 * These are for timing of a transmission.
	 * All are in usual unix time (seconds since 1/1/1970) but higher resolution
	 */
	var time_ptt = time.Now()

	/*
	 * Turn on transmitter.
	 * Start sending leading flag bytes.
	 */

	// TODO: This was written assuming bits/sec = baud.
	// Does it is need to be scaled differently for PSK?

	logrus.WithFields(logrus.Fields{
		"t":       time.Since(time_ptt),
		"channel": channel,
		"speed":   xs.bits_per_sec[channel],
	}).Debug("xmit_thread: Turn on PTT now")
	pttControl.Set(OCTYPE_PTT, channel, 1)

	// Inform data link state machine that we are now transmitting.

	dataLinkQueue.SeizeConfirm(channel) // C4.2.  "This primitive indicates, to the Data-link State
	// machine, that the transmission opportunity has arrived."

	var timing = xs.channelTiming(channel)

	var pre_flags = xs.msToBits(timing.txdelay*10, channel) / 8

	/* Total number of bits in transmission including all flags and bit stuffing. */
	var num_bits = xs.layer2Sender(channel).SendPreamblePostamble(pre_flags, false)

	logrus.WithFields(logrus.Fields{
		"t":         time.Since(time_ptt),
		"txdelay":   timing.txdelay,
		"pre_flags": pre_flags,
		"num_bits":  num_bits,
	}).Debug("xmit_thread: preamble")

	var presleep = time.Now()

	time.Sleep(10 * time.Millisecond) // Give data link state machine a chance to
	// to stuff more frames into the transmit queue,
	// in response to dataLinkQueue.SeizeConfirm, so
	// we don't run off the end too soon.

	logrus.WithFields(logrus.Fields{
		"t":       time.Since(time_ptt),
		"naptime": time.Since(presleep),
	}).Debug("xmit_thread: should be 0.010 second after the above")

	var numframe = 0 /* Number of frames sent during this transmission. */

	/*
	 * Transmit the frame.
	 */

	var nb = xs.send_one_frame(channel, prio, pp)

	num_bits += nb
	if nb > 0 {
		numframe++
	}
	logrus.WithFields(logrus.Fields{
		"t":        time.Since(time_ptt),
		"nb":       nb,
		"num_bits": num_bits,
		"numframe": numframe,
	}).Debug("xmit_thread: frame sent")

	/*
	 * See if we can bundle additional frames into this transmission.
	 */

	var done = false
	for numframe < max_bundle && !done {
		/*
		 * Peek at what is available.
		 * Don't remove from queue yet because it might not be eligible.
		 */
		prio = TQ_PRIO_1_LO

		pp = transmitQueue.Peek(channel, TQ_PRIO_0_HI)
		if pp != nil {
			prio = TQ_PRIO_0_HI
		} else {
			pp = transmitQueue.Peek(channel, TQ_PRIO_1_LO)
		}

		if pp != nil {
			switch frame_flavor(pp) {
			default:
				done = true // not eligible for bundling.

			case FLAVOR_APRS_NEW, FLAVOR_OTHER:
				pp = transmitQueue.Remove(channel, prio)
				logrus.WithFields(logrus.Fields{
					"t":       time.Since(time_ptt),
					"channel": channel,
					"prio":    prio,
					"pp":      pp,
				}).Debug("xmit_thread: tq_remove returned")

				nb = xs.send_one_frame(channel, prio, pp)

				num_bits += nb
				if nb > 0 {
					numframe++
				}
				logrus.WithFields(logrus.Fields{
					"t":        time.Since(time_ptt),
					"nb":       nb,
					"num_bits": num_bits,
					"numframe": numframe,
				}).Debug("xmit_thread: bundled frame sent")
			}
		} else {
			done = true
		}
	}

	/*
	 * Need TXTAIL because we don't know exactly when the sound is done.
	 */

	var post_flags = xs.msToBits(timing.txtail*10, channel) / 8
	nb = xs.layer2Sender(channel).SendPreamblePostamble(post_flags, true)
	num_bits += nb
	logrus.WithFields(logrus.Fields{
		"t":          time.Since(time_ptt),
		"txtail":     timing.txtail,
		"post_flags": post_flags,
		"nb":         nb,
		"num_bits":   num_bits,
	}).Debug("xmit_thread: postamble")

	/*
	 * While demodulating is CPU intensive, generating the tones is not.
	 * Example: on the RPi model 1, with 50% of the CPU taken with two receive
	 * channels, a transmission of more than a second is generated in
	 * about 40 mS of elapsed real time.
	 */

	xs.audio.wait(ACHAN2ADEV(channel))

	/*
	 * Ideally we should be here just about the time when the audio is ending.
	 * However, the innards of "wait" are not satisfactory in all cases.
	 *
	 * Calculate how long the frame(s) should take in milliseconds.
	 */

	var durationMS = xs.bitsToMS(num_bits, channel)

	/*
	 * See how long it has been since PTT was turned on.
	 * Wait additional time if necessary.
	 */

	var already = time.Since(time_ptt)
	var wait_more = time.Duration(durationMS)*time.Millisecond - already

	logrus.WithFields(logrus.Fields{
		"t":           time.Since(time_ptt),
		"duration_ms": durationMS,
		"already":     already,
		"wait_more":   wait_more,
	}).Debug("xmit_thread: transmission duration")

	if wait_more > 0 {
		time.Sleep(wait_more)
	} else if wait_more < -100*time.Millisecond {
		/* If we run over by 10 mSec or so, it's nothing to worry about. */
		/* However, if PTT is still on about 1/10 sec after audio */
		/* should be done, something is wrong. */

		/* Looks like a bug with the RPi audio system. Never an issue with Ubuntu.  */
		/* This runs over randomly sometimes. TODO:  investigate more fully sometime. */
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Transmit timing error: PTT is on %d mSec too long.\n", -wait_more.Milliseconds())
	}

	/*
	 * Turn off transmitter.
	 */
	logrus.WithFields(logrus.Fields{
		"t":           time.Since(time_ptt),
		"duration_ms": durationMS,
	}).Debug("xmit_thread: Turn off PTT now")

	pttControl.Set(OCTYPE_PTT, channel, 0)
} /* end xmit_ax25_frames */

/*-------------------------------------------------------------------
 *
 * Name:        send_one_frame
 *
 * Purpose:     Send one AX.25 frame.
 *
 * Inputs:	c	- Channel number.
 *
 *		p	- Priority.
 *
 *		pp	- Packet object pointer.  Caller retains ownership of it.
 *
 * Returns:	Number of bits transmitted.
 *
 * Description:	Caller is responsible for activiating PTT, TXDELAY,
 *		deciding how many frames can be in one transmission,
 *		deactivating PTT.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) send_one_frame(c int, p int, pp *ax25.Packet) int {
	if pp.IsNullFrame() {
		// Issue 132 - We could end up in a situation where:
		// Transmitter is already on.
		// Application wants to send a frame.
		// dl_seize_request turns into this null frame.
		// It was being ignored here so the data got stuck in the queue.
		// I think the solution is to send back a seize confirm here.
		// It shouldn't hurt if we send it redundantly.
		// Added for 1.5 beta test 4.
		dataLinkQueue.SeizeConfirm(c) // C4.2.  "This primitive indicates, to the Data-link State
		// machine, that the transmission opportunity has arrived."

		time.Sleep(10 * time.Millisecond) // Give data link state machine a chance to
		// to stuff more frames into the transmit queue,
		// in response to dataLinkQueue.SeizeConfirm, so
		// we don't run off the end too soon.

		return (0)
	}

	var ts = xs.timestampPrefix()

	var stemp = pp.FormatAddrs()

	var pinfo = pp.Info()

	text_color_set(DW_COLOR_XMIT)
	/*
		#if 0						// FIXME - enable this?
			dw_printf ("[%d%c%s%s] ", c,
					p==TQ_PRIO_0_HI ? 'H' : 'L',
					xs.p_modem.achan[c].fx25_strength ? "F" : "",
					ts);
		#else
	*/
	dw_printf("[%d%c%s] ", c, priorityToRune(p), ts)
	/* #endif */
	dw_printf("%s", stemp) /* stations followed by : */

	/* Demystify non-APRS.  Use same format for received frames in direwolf.c. */

	if !pp.IsAPRS() {
		var _, desc, _, _, _, ftype = pp.FrameType()

		dw_printf("(%s)", desc)

		if ftype == ax25.FrameTypeUXID {
			var _, info2text, _ = xid_parse(pinfo)
			dw_printf(" %s\n", info2text)
		} else {
			ax25.SafePrint(pinfo, !pp.IsAPRS())
			dw_printf("\n")
		}
	} else {
		ax25.SafePrint(pinfo, !pp.IsAPRS())
		dw_printf("\n")
	}

	pp.CheckAddresses(ax25.AddrStrict)

	/* Optional hex dump of packet. */

	if xs.debugXmitPacket {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		pp.HexDump()
		dw_printf("------\n")
	}

	/*
	 * Transmit the frame.
	 */
	var send_invalid_fcs2 = false

	if xs.p_modem.xmit_error_rate != 0 {
		// https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/math/rand/rand.go;l=189
		// rand.Float64 excludes 1.0 so let's just use the internal implementation
		var r = float64(rand.Int63n(1<<53)) / (1 << 53) // Random, 0.0 to 1.0

		if float64(xs.p_modem.xmit_error_rate)/100.0 > r {
			send_invalid_fcs2 = true

			text_color_set(DW_COLOR_INFO)
			dw_printf("Intentionally sending invalid CRC for frame above.  Xmit Error rate = %d per cent.\n", xs.p_modem.xmit_error_rate)
		}
	}

	var nb = xs.layer2Sender(c).SendFrame(pp, send_invalid_fcs2)

	metrics.RecordFrameTransmitted(c)

	if xs.onTransmit != nil {
		xs.onTransmit(c, pp)
	}

	// Optionally send confirmation to AGW client app if monitoring enabled.

	agwServer.SendMonitored(c, pp, 1)

	return nb
} /* end send_one_frame */

/*-------------------------------------------------------------------
 *
 * Name:        xmit_speech
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit information part of frame as speech.
 *
 * Inputs:	c	- Channel number.
 *
 *		pp	- Packet object pointer.
 *			  Ownership is transferred here so caller should
 *			  not reference it after this.
 *
 * Description:	Turn on transmitter.
 *		Invoke the text-to-speech script.
 *		Turn off transmitter.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_speech(ctx context.Context, c int, pp *ax25.Packet) {
	/*
	 * Print spoken packet.  Prefix by channel.
	 */
	var ts = xs.timestampPrefix()

	var pinfo = pp.Info()

	text_color_set(DW_COLOR_XMIT)
	dw_printf("[%d.speech%s] \"%s\"\n", c, ts, string(pinfo))

	if xs.p_modem.tts_script == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Text-to-speech script has not been configured.\n")

		return
	}

	/*
	 * Turn on transmitter.
	 */
	pttControl.Set(OCTYPE_PTT, c, 1)

	/*
	 * Invoke the speech-to-text script.
	 */

	xmit_speak_it(ctx, xs.p_modem.tts_script, c, string(pinfo))

	/*
	 * Turn off transmitter.
	 */

	pttControl.Set(OCTYPE_PTT, c, 0)
} /* end xmit_speech */

/* Broken out into separate function so configuration can validate it. */

func xmit_speak_it(ctx context.Context, script string, c int, msg string) error {
	// Deliberately not cancelled with ctx.  xmit_thread finishes sending what
	// is already queued when it is cancelled, and a SPEECH frame in that
	// drain has already keyed PTT by the time we get here: a script killed
	// before it starts would put an unmodulated carrier on the air instead of
	// the announcement.  The caller's context still bounds how long the rest
	// of the application waits.
	var speakCtx = context.WithoutCancel(ctx)

	var cmd = exec.CommandContext(speakCtx, script, strconv.Itoa(c), msg) //nolint:gosec // Trust the user-supplied config

	var err = cmd.Run()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Failed to run text-to-speech script, %s\n", script)

		var cwd, _ = os.Getwd()
		dw_printf("CWD = %s\n", cwd)

		dw_printf("PATH = %s\n", os.Getenv("PATH"))
	}

	return (err)
}

func (xs *XmitService) timestampPrefix() string {
	if xs.p_modem.timestamp_format != "" {
		var formattedTime, _ = strftime.Format(xs.p_modem.timestamp_format, time.Now())

		return " " + formattedTime // space after channel.
	}

	return ""
}

/*-------------------------------------------------------------------
 *
 * Name:        xmit_morse
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit information part of frame as Morse code.
 *
 * Inputs:	c	- Channel number.
 *
 *		pp	- Packet object pointer.
 *			  Ownership is transferred here so caller should
 *			  not reference it after this.
 *
 *		wpm	- Speed in words per minute.
 *
 * Description:	Turn on transmitter.
 *		Send text as Morse code.
 *		A small amount of quiet padding will appear at start and end.
 *		Turn off transmitter.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_morse(c int, pp *ax25.Packet, wpm int) {
	var ts = xs.timestampPrefix()

	var pinfo = pp.Info()

	text_color_set(DW_COLOR_XMIT)
	dw_printf("[%d.morse%s] \"%s\"\n", c, ts, string(pinfo))

	pttControl.Set(OCTYPE_PTT, c, 1)
	var start_ptt = time.Now()

	// make txdelay at least 300 and txtail at least 250 ms.

	var timing = xs.channelTiming(c)
	var _length_ms = morse_send(xs.toneGenerators[c], c, string(pinfo), wpm, max(timing.txdelay*10, 300), max(timing.txtail*10, 250))
	var waitDuration = time.Duration(_length_ms) * time.Millisecond

	// there is probably still sound queued up in the output buffers.

	var wait_until = start_ptt.Add(waitDuration)

	var timeToWait = time.Until(wait_until)
	if timeToWait.Milliseconds() > 0 {
		time.Sleep(timeToWait)
	}

	pttControl.Set(OCTYPE_PTT, c, 0)
} /* end xmit_morse */

/*-------------------------------------------------------------------
 *
 * Name:        xmit_dtmf
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit information part of frame as DTMF tones.
 *
 * Inputs:	c	- Channel number.
 *
 *		pp	- Packet object pointer.
 *			  Ownership is transferred here so caller should
 *			  not reference it after this.
 *
 *		speed	- Button presses per second.
 *
 * Description:	Turn on transmitter.
 *		Send text as touch tones.
 *		A small amount of quiet padding will appear at start and end.
 *		Turn off transmitter.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_dtmf(c int, pp *ax25.Packet, speed int) {
	var ts = xs.timestampPrefix()

	var pinfo = pp.Info()

	text_color_set(DW_COLOR_XMIT)
	dw_printf("[%d.dtmf%s] \"%s\"\n", c, ts, string(pinfo))

	pttControl.Set(OCTYPE_PTT, c, 1)
	var start_ptt = time.Now()

	// make txdelay at least 300 and txtail at least 250 ms.

	var timing = xs.channelTiming(c)
	var _length_ms = dtmf_send(xs.toneGenerators[c], c, string(pinfo), speed, max(timing.txdelay*10, 300), max(timing.txtail*10, 250))
	var waitDuration = time.Duration(_length_ms) * time.Millisecond

	// there is probably still sound queued up in the output buffers.

	var wait_until = start_ptt.Add(waitDuration)

	var timeToWait = time.Until(wait_until)
	if timeToWait.Milliseconds() > 0 {
		time.Sleep(timeToWait)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Oops.  CPU too slow to keep up with DTMF generation.\n")
	}

	pttControl.Set(OCTYPE_PTT, c, 0)
} /* end xmit_dtmf */

/*-------------------------------------------------------------------
 *
 * Name:        wait_for_clear_channel
 *
 * Purpose:     Wait for the radio channel to be clear and any
 *		additional time for collision avoidance.
 *
 * Inputs:	chan	-	Radio channel number.
 *
 *		slottime - 	Amount of time to wait for each iteration
 *				of the waiting algorithm.  10 mSec units.
 *
 *		persist -	Probability of transmitting.
 *
 *		fulldup -	Full duplex.  Just start sending immediately.
 *
 * Returns:	True for OK.  False for timeout, or if ctx is cancelled.
 *
 * Description:	New in version 1.2: also obtain a lock on audio out device.
 *
 *		New in version 1.5: full duplex.
 *		Just start transmitting rather than waiting for clear channel.
 *		This would only be appropriate when transmit and receive are
 *		using different radio frequencies.  e.g.  VHF up, UHF down satellite.
 *
 * Transmit delay algorithm:
 *
 *		Wait for channel to be clear.
 *		If anything in high priority queue, bail out of the following.
 *
 *		Wait slottime * 10 milliseconds.
 *		Generate an 8 bit random number in range of 0 - 255.
 *		If random number <= persist value, return.
 *		Otherwise repeat.
 *
 * Example:
 *
 *		For typical values of slottime=10 and persist=63,
 *
 *		Delay		Probability
 *		-----		-----------
 *		100		.25					= 25%
 *		200		.75 * .25				= 19%
 *		300		.75 * .75 * .25				= 14%
 *		400		.75 * .75 * .75 * .25			= 11%
 *		500		.75 * .75 * .75 * .75 * .25		= 8%
 *		600		.75 * .75 * .75 * .75 * .75 * .25	= 6%
 *		700		.75 * .75 * .75 * .75 * .75 * .75 * .25	= 4%
 *		etc.		...
 *
 *--------------------------------------------------------------------*/

/* Give up if we can't get a clear channel in a minute. */
/* That's a long time to wait for APRS. */
/* Might need to revisit some day for connected mode file transfers. */

const WAIT_TIMEOUT_MS = 60 * 1000
const WAIT_CHECK_EVERY_MS = 10

func (xs *XmitService) wait_for_clear_channel(ctx context.Context, channel int, slottime int, persist int, fulldup bool) bool {
	/*
	 * For full duplex we skip the channel busy check and random wait.
	 * We still need to wait if operating in stereo and the other audio
	 * half is busy.
	 */
	var n = 0

	if !fulldup {
	start_over_again:

		for layer2Receiver.DataDetectAny(channel) > 0 {
			if !dwutil.SleepCtx(ctx, WAIT_CHECK_EVERY_MS*time.Millisecond) {
				return false
			}

			n++
			if n > (WAIT_TIMEOUT_MS / WAIT_CHECK_EVERY_MS) {
				return false
			}
		}

		//TODO:  rethink dwait.

		/*
		 * Added in version 1.2 - for transceivers that can't
		 * turn around fast enough when using squelch and VOX.
		 */

		if xs.p_modem.achan[channel].dwait > 0 {
			if !dwutil.SleepCtx(ctx, time.Duration(xs.p_modem.achan[channel].dwait)*10*time.Millisecond) {
				return false
			}
		}

		if layer2Receiver.DataDetectAny(channel) > 0 {
			goto start_over_again
		}

		/*
		 * Wait random time.
		 * Proceed to transmit sooner if anything shows up in high priority queue.
		 */
		for transmitQueue.Peek(channel, TQ_PRIO_0_HI) == nil {
			if !dwutil.SleepCtx(ctx, time.Duration(slottime)*10*time.Millisecond) {
				return false
			}

			if layer2Receiver.DataDetectAny(channel) > 0 {
				goto start_over_again
			}

			var r = rand.Int() & 0xff
			if r <= persist {
				break
			}
		}
	}

	/*
	 * This is to prevent two channels from transmitting at the same time
	 * thru a stereo audio device.
	 * We are not clever enough to combine two audio streams.
	 * They must go out one at a time.
	 * Documentation recommends using separate audio device for each channel rather than stereo.
	 * That also allows better use of multiple cores for receiving.
	 */

	// TODO: review this.

	for !xs.audio.outputMu[ACHAN2ADEV(channel)].TryLock() {
		if !dwutil.SleepCtx(ctx, WAIT_CHECK_EVERY_MS*time.Millisecond) {
			return false
		}

		n++
		if n > (WAIT_TIMEOUT_MS / WAIT_CHECK_EVERY_MS) {
			return false
		}
	}

	return true
} /* end wait_for_clear_channel */

func (xs *XmitService) bitsToMS(b, ch int) int {
	return b * 1000 / xs.bits_per_sec[ch]
}

func (xs *XmitService) msToBits(ms, ch int) int {
	return ms * xs.bits_per_sec[ch] / 1000
}

// Layer2Sender turns frames into the bits one radio channel sends, with
// whichever of HDLC, FX.25 and IL2P the channel is set to use.  The three
// share the channel's line, whose NRZI level carries over from one frame to
// the next.  Each channel wants its own, and only one goroutine may drive it
// at a time.
type Layer2Sender struct {
	channel       int
	audioConfig   *RadioConfig
	toneGenerator *ToneGenerator // Where the bits go; nil for a channel with no radio.

	line *linecode.Encoder // Puts the bits on the line, keeping its NRZI level.

	hdlc *hdlc.Sender // Sends AX.25 frames, and the flags between them.
	fx25 *fx25.Sender // Sends FX.25, on the same line.
	il2p *il2p.Sender // Sends IL2P, on the same line.
	eas  *eas.Sender  // Sends EAS SAME, on the same line.
}

// NewLayer2Sender makes a Layer2Sender for channel, sending the layer 2
// protocol audioConfig says to use there to toneGenerator, with FX.25's
// debug level at fx25Debug and IL2P's at il2pDebug.
func NewLayer2Sender(channel int, audioConfig *RadioConfig, toneGenerator *ToneGenerator, fx25Debug int, il2pDebug int) *Layer2Sender {
	var s = new(Layer2Sender)
	s.channel = channel
	s.audioConfig = audioConfig
	s.toneGenerator = toneGenerator
	s.line = linecode.NewEncoder(s.putBit)
	s.hdlc = hdlc.NewSender(s.line, channel)
	s.fx25 = fx25.NewSender(s.line, channel, fx25Debug)
	s.il2p = il2p.NewSender(s.line, channel, il2pDebug)
	s.eas = eas.NewSender(s.line)

	return s
}

// putQuietMs sends timeMs of silence.
func (s *Layer2Sender) putQuietMs(timeMs int) {
	if s.toneGenerator == nil {
		logrus.WithField("channel", s.channel).Error("Invalid channel for tone generation")

		return
	}

	s.toneGenerator.PutQuietMs(timeMs)
}

// flush pushes out whatever the channel's samples are waiting in.
func (s *Layer2Sender) flush() {
	if s.toneGenerator == nil {
		logrus.WithField("channel", s.channel).Error("Invalid channel for tone generation")

		return
	}

	s.toneGenerator.Flush()
}

/*-------------------------------------------------------------
 *
 * Name:	SendFrame (layer2_send_frame in Dire Wolf)
 *
 * Purpose:	Convert frames to a stream of bits.
 *		Originally this was for AX.25 only, hence the file name.
 *		Over time, FX.25 and IL2P were shoehorned in.
 *
 * Inputs:	pp	- Packet object.
 *
 *		badFCS	- Append an invalid FCS for testing purposes.
 *			  Applies only to regular AX.25.
 *
 * Outputs:	Bits are shipped out to the sender's tone generator.
 *
 * Returns:	Number of bits sent including "flags" and the
 *		stuffing bits.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *
 * Description:	For AX.25, send:
 *			start flag
 *			bit stuffed data
 *			calculated FCS
 *			end flag
 *		NRZI encoding for all but the "flags."
 *
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		the tone generator are processed correctly.
 *
 *--------------------------------------------------------------*/

func (s *Layer2Sender) SendFrame(pp *ax25.Packet, badFCS bool) int {
	var achan = &s.audioConfig.achan[s.channel]

	if achan.layer2_xmit == LAYER2_IL2P { //nolint:staticcheck
		var n = s.il2p.SendFrame(pp, achan.il2p_version, achan.il2p_max_fec, achan.il2p_crc, achan.il2p_invert_polarity)
		if n > 0 {
			return n
		}

		logrus.WithField("channel", s.channel).Warn("Unable to send IL2P frame.  Falling back to regular AX.25.")
		// Not sure if we should fall back to AX.25 or not here.
	} else if achan.layer2_xmit == LAYER2_FX25 {
		var fbuf = pp.Pack()

		var n = s.fx25.SendFrame(fbuf, achan.fx25_strength)
		if n > 0 {
			return n
		}

		logrus.WithField("channel", s.channel).Warn("Unable to send FX.25.  Falling back to regular AX.25.")
		// Definitely need to fall back to AX.25 here because
		// the FX.25 frame length is so limited.
	}

	var fbuf = pp.Pack()

	return s.hdlc.SendFrame(fbuf, badFCS)
}

/*-------------------------------------------------------------
 *
 * Name:	SendPreamblePostamble (layer2_preamble_postamble in Dire Wolf)
 *
 * Purpose:	Send filler pattern before and after the frame.
 *		For HDLC it is 01111110, for IL2P 01010101.
 *
 * Inputs:	nbytes	- Number of bytes to send.
 *
 *		finish	- True for end of transmission.
 *			  This causes the last audio buffer to be flushed.
 *
 * Outputs:	Bits are shipped out to the sender's tone generator.
 *
 * Returns:	Number of bits sent.
 *		There is no bit-stuffing so we would expect this to
 *		be 8 * nbytes.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		the tone generator are processed correctly.
 *
 *--------------------------------------------------------------*/

func (s *Layer2Sender) SendPreamblePostamble(nbytes int, finish bool) int {
	logrus.WithFields(logrus.Fields{
		"channel": s.channel,
		"nbytes":  nbytes,
		"finish":  finish,
	}).Debug("layer2_preamble_postamble")

	// When the transmitter is on but not sending data, it should be sending
	// a stream of a filler pattern.
	// For AX.25, it is the 01111110 "flag" pattern with NRZI and no bit stuffing.
	// For IL2P, it is 01010101 without NRZI.

	var achan = &s.audioConfig.achan[s.channel]

	var sent int

	if achan.layer2_xmit == LAYER2_IL2P {
		sent = s.il2p.SendPreamble(nbytes, achan.il2p_invert_polarity)
	} else {
		sent = s.hdlc.SendFlags(nbytes)
	}

	/* Push out the final partial buffer! */

	if finish {
		s.flush()
	}

	return sent
}

/* end xmit.c */

/*-------------------------------------------------------------------
 *
 * Name:        sendEAS (eas_send in Dire Wolf)
 *
 * Purpose:    	Serialize EAS SAME for transmission.
 *
 * Inputs:	str	- Character string to send.
 *		repeat	- Number of times to repeat with 1 sec quiet between.
 *		txdelay	- Delay (ms) from PTT to first preamble bit.
 *		txtail	- Delay (ms) from last data bit to PTT off.
 *
 *
 * Returns:	Total number of milliseconds to activate PTT.
 *		This includes delays before the first character
 *		and after the last to avoid chopping off part of it.
 *
 * Description:	xmit_thread calls this instead of the usual hdlc_send
 *		when we have a special packet that means send EAS SAME
 *		code.
 *
 *--------------------------------------------------------------------*/

func (s *Layer2Sender) sendEAS(str []byte, repeat int, txdelay int, txtail int) int {
	var bytes_sent = 0
	const gap = 1000
	var gaps_sent = 0

	s.putQuietMs(txdelay)

	for r := range repeat {
		bytes_sent += s.eas.SendMessage(str) / 8

		if r < repeat-1 {
			s.putQuietMs(gap)

			gaps_sent++
		}
	}

	s.putQuietMs(txtail)

	s.flush()

	var elapsed = txdelay + int(float64(bytes_sent)*8*1.92) + (gaps_sent * gap) + txtail

	// dw_printf ("DEBUG:  EAS total time = %d ms\n", elapsed);

	return (elapsed)
} /* end sendEAS */
