package main

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
// #include <sys/soundcard.h>
// #include <sys/socket.h>
// #include <netinet/in.h>
// #include <netdb.h>
// #include <hamlib/rig.h>
// #include "version.h"
// #include "audio.h"
// #include "config.h"
// #include "multi_modem.h"
// #include "demod.h"
// #include "hdlc_rec.h"
// #include "hdlc_rec2.h"
// #include "ax25_pad.h"
// #include "xid.h"
// #include "decode_aprs.h"
// #include "encode_aprs.h"
// #include "textcolor.h"
// #include "server.h"
// #include "kiss.h"
// #include "kissnet.h"
// #include "kissserial.h"
// #include "kiss_frame.h"
// #include "waypoint.h"
// #include "gen_tone.h"
// #include "digipeater.h"
// #include "cdigipeater.h"
// #include "tq.h"
// #include "xmit.h"
// #include "ptt.h"
// #include "beacon.h"
// #include "dtmf.h"
// #include "aprs_tt.h"
// #include "tt_user.h"
// #include "igate.h"
// #include "pfilter.h"
// #include "symbols.h"
// #include "dwgps.h"
// #include "waypoint.h"
// #include "log.h"
// #include "recv.h"
// #include "morse.h"
// #include "mheard.h"
// #include "ax25_link.h"
// #include "dtime_now.h"
// #include "fx25.h"
// #include "il2p.h"
// #include "dwsock.h"
// #include "dns_sd_dw.h"
// #include "dlq.h"		// for fec_type_t definition.
// #include "deviceid.h"
// #include "nettnc.h"
// void setup_sigint_handler();
// extern struct audio_s audio_config;
// extern struct tt_config_s tt_config;
// extern struct misc_config_s misc_config;
// extern int d_p_opt;
// extern int d_u_opt;
// extern int q_h_opt;
// extern int q_d_opt;
// extern int A_opt_ais_to_obj;
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0 -DUSE_AVAHI_CLIENT
import "C"

import (
	"fmt"
	"os"
	"strconv"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

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

func main() {
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

	if *printUTF8Test {
		fmt.Printf("\n  UTF-8 test string: ma%c%cana %c%c F%c%c%c%ce\n\n",
			0xc3, 0xb1,
			0xc2, 0xb0,
			0xc3, 0xbc, 0xc3, 0x9f)

		os.Exit(0)
	}

	if *symbolDump {
		C.symbols_init()
		C.symbols_list()
		os.Exit(0)
	}

	if *aisToAPRS {
		C.A_opt_ais_to_obj = 1
	}

	var d_k_opt C.int = 0 /* "-d k" option for serial port KISS.  Can be repeated for more detail. */
	var d_n_opt C.int = 0 /* "-d n" option for Network KISS.  Can be repeated for more detail. */
	var d_t_opt C.int = 0 /* "-d t" option for Tracker.  Can be repeated for more detail. */
	var d_g_opt = 0       /* "-d g" option for GPS. Can be repeated for more detail. */
	var d_o_opt C.int = 0 /* "-d o" option for output control such as PTT and DCD. */
	var d_i_opt = 0       /* "-d i" option for IGate.  Repeat for more detail */
	var d_m_opt = 0       /* "-d m" option for mheard list. */
	var d_f_opt = 0       /* "-d f" option for filtering.  Repeat for more detail. */
	var d_h_opt = 0       /* "-d h" option for hamlib debugging.  Repeat for more detail */
	var d_x_opt = 1       /* "-d x" option for FX.25.  Default minimal. Repeat for more detail.  -qx to silence. */
	var d_2_opt = 0       /* "-d 2" option for IL2P.  Default minimal. Repeat for more detail. */
	var d_c_opt = 0       /* "-d c" option for connected mode data link state machine. */
	var aprstt_debug = 0  /* "-d d" option for APRStt (think Dtmf) debug. */

	if *debugStr != "" {
		// New in 1.1.  Can combine multiple such as "-d pkk"

		for _, p := range *debugStr {
			switch p {
			case 'a':
				C.server_set_debug(1)
			case 'k':
				d_k_opt++
				C.kissserial_set_debug(d_k_opt)
				C.kisspt_set_debug(d_k_opt)
			case 'n':
				d_n_opt++
				C.kiss_net_set_debug(d_n_opt)
			case 'u':
				C.d_u_opt = 1
				// separate out gps & waypoints.
			case 'g':
				d_g_opt++
			case 'w':
				C.waypoint_set_debug(1) // not documented yet.
			case 't':
				d_t_opt++
				C.beacon_tracker_set_debug(d_t_opt)
			case 'p':
				C.d_p_opt = 1 // TODO: packet dump for xmit side.
			case 'o':
				d_o_opt++
				C.ptt_set_debug(d_o_opt)
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
				C.q_h_opt = 1
			case 'd':
				C.q_d_opt = 1
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

	C.symbols_init()

	C.dwsock_init()

	var digi_config C.struct_digi_config_s
	var cdigi_config C.struct_cdigi_config_s
	var igate_config C.struct_igate_config_s

	C.config_init(C.CString(*configFileName), &C.audio_config, &digi_config, &cdigi_config, &C.tt_config, &igate_config, &C.misc_config)

	if *audioSampleRate != 0 {
		if *audioSampleRate < C.MIN_SAMPLES_PER_SEC || *audioSampleRate > C.MAX_SAMPLES_PER_SEC {
			fmt.Printf("-r option, audio samples/sec, is out of range.\n")
			os.Exit(1)
		}
		C.audio_config.adev[0].samples_per_sec = C.int(*audioSampleRate)
	}

	if *audioChannels != 0 {
		if *audioChannels < 1 || *audioChannels > 2 {
			fmt.Printf("-n option, number of audio channels, is out of range.\n")
			os.Exit(1)
		}
		C.audio_config.adev[0].num_channels = C.int(*audioChannels)
		if *audioChannels == 2 {
			C.audio_config.chan_medium[1] = C.MEDIUM_RADIO
		}
	}

	if *bitsPerSample != 0 {
		if *bitsPerSample != 8 && *bitsPerSample != 16 {
			fmt.Printf("-b option, bits per sample, must be 8 or 16.\n")
			os.Exit(1)
		}

		C.audio_config.adev[0].bits_per_sample = C.int(*bitsPerSample)
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

		C.audio_config.achan[0].baud = C.int(bitrate)

		/* We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c, */
		/* that need to be kept in sync.  Maybe it could be a common function someday. */

		if C.audio_config.achan[0].baud < 600 {
			C.audio_config.achan[0].modem_type = C.MODEM_AFSK
			C.audio_config.achan[0].mark_freq = 1600 // Typical for HF SSB.
			C.audio_config.achan[0].space_freq = 1800
			C.audio_config.achan[0].decimate = 3 // Reduce CPU load.
		} else if C.audio_config.achan[0].baud < 1800 {
			C.audio_config.achan[0].modem_type = C.MODEM_AFSK
			C.audio_config.achan[0].mark_freq = C.DEFAULT_MARK_FREQ
			C.audio_config.achan[0].space_freq = C.DEFAULT_SPACE_FREQ
		} else if C.audio_config.achan[0].baud < 3600 {
			C.audio_config.achan[0].modem_type = C.MODEM_QPSK
			C.audio_config.achan[0].mark_freq = 0
			C.audio_config.achan[0].space_freq = 0
			if C.audio_config.achan[0].baud != 2400 {
				fmt.Printf("Bit rate should be standard 2400 rather than specified %d.\n", C.audio_config.achan[0].baud)
			}
		} else if C.audio_config.achan[0].baud < 7200 {
			C.audio_config.achan[0].modem_type = C.MODEM_8PSK
			C.audio_config.achan[0].mark_freq = 0
			C.audio_config.achan[0].space_freq = 0
			if C.audio_config.achan[0].baud != 4800 {
				fmt.Printf("Bit rate should be standard 4800 rather than specified %d.\n", C.audio_config.achan[0].baud)
			}
		} else if C.audio_config.achan[0].baud == 0xA15A15 {
			C.audio_config.achan[0].modem_type = C.MODEM_AIS
			C.audio_config.achan[0].baud = 9600
			C.audio_config.achan[0].mark_freq = 0
			C.audio_config.achan[0].space_freq = 0
		} else if C.audio_config.achan[0].baud == 0xEA5EA5 {
			C.audio_config.achan[0].modem_type = C.MODEM_EAS
			C.audio_config.achan[0].baud = 521 // Actually 520.83 but we have an integer field here.
			// Will make more precise in afsk demod init.
			C.audio_config.achan[0].mark_freq = 2083  // Actually 2083.3 - logic 1.
			C.audio_config.achan[0].space_freq = 1563 // Actually 1562.5 - logic 0.
			C.strcpy(&C.audio_config.achan[0].profiles[0], C.CString("A"))
		} else {
			C.audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
			C.audio_config.achan[0].mark_freq = 0
			C.audio_config.achan[0].space_freq = 0
		}
	}

	if *g3ruh {
		// Force G3RUH mode, overriding default for speed.
		//   Example:   -B 2400 -g

		C.audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
		C.audio_config.achan[0].mark_freq = 0
		C.audio_config.achan[0].space_freq = 0
	}

	if *direwolf15compat {
		// V.26 compatible with earlier versions of direwolf.
		//   Example:   -B 2400 -j    or simply   -j

		C.audio_config.achan[0].v26_alternative = C.V26_A
		C.audio_config.achan[0].modem_type = C.MODEM_QPSK
		C.audio_config.achan[0].mark_freq = 0
		C.audio_config.achan[0].space_freq = 0
		C.audio_config.achan[0].baud = 2400
	}

	if *mfj2400compat {
		// V.26 compatible with MFJ and maybe others.
		//   Example:   -B 2400 -J     or simply   -J

		C.audio_config.achan[0].v26_alternative = C.V26_B
		C.audio_config.achan[0].modem_type = C.MODEM_QPSK
		C.audio_config.achan[0].mark_freq = 0
		C.audio_config.achan[0].space_freq = 0
		C.audio_config.achan[0].baud = 2400
	}

	if *audioStatsInterval > 0 {
		if *audioStatsInterval < 10 {
			fmt.Printf("Setting such a small audio statistics interval (<10) will produce inaccurate sample rate display.\n")
		}
		C.audio_config.statistics_interval = C.int(*audioStatsInterval)
	}

	if *modemProfile != "" {
		/* -P for modem profile. */
		C.strcpy(&C.audio_config.achan[0].profiles[0], C.CString(*modemProfile))
	}

	if *decimate != 0 {
		if *decimate < 1 || *decimate > 8 {
			fmt.Printf("Crazy value for -D. \n")
			os.Exit(1)
		}

		// Reduce audio sampling rate to reduce CPU requirements.
		C.audio_config.achan[0].decimate = C.int(*decimate)
	}

	if *upsample != 0 {
		if *upsample < 1 || *upsample > 4 {
			fmt.Printf("Crazy value for -U. \n")
			os.Exit(1)
		}

		// Increase G3RUH audio sampling rate to improve performance.
		// The value is normally determined automatically based on audio
		// sample rate and baud.  This allows override for experimentation.
		C.audio_config.achan[0].upsample = C.int(*upsample)
	}

	C.strcpy(&C.audio_config.timestamp_format[0], C.CString(*timestampFormat))

	// temp - only xmit errors.

	if *errorRateStr != "" {
		var e = *errorRateStr
		if e[0] == 'r' || e[0] == 'R' {
			var E_rx_opt, _ = strconv.Atoi(e[1:])
			if E_rx_opt < 1 || E_rx_opt > 99 {
				fmt.Printf("-ER must be in range of 1 to 99.\n")
				E_rx_opt = 10
			}
			C.audio_config.recv_error_rate = C.int(E_rx_opt)
		} else {
			var E_tx_opt, _ = strconv.Atoi(e)
			if E_tx_opt < 1 || E_tx_opt > 99 {
				fmt.Printf("-E must be in range of 1 to 99.\n")
				E_tx_opt = 10
			}
			C.audio_config.xmit_error_rate = C.int(E_tx_opt)
		}
	}

	if *logDir != "" && *logFile != "" {
		fmt.Printf("Logging options -l and -L can't be used together.  Pick one or the other.\n")
		os.Exit(1)
	}

	if *logFile != "" {
		C.misc_config.log_daily_names = 0
		C.strcpy(&C.misc_config.log_path[0], C.CString(*logFile))
	} else if *logDir != "" {
		C.misc_config.log_daily_names = 1
		C.strcpy(&C.misc_config.log_path[0], C.CString(*logDir))
	}

	if *enablePseudoTerminal {
		C.misc_config.enable_kiss_pt = 1
	}

	if input_file != "" {
		C.strcpy(&C.audio_config.adev[0].adevice_in[0], C.CString(input_file))
	}

	C.audio_config.recv_ber = C.float(*bitErrorRate)

	if *fx25CheckBytes > 0 {
		if *il2pNormal != -1 || *il2pInverted != -1 {
			fmt.Printf("Can't mix -X with -I or -i.\n")
			os.Exit(1)
		}
		C.audio_config.achan[0].fx25_strength = C.int(*fx25CheckBytes)
		C.audio_config.achan[0].layer2_xmit = C.LAYER2_FX25
	}

	if *il2pNormal != -1 && *il2pInverted != -1 {
		fmt.Printf("Can't use both -I and -i at the same time.\n")
		os.Exit(1)
	}

	if *il2pNormal >= 0 {
		C.audio_config.achan[0].layer2_xmit = C.LAYER2_IL2P
		if *il2pNormal > 0 {
			C.audio_config.achan[0].il2p_max_fec = 1
		}
		if C.audio_config.achan[0].il2p_max_fec == 0 {
			fmt.Printf("It is highly recommended that 1, rather than 0, is used with -I for best results.\n")
		}
		C.audio_config.achan[0].il2p_invert_polarity = 0 // normal
	}

	if *il2pInverted >= 0 {
		C.audio_config.achan[0].layer2_xmit = C.LAYER2_IL2P
		if *il2pInverted > 0 {
			C.audio_config.achan[0].il2p_max_fec = 1
		}
		if C.audio_config.achan[0].il2p_max_fec == 0 {
			fmt.Printf("It is highly recommended that 1, rather than 0, is used with -i for best results.\n")
		}
		C.audio_config.achan[0].il2p_invert_polarity = 1 // invert for transmit
		if C.audio_config.achan[0].baud == 1200 {
			fmt.Printf("Using -i with 1200 bps is a bad idea.  Use -I instead.\n")
		}
	}

	// Done parsing, let's start doing!

	// TODO: control development/beta/release by version.h instead of changing here.
	// Print platform.  This will provide more information when people send a copy the information displayed.

	// Might want to print OS version here.   For Windows, see:
	// https://msdn.microsoft.com/en-us/library/ms724451(v=VS.85).aspx

	C.text_color_init(C.int(*textColor))
	C.text_color_set(C.DW_COLOR_INFO)
	// fmt.Printf ("Dire Wolf version %d.%d (%s) BETA TEST 7\n", MAJOR_VERSION, MINOR_VERSION, __DATE__);
	fmt.Printf("Dire Wolf DEVELOPMENT version %d.%d %s (%s)\n", C.MAJOR_VERSION, C.MINOR_VERSION, "D", C.__DATE__)
	// fmt.Printf ("Dire Wolf version %d.%d\n", MAJOR_VERSION, MINOR_VERSION);

	C.setlinebuf(C.stdout)
	C.setup_sigint_handler()

	/*
	 * Open the audio source
	 *	- soundcard
	 *	- stdin
	 *	- UDP
	 * Files not supported at this time.
	 * Can always "cat" the file and pipe it into stdin.
	 */
	C.deviceid_init()

	var err = C.audio_open(&C.audio_config)
	if err < 0 {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("Pointless to continue without audio device.\n")
		direwolf.SLEEP_SEC(5)
		pflag.Usage()
		os.Exit(1)
	}

	/*
	 * Initialize the demodulator(s) and layer 2 decoder (HDLC, IL2P).
	 */
	C.multi_modem_init(&C.audio_config)
	C.fx25_init(C.int(d_x_opt))
	C.il2p_init(C.int(d_2_opt))

	/*
	 * New in 1.8 - Allow a channel to be mapped to a network TNC rather than
	 * an internal modem and radio.
	 * I put it here so channel properties would come out in right order.
	 */
	C.nettnc_init(&C.audio_config)

	/*
	 * Initialize the touch tone decoder & APRStt gateway.
	 */
	C.dtmf_init(&C.audio_config, audio_amplitude)
	C.aprs_tt_init(&C.tt_config, C.int(aprstt_debug))
	C.tt_user_init(&C.audio_config, &C.tt_config)

	/*
	 * Should there be an option for audio output level?
	 * Note:  This is not the same as a volume control you would see on the screen.
	 * It is the range of the digital sound representation.
	 */
	C.gen_tone_init(&C.audio_config, audio_amplitude, 0)
	C.morse_init(&C.audio_config, audio_amplitude)

	if !(C.audio_config.adev[0].bits_per_sample == 8 || C.audio_config.adev[0].bits_per_sample == 16) { //nolint:staticcheck
		panic("audio_config.adev[0].bits_per_sample == 8 || C.audio_config.adev[0].bits_per_sample == 16")
	}
	if !(C.audio_config.adev[0].num_channels == 1 || C.audio_config.adev[0].num_channels == 2) { //nolint:staticcheck
		panic("assert(C.audio_config.adev[0].num_channels == 1 || C.audio_config.adev[0].num_channels == 2)")
	}
	if !(C.audio_config.adev[0].samples_per_sec >= C.MIN_SAMPLES_PER_SEC && C.audio_config.adev[0].samples_per_sec <= C.MAX_SAMPLES_PER_SEC) { //nolint:staticcheck
		panic("assert(C.audio_config.adev[0].samples_per_sec >= MIN_SAMPLES_PER_SEC && C.audio_config.adev[0].samples_per_sec <= MAX_SAMPLES_PER_SEC)")
	}

	/*
	 * Initialize the transmit queue.
	 */

	C.xmit_init(&C.audio_config, C.d_p_opt)

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
				C.text_color_set(C.DW_COLOR_ERROR)
				fmt.Printf("Invalid option '%c' for -x. Must be a, m, s, or p.\n", p)
				C.text_color_set(C.DW_COLOR_INFO)
				os.Exit(1)
			}
		}
		if transmitCalibrationChannel < 0 || transmitCalibrationChannel >= C.MAX_RADIO_CHANS {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Invalid channel %d for -x. \n", transmitCalibrationChannel)
			C.text_color_set(C.DW_COLOR_INFO)
			os.Exit(1)
		}

		if C.audio_config.chan_medium[transmitCalibrationChannel] == C.MEDIUM_RADIO {
			if C.audio_config.achan[transmitCalibrationChannel].mark_freq != 0 && C.audio_config.achan[transmitCalibrationChannel].space_freq != 0 {
				var max_duration = 60
				var n = C.audio_config.achan[transmitCalibrationChannel].baud * C.int(max_duration)

				C.text_color_set(C.DW_COLOR_INFO)
				C.ptt_set(C.OCTYPE_PTT, C.int(transmitCalibrationChannel), 1)

				switch transmitCalibrationType {
				default:
				case 'a': // Alternating tones: -x a
					fmt.Printf("\nSending alternating mark/space calibration tones (%d/%dHz) on channel %d.\nPress control-C to terminate.\n",
						C.audio_config.achan[transmitCalibrationChannel].mark_freq,
						C.audio_config.achan[transmitCalibrationChannel].space_freq,
						transmitCalibrationChannel)
					for n > 0 {
						C.tone_gen_put_bit(C.int(transmitCalibrationChannel), n&1)
						n--
					}
				case 'm': // "Mark" tone: -x m
					fmt.Printf("\nSending mark calibration tone (%dHz) on channel %d.\nPress control-C to terminate.\n",
						C.audio_config.achan[transmitCalibrationChannel].mark_freq, transmitCalibrationChannel)
					for n > 0 {
						C.tone_gen_put_bit(C.int(transmitCalibrationChannel), 1)
						n--
					}
				case 's': // "Space" tone: -x s
					fmt.Printf("\nSending space calibration tone (%dHz) on channel %d.\nPress control-C to terminate.\n",
						C.audio_config.achan[transmitCalibrationChannel].space_freq, transmitCalibrationChannel)
					for n > 0 {
						C.tone_gen_put_bit(C.int(transmitCalibrationChannel), 0)
						n--
					}
				case 'p': // Silence - set PTT only: -x p
					fmt.Printf("\nSending silence (Set PTT only) on channel %d.\nPress control-C to terminate.\n", transmitCalibrationChannel)
					direwolf.SLEEP_SEC(max_duration)
				}

				C.ptt_set(C.OCTYPE_PTT, C.int(transmitCalibrationChannel), 0)
				C.text_color_set(C.DW_COLOR_INFO)
				os.Exit(0)
			} else {
				C.text_color_set(C.DW_COLOR_ERROR)
				fmt.Printf("\nMark/Space frequencies not defined for channel %d. Cannot calibrate using this modem type.\n", transmitCalibrationChannel)
				C.text_color_set(C.DW_COLOR_INFO)
				os.Exit(1)
			}
		} else {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("\nChannel %d is not configured as a radio channel.\n", transmitCalibrationChannel)
			C.text_color_set(C.DW_COLOR_INFO)
			os.Exit(1)
		}
	}

	/*
	 * Initialize the digipeater and IGate functions.
	 */
	C.digipeater_init(&C.audio_config, &digi_config)
	C.igate_init(&C.audio_config, &igate_config, &digi_config, C.int(d_i_opt))
	C.cdigipeater_init(&C.audio_config, &cdigi_config)
	C.pfilter_init(&igate_config, C.int(d_f_opt))
	C.ax25_link_init(&C.misc_config, C.int(d_c_opt))

	/*
	 * Provide the AGW & KISS socket interfaces for use by a client application.
	 */
	C.server_init(&C.audio_config, &C.misc_config)
	C.kissnet_init(&C.misc_config)

	// TODO KG This checks `misc_config.kiss_port > 0` but `kiss_port` is now an array?
	// Let's just check [0] for now...
	if C.misc_config.kiss_port[0] > 0 && C.misc_config.dns_sd_enabled > 0 {
		C.dns_sd_announce(&C.misc_config)
	}

	/*
	 * Create a pseudo terminal and KISS TNC emulator.
	 */
	C.kisspt_init(&C.misc_config)
	C.kissserial_init(&C.misc_config)
	C.kiss_frame_init(&C.audio_config)

	/*
	 * Open port for communication with GPS.
	 */
	C.dwgps_init(&C.misc_config, C.int(d_g_opt))

	C.waypoint_init(&C.misc_config)

	/*
	 * Enable beaconing.
	 * Open log file first because "-dttt" (along with -l...) will
	 * log the tracker beacon transmissions with fake channel 999.
	 */

	C.log_init(C.misc_config.log_daily_names, &C.misc_config.log_path[0])
	C.mheard_init(C.int(d_m_opt))
	C.beacon_init(&C.audio_config, &C.misc_config, &igate_config)

	/*
	 * Get sound samples and decode them.
	 * Use hot attribute for all functions called for every audio sample.
	 */

	C.recv_init(&C.audio_config)
	C.recv_process()
}
