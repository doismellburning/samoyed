// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"bytes"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/mheard"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// The packet decoders are the part of Samoyed that anyone on frequency can
// reach, and they take nothing more than a []byte or a string, so they are
// cheap to fuzz.  A target's job is to make the call; the failure it is
// looking for is a panic, so there is usually nothing to assert.
//
// Seeds added here run as ordinary unit tests under "go test", so an input
// that once crashed a decoder stays checked even when nobody is fuzzing.
// Fuzzing proper is "go test ./internal/direwolf/ -run XXX -fuzz FuzzSomething".

// The decoders narrate a malformed packet at length, and a fuzzing run has
// nobody to read it, so point stdout, and logrus, at the bin for the duration.
func fuzzQuietly(tb testing.TB) {
	tb.Helper()

	var devNull, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(tb, err)

	var saved = os.Stdout
	os.Stdout = devNull

	var savedLog = logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)

	tb.Cleanup(func() {
		os.Stdout = saved
		logrus.SetOutput(savedLog)
		devNull.Close()
	})
}

// FuzzAX25FromFrame covers the path every received frame takes, from the
// modem, a KISS client, a network TNC, an AGW client or IL2P: build a packet
// from the bytes off the air, then ask it the questions the receive path asks.
func FuzzAX25FromFrame(f *testing.F) {
	fuzzQuietly(f)

	// An ordinary APRS position report.
	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)
	f.Add(pp.FrameData())

	// Addresses and a control byte, with no PID and no information part:
	// the shortest frame AX25FromFrame accepts (issue #670).
	f.Add([]byte("000000000000010"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var pp = ax25.FromFrame(data, ax25.ALevel{Rec: 50, Mark: 50, Space: 50})
		if pp == nil {
			return
		}

		pp.FormatAddrs()
		pp.Info()
		pp.FormatViaPath()
		pp.FrameType()
		pp.IsAPRS()
		pp.DedupeCRC()
		pp.DTI()
		pp.CheckAddresses(ax25.AddrLenient)
	})
}

// FuzzAX25FromText covers the other way in: a monitor-format string, as the
// APRS-IS connection and the command line tools hand us.
func FuzzAX25FromText(f *testing.F) {
	fuzzQuietly(f)

	f.Add("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#")
	f.Add("Q1TEST>APDW17::Q2TEST   :Hello")
	f.Add(">:")

	f.Fuzz(func(t *testing.T, monitor string) {
		var pp = ax25.FromTextWithStrictness(monitor, ax25.AddrLenient)
		if pp == nil {
			return
		}

		pp.FormatAddrs()
		pp.Info()
		pp.FrameType()
	})
}

// FuzzIL2PDecodeFrame covers the IL2P receive path: header FEC, descrambling
// and the payload blocks.
func FuzzIL2PDecodeFrame(f *testing.F) {
	fuzzQuietly(f)

	il2p_init(0)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	for _, version := range []il2p_version_t{IL2P_VERSION_0_4, IL2P_VERSION_0_6} {
		var encoded, length = il2p_encode_frame(pp, version, 0)
		require.Positive(f, length)
		f.Add(encoded, int(version))
	}

	f.Add(make([]byte, 30), int(IL2P_VERSION_0_4))

	f.Fuzz(func(t *testing.T, irec []byte, version int) {
		il2p_decode_frame(irec, il2p_version_t(version))
	})
}

// kissFuzzMaxStream bounds the streams the KISS targets try.  Room for an
// overlong frame and a few hundred short ones is all the collector needs, and
// the queues they land on are only drained by threads a fuzzing run doesn't
// have, so a much longer stream spends its time walking them, not finding
// anything.
const kissFuzzMaxStream = 4 * kiss.MaxFrameLen

// kissFuzzSeeds are streams a KISS peer might send, well formed or not, for
// the targets that take one.
func kissFuzzSeeds(f *testing.F) {
	f.Helper()

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	var frame = kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...))

	for _, seed := range [][]byte{
		frame,
		append([]byte("XFLOW OFF\rKISS ON\rRESTART\r"), frame...),
		{kiss.FEND, kiss.FESC, kiss.FEND}, // Nothing in it once unescaped - used to crash.
		{kiss.FEND, kiss.CmdTxDelay, 30, kiss.FEND, kiss.FEND, kiss.CmdPersistence, kiss.FEND},
		kiss.Encapsulate([]byte("\x06TNC:")),
		kiss.Encapsulate([]byte("\x06TXBUF:")),
		{kiss.FEND, 0xff, kiss.FEND},
		append(append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen)...), kiss.FEND),
	} {
		for debug := range byte(3) {
			f.Add(seed, debug)
		}
	}
}

// FuzzKissRecByte covers what a KISS client application sends the TNC, over
// the TCP port, the serial port or the pseudo terminal: anyone who can reach
// one of those reaches this, frame collection and command handling both.
func FuzzKissRecByte(f *testing.F) {
	fuzzQuietly(f)
	kissFuzzSeeds(f)

	f.Fuzz(func(t *testing.T, stream []byte, debug byte) {
		if len(stream) > kissFuzzMaxStream {
			t.Skip()
		}

		setupKissProcessMsg(t)

		var audioConfig = kissTestRadioConfig()
		var _, sendfun = recordingSendfun()
		var kc kiss.Collector

		for _, b := range stream {
			KissRecByte(&kc, audioConfig, b, int(debug%3), nil, -1, sendfun)
		}
	})
}

// FuzzNetTNCRecByte covers what a network TNC sends us, for the channel it is
// attached to - it is at the far end of a TCP connection, and may not be ours.
func FuzzNetTNCRecByte(f *testing.F) {
	fuzzQuietly(f)
	kissFuzzSeeds(f)

	f.Fuzz(func(t *testing.T, stream []byte, debug byte) {
		if len(stream) > kissFuzzMaxStream {
			t.Skip()
		}

		expectReceivedFrames(t)

		var kc kiss.Collector

		for _, b := range stream {
			nettncRecByte(&kc, b, int(debug%3), nettncTestChannel)
		}
	})
}

// igateFuzzMaxStream bounds what FuzzIGateServerLines feeds the IGate.  A few
// lines' worth, including one over the limit, is all the reader has to get
// right.
const igateFuzzMaxStream = 4 * igateMaxLineLen

// FuzzIGateServerLines covers what the APRS-IS server sends the IGate: lines
// gathered from its byte stream, then each one shown, remembered, or turned
// into a frame for the radio and for a client application.  The server is
// across the internet, and anything sent to it by anyone comes back out.
func FuzzIGateServerLines(f *testing.F) {
	fuzzQuietly(f)

	for _, seed := range []string{
		"# aprsc 2.1.19-g730c5c0\r\n# logresp Q1TEST verified, server T2TEST\r\n",
		"Q2TEST-1>APWW10,TCPIP*,qAC,T2TEST:>hello\r\n",
		"Q2TEST>APDW17,WIDE1-1,qAR,Q3TEST:!4237.14N/07120.83W#\r\n",
		"WHO-IS>APJIW4,TCPIP*,qAC,AE5PL-JF::Q2TEST   :Hello there{583\r\n",
		"Q2TEST>APWW10,TCPIP*,qAC,T2TEST:}Q3TEST>APDW17,TCPIP,Q2TEST*:>third party\r\n",
		"Q2TEST>APWW10,TCPIP*,qAC,T2TEST:>nul\x00inside\r\n",
		"Q2TEST>APWW10,NOGATE,qAC,T2TEST:>not for RF\r\n",
		"\r\n\n\r\n",
		strings.Repeat("A", igateMaxLineLen) + "\r\nQ2TEST>APWW10,TCPIP*,qAC,T2TEST:>after\r\n",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, stream []byte) {
		if len(stream) > igateFuzzMaxStream {
			t.Skip()
		}

		setupIGateFromServer(t)

		var lines = new(igateLineReader)

		for _, b := range stream {
			if line, complete := lines.add(b); complete {
				igate.processServerLine(line)
			}
		}
	})
}

// setupIGateFromServer gives FuzzIGateServerLines an IGate that will pass
// what it hears from the server both to the radio and to ICHANNEL, with
// nothing connected - processServerLine doesn't need the socket.
func setupIGateFromServer(t *testing.T) {
	t.Helper()

	var origIGate, origMheard = igate, mheardDB

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.mycall[0] = "Q1TEST"
	audioConfig.igate_vchannel = 1

	var igateConfig = new(igate_config_s)
	igateConfig.tx_chan = 0
	igateConfig.tx_limit_1 = IGATE_TX_LIMIT_1_DEFAULT
	igateConfig.tx_limit_5 = IGATE_TX_LIMIT_5_DEFAULT
	igateConfig.igmsp = 1

	igate = NewIGate(audioConfig, igateConfig, new(digi_config_s), NewPacketFilter(igateConfig, nil, 0), 0)
	mheardDB = mheard.New(0)

	transmitQueue.Init(audioConfig)
	dataLinkQueue.Init()

	t.Cleanup(func() {
		igate, mheardDB = origIGate, origMheard

		for p := range TQ_NUM_PRIO {
			for transmitQueue.Remove(0, p) != nil { //revive:disable-line:empty-block
			}
		}

		dataLinkQueue.Init()
	})
}

// fx25FuzzMaxStream bounds the bytes, each eight received bits, the FX.25
// target feeds the receiver.  The largest codeblock, tag and all, is under
// 300 bytes, so this is room for a handful of them back to back.
const fx25FuzzMaxStream = 2048

// FuzzFX25RecBit covers the FX.25 receive path: hunting the bit stream for a
// correlation tag, gathering the codeblock it announces, then the
// Reed-Solomon decoder and the HDLC unstuffing of what it hands back.  Anyone
// transmitting on the channel controls those bits.
func FuzzFX25RecBit(f *testing.F) {
	fuzzQuietly(f)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	// A real codeblock for each correlation tag, so the fuzzer starts from
	// something the receiver accepts.  Only the short test frame fits the
	// smallest of them.
	for ctag := CTAG_MIN; ctag <= CTAG_MAX; ctag++ {
		var ctagNum, data, check = fx25_encode_frame(0, slices.Clone(fxTestFrame), 100+ctag, 0)
		require.Equal(f, ctag, ctagNum)
		f.Add(fxTestBlock(ctagNum, data, check), byte(0))
	}

	// The APRS frame, as an ordinary FX.25 configuration would send it.
	for _, checkBytes := range []int{16, 32, 64} {
		var ctagNum, data, check = fx25_encode_frame(0, pp.FrameData(), checkBytes, 0)
		require.Positive(f, ctagNum)

		var block = fxTestBlock(ctagNum, data, check)
		var frames, _ = fxTestReceive(block)
		require.Len(f, frames, 1, "the seed should be one the receiver accepts")
		f.Add(block, byte(2))

		// More damage than the check bytes can repair.
		var damaged = slices.Clone(block)
		for j := 24; j < 24+checkBytes; j++ {
			damaged[j] ^= 0xff
		}

		f.Add(damaged, byte(3))
	}

	f.Fuzz(func(t *testing.T, stream []byte, debug byte) {
		if len(stream) > fx25FuzzMaxStream {
			t.Skip()
		}

		// What the receive path does next with a frame, short of queueing it.
		var sink = func(channel int, subchannel int, slice int, frame []byte, derrors int) {
			ax25.FromFrame(frame, ax25.ALevel{Rec: 50, Mark: 50, Space: 50})
		}

		var rx = newFX25Receiver(0, 0, 0, int(debug%4), sink)

		for _, b := range stream {
			for imask := byte(0x01); imask != 0; imask <<= 1 {
				rx.recBit(int(b & imask))
			}
		}
	})
}

// fuzzAGWServer returns a server with a client attached as client 0, by way of
// a pipe whose far end is read and thrown away, and makes it the one the
// connected-mode link reports to.  Nothing a target does then blocks on a
// reply that nobody collects.
func fuzzAGWServer(tb testing.TB) *AGWServer {
	tb.Helper()

	var s = new(AGWServer)

	var ours, theirs = net.Pipe()
	s.clients[0].conn = ours

	go io.Copy(io.Discard, theirs) //nolint:errcheck // Ends when the pipe is closed, which is all it can report.

	var saved = agwServer
	agwServer = s

	tb.Cleanup(func() {
		agwServer = saved

		ours.Close()
		theirs.Close()
	})

	return s
}

// fuzzLinkConfig is the radio configuration the connected-mode targets run
// with: one radio channel, 0, which is where everything they do happens.
func fuzzLinkConfig() *RadioConfig {
	var cfg = new(RadioConfig)
	cfg.chan_medium[0] = MEDIUM_RADIO

	return cfg
}

// fuzzLinkKeep puts the connected-mode link back as it was once a target has
// finished with it, for the sake of whatever test runs next.
func fuzzLinkKeep(tb testing.TB) {
	tb.Helper()

	var saved = *ax25Link

	tb.Cleanup(func() {
		*ax25Link = saved

		dataLinkQueue.Init()
	})
}

// linkFuzzPaclen is the longest information part the connected-mode targets'
// link will send in one frame.
const linkFuzzPaclen = 64

// fuzzLinkReset gives each run of a connected-mode target a link with no
// state machines, nothing registered and nothing queued either way, so one
// input cannot leave anything behind for the next.
func fuzzLinkReset(cfg *RadioConfig, v22 bool) {
	transmitQueue.Init(cfg)
	dataLinkQueue.Init()

	var miscConfig = new(misc_config_s)
	// Shorter than the default, so a client's data can be long enough to
	// need splitting into several frames.
	miscConfig.paclen = linkFuzzPaclen
	miscConfig.retry = AX25_N2_RETRY_DEFAULT
	miscConfig.frack = AX25_T1V_FRACK_DEFAULT
	miscConfig.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT
	miscConfig.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT

	if v22 {
		miscConfig.maxv22 = 3
	}

	*ax25Link = *NewAX25Link()
	ax25_link_init(miscConfig, 0)
}

// fuzzLinkDrain does what recv_process does with everything on the data link
// queue, for the items that concern the connected-mode link: there is no
// receive thread in a fuzzing run to do it.
func fuzzLinkDrain() {
	for {
		var item = dataLinkQueue.Remove()
		if item == nil {
			return
		}

		switch item._type {
		case DLQ_REC_FRAME:
			lm_data_indication(item)
		case DLQ_CONNECT_REQUEST:
			dl_connect_request(item)
		case DLQ_DISCONNECT_REQUEST:
			dl_disconnect_request(item)
		case DLQ_XMIT_DATA_REQUEST:
			dl_data_request(item)
		case DLQ_REGISTER_CALLSIGN:
			dl_register_callsign(item)
		case DLQ_UNREGISTER_CALLSIGN:
			dl_unregister_callsign(item)
		case DLQ_OUTSTANDING_FRAMES_REQUEST:
			dl_outstanding_frames_request(item)
		case DLQ_CHANNEL_BUSY:
			lm_channel_busy(item)
		case DLQ_SEIZE_CONFIRM:
			lm_seize_confirm(item)
		case DLQ_CLIENT_CLEANUP:
			dl_client_cleanup(item)
		}

		dataLinkQueue.Delete(item)
	}
}

// fuzzLinkExpireTimers lets enough time pass for every running, unpaused timer
// to run out, without the run having to wait for it.
func fuzzLinkExpireTimers() {
	var now = time.Now()

	for p := ax25Link.listHead; p != nil; p = p.next {
		if !p.t1_exp.IsZero() && p.t1_paused_at.IsZero() {
			p.t1_exp = now
		}

		if !p.t3_exp.IsZero() {
			p.t3_exp = now
		}

		if !p.tm201_exp.IsZero() && p.tm201_paused_at.IsZero() {
			p.tm201_exp = now
		}
	}

	dl_timer_expiry()
}

// The connected-mode target's input is a script of things happening to the
// link, each introduced by one of these, in its low three bits.
const (
	// A frame from Q2TEST to Q1TEST.  The two bits above the op are the C
	// bits of the destination and source addresses, then a length byte, then
	// that many bytes of frame from the control field on.
	linkOpFrame byte = iota
	linkOpSeizeConfirm
	linkOpExpireTimers
	// Client 0 sends Q2TEST data: a length byte, then the data.
	linkOpData
	linkOpConnect
	linkOpDisconnect
	// The bit above the op says whether it is DCD or PTT that changed, the
	// one above that whether it went on or off.
	linkOpChannelBusy
	linkOpOutstanding

	linkOpCount
)

const (
	linkCmd byte = 2 // Destination C bit set: a command.
	linkRes byte = 1 // Source C bit set: a response.
)

// linkFuzzMaxScript bounds the connected-mode target's script.  A few dozen
// events take a link anywhere it can go; a longer script spends its time
// building ever longer transmit queues.
const linkFuzzMaxScript = 2048

// linkFuzzScript builds a script for the connected-mode target.
type linkFuzzScript []byte

func (s linkFuzzScript) frame(cr byte, body ...byte) linkFuzzScript {
	return append(append(s, linkOpFrame|cr<<3, byte(len(body))), body...)
}

func (s linkFuzzScript) data(data string) linkFuzzScript {
	return append(append(s, linkOpData, byte(len(data))), data...)
}

func (s linkFuzzScript) busy(dcd bool, on bool) linkFuzzScript {
	var op = linkOpChannelBusy

	if dcd {
		op |= 1 << 3
	}

	if on {
		op |= 1 << 4
	}

	return append(s, op)
}

func (s linkFuzzScript) op(op byte) linkFuzzScript {
	return append(s, op)
}

// linkFuzzChunk takes a length byte and up to that many bytes after it off the
// front of script.
func linkFuzzChunk(script []byte) ([]byte, []byte) {
	if len(script) == 0 {
		return nil, nil
	}

	var n = min(int(script[0]), len(script)-1)

	return script[1 : 1+n], script[1+n:]
}

// linkFuzzAddrs is the address part of a frame from Q2TEST to Q1TEST, with
// its C bits set from cr.
func linkFuzzAddrs(cr byte) []byte {
	var addrs = make([]byte, 0, 2*7)

	for _, c := range []byte("Q1TEST") {
		addrs = append(addrs, c<<1)
	}

	addrs = append(addrs, 0x60|(cr&2)<<6)

	for _, c := range []byte("Q2TEST") {
		addrs = append(addrs, c<<1)
	}

	return append(addrs, 0x61|(cr&1)<<7)
}

// FuzzAX25Link covers the connected-mode link: the state machines a station on
// frequency drives by sending us frames.  It plays out a script of frames from
// the station, client requests and timers running out against a link with
// Q1TEST registered for incoming connections, so a frame can reach every
// state, not just the disconnected one every link starts in.
func FuzzAX25Link(f *testing.F) {
	fuzzQuietly(f)
	fuzzLinkKeep(f)
	fuzzAGWServer(f)

	var xid xid_param_s
	xid.full_duplex = maybe.Just(false)
	xid.srej = srej_single
	xid.modulo = 128
	xid.i_field_length_rx = maybe.Just(256)
	xid.window_size_rx = maybe.Just(32)
	xid.ack_timer = maybe.Just(3000)
	xid.retries = maybe.Just(10)

	var xidInfo = xid_encode(&xid, ax25.CRCmd)

	for _, script := range []linkFuzzScript{
		// They connect to us, send a couple of frames, take some back,
		// and hang up.
		linkFuzzScript{}.
			frame(linkCmd, 0x3f). // SABM, P
			op(linkOpSeizeConfirm).
			frame(linkCmd, 0x00, 0xf0, 'H', 'e', 'l', 'l', 'o'). // I, N(S)=0, N(R)=0
			frame(linkCmd, 0x02, 0xf0, '!').                     // I, N(S)=1, N(R)=0
			frame(linkCmd, 0x06, 0xf0, '?').                     // I, N(S)=3: out of sequence
			op(linkOpSeizeConfirm).
			frame(linkCmd, 0x11). // RR, P
			data("Hello yourself").
			data(strings.Repeat("Long enough to be split. ", 8)).
			op(linkOpSeizeConfirm).
			frame(linkRes, 0x21). // RR, N(R)=1
			op(linkOpOutstanding).
			frame(linkCmd, 0x53), // DISC, P

		// We connect to them, and they make heavy weather of taking the data.
		linkFuzzScript{}.
			op(linkOpConnect).
			frame(linkRes, 0x73). // UA, F
			data("One").
			data("Two").
			data("Three").
			op(linkOpSeizeConfirm).
			op(linkOpExpireTimers).
			frame(linkRes, 0x09). // REJ, N(R)=0
			op(linkOpSeizeConfirm).
			frame(linkRes, 0x2d). // SREJ, N(R)=1
			frame(linkRes, 0x45). // RNR, N(R)=2
			op(linkOpExpireTimers).
			frame(linkRes, 0x71). // RR, F, N(R)=3
			busy(true, true).
			op(linkOpExpireTimers).
			busy(true, false).
			op(linkOpDisconnect).
			frame(linkRes, 0x73), // UA, F

		// They connect with v2.2, negotiate, test, and give up on us.
		linkFuzzScript{}.
			frame(linkCmd, 0x7f).                       // SABME, P
			frame(linkCmd, 0x00, 0x01, 0xf0, 'a', 'b'). // I, modulo 128, P
			frame(linkCmd, append([]byte{0xbf}, xidInfo...)...).
			frame(linkCmd, 0xf3, 't', 'e', 's', 't'). // TEST, P
			frame(linkCmd, 0x03, 0xf0, 'U', 'I').     // UI
			data(strings.Repeat("Long enough to be segmented. ", 8)).
			op(linkOpSeizeConfirm).
			frame(linkRes, 0x87, 0x00, 0x00, 0x00). // FRMR
			op(linkOpExpireTimers).
			frame(linkRes, 0x1f), // DM, F

		// We try to connect and nobody answers.
		linkFuzzScript{}.
			op(linkOpConnect).
			op(linkOpExpireTimers).op(linkOpExpireTimers).op(linkOpExpireTimers).
			op(linkOpExpireTimers).op(linkOpExpireTimers).op(linkOpExpireTimers).
			op(linkOpExpireTimers).op(linkOpExpireTimers).op(linkOpExpireTimers).
			op(linkOpExpireTimers).op(linkOpExpireTimers).op(linkOpExpireTimers),

		// We try to connect and they refuse.
		linkFuzzScript{}.
			op(linkOpConnect).
			frame(linkRes, 0x1f), // DM, F
	} {
		f.Add([]byte(script), false)
		f.Add([]byte(script), true)
	}

	var cfg = fuzzLinkConfig()
	var alevel = ax25.ALevel{Rec: 50, Mark: 50, Space: 50}

	var addrs [ax25.MaxAddrs]string
	addrs[OWNCALL] = "Q1TEST"
	addrs[PEERCALL] = "Q2TEST"

	f.Fuzz(func(t *testing.T, script []byte, v22 bool) {
		if len(script) > linkFuzzMaxScript {
			t.Skip()
		}

		fuzzLinkReset(cfg, v22)
		dataLinkQueue.RegisterCallsign("Q1TEST", 0, 0)
		fuzzLinkDrain()

		for len(script) > 0 {
			var op = script[0]
			script = script[1:]

			var chunk []byte

			switch op % linkOpCount {
			case linkOpFrame:
				chunk, script = linkFuzzChunk(script)

				var pp = ax25.FromFrame(append(linkFuzzAddrs(op>>3), chunk...), alevel)
				if pp != nil {
					dataLinkQueue.RecFrame(0, 0, 0, pp, alevel, fec_type_none, RETRY_NONE, "")
				}

			case linkOpSeizeConfirm:
				dataLinkQueue.SeizeConfirm(0)

			case linkOpExpireTimers:
				fuzzLinkExpireTimers()

			case linkOpData:
				chunk, script = linkFuzzChunk(script)
				dataLinkQueue.XmitDataRequest(addrs, 2, 0, 0, 0xf0, chunk)

			case linkOpConnect:
				dataLinkQueue.ConnectRequest(addrs, 2, 0, 0, 0xf0)

			case linkOpDisconnect:
				dataLinkQueue.DisconnectRequest(addrs, 2, 0, 0)

			case linkOpChannelBusy:
				var activity = OCTYPE_PTT
				if op&(1<<3) != 0 {
					activity = OCTYPE_DCD
				}

				dataLinkQueue.ChannelBusy(0, activity, int(op>>4)&1)

			case linkOpOutstanding:
				dataLinkQueue.OutstandingFramesRequest(addrs, 2, 0, 0)
			}

			fuzzLinkDrain()
		}
	})
}
