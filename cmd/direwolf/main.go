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
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0 -DUSE_AVAHI_CLIENT
import "C"

import (
	"fmt"
	"os"

	direwolf "github.com/doismellburning/samoyed/src"
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
	var enable_pseudo_terminal = false
	var digi_config C.struct_digi_config_s
	var cdigi_config C.struct_cdigi_config_s
	var igate_config C.struct_igate_config_s
	var r_opt, n_opt, b_opt, B_opt, D_opt, U_opt C.int
	var P_opt string
	var l_opt_logdir string
	var L_opt_logfile string
	var input_file string
	var T_opt_timestamp string

	var t_opt C.int = 1 /* Text color option. */
	var a_opt C.int = 0 /* "-a n" interval, in seconds, for audio statistics report.  0 for none. */
	var g_opt = false   /* G3RUH mode, ignoring default for speed. */
	var j_opt = false   /* 2400 bps PSK compatible with direwolf <= 1.5 */
	var J_opt = false   /* 2400 bps PSK compatible MFJ-2400 and maybe others. */

	// FIXME KG var d_k_opt = 0 /* "-d k" option for serial port KISS.  Can be repeated for more detail. */
	// FIXME KG var d_n_opt = 0 /* "-d n" option for Network KISS.  Can be repeated for more detail. */
	// FIXME KG var d_t_opt = 0 /* "-d t" option for Tracker.  Can be repeated for more detail. */
	var d_g_opt = 0 /* "-d g" option for GPS. Can be repeated for more detail. */
	// FIXME KG var d_o_opt = 0 /* "-d o" option for output control such as PTT and DCD. */
	var d_i_opt = 0 /* "-d i" option for IGate.  Repeat for more detail */
	var d_m_opt = 0 /* "-d m" option for mheard list. */
	var d_f_opt = 0 /* "-d f" option for filtering.  Repeat for more detail. */
	var d_h_opt = 0 /* "-d h" option for hamlib debugging.  Repeat for more detail */
	var d_x_opt = 1 /* "-d x" option for FX.25.  Default minimal. Repeat for more detail.  -qx to silence. */
	var d_2_opt = 0 /* "-d 2" option for IL2P.  Default minimal. Repeat for more detail. */
	var d_c_opt = 0 /* "-d c" option for connected mode data link state machine. */

	var aprstt_debug = 0 /* "-d d" option for APRStt (think Dtmf) debug. */

	var E_tx_opt C.int = 0 /* "-E n" Error rate % for clobbering transmit frames. */
	var E_rx_opt C.int = 0 /* "-E Rn" Error rate % for clobbering receive frames. */

	var e_recv_ber = 0.0             /* Receive Bit Error Rate (BER). */
	var X_fx25_xmit_enable C.int = 0 /* FX.25 transmit enable. */

	var I_opt = -1 /* IL2P transmit, normal polarity, arg is max_fec. */
	var i_opt = -1 /* IL2P transmit, inverted polarity, arg is max_fec. */

	var x_opt_mode = ' ' /* "-x N" option for transmitting calibration tones. */
	var x_opt_chan = 0   /* Split into 2 parts.  Mode e.g.  m, a, and optional channel. */

	/*
	 * Pre-scan the command line options for the text color option.
	 * We need to set this before any text output.
	 * Default will be no colors if stdout is not a terminal (i.e. piped into
	 * something else such as "tee") but command line can override this.
	 */

	t_opt = C.isatty(C.fileno(C.stdout))
	/* 1 = normal, 0 = no text colors. */
	/* 2, 3, ... alternate escape sequences for different terminals. */

	// FIXME: consider case of no space between t and number.

	/* FIXME KG
	for j := 1; j < argc-1; j++ {
		if strcmp(argv[j], "-t") == 0 {
			t_opt = atoi(argv[j+1])
			//fmt.Printf ("DEBUG: text color option = %d.\n", t_opt);
		}
	}
	*/

	// TODO: control development/beta/release by version.h instead of changing here.
	// Print platform.  This will provide more information when people send a copy the information displayed.

	// Might want to print OS version here.   For Windows, see:
	// https://msdn.microsoft.com/en-us/library/ms724451(v=VS.85).aspx

	C.text_color_init(t_opt)
	C.text_color_set(C.DW_COLOR_INFO)
	//fmt.Printf ("Dire Wolf version %d.%d (%s) BETA TEST 7\n", MAJOR_VERSION, MINOR_VERSION, __DATE__);
	fmt.Printf("Dire Wolf DEVELOPMENT version %d.%d %s (%s)\n", C.MAJOR_VERSION, C.MINOR_VERSION, "D", C.__DATE__)
	//fmt.Printf ("Dire Wolf version %d.%d\n", MAJOR_VERSION, MINOR_VERSION);

	C.setlinebuf(C.stdout)
	C.setup_sigint_handler()

	// I've seen many references to people running this as root.
	// There is no reason to do that.
	// Ordinary users can access audio, gpio, etc. if they are in the correct groups.
	// Giving an applications permission to do things it does not need to do
	// is a huge security risk.

	if os.Getuid() == 0 || os.Geteuid() == 0 {
		C.text_color_set(C.DW_COLOR_ERROR)
		for n := 0; n < 15; n++ {
			fmt.Printf("\n")
			fmt.Printf("Why are you running this as root user?.\n")
			fmt.Printf("Dire Wolf requires only privileges available to ordinary users.\n")
			fmt.Printf("Running this as root is an unnecessary security risk.\n")
			//SLEEP_SEC(1);
		}
	}

	/*
	 * Default location of configuration file is current directory.
	 * Can be overridden by -c command line option.
	 * TODO:  Automatically search other places.
	 */

	var config_file = "direwolf.conf"

	/*
	 * Look at command line options.
	 * So far, the only one is the configuration file location.
	 */

	/* FIXME KG
		strlcpy (input_file, "", sizeof(input_file));
		for {
	          c = getopt_long(argc, argv, "hP:B:gjJD:U:c:px:r:b:n:d:q:t:ul:L:Sa:E:T:e:X:AI:i:",
	                        long_options, &option_index);
	          if (c == -1)
	            break;

	          switch (c) {

	          case 0:				// possible future use
		    C.text_color_set(C.DW_COLOR_DEBUG);
	            fmt.Printf("option %s", long_options[option_index].name);
	            if (optarg) {
	                fmt.Printf(" with arg %s", optarg);
	            }
	            fmt.Printf("\n");
	            break;

	          case 'a':				// -a for audio statistics interval

		    a_opt = atoi(optarg);
		    if (a_opt < 0) a_opt = 0;
		    if (a_opt < 10) {
		      C.text_color_set(C.DW_COLOR_ERROR);
	              fmt.Printf("Setting such a small audio statistics interval will produce inaccurate sample rate display.\n");
	   	    }
	            break;

	          case 'c':				// -c for configuration file name

		    strlcpy (config_file, optarg, sizeof(config_file));
	            break;

	          case 'p':				/* -p enable pseudo terminal

		    // We want this to be off by default because it hangs
		    // eventually when nothing is reading from other side./

		    enable_pseudo_terminal = 1;
	            break;

	          case 'B':				// -B baud rate and modem properties.
							// Also implies modem type based on speed.
							// Special case "AIS" rather than number.
		    if (strcasecmp(optarg, "AIS") == 0) {
		      B_opt = 12345;	// See special case below.
		    }
		    else if (strcasecmp(optarg, "EAS") == 0) {
		      B_opt = 23456;	// See special case below.
		    }
		    else {
		      B_opt = atoi(optarg);
		    }
	            if (B_opt < MIN_BAUD || B_opt > MAX_BAUD) {
		      C.text_color_set(C.DW_COLOR_ERROR);
	              fmt.Printf ("Use a more reasonable data baud rate in range of %d - %d.\n", MIN_BAUD, MAX_BAUD);
	              exit (EXIT_FAILURE);
	            }
	            break;

	          case 'g':				/* -g G3RUH modem, overriding default mode for speed.

		    g_opt = 1;
	            break;

	          case 'j':				/* -j V.26 compatible with earlier direwolf.

		    j_opt = 1;
	            break;

	          case 'J':				/* -J V.26 compatible with MFJ-2400.

		    J_opt = 1;
	            break;

		  case 'P':				/* -P for modem profile.

		    //debug: fmt.Printf ("Demodulator profile set to \"%s\"\n", optarg);
		    strlcpy (P_opt, optarg, sizeof(P_opt));
		    break;

	          case 'D':				/* -D divide AFSK demodulator sample rate

		    D_opt = atoi(optarg);
	            if (D_opt < 1 || D_opt > 8) {
		      C.text_color_set(C.DW_COLOR_ERROR);
	              fmt.Printf ("Crazy value for -D. \n");
	              exit (EXIT_FAILURE);
	            }
	            break;

	          case 'U':				/* -U multiply G3RUH demodulator sample rate (upsample)

		    U_opt = atoi(optarg);
	            if (U_opt < 1 || U_opt > 4) {
		      C.text_color_set(C.DW_COLOR_ERROR);
	              fmt.Printf ("Crazy value for -U. \n");
	              exit (EXIT_FAILURE);
	            }
	            break;

	          case 'x':				/* -x N for transmit calibration tones.
							/* N is composed of a channel number and/or one letter
							/* for the mode: mark, space, alternate, ptt-only.

		    for (char *p = optarg; *p != '\0'; p++ ) {
		      switch (*p) {
		      case '0':
		      case '1':
		      case '2':
		      case '3':
		      case '4':
		      case '5':
		      case '6':
		      case '7':
		      case '8':
		      case '9':
		        x_opt_chan = x_opt_chan * 10 + *p - '0';
		        if (x_opt_mode == ' ') x_opt_mode = 'a';
		        break;
		      case 'a':  x_opt_mode = *p; break; // Alternating tones
		      case 'm':  x_opt_mode = *p; break; // Mark tone
		      case 's':  x_opt_mode = *p; break; // Space tone
		      case 'p':  x_opt_mode = *p; break; // Set PTT only
	      	      default:
		        C.text_color_set(C.DW_COLOR_ERROR);
		        fmt.Printf ("Invalid option '%c' for -x. Must be a, m, s, or p.\n", *p);
		        C.text_color_set(C.DW_COLOR_INFO);
	      	    	exit (EXIT_FAILURE);
	      	    	break;
	      	     }
		    }
		    if (x_opt_chan < 0 || x_opt_chan >= MAX_RADIO_CHANS) {
		      C.text_color_set(C.DW_COLOR_ERROR);
		      fmt.Printf ("Invalid channel %d for -x. \n", x_opt_chan);
		      C.text_color_set(C.DW_COLOR_INFO);
		      exit (EXIT_FAILURE);
		    }
	            break;

	          case 'r':				/* -r audio samples/sec.  e.g. 44100

		    r_opt = atoi(optarg);
		    if (r_opt < MIN_SAMPLES_PER_SEC || r_opt > MAX_SAMPLES_PER_SEC)
		    {
		      C.text_color_set(C.DW_COLOR_ERROR);
	              fmt.Printf("-r option, audio samples/sec, is out of range.\n");
		      r_opt = 0;
	   	    }
	            break;

	          case 'n':				/* -n number of audio channels for first audio device.  1 or 2.

		    n_opt = atoi(optarg);
		    if (n_opt < 1 || n_opt > 2)
		    {
		      C.text_color_set(C.DW_COLOR_ERROR);
	              fmt.Printf("-n option, number of audio channels, is out of range.\n");
		      n_opt = 0;
	   	    }
	            break;

	          case 'b':				/* -b bits per sample.  8 or 16.

		    b_opt = atoi(optarg);
		    if (b_opt != 8 && b_opt != 16)
		    {
		      C.text_color_set(C.DW_COLOR_ERROR);
	              fmt.Printf("-b option, bits per sample, must be 8 or 16.\n");
		      b_opt = 0;
	   	    }
	            break;

		  case 'h':			// -h for help
	          case '?':

	            /* For '?' unknown option message was already printed.
	            usage ();
	            break;

		  case 'd':				/* Set debug option.

		    /* New in 1.1.  Can combine multiple such as "-d pkk"

		    for (p=optarg; *p!='\0'; p++) {
		     switch (*p) {

		      case 'a':  server_set_debug(1); break;

		      case 'k':  d_k_opt++; kissserial_set_debug (d_k_opt); kisspt_set_debug (d_k_opt); break;
		      case 'n':  d_n_opt++; kiss_net_set_debug (d_n_opt); break;

		      case 'u':  d_u_opt = 1; break;

			// separate out gps & waypoints.

		      case 'g':  d_g_opt++; break;
		      case 'w':	 waypoint_set_debug (1); break;		// not documented yet.
		      case 't':  d_t_opt++; beacon_tracker_set_debug (d_t_opt); break;

		      case 'p':  d_p_opt = 1; break;			// TODO: packet dump for xmit side.
		      case 'o':  d_o_opt++; ptt_set_debug(d_o_opt); break;
		      case 'i':  d_i_opt++; break;
		      case 'm':  d_m_opt++; break;
		      case 'f':  d_f_opt++; break;
	#if AX25MEMDEBUG
		      case 'l':  ax25memdebug_set(); break;		// Track down memory Leak.  Not documented.
	#endif								// Previously 'm' but that is now used for mheard.
	#if USE_HAMLIB
		      case 'h':  d_h_opt++; break;			// Hamlib verbose level.
	#endif
		      case 'c':  d_c_opt++; break;			// Connected mode data link state machine
		      case 'x':  d_x_opt++; break;			// FX.25
		      case '2':  d_2_opt++; break;			// IL2P
		      case 'd':	 aprstt_debug++; break;			// APRStt (mnemonic Dtmf)
		      default: break;
		     }
		    }
		    break;

		  case 'q':				/* Set quiet option.

		    /* New in 1.2.  Quiet option to suppress some types of printing.
		    /* Can combine multiple such as "-q hd"

		    for (p=optarg; *p!='\0'; p++) {
		     switch (*p) {
		      case 'h':  q_h_opt = 1; break;
		      case 'd':  q_d_opt = 1; break;
		      case 'x':  d_x_opt = 0; break;	// Defaults to minimal info.  This silences.
		      default: break;
		     }
		    }
		    break;

		  case 't':				/* Was handled earlier.
		    break;


		  case 'u':				/* Print UTF-8 test and exit.

		    fmt.Printf ("\n  UTF-8 test string: ma%c%cana %c%c F%c%c%c%ce\n\n",
				0xc3, 0xb1,
				0xc2, 0xb0,
				0xc3, 0xbc, 0xc3, 0x9f);

		    exit (0);
		    break;

	          case 'l':				/* -l for log directory with daily files

		    strlcpy (l_opt_logdir, optarg, sizeof(l_opt_logdir));
	            break;

	          case 'L':				/* -L for log file name with full path

		    strlcpy (L_opt_logfile, optarg, sizeof(L_opt_logfile));
	            break;


		  case 'S':				// Print symbol tables and exit.

		    symbols_init ();
		    symbols_list ();
		    exit (0);
		    break;

	          case 'E':				// -E Error rate (%) for corrupting frames.
							// Just a number is transmit.  Precede by R for receive.

		    if (*optarg == 'r' || *optarg == 'R') {
		      E_rx_opt = atoi(optarg+1);
		      if (E_rx_opt < 1 || E_rx_opt > 99) {
		        C.text_color_set(C.DW_COLOR_ERROR);
	                  fmt.Printf("-ER must be in range of 1 to 99.\n");
		      E_rx_opt = 10;
		      }
		    }
		    else {
		      E_tx_opt = atoi(optarg);
		      if (E_tx_opt < 1 || E_tx_opt > 99) {
		        C.text_color_set(C.DW_COLOR_ERROR);
	                fmt.Printf("-E must be in range of 1 to 99.\n");
		        E_tx_opt = 10;
		      }
		    }
	            break;

	          case 'T':				/* -T for receive timestamp.
		    strlcpy (T_opt_timestamp, optarg, sizeof(T_opt_timestamp));
	            break;

		  case 'e':				/* -e Receive Bit Error Rate (BER).

		    e_recv_ber = atof(optarg);
		    break;

	          case 'X':

		    X_fx25_xmit_enable = atoi(optarg);
	            break;

	          case 'I':			// IL2P, normal polarity

		    I_opt = atoi(optarg);
	            break;

	          case 'i':			// IL2P, inverted polarity

		    i_opt = atoi(optarg);
	            break;

		  case 'A':			// -A 	convert AIS to APRS object

		    A_opt_ais_to_obj = 1;
		    break;

	          default:

	            /* Should not be here.
		    C.text_color_set(C.DW_COLOR_DEBUG);
	            fmt.Printf("?? getopt returned character code 0%o ??\n", c);
	            usage ();
	          }
		}  /* end while(1) for options
	*/

	/* FIXME KG
	if optind < argc {

		if optind < argc-1 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Warning: File(s) beyond the first are ignored.\n")
		}

		strlcpy(input_file, argv[optind], sizeof(input_file))

	}
	*/

	/*
	 * Get all types of configuration settings from configuration file.
	 *
	 * Possibly override some by command line options.
	 */

	C.rig_set_debug(uint32(d_h_opt))

	C.symbols_init()

	C.dwsock_init()

	C.config_init(C.CString(config_file), &C.audio_config, &digi_config, &cdigi_config, &C.tt_config, &igate_config, &C.misc_config)

	if r_opt != 0 {
		C.audio_config.adev[0].samples_per_sec = r_opt
	}

	if n_opt != 0 {
		C.audio_config.adev[0].num_channels = n_opt
		if n_opt == 2 {
			C.audio_config.chan_medium[1] = C.MEDIUM_RADIO
		}
	}

	if b_opt != 0 {
		C.audio_config.adev[0].bits_per_sample = b_opt
	}

	if B_opt != 0 {
		C.audio_config.achan[0].baud = B_opt

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
				C.text_color_set(C.DW_COLOR_ERROR)
				fmt.Printf("Bit rate should be standard 2400 rather than specified %d.\n", C.audio_config.achan[0].baud)
			}
		} else if C.audio_config.achan[0].baud < 7200 {
			C.audio_config.achan[0].modem_type = C.MODEM_8PSK
			C.audio_config.achan[0].mark_freq = 0
			C.audio_config.achan[0].space_freq = 0
			if C.audio_config.achan[0].baud != 4800 {
				C.text_color_set(C.DW_COLOR_ERROR)
				fmt.Printf("Bit rate should be standard 4800 rather than specified %d.\n", C.audio_config.achan[0].baud)
			}
		} else if C.audio_config.achan[0].baud == 12345 {
			C.audio_config.achan[0].modem_type = C.MODEM_AIS
			C.audio_config.achan[0].baud = 9600
			C.audio_config.achan[0].mark_freq = 0
			C.audio_config.achan[0].space_freq = 0
		} else if C.audio_config.achan[0].baud == 23456 {
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

	if g_opt {

		// Force G3RUH mode, overriding default for speed.
		//   Example:   -B 2400 -g

		C.audio_config.achan[0].modem_type = C.MODEM_SCRAMBLE
		C.audio_config.achan[0].mark_freq = 0
		C.audio_config.achan[0].space_freq = 0
	}

	if j_opt {

		// V.26 compatible with earlier versions of direwolf.
		//   Example:   -B 2400 -j    or simply   -j

		C.audio_config.achan[0].v26_alternative = C.V26_A
		C.audio_config.achan[0].modem_type = C.MODEM_QPSK
		C.audio_config.achan[0].mark_freq = 0
		C.audio_config.achan[0].space_freq = 0
		C.audio_config.achan[0].baud = 2400
	}

	if J_opt {

		// V.26 compatible with MFJ and maybe others.
		//   Example:   -B 2400 -J     or simply   -J

		C.audio_config.achan[0].v26_alternative = C.V26_B
		C.audio_config.achan[0].modem_type = C.MODEM_QPSK
		C.audio_config.achan[0].mark_freq = 0
		C.audio_config.achan[0].space_freq = 0
		C.audio_config.achan[0].baud = 2400
	}

	C.audio_config.statistics_interval = a_opt

	if P_opt != "" {
		/* -P for modem profile. */
		C.strcpy(&C.audio_config.achan[0].profiles[0], C.CString(P_opt))
	}

	if D_opt != 0 {
		// Reduce audio sampling rate to reduce CPU requirements.
		C.audio_config.achan[0].decimate = D_opt
	}

	if U_opt != 0 {
		// Increase G3RUH audio sampling rate to improve performance.
		// The value is normally determined automatically based on audio
		// sample rate and baud.  This allows override for experimentation.
		C.audio_config.achan[0].upsample = U_opt
	}

	C.strcpy(&C.audio_config.timestamp_format[0], C.CString(T_opt_timestamp))

	// temp - only xmit errors.

	C.audio_config.xmit_error_rate = E_tx_opt
	C.audio_config.recv_error_rate = E_rx_opt

	if l_opt_logdir != "" && L_opt_logfile != "" {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("Logging options -l and -L can't be used together.  Pick one or the other.\n")
		os.Exit(1)
	}

	if L_opt_logfile != "" {
		C.misc_config.log_daily_names = 0
		C.strcpy(&C.misc_config.log_path[0], C.CString(L_opt_logfile))
	} else if l_opt_logdir != "" {
		C.misc_config.log_daily_names = 1
		C.strcpy(&C.misc_config.log_path[0], C.CString(l_opt_logdir))
	}

	if enable_pseudo_terminal {
		C.misc_config.enable_kiss_pt = 1
	}

	if input_file != "" {
		C.strcpy(&C.audio_config.adev[0].adevice_in[0], C.CString(input_file))
	}

	C.audio_config.recv_ber = C.float(e_recv_ber)

	if X_fx25_xmit_enable > 0 {
		if I_opt != -1 || i_opt != -1 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Can't mix -X with -I or -i.\n")
			os.Exit(1)
		}
		C.audio_config.achan[0].fx25_strength = X_fx25_xmit_enable
		C.audio_config.achan[0].layer2_xmit = C.LAYER2_FX25
	}

	if I_opt != -1 && i_opt != -1 {
		C.text_color_set(C.DW_COLOR_ERROR)
		fmt.Printf("Can't use both -I and -i at the same time.\n")
		os.Exit(1)
	}

	if I_opt >= 0 {
		C.audio_config.achan[0].layer2_xmit = C.LAYER2_IL2P
		if I_opt > 0 {
			C.audio_config.achan[0].il2p_max_fec = 1
		}
		if C.audio_config.achan[0].il2p_max_fec == 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("It is highly recommended that 1, rather than 0, is used with -I for best results.\n")
		}
		C.audio_config.achan[0].il2p_invert_polarity = 0 // normal
	}

	if i_opt >= 0 {
		C.audio_config.achan[0].layer2_xmit = C.LAYER2_IL2P
		if i_opt > 0 {
			C.audio_config.achan[0].il2p_max_fec = 1
		}
		if C.audio_config.achan[0].il2p_max_fec == 0 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("It is highly recommended that 1, rather than 0, is used with -i for best results.\n")
		}
		C.audio_config.achan[0].il2p_invert_polarity = 1 // invert for transmit
		if C.audio_config.achan[0].baud == 1200 {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("Using -i with 1200 bps is a bad idea.  Use -I instead.\n")
		}
	}

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
		// FIXME KG usage()
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

	/* FIXME KG
	assert(C.audio_config.adev[0].bits_per_sample == 8 || C.audio_config.adev[0].bits_per_sample == 16)
	assert(C.audio_config.adev[0].num_channels == 1 || C.audio_config.adev[0].num_channels == 2)
	assert(C.audio_config.adev[0].samples_per_sec >= MIN_SAMPLES_PER_SEC && C.audio_config.adev[0].samples_per_sec <= MAX_SAMPLES_PER_SEC)
	*/

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

	if x_opt_mode != ' ' {
		if C.audio_config.chan_medium[x_opt_chan] == C.MEDIUM_RADIO {
			if C.audio_config.achan[x_opt_chan].mark_freq != 0 && C.audio_config.achan[x_opt_chan].space_freq != 0 {
				var max_duration = 60
				var n = C.audio_config.achan[x_opt_chan].baud * C.int(max_duration)

				C.text_color_set(C.DW_COLOR_INFO)
				C.ptt_set(C.OCTYPE_PTT, C.int(x_opt_chan), 1)

				switch x_opt_mode {
				default:
				case 'a': // Alternating tones: -x a
					fmt.Printf("\nSending alternating mark/space calibration tones (%d/%dHz) on channel %d.\nPress control-C to terminate.\n",
						C.audio_config.achan[x_opt_chan].mark_freq,
						C.audio_config.achan[x_opt_chan].space_freq,
						x_opt_chan)
					for n > 0 {
						C.tone_gen_put_bit(C.int(x_opt_chan), n&1)
						n--
					}
					break
				case 'm': // "Mark" tone: -x m
					fmt.Printf("\nSending mark calibration tone (%dHz) on channel %d.\nPress control-C to terminate.\n",
						C.audio_config.achan[x_opt_chan].mark_freq, x_opt_chan)
					for n > 0 {
						C.tone_gen_put_bit(C.int(x_opt_chan), 1)
						n--
					}
					break
				case 's': // "Space" tone: -x s
					fmt.Printf("\nSending space calibration tone (%dHz) on channel %d.\nPress control-C to terminate.\n",
						C.audio_config.achan[x_opt_chan].space_freq, x_opt_chan)
					for n > 0 {
						C.tone_gen_put_bit(C.int(x_opt_chan), 0)
						n--
					}
					break
				case 'p': // Silence - set PTT only: -x p
					fmt.Printf("\nSending silence (Set PTT only) on channel %d.\nPress control-C to terminate.\n", x_opt_chan)
					direwolf.SLEEP_SEC(max_duration)
					break
				}

				C.ptt_set(C.OCTYPE_PTT, C.int(x_opt_chan), 0)
				C.text_color_set(C.DW_COLOR_INFO)
				os.Exit(1)
			} else {
				C.text_color_set(C.DW_COLOR_ERROR)
				fmt.Printf("\nMark/Space frequencies not defined for channel %d. Cannot calibrate using this modem type.\n", x_opt_chan)
				C.text_color_set(C.DW_COLOR_INFO)
				os.Exit(1)
			}
		} else {
			C.text_color_set(C.DW_COLOR_ERROR)
			fmt.Printf("\nChannel %d is not configured as a radio channel.\n", x_opt_chan)
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
