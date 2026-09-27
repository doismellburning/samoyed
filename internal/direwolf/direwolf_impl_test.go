// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goHamlib "github.com/xylo04/goHamlib"
)

// --- DirewolfMain, driven in-process ---

// direwolfExit is what the osExit swapped in by runDirewolfMain panics with,
// so that the exit status can be recovered instead of ending the test binary.
type direwolfExit struct {
	code int
}

// dwMainTestConfig is a channel with no hardware behind it, and no network
// ports, so nothing it asks for can clash with anything else.
const dwMainTestConfig = `
ADEVICE stdin
ACHANNELS 1
CHANNEL 0
MYCALL Q1TEST
AGWPORT 0
KISSPORT 0
`

// writeDWMainConfig writes content to a configuration file in a temporary
// directory and returns its name.
func writeDWMainConfig(t *testing.T, content string) string {
	t.Helper()

	var name = filepath.Join(t.TempDir(), "direwolf.conf")
	require.NoError(t, os.WriteFile(name, []byte(content), 0o600))

	return name
}

// runDirewolfMain runs DirewolfMain with the given command line arguments,
// until it asks to exit, and returns the exit status it asked for along with
// everything it printed on stdout or stderr.
//
// Every global DirewolfMain touches on the way is put back afterwards.
func runDirewolfMain(t *testing.T, ctx context.Context, args ...string) (int, string) {
	t.Helper()

	var (
		origArgs        = os.Args
		origCommandLine = pflag.CommandLine
		origUsage       = pflag.Usage
		origExit        = osExit
		origLogOut      = logrus.StandardLogger().Out
		origLogLevel    = logrus.GetLevel()
		origAudioConfig = audio_config
		origMiscConfig  = misc_config
		origTTConfig    = dw_tt_config
		origSymbols     = aprsSymbolData
		origDeviceID    = deviceIDData
		origColor       = _text_color_level
		origLogger      = packetLogger
		origPTT         = pttControl
		origGPS         = gpsReceiver
		origWaypoint    = waypointSender
		origOpts        = [...]bool{d_u_opt, d_p_opt, q_h_opt, q_d_opt, A_opt_ais_to_obj}
	)

	t.Cleanup(func() {
		os.Args = origArgs
		pflag.CommandLine = origCommandLine
		pflag.Usage = origUsage
		osExit = origExit
		logrus.SetOutput(origLogOut)
		logrus.SetLevel(origLogLevel)
		goHamlib.SetDebugLevel(goHamlib.DebugLevel(0))
		audio_config = origAudioConfig
		misc_config = origMiscConfig
		dw_tt_config = origTTConfig
		aprsSymbolData = origSymbols
		deviceIDData = origDeviceID
		_text_color_level = origColor
		packetLogger = origLogger
		pttControl = origPTT
		gpsReceiver = origGPS
		waypointSender = origWaypoint
		d_u_opt, d_p_opt, q_h_opt, q_d_opt, A_opt_ais_to_obj = origOpts[0], origOpts[1], origOpts[2], origOpts[3], origOpts[4]
		teardownOnce = sync.Once{}
	})

	// The teardown a stop runs must only see what this run set up.
	packetLogger = nil
	pttControl = nil
	gpsReceiver = nil
	waypointSender = nil
	teardownOnce = sync.Once{}

	os.Args = append([]string{"direwolf"}, args...)
	pflag.CommandLine = pflag.NewFlagSet("direwolf", pflag.ContinueOnError)
	osExit = func(code int) { panic(direwolfExit{code: code}) }

	var code = -1
	var returned = false

	var output = testutils.CaptureOutput(t, func() {
		var origStderr = os.Stderr

		os.Stderr = os.Stdout

		defer func() { os.Stderr = origStderr }()

		defer func() {
			var r = recover()
			if r == nil {
				return
			}

			var exit, ok = r.(direwolfExit)
			if !ok {
				panic(r)
			}

			code = exit.code
		}()

		DirewolfMain(ctx)

		returned = true
	})

	// DirewolfMain only ever leaves by exiting.
	require.False(t, returned, "DirewolfMain returned rather than exiting: %s", output)

	return code, output
}

func Test_DirewolfMain_help(t *testing.T) {
	var code, output = runDirewolfMain(t, t.Context(), "--help")

	assert.Equal(t, 1, code)
	assert.Contains(t, output, "Usage: direwolf [options]")
	assert.Contains(t, output, "--config-file")
	assert.Contains(t, output, "Documentation can be found online")
}

func Test_DirewolfMain_version(t *testing.T) {
	var code, _ = runDirewolfMain(t, t.Context(), "--version")

	assert.Equal(t, 0, code)
}

func Test_DirewolfMain_print_utf8_test(t *testing.T) {
	var code, output = runDirewolfMain(t, t.Context(), "-u")

	assert.Equal(t, 0, code)
	// Not asserting on the string itself: each byte of its UTF-8 goes through
	// %c as a rune of its own, so it comes out double encoded, as "maÃ±ana".
	assert.Contains(t, output, "UTF-8 test string: ")
}

func Test_DirewolfMain_symbol_dump(t *testing.T) {
	var code, output = runDirewolfMain(t, t.Context(), "--symbol-dump")

	assert.Equal(t, 0, code)
	assert.NotEmpty(t, output)
}

func Test_DirewolfMain_config_check(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   int
		output string
	}{
		{
			name:   "clean",
			config: dwMainTestConfig,
			want:   0,
			output: "no problems found",
		},
		{
			name:   "errors",
			config: dwMainTestConfig + "ARATE 1\n",
			want:   1,
			output: "1 error, 0 warnings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var conf = writeDWMainConfig(t, tt.config)

			var code, output = runDirewolfMain(t, t.Context(), "-c", conf, "--config-check")

			assert.Equal(t, tt.want, code)
			assert.Contains(t, output, tt.output)
		})
	}
}

// A configuration the daemon cannot start with at all stops it, without a
// check having been asked for.
func Test_DirewolfMain_fatal_config(t *testing.T) {
	var conf = writeDWMainConfig(t, "ADEVICE\n")

	var code, output = runDirewolfMain(t, t.Context(), "-c", conf)

	assert.Equal(t, 1, code)
	assert.Contains(t, output, "Missing name of audio device")
}

func Test_DirewolfMain_bad_options(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		output string
	}{
		{
			name:   "sample rate",
			args:   []string{"-r", "1"},
			output: "-r option, audio samples/sec, is out of range.",
		},
		{
			name:   "audio channels",
			args:   []string{"-n", "3"},
			output: "-n option, number of audio channels, is out of range.",
		},
		{
			name:   "bits per sample",
			args:   []string{"-b", "12"},
			output: "-b option, bits per sample, must be 8 or 16.",
		},
		{
			name:   "modem",
			args:   []string{"-D", "9"},
			output: "decimate should be between 0 and 8 inclusive, not 9",
		},
		{
			// Each of the error rate forms is checked on the way to a
			// complaint about the log options, which ends the run before
			// anything is started up.
			name:   "log options together, receive error rate out of range",
			args:   []string{"-E", "R0", "-l", "logs", "-L", "log.txt"},
			output: "-ER must be in range of 1 to 99.",
		},
		{
			name:   "log options together, transmit error rate out of range",
			args:   []string{"-E", "100", "-l", "logs", "-L", "log.txt"},
			output: "-E must be in range of 1 to 99.",
		},
		{
			name:   "log options together, small audio statistics interval",
			args:   []string{"-a", "5", "-l", "logs", "-L", "log.txt"},
			output: "Setting such a small audio statistics interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var conf = writeDWMainConfig(t, dwMainTestConfig)

			var code, output = runDirewolfMain(t, t.Context(), append([]string{"-c", conf}, tt.args...)...)

			assert.Equal(t, 1, code)
			assert.Contains(t, output, tt.output)
		})
	}

	t.Run("log options together", func(t *testing.T) {
		var conf = writeDWMainConfig(t, dwMainTestConfig)

		var code, output = runDirewolfMain(t, t.Context(), "-c", conf, "-l", "logs", "-L", "log.txt")

		assert.Equal(t, 1, code)
		assert.Contains(t, output, "Logging options -l and -L can't be used together.")
	})
}

// A stop that has already arrived by the time the command line has been dealt
// with ends the run there, cleanly, having applied every option on the way.
func Test_DirewolfMain_options_then_stop(t *testing.T) {
	var conf = writeDWMainConfig(t, dwMainTestConfig)
	var logFile = filepath.Join(t.TempDir(), "packets.log")

	var ctx, cancel = context.WithCancel(t.Context())
	cancel()

	var code, output = runDirewolfMain(t, ctx,
		"-c", conf,
		"-a", "30",
		"-r", "22050",
		"-n", "2",
		"-b", "8",
		"-d", "aknugwtpoimfhcx2dz",
		"-q", "hdxz",
		"-E", "R50",
		"-T", "%H:%M:%S",
		"-e", "0.01",
		"-A",
		"-p",
		"-L", logFile,
		"stdin", "ignored",
	)

	assert.Equal(t, 0, code)
	assert.Contains(t, output, "Warning: File(s) beyond the first are ignored.")
	assert.Contains(t, output, "QRT")

	assert.Equal(t, 22050, audio_config.adev[0].samples_per_sec)
	assert.Equal(t, 2, audio_config.adev[0].num_channels)
	assert.Equal(t, MEDIUM_RADIO, audio_config.chan_medium[1])
	assert.Equal(t, 8, audio_config.adev[0].bits_per_sample)
	assert.Equal(t, 30, audio_config.statistics_interval)
	assert.Equal(t, "%H:%M:%S", audio_config.timestamp_format)
	assert.Equal(t, 50, audio_config.recv_error_rate)
	assert.InDelta(t, 0.01, audio_config.recv_ber, 1e-9)
	assert.Equal(t, "stdin", audio_config.adev[0].adevice_in)
	assert.Equal(t, logFile, misc_config.log_path)
	assert.False(t, misc_config.log_daily_names)
	assert.True(t, misc_config.enable_kiss_pt)
	assert.True(t, d_u_opt)
	assert.True(t, d_p_opt)
	assert.True(t, q_h_opt)
	assert.True(t, q_d_opt)
	assert.True(t, A_opt_ais_to_obj)
	assert.Equal(t, logrus.DebugLevel, logrus.GetLevel())
}

// The other forms of the error rate and log options.
func Test_DirewolfMain_other_options_then_stop(t *testing.T) {
	var conf = writeDWMainConfig(t, dwMainTestConfig)
	var logDir = t.TempDir()

	var ctx, cancel = context.WithCancel(t.Context())
	cancel()

	var code, _ = runDirewolfMain(t, ctx, "-c", conf, "-E", "20", "-l", logDir, "-n", "1")

	assert.Equal(t, 0, code)
	assert.Equal(t, 20, audio_config.xmit_error_rate)
	assert.Equal(t, logDir, misc_config.log_path)
	assert.True(t, misc_config.log_daily_names)
	assert.Equal(t, 1, audio_config.adev[0].num_channels)
}

// --- stopIfCancelled ---

func Test_stopIfCancelled_carries_on_while_running(t *testing.T) {
	var origExit = osExit

	t.Cleanup(func() { osExit = origExit })

	osExit = func(code int) { t.Fatalf("exited with %d", code) }

	stopIfCancelled(t.Context())
}

// --- teardown ---

// The teardown closes the packet log and the waypoint output when startup got
// as far as opening them, and does it only once however often it is asked.
func Test_teardown_closes_what_startup_opened(t *testing.T) {
	var origLogger, origWaypoint, origPTT, origGPS = packetLogger, waypointSender, pttControl, gpsReceiver

	t.Cleanup(func() {
		packetLogger, waypointSender, pttControl, gpsReceiver = origLogger, origWaypoint, origPTT, origGPS
		teardownOnce = sync.Once{}
	})

	teardownOnce = sync.Once{}
	pttControl = nil
	gpsReceiver = nil
	packetLogger = NewPacketLogger(false, t.TempDir())

	var ws, wsErr = NewWaypointSender(t.Context(), new(misc_config_s), nil)
	require.NoError(t, wsErr)

	waypointSender = ws

	var origLogOut = logrus.StandardLogger().Out

	t.Cleanup(func() { logrus.SetOutput(origLogOut) })

	var output = testutils.CaptureOutput(t, func() {
		logrus.SetOutput(os.Stdout)
		teardown()
		teardown()
	})

	assert.Equal(t, 1, strings.Count(output, "QRT"))
}

// --- app_process_rec_packet ---

// recPacketTest is what app_process_rec_packet reaches out to, set up afresh.
type recPacketTest struct {
	audioConfig *AudioConfig
	waypoints   net.PacketConn
}

// setupRecPacketTest sets up everything app_process_rec_packet hands a packet
// on to, with nothing configured that would transmit, and puts back what was
// there before when the test ends.
func setupRecPacketTest(t *testing.T) *recPacketTest {
	t.Helper()

	var (
		origAudioConfig = audio_config
		origTTConfig    = dw_tt_config
		origLogger      = packetLogger
		origMheard      = mheardDB
		origWaypoint    = waypointSender
		origAGW         = agwServer
		origKissNet     = kissNetSvc
		origKissSerial  = kissSerial
		origKissPT      = kissPT
		origIGate       = igate
		origDigi        = aprsDigipeater
		origCDigi       = connectedDigipeater
		origTT          = ttGateway
		origDeviceID    = deviceIDData
		origLogOut      = logrus.StandardLogger().Out
		origOpts        = [...]bool{d_u_opt, d_p_opt, q_h_opt, q_d_opt, A_opt_ais_to_obj}
	)

	t.Cleanup(func() {
		audio_config = origAudioConfig
		dw_tt_config = origTTConfig
		packetLogger = origLogger
		mheardDB = origMheard
		waypointSender = origWaypoint
		agwServer = origAGW
		kissNetSvc = origKissNet
		kissSerial = origKissSerial
		kissPT = origKissPT
		igate = origIGate
		aprsDigipeater = origDigi
		connectedDigipeater = origCDigi
		ttGateway = origTT
		deviceIDData = origDeviceID
		logrus.SetOutput(origLogOut)
		d_u_opt, d_p_opt, q_h_opt, q_d_opt, A_opt_ais_to_obj = origOpts[0], origOpts[1], origOpts[2], origOpts[3], origOpts[4]
	})

	d_u_opt, d_p_opt, q_h_opt, q_d_opt, A_opt_ais_to_obj = false, false, false, false, false

	var audioConfig = new(AudioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.igate_vchannel = -1
	audio_config = audioConfig

	var noTouchTones tt_config_s

	dw_tt_config = noTouchTones

	deviceIDData = NewDeviceIDData()
	packetLogger = NewPacketLogger(false, "")
	mheardDB = NewMHeardDB(0)

	// Waypoints go to a UDP socket of our own, so a position that reaches
	// them can be seen to have done so.
	var waypoints, listenErr = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { waypoints.Close() })

	var miscConfig = new(misc_config_s)
	miscConfig.waypoint_udp_hostname = "127.0.0.1"
	miscConfig.waypoint_udp_portnum = waypoints.LocalAddr().(*net.UDPAddr).Port //nolint:forcetypeassert

	var ws, wsErr = NewWaypointSender(t.Context(), miscConfig, nil)
	require.NoError(t, wsErr)

	t.Cleanup(ws.Close)

	waypointSender = ws

	agwServer = nil
	kissNetSvc = NewKissNetService(new(misc_config_s), audioConfig, 0)
	kissSerial = nil
	kissPT = nil

	var igateConfig = new(igate_config_s)
	var digiConfig = new(digi_config_s)
	var filter = NewPacketFilter(igateConfig, 0)

	igate = NewIGate(audioConfig, igateConfig, digiConfig, filter, 0)
	aprsDigipeater = NewDigipeater(audioConfig, digiConfig, filter)
	connectedDigipeater = NewConnectedDigipeater(audioConfig, new(cdigi_config_s), filter)
	ttGateway = NewTTGateway(audioConfig, &dw_tt_config, 0)

	return &recPacketTest{audioConfig: audioConfig, waypoints: waypoints}
}

// processRecPacket runs app_process_rec_packet on channel 0 and returns what
// it printed, log entries included.
func processRecPacket(t *testing.T, subchan int, slice int, pp *ax25.Packet, alevel ax25.ALevel, fecType fec_type_t, retries BitFixLevel, spectrum string) string {
	t.Helper()

	require.NotNil(t, pp)

	return testutils.CaptureOutput(t, func() {
		logrus.SetOutput(os.Stdout)
		app_process_rec_packet(t.Context(), 0, subchan, slice, pp, alevel, fecType, retries, spectrum)
	})
}

// readWaypoint returns the next waypoint sentence sent, or "" if none turns up.
func (rt *recPacketTest) readWaypoint(t *testing.T) string {
	t.Helper()

	require.NoError(t, rt.waypoints.SetReadDeadline(time.Now().Add(2*time.Second)))

	var buf = make([]byte, 1024)

	var n, _, err = rt.waypoints.ReadFrom(buf)
	if err != nil {
		return ""
	}

	return string(buf[:n])
}

// goodLevel is an audio level with nothing to complain about.
func goodLevel() ax25.ALevel {
	return ax25.ALevel{Rec: 50, Mark: 50, Space: 50}
}

func Test_app_process_rec_packet_aprs_position(t *testing.T) {
	var rt = setupRecPacketTest(t)

	var pp = ax25.FromText("Q1TEST>APRS,WIDE1-1:!4221.60N/07103.60W-Test", true)

	var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "Heard")
	assert.Contains(t, output, "heard=Q1TEST")
	assert.Contains(t, output, "Q1TEST>APRS,WIDE1-1:")
	assert.Contains(t, output, "!4221.60N/07103.60W-Test")

	var sentence = rt.readWaypoint(t)
	assert.Contains(t, sentence, "Q1TEST")

	var heard = mheardDB.Count(0, 30)
	assert.Equal(t, 1, heard, "the station should have been remembered as heard")
}

func Test_app_process_rec_packet_object_name_is_waypoint(t *testing.T) {
	var rt = setupRecPacketTest(t)

	var pp = ax25.FromText("Q1TEST>APRS:;LEADER   *092345z4903.50N/07201.75W>088/036", true)

	processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, rt.readWaypoint(t), "LEADER")
}

func Test_app_process_rec_packet_heard_via_digipeater(t *testing.T) {
	setupRecPacketTest(t)

	var pp = ax25.FromText("Q1TEST>APRS,Q2TEST*:>status", true)

	var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "heard=Q2TEST")
	assert.Contains(t, output, "digipeater=true")
}

// Hearing WIDEn-0 most likely means hearing the station before it in the path.
func Test_app_process_rec_packet_heard_via_wide(t *testing.T) {
	setupRecPacketTest(t)

	for _, subchan := range []int{0, -2} {
		t.Run(strconv.Itoa(subchan), func(t *testing.T) {
			var pp = ax25.FromText("Q1TEST>APRS,Q2TEST*,WIDE2*:>status", true)

			var output = processRecPacket(t, subchan, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

			assert.Contains(t, output, "heard=WIDE2")
			assert.Contains(t, output, "probably_really=Q2TEST")
		})
	}
}

func Test_app_process_rec_packet_fec_and_retries(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.audioConfig.achan[0].fix_bits = RETRY_INVERT_SINGLE

	tests := []struct {
		name    string
		fecType fec_type_t
		retries BitFixLevel
		want    string
	}{
		{name: "FX.25", fecType: fec_type_fx25, retries: RETRY_NONE, want: "FX.25"},
		{name: "IL2P", fecType: fec_type_il2p, retries: RETRY_NONE, want: "IL2P"},
		{name: "fixed bits", fecType: fec_type_none, retries: RETRY_INVERT_SINGLE, want: RETRY_INVERT_SINGLE.String()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pp = ax25.FromText("Q1TEST>APRS:>status", true)

			var output = processRecPacket(t, 0, 0, pp, goodLevel(), tt.fecType, tt.retries, "|")

			assert.Contains(t, output, tt.want)
		})
	}
}

func Test_app_process_rec_packet_audio_levels(t *testing.T) {
	setupRecPacketTest(t)

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)

	var output = processRecPacket(t, 0, 0, pp, ax25.ALevel{Rec: 150, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")
	assert.Contains(t, output, "Audio input level is too high")

	pp = ax25.FromText("Q1TEST>APRS:>status", true)

	output = processRecPacket(t, 0, 0, pp, ax25.ALevel{Rec: 1, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")
	assert.Contains(t, output, "Audio input level is too low")

	// A network TNC has no audio level to be low.
	pp = ax25.FromText("Q1TEST>APRS:>status", true)

	output = processRecPacket(t, -3, 0, pp, ax25.ALevel{Rec: 1, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")
	assert.NotContains(t, output, "Audio input level is too low")
	assert.Contains(t, output, "subchan=nettnc")
}

func Test_app_process_rec_packet_quiet(t *testing.T) {
	setupRecPacketTest(t)

	q_h_opt = true
	q_d_opt = true

	var pp = ax25.FromText("Q1TEST>APRS:!4221.60N/07103.60W-Test", true)

	var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.NotContains(t, output, "Heard")
	assert.Contains(t, output, "!4221.60N/07103.60W-Test")
}

func Test_app_process_rec_packet_timestamp(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.audioConfig.timestamp_format = "TS%Y"

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)

	var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, fmt.Sprintf("ts=TS%d", time.Now().Year()))
}

// A packet from the IGate's virtual channel is printed and passed to client
// applications, and goes no further.
func Test_app_process_rec_packet_from_igate_channel(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.audioConfig.igate_vchannel = 0

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)

	var output = processRecPacket(t, -2, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.NotContains(t, output, "Heard")
	assert.Contains(t, output, "subchan=is")
}

func Test_app_process_rec_packet_dtmf(t *testing.T) {
	setupRecPacketTest(t)

	dw_tt_config.gateway_enabled = 1

	var pp = ax25.FromText("DTMF>APRS:t2A22A#", true)

	var output = processRecPacket(t, -1, 0, pp, ax25.ALevel{Rec: -2, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "subchan=dtmf")
}

// A touch tone sequence can be simulated with a packet whose information
// starts with 't'.
func Test_app_process_rec_packet_simulated_dtmf(t *testing.T) {
	setupRecPacketTest(t)

	dw_tt_config.gateway_enabled = 1

	var pp = ax25.FromText("DTMF>APRS:t2A22A#", true)

	var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "heard=DTMF")
}

func Test_app_process_rec_packet_non_aprs(t *testing.T) {
	setupRecPacketTest(t)

	d_u_opt = true
	d_p_opt = true

	var origLevel = logrus.GetLevel()

	t.Cleanup(func() { logrus.SetLevel(origLevel) })

	logrus.SetLevel(logrus.DebugLevel)

	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"

	t.Run("SABM", func(t *testing.T) {
		var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUSABM, 1, 0, nil)

		var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

		assert.Contains(t, output, "desc=")
		assert.Contains(t, output, "--debug p hexdump below")
	})

	t.Run("XID", func(t *testing.T) {
		var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUXID, 1, 0, nil)

		var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

		assert.Contains(t, output, "info=")
	})

	t.Run("non-printable", func(t *testing.T) {
		var pp = ax25.FromText("Q1TEST>APRS:>caf\xc3\xa9\x01", true)

		var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

		assert.Contains(t, output, "--debug u hexdump below")
	})
}

// A frame with no AX.25 addresses at all has nobody to have been heard.
func Test_app_process_rec_packet_no_addresses(t *testing.T) {
	setupRecPacketTest(t)

	var frame = []byte("abcdefghijklmnopqrstuvwxyz")

	var pp = ax25.FromFrame(frame, goodLevel())
	require.NotNil(t, pp)
	require.Equal(t, 0, pp.NumAddr())

	var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "Heard")
}

func Test_app_process_rec_packet_ais_to_object(t *testing.T) {
	var rt = setupRecPacketTest(t)

	A_opt_ais_to_obj = true

	var sentence = aisPositionReport(t, 208, 900)
	var pp = ax25.FromText(fmt.Sprintf("Q1TEST>APRS:{%c%c%s", USER_DEF_USER_ID, USER_DEF_TYPE_AIS, sentence), true)

	var output = processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "ais_obj_packet=")
	assert.Contains(t, output, ";366730000*")

	// The position goes out as a waypoint named after the vessel.
	assert.Contains(t, rt.readWaypoint(t), "366730000")
}

func Test_app_process_rec_packet_multiple_subchannels(t *testing.T) {
	setupRecPacketTest(t)

	var origDemods = demodulators

	t.Cleanup(func() { demodulators = origDemods })

	// Only the layout matters here, not a demodulator that could do anything.
	var d = new(Demodulator)
	d.numSubchan = 2
	d.numSlicers = 3
	demodulators[0] = d

	var numSubchan, numSlicers = channelLayout(0)
	require.Greater(t, numSubchan, 1)
	require.Greater(t, numSlicers, 1)

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)

	var output = processRecPacket(t, 1, 2, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "subchan=1")
	assert.Contains(t, output, "slice=2")
}
