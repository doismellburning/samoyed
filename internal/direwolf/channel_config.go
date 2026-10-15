package direwolf

import (
	"github.com/doismellburning/samoyed/internal/axudp"
	"github.com/doismellburning/samoyed/internal/digipeater"
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/ptt"
)

type medium_e = phy.Medium

const (
	MEDIUM_NONE     = phy.MediumNone
	MEDIUM_RADIO    = phy.MediumRadio
	MEDIUM_IGATE    = phy.MediumIGate
	MEDIUM_NETTNC   = phy.MediumNetTNC
	MediumSerialTNC = phy.MediumSerialTNC
	MEDIUM_AXUDP    = phy.MediumAXUDP
)

type sanity_t = phy.Sanity

const (
	SANITY_APRS = phy.SanityAPRS
	SANITY_AX25 = phy.SanityAX25
	SANITY_NONE = phy.SanityNone
)

type modem_t = phy.Modem

const (
	MODEM_AFSK     = phy.AFSK
	MODEM_BASEBAND = phy.Baseband
	MODEM_SCRAMBLE = phy.Scramble
	MODEM_QPSK     = phy.QPSK
	MODEM_8PSK     = phy.PSK8
	MODEM_OFF      = phy.Off
	MODEM_16_QAM   = phy.QAM16
	MODEM_64_QAM   = phy.QAM64
	MODEM_AIS      = phy.AIS
	MODEM_EAS      = phy.EAS
	MODEM_BPSK     = phy.BPSK
)

type layer2_t = phy.Layer2

const (
	LAYER2_AX25 = phy.Layer2AX25
	LAYER2_FX25 = phy.Layer2FX25
	LAYER2_IL2P = phy.Layer2IL2P
)

type v26_e int

const (
	V26_UNSPECIFIED v26_e = iota
	V26_A
	V26_B
)

const V26_DEFAULT = V26_B

type dtmf_decode_t int

const (
	DTMF_DECODE_OFF dtmf_decode_t = iota
	DTMF_DECODE_ON
)

/* Properties for each radio channel, common to receive and transmit. */
/* Can be different for each radio channel. */

type achan_param_s struct {

	// Currently, we have a fixed mapping from audio sources to channel.
	//
	//		ADEVICE		CHANNEL (mono)		(stereo)
	//		0		0			0, 1
	//		1		2			2, 3
	//		2		4			4, 5
	//
	// A future feauture might allow the user to specify a different audio source.
	// This would allow multiple modems (with associated channel) to share an audio source.
	// int audio_source;	// Default would be [0,1,2,3,4,5]

	// What else should be moved out of structure and enlarged when NETTNC is implemented.  ???

	modem_type modem_t

	/* Usual AFSK. */
	/* Baseband signal. Not used yet. */
	/* Scrambled http://www.amsat.org/amsat/articles/g3ruh/109/fig03.gif */
	/* Might try MFJ-2400 / CCITT v.26 / Bell 201 someday. */
	/* No modem.  Might want this for DTMF only channel. */

	layer2_xmit layer2_t

	// IL2P - New for version 1.7.
	// New layer 2 with FEC.  Much less overhead than FX.25 but no longer backward compatible.
	// Only applies to transmit.
	// Listening for FEC sync word should add negligible overhead so
	// we leave reception enabled all the time as we do with FX.25.
	// TODO:  FX.25 should probably be put here rather than global for all channels.

	fx25_strength int // Strength of FX.25 FEC.
	// 16, 23, 64 for specific number of parity symbols.
	// 1 for automatic selection based on frame size.

	il2p_max_fec int // 1 for max FEC length, 0 for automatic based on size.
	// Only consulted when speaking v0.4; v0.6 always uses max FEC.

	il2p_version il2p.Version // IL2P protocol version spoken on this channel.

	il2p_invert_polarity int // 1 means invert on transmit.  Receive handles either automatically.

	il2p_crc bool // true to append trailing CRC after IL2P frame.  Default true.

	v26_alternative v26_e

	// Original implementation used alternative A for 2400 bbps PSK.
	// Years later, we discover that MFJ-2400 used alternative B.
	// It's likely the others did too.  it also works a little better.
	// Default to MFJ compatible and print warning if user did not
	// pick one explicitly.

	dtmf_decode dtmf_decode_t

	/* Originally the DTMF ("Touch Tone") decoder was always */
	/* enabled because it took a negligible amount of CPU. */
	/* There were complaints about the false positives when */
	/* hearing other modulation schemes on HF SSB so now it */
	/* is enabled only when needed. */

	/* "On" will send special "t" packet to attached applications */
	/* and process as APRStt.  Someday we might want to separate */
	/* these but for now, we have a single off/on. */

	decimate int /* Reduce AFSK sample rate by this factor to */
	/* decrease computational requirements. */

	upsample int /* Upsample by this factor for G3RUH. */

	mark_freq  int /* Two tones for AFSK modulation, in Hz. */
	space_freq int /* Standard tones are 1200 and 2200 for 1200 baud. */

	baud int /* Data bits per second. */
	/* Standard rates are 1200 for VHF and 300 for HF. */
	/* This should really be called bits per second. */

	/* Next 3 come from config file or command line. */

	profiles string /* zero or more of ABC etc, optional + */

	num_freq int /* Number of different frequency pairs for decoders. */

	offset int /* Spacing between filter frequencies. */

	/* How many demodulators and slicers these come to is the Demodulator's. */

	/* These are for dealing with imperfect frames. */

	fix_bits BitFixLevel /* Level of effort to recover from */
	/* a bad FCS on the frame. */
	/* 0 = no effort */
	/* 1 = try fixing a single bit */
	/* 2... = more techniques... */

	sanity_test sanity_t /* Sanity test to apply when finding a good */
	/* CRC after making a change. */
	/* Must look like APRS, AX.25, or anything. */

	passall bool /* Allow thru even with bad CRC. */

	/* Additional properties for transmit. */

	/* Originally we had control outputs only for PTT. */
	/* In version 1.2, we generalize this to allow others such as DCD. */
	/* In version 1.4 we add CON for connected to another station. */
	/* Index following structure by one of these: */

	octrl [NUM_OCTYPES]ptt.OutputConfig

	ictrl [NUM_ICTYPES]ptt.InputConfig

	/* Transmit timing. */

	dwait int /* First wait extra time for receiver squelch. */
	/* Default 0 units of 10 mS each . */

	slottime int /* Slot time in 10 mS units for persistence algorithm. */
	/* Typical value is 10 meaning 100 milliseconds. */

	persist int /* Sets probability for transmitting after each */
	/* slot time delay.  Transmit if a random number */
	/* in range of 0 - 255 <= persist value.  */
	/* Otherwise wait another slot time and try again. */
	/* Default value is 63 for 25% probability. */

	txdelay int /* After turning on the transmitter, */
	/* send "flags" for txdelay * 10 mS. */
	/* Default value is 30 meaning 300 milliseconds. */

	txtail int /* Amount of time to keep transmitting after we */
	/* are done sending the data.  This is to avoid */
	/* dropping PTT too soon and chopping off the end */
	/* of the frame.  Again 10 mS units. */
	/* At this point, I'm thinking of 10 (= 100 mS) as the default */
	/* because we're not quite sure when the soundcard audio stops. */

	fulldup bool /* Full Duplex. */

}

// pttChannels is each radio channel's configuration, as the PTT needs it.
func (c *RadioConfig) pttChannels() [MAX_RADIO_CHANS]ptt.ChannelConfig {
	var channels [MAX_RADIO_CHANS]ptt.ChannelConfig

	for ch := range MAX_RADIO_CHANS {
		channels[ch] = ptt.ChannelConfig{
			Radio:      c.chan_medium[ch] == MEDIUM_RADIO,
			FullDuplex: c.achan[ch].fulldup,
			Outputs:    c.achan[ch].octrl,
			Inputs:     c.achan[ch].ictrl,
		}
	}

	return channels
}

// RadioConfig is the configuration of the radio channels: the sound devices
// behind them, each channel's modem, PTT and transmit timing, and what sits on
// each channel.  It was Dire Wolf's struct audio_s.
type RadioConfig struct {

	/* Previously we could handle only a single audio device. */
	/* In version 1.2, we generalize this to handle multiple devices. */
	/* This means we can now have more than 2 radio channels. */

	adev [MAX_ADEVS]adev_param_s

	/* Common to all channels. */

	tts_script string /* Script for text to speech. */

	statistics_interval int /* Number of seconds between the audio */
	/* statistics reports.  This is set by */
	/* the "-a" option.  0 to disable feature. */

	xmit_error_rate int /* For testing purposes, we can generate frames with an invalid CRC */
	/* to simulate corruption while going over the air. */
	/* This is the probability, in per cent, of randomly corrupting it. */
	/* Normally this is 0.  25 would mean corrupt it 25% of the time. */

	recv_error_rate int /* Similar but the % probability of dropping a received frame. */

	recv_ber float64 /* Receive Bit Error Rate (BER). */
	/* Probability of inverting a bit coming out of the modem. */

	fx25_auto_enable int /* Turn on FX.25 for current connected mode session */
	/* under poor conditions. */
	/* Set to 0 to disable feature. */
	/* I put it here, rather than with the rest of the link layer */
	/* parameters because it is really a part of the HDLC layer */
	/* and is part of the KISS TNC functionality rather than our data link layer. */
	/* Future: not used yet. */

	timestamp_format string /* -T option */
	/* Precede received & transmitted frames with timestamp. */
	/* Command line option uses "strftime" format string. */

	/* originally a "channel" was always connected to an internal modem. */
	/* In version 1.6, this is generalized so that a channel (as seen by client application) */
	/* can be connected to something else.  Initially, this will allow application */
	/* access to the IGate.  In version 1.8 we add network KISS TNC. */

	// Watch out for maximum number of channels.
	//	MAX_CHANS - Originally, this was 6 for internal modem adio channels. Has been phased out.
	// After adding virtual channels (IGate, network TNC), this is split into two different numbers:
	//	MAX_RADIO_CHANNELS - For internal modems.
	//	MAX_TOTAL_CHANNELS - limited by KISS channels/ports.  Needed for digipeating, filtering, etc.

	// Properties for all channels.

	mycall [MAX_TOTAL_CHANS]string /* Call associated with this radio channel. */
	/* Could all be the same or different. */

	chan_medium [MAX_TOTAL_CHANS]medium_e
	// MEDIUM_NONE for invalid.
	// MEDIUM_RADIO for internal modem.  (only possibility earlier)
	// MEDIUM_IGATE allows application access to IGate.
	// MEDIUM_NETTNC for external TNC via TCP.
	// MediumSerialTNC for external TNC via serial port.
	// MEDIUM_AXUDP for AX.25 frames in UDP datagrams.

	igate_vchannel int /* Virtual channel mapped to APRS-IS. */
	/* -1 for none. */
	/* Redundant but it makes things quicker and simpler */
	/* than always searching thru above. */

	// Applies only to network TNC type channels.

	nettnc_addr [MAX_TOTAL_CHANS]string // Network TNC address:  hostname or IP addr.

	nettnc_port [MAX_TOTAL_CHANS]int // Network TNC TCP port.

	// Applies only to serial TNC type channels.

	serialTNCDevice [MAX_TOTAL_CHANS]string // Serial TNC device name.

	serialTNCBaud [MAX_TOTAL_CHANS]int // Serial TNC baud rate.

	// Applies only to AXUDP channels.

	axudp_port   [MAX_TOTAL_CHANS]int          // Local UDP port.
	axudp_routes [MAX_TOTAL_CHANS]axudp.Routes // Where frames sent on the channel go.

	achan [MAX_RADIO_CHANS]achan_param_s

	/* TODO KG
	//#ifdef USE_HAMLIB
	rigs int              // Total number of configured rigs
	rig  [MAX_RIGS]*C.RIG // HAMLib rig instances
	//#endif
	*/

}

const DEFAULT_FIX_BITS = BitFixNone // Interesting research project but even a single bit fix up
// will occasionally let corrupted packets through.

/*
 * Standard for AFSK on VHF FM.
 * Reversing mark and space makes no difference because
 * NRZI encoding only cares about change or lack of change
 * between the two tones.
 *
 * HF SSB uses 300 baud and 200 Hz shift.
 * 1600 & 1800 Hz is a popular tone pair, sometimes
 * called the KAM tones.
 */

const DEFAULT_MARK_FREQ = 1200
const DEFAULT_SPACE_FREQ = 2200
const DEFAULT_BAUD = 1200

/* Used for sanity checking in config file and command line options. */
/* 9600 baud is known to work.  */
/* TODO: Is 19200 possible with a soundcard at 44100 samples/sec or do we need a higher sample rate? */

const MIN_BAUD = 100

const MAX_BAUD = 40000 // Anyone want to try 38.4 k baud?

/*
 * Typical transmit timings for VHF.
 */

const DEFAULT_DWAIT = 0
const DEFAULT_SLOTTIME = 10 // *10mS = 100mS
const DEFAULT_PERSIST = 63
const DEFAULT_TXDELAY = 30    // *10mS = 300mS
const DEFAULT_TXTAIL = 10     // *10mS = 100mS
const DEFAULT_FULLDUP = false // false = half duplex

// digipeaterPorts is our callsign and the medium of each channel, as the
// digipeaters need them.
func (c *RadioConfig) digipeaterPorts() [MAX_TOTAL_CHANS]digipeater.Port {
	var ports [MAX_TOTAL_CHANS]digipeater.Port

	for channel := range MAX_TOTAL_CHANS {
		ports[channel] = digipeater.Port{MyCall: c.mycall[channel], Medium: c.chan_medium[channel]}
	}

	return ports
}

// connectedModeAllowed reports whether AX.25 connected mode can be used on
// channel: one that reaches the air, through our own modem, a network or
// serial TNC, or AXUDP.
func (c *RadioConfig) connectedModeAllowed(channel int) bool {
	if channel < 0 || channel >= MAX_TOTAL_CHANS {
		return false
	}

	var m = c.chan_medium[channel]

	return m == MEDIUM_RADIO || m == MEDIUM_NETTNC || m == MediumSerialTNC || m == MEDIUM_AXUDP
}
