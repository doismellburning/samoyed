// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/aprslog"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/mheard"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- stopIfCancelled ---

// A run that nobody has asked to stop carries on: were stopIfCancelled to
// clean up and exit here, the test binary would go with it.
func Test_stopIfCancelled_carries_on_while_running(t *testing.T) {
	stopIfCancelled(t.Context(), new(teardownList))
}

// --- teardown ---

// The teardown releases what was added to it, in the order it was added - so
// the PTT, which startup acquires first, is released first - and does it only
// once however often it is asked.
func Test_teardown_releases_what_startup_acquired(t *testing.T) {
	var td = new(teardownList)
	var released []string

	td.add(func() { released = append(released, "ptt") })
	td.add(func() { released = append(released, "gps") })

	var origLogOut = logrus.StandardLogger().Out

	t.Cleanup(func() { logrus.SetOutput(origLogOut) })

	var output = testutils.CaptureOutput(t, func() {
		logrus.SetOutput(os.Stdout)
		td.run()
		td.run()
	})

	assert.Equal(t, []string{"ptt", "gps"}, released)
	assert.Equal(t, 1, strings.Count(output, "QRT"))
}

// A teardown with nothing added, as when startup stops before acquiring
// anything, still says it is going.
func Test_teardown_with_nothing_to_release(t *testing.T) {
	var origLogOut = logrus.StandardLogger().Out

	t.Cleanup(func() { logrus.SetOutput(origLogOut) })

	var output = testutils.CaptureOutput(t, func() {
		logrus.SetOutput(os.Stdout)
		new(teardownList).run()
	})

	assert.Contains(t, output, "QRT")
}

// --- app_process_rec_packet ---

// recPacketTest is what app_process_rec_packet reaches out to, set up afresh.
type recPacketTest struct {
	handler     *recPacketHandler
	audioConfig *RadioConfig
	waypoints   net.PacketConn
}

// setupRecPacketTest sets up everything app_process_rec_packet hands a packet
// on to, with nothing configured that would transmit, and puts back the
// globals it still needs - those other files read too - when the test ends.
func setupRecPacketTest(t *testing.T) *recPacketTest {
	t.Helper()

	var (
		origIGate  = igate
		origLogOut = logrus.StandardLogger().Out
	)

	t.Cleanup(func() {
		igate = origIGate
		logrus.SetOutput(origLogOut)
	})

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.igate_vchannel = -1

	var heardDB = mheard.New(0)

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

	var igateConfig = new(igate_config_s)
	var digiConfig = new(digi_config_s)
	var filter = NewPacketFilter(igateConfig, aprs.NewDecoderFromDataFiles(), heardDB, 0)

	igate = NewIGate(audioConfig, igateConfig, digiConfig, filter, heardDB, 0)

	var handler = new(recPacketHandler)
	handler.audioConfig = audioConfig
	handler.ttConfig = new(tt_config_s) // No touch tones.
	handler.decoder = aprs.NewDecoderFromDataFiles()
	handler.logger = aprslog.New(false, "")
	handler.heard = heardDB
	handler.waypoints = ws
	handler.digipeater = NewDigipeater(audioConfig, digiConfig, filter)
	handler.connectedDigipeater = NewConnectedDigipeater(audioConfig, new(cdigi_config_s), filter)
	handler.ttGateway = NewTTGateway(audioConfig, handler.ttConfig, nil, handler.digipeater.Remember, 0)

	return &recPacketTest{handler: handler, audioConfig: audioConfig, waypoints: waypoints}
}

// processRecPacket runs app_process_rec_packet on channel 0 and returns what
// it printed, log entries included.
func (rt *recPacketTest) processRecPacket(t *testing.T, subchan int, slice int, pp *ax25.Packet, alevel ax25.ALevel, fecType fec_type_t, retries BitFixLevel, spectrum string) string {
	t.Helper()

	require.NotNil(t, pp)

	return testutils.CaptureOutput(t, func() {
		logrus.SetOutput(os.Stdout)
		rt.handler.app_process_rec_packet(t.Context(), 0, subchan, slice, pp, alevel, fecType, retries, spectrum)
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

	var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "Heard")
	assert.Contains(t, output, "heard=Q1TEST")
	assert.Contains(t, output, "Q1TEST>APRS,WIDE1-1:")
	assert.Contains(t, output, "!4221.60N/07103.60W-Test")

	var sentence = rt.readWaypoint(t)
	assert.Contains(t, sentence, "Q1TEST")

	var heard = rt.handler.heard.Count(0, 30)
	assert.Equal(t, 1, heard, "the station should have been remembered as heard")
}

func Test_app_process_rec_packet_object_name_is_waypoint(t *testing.T) {
	var rt = setupRecPacketTest(t)

	var pp = ax25.FromText("Q1TEST>APRS:;LEADER   *092345z4903.50N/07201.75W>088/036", true)

	rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, rt.readWaypoint(t), "LEADER")
}

func Test_app_process_rec_packet_heard_via_digipeater(t *testing.T) {
	var rt = setupRecPacketTest(t)

	var pp = ax25.FromText("Q1TEST>APRS,Q2TEST*:>status", true)

	var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "heard=Q2TEST")
	assert.Contains(t, output, "digipeater=true")
}

// Hearing WIDEn-0 most likely means hearing the station before it in the path.
func Test_app_process_rec_packet_heard_via_wide(t *testing.T) {
	var rt = setupRecPacketTest(t)

	for _, subchan := range []int{0, -2} {
		t.Run(strconv.Itoa(subchan), func(t *testing.T) {
			var pp = ax25.FromText("Q1TEST>APRS,Q2TEST*,WIDE2*:>status", true)

			var output = rt.processRecPacket(t, subchan, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

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

			var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), tt.fecType, tt.retries, "|")

			assert.Contains(t, output, tt.want)
		})
	}
}

func Test_app_process_rec_packet_audio_levels(t *testing.T) {
	var rt = setupRecPacketTest(t)

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)

	var output = rt.processRecPacket(t, 0, 0, pp, ax25.ALevel{Rec: 150, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")
	assert.Contains(t, output, "Audio input level is too high")

	pp = ax25.FromText("Q1TEST>APRS:>status", true)

	output = rt.processRecPacket(t, 0, 0, pp, ax25.ALevel{Rec: 1, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")
	assert.Contains(t, output, "Audio input level is too low")

	// A network TNC has no audio level to be low.
	pp = ax25.FromText("Q1TEST>APRS:>status", true)

	output = rt.processRecPacket(t, -3, 0, pp, ax25.ALevel{Rec: 1, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")
	assert.NotContains(t, output, "Audio input level is too low")
	assert.Contains(t, output, "subchan=nettnc")
}

func Test_app_process_rec_packet_quiet(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.handler.quietHeard = true
	rt.handler.quietDecode = true

	var pp = ax25.FromText("Q1TEST>APRS:!4221.60N/07103.60W-Test", true)

	var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.NotContains(t, output, "Heard")
	assert.Contains(t, output, "!4221.60N/07103.60W-Test")
}

func Test_app_process_rec_packet_timestamp(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.audioConfig.timestamp_format = "TS%Y"

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)

	var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, fmt.Sprintf("ts=TS%d", time.Now().Year()))
}

// A packet from the IGate's virtual channel is printed and passed to client
// applications, and goes no further.
func Test_app_process_rec_packet_from_igate_channel(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.audioConfig.igate_vchannel = 0

	var pp = ax25.FromText("Q1TEST>APRS:>status", true)

	var output = rt.processRecPacket(t, -2, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.NotContains(t, output, "Heard")
	assert.Contains(t, output, "subchan=is")
}

func Test_app_process_rec_packet_dtmf(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.handler.ttConfig.gateway_enabled = 1

	var pp = ax25.FromText("DTMF>APRS:t2A22A#", true)

	var output = rt.processRecPacket(t, -1, 0, pp, ax25.ALevel{Rec: -2, Mark: 0, Space: 0}, fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "subchan=dtmf")
}

// A touch tone sequence can be simulated with a packet whose information
// starts with 't'.
func Test_app_process_rec_packet_simulated_dtmf(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.handler.ttConfig.gateway_enabled = 1

	var pp = ax25.FromText("DTMF>APRS:t2A22A#", true)

	var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "heard=DTMF")
}

func Test_app_process_rec_packet_non_aprs(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.handler.dumpUTF8 = true
	rt.handler.dumpPackets = true

	var origLevel = logrus.GetLevel()

	t.Cleanup(func() { logrus.SetLevel(origLevel) })

	logrus.SetLevel(logrus.DebugLevel)

	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"

	t.Run("SABM", func(t *testing.T) {
		var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUSABM, 1, 0, nil)

		var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

		assert.Contains(t, output, "desc=")
		assert.Contains(t, output, "--debug p hexdump below")
	})

	t.Run("XID", func(t *testing.T) {
		var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUXID, 1, 0, nil)

		var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

		assert.Contains(t, output, "info=")
	})

	t.Run("non-printable", func(t *testing.T) {
		var pp = ax25.FromText("Q1TEST>APRS:>caf\xc3\xa9\x01", true)

		var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

		assert.Contains(t, output, "--debug u hexdump below")
	})
}

// A frame with no AX.25 addresses at all has nobody to have been heard.
func Test_app_process_rec_packet_no_addresses(t *testing.T) {
	var rt = setupRecPacketTest(t)

	var frame = []byte("abcdefghijklmnopqrstuvwxyz")

	var pp = ax25.FromFrame(frame, goodLevel())
	require.NotNil(t, pp)
	require.Equal(t, 0, pp.NumAddr())

	var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "Heard")
}

func Test_app_process_rec_packet_ais_to_object(t *testing.T) {
	var rt = setupRecPacketTest(t)

	rt.handler.aisToObject = true

	var sentence = aisPositionReport(t, 208, 900)
	var pp = ax25.FromText(fmt.Sprintf("Q1TEST>APRS:{%c%c%s", aprs.UserDefUserID, aprs.UserDefTypeAIS, sentence), true)

	var output = rt.processRecPacket(t, 0, 0, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "ais_obj_packet=")
	assert.Contains(t, output, ";366730000*")

	// The position goes out as a waypoint named after the vessel.
	assert.Contains(t, rt.readWaypoint(t), "366730000")
}

func Test_app_process_rec_packet_multiple_subchannels(t *testing.T) {
	var rt = setupRecPacketTest(t)

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

	var output = rt.processRecPacket(t, 1, 2, pp, goodLevel(), fec_type_none, RETRY_NONE, "")

	assert.Contains(t, output, "subchan=1")
	assert.Contains(t, output, "slice=2")
}
