// Package direwolf is a cgo wrapper for the Dire Wolf C source, eventually leading to a full port.
package direwolf

// #define DIREWOLF_C 1
// #include "direwolf.h"
// #include <stdio.h>
// #include <math.h>
// #include <stdlib.h>
// #include <getopt.h>
// #include <assert.h>
// #include <string.h>
// #include <signal.h>
// #include <ctype.h>
// #include <unistd.h>
// #include <fcntl.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <netinet/in.h>
// #include <netdb.h>
// #include <hamlib/rig.h>
// #include "version.h"
// #include "audio.h"
// #include "config.h"
// #include "ax25_pad.h"
// #include "decode_aprs.h"
// #include "digipeater.h"
// #include "cdigipeater.h"
// #include "igate.h"
// #include "dwgps.h"
// #include "fx25.h"
// #include "dlq.h"		// for fec_type_t definition.
// #cgo pkg-config: alsa avahi-client hamlib libbsd-overlay libgpiod libudev
// #cgo CFLAGS: -I../external/geotranz -DMAJOR_VERSION=0 -DMINOR_VERSION=0 -DUSE_CM108 -DUSE_AVAHI_CLIENT -DUSE_HAMLIB -DUSE_ALSA
// #cgo LDFLAGS: -lm
import "C"

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	_ "github.com/doismellburning/samoyed/external/geotranz" // Pulls this in for cgo
	"github.com/lestrrat-go/strftime"
	"github.com/spf13/pflag"
)

/*------------------------------------------------------------------
 *
 * Purpose:   	Main program for "Dire Wolf" which includes:
 *
 *			Various DSP modems using the "sound card."
 *			AX.25 encoder/decoder.
 *			APRS data encoder / decoder.
 *			APRS digipeater.
 *			KISS TNC emulator.
 *			APRStt (touch tone input) gateway
 *			Internet Gateway (IGate)
 *			Ham Radio of Things - IoT with Ham Radio
 *			FX.25 Forward Error Correction.
 *			IL2P Forward Error Correction.
 *			Emergency Alert System (EAS) Specific Area Message Encoding (SAME) receiver.
 *			AIS receiver for tracking ships.
 *
 *---------------------------------------------------------------*/

var d_u_opt bool /* "-d u" command line option to print UTF-8 also in hexadecimal. */
var d_p_opt bool /* "-d p" option for dumping packets over radio. */

var q_h_opt bool /* "-q h" Quiet, suppress the "heard" line with audio level. */
var q_d_opt bool /* "-q d" Quiet, suppress the printing of description of APRS packets. */

var A_opt_ais_to_obj bool /* "-A" Convert received AIS to APRS "Object Report." */

var audio_config *C.struct_audio_s
var dw_tt_config tt_config_s
var misc_config *C.struct_misc_config_s

/*-------------------------------------------------------------------
 *
 * Name:        main
 *
 * Purpose:     Main program for packet radio virtual TNC.
 *
 * Inputs:	Command line arguments.
 *		See usage message for details.
 *
 * Outputs:	Decoded information is written to stdout.
 *
 *		A socket and pseudo terminal are created for
 *		for communication with other applications.
 *
 *--------------------------------------------------------------------*/

const audio_amplitude = 100 /* % of audio sample range. */
/* This translates to +-32k for 16 bit samples. */
/* Currently no option to change this. */

func DirewolfMain() {
	var audioStatsInterval = pflag.IntP("audio-stats-interval", "a", 0, "Audio statistics interval in seconds.  0 to disable.")
	var configFileName = pflag.StringP("config-file", "c", "direwolf.conf", "Configuration file name.")
	var enablePseudoTerminal = pflag.BoolP("enable-ptty", "p", false, "Enable pseudo terminal for KISS protocol.")
	var bitrateStr = pflag.StringP("bitrate", "B", strconv.Itoa(C.DEFAULT_BAUD), `Bits/second for data.  Proper modem automatically selected for speed.
300 bps defaults to AFSK tones of 1600 & 1800.
1200 bps uses AFSK tones of 1200 & 2200.
2400 bps uses QPSK based on V.26 standard.
4800 bps uses 8PSK based on V.27 standard.
9600 bps and up uses K9NG/G3RUH standard.
AIS for ship Automatic Identification System.
EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).`)
	var g3ruh = pflag.BoolP("g3ruh", "g", false, "Use G3RUH modem rather than default for data rate.")
	var direwolf15compat = pflag.BoolP("direwolf-15-compat", "j", false, "2400 bps QPSK compatible with direwolf <= 1.5.")
	var mfj2400compat = pflag.BoolP("mfj-2400-compat", "J", false, "2400 bps QPSK compatible with MFJ-2400.")
	var modemProfile = pflag.StringP("modem-profile", "P", "", "Select the modem type such as D (default for 300 bps), E+ (default for 1200 bps), PQRS for 2400 bps, etc.")
	var decimate = pflag.IntP("decimate", "D", 0, "Divide audio sample rate by n for channel 0. 0 is auto-select.")
	var upsample = pflag.IntP("upsample", "U", 0, "Upsample for G3RUH to improve performance when the sample rate to baud ratio is low.")
	var transmitCalibration = pflag.StringP("transmit-calibration", "x", "", `Send Xmit level calibration tones.
a = Alternating mark/space tones.
m = Steady mark tone (e.g. 1200Hz).
s = Steady space tone (e.g. 2200Hz).
p = Silence (Set PTT only).
Optionally add a number to specify radio channel.`)
	var audioSampleRate = pflag.IntP("audio-sample-rate", "r", 0, "Audio sample rate, per sec.")
	var audioChannels = pflag.IntP("audio-channels", "n", 0, "Number of audio channels, 1 or 2.")
	var bitsPerSample = pflag.IntP("bits-per-sample", "b", 0, "Bits per audio sample, 8 or 16.")
	var debugStr = pflag.StringP("debug", "d", "", `Debug options:
2 = IL2P.
a = AGWPE network protocol client.
c = Connected mode data link state machine.
d = APRStt (DTMF to APRS object translation).
f = packet Filtering.
g = GPS interface.
h = hamlib increase verbose level.
i = IGate.
k = KISS serial port or pseudo terminal client.
m = Monitor heard station list.
n = KISS network client.
o = output controls such as PTT and DCD.
p = dump Packets in hexadecimal.
t = Tracker beacon.
u = Display non-ASCII text in hexadecimal.
w = Waypoints for Position or Object Reports.
x = FX.25 increase verbose level.`)
	var quietStr = pflag.StringP("quiet", "q", "", `Quiet (suppress output) options:
h = Heard line with the audio level.
d = Description of APRS packets.
x = Silence FX.25 information.`)
	var textColor = pflag.IntP("text-color", "t", 0, `Text colors.  0=disabled. 1=default.  2,3,4,... alternatives. Use 9 to test compatibility with your terminal.`)
	var printUTF8Test = pflag.BoolP("print-utf8-test", "u", false, "Print UTF-8 test string and exit.")
	var logDir = pflag.StringP("log-dir", "l", "", "Directory name for log files.")
	var logFile = pflag.StringP("log-file", "L", "", "File name for logging.")
	var symbolDump = pflag.BoolP("symbol-dump", "S", false, "Print symbol tables and exit.")
	var errorRateStr = pflag.StringP("error-rate", "E", "", "Error rate percentage for clobbering frames - transmitted frames by default, prefix with R to affect received frames")
	var timestampFormat = pflag.StringP("timestamp-format", "T", "", "Precede received frames with 'strftime' format time stamp.")
	var bitErrorRate = pflag.Float64P("bit-error-rate", "e", 0.0, "Receive Bit Error Rate (BER).")
	var fx25CheckBytes = pflag.IntP("fx25-check-bytes", "X", 0, "1 to enable FX.25 transmit.  16, 32, 64 for specific number of check bytes.")
	var il2pNormal = pflag.IntP("il2p", "I", -1, "Enable IL2P transmit.  n=1 is recommended.  0 uses weaker FEC.")
	var il2pInverted = pflag.IntP("il2p-inverted", "i", -1, "Enable IL2P transmit, inverted polarity.  n=1 is recommended.  0 uses weaker FEC.")
	var aisToAPRS = pflag.BoolP("ais-to-aprs", "A", false, "Convert AIS positions to APRS Object Reports.")

	var showVersion = pflag.BoolP("version", "V", false, "Show version.")
	var help = pflag.BoolP("help", "h", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s - a software 'soundcard' modem/TNC and APRS encoder/decoder.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Usage: direwolf [options] [ - | stdin | UDP:nnnn]\n")
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "After any options, there can be a single command line argument for the source of\n")
		fmt.Fprintf(os.Stderr, "received audio.  This can override the audio input specified in the configuration file.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Documentation can be found online at https://github.com/doismellburning/samoyed/\n")
	}

	// !!! PARSE !!!
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	if *showVersion {
		text_color_init(*textColor)
		printVersion(true)
		os.Exit(0)
	}

	if *printUTF8Test {
		fmt.Printf("\n  UTF-8 test string: ma%c%cana %c%c F%c%c%c%ce\n\n",
			0xc3, 0xb1,
			0xc2, 0xb0,
			0xc3, 0xbc, 0xc3, 0x9f)

		os.Exit(0)
	}

	if *symbolDump {
		symbols_init()
		symbols_list()
		os.Exit(0)
	}

	if *aisToAPRS {
		A_opt_ais_to_obj = true
	}

	var d_k_opt = 0      /* "-d k" option for serial port KISS.  Can be repeated for more detail. */
	var d_n_opt = 0      /* "-d n" option for Network KISS.  Can be repeated for more detail. */
	var d_t_opt = 0      /* "-d t" option for Tracker.  Can be repeated for more detail. */
	var d_g_opt = 0      /* "-d g" option for GPS. Can be repeated for more detail. */
	var d_o_opt = 0      /* "-d o" option for output control such as PTT and DCD. */
	var d_i_opt = 0      /* "-d i" option for IGate.  Repeat for more detail */
	var d_m_opt = 0      /* "-d m" option for mheard list. */
	var d_f_opt = 0      /* "-d f" option for filtering.  Repeat for more detail. */
	var d_h_opt = 0      /* "-d h" option for hamlib debugging.  Repeat for more detail */
	var d_x_opt = 1      /* "-d x" option for FX.25.  Default minimal. Repeat for more detail.  -qx to silence. */
	var d_2_opt = 0      /* "-d 2" option for IL2P.  Default minimal. Repeat for more detail. */
	var d_c_opt = 0      /* "-d c" option for connected mode data link state machine. */
	var aprstt_debug = 0 /* "-d d" option for APRStt (think Dtmf) debug. */

	if *debugStr != "" {
		// New in 1.1.  Can combine multiple such as "-d pkk"

		for _, p := range *debugStr {
			switch p {
			case 'a':
				server_set_debug(1)
			case 'k':
				d_k_opt++
				kissserial_set_debug(d_k_opt)
				kisspt_set_debug(d_k_opt)
			case 'n':
				d_n_opt++
				kiss_net_set_debug(d_n_opt)
			case 'u':
				d_u_opt = true
				// separate out gps & waypoints.
			case 'g':
				d_g_opt++
			case 'w':
				waypoint_set_debug(1) // not documented yet.
			case 't':
				d_t_opt++
				beacon_tracker_set_debug(d_t_opt)
			case 'p':
				d_p_opt = true // TODO: packet dump for xmit side.
			case 'o':
				d_o_opt++
				ptt_set_debug(d_o_opt)
			case 'i':
				d_i_opt++
			case 'm':
				d_m_opt++
			case 'f':
				d_f_opt++
			case 'h':
				d_h_opt++ // Hamlib verbose level.
			case 'c':
				d_c_opt++ // Connected mode data link state machine
			case 'x':
				d_x_opt++ // FX.25
			case '2':
				d_2_opt++ // IL2P
			case 'd':
				aprstt_debug++ // APRStt (mnemonic Dtmf)
			default:
			}
		}
	}

	if *quietStr != "" {
		// New in 1.2.  Quiet option to suppress some types of printing.
		// Can combine multiple such as "-q hd"

		for _, p := range *quietStr {
			switch p {
			case 'h':
				q_h_opt = true
			case 'd':
				q_d_opt = true
			case 'x':
				d_x_opt = 0 // Defaults to minimal info.  This silences.
			default:
			}
		}
	}

	var input_file string

	if len(pflag.Args()) > 0 {
		if len(pflag.Args()) > 1 {
			fmt.Printf("Warning: File(s) beyond the first are ignored.\n")
		}

		input_file = pflag.Arg(0)
	}

	/*
	 * Get all types of configuration settings from configuration file.
	 *
	 * Possibly override some by command line options.
	 */

	C.rig_set_debug(uint32(d_h_opt))

	symbols_init()

	audio_config = new(C.struct_audio_s)
	misc_config = new(C.struct_misc_config_s)
	var digi_config C.struct_digi_config_s
	var cdigi_config C.struct_cdigi_config_s
	var igate_config C.struct_igate_config_s

	config_init(C.CString(*configFileName), audio_config, &digi_config, &cdigi_config, &dw_tt_config, &igate_config, misc_config)

	if *audioSampleRate != 0 {
		if *audioSampleRate < C.MIN_SAMPLES_PER_SEC || *audioSampleRate > C.MAX_SAMPLES_PER_SEC {
			fmt.Printf("-r option, audio samples/sec, is out of range.\n")
			os.Exit(1)
		}
		audio_config.adev[0].samples_per_sec = C.int(*audioSampleRate)
	}

	if *audioChannels != 0 {
		if *audioChannels < 1 || *audioChannels > 2 {
			fmt.Printf("-n option, number of audio channels, is out of range.\n")
			os.Exit(1)
		}
		audio_config.adev[0].num_channels = C.int(*audioChannels)
		if *audioChannels == 2 {
			audio_config.chan_medium[1] = C.MEDIUM_RADIO
		}
	}

	if *bitsPerSample != 0 {
		if *bitsPerSample != 8 && *bitsPerSample != 16 {
			fmt.Printf("-b option, bits per sample, must be 8 or 16.\n")
			os.Exit(1)
		}

		audio_config.adev[0].bits_per_sample = C.int(*bitsPerSample)
	}

	if *bitrateStr != "" {
		var bitrate, bitrateParseErr = strconv.Atoi(*bitrateStr)
		if *bitrateStr == "AIS" {
			bitrate = 0xA15A15
		} else if *bitrateStr == "EAS" {
			bitrate = 0xEA5EA5
		} else if bitrateParseErr != nil {
			fmt.Fprintf(os.Stderr, "Invalid bitrate (should be an integer or 'AIS' or 'EAS'): %s\n", *bitrateStr)
			pflag.Usage()
			os.Exit(1)
		}

		audio_config.achan[0].baud = C.int(bitrate)

		/* We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c, */
		/* that need to be kept in sync.  Maybe it could be a common function someday. */

		if audio_config.achan[0].baud < 600 {
			audio_config.achan[0].modem_type = C.MODEM_AFSK
			audio_config.achan[0].mark_freq = 1600 // Typical for HF SSB.
			audio_config.achan[0].space_freq = 1800
			audio_config.achan[0].decimate = 3 // Reduce CPU load.
		} else if audio_config.achan[0].baud < 1800 {
			audio_config.achan[0].modem_type = C.MODEM_AFSK
			audio_config.achan[0].mark_freq = C.DEFAULT_MARK_FREQ
			audio_config.achan[0].space_freq = C.DEFAULT_SPACE_FREQ
		} else if audio_config.achan[0].baud < 3600 {
			audio_config.achan[0].modem_type = C.MODEM_QPSK
			audio_config.achan[0].mark_freq = 0
			audio_config.achan[0].space_freq = 0
			if audio_config.achan[0].baud != 2400 {
				fmt.Printf("Bit rate should be standard 2400 rather than specified %d.\n", audio_config.achan[0].baud)
			}
		} else if audio_config.achan[0].baud < 7200 {
			audio_config.achan[0].modem_type = C.MODEM_8PSK
			audio_config.achan[0].mark_freq = 0
			audio_config.achan[0].space_freq = 0
			if audio_config.achan[0].baud != 4800 {
				fmt.Printf("Bit rate should be standard 4800 rather than specified %d.\n", audio_config.achan[0].baud)
			}
		} else if audio_config.achan[0].baud == 0xA15A15 {
			audio_config.achan[0].modem_type = C.MODEM_AIS
			audio_config.achan[0].baud = 9600
			audio_config.achan[0].mark_freq = 0
			audio_config.achan[0].space_freq = 0
		} else if audio_config.achan[0].baud == 0xEA5EA5 {
			audio_config.achan[0].modem_type = C.MODEM_EAS
			audio_config.achan[0].baud = 521 // Actually 520.83 but we have an integer field here.
			// Will make more precise in afsk demod init.
			audio_config.achan[0].mark_freq = 2083  // Actually 2083.3 - logic 1.
			audio_config.achan[0].space_freq = 1563 // Actually 1562.5 - logic 0.
			C.strcpy(&audio_config.achan[0].profiles[0], C.CString("A"))
		} else {
			audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
			audio_config.achan[0].mark_freq = 0
			audio_config.achan[0].space_freq = 0
		}
	}

	if *g3ruh {
		// Force G3RUH mode, overriding default for speed.
		//   Example:   -B 2400 -g

		audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
		audio_config.achan[0].mark_freq = 0
		audio_config.achan[0].space_freq = 0
	}

	if *direwolf15compat {
		// V.26 compatible with earlier versions of direwolf.
		//   Example:   -B 2400 -j    or simply   -j

		audio_config.achan[0].v26_alternative = C.V26_A
		audio_config.achan[0].modem_type = C.MODEM_QPSK
		audio_config.achan[0].mark_freq = 0
		audio_config.achan[0].space_freq = 0
		audio_config.achan[0].baud = 2400
	}

	if *mfj2400compat {
		// V.26 compatible with MFJ and maybe others.
		//   Example:   -B 2400 -J     or simply   -J

		audio_config.achan[0].v26_alternative = C.V26_B
		audio_config.achan[0].modem_type = C.MODEM_QPSK
		audio_config.achan[0].mark_freq = 0
		audio_config.achan[0].space_freq = 0
		audio_config.achan[0].baud = 2400
	}

	if *audioStatsInterval > 0 {
		if *audioStatsInterval < 10 {
			fmt.Printf("Setting such a small audio statistics interval (<10) will produce inaccurate sample rate display.\n")
		}
		audio_config.statistics_interval = C.int(*audioStatsInterval)
	}

	if *modemProfile != "" {
		/* -P for modem profile. */
		C.strcpy(&audio_config.achan[0].profiles[0], C.CString(*modemProfile))
	}

	if *decimate != 0 {
		if *decimate < 1 || *decimate > 8 {
			fmt.Printf("Crazy value for -D. \n")
			os.Exit(1)
		}

		// Reduce audio sampling rate to reduce CPU requirements.
		audio_config.achan[0].decimate = C.int(*decimate)
	}

	if *upsample != 0 {
		if *upsample < 1 || *upsample > 4 {
			fmt.Printf("Crazy value for -U. \n")
			os.Exit(1)
		}

		// Increase G3RUH audio sampling rate to improve performance.
		// The value is normally determined automatically based on audio
		// sample rate and baud.  This allows override for experimentation.
		audio_config.achan[0].upsample = C.int(*upsample)
	}

	C.strcpy(&audio_config.timestamp_format[0], C.CString(*timestampFormat))

	// temp - only xmit errors.

	if *errorRateStr != "" {
		var e = *errorRateStr
		if e[0] == 'r' || e[0] == 'R' {
			var E_rx_opt, _ = strconv.Atoi(e[1:])
			if E_rx_opt < 1 || E_rx_opt > 99 {
				fmt.Printf("-ER must be in range of 1 to 99.\n")
				E_rx_opt = 10
			}
			audio_config.recv_error_rate = C.int(E_rx_opt)
		} else {
			var E_tx_opt, _ = strconv.Atoi(e)
			if E_tx_opt < 1 || E_tx_opt > 99 {
				fmt.Printf("-E must be in range of 1 to 99.\n")
				E_tx_opt = 10
			}
			audio_config.xmit_error_rate = C.int(E_tx_opt)
		}
	}

	if *logDir != "" && *logFile != "" {
		fmt.Printf("Logging options -l and -L can't be used together.  Pick one or the other.\n")
		os.Exit(1)
	}

	if *logFile != "" {
		misc_config.log_daily_names = 0
		C.strcpy(&misc_config.log_path[0], C.CString(*logFile))
	} else if *logDir != "" {
		misc_config.log_daily_names = 1
		C.strcpy(&misc_config.log_path[0], C.CString(*logDir))
	}

	if *enablePseudoTerminal {
		misc_config.enable_kiss_pt = 1
	}

	if input_file != "" {
		C.strcpy(&audio_config.adev[0].adevice_in[0], C.CString(input_file))
	}

	audio_config.recv_ber = C.float(*bitErrorRate)

	if *fx25CheckBytes > 0 {
		if *il2pNormal != -1 || *il2pInverted != -1 {
			fmt.Printf("Can't mix -X with -I or -i.\n")
			os.Exit(1)
		}
		audio_config.achan[0].fx25_strength = C.int(*fx25CheckBytes)
		audio_config.achan[0].layer2_xmit = C.LAYER2_FX25
	}

	if *il2pNormal != -1 && *il2pInverted != -1 {
		fmt.Printf("Can't use both -I and -i at the same time.\n")
		os.Exit(1)
	}

	if *il2pNormal >= 0 {
		audio_config.achan[0].layer2_xmit = C.LAYER2_IL2P
		if *il2pNormal > 0 {
			audio_config.achan[0].il2p_max_fec = 1
		}
		if audio_config.achan[0].il2p_max_fec == 0 {
			fmt.Printf("It is highly recommended that 1, rather than 0, is used with -I for best results.\n")
		}
		audio_config.achan[0].il2p_invert_polarity = 0 // normal
	}

	if *il2pInverted >= 0 {
		audio_config.achan[0].layer2_xmit = C.LAYER2_IL2P
		if *il2pInverted > 0 {
			audio_config.achan[0].il2p_max_fec = 1
		}
		if audio_config.achan[0].il2p_max_fec == 0 {
			fmt.Printf("It is highly recommended that 1, rather than 0, is used with -i for best results.\n")
		}
		audio_config.achan[0].il2p_invert_polarity = 1 // invert for transmit
		if audio_config.achan[0].baud == 1200 {
			fmt.Printf("Using -i with 1200 bps is a bad idea.  Use -I instead.\n")
		}
	}

	// Done parsing, let's start doing!

	// TODO: control development/beta/release by version.h instead of changing here.
	// Print platform.  This will provide more information when people send a copy the information displayed.

	// Might want to print OS version here.   For Windows, see:
	// https://msdn.microsoft.com/en-us/library/ms724451(v=VS.85).aspx

	text_color_init(*textColor)
	printVersion(false)

	C.setlinebuf(C.stdout)
	setup_sigint_handler()

	/*
	 * Open the audio source
	 *	- soundcard
	 *	- stdin
	 *	- UDP
	 * Files not supported at this time.
	 * Can always "cat" the file and pipe it into stdin.
	 */
	deviceid_init()

	var err = audio_open(audio_config)
	if err < 0 {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Pointless to continue without audio device.\n")
		SLEEP_SEC(5)
		pflag.Usage()
		os.Exit(1)
	}

	/*
	 * Initialize the demodulator(s) and layer 2 decoder (HDLC, IL2P).
	 */
	multi_modem_init(audio_config)
	C.fx25_init(C.int(d_x_opt))
	il2p_init(C.int(d_2_opt))

	/*
	 * New in 1.8 - Allow a channel to be mapped to a network TNC rather than
	 * an internal modem and radio.
	 * I put it here so channel properties would come out in right order.
	 */
	nettnc_init(audio_config)

	/*
	 * Initialize the touch tone decoder & APRStt gateway.
	 */
	dtmf_init(audio_config, audio_amplitude)
	aprs_tt_init(&dw_tt_config, aprstt_debug)
	tt_user_init(audio_config, &dw_tt_config)

	/*
	 * Should there be an option for audio output level?
	 * Note:  This is not the same as a volume control you would see on the screen.
	 * It is the range of the digital sound representation.
	 */
	gen_tone_init(audio_config, audio_amplitude, 0)
	morse_init(audio_config, audio_amplitude)

	if !(audio_config.adev[0].bits_per_sample == 8 || audio_config.adev[0].bits_per_sample == 16) { //nolint:staticcheck
		panic("audio_config.adev[0].bits_per_sample == 8 || audio_config.adev[0].bits_per_sample == 16")
	}
	if !(audio_config.adev[0].num_channels == 1 || audio_config.adev[0].num_channels == 2) { //nolint:staticcheck
		panic("assert(audio_config.adev[0].num_channels == 1 || audio_config.adev[0].num_channels == 2)")
	}
	if !(audio_config.adev[0].samples_per_sec >= C.MIN_SAMPLES_PER_SEC && audio_config.adev[0].samples_per_sec <= C.MAX_SAMPLES_PER_SEC) { //nolint:staticcheck
		panic("assert(audio_config.adev[0].samples_per_sec >= MIN_SAMPLES_PER_SEC && audio_config.adev[0].samples_per_sec <= MAX_SAMPLES_PER_SEC)")
	}

	/*
	 * Initialize the transmit queue.
	 */

	xmit_init(audio_config, IfThenElse(d_p_opt, C.int(1), C.int(0)))

	/*
	 * If -x N option specified, transmit calibration tones for transmitter
	 * audio level adjustment, up to 1 minute then quit.
	 * a: Alternating mark/space tones
	 * m: Mark tone (e.g. 1200Hz)
	 * s: Space tone (e.g. 2200Hz)
	 * p: Set PTT only.
	 * A leading or trailing number is the channel.
	 */

	if *transmitCalibration != "" {
		var transmitCalibrationType = ' '
		var transmitCalibrationChannel int
		for _, p := range *transmitCalibration {
			switch p {
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				transmitCalibrationChannel = transmitCalibrationChannel*10 + int(p-'0')
				if transmitCalibrationType == ' ' {
					transmitCalibrationType = 'a'
				}
			case 'a':
				transmitCalibrationType = p // Alternating tones
			case 'm':
				transmitCalibrationType = p // Mark tone
			case 's':
				transmitCalibrationType = p // Space tone
			case 'p':
				transmitCalibrationType = p // Set PTT only
			default:
				text_color_set(DW_COLOR_ERROR)
				fmt.Printf("Invalid option '%c' for -x. Must be a, m, s, or p.\n", p)
				text_color_set(DW_COLOR_INFO)
				os.Exit(1)
			}
		}
		if transmitCalibrationChannel < 0 || transmitCalibrationChannel >= C.MAX_RADIO_CHANS {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Invalid channel %d for -x. \n", transmitCalibrationChannel)
			text_color_set(DW_COLOR_INFO)
			os.Exit(1)
		}

		if audio_config.chan_medium[transmitCalibrationChannel] == C.MEDIUM_RADIO {
			if audio_config.achan[transmitCalibrationChannel].mark_freq != 0 && audio_config.achan[transmitCalibrationChannel].space_freq != 0 {
				var max_duration = 60
				var n = audio_config.achan[transmitCalibrationChannel].baud * C.int(max_duration)

				text_color_set(DW_COLOR_INFO)
				ptt_set(C.OCTYPE_PTT, C.int(transmitCalibrationChannel), 1)

				switch transmitCalibrationType {
				default:
				case 'a': // Alternating tones: -x a
					fmt.Printf("\nSending alternating mark/space calibration tones (%d/%dHz) on channel %d.\nPress control-C to terminate.\n",
						audio_config.achan[transmitCalibrationChannel].mark_freq,
						audio_config.achan[transmitCalibrationChannel].space_freq,
						transmitCalibrationChannel)
					for n > 0 {
						tone_gen_put_bit(C.int(transmitCalibrationChannel), n&1)
						n--
					}
				case 'm': // "Mark" tone: -x m
					fmt.Printf("\nSending mark calibration tone (%dHz) on channel %d.\nPress control-C to terminate.\n",
						audio_config.achan[transmitCalibrationChannel].mark_freq, transmitCalibrationChannel)
					for n > 0 {
						tone_gen_put_bit(C.int(transmitCalibrationChannel), 1)
						n--
					}
				case 's': // "Space" tone: -x s
					fmt.Printf("\nSending space calibration tone (%dHz) on channel %d.\nPress control-C to terminate.\n",
						audio_config.achan[transmitCalibrationChannel].space_freq, transmitCalibrationChannel)
					for n > 0 {
						tone_gen_put_bit(C.int(transmitCalibrationChannel), 0)
						n--
					}
				case 'p': // Silence - set PTT only: -x p
					fmt.Printf("\nSending silence (Set PTT only) on channel %d.\nPress control-C to terminate.\n", transmitCalibrationChannel)
					SLEEP_SEC(max_duration)
				}

				ptt_set(C.OCTYPE_PTT, C.int(transmitCalibrationChannel), 0)
				text_color_set(DW_COLOR_INFO)
				os.Exit(0)
			} else {
				text_color_set(DW_COLOR_ERROR)
				fmt.Printf("\nMark/Space frequencies not defined for channel %d. Cannot calibrate using this modem type.\n", transmitCalibrationChannel)
				text_color_set(DW_COLOR_INFO)
				os.Exit(1)
			}
		} else {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("\nChannel %d is not configured as a radio channel.\n", transmitCalibrationChannel)
			text_color_set(DW_COLOR_INFO)
			os.Exit(1)
		}
	}

	/*
	 * Initialize the digipeater and IGate functions.
	 */
	digipeater_init(audio_config, &digi_config)
	igate_init(audio_config, &igate_config, &digi_config, C.int(d_i_opt))
	cdigipeater_init(audio_config, &cdigi_config)
	pfilter_init(&igate_config, d_f_opt)
	ax25_link_init(misc_config, C.int(d_c_opt))

	/*
	 * Provide the AGW & KISS socket interfaces for use by a client application.
	 */
	server_init(audio_config, misc_config)
	kissnet_init(misc_config)

	// TODO KG This checks `misc_config.kiss_port > 0` but `kiss_port` is now an array?
	// Let's just check [0] for now...
	if misc_config.kiss_port[0] > 0 && misc_config.dns_sd_enabled > 0 {
		dns_sd_announce(misc_config)
	}

	/*
	 * Create a pseudo terminal and KISS TNC emulator.
	 */
	kisspt_init(misc_config)
	kissserial_init(misc_config)
	kiss_frame_init(audio_config)

	/*
	 * Open port for communication with GPS.
	 */
	dwgps_init(misc_config, C.int(d_g_opt))

	waypoint_init(misc_config)

	/*
	 * Enable beaconing.
	 * Open log file first because "-dttt" (along with -l...) will
	 * log the tracker beacon transmissions with fake channel 999.
	 */

	log_init((misc_config.log_daily_names > 0), C.GoString(&misc_config.log_path[0]))
	mheard_init(d_m_opt)
	beacon_init(audio_config, misc_config, &igate_config)

	/*
	 * Get sound samples and decode them.
	 * Use hot attribute for all functions called for every audio sample.
	 */

	recv_init(audio_config)
	recv_process()
}

/*-------------------------------------------------------------------
 *
 * Name:        app_process_rec_frame
 *
 * Purpose:     This is called when we receive a frame with a valid
 *		FCS and acceptable size.
 *
 * Inputs:	chan	- Audio channel number, 0 or 1.
 *		subchan	- Which modem caught it.
 *			  Special cases:
 *				-1 for DTMF decoder.
 *				-2 for channel mapped to APRS-IS.
 *				-3 for channel mapped to network TNC.
 *		slice	- Slicer which caught it.
 *		pp	- Packet handle.
 *		alevel	- Audio level, range of 0 - 100.
 *				(Special case, use negative to skip
 *				 display of audio level line.
 *				 Use -2 to indicate DTMF message.)
 *		retries	- Level of bit correction used.
 *		spectrum - Display of how well multiple decoders did.
 *
 *
 * Description:	Print decoded packet.
 *		Optionally send to another application.
 *
 *--------------------------------------------------------------------*/

// TODO:  Use only one printf per line so output doesn't get jumbled up with stuff from other threads.

func app_process_rec_packet(channel C.int, subchan C.int, slice C.int, pp C.packet_t, alevel C.alevel_t, fec_type C.fec_type_t, retries C.retry_t, spectrum string) {
	/* FIXME KG
	assert (chan >= 0 && chan < MAX_TOTAL_CHANS);		// TOTAL for virtual channels
	assert (subchan >= -3 && subchan < MAX_SUBCHANS);
	assert (slice >= 0 && slice < MAX_SLICERS);
	assert (pp != NULL);	// 1.1J+
	*/

	// Extra stuff before slice indicators.
	// Can indicate FX.25/IL2P or fix_bits.
	var display_retries string

	switch fec_type {
	case C.fec_type_fx25:
		display_retries = " FX.25 "
	case C.fec_type_il2p:
		display_retries = " IL2P "
	default:
		// Possible fix_bits indication.
		if audio_config.achan[channel].fix_bits != C.RETRY_NONE || audio_config.achan[channel].passall > 0 {
			// FIXME KG assert(retries >= C.RETRY_NONE && retries <= C.RETRY_MAX)
			display_retries = fmt.Sprintf(" [%s] ", retry_text[int(retries)])
		}
	}

	var stemp [500]C.char
	ax25_format_addrs(pp, &stemp[0])

	var pinfo *C.uchar
	var info_len = ax25_get_info(pp, &pinfo)

	/* Print so we can see what is going on. */

	/* Display audio input level. */
	/* Who are we hearing?   Original station or digipeater. */

	var h C.int
	var heard [C.AX25_MAX_ADDR_LEN]C.char
	if ax25_get_num_addr(pp) == 0 {
		/* Not AX.25. No station to display below. */
		h = -1
	} else {
		h = ax25_get_heard(pp)
		ax25_get_addr_with_ssid(pp, h, &heard[0])
	}

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("\n")

	// The HEARD line.

	if !q_h_opt && alevel.rec >= 0 { /* suppress if "-q h" option */
		// FIXME: rather than checking for ichannel, how about checking medium==radio
		if channel != audio_config.igate_vchannel { // suppress if from ICHANNEL
			if h != -1 && h != C.AX25_SOURCE {
				dw_printf("Digipeater ")
			}

			var alevel_text [C.AX25_ALEVEL_TO_TEXT_SIZE]C.char

			ax25_alevel_to_text(alevel, &alevel_text[0])

			// Experiment: try displaying the DC bias.
			// Should be 0 for soundcard but could show mistuning with SDR.

			/*
			  char bias[16];
			  snprintf (bias, sizeof(bias), " DC%+d", multi_modem_get_dc_average (channel));
			  strlcat (alevel_text, bias, sizeof(alevel_text));
			*/

			/* As suggested by KJ4ERJ, if we are receiving from */
			/* WIDEn-0, it is quite likely (but not guaranteed), that */
			/* we are actually hearing the preceding station in the path. */

			var _heard = C.GoString(&heard[0]) // TODO Quick convenience hack
			if h >= C.AX25_REPEATER_2 &&
				strings.EqualFold(_heard[:4], "WIDE") &&
				unicode.IsDigit(rune(_heard[4])) &&
				len(_heard) == 5 {
				var probably_really [C.AX25_MAX_ADDR_LEN]C.char
				ax25_get_addr_with_ssid(pp, h-1, &probably_really[0])

				// audio level applies only for internal modem channels.
				if subchan >= 0 {
					dw_printf("%s (probably %s) audio level = %s  %s  %s\n", _heard, C.GoString(&probably_really[0]), C.GoString(&alevel_text[0]), display_retries, spectrum)
				} else {
					dw_printf("%s (probably %s)\n", _heard, C.GoString(&probably_really[0]))
				}
			} else if _heard == "DTMF" {
				dw_printf("%s audio level = %s  tt\n", _heard, C.GoString(&alevel_text[0]))
			} else {
				// audio level applies only for internal modem channels.
				if subchan >= 0 {
					dw_printf("%s audio level = %s  %s  %s\n", _heard, C.GoString(&alevel_text[0]), display_retries, spectrum)
				} else {
					dw_printf("%s\n", _heard)
				}
			}
		}
	}

	/* Version 1.2:   Cranking the input level way up produces 199. */
	/* Keeping it under 100 gives us plenty of headroom to avoid saturation. */

	// TODO:  suppress this message if not using soundcard input.
	// i.e. we have no control over the situation when using SDR.

	if alevel.rec > 110 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Audio input level is too high. This may cause distortion and reduced decode performance.\n")
		dw_printf("Solution is to decrease the audio input level.\n")
		dw_printf("Setting audio input level so most stations are around 50 will provide good dyanmic range.\n")
	} else if alevel.rec < 5 && channel != audio_config.igate_vchannel && subchan != -3 {
		// FIXME: rather than checking for ichannel, how about checking medium==radio
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Audio input level is too low.  Increase so most stations are around 50.\n")
	}

	// Display non-APRS packets in a different color.

	// Display subchannel only when multiple modems configured for channel.

	// -1 for APRStt DTMF decoder.

	var ts string // optional time stamp

	if C.strlen(&audio_config.timestamp_format[0]) > 0 {
		var formattedTime, _ = strftime.Format(C.GoString(&audio_config.timestamp_format[0]), time.Now())
		ts = " " + formattedTime // space after channel.
	}

	switch subchan {
	case -1: // dtmf
		text_color_set(DW_COLOR_REC)
		dw_printf("[%d.dtmf%s] ", channel, ts)
	case -2: // APRS-IS
		text_color_set(DW_COLOR_REC)
		dw_printf("[%d.is%s] ", channel, ts)
	case -3: // nettnc
		text_color_set(DW_COLOR_REC)
		dw_printf("[%d%s] ", channel, ts)
	default:
		if ax25_is_aprs(pp) > 0 {
			text_color_set(DW_COLOR_REC)
		} else {
			text_color_set(DW_COLOR_DECODED)
		}

		if audio_config.achan[channel].num_subchan > 1 && audio_config.achan[channel].num_slicers == 1 {
			dw_printf("[%d.%d%s] ", channel, subchan, ts)
		} else if audio_config.achan[channel].num_subchan == 1 && audio_config.achan[channel].num_slicers > 1 {
			dw_printf("[%d.%d%s] ", channel, slice, ts)
		} else if audio_config.achan[channel].num_subchan > 1 && audio_config.achan[channel].num_slicers > 1 {
			dw_printf("[%d.%d.%d%s] ", channel, subchan, slice, ts)
		} else {
			dw_printf("[%d%s] ", channel, ts)
		}
	}

	dw_printf("%s", C.GoString(&stemp[0])) /* stations followed by : */

	/* Demystify non-APRS.  Use same format for transmitted frames in xmit.c. */

	var asciiOnly C.int = 0 // Quick bodge because these C bools are ints...
	if (ax25_is_aprs(pp) == 0) && !d_u_opt {
		asciiOnly = 1
	}

	if ax25_is_aprs(pp) == 0 {
		var cr C.cmdres_t
		var desc [80]C.char
		var pf, nr, ns C.int

		var ftype = ax25_frame_type(pp, &cr, &desc[0], &pf, &nr, &ns)

		/* Could change by 1, since earlier call, if we guess at modulo 128. */
		info_len = ax25_get_info(pp, &pinfo)

		dw_printf("(%s)", C.GoString(&desc[0]))
		if ftype == C.frame_type_U_XID {
			var param xid_param_s
			var info2text [150]C.char

			xid_parse(pinfo, info_len, &param, &info2text[0], C.int(len(info2text)))
			dw_printf(" %s\n", C.GoString(&info2text[0]))
		} else {
			ax25_safe_print((*C.char)(unsafe.Pointer(pinfo)), info_len, asciiOnly)
			dw_printf("\n")
		}
	} else {
		// for APRS we generally want to display non-ASCII to see UTF-8.
		// for other, probably want to restrict to ASCII only because we are
		// more likely to have compressed data than UTF-8 text.

		// TODO: Might want to use d_u_opt for transmitted frames too.

		ax25_safe_print((*C.char)(unsafe.Pointer(pinfo)), info_len, asciiOnly)
		dw_printf("\n")
	}

	// Also display in pure ASCII if non-ASCII characters and "-d u" option specified.

	if d_u_opt {
		var hasNonPrintable = false
		for _, r := range C.GoString((*C.char)(unsafe.Pointer(pinfo))) {
			if !unicode.IsPrint(r) {
				hasNonPrintable = true
				break
			}
		}

		if hasNonPrintable {
			text_color_set(DW_COLOR_DEBUG)
			ax25_safe_print((*C.char)(unsafe.Pointer(pinfo)), info_len, 1)
			dw_printf("\n")
		}
	}

	/* Optional hex dump of packet. */

	if d_p_opt {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		ax25_hex_dump(pp)
		dw_printf("------\n")
	}

	/*
	 * Decode the contents of UI frames and display in human-readable form.
	 * Could be APRS or anything random for old fashioned packet beacons.
	 *
	 * Suppress printed decoding if "-q d" option used.
	 */
	var ais_obj_packet [300]C.char

	if ax25_is_aprs(pp) > 0 {
		var A C.decode_aprs_t

		// we still want to decode it for logging and other processing.
		// Just be quiet about errors if "-qd" is set.

		decode_aprs(&A, pp, IfThenElse(q_d_opt, C.int(1), C.int(0)), nil)

		if !q_d_opt {
			// Print it all out in human readable format unless "-q d" option used.

			decode_aprs_print(&A)
		}

		/*
		 * Perform validity check on each address.
		 * This should print an error message if any issues.
		 */
		ax25_check_addresses(pp)

		// Send to log file.

		log_write(int(channel), &A, pp, alevel, retries)

		// temp experiment.
		// log_rr_bits (&A, pp);

		// Add to list of stations heard over the radio.

		mheard_save_rf(channel, &A, pp, alevel, retries)

		// For AIS, we have an option to convert the NMEA format, in User Defined data,
		// into an APRS "Object Report" and send that to the clients as well.

		// FIXME: partial implementation.

		var user_def_da = C.CString("{" + string(C.USER_DEF_USER_ID) + string(C.USER_DEF_TYPE_AIS))

		if C.strncmp((*C.char)(unsafe.Pointer(pinfo)), user_def_da, 3) == 0 {
			waypoint_send_ais([]byte(C.GoString((*C.char)(unsafe.Pointer(pinfo)))[3:]))

			if A_opt_ais_to_obj && A.g_lat != G_UNKNOWN && A.g_lon != G_UNKNOWN {
				var ais_obj_info = encode_object(&A.g_name[0], 0, C.time(nil),
					A.g_lat, A.g_lon, 0, // no ambiguity
					A.g_symbol_table, A.g_symbol_code,
					0, 0, 0, C.CString(""), // power, height, gain, direction.
					// Unknown not handled properly.
					// Should encode_object take floating point here?
					C.int(A.g_course+0.5), C.int(DW_MPH_TO_KNOTS(float64(A.g_speed_mph))+0.5),
					0, 0, 0, &A.g_comment[0]) // freq, tone, offset

				// TODO Bodge
				var _ais_obj_packet = fmt.Sprintf("%s>%s%1d%1d,NOGATE:%s", C.GoString(&A.g_src[0]), C.APP_TOCALL, C.MAJOR_VERSION, C.MINOR_VERSION, ais_obj_info)
				C.strcpy(&ais_obj_packet[0], C.CString(_ais_obj_packet))

				dw_printf("[%d.AIS] %s\n", channel, _ais_obj_packet)

				// This will be sent to client apps after the User Defined Data representation.
			}
		}

		// Convert to NMEA waypoint sentence if we have a location.

		if A.g_lat != G_UNKNOWN && A.g_lon != G_UNKNOWN {
			var nameIn = &A.g_src[0]
			if C.strlen(&A.g_name[0]) > 0 {
				nameIn = &A.g_name[0]
			}

			waypoint_send_sentence(C.GoString(nameIn),
				float64(A.g_lat), float64(A.g_lon), rune(A.g_symbol_table), byte(A.g_symbol_code),
				DW_FEET_TO_METERS(float64(A.g_altitude_ft)), float64(A.g_course), DW_MPH_TO_KNOTS(float64(A.g_speed_mph)),
				C.GoString(&A.g_comment[0]))
		}
	}

	/* Send to another application if connected. */
	// TODO:  Put a wrapper around this so we only call one function to send by all methods.
	// We see the same sequence in tt_user.c.

	var fbuf [C.AX25_MAX_PACKET_LEN]C.uchar
	var flen = ax25_pack(pp, &fbuf[0])

	server_send_rec_packet(channel, pp, &fbuf[0], flen)                                                                // AGW net protocol
	kissnet_send_rec_packet(channel, KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&fbuf[0]), flen), flen, nil, -1)    // KISS TCP
	kissserial_send_rec_packet(channel, KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&fbuf[0]), flen), flen, nil, -1) // KISS serial port
	kisspt_send_rec_packet(channel, KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&fbuf[0]), flen), flen, nil, -1)     // KISS pseudo terminal

	if A_opt_ais_to_obj && C.strlen(&ais_obj_packet[0]) != 0 {
		var ao_pp = ax25_from_text(&ais_obj_packet[0], 1)
		if ao_pp != nil {
			var ao_fbuf [C.AX25_MAX_PACKET_LEN]C.uchar
			var ao_flen = ax25_pack(ao_pp, &ao_fbuf[0])

			server_send_rec_packet(channel, ao_pp, &ao_fbuf[0], ao_flen)
			kissnet_send_rec_packet(channel, KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&ao_fbuf[0]), ao_flen), ao_flen, nil, -1)
			kissserial_send_rec_packet(channel, KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&ao_fbuf[0]), ao_flen), ao_flen, nil, -1)
			kisspt_send_rec_packet(channel, KISS_CMD_DATA_FRAME, C.GoBytes(unsafe.Pointer(&ao_fbuf[0]), ao_flen), ao_flen, nil, -1)
			ax25_delete(ao_pp)
		}
	}

	/*
	 * If it is from the ICHANNEL, we are done.
	 * Don't digipeat.  Don't IGate.
	 * Don't do anything with it after printing and sending to client apps.
	 */

	if channel == audio_config.igate_vchannel {
		return
	}

	/*
	 * If it came from DTMF decoder (subchan == -1), send it to APRStt gateway.
	 * Otherwise, it is a candidate for IGate and digipeater.
	 *
	 * It is also useful to have some way to simulate touch tone
	 * sequences with BEACON sendto=R0 for testing.
	 */

	if subchan == -1 { // from DTMF decoder
		if dw_tt_config.gateway_enabled > 0 && info_len >= 2 {
			aprs_tt_sequence(int(channel), C.GoString((*C.char)(unsafe.Pointer(pinfo)))[1:])
		}
	} else if *pinfo == 't' && info_len >= 2 && dw_tt_config.gateway_enabled > 0 {
		// For testing.
		// Would be nice to verify it was generated locally,
		// not received over the air.
		aprs_tt_sequence(int(channel), C.GoString((*C.char)(unsafe.Pointer(pinfo)))[1:])
	} else {
		/*
		 * Send to the IGate processing.
		 * Use only those with correct CRC; We don't want to spread corrupted data!
		 * Our earlier "fix bits" hack could allow corrupted information to get thru.
		 * However, if it used FEC mode (FX.25. IL2P), we have much higher level of
		 * confidence that it is correct.
		 */
		if ax25_is_aprs(pp) > 0 && (retries == C.RETRY_NONE || fec_type == C.fec_type_fx25 || fec_type == C.fec_type_il2p) {
			igate_send_rec_packet(channel, pp)
		}

		/* Send out a regenerated copy. Applies to all types, not just APRS. */
		/* This was an experimental feature never documented in the User Guide. */
		/* Initial feedback was positive but it fell by the wayside. */
		/* Should follow up with testers and either document this or clean out the clutter. */

		digi_regen(channel, pp)

		/*
		 * Send to APRS digipeater.
		 * Use only those with correct CRC; We don't want to spread corrupted data!
		 * Our earlier "fix bits" hack could allow corrupted information to get thru.
		 * However, if it used FEC mode (FX.25. IL2P), we have much higher level of
		 * confidence that it is correct.
		 */
		if ax25_is_aprs(pp) > 0 && (retries == C.RETRY_NONE || fec_type == C.fec_type_fx25 || fec_type == C.fec_type_il2p) {
			digipeater(channel, pp)
		}

		/*
		 * Connected mode digipeater.
		 * Use only those with correct CRC (or using FEC.)
		 */

		if channel < C.MAX_RADIO_CHANS {
			if retries == C.RETRY_NONE || fec_type == C.fec_type_fx25 || fec_type == C.fec_type_il2p {
				cdigipeater(channel, pp)
			}
		}
	}
} /* end app_process_rec_packet */

func setup_sigint_handler() {
	var sigChan = make(chan os.Signal, 1)

	signal.Notify(sigChan, syscall.SIGINT)

	go func() {
		<-sigChan
		cleanup()
	}()
}

func cleanup() {
	text_color_set(DW_COLOR_INFO)
	dw_printf("\nQRT\n")
	log_term()
	ptt_term()
	dwgps_term()
	SLEEP_SEC(1)
	os.Exit(0)
}
