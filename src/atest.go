// Test fixture for the Dire Wolf demodulators.

package direwolf

/*-------------------------------------------------------------------
 *
 * Purpose:     Test fixture for the Dire Wolf demodulators.
 *
 * Inputs:	Takes audio from a .WAV file instead of the audio device.
 *
 * Description:	This can be used to test the demodulators under
 *		controlled and reproducible conditions for tweaking.
 *
 *		For example
 *
 *		(1) Download WA8LMF's TNC Test CD image file from
 *			http://wa8lmf.net/TNCtest/index.htm
 *
 *		(2) Burn a physical CD.
 *
 *		(3) "Rip" the desired tracks with Windows Media Player.
 *			Select .WAV file format.
 *
 *		"Track 2" is used for most tests because that is more
 *		realistic for most people using the speaker output.
 *
 *
 * 	Without ONE_CHAN defined:
 *
 *	  Notice that the number of packets decoded, as reported by
 *	  this test program, will be twice the number expected because
 *	  we are decoding the left and right audio channels separately.
 *
 *
 * 	With ONE_CHAN defined:
 *
 *	  Only process one channel.
 *
 *--------------------------------------------------------------------*/

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"unicode"
	"unsafe"

	"github.com/sirupsen/logrus"
)

type atest_header_t struct {
	RIFF     [4]byte /* "RIFF" */
	Filesize int32   /* file length - 8 */
	WAVE     [4]byte /* "WAVE" */
}

type atest_chunk_t struct {
	Id       [4]byte /* "LIST" or "fmt " */
	Datasize int32
}

type atest_format_t struct {
	Wformattag      int16 /* 1 for PCM. */
	Nchannels       int16 /* 1 for mono, 2 for stereo. */
	Nsamplespersec  int32 /* sampling freq, Hz. */
	Navgbytespersec int32 /* = nblockalign*nsamplespersec. */
	Nblockalign     int16 /* = wbitspersample/8 * nchannels. */
	Wbitspersample  int16 /* 16 or 8. */
}

type atest_wav_data_t struct {
	Data     [4]byte /* "data" */
	Datasize int32
}

const EXPERIMENT_G = true
const EXPERIMENT_H = true

/*
 * atestFixBits maps the -F argument onto a fix_bits level and the PASSALL flag.
 *
 * 0 up to BitFixLevelHighest are the levels of effort that the FIX_BITS
 * configuration keyword accepts.  One more than that - the value that decodes
 * as "PASSALL" - asks for all of them and then hands over frames that still
 * have a bad CRC, which is the only way to reach PASSALL from the command line
 * and keeps -F a scale where each value is at least as permissive as the last.
 *
 * The final return value is false for an argument outside that range.
 */
func atestFixBits(n int) (BitFixLevel, bool, bool) {
	switch {
	case n < int(BitFixNone) || n > int(BitFixPassall):
		return DEFAULT_FIX_BITS, false, false
	case BitFixLevel(n) == BitFixPassall:
		return BitFixLevelHighest, true, true
	default:
		return BitFixLevel(n), false, true
	}
}

// AtestOptions are samoyed-atest's choices about how to decode, which is
// everything on its command line apart from the files to decode and what
// counts as the test passing.
type AtestOptions struct {
	// Modem is the modem chosen by -B, -g, -k, -j, -J, -P, -D and -U.
	Modem *ModemFlags

	// FixBits is the -F level, from BitFixNone up to BitFixPassall.
	FixBits int

	// IL2PVersion is the version of IL2P to receive, as
	// il2p_parse_version takes it.
	IL2PVersion string

	// DecodeOnly is which audio channel of the file to decode: 0 or 1, or 2
	// for both.
	DecodeOnly int

	// BitErrorRate is the receive bit error rate to simulate.
	BitErrorRate float64

	// HexDisplay prints each frame as hexadecimal bytes as well.
	HexDisplay bool

	// Debug levels for DCD output ("-d o"), FX.25 ("-d x") and IL2P ("-d 2").
	DebugDCD  int
	DebugFX25 int
	DebugIL2P int
}

// An Atest decodes .WAV files with the demodulators, reporting on each frame
// it finds.
type Atest struct {
	audio      *audio_s
	decodeOnly int

	// One sink for the whole run, so its DCD counts are of everything
	// decoded rather than of the file being decoded at the time.
	sink *atestSink
}

// AtestFileResult is what decoding one .WAV file found.
type AtestFileResult struct {
	PacketsDecoded int

	// Seconds is how long the audio in the file lasts.
	Seconds float64
}

// NewAtest sets up the demodulators as opts asks.  The error is for an
// option that is out of range or makes no sense.
func NewAtest(opts *AtestOptions) (*Atest, error) {
	var audio = atestDefaultAudio()

	var il2p_version, il2p_version_ok = il2p_parse_version(opts.IL2PVersion)
	if !il2p_version_ok {
		return nil, fmt.Errorf("invalid IL2P version %s: expected 0.4, 0.6, or compat", opts.IL2PVersion)
	}

	for channel := range MAX_RADIO_CHANS {
		audio.achan[channel].il2p_version = il2p_version
	}

	var fixBitsLevel, fixBitsPassall, fixBitsValid = atestFixBits(opts.FixBits)
	if !fixBitsValid {
		return nil, fmt.Errorf("fix bits should be between %d and %d inclusive, not %d", BitFixNone, BitFixPassall, opts.FixBits)
	}

	audio.achan[0].fix_bits = fixBitsLevel
	audio.achan[0].passall = fixBitsPassall

	if opts.DecodeOnly < 0 || opts.DecodeOnly > 2 {
		return nil, fmt.Errorf("channel to decode should be 0, 1 or 2 (both), not %d", opts.DecodeOnly)
	}

	audio.recv_ber = opts.BitErrorRate

	if opts.Modem != nil {
		var modemErr = opts.Modem.apply(&audio.achan[0])
		if modemErr != nil {
			return nil, modemErr
		}
	}

	atestSingleSlicer(&audio.achan[0])

	audio.achan[1] = audio.achan[0]

	FX25Init(opts.DebugFX25)
	il2p_init(opts.DebugIL2P)

	var a = new(Atest)
	a.audio = audio
	a.decodeOnly = opts.DecodeOnly
	a.sink = new(atestSink)
	a.sink.audio = audio
	a.sink.hexDisplay = opts.HexDisplay
	a.sink.debugDCD = opts.DebugDCD
	a.sink.sampleNumber = -1

	return a, nil
}

// DCDStats are the DCD counts across every file decoded so far: how many
// times the channel became busy, and how many frames arrived without it.
func (a *Atest) DCDStats() (int, int) {
	return a.sink.dcdCount, a.sink.dcdMissingErrors
}

// DecodeFile decodes the .WAV file called name.
func (a *Atest) DecodeFile(name string) (AtestFileResult, error) {
	var f, err = os.Open(name) //nolint:gosec // File path from CLI is expected for this tool
	if err != nil {
		return AtestFileResult{}, fmt.Errorf("couldn't open file %s for read: %w", name, err)
	}

	defer f.Close()

	return a.DecodeWAV(f, name)
}

// DecodeWAV decodes a .WAV file from r, calling it name in what it reports.
func (a *Atest) DecodeWAV(r io.ReadSeeker, name string) (AtestFileResult, error) {
	var count [MAX_SUBCHANS]int // Experiments G and H
	var space_gain [MAX_SUBCHANS]float64

	var header atest_header_t
	var chunk atest_chunk_t
	var format atest_format_t
	var wav_data atest_wav_data_t

	/*
	 * Read the file header.
	 * Doesn't handle all possible cases but good enough for our purposes.
	 */

	var err = binary.Read(r, binary.LittleEndian, &header)
	if err != nil {
		return AtestFileResult{}, fmt.Errorf("WAV file error: Could not read file header: %w", err)
	}

	if string(header.RIFF[:]) != "RIFF" || string(header.WAVE[:]) != "WAVE" {
		return AtestFileResult{}, errors.New("this is not a .WAV format file")
	}

	err = binary.Read(r, binary.LittleEndian, &chunk)
	if err != nil {
		return AtestFileResult{}, fmt.Errorf("WAV file error: Could not read chunk header: %w", err)
	}

	for string(chunk.Id[:]) != "fmt " {
		if chunk.Datasize < 0 {
			return AtestFileResult{}, fmt.Errorf("WAV file error: Invalid chunk datasize %d", chunk.Datasize)
		}

		_, err = r.Seek(int64(chunk.Datasize)+int64(chunk.Datasize%2), io.SeekCurrent)
		if err != nil {
			return AtestFileResult{}, fmt.Errorf("WAV file error: Could not Seek: %w", err)
		}

		err = binary.Read(r, binary.LittleEndian, &chunk)
		if err != nil {
			return AtestFileResult{}, errors.New(`WAV file error: Could not find "fmt " chunk`)
		}
	}

	if chunk.Datasize != 16 && chunk.Datasize != 18 {
		return AtestFileResult{}, fmt.Errorf("WAV file error: Need fmt chunk datasize of 16 or 18.  Found %d", chunk.Datasize)
	}

	err = binary.Read(r, binary.LittleEndian, &format)
	if err != nil {
		return AtestFileResult{}, fmt.Errorf("WAV file error: Could not read fmt chunk: %w", err)
	}

	// KG If Datasize > sizeof(format), skip until the actual data
	var formatSize = int32(unsafe.Sizeof(format))
	if chunk.Datasize > formatSize {
		var extra = chunk.Datasize - formatSize

		_, err = r.Seek(int64(extra), io.SeekCurrent)
		if err != nil {
			return AtestFileResult{}, fmt.Errorf("WAV file error: Could not Seek: %w", err)
		}
	}

	err = binary.Read(r, binary.LittleEndian, &wav_data)
	if err != nil {
		return AtestFileResult{}, fmt.Errorf("WAV file error: Could not read data chunk header: %w", err)
	}

	for string(wav_data.Data[:]) != "data" {
		if wav_data.Datasize < 0 {
			return AtestFileResult{}, fmt.Errorf("WAV file error: Invalid chunk datasize %d", wav_data.Datasize)
		}

		_, err = r.Seek(int64(wav_data.Datasize)+int64(wav_data.Datasize%2), io.SeekCurrent)
		if err != nil {
			return AtestFileResult{}, fmt.Errorf("WAV file error: Could not Seek: %w", err)
		}

		err = binary.Read(r, binary.LittleEndian, &wav_data)
		if err != nil {
			return AtestFileResult{}, errors.New(`WAV file error: Could not find "data" chunk`)
		}
	}

	if wav_data.Datasize < 0 {
		return AtestFileResult{}, fmt.Errorf("WAV file error: Invalid data chunk datasize %d", wav_data.Datasize)
	}

	if format.Wformattag != 1 {
		return AtestFileResult{}, fmt.Errorf("sorry, I only understand audio format 1 (PCM).  This file has %d", format.Wformattag)
	}

	if format.Nchannels != 1 && format.Nchannels != 2 {
		return AtestFileResult{}, fmt.Errorf("sorry, I only understand 1 or 2 channels.  This file has %d", format.Nchannels)
	}

	if format.Wbitspersample != 8 && format.Wbitspersample != 16 {
		return AtestFileResult{}, fmt.Errorf("sorry, I only understand 8 or 16 bits per sample.  This file has %d", format.Wbitspersample)
	}

	if format.Nsamplespersec < MIN_SAMPLES_PER_SEC || format.Nsamplespersec > MAX_SAMPLES_PER_SEC {
		return AtestFileResult{}, fmt.Errorf("sorry, I only understand sample rates from %d to %d.  This file has %d", MIN_SAMPLES_PER_SEC, MAX_SAMPLES_PER_SEC, format.Nsamplespersec)
	}

	var audio = a.audio

	audio.adev[0].samples_per_sec = int(format.Nsamplespersec)
	audio.adev[0].bits_per_sample = int(format.Wbitspersample)
	audio.adev[0].num_channels = int(format.Nchannels)

	audio.chan_medium[0] = MEDIUM_RADIO
	if format.Nchannels == 2 {
		audio.chan_medium[1] = MEDIUM_RADIO
	}

	text_color_set(DW_COLOR_INFO)
	fmt.Printf("%d samples per second.  %d bits per sample.  %d audio channels.\n",
		audio.adev[0].samples_per_sec,
		audio.adev[0].bits_per_sample,
		(audio.adev[0].num_channels))
	// nnum_channels is known to be 1 or 2.
	var one_filetime = float64(wav_data.Datasize) /
		float64((audio.adev[0].bits_per_sample/8)*(audio.adev[0].num_channels)*audio.adev[0].samples_per_sec)

	fmt.Printf("%d audio bytes in file.  Duration = %.1f seconds.\n",
		wav_data.Datasize,
		one_filetime)
	fmt.Printf("Fix Bits level = %d\n", audio.achan[0].fix_bits)

	/*
	 * Initialize the AFSK demodulator and HDLC decoder.
	 * Needs to be done for each file because they could have different sample rates.
	 */
	multi_modem_init(audio, a.sink)

	a.sink.packetsDecoded = 0

	var src = newReaderSampleSource(r, wav_data.Datasize)

	var e_o_f = false
	for !e_o_f {
		for c := range audio.adev[0].num_channels {
			/* This reads either 1 or 2 bytes depending on */
			/* bits per sample.  */
			var audio_sample = demod_get_sample(ACHAN2ADEV(c), src)

			if audio_sample >= 256*256 {
				e_o_f = true

				continue
			}

			if c == 0 {
				a.sink.sampleNumber++
			}

			if a.decodeOnly == 0 && c != 0 {
				continue
			}

			if a.decodeOnly == 1 && c != 1 {
				continue
			}

			multi_modem_process_sample(c, audio_sample)
		}

		/* When a complete frame is accumulated, */
		/* process_rec_frame, below, is called. */
	}

	text_color_set(DW_COLOR_INFO)
	fmt.Printf("\n\n")

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

	fmt.Printf("%d from %s\n", a.sink.packetsDecoded, name)

	return AtestFileResult{PacketsDecoded: a.sink.packetsDecoded, Seconds: one_filetime}, nil
}

/*
 * Sample data from a .WAV file, in place of the audio device.
 */

// readerSampleSource hands out up to nbytes of sample data read from r - for
// atest, the "data" chunk of an open .WAV file.  A file shorter than its header
// claims therefore ends at whichever of the two comes first.
type readerSampleSource struct {
	r         *bufio.Reader
	remaining int32
}

func newReaderSampleSource(r io.Reader, nbytes int32) *readerSampleSource {
	var s = new(readerSampleSource)
	s.r = bufio.NewReader(r)
	s.remaining = nbytes

	return s
}

func (s *readerSampleSource) GetByte(_ int) int {
	if s.remaining <= 0 {
		return (-1)
	}

	var data, err = s.r.ReadByte()
	s.remaining--

	if errors.Is(err, io.EOF) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unexpected end of file.\n")

		return (-1)
	}

	// TODO KG Better error handling

	return int(data)
}

/*
 * This is called when we have a good frame.
 */

func (s *atestSink) RecFrame(channel int, subchan int, slice int, pp *packet_t, alevel ALevel, fec_type fec_type_t, retries BitFixLevel, spectrum string) {
	s.packetsDecoded++

	if hdlcReceiver.DataDetectAny(channel) == 0 {
		s.dcdMissingErrors++
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
	dw_printf("DECODED[%d] ", s.packetsDecoded)

	/* Insert time stamp relative to start of file. */

	var sec = float64(s.sampleNumber) / float64(s.audio.adev[0].samples_per_sec)
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
		if s.audio.achan[channel].fix_bits == RETRY_NONE && !s.audio.achan[channel].passall {
			// No fix_bits or passall specified.
			dw_printf("%s audio level = %s     %s\n", heard, alevel_text, spectrum)
		} else {
			Assert(retries >= RETRY_NONE && retries <= BitFixPassall) // validate array index.
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

	if s.audio.achan[channel].num_subchan > 1 && s.audio.achan[channel].num_slicers == 1 {
		dw_printf("[%d.%d] ", channel, subchan)
	} else if s.audio.achan[channel].num_subchan == 1 && s.audio.achan[channel].num_slicers > 1 {
		dw_printf("[%d.%d] ", channel, slice)
	} else if s.audio.achan[channel].num_subchan > 1 && s.audio.achan[channel].num_slicers > 1 {
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

	if s.hexDisplay {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		ax25_hex_dump(pp)
		dw_printf("------\n")
	}

	/*
		#if 0		// temp experiment

		#include "decode_aprs.h"

			if (ax25_is_aprs(pp)) {

			  decode_aprs_t A;

			  decode_aprs (&A, pp, 0, NULL);

			  // Temp experiment to see how different systems set the RR bits in the source and destination.
			  // log_rr_bits (&A, pp);

			}
		#endif
	*/
} /* end RecFrame */

// atestSink is where atest's decoders report what they have heard.  A running
// Samoyed acts on that - queueing the frame for the receive thread, keying a
// DCD output line - where atest reports on it: the frame, and with "-d o", the
// time the channel was busy for.
type atestSink struct {
	audio      *audio_s
	hexDisplay bool // -h
	debugDCD   int  // -d o

	// sampleNumber is the number of the sample being decoded, counted on
	// channel 0 across every file, for the time a frame was decoded at.
	sampleNumber int

	// packetsDecoded counts the frames decoded from the file being read;
	// DecodeWAV clears it between files.
	packetsDecoded   int
	dcdMissingErrors int
	dcdCount         int
	dcdStartSeconds  [MAX_RADIO_CHANS]float64
}

func (s *atestSink) DCDChange(channel int, state int) {
	if s.debugDCD > 0 {
		var t = float64(s.sampleNumber) / float64(s.audio.adev[0].samples_per_sec)

		text_color_set(DW_COLOR_INFO)

		if state != 0 {
			//sec1 = t;
			//min1 = (int)(sec1 / 60.);
			//sec1 -= min1 * 60;
			//dw_printf ("DCD[%d] = ON    %d:%06.3f\n",  channel, min1, sec1);
			s.dcdCount++
			s.dcdStartSeconds[channel] = t
		} else {
			//dw_printf ("DCD[%d] = off   %d:%06.3f   %3.0f\n",  channel, min, sec, (t - s.dcdStartSeconds[channel]) * 1000.);
			var sec1 = s.dcdStartSeconds[channel]
			var min1 = (int)(sec1 / 60.)
			sec1 -= float64(min1 * 60)

			var sec2 = t
			var min2 = (int)(sec2 / 60.)
			sec2 -= float64(min2 * 60)

			dw_printf("DCD[%d]  %d:%06.3f - %d:%06.3f =  %3.0f\n", channel, min1, sec1, min2, sec2, (t-s.dcdStartSeconds[channel])*1000.)
		}
	}
}

// atestDefaultAudio is the audio configuration atest starts from, before its options.
func atestDefaultAudio() *audio_s {
	var audio = new(audio_s)

	/*
	 * First apply defaults.
	 */

	audio.adev[0].num_channels = DEFAULT_NUM_CHANNELS
	audio.adev[0].samples_per_sec = DEFAULT_SAMPLES_PER_SEC
	audio.adev[0].bits_per_sample = DEFAULT_BITS_PER_SAMPLE

	for channel := range MAX_RADIO_CHANS {
		audio.achan[channel].modem_type = MODEM_AFSK

		audio.achan[channel].mark_freq = DEFAULT_MARK_FREQ
		audio.achan[channel].space_freq = DEFAULT_SPACE_FREQ
		audio.achan[channel].baud = DEFAULT_BAUD

		audio.achan[channel].profiles = "A"

		audio.achan[channel].num_freq = 1
		audio.achan[channel].offset = 0

		audio.achan[channel].fix_bits = RETRY_NONE

		audio.achan[channel].sanity_test = SANITY_APRS
		// audio.achan[channel].sanity_test = SANITY_AX25;
		// audio.achan[channel].sanity_test = SANITY_NONE;
	}

	return audio
}

// atestSingleSlicer gives G3RUH and AIS a single slicer, unless -P asked for
// something else, where direwolf gives them several by default ("+").
// That is how atest has always decoded them, and what the expected decode
// counts in test-scripts were measured with.
func atestSingleSlicer(achan *achan_param_s) {
	if achan.profiles == "" && (achan.modem_type == MODEM_SCRAMBLE || achan.modem_type == MODEM_AIS) {
		achan.profiles = "-"

		logrus.WithField("profiles", achan.profiles).Info("Decoding with a single slicer")
	}
}
