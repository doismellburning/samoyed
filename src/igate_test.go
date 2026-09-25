package direwolf

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_is_message_message(t *testing.T) {
	tests := []struct {
		name  string
		infop string
		want  bool
	}{
		{
			name:  "no colon prefix",
			infop: "W1AW>APRS:Hello",
			want:  false,
		},
		{
			name:  "too short",
			infop: ":ABCDE",
			want:  false,
		},
		{
			name:  "exactly 10 chars (too short for addressee delimiter)",
			infop: ":123456789",
			want:  false,
		},
		{
			name:  "telemetry PARM keyword",
			infop: ":ABCDEFGHI:PARM.something",
			want:  false,
		},
		{
			name:  "telemetry UNIT keyword",
			infop: ":ABCDEFGHI:UNIT.something",
			want:  false,
		},
		{
			name:  "telemetry EQNS keyword",
			infop: ":ABCDEFGHI:EQNS.something",
			want:  false,
		},
		{
			name:  "telemetry BITS keyword",
			infop: ":ABCDEFGHI:BITS.something",
			want:  false,
		},
		{
			name:  "bulletin BLN prefix",
			infop: ":BLN_someXX:this is a bulletin",
			want:  false,
		},
		{
			name:  "weather NWS prefix",
			infop: ":NWS_someXX:weather alert",
			want:  false,
		},
		{
			name:  "weather SKY prefix",
			infop: ":SKY_someXX:sky forecast",
			want:  false,
		},
		{
			name:  "weather CWA prefix",
			infop: ":CWA_someXX:watch area",
			want:  false,
		},
		{
			name:  "weather BOM prefix",
			infop: ":BOM_someXX:bureau message",
			want:  false,
		},
		{
			name:  "valid message",
			infop: ":W1AW     :Hello there!",
			want:  true,
		},
		{
			name:  "valid ack",
			infop: ":W1AW     :ack42",
			want:  true,
		},
		{
			name:  "valid rej",
			infop: ":W1AW     :rej99",
			want:  true,
		},
		{
			name:  "exactly 11 chars, valid",
			infop: ":W1AW     :",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, is_message_message(tt.infop))
		})
	}
}

// The IGate is the bridge between the radio and APRS-IS, and most of what it
// does is decide what not to pass on.  The server end is a socket the test
// holds the other end of, so nothing here needs the internet.

// setupIGate points the IGate at a socket the test can read, logged in and
// ready to send, and hands back the server's end of the connection.
func setupIGate(t *testing.T) net.Conn {
	t.Helper()

	var origIGate, origMheard = igate, mheardDB

	t.Cleanup(func() {
		igate, mheardDB = origIGate, origMheard
	})

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.mycall[0] = "Q1TEST"

	var igateConfig = new(igate_config_s)
	igateConfig.t2_server_name = "localhost"
	igateConfig.t2_login = "Q1TEST"
	igateConfig.t2_passcode = "12345"
	igateConfig.tx_chan = 0
	igateConfig.tx_limit_1 = IGATE_TX_LIMIT_1_DEFAULT
	igateConfig.tx_limit_5 = IGATE_TX_LIMIT_5_DEFAULT
	igateConfig.igmsp = 1

	var digiConfig = new(digi_config_s)

	igate = NewIGate(audioConfig, igateConfig, digiConfig, 0)

	pfilter_init(igateConfig, 0)

	mheardDB = NewMHeardDB(0)

	var server, client = connectedTCPPair(t)

	igate.sock = client
	igate.okToSend = true

	return server
}

// readFromIGate reads one line - what the IGate separates its records with -
// from the server end.
func readFromIGate(t *testing.T, server net.Conn) string {
	t.Helper()

	require.NoError(t, server.SetReadDeadline(time.Now().Add(10*time.Second)))

	var line []byte

	var buf = make([]byte, 1)

	for {
		var n, err = server.Read(buf)
		require.NoError(t, err, "gave up waiting for a record; got %q so far", line)

		if n == 0 {
			continue
		}

		if buf[0] == '\n' {
			return string(bytes.TrimSuffix(line, []byte("\r")))
		}

		line = append(line, buf[0])
	}
}

// requireIGateSilent checks that nothing was passed on to the server.
func requireIGateSilent(t *testing.T, server net.Conn, msgAndArgs ...any) {
	t.Helper()

	require.NoError(t, server.SetReadDeadline(time.Now().Add(250*time.Millisecond)))

	var _, readErr = server.Read(make([]byte, 1))
	assert.Error(t, readErr, msgAndArgs...)
}

// A packet heard on the radio goes to the server with our own callsign
// appended to the path after a "q construct" saying how it got there.
func TestIGateSendRecPacket(t *testing.T) {
	var server = setupIGate(t)

	var pp = AX25FromText("Q2TEST>APDW17:=4237.14N/07120.83W#hello", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	assert.Equal(t, "Q2TEST>APDW17,qAR,Q1TEST:=4237.14N/07120.83W#hello", readFromIGate(t, server))
	assert.Equal(t, 1, igate.uplinkCount())
}

// An IGate that cannot transmit says qAO rather than qAR: it is receive-only,
// and the distinction matters to the network.
func TestIGateSendRecPacketReceiveOnly(t *testing.T) {
	var server = setupIGate(t)

	igate.config.tx_chan = -1

	var pp = AX25FromText("Q2TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	assert.Equal(t, "Q2TEST>APDW17,qAO,Q1TEST:>hello", readFromIGate(t, server))
}

// Nothing goes anywhere before the login has completed, or with no connection
// at all - a packet is dropped rather than queued for a server we may never
// reach.
func TestIGateSendRecPacketNotReady(t *testing.T) {
	var server = setupIGate(t)

	igate.okToSend = false

	var pp = AX25FromText("Q2TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	requireIGateSilent(t, server, "a packet was sent before the login completed")

	igate.okToSend = true
	igate.sock = nil

	assert.NotPanics(t, func() { igate.sendRecPacket(0, pp) })
}

// These path entries are how a station says "do not put this on the internet",
// and they are honoured in both directions.
func TestIGateSendRecPacketPathSaysNo(t *testing.T) {
	for _, via := range []string{"TCPIP", "TCPXX", "RFONLY", "NOGATE"} {
		t.Run(via, func(t *testing.T) {
			var server = setupIGate(t)

			var pp = AX25FromText("Q2TEST>APDW17,"+via+":>hello", true)
			require.NotNil(t, pp)

			igate.sendRecPacket(0, pp)

			requireIGateSilent(t, server, "a packet with %s in the path was passed on", via)
		})
	}
}

// A generic query is a request for every station in earshot to answer, which
// is not something to put on the internet.
func TestIGateSendRecPacketGenericQuery(t *testing.T) {
	var server = setupIGate(t)

	var pp = AX25FromText("Q2TEST>APDW17:?APRS?", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	requireIGateSilent(t, server, "a generic query was passed on")
}

// A packet with nothing in the information part says nothing, and the servers
// would only drop it.
func TestIGateSendRecPacketEmptyInformation(t *testing.T) {
	var server = setupIGate(t)

	var pp = AX25FromText("Q2TEST>APDW17:", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	requireIGateSilent(t, server, "a packet with no information part was passed on")
}

// A carriage return in the information part would look like the end of a
// record to the server, so the packet is cut there.
func TestIGateSendRecPacketCutAtCR(t *testing.T) {
	var server = setupIGate(t)

	var pp = AX25FromText("Q2TEST>APDW17:>before\rafter", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	assert.Equal(t, "Q2TEST>APDW17,qAR,Q1TEST:>before", readFromIGate(t, server))
}

// A third party packet carries somebody else's packet inside it; the payload
// is what goes to the server, not the wrapper.
func TestIGateSendRecPacketThirdParty(t *testing.T) {
	var server = setupIGate(t)

	var pp = AX25FromText("Q3TEST>APDW17:}Q2TEST>APDW17,Q3TEST*:>inner", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	assert.Equal(t, "Q2TEST>APDW17,Q3TEST*,qAR,Q1TEST:>inner", readFromIGate(t, server))
}

// FILTER on the channel-to-IGate pair is how the configuration narrows what
// reaches the internet.
func TestIGateSendRecPacketFiltered(t *testing.T) {
	var server = setupIGate(t)

	igate.digiConfig.filter_str[0][MAX_TOTAL_CHANS] = "b/Q9TEST"

	var pp = AX25FromText("Q2TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	requireIGateSilent(t, server, "a packet the filter rejected was passed on")
}

// The same packet arriving again by another digipeated route is the same
// packet: the servers drop duplicates, so there is no point sending them.
func TestIGateDropsDuplicates(t *testing.T) {
	var server = setupIGate(t)

	igate.config.rx2ig_dedupe_time = 30

	var pp = AX25FromText("Q2TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	assert.Equal(t, "Q2TEST>APDW17,qAR,Q1TEST:>hello", readFromIGate(t, server))

	igate.sendRecPacket(0, pp)

	requireIGateSilent(t, server, "the same packet was sent to the server twice")
}

// With duplicate checking switched off - which is the default - the same
// packet goes every time, so the servers can see the paths it took.
func TestIGateDedupeDisabled(t *testing.T) {
	var server = setupIGate(t)

	igate.config.rx2ig_dedupe_time = 0

	var pp = AX25FromText("Q2TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)
	readFromIGate(t, server)

	igate.sendRecPacket(0, pp)

	assert.Equal(t, "Q2TEST>APDW17,qAR,Q1TEST:>hello", readFromIGate(t, server))
}

// A server we can no longer write to is given up, so that the connecting
// thread makes a new one rather than us writing into a dead socket forever.
func TestIGateSendMsgWriteErrorClosesTheConnection(t *testing.T) {
	var server = setupIGate(t)

	require.NoError(t, server.Close())

	assert.Eventually(t, func() bool {
		igate.sendMsgToServer("test")

		return igate.sock == nil
	}, 10*time.Second, 50*time.Millisecond, "the dead connection was never given up")
}

// setupIGateToRadio adds what the IS>RF direction needs on top of setupIGate:
// a transmit queue for the frames it decides to send.
func setupIGateToRadio(t *testing.T) {
	t.Helper()

	setupIGate(t)

	var audioConfig = igate.audioConfig

	transmitQueue.Init(audioConfig)

	t.Cleanup(func() {
		for p := range TQ_NUM_PRIO {
			for transmitQueue.Remove(0, p) != nil { //revive:disable-line:empty-block
			}
		}
	})
}

// A packet from the server is transmitted wrapped as third party traffic,
// with the via path it arrived with replaced by TCPIP and our own callsign.
func TestIGateTransmitFromServer(t *testing.T) {
	setupIGateToRadio(t)

	igate.maybeXmitPacketFromIGate([]byte("Q2TEST-1>APWW10,TCPIP*,qAC,T2TEST:>hello"), 0)

	var sent = transmitQueue.Remove(0, TQ_PRIO_1_LO)
	require.NotNil(t, sent, "nothing was queued for transmission")

	var info = string(AX25GetInfo(sent))

	assert.Equal(t, "Q1TEST", ax25_get_addr_with_ssid(sent, AX25_SOURCE))
	assert.Equal(t, "}Q2TEST-1>APWW10,TCPIP,Q1TEST*:>hello", info)
	assert.Equal(t, 1, igate.stats.rfXmitPackets)
}

// These path entries say the packet should not go to RF, and qAX says it came
// from somewhere that did not identify itself properly.
func TestIGateTransmitPathSaysNo(t *testing.T) {
	for _, via := range []string{"qAX", "TCPXX", "RFONLY", "NOGATE"} {
		t.Run(via, func(t *testing.T) {
			setupIGateToRadio(t)

			igate.maybeXmitPacketFromIGate([]byte("Q2TEST>APWW10,"+via+":>hello"), 0)

			assert.Nil(t, transmitQueue.Remove(0, TQ_PRIO_1_LO), "a packet with %s in the path was transmitted", via)
		})
	}
}

// Something the parser cannot make sense of is reported rather than passed on
// as a frame that would not survive the trip.
func TestIGateTransmitUnparseable(t *testing.T) {
	setupIGateToRadio(t)

	var output = testutils.CaptureOutput(t, func() {
		igate.maybeXmitPacketFromIGate([]byte("this is not a packet"), 0)
	})

	assert.Contains(t, output, "Could not parse message from server")
	assert.Nil(t, transmitQueue.Remove(0, TQ_PRIO_1_LO))
}

// IGFILTER on the IGate-to-channel pair narrows what is put on the air.
func TestIGateTransmitFiltered(t *testing.T) {
	setupIGateToRadio(t)

	igate.digiConfig.filter_str[MAX_TOTAL_CHANS][0] = "b/Q9TEST"

	igate.maybeXmitPacketFromIGate([]byte("Q2TEST>APWW10,qAC,T2TEST:>hello"), 0)

	assert.Nil(t, transmitQueue.Remove(0, TQ_PRIO_1_LO), "a packet the filter rejected was transmitted")
}

// Having transmitted a message for somebody, we pass along their next
// position report even though the filter would otherwise drop it, so that the
// recipient can see where the sender is.
func TestIGateTransmitCourtesyPosition(t *testing.T) {
	setupIGateToRadio(t)

	// A filter that passes nothing, so only the special case can get through.
	igate.digiConfig.filter_str[MAX_TOTAL_CHANS][0] = "b/Q9TEST"

	// SetMSP only knows about stations that have been heard.
	mheardDB.SaveIS("Q2TEST>APWW10,qAC,T2TEST::Q3TEST   :Hello there")
	mheardDB.SetMSP("Q2TEST", 1)

	igate.maybeXmitPacketFromIGate([]byte("Q2TEST>APWW10,qAC,T2TEST:=4237.14N/07120.83W#"), 0)

	assert.NotNil(t, transmitQueue.Remove(0, TQ_PRIO_1_LO), "the message sender's position was not passed along")

	// Once only: the count is used up.
	igate.maybeXmitPacketFromIGate([]byte("Q2TEST>APWW10,qAC,T2TEST:=4237.14N/07120.83W#"), 0)

	assert.Nil(t, transmitQueue.Remove(0, TQ_PRIO_1_LO), "the special case should have been used up")
}

// Transmitting a message for a station is what arranges for its next position
// to be passed along, and it is counted separately in the IGate statistics.
func TestIGateTransmitMessageRemembersTheSender(t *testing.T) {
	setupIGateToRadio(t)

	// The station has to have been heard for us to remember anything about it.
	mheardDB.SaveIS("Q2TEST>APWW10,qAC,T2TEST::Q3TEST   :Hello there")

	igate.maybeXmitPacketFromIGate([]byte("Q2TEST>APWW10,qAC,T2TEST::Q3TEST   :Hello there"), 0)

	require.NotNil(t, transmitQueue.Remove(0, TQ_PRIO_1_LO))

	assert.Equal(t, 1, igate.msgCount())
	assert.Equal(t, 0, igate.pktCount(), "a message is not counted as an other packet")
	assert.Equal(t, igate.config.igmsp, mheardDB.GetMSP("Q2TEST"))
}

// The same packet again within the dedupe window is dropped: it has already
// been on the air once.
func TestIGToTxAllowDropsDuplicates(t *testing.T) {
	setupIGate(t)

	var pp = AX25FromText("Q2TEST>APWW10:>hello", true)
	require.NotNil(t, pp)

	assert.True(t, igate.igToTxAllow(pp, 0))

	igate.igToTxRemember(pp, 0, 0)

	var output = testutils.CaptureOutput(t, func() { assert.False(t, igate.igToTxAllow(pp, 0)) })

	assert.Contains(t, output, "Drop duplicate packet transmitted recently")

	// Another channel is a different transmission.
	assert.True(t, igate.igToTxAllow(pp, 1))
}

// The transmit history is remembered into by the digipeater from the receive
// thread, by APRStt object reports from an audio thread, and by the IGate's own
// APRS-IS to RF path, which also consults it.  Run under -race.
func TestIGToTxHistoryConcurrent(t *testing.T) {
	setupIGate(t)

	var pp = AX25FromText("Q2TEST>APWW10:>hello", true)
	require.NotNil(t, pp)

	var done = make(chan struct{})

	go func() {
		defer close(done)

		for range 100 {
			igate.igToTxRemember(pp, 0, 1)
		}
	}()

	testutils.CaptureOutput(t, func() {
		for range 100 {
			igate.igToTxAllow(pp, 0)
		}
	})

	<-done

	testutils.CaptureOutput(t, func() { assert.False(t, igate.igToTxAllow(pp, 0)) })
}

// A repeated "message" is a retry that did not get an ack, so it is not
// treated as a duplicate to be suppressed.
func TestIGToTxAllowKeepsDuplicateMessages(t *testing.T) {
	setupIGate(t)

	var pp = AX25FromText("Q2TEST>APWW10::Q3TEST   :Hello there", true)
	require.NotNil(t, pp)

	igate.igToTxRemember(pp, 0, 0)

	assert.True(t, igate.igToTxAllow(pp, 0), "a repeated message should be allowed through")
}

// There are limits on how much the IGate may put on the air, over a minute
// and over five, so that a busy server cannot swamp the channel.
func TestIGToTxAllowRateLimits(t *testing.T) {
	setupIGate(t)

	igate.config.tx_limit_1 = 2
	igate.config.tx_limit_5 = 100

	for i := range 2 {
		var pp = AX25FromText(fmt.Sprintf("Q2TEST>APWW10:>hello %d", i), true)
		require.NotNil(t, pp)

		require.True(t, igate.igToTxAllow(pp, 0))

		igate.igToTxRemember(pp, 0, 0)
	}

	var next = AX25FromText("Q2TEST>APWW10:>one too many", true)
	require.NotNil(t, next)

	var output = testutils.CaptureOutput(t, func() { assert.False(t, igate.igToTxAllow(next, 0)) })

	assert.Contains(t, output, "maximum of 2 packets in 1 minute")
}

// The five minute limit is separate, and reached by packets that the one
// minute limit has let through.
func TestIGToTxAllowFiveMinuteLimit(t *testing.T) {
	setupIGate(t)

	igate.config.tx_limit_1 = 100
	igate.config.tx_limit_5 = 3

	for i := range 3 {
		var pp = AX25FromText(fmt.Sprintf("Q2TEST>APWW10:>hello %d", i), true)
		require.NotNil(t, pp)

		igate.igToTxRemember(pp, 0, 0)
	}

	var next = AX25FromText("Q2TEST>APWW10:>one too many", true)
	require.NotNil(t, next)

	var output = testutils.CaptureOutput(t, func() { assert.False(t, igate.igToTxAllow(next, 0)) })

	assert.Contains(t, output, "maximum of 3 packets in 5 minutes")
}

// Messages get three times the limit: they are rare, deliberate, and it would
// be a shame to drop one because of the repetitive traffic around it.
func TestIGToTxAllowRaisesTheLimitForMessages(t *testing.T) {
	setupIGate(t)

	igate.config.tx_limit_1 = 1
	igate.config.tx_limit_5 = 100

	var pp = AX25FromText("Q2TEST>APWW10:>hello", true)
	require.NotNil(t, pp)

	igate.igToTxRemember(pp, 0, 0)

	var message = AX25FromText("Q2TEST>APWW10::Q3TEST   :Hello there", true)
	require.NotNil(t, message)

	assert.True(t, igate.igToTxAllow(message, 0), "a message should get three times the limit")
}

// The limit is on what the IGate transmits, so frames the digipeater sent do
// not count against it.
func TestIGToTxAllowIgnoresDigipeatedFrames(t *testing.T) {
	setupIGate(t)

	igate.config.tx_limit_1 = 1
	igate.config.tx_limit_5 = 100

	var pp = AX25FromText("Q2TEST>APWW10:>hello", true)
	require.NotNil(t, pp)

	igate.igToTxRemember(pp, 0, 1) // Transmitted by the digipeater, not the IGate.

	var next = AX25FromText("Q2TEST>APWW10:>something else", true)
	require.NotNil(t, next)

	assert.True(t, igate.igToTxAllow(next, 0), "a digipeated frame should not count against the IGate's limit")
}

// The dedupe history is a ring, so a packet falls out of it once enough
// others have gone by.
func TestIGToTxRememberWrapsAround(t *testing.T) {
	setupIGate(t)

	igate.config.tx_limit_1 = 1000
	igate.config.tx_limit_5 = 1000

	var first = AX25FromText("Q2TEST>APWW10:>first", true)
	require.NotNil(t, first)

	igate.igToTxRemember(first, 0, 0)

	for i := range IG2TX_HISTORY_MAX {
		var pp = AX25FromText(fmt.Sprintf("Q2TEST>APWW10:>filler %d", i), true)
		require.NotNil(t, pp)

		igate.igToTxRemember(pp, 0, 0)
	}

	assert.True(t, igate.igToTxAllow(first, 0), "the oldest entry should have fallen out of the history")
}

// The counters behind the IGate statistics beacon.
func TestIGateCounters(t *testing.T) {
	setupIGate(t)

	assert.Equal(t, 0, igate.msgCount())
	assert.Equal(t, 0, igate.pktCount())
	assert.Equal(t, 0, igate.uplinkCount())
	assert.Equal(t, 0, igate.downlinkCount())

	igate.stats.rfXmitPackets = 5
	igate.stats.msgCount = 2
	igate.stats.uplinkPackets = 7
	igate.stats.downlinkPackets = 9

	assert.Equal(t, 2, igate.msgCount())
	assert.Equal(t, 3, igate.pktCount(), "other packets are the ones that were not messages")
	assert.Equal(t, 7, igate.uplinkCount())
	assert.Equal(t, 9, igate.downlinkCount())
}

// SATgate mode holds back a packet heard directly from a satellite for a
// while, so that a terrestrial digipeat of the same packet - which carries
// more information about the path - gets to the server first.
func TestIGateSatgateDelaysDirectPackets(t *testing.T) {
	var server = setupIGate(t)

	t.Cleanup(func() { igate.dpQueueHead = nil })

	igate.config.satgate_delay = 1
	igate.dpQueueHead = nil

	// Heard directly - no digipeater has been used - but with a path, so
	// somebody else may yet repeat it.
	var pp = AX25FromText("Q2TEST>APDW17,WIDE1-1:>hello", true)
	require.NotNil(t, pp)

	var output = testutils.CaptureOutput(t, func() { igate.sendRecPacket(0, pp) })

	assert.Contains(t, output, "SATgate mode, delay packet heard directly")
	assert.NotNil(t, igate.dpQueueHead, "the packet was not put on the delay queue")

	requireIGateSilent(t, server, "the packet went to the server without being delayed")

	// The thread is what lets it go once its time has come.
	var ctx, cancel = context.WithCancel(t.Context())

	var done = make(chan struct{})

	go func() {
		defer close(done)

		igate.satgateDelayThread(ctx)
	}()

	assert.Equal(t, "Q2TEST>APDW17,WIDE1-1,qAR,Q1TEST:>hello", readFromIGate(t, server))

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("satgateDelayThread did not finish after its context was cancelled")
	}
}

// A packet that has already been through a digipeater was not heard directly,
// so there is nothing to wait for.
func TestIGateSatgateDoesNotDelayRepeatedPackets(t *testing.T) {
	var server = setupIGate(t)

	t.Cleanup(func() { igate.dpQueueHead = nil })

	igate.config.satgate_delay = 1
	igate.dpQueueHead = nil

	var pp = AX25FromText("Q2TEST>APDW17,Q3TEST*:>hello", true)
	require.NotNil(t, pp)

	igate.sendRecPacket(0, pp)

	assert.Equal(t, "Q2TEST>APDW17,Q3TEST*,qAR,Q1TEST:>hello", readFromIGate(t, server))
	assert.Nil(t, igate.dpQueueHead, "a repeated packet should not have been delayed")
}

// The delay queue keeps packets in the order they arrived, so that they reach
// the server in the order they were heard.
func TestIGateSatgateQueueKeepsOrder(t *testing.T) {
	setupIGate(t)

	t.Cleanup(func() { igate.dpQueueHead = nil })

	igate.config.satgate_delay = 60
	igate.dpQueueHead = nil

	for _, text := range []string{"Q2TEST>APDW17:>first", "Q2TEST>APDW17:>second"} {
		var pp = AX25FromText(text, true)
		require.NotNil(t, pp)

		testutils.CaptureOutput(t, func() { igate.satgateDelayPacket(pp, 0) })
	}

	require.NotNil(t, igate.dpQueueHead)
	assert.Equal(t, ">first", string(AX25GetInfo(igate.dpQueueHead)))

	var second = ax25_get_nextp(igate.dpQueueHead)
	require.NotNil(t, second, "the second packet was not queued behind the first")
	assert.Equal(t, ">second", string(AX25GetInfo(second)))
}

// "-d ig" and more of it prints what is happening at each stage, which is how
// somebody works out why their packets are not showing up on aprs.fi.
func TestIGateDebugOutput(t *testing.T) {
	var server = setupIGate(t)

	igate.debugLevel = 3
	igate.config.rx2ig_dedupe_time = 30

	var pp = AX25FromText("Q2TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	var output = testutils.CaptureOutput(t, func() { igate.sendRecPacket(0, pp) })

	assert.Contains(t, output, "[rx>ig]")
	assert.Contains(t, output, "rx_to_ig_allow? YES")
	assert.Contains(t, output, "rx_to_ig_remember")

	readFromIGate(t, server)

	// And the second time round, why it was dropped.
	output = testutils.CaptureOutput(t, func() { igate.sendRecPacket(0, pp) })

	assert.Contains(t, output, "rx_to_ig_allow? NO. Seen")
	assert.Contains(t, output, "Drop duplicate of same packet seen recently")
}

// The same for the other direction.
func TestIGateToRadioDebugOutput(t *testing.T) {
	setupIGateToRadio(t)

	igate.debugLevel = 3

	var pp = AX25FromText("Q2TEST>APWW10:>hello", true)
	require.NotNil(t, pp)

	var output = testutils.CaptureOutput(t, func() {
		assert.True(t, igate.igToTxAllow(pp, 0))

		igate.igToTxRemember(pp, 0, 0)

		assert.False(t, igate.igToTxAllow(pp, 0))
	})

	assert.Contains(t, output, "ig_to_tx_allow? YES")
	assert.Contains(t, output, "ig_to_tx_remember")
	assert.Contains(t, output, "ig_to_tx_allow? NO. Duplicate sent")

	// And a packet turned away for its path.
	output = testutils.CaptureOutput(t, func() {
		igate.maybeXmitPacketFromIGate([]byte("Q2TEST>APWW10,NOGATE:>hello"), 0)
	})

	assert.Contains(t, output, "Do not transmit with NOGATE in path")
}

// The RF>IS side says why it dropped something too.
func TestIGateSendRecPacketDebugOutput(t *testing.T) {
	setupIGate(t)

	igate.debugLevel = 1

	for _, c := range []struct {
		name   string
		text   string
		expect string
	}{
		{"path says no", "Q2TEST>APDW17,NOGATE:>hello", "Do not relay with NOGATE in path"},
		{"generic query", "Q2TEST>APDW17:?APRS?", "Do not relay generic query"},
		{"no information", "Q2TEST>APDW17:", "Information part length is zero"},
		{"third party", "Q3TEST>APDW17:}Q2TEST>APDW17,TCPIP:>inner", "Do not relay with TCPIP in path"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var pp = AX25FromText(c.text, true)
			require.NotNil(t, pp)

			var output = testutils.CaptureOutput(t, func() { igate.sendRecPacket(0, pp) })

			assert.Contains(t, output, c.expect)
		})
	}

	// And the filter, which is off by default and only says so with -d ig.
	igate.digiConfig.filter_str[0][MAX_TOTAL_CHANS] = "b/Q9TEST"

	var pp = AX25FromText("Q2TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	var output = testutils.CaptureOutput(t, func() { igate.sendRecPacket(0, pp) })

	assert.Contains(t, output, "was rejected by filter")
}
