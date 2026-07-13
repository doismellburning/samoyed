/* Test fixture for the Dire Wolf demodulators */

package main

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
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
	"unsafe"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

type atestHeader struct {
	RIFF     [4]byte /* "RIFF" */
	Filesize int32   /* file length - 8 */
	WAVE     [4]byte /* "WAVE" */
}

type atestChunk struct {
	Id       [4]byte /* "LIST" or "fmt " */
	Datasize int32
}

type atestFormat struct {
	Wformattag      int16 /* 1 for PCM. */
	Nchannels       int16 /* 1 for mono, 2 for stereo. */
	Nsamplespersec  int32 /* sampling freq, Hz. */
	Navgbytespersec int32 /* = nblockalign*nsamplespersec. */
	Nblockalign     int16 /* = wbitspersample/8 * nchannels. */
	Wbitspersample  int16 /* 16 or 8. */
}

type atestWAVData struct {
	Data     [4]byte /* "data" */
	Datasize int32
}

func main() {
	atestMain()
}

func atestMain() {
	direwolf.TextColorInit(1)

	var bitrateStr = pflag.StringP("bitrate", "B", strconv.Itoa(direwolf.DEFAULT_BAUD), `Bits/second for data.  Proper modem automatically selected for speed.
300 bps defaults to AFSK tones of 1600 & 1800.
1200 bps uses AFSK tones of 1200 & 2200.
2400 bps uses QPSK based on V.26 standard.
4800 bps uses 8PSK based on V.27 standard.
9600 bps and up uses K9NG/G3RUH standard.
AIS for ship Automatic Identification System.
EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).`)
	var g3ruh = pflag.BoolP("g3ruh", "g", false, "Use G3RUH modem rather than default for data rate.")
	var bpsk = pflag.BoolP("bpsk", "k", false, "Use BPSK modem rather than default for data rate.")
	var direwolf15compat = pflag.BoolP("direwolf-15-compat", "j", false, "2400 bps QPSK compatible with direwolf <= 1.5.")
	var mfj2400compat = pflag.BoolP("mfj-2400-compat", "J", false, "2400 bps QPSK compatible with MFJ-2400.")
	var modemProfile = pflag.StringP("modem-profile", "P", "", "Select the demodulator type such as D (default for 300 bps), E+ (default for 1200 bps), PQRS for 2400 bps, etc.")
	var decimate = pflag.IntP("decimate", "D", 0, "Divide audio sample rate by n. 0 is auto-select.")
	var upsample = pflag.IntP("upsample", "U", 0, "Upsample for G3RUH to improve performance when the sample rate to baud ratio is low.")
	var fixBits = pflag.IntP("fix-bits", "F", 0, `Amount of effort to try fixing frames with an invalid CRC.
0 (default) = consider only correct frames.
1 = Try to fix only a sigle bit.
Higher values = Try modifying more bits to get a good CRC.`)
	var errorIfLessThan = pflag.IntP("error-if-less-than", "L", -1, "Error if less than this number decoded.")
	var errorIfGreaterThan = pflag.IntP("error-if-greater-than", "G", -1, "Error if greater than this number decoded.")
	var channel0 = pflag.BoolP("channel-0", "0", false, "Use channel 0 (left) of stereo audio (default).")
	var channel1 = pflag.BoolP("channel-1", "1", false, "Use channel 1 (right) of stereo audio.")
	var channel2 = pflag.BoolP("channel-2", "2", false, "Use both channels of stereo audio.")
	var hexDisplay = pflag.BoolP("hex-display", "h", false, "Print frame contents as hexadecimal bytes.")
	var bitErrorRate = pflag.Float64P("bit-error-rate", "e", 0.0, "Receive Bit Error Rate (BER).")
	var debugFlags = pflag.StringSliceP("debug", "d", []string{}, `Debug (repeat for increased verbosity).
x = FX.25
o = DCD output control
2 = IL2P`)
	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s is a test application which decodes AX.25 frames from audio recordings.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "This provides an easy way to test decoding performance and functionality much quicker than normal real-time.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... <WAV FILE>...\n", os.Args[0])
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "$ gen_packets -o test1.wav\n")
		fmt.Fprintf(os.Stderr, "$ atest test1.wav\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "$ gen_packets -B 300 -o test3.wav\n")
		fmt.Fprintf(os.Stderr, "$ atest -B 300 test3.wav\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "$ gen_packets -B 9600 -o test9.wav\n")
		fmt.Fprintf(os.Stderr, "$ atest -B 9600 test9.wav\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Try different combinations of options to compare decoding performance.\n")
	}

	// !!! PARSE !!!
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	var dxOpt = 0
	var doOpt = 0
	var d2Opt = 0

	for _, debugFlag := range *debugFlags {
		switch debugFlag {
		case "x":
			dxOpt++
		case "o":
			doOpt++
		case "2":
			d2Opt++
		default:
			fmt.Fprintf(os.Stderr, "Unrecognised debug flag: %s\n", debugFlag)
			pflag.Usage()
			os.Exit(1)
		}
	}

	var channelFlagCount int

	for _, b := range []bool{*channel0, *channel1, *channel2} {
		if b {
			channelFlagCount++
		}
	}

	if channelFlagCount == 0 {
		*channel0 = true
	}

	if channelFlagCount > 1 {
		fmt.Fprintf(os.Stderr, "Exactly one of left/right/both channels must be selected.\n")
		pflag.Usage()
		os.Exit(1)
	}

	var decodeOnly = 0 /* Set to 0 or 1 to decode only one channel.  2 for both.  */
	if *channel0 {
		decodeOnly = 0
	}

	if *channel1 {
		decodeOnly = 1
	}

	if *channel2 {
		decodeOnly = 2
	}

	if len(pflag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "Specify .WAV file name on command line.\n\n")
		pflag.Usage()
		os.Exit(1)
	}

	var opts = direwolf.AtestOptions{
		BitrateStr:       *bitrateStr,
		G3RUH:            *g3ruh,
		BPSK:             *bpsk,
		Direwolf15Compat: *direwolf15compat,
		MFJ2400Compat:    *mfj2400compat,
		ModemProfile:     *modemProfile,
		Decimate:         *decimate,
		Upsample:         *upsample,
		FixBits:          *fixBits,
		DecodeOnly:       decodeOnly,
		HexDisplay:       *hexDisplay,
		BitErrorRate:     *bitErrorRate,
		FX25DebugLevel:   dxOpt,
		DCDDebugLevel:    doOpt,
		IL2PDebugLevel:   d2Opt,
	}

	var err = direwolf.AtestConfigure(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		pflag.Usage()
		os.Exit(1)
	}

	direwolf.ATEST_C = true

	var startTime = time.Now()
	var totalFiletime float64
	var packetsDecodedTotal = 0

	for _, wavFileName := range pflag.Args() {
		var format, dataSize, reader, openErr = openWAV(wavFileName)
		if openErr != nil {
			fmt.Fprintf(os.Stderr, "%s\n", openErr)
			os.Exit(1)
		}

		fmt.Printf("%d samples per second.  %d bits per sample.  %d audio channels.\n",
			format.Nsamplespersec, format.Wbitspersample, format.Nchannels)

		var oneFiletime = float64(dataSize) / float64((int(format.Wbitspersample)/8)*int(format.Nchannels)*int(format.Nsamplespersec))
		totalFiletime += oneFiletime

		fmt.Printf("%d audio bytes in file.  Duration = %.1f seconds.\n", dataSize, oneFiletime)

		var packetsDecodedOne = direwolf.AtestDecodeWAV(int(format.Nsamplespersec), int(format.Wbitspersample), int(format.Nchannels), dataSize, reader)

		fmt.Printf("%d from %s\n", packetsDecodedOne, wavFileName)
		packetsDecodedTotal += packetsDecodedOne
	}

	var elapsed = time.Since(startTime)

	fmt.Printf("%d packets decoded in %.3f seconds.  %.1f x realtime\n", packetsDecodedTotal, elapsed.Seconds(), totalFiletime/elapsed.Seconds())

	if doOpt > 0 {
		var dcdCount, dcdMissingErrors = direwolf.AtestDCDCounts()
		fmt.Printf("DCD count = %d\n", dcdCount)
		fmt.Printf("DCD missing errors = %d\n", dcdMissingErrors)
	}

	if *errorIfLessThan != -1 && packetsDecodedTotal < *errorIfLessThan {
		fmt.Printf("\n * * * TEST FAILED: number decoded is less than %d * * * \n", *errorIfLessThan)
		os.Exit(1)
	}

	if *errorIfGreaterThan != -1 && packetsDecodedTotal > *errorIfGreaterThan {
		fmt.Printf("\n * * * TEST FAILED: number decoded is greater than %d * * * \n", *errorIfGreaterThan)
		os.Exit(1)
	}
}

// openWAV reads a WAV file's header, skipping any chunks before "fmt " and
// between "fmt " and "data". Doesn't handle all possible cases but good
// enough for our purposes. Returns the format chunk, the size of the audio
// data, and a reader positioned at the start of that data.
func openWAV(wavFileName string) (atestFormat, int32, *bufio.Reader, error) {
	var format atestFormat

	var fp, err = os.Open(wavFileName) //nolint:gosec // File path from CLI is expected for this tool
	if err != nil {
		return format, 0, nil, fmt.Errorf("couldn't open file %s for read: %w", wavFileName, err)
	}

	var header atestHeader

	err = binary.Read(fp, binary.LittleEndian, &header)
	if err != nil {
		return format, 0, nil, fmt.Errorf("WAV file error: could not read file header: %w", err)
	}

	if string(header.RIFF[:]) != "RIFF" || string(header.WAVE[:]) != "WAVE" {
		return format, 0, nil, fmt.Errorf("%s is not a .WAV format file", wavFileName)
	}

	var chunk atestChunk

	err = binary.Read(fp, binary.LittleEndian, &chunk)
	if err != nil {
		return format, 0, nil, fmt.Errorf("WAV file error: could not read chunk header: %w", err)
	}

	for string(chunk.Id[:]) != "fmt " {
		if chunk.Datasize < 0 {
			return format, 0, nil, fmt.Errorf("WAV file error: invalid chunk datasize %d", chunk.Datasize)
		}

		_, err = fp.Seek(int64(chunk.Datasize)+int64(chunk.Datasize%2), io.SeekCurrent)
		if err != nil {
			return format, 0, nil, fmt.Errorf("WAV file error: could not seek: %w", err)
		}

		err = binary.Read(fp, binary.LittleEndian, &chunk)
		if err != nil {
			return format, 0, nil, fmt.Errorf(`WAV file error: could not find "fmt " chunk`)
		}
	}

	if chunk.Datasize != 16 && chunk.Datasize != 18 {
		return format, 0, nil, fmt.Errorf("WAV file error: need fmt chunk datasize of 16 or 18, found %d", chunk.Datasize)
	}

	binary.Read(fp, binary.LittleEndian, &format) //nolint:errcheck

	// KG If Datasize > sizeof(format), skip until the actual data
	var formatSize = int32(unsafe.Sizeof(format))
	if chunk.Datasize > formatSize {
		var extra = chunk.Datasize - formatSize

		_, err = fp.Seek(int64(extra), io.SeekCurrent)
		if err != nil {
			return format, 0, nil, fmt.Errorf("WAV file error: could not seek: %w", err)
		}
	}

	var wavData atestWAVData

	err = binary.Read(fp, binary.LittleEndian, &wavData)
	if err != nil {
		return format, 0, nil, fmt.Errorf("WAV file error: could not read data chunk header: %w", err)
	}

	for string(wavData.Data[:]) != "data" {
		if wavData.Datasize < 0 {
			return format, 0, nil, fmt.Errorf("WAV file error: invalid chunk datasize %d", wavData.Datasize)
		}

		_, err = fp.Seek(int64(wavData.Datasize)+int64(wavData.Datasize%2), io.SeekCurrent)
		if err != nil {
			return format, 0, nil, fmt.Errorf("WAV file error: could not seek: %w", err)
		}

		err = binary.Read(fp, binary.LittleEndian, &wavData)
		if err != nil {
			return format, 0, nil, fmt.Errorf(`WAV file error: could not find "data" chunk`)
		}
	}

	if format.Wformattag != 1 {
		return format, 0, nil, fmt.Errorf("sorry, only audio format 1 (PCM) is understood, this file has %d", format.Wformattag)
	}

	if format.Nchannels != 1 && format.Nchannels != 2 {
		return format, 0, nil, fmt.Errorf("sorry, only 1 or 2 channels are understood, this file has %d", format.Nchannels)
	}

	if format.Wbitspersample != 8 && format.Wbitspersample != 16 {
		return format, 0, nil, fmt.Errorf("sorry, only 8 or 16 bits per sample are understood, this file has %d", format.Wbitspersample)
	}

	return format, wavData.Datasize, bufio.NewReader(fp), nil
}
