// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The KISS command set is what a client application uses to drive the TNC:
// data frames to transmit, and the timing parameters that decide when they go
// out.  KissHandler.processMsg is where a frame with its escapes already removed is
// acted on, so it can be driven directly, with a recording stand-in for the
// function that answers the client.

// sentToClient is one message the TNC sent back to the client application:
// a frame, or the fake command prompt, which is text and has no channel or
// command.
type sentToClient struct {
	channel int
	cmd     int
	body    []byte
	prompt  bool
}

// recordingClient is a kissClient that keeps what it is sent, so a test can
// see what the TNC would have answered.
type recordingClient struct {
	sent *[]sentToClient
}

func (c recordingClient) reply(channel int, cmd int, frame []byte) {
	*c.sent = append(*c.sent, sentToClient{channel: channel, cmd: cmd, body: frame, prompt: false})
}

func (c recordingClient) prompt(text []byte) {
	*c.sent = append(*c.sent, sentToClient{channel: 0, cmd: 0, body: text, prompt: true})
}

func (c recordingClient) radioChannel(frameChannel int) int {
	return frameChannel
}

// recordingKissClient hands back a client that appends what it is sent to
// the slice it also returns.
func recordingKissClient() (*[]sentToClient, recordingClient) {
	var sent = new([]sentToClient)

	return sent, recordingClient{sent: sent}
}

// kissTestRadioConfig is the channel table the KISS tests hand to
// the KissHandler, to check a transmit
// request against: channels 0 and 1 are radios, and nothing else is set up.
func kissTestRadioConfig() *RadioConfig {
	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[1] = MEDIUM_RADIO

	return audioConfig
}

// setupKissProcessMsg hands back a KissHandler for the KISS tests to drive,
// with the channel table kissTestRadioConfig lays out, and transmit settings
// and a transmit queue of its own to put data frames on.
func setupKissProcessMsg(t *testing.T) *KissHandler {
	t.Helper()

	var audioConfig = kissTestRadioConfig()

	var tq = NewTransmitQueue()
	tq.Init(audioConfig)

	return NewKissHandler(audioConfig, new(XmitService), tq, nil)
}

// With KISSCOPY, a data frame a client sends is shown to the TCP clients the
// handler was given, whichever transport it came by - here, one with no TCP
// port of its own, such as the serial port.
func Test_kiss_process_msg_copies_to_its_peers(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var peers, clients = newAttachedKissNet(t, -1, true, 1)

	h.peers = peers

	var pp = newTestPacket(t)
	var _, from = recordingKissClient()

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), from)

	assert.Equal(t,
		kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)),
		readKissNetFrame(t, clients[0]))
	assert.Equal(t, 1, h.queue.Count(0, TQ_PRIO_1_LO, "", "", false))
}

// A data frame from the client is a frame to transmit, and an original - one
// with no used digipeater in it - goes on the low priority queue behind
// anything already being repeated.
func Test_kiss_process_msg_data_frame(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), from)

	assert.Equal(t, 1, h.queue.Count(0, TQ_PRIO_1_LO, "", "", false))
	assert.Equal(t, 0, h.queue.Count(0, TQ_PRIO_0_HI, "", "", false))
}

// A frame that has already been through a digipeater is somebody waiting on
// air for it, so it jumps the queue.
func Test_kiss_process_msg_repeated_frame_is_high_priority(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST,Q3TEST*:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), from)

	assert.Equal(t, 1, h.queue.Count(0, TQ_PRIO_0_HI, "", "", false))
}

// The channel is in the top half of the first byte, and a client asking for a
// channel we do not have is the AX.25-for-Linux CRC mode problem, which gets
// an explanation rather than a transmission.
func Test_kiss_process_msg_invalid_channel(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output = testutils.CaptureOutput(t, func() {
		h.processMsg(append([]byte{0x80 | kiss.CmdDataFrame}, pp.FrameData()...), from)
	})

	assert.Contains(t, output, "Invalid transmit channel 8 from KISS client app")
	assert.Contains(t, output, "kissparms -c 1 -p radio")
	assert.Equal(t, 0, h.queue.Count(8, -1, "", "", false))
}

// A frame whose only content is an escaped FEND reads as a request for channel
// 12, and the hex dump that goes with the explanation had nothing left to show
// once it skipped what it took for a leading FEND - it indexed past the end.
// Any client of the KISS ports could send it.
func Test_kiss_process_msg_invalid_channel_escaped_fend_only(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var _, from = recordingKissClient()
	var kc kiss.Collector

	var output = testutils.CaptureOutput(t, func() {
		for _, b := range []byte{kiss.FEND, kiss.FESC, kiss.TFEND, kiss.FEND} {
			h.RecByte(&kc, b, 0, from)
		}
	})

	assert.Contains(t, output, "Invalid transmit channel 12 from KISS client app")
}

// The debug print of a message has something to say even when there is nothing
// of the message to print.
func Test_kiss_debug_print_empty(t *testing.T) {
	for _, msg := range [][]byte{nil, {kiss.FEND}} {
		var hook = test.NewGlobal()
		t.Cleanup(hook.Reset)

		kiss_debug_print(FROM_CLIENT, "", msg)

		require.NotNil(t, hook.LastEntry(), "message %v", msg)
		assert.Equal(t, "Empty message for KISS client application", hook.LastEntry().Message)
	}
}

// A KISS TCP port carrying a single radio channel ignores the channel in the
// frame: the application thinks it has a one-radio TNC and always says 0.
func Test_kiss_process_msg_port_channel_overrides_the_frame(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kps = new(kissport_status_s)
	kps.channel = 1

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), kissNetClient{kns: nil, kps: kps, client: 0, conn: nil})

	assert.Equal(t, 1, h.queue.Count(1, TQ_PRIO_1_LO, "", "", false), "the port's channel should have been used")
	assert.Equal(t, 0, h.queue.Count(0, TQ_PRIO_1_LO, "", "", false))
}

// A KISS TCP port's channel comes from the configuration rather than a
// nibble, so nothing bounds it to the channel table.  The validity check used
// to go on to ask whether an out-of-range channel was the IGate's - indexing
// the table with the very channel it had just found to be out of range.
func Test_kiss_process_msg_port_channel_out_of_range(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kps = new(kissport_status_s)
	kps.channel = MAX_TOTAL_CHANS

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output string

	assert.NotPanics(t, func() {
		output = testutils.CaptureOutput(t, func() {
			h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), kissNetClient{kns: nil, kps: kps, client: 0, conn: nil})
		})
	})

	assert.Contains(t, output, "Invalid transmit channel 16 from KISS client app")
}

// Bytes that are not an AX.25 frame cannot be transmitted, and are reported
// rather than passed on.
func Test_kiss_process_msg_undecodable_data_frame(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var output = testutils.CaptureOutput(t, func() {
		h.processMsg([]byte{kiss.CmdDataFrame, 'n', 'o'}, from)
	})

	assert.Contains(t, output, "Invalid KISS data frame from client app")
}

// The timing parameters are the rest of what a client sets up, and each is one
// byte after the command.
func Test_kiss_process_msg_timing_parameters(t *testing.T) {
	var h = setupKissProcessMsg(t)
	var xs = h.xmit

	var _, from = recordingKissClient()

	h.processMsg([]byte{kiss.CmdTxDelay, 30}, from)
	h.processMsg([]byte{kiss.CmdPersistence, 63}, from)
	h.processMsg([]byte{kiss.CmdSlotTime, 10}, from)
	h.processMsg([]byte{kiss.CmdTxTail, 10}, from)
	h.processMsg([]byte{kiss.CmdFullDuplex, 1}, from)

	assert.Equal(t, 30, xs.timing[0].txdelay)
	assert.Equal(t, 63, xs.timing[0].persist)
	assert.Equal(t, 10, xs.timing[0].slottime)
	assert.Equal(t, 10, xs.timing[0].txtail)
	assert.True(t, xs.timing[0].fulldup)
}

// A value nobody would want on purpose is applied, because the client asked,
// but pointed at the part of the guide that explains what it means.
func Test_kiss_process_msg_extreme_timing_parameters(t *testing.T) {
	var h = setupKissProcessMsg(t)
	var xs = h.xmit

	var _, from = recordingKissClient()

	for _, c := range []struct {
		name string
		msg  []byte
	}{
		{"TXDELAY", []byte{kiss.CmdTxDelay, 200}},
		{"PERSIST", []byte{kiss.CmdPersistence, 1}},
		{"SLOTTIME", []byte{kiss.CmdSlotTime, 100}},
		{"TXTAIL", []byte{kiss.CmdTxTail, 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var output = testutils.CaptureOutput(t, func() { h.processMsg(c.msg, from) })

			assert.Contains(t, output, "Radio Channel - Transmit Timing")
		})
	}

	// Applied all the same.
	assert.Equal(t, 200, xs.timing[0].txdelay)
}

// A parameter command with no parameter is a protocol error, and leaves the
// setting alone rather than applying whatever happened to be next.
func Test_kiss_process_msg_missing_parameter(t *testing.T) {
	var h = setupKissProcessMsg(t)
	var xs = h.xmit

	var _, from = recordingKissClient()

	for _, c := range []struct {
		name string
		cmd  byte
	}{
		{"TXDELAY", kiss.CmdTxDelay},
		{"PERSISTENCE", kiss.CmdPersistence},
		{"SLOTTIME", kiss.CmdSlotTime},
		{"TXTAIL", kiss.CmdTxTail},
		{"FULLDUPLEX", kiss.CmdFullDuplex},
		{"SET HARDWARE", kiss.CmdSetHardware},
	} {
		t.Run(c.name, func(t *testing.T) {
			var output = testutils.CaptureOutput(t, func() { h.processMsg([]byte{c.cmd}, from) })

			assert.Contains(t, output, "KISS ERROR")
		})
	}

	assert.Equal(t, 0, xs.timing[0].txdelay)
}

// Leaving KISS mode is for a TNC that has another mode to go back to.  We
// don't, so it is noted and ignored.
func Test_kiss_process_msg_end_kiss(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var output = testutils.CaptureOutput(t, func() {
		h.processMsg([]byte{kiss.CmdEndKiss}, from)
	})

	assert.Contains(t, output, "end KISS mode - Ignored")
}

// A command we don't implement gets the troubleshooting tip, and the two
// XKISS ones additionally say what the application should be configured as
// instead.
func Test_kiss_process_msg_unsupported_commands(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var output = testutils.CaptureOutput(t, func() {
		h.processMsg([]byte{7}, from)
	})

	assert.Contains(t, output, "KISS Invalid command 7")
	assert.Contains(t, output, `Use "-d kn" option`)
	assert.NotContains(t, output, "XKISS")

	output = testutils.CaptureOutput(t, func() {
		h.processMsg([]byte{kiss.CmdXKissData}, from)
	})

	assert.Contains(t, output, `"XKISS" protocol which is not supported`)
	assert.Contains(t, output, "Winlink Express")

	output = testutils.CaptureOutput(t, func() {
		h.processMsg([]byte{kiss.CmdXKissPoll}, from)
	})

	assert.Contains(t, output, `"XKISS" protocol which is not supported`)
}

// "Set hardware" is the one command with an answer: the human readable
// question-and-answer form fldigi established, which several applications
// already speak.
func Test_kiss_set_hardware_tnc_version(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var sent, from = recordingKissClient()

	h.processMsg(append([]byte{kiss.CmdSetHardware}, []byte("TNC:")...), from)

	require.Len(t, *sent, 1)
	assert.Equal(t, kiss.CmdSetHardware, (*sent)[0].cmd)
	assert.Contains(t, string((*sent)[0].body), "DIREWOLF ")
}

// TXBUF is the one an application actually wanted: how much is still waiting
// to go out, so that it can throttle a long transmission.
func Test_kiss_set_hardware_txbuf(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var sent, from = recordingKissClient()

	kiss_set_hardware(0, []byte("TXBUF:"), from, h.queue)

	require.Len(t, *sent, 1)
	assert.Equal(t, "TXBUF:0", string((*sent)[0].body))

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	h.queue.Append(0, TQ_PRIO_1_LO, pp)

	kiss_set_hardware(0, []byte("TXBUF:"), from, h.queue)

	require.Len(t, *sent, 2)
	assert.NotEqual(t, "TXBUF:0", string((*sent)[1].body), "a queued frame should count towards TXBUF")
}

// A query with a parameter, a command we don't know, and one that isn't in the
// COMMAND:parameter form at all all get complained about rather than guessed
// at.
func Test_kiss_set_hardware_malformed(t *testing.T) {
	var sent, from = recordingKissClient()

	for _, c := range []struct {
		command string
		expect  string
	}{
		{"TNC:9", "Did not expect a parameter"},
		{"TXBUF:9", "Did not expect a parameter"},
		{"NOSUCH:", "unrecognized command: NOSUCH"},
		{"TNC", "expected the form COMMAND:"},
	} {
		t.Run(c.command, func(t *testing.T) {
			var output = testutils.CaptureOutput(t, func() {
				kiss_set_hardware(0, []byte(c.command), from, nil)
			})

			assert.Contains(t, output, c.expect)
		})
	}

	// The two queries still answer, wrong parameter or not.
	assert.Len(t, *sent, 2)
}

// RecByte is fed the client's byte stream one byte at a time, and acts on
// each whole frame kiss.Collector finds in it, and on the text around them.

// feedKissBytes hands the bytes to the collector one at a time, as a
// transport does, and returns whatever was sent back to the client.
func feedKissBytes(h *KissHandler, kf *kiss.Collector, debug int, data []byte) *[]sentToClient {
	var sent, from = recordingKissClient()

	for _, b := range data {
		h.RecByte(kf, b, debug, from)
	}

	return sent
}

func Test_KissRecByte_whole_frame(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	feedKissBytes(h, kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Equal(t, 1, h.queue.Count(0, TQ_PRIO_1_LO, "", "", false))
}

// An application that thinks it is talking to an old command-mode TNC sends
// lines of text.  Each one ending in a carriage return is answered with a
// command prompt, which is what stops it trying.
func Test_KissRecByte_command_prompt(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var sent = feedKissBytes(h, kf, 0, []byte("XFLOW OFF\r"))

	require.Len(t, *sent, 1)
	assert.Equal(t, "\r\ncmd:", string((*sent)[0].body))
	assert.True(t, (*sent)[0].prompt, "a text reply is not a frame")
}

// "RESTART" and "RESET" get an empty KISS frame instead, which is what the
// applications sending them are waiting for.
func Test_KissRecByte_restart(t *testing.T) {
	var h = setupKissProcessMsg(t)

	for _, word := range []string{"RESTART\r", "reset\r"} {
		t.Run(word, func(t *testing.T) {
			var kf = new(kiss.Collector)

			var sent = feedKissBytes(h, kf, 0, []byte(word))

			require.Len(t, *sent, 1)
			assert.Equal(t, "\xc0\xc0", string((*sent)[0].body))
		})
	}
}

// With the debug option on, the noise before a frame is shown, so that a
// client that is not being understood can be looked at.
func Test_KissRecByte_noise_is_printed(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(h, kf, 1, append([]byte("junk"), kiss.FEND))
	})

	assert.Contains(t, output, "Rejected Noise")
}

// FENDs with nothing between them are how some clients idle, and are not
// frames.
func Test_KissRecByte_empty_frames(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	feedKissBytes(h, kf, 0, []byte{kiss.FEND, kiss.FEND, kiss.FEND, kiss.FEND})

	assert.Equal(t, 0, h.queue.Count(0, -1, "", "", false))
}

// A client that never sends a closing FEND is told about once, not once for
// every byte past the limit.
func Test_KissRecByte_overlong_frame(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(h, kf, 0, append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...))
	})

	assert.Equal(t, 1, strings.Count(output, "KISS message exceeded maximum length"))
}

// The client does eventually send its closing FEND, and the byte it used to be
// written to was one past the end of the buffer - which took the whole program
// down, from anything that could talk to the KISS port.  The overlong frame is
// thrown away, and the collector is left ready for the next one.
func Test_KissRecByte_overlong_frame_closing_fend(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var overlong = append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(h, kf, 0, append(overlong, kiss.FEND))
	})

	assert.Contains(t, output, "KISS message exceeded maximum length.  Discarding it.")
	assert.Equal(t, 0, h.queue.Count(0, -1, "", "", false), "a fragment of the overlong frame was acted on")

	// And a well formed frame after it still gets through.
	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	feedKissBytes(h, kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Equal(t, 1, h.queue.Count(0, -1, "", "", false))
}

// With "-d kn" the frame is shown as it arrived and again as it was decoded,
// which is the pair a protocol problem shows up in.
func Test_KissRecByte_debug_prints_both_forms(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(h, kf, 2, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))
	})

	assert.Contains(t, output, "<<< Data frame from KISS client application, channel 0")
	assert.Contains(t, output, "Packet content after removing KISS framing")
}

// The per-client bookkeeping for KISS over TCP: a client's connection and the
// frame collector that goes with it are handed out together, and a connection
// is only forgotten by whoever still has the one that is current.

func Test_kissport_status_attach_and_detach(t *testing.T) {
	var kps = new(kissport_status_s)

	assert.Nil(t, kps.clientConn(0))
	assert.Equal(t, 0, kps.findFreeClient())

	var here, there = net.Pipe()

	t.Cleanup(func() { here.Close(); there.Close() })

	require.True(t, kps.attachClient(0, here))

	assert.Same(t, here, kps.clientConn(0))
	assert.Equal(t, 1, kps.findFreeClient(), "the next client should get the next slot")

	var conn, frame = kps.connAndFrame(0)
	assert.Same(t, here, conn)
	assert.NotNil(t, frame, "a newly attached client needs a frame collector")

	assert.True(t, kps.detachClientIfCurrent(0, here))
	assert.Nil(t, kps.clientConn(0))
}

// A read goroutine that notices its connection has gone must not clear a slot
// that has since been given to a newer connection from the same client.
func Test_kissport_status_detach_stale_connection(t *testing.T) {
	var kps = new(kissport_status_s)

	var older, otherEndOfOlder = net.Pipe()
	var newer, otherEndOfNewer = net.Pipe()

	t.Cleanup(func() {
		older.Close()
		otherEndOfOlder.Close()
		newer.Close()
		otherEndOfNewer.Close()
	})

	require.True(t, kps.attachClient(0, older))
	require.True(t, kps.attachClient(0, newer))

	assert.False(t, kps.detachClientIfCurrent(0, older), "the stale connection was not the current one")
	assert.Same(t, newer, kps.clientConn(0), "the newer connection was cleared by the older one going away")
}

// Every slot taken means there is nowhere to put another client.
func Test_kissport_status_all_clients_in_use(t *testing.T) {
	var kps = new(kissport_status_s)

	for c := range MAX_NET_CLIENTS {
		var here, there = net.Pipe()

		t.Cleanup(func() { here.Close(); there.Close() })

		require.True(t, kps.attachClient(c, here))
	}

	assert.Equal(t, -1, kps.findFreeClient())
}

// A port that has been stopped hangs up on everyone, and a connection that was
// already in the accept queue when it stopped is not attached after the fact.
func Test_kissport_status_stop(t *testing.T) {
	var kps = new(kissport_status_s)

	var here, there = net.Pipe()

	t.Cleanup(func() { here.Close(); there.Close() })

	require.True(t, kps.attachClient(0, here))

	kps.stop()

	assert.Nil(t, kps.clientConn(0))

	var late, otherEndOfLate = net.Pipe()

	t.Cleanup(func() { late.Close(); otherEndOfLate.Close() })

	assert.False(t, kps.attachClient(0, late), "a stopped port should not take on a new client")
}

// FEND FESC FEND is a frame, but escapes nothing, so there is no type byte
// once it is unescaped.  Three bytes from any KISS client used to take the
// whole program down, looking for one.
func Test_KissRecByte_frame_empty_once_unescaped(t *testing.T) {
	var h = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(h, kf, 2, []byte{kiss.FEND, kiss.FESC, kiss.FEND})
	})

	assert.Contains(t, output, "nothing in it")
	assert.Equal(t, 0, h.queue.Count(0, -1, "", "", false))
}

// What goes to a client is the type byte - channel in the top nybble, command
// in the bottom - then the frame, with KISS framing and escapes added.
func Test_kissClientFrame(t *testing.T) {
	var cases = []struct {
		name    string
		channel int
		cmd     int
		frame   []byte
		want    []byte
	}{
		{"data frame", 0, kiss.CmdDataFrame, []byte("abc"), []byte{kiss.FEND, 0x00, 'a', 'b', 'c', kiss.FEND}},
		{"channel and command", 3, kiss.CmdSetHardware, []byte("TNC:"), []byte{kiss.FEND, 0x36, 'T', 'N', 'C', ':', kiss.FEND}},
		{"escapes", 0, kiss.CmdDataFrame, []byte{kiss.FEND, kiss.FESC}, []byte{kiss.FEND, 0x00, kiss.FESC, kiss.TFEND, kiss.FESC, kiss.TFESC, kiss.FEND}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, kissClientFrame(c.channel, c.cmd, c.frame, 0, "Test"))
		})
	}
}

// A frame longer than AX.25 allows is cut to length before it is framed, so
// the client is sent what it was told it would get, and the user hears why.
func Test_kissClientFrame_truncates(t *testing.T) {
	var frame = bytes.Repeat([]byte{'x'}, ax25.MaxPacketLen+10)

	var got []byte

	var output = testutils.CaptureOutput(t, func() {
		got = kissClientFrame(0, kiss.CmdDataFrame, frame, 0, "Test")
	})

	assert.Contains(t, output, "Test KISS buffer too small.  Truncated.")

	// FEND, the type indicator, the frame, FEND - and 'x' needs no escaping.
	assert.Len(t, got, ax25.MaxPacketLen+3)
}
