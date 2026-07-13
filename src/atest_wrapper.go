/* Shared state for the "atest" test fixture, kept in package direwolf because
 * it plugs into the audio/PTT/input dispatch functions that core code calls
 * unqualified, and because dlq_rec_frame_fake needs unexported types
 * (packet_t, fec_type_t) that can't be named from cmd/samoyed-atest. */
//nolint:gochecknoglobals
package direwolf

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

var ATEST_C = false

var atestBuf *bufio.Reader
var atest_remaining_bytes int32
var e_o_f bool
var packets_decoded_one = 0

var my_audio_config *audio_s

var space_gain [MAX_SUBCHANS]float64

var sample_number = -1 /* Sample number from the file. */
/* Incremented only for channel 0. */
/* Use to print timestamp, relative to beginning */
/* of file, when frame was decoded. */

var h_opt = false // Hexadecimal display of received packet.
var d_o_opt = 0   // "-d o" option for DCD output control. */
var dcd_count = 0
var dcd_missing_errors = 0

var decode_only = 0 /* Set to 0 or 1 to decode only one channel.  2 for both.  */

const EXPERIMENT_G = true
const EXPERIMENT_H = true

// AtestOptions carries CLI-flag-derived config from cmd/samoyed-atest into
// AtestConfigure.
type AtestOptions struct {
	BitrateStr       string
	G3RUH            bool
	BPSK             bool
	Direwolf15Compat bool
	MFJ2400Compat    bool
	ModemProfile     string
	Decimate         int
	Upsample         int
	FixBits          int
	DecodeOnly       int // 0, 1, or 2 — resolved by cmd/ from -0/-1/-2 flags.
	HexDisplay       bool
	BitErrorRate     float64
	FX25DebugLevel   int // count of "x" in -d.
	DCDDebugLevel    int // count of "o" in -d.
	IL2PDebugLevel   int // count of "2" in -d.
}

// AtestConfigure builds the audio config used by the demodulator from opts,
// validates it, and initializes the FX.25/IL2P debug levels. On error, the
// caller is responsible for printing usage and exiting.
func AtestConfigure(opts AtestOptions) error {
	my_audio_config = new(audio_s)

	/*
	 * First apply defaults.
	 */

	my_audio_config.adev[0].num_channels = DEFAULT_NUM_CHANNELS
	my_audio_config.adev[0].samples_per_sec = DEFAULT_SAMPLES_PER_SEC
	my_audio_config.adev[0].bits_per_sample = DEFAULT_BITS_PER_SAMPLE

	for channel := range MAX_RADIO_CHANS {
		my_audio_config.achan[channel].modem_type = MODEM_AFSK

		my_audio_config.achan[channel].mark_freq = DEFAULT_MARK_FREQ
		my_audio_config.achan[channel].space_freq = DEFAULT_SPACE_FREQ
		my_audio_config.achan[channel].baud = DEFAULT_BAUD

		my_audio_config.achan[channel].profiles = "A"

		my_audio_config.achan[channel].num_freq = 1
		my_audio_config.achan[channel].offset = 0

		my_audio_config.achan[channel].fix_bits = RETRY_NONE

		my_audio_config.achan[channel].sanity_test = SANITY_APRS
	}

	if opts.Decimate < 0 || opts.Decimate > 8 {
		return fmt.Errorf("decimate should be between 0 and 8 inclusive, not %d", opts.Decimate)
	}

	my_audio_config.achan[0].decimate = opts.Decimate

	if opts.Upsample != 0 {
		if opts.Upsample < 1 || opts.Upsample > 8 {
			return fmt.Errorf("upsample should be between 1 and 4 inclusive, not %d", opts.Upsample)
		}

		my_audio_config.achan[0].upsample = opts.Upsample
	}

	if BitFixLevel(opts.FixBits) < RETRY_NONE || BitFixLevel(opts.FixBits) > RETRY_MAX {
		return fmt.Errorf("fix bits should be between %d and %d inclusive, not %d", RETRY_NONE, RETRY_MAX, opts.FixBits)
	}

	my_audio_config.achan[0].fix_bits = BitFixLevel(opts.FixBits)

	my_audio_config.recv_ber = opts.BitErrorRate

	h_opt = opts.HexDisplay

	// Hacks for the magic strings
	var bitrate, bitrateParseErr = strconv.Atoi(opts.BitrateStr)
	if opts.BitrateStr == "AIS" {
		bitrate = 0xA15A15
	} else if opts.BitrateStr == "EAS" {
		bitrate = 0xEA5EA5
	} else if bitrateParseErr != nil {
		return fmt.Errorf("invalid bitrate (should be an integer or 'AIS' or 'EAS'): %s", opts.BitrateStr)
	}

	/*
	 * Set modem type based on data rate.
	 * (Could be overridden by -g, -j, or -J later.)
	 */
	/*    300 implies 1600/1800 AFSK. */
	/*    1200 implies 1200/2200 AFSK. */
	/*    2400 implies V.26 QPSK. */
	/*    4800 implies V.27 8PSK. */
	/*    9600 implies G3RUH baseband scrambled. */

	my_audio_config.achan[0].baud = bitrate

	/* We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c, */
	/* that need to be kept in sync.  Maybe it could be a common function someday. */

	if my_audio_config.achan[0].baud == 100 { // What was this for?
		my_audio_config.achan[0].modem_type = MODEM_AFSK
		my_audio_config.achan[0].mark_freq = 1615
		my_audio_config.achan[0].space_freq = 1785
	} else if my_audio_config.achan[0].baud < 600 { // e.g. HF SSB packet
		my_audio_config.achan[0].modem_type = MODEM_AFSK
		my_audio_config.achan[0].mark_freq = 1600
		my_audio_config.achan[0].space_freq = 1800
		// Previously we had a "D" which was fine tuned for 300 bps.
		// In v1.7, it's not clear if we should use "B" or just stick with "A".
	} else if my_audio_config.achan[0].baud < 1800 { // common 1200
		my_audio_config.achan[0].modem_type = MODEM_AFSK
		my_audio_config.achan[0].mark_freq = DEFAULT_MARK_FREQ
		my_audio_config.achan[0].space_freq = DEFAULT_SPACE_FREQ
	} else if my_audio_config.achan[0].baud < 3600 {
		my_audio_config.achan[0].modem_type = MODEM_QPSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].profiles = ""
	} else if my_audio_config.achan[0].baud < 7200 {
		my_audio_config.achan[0].modem_type = MODEM_8PSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].profiles = ""
	} else if my_audio_config.achan[0].baud == 0xA15A15 { // Hack for different use of 9600
		my_audio_config.achan[0].modem_type = MODEM_AIS
		my_audio_config.achan[0].baud = 9600
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].profiles = " " // avoid getting default later.
	} else if my_audio_config.achan[0].baud == 0xEA5EA5 {
		my_audio_config.achan[0].modem_type = MODEM_EAS
		my_audio_config.achan[0].baud = 521 // Actually 520.83 but we have an integer field here.
		// Will make more precise in afsk demod init.
		my_audio_config.achan[0].mark_freq = 2083  // Actually 2083.3 - logic 1.
		my_audio_config.achan[0].space_freq = 1563 // Actually 1562.5 - logic 0.
		my_audio_config.achan[0].profiles = "A"
	} else {
		my_audio_config.achan[0].modem_type = MODEM_SCRAMBLE
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].profiles = " " // avoid getting default later.
	}

	if my_audio_config.achan[0].baud < MIN_BAUD || my_audio_config.achan[0].baud > MAX_BAUD {
		return fmt.Errorf("use a more reasonable bit rate in range of %d - %d", MIN_BAUD, MAX_BAUD)
	}

	/*
	 * -g option means force g3RUH regardless of speed.
	 */

	if opts.G3RUH {
		my_audio_config.achan[0].modem_type = MODEM_SCRAMBLE
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].profiles = " " // avoid getting default later.
	}

	if opts.BPSK {
		my_audio_config.achan[0].modem_type = MODEM_BPSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].profiles = ""
	}

	/*
	 * We have two different incompatible flavors of V.26.
	 */
	if opts.Direwolf15Compat {
		// V.26 compatible with earlier versions of direwolf.
		//   Example:   -B 2400 -j    or simply   -j
		my_audio_config.achan[0].v26_alternative = V26_A
		my_audio_config.achan[0].modem_type = MODEM_QPSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].baud = 2400
		my_audio_config.achan[0].profiles = ""
	}

	if opts.MFJ2400Compat {
		// V.26 compatible with MFJ and maybe others.
		//   Example:   -B 2400 -J     or simply   -J
		my_audio_config.achan[0].v26_alternative = V26_B
		my_audio_config.achan[0].modem_type = MODEM_QPSK
		my_audio_config.achan[0].mark_freq = 0
		my_audio_config.achan[0].space_freq = 0
		my_audio_config.achan[0].baud = 2400
		my_audio_config.achan[0].profiles = ""
	}

	// Needs to be after -B, -j, -J.
	if opts.ModemProfile != "" {
		fmt.Printf("Demodulator profile set to \"%s\"\n", opts.ModemProfile)
		my_audio_config.achan[0].profiles = opts.ModemProfile
	}

	my_audio_config.achan[1] = my_audio_config.achan[0]

	FX25Init(opts.FX25DebugLevel)
	il2p_init(opts.IL2PDebugLevel)

	decode_only = opts.DecodeOnly
	d_o_opt = opts.DCDDebugLevel

	return nil
}

// AtestDecodeWAV feeds one already-parsed WAV file's PCM data through the
// demodulator/HDLC pipeline. sampleRate/bitsPerSample/numChannels/dataSize
// come from cmd/samoyed-atest's own WAV header parse. Returns the number of
// packets decoded for this file.
func AtestDecodeWAV(sampleRate, bitsPerSample, numChannels int, dataSize int32, r *bufio.Reader) int {
	my_audio_config.adev[0].samples_per_sec = sampleRate
	my_audio_config.adev[0].bits_per_sample = bitsPerSample
	my_audio_config.adev[0].num_channels = numChannels

	my_audio_config.chan_medium[0] = MEDIUM_RADIO
	if numChannels == 2 {
		my_audio_config.chan_medium[1] = MEDIUM_RADIO
	}

	fmt.Printf("Fix Bits level = %d\n", my_audio_config.achan[0].fix_bits)

	/*
	 * Initialize the AFSK demodulator and HDLC decoder.
	 * Needs to be done for each file because they could have different sample rates.
	 */
	multi_modem_init(my_audio_config)

	packets_decoded_one = 0

	atestBuf = r
	atest_remaining_bytes = dataSize

	e_o_f = false
	for !e_o_f {
		for c := range numChannels {
			/* This reads either 1 or 2 bytes depending on */
			/* bits per sample.  */
			var audio_sample = demod_get_sample(ACHAN2ADEV(c))

			if audio_sample >= 256*256 {
				e_o_f = true
				continue
			}

			if c == 0 {
				sample_number++
			}

			if decode_only == 0 && c != 0 {
				continue
			}

			if decode_only == 1 && c != 1 {
				continue
			}

			multi_modem_process_sample(c, audio_sample)
		}

		/* When a complete frame is accumulated, */
		/* process_rec_frame, below, is called. */
	}

	text_color_set(DW_COLOR_INFO)
	fmt.Printf("\n\n")

	var count [MAX_SUBCHANS]int // Experiments G and H

	if EXPERIMENT_G {
		for j := range MAX_SUBCHANS {
			var db = 20.0 * math.Log10(space_gain[j])
			fmt.Printf("%+.1f dB, %d\n", db, count[j])
		}
	}

	if EXPERIMENT_H {
		for j := range MAX_SUBCHANS {
			fmt.Printf("%d\n", count[j])
		}
	}

	return packets_decoded_one
}

// AtestDCDCounts returns the accumulated DCD count and DCD-missing-error
// count, for the "-d o" summary printed at the end of a run.
func AtestDCDCounts() (int, int) {
	return dcd_count, dcd_missing_errors
}

/*
 * Simulate sample from the audio device.
 */

func audio_get_fake(_ int) int {
	if atest_remaining_bytes <= 0 {
		e_o_f = true
		return (-1)
	}

	var data, err = atestBuf.ReadByte()
	atest_remaining_bytes--

	if errors.Is(err, io.EOF) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unexpected end of file.\n")

		e_o_f = true
	}

	// TODO KG Better error handling

	return int(data)
}

func audio_get(a int) int {
	if ATEST_C {
		return audio_get_fake(a)
	} else {
		return audio_get_real(a)
	}
}

/*
 * This is called when we have a good frame.
 */

func dlq_rec_frame_fake(channel int, subchan int, slice int, pp *packet_t, alevel ALevel, fec_type fec_type_t, retries BitFixLevel, spectrum string) {
	packets_decoded_one++

	if hdlc_rec_data_detect_any(channel) == 0 {
		dcd_missing_errors++
	}

	var stemp = AX25FormatAddrs(pp)

	var info = AX25GetInfo(pp)

	/* Print so we can see what is going on. */

	//TODO: quiet option - suppress packet printing, only the count at the end.

	/* Display audio input level. */
	/* Who are we hearing?   Original station or digipeater? */

	var h int
	var heard string

	if ax25_get_num_addr(pp) == 0 {
		/* Not AX.25. No station to display below. */
		h = -1
	} else {
		h = ax25_get_heard(pp)
		heard = ax25_get_addr_with_ssid(pp, h)
	}

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("\n")
	dw_printf("DECODED[%d] ", packets_decoded_one)

	/* Insert time stamp relative to start of file. */

	var sec = float64(sample_number) / float64(my_audio_config.adev[0].samples_per_sec)
	var minutes = int(sec / 60.)
	sec -= float64(minutes * 60)

	dw_printf("%d:%06.3f ", minutes, sec)

	if h != AX25_SOURCE {
		dw_printf("Digipeater ")
	}

	var alevel_text = ax25_alevel_to_text(alevel)

	/* As suggested by KJ4ERJ, if we are receiving from */
	/* WIDEn-0, it is quite likely (but not guaranteed), that */
	/* we are actually hearing the preceding station in the path. */

	if h >= AX25_REPEATER_2 &&
		strings.HasPrefix(heard, "WIDE") &&
		unicode.IsDigit(rune(heard[4])) &&
		len(heard) == 5 {
		var probably_really = ax25_get_addr_with_ssid(pp, h-1)

		heard += " (probably " + probably_really + ")"
	}

	switch fec_type {
	case fec_type_fx25:
		dw_printf("%s audio level = %s   FX.25  %s\n", heard, alevel_text, spectrum)
	case fec_type_il2p:
		dw_printf("%s audio level = %s   IL2P  %s\n", heard, alevel_text, spectrum)
	default:
		//case fec_type_none:
		if my_audio_config.achan[channel].fix_bits == RETRY_NONE && !my_audio_config.achan[channel].passall {
			// No fix_bits or passall specified.
			dw_printf("%s audio level = %s     %s\n", heard, alevel_text, spectrum)
		} else {
			Assert(retries >= RETRY_NONE && retries <= RETRY_MAX) // validate array index.
			dw_printf("%s audio level = %s   [%s]   %s\n", heard, alevel_text, retries.String(), spectrum)
		}
	}

	// Display non-APRS packets in a different color.

	// Display channel with subchannel/slice if applicable.

	if ax25_is_aprs(pp) {
		text_color_set(DW_COLOR_REC)
	} else {
		text_color_set(DW_COLOR_DEBUG)
	}

	if my_audio_config.achan[channel].num_subchan > 1 && my_audio_config.achan[channel].num_slicers == 1 {
		dw_printf("[%d.%d] ", channel, subchan)
	} else if my_audio_config.achan[channel].num_subchan == 1 && my_audio_config.achan[channel].num_slicers > 1 {
		dw_printf("[%d.%d] ", channel, slice)
	} else if my_audio_config.achan[channel].num_subchan > 1 && my_audio_config.achan[channel].num_slicers > 1 {
		dw_printf("[%d.%d.%d] ", channel, subchan, slice)
	} else {
		dw_printf("[%d] ", channel)
	}

	dw_printf("%s", stemp) /* stations followed by : */
	AX25SafePrint(info, false)
	dw_printf("\n")

	/*
	 * -h option for hexadecimal display.  (new in 1.6)
	 */

	if h_opt {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		ax25_hex_dump(pp)
		dw_printf("------\n")
	}

	AX25Delete(pp)
} /* end fake dlq_append */

var dcd_start_seconds [MAX_RADIO_CHANS]float64

func ptt_set_fake(_ int, channel int, ptt_signal int) {
	// Should only get here for DCD output control.
	if d_o_opt > 0 {
		var t = float64(sample_number) / float64(my_audio_config.adev[0].samples_per_sec)

		text_color_set(DW_COLOR_INFO)

		if ptt_signal != 0 {
			dcd_count++
			dcd_start_seconds[channel] = t
		} else {
			var sec1 = dcd_start_seconds[channel]
			var min1 = (int)(sec1 / 60.)
			sec1 -= float64(min1 * 60)

			var sec2 = t
			var min2 = (int)(sec2 / 60.)
			sec2 -= float64(min2 * 60)

			dw_printf("DCD[%d]  %d:%06.3f - %d:%06.3f =  %3.0f\n", channel, min1, sec1, min2, sec2, (t-dcd_start_seconds[channel])*1000.)
		}
	}
}

func ptt_set(ot int, channel int, ptt_signal int) {
	if ATEST_C {
		ptt_set_fake(ot, channel, ptt_signal)
	} else {
		ptt_set_real(ot, channel, ptt_signal)
	}
}

func get_input_fake(it int, channel int) int {
	return -1
}

func get_input(it int, channel int) int {
	if ATEST_C {
		return get_input_fake(it, channel)
	} else {
		return get_input_real(it, channel)
	}
}
