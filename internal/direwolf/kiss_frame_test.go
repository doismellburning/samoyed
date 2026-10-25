// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The KISS command set is what a client application uses to drive the TNC:
// data frames to transmit, and the timing parameters that decide when they go
// out.  KissHandler.processMsg is where a frame with its escapes already removed is
// acted on, so it can be driven directly, with a recording stand-in for the
// function that answers the client.

// captureKissLog runs fn and returns what it wrote to stdout, logrus entries
// included.  samoyed-direwolf sends logrus to stdout alongside everything
// else, and any -d option turns on Debug, which is where the KISS debug
// prints go, so do the same here.
func captureKissLog(t *testing.T, fn func()) string {
	t.Helper()

	return testutils.CaptureOutput(t, func() {
		var oldOut = logrus.StandardLogger().Out
		var oldLevel = logrus.GetLevel()

		defer func() {
			logrus.SetOutput(oldOut)
			logrus.SetLevel(oldLevel)
		}()

		logrus.SetOutput(os.Stdout)
		logrus.SetLevel(logrus.DebugLevel)
		fn()
	})
}

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

func (c recordingClient) Reply(channel int, cmd int, frame []byte) {
	*c.sent = append(*c.sent, sentToClient{channel: channel, cmd: cmd, body: frame, prompt: false})
}

func (c recordingClient) Prompt(text []byte) {
	*c.sent = append(*c.sent, sentToClient{channel: 0, cmd: 0, body: text, prompt: true})
}

func (c recordingClient) RadioChannel(frameChannel int) int {
	return frameChannel
}

// recordingKissClient hands back a client that appends what it is sent to
// the slice it also returns.
func recordingKissClient() (*[]sentToClient, recordingClient) {
	var sent = new([]sentToClient)

	return sent, recordingClient{sent: sent}
}

// newKissTestPacket is a frame for a client to send.
func newKissTestPacket(t *testing.T) *ax25.Packet {
	t.Helper()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	return pp
}

// tncTransmission is a frame the handler asked the TNC to transmit.
type tncTransmission struct {
	channel int
	urgent  bool
	pp      *ax25.Packet
}

// tncTiming is the transmit timing a client set for a channel.
type tncTiming struct {
	txdelay, persist, slottime, txtail int
	fulldup                            bool
}

// recordingTNC is a kissHandlerTNC that keeps what it is asked to do, so a
// test can see what the handler made of a client's messages.  Channels 0 and 1
// are the ones it has, and queued is what it says is waiting to go out.
type recordingTNC struct {
	transmitted []tncTransmission
	timing      map[int]*tncTiming
	queued      int
}

func newRecordingTNC() *recordingTNC {
	var r = new(recordingTNC)
	r.timing = make(map[int]*tncTiming)

	return r
}

func (r *recordingTNC) Transmit(channel int, urgent bool, pp *ax25.Packet) {
	r.transmitted = append(r.transmitted, tncTransmission{channel: channel, urgent: urgent, pp: pp})
}

func (r *recordingTNC) QueuedBytes(int) int { return r.queued }

func (r *recordingTNC) ChannelValid(channel int) bool { return channel == 0 || channel == 1 }

// timingFor is the timing set for channel so far.
func (r *recordingTNC) timingFor(channel int) *tncTiming {
	if r.timing[channel] == nil {
		r.timing[channel] = new(tncTiming)
	}

	return r.timing[channel]
}

func (r *recordingTNC) SetTxdelay(channel, value int)  { r.timingFor(channel).txdelay = value }
func (r *recordingTNC) SetPersist(channel, value int)  { r.timingFor(channel).persist = value }
func (r *recordingTNC) SetSlottime(channel, value int) { r.timingFor(channel).slottime = value }
func (r *recordingTNC) SetTxtail(channel, value int)   { r.timingFor(channel).txtail = value }
func (r *recordingTNC) SetFulldup(channel int, value bool) {
	r.timingFor(channel).fulldup = value
}

// count is how many frames went to channel, urgent or not as asked.
func (r *recordingTNC) count(channel int, urgent bool) int {
	var n = 0

	for _, x := range r.transmitted {
		if x.channel == channel && x.urgent == urgent {
			n++
		}
	}

	return n
}

// countOn is how many frames went to channel at all.
func (r *recordingTNC) countOn(channel int) int {
	return r.count(channel, false) + r.count(channel, true)
}

// take is the first frame that went to channel, urgent or not as asked, or
// nil if none did.
func (r *recordingTNC) take(channel int, urgent bool) *ax25.Packet {
	for _, x := range r.transmitted {
		if x.channel == channel && x.urgent == urgent {
			return x.pp
		}
	}

	return nil
}

// setupKissProcessMsg hands back a KissHandler for the KISS tests to drive,
// and the recording TNC it hands what it is asked to do.
func setupKissProcessMsg(t *testing.T) (*KissHandler, *recordingTNC) {
	t.Helper()

	var tnc = newRecordingTNC()

	return NewKissHandler(t.Context(), tnc, nil), tnc
}

// copiedFrame is a data frame the handler had copied to the other clients.
type copiedFrame struct {
	msg     []byte
	channel int
	cmd     int
	from    kissClient
}

// recordingCopier keeps what its Copy, a kissCopier, is asked to copy.
type recordingCopier struct {
	copied []copiedFrame
}

func (r *recordingCopier) Copy(msg []byte, channel int, cmd int, from kissClient) {
	r.copied = append(r.copied, copiedFrame{msg: msg, channel: channel, cmd: cmd, from: from})
}

// A data frame a client sends is copied to the other clients - KISSCOPY - as
// well as transmitted, with the channel and command the handler made of it
// and the client it came from, so as not to be copied back.
func Test_kiss_process_msg_copies_to_its_peers(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var peers = new(recordingCopier)

	h.copyToPeers = peers.Copy

	var pp = newKissTestPacket(t)
	var _, from = recordingKissClient()

	var msg = append([]byte{0x10 | kiss.CmdDataFrame}, pp.FrameData()...)

	h.processMsg(msg, from)

	assert.Equal(t, []copiedFrame{{msg: msg, channel: 1, cmd: kiss.CmdDataFrame, from: from}}, peers.copied)
	assert.Equal(t, 1, tnc.count(1, false))
}

// Nor is anything but a data frame copied.
func Test_kiss_process_msg_copies_only_data_frames(t *testing.T) {
	t.Parallel()

	var h, _ = setupKissProcessMsg(t)

	var peers = new(recordingCopier)

	h.copyToPeers = peers.Copy

	var _, from = recordingKissClient()

	h.processMsg([]byte{kiss.CmdTxDelay, 30}, from)

	assert.Empty(t, peers.copied)
}

// A data frame from the client is a frame to transmit, and an original - one
// with no used digipeater in it - goes on the low priority queue behind
// anything already being repeated.
func Test_kiss_process_msg_data_frame(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), from)

	assert.Equal(t, 1, tnc.count(0, false))
	assert.Equal(t, 0, tnc.count(0, true))
}

// A frame that has already been through a digipeater is somebody waiting on
// air for it, so it jumps the queue.
func Test_kiss_process_msg_repeated_frame_is_high_priority(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST,Q3TEST*:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), from)

	assert.Equal(t, 1, tnc.count(0, true))
}

// The channel is in the top half of the first byte, and a client asking for a
// channel we do not have is the AX.25-for-Linux CRC mode problem, which gets
// an explanation rather than a transmission.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_invalid_channel(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output = captureKissLog(t, func() {
		h.processMsg(append([]byte{0x80 | kiss.CmdDataFrame}, pp.FrameData()...), from)
	})

	assert.Contains(t, output, "Invalid transmit channel from KISS client application")
	assert.Contains(t, output, "channel=8")
	assert.Contains(t, output, "kissparms -c 1 -p radio")
	assert.Equal(t, 0, tnc.countOn(8))
}

// A frame whose only content is an escaped FEND reads as a request for channel
// 12, and the hex dump that goes with the explanation had nothing left to show
// once it skipped what it took for a leading FEND - it indexed past the end.
// Any client of the KISS ports could send it.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_invalid_channel_escaped_fend_only(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()
	var kc kiss.Collector

	var output = captureKissLog(t, func() {
		for _, b := range []byte{kiss.FEND, kiss.FESC, kiss.TFEND, kiss.FEND} {
			h.RecByte(&kc, b, 0, from)
		}
	})

	assert.Contains(t, output, "Invalid transmit channel from KISS client application")
	assert.Contains(t, output, "channel=12")
}

// The debug print of a message has something to say even when there is nothing
// of the message to print.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func Test_kiss_debug_print_empty(t *testing.T) {
	for _, msg := range [][]byte{nil, {kiss.FEND}} {
		var hook = test.NewGlobal()
		t.Cleanup(hook.Reset)

		kiss_debug_print(FROM_CLIENT, "", msg)

		require.NotNil(t, hook.LastEntry(), "message %v", msg)
		assert.Equal(t, "Empty message for KISS client application", hook.LastEntry().Message)
	}
}

// singleChannelClient is a client whose messages are all for one radio
// channel, whatever their frames say - as on a KISS TCP port that carries one.
type singleChannelClient struct {
	recordingClient

	channel int
}

func (c singleChannelClient) RadioChannel(int) int {
	return c.channel
}

// The radio channel a frame is for is the one its client says, which need
// not be the one in the frame: an application that thinks it has a one-radio
// TNC always says 0.
func Test_kiss_process_msg_client_channel_overrides_the_frame(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), singleChannelClient{recordingClient: from, channel: 1})

	assert.Equal(t, 1, tnc.count(1, false), "the client's channel should have been used")
	assert.Equal(t, 0, tnc.count(0, false))
}

// Bytes that are not an AX.25 frame cannot be transmitted, and are reported
// rather than passed on.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_undecodable_data_frame(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var output = captureKissLog(t, func() {
		h.processMsg([]byte{kiss.CmdDataFrame, 'n', 'o'}, from)
	})

	assert.Contains(t, output, "Invalid KISS data frame from client app")
}

// The timing parameters are the rest of what a client sets up, and each is one
// byte after the command.
func Test_kiss_process_msg_timing_parameters(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	h.processMsg([]byte{kiss.CmdTxDelay, 30}, from)
	h.processMsg([]byte{kiss.CmdPersistence, 63}, from)
	h.processMsg([]byte{kiss.CmdSlotTime, 10}, from)
	h.processMsg([]byte{kiss.CmdTxTail, 10}, from)
	h.processMsg([]byte{kiss.CmdFullDuplex, 1}, from)

	assert.Equal(t, tncTiming{txdelay: 30, persist: 63, slottime: 10, txtail: 10, fulldup: true}, *tnc.timingFor(0))
}

// A value nobody would want on purpose is applied, because the client asked,
// but pointed at the part of the guide that explains what it means.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_extreme_timing_parameters(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

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
			var output = captureKissLog(t, func() { h.processMsg(c.msg, from) })

			assert.Contains(t, output, "Radio Channel - Transmit Timing")
		})
	}

	// Applied all the same.
	assert.Equal(t, 200, tnc.timingFor(0).txdelay)
}

// A parameter command with no parameter is a protocol error, and leaves the
// setting alone rather than applying whatever happened to be next.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_missing_parameter(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

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
			var output = captureKissLog(t, func() { h.processMsg([]byte{c.cmd}, from) })

			assert.Contains(t, output, "Missing value for KISS command")
		})
	}

	assert.Empty(t, tnc.timing)
}

// Leaving KISS mode is for a TNC that has another mode to go back to.  We
// don't, so it is noted and ignored.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_end_kiss(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var output = captureKissLog(t, func() {
		h.processMsg([]byte{kiss.CmdEndKiss}, from)
	})

	assert.Contains(t, output, "end KISS mode ignored")
}

// A command we don't implement gets the troubleshooting tip, and XKISS
// polling additionally says what the application should be configured as
// instead.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_unsupported_commands(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var output = captureKissLog(t, func() {
		h.processMsg([]byte{7}, from)
	})

	assert.Contains(t, output, "Invalid KISS command")
	assert.Contains(t, output, "command=7")
	assert.Contains(t, output, "'-d kn' option")
	assert.NotContains(t, output, "XKISS")

	output = captureKissLog(t, func() {
		h.processMsg([]byte{kiss.CmdXKissPoll}, from)
	})

	assert.Contains(t, output, "XKISS protocol, which is not supported")
	assert.Contains(t, output, "with or without ACKMODE")
}

// replyingClient is a kissClient that hands each reply over replies, from
// whichever goroutine sends it.
type replyingClient struct {
	replies chan sentToClient
}

func (c replyingClient) Reply(channel int, cmd int, frame []byte) {
	c.replies <- sentToClient{channel: channel, cmd: cmd, body: frame, prompt: false}
}

func (c replyingClient) Prompt(_ []byte) {}

func (c replyingClient) RadioChannel(frameChannel int) int {
	return frameChannel
}

// ackmodeFrame is an ACKMODE data frame for channel, with frame ID id, carrying
// pp.
func ackmodeFrame(channel int, id []byte, pp *ax25.Packet) []byte {
	var msg = []byte{byte((channel<<4)&0xf0) | kiss.CmdXKissData}
	msg = append(msg, id...)

	return append(msg, pp.FrameData()...)
}

// An ACKMODE data frame is queued like any other, and its ID goes back to the
// client that sent it - once the frame has been transmitted, not when it is
// queued.
func Test_kiss_process_msg_ackmode(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var from = replyingClient{replies: make(chan sentToClient, 1)}

	var pp = newKissTestPacket(t)

	h.processMsg(ackmodeFrame(1, []byte{0x12, 0x34}, pp), from)

	assert.Empty(t, from.replies, "nothing has been transmitted yet")

	var queued = tnc.take(1, false)
	require.NotNil(t, queued)
	assert.Equal(t, pp.FrameData(), queued.FrameData(), "the ID should not be part of the frame")

	queued.Transmitted()

	select {
	case reply := <-from.replies:
		assert.Equal(t, sentToClient{channel: 1, cmd: kiss.CmdXKissData, body: []byte{0x12, 0x34}, prompt: false}, reply)
	case <-time.After(5 * time.Second):
		require.Fail(t, "no acknowledgement")
	}
}

// The transmit thread is what says a frame has gone, so a client that has
// stopped reading mustn't hold it up: nothing else would go out meanwhile.
func Test_kiss_process_msg_ackmode_does_not_wait_for_the_client(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var from = replyingClient{replies: make(chan sentToClient)} // Nobody reads it until the end.

	h.processMsg(ackmodeFrame(0, []byte{0x12, 0x34}, newKissTestPacket(t)), from)

	var queued = tnc.take(0, false)
	require.NotNil(t, queued)

	var done = make(chan struct{})

	go func() {
		queued.Transmitted()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail(t, "Transmitted waited for a client that isn't reading")
	}

	<-from.replies // Let the acknowledgement go.
}

// An ordinary data frame asks for no acknowledgement, so it gets none.
func Test_kiss_process_msg_data_frame_not_acknowledged(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var sent, from = recordingKissClient()

	h.processMsg(append([]byte{kiss.CmdDataFrame}, newKissTestPacket(t).FrameData()...), from)

	var queued = tnc.take(0, false)
	require.NotNil(t, queued)

	queued.Transmitted()

	assert.Empty(t, *sent)
}

// An ACKMODE frame too short to hold its ID has nothing to send and nothing
// to acknowledge.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_ackmode_without_id(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var sent, from = recordingKissClient()

	var output = captureKissLog(t, func() {
		h.processMsg([]byte{kiss.CmdXKissData, 0x12}, from)
	})

	assert.Contains(t, output, "KISS ACKMODE data frame is missing its frame ID.")
	assert.Equal(t, 0, tnc.count(0, false))
	assert.Empty(t, *sent)
}

// With KISSCOPY, an ACKMODE frame is copied to the other clients as an
// ordinary data frame: they didn't ask for an acknowledgement, and the ID is
// not part of the AX.25 frame.
func Test_kiss_process_msg_ackmode_copies_as_data_frame(t *testing.T) {
	t.Parallel()

	var h, _ = setupKissProcessMsg(t)

	var peers = new(recordingCopier)

	h.copyToPeers = peers.Copy

	var pp = newKissTestPacket(t)
	var _, from = recordingKissClient()

	h.processMsg(ackmodeFrame(0, []byte{0x12, 0x34}, pp), from)

	require.Len(t, peers.copied, 1)
	assert.Equal(t, append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), peers.copied[0].msg)
	assert.Equal(t, kiss.CmdDataFrame, peers.copied[0].cmd)
}

// "Set hardware" is the one command with an answer: the human readable
// question-and-answer form fldigi established, which several applications
// already speak.
func Test_kiss_set_hardware_tnc_version(t *testing.T) {
	t.Parallel()

	var h, _ = setupKissProcessMsg(t)

	var sent, from = recordingKissClient()

	h.processMsg(append([]byte{kiss.CmdSetHardware}, []byte("TNC:")...), from)

	require.Len(t, *sent, 1)
	assert.Equal(t, kiss.CmdSetHardware, (*sent)[0].cmd)
	assert.Contains(t, string((*sent)[0].body), "DIREWOLF ")
}

// TXBUF is the one an application actually wanted: how much is still waiting
// to go out, so that it can throttle a long transmission.
func Test_kiss_set_hardware_txbuf(t *testing.T) {
	t.Parallel()

	var tnc = newRecordingTNC()

	var sent, from = recordingKissClient()

	kiss_set_hardware(0, []byte("TXBUF:"), from, tnc)

	require.Len(t, *sent, 1)
	assert.Equal(t, "TXBUF:0", string((*sent)[0].body))

	tnc.queued = 42

	kiss_set_hardware(0, []byte("TXBUF:"), from, tnc)

	require.Len(t, *sent, 2)
	assert.Equal(t, "TXBUF:42", string((*sent)[1].body))
}

// A query with a parameter, a command we don't know, and one that isn't in the
// COMMAND:parameter form at all all get complained about rather than guessed
// at.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_set_hardware_malformed(t *testing.T) {
	var sent, from = recordingKissClient()

	for _, c := range []struct {
		command string
		expect  string
	}{
		{"TNC:9", "did not expect a parameter"},
		{"TXBUF:9", "did not expect a parameter"},
		{"NOSUCH:", "command=NOSUCH"},
		{"TNC", "expected the form COMMAND:"},
	} {
		t.Run(c.command, func(t *testing.T) {
			var output = captureKissLog(t, func() {
				kiss_set_hardware(0, []byte(c.command), from, newRecordingTNC())
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
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	feedKissBytes(h, kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Equal(t, 1, tnc.count(0, false))
}

// An application that thinks it is talking to an old command-mode TNC sends
// lines of text.  Each one ending in a carriage return is answered with a
// command prompt, which is what stops it trying.
func Test_KissRecByte_command_prompt(t *testing.T) {
	t.Parallel()

	var h, _ = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var sent = feedKissBytes(h, kf, 0, []byte("XFLOW OFF\r"))

	require.Len(t, *sent, 1)
	assert.Equal(t, "\r\ncmd:", string((*sent)[0].body))
	assert.True(t, (*sent)[0].prompt, "a text reply is not a frame")
}

// "RESTART" and "RESET" get an empty KISS frame instead, which is what the
// applications sending them are waiting for.
func Test_KissRecByte_restart(t *testing.T) {
	t.Parallel()

	var h, _ = setupKissProcessMsg(t)

	for _, word := range []string{"RESTART\r", "reset\r"} {
		t.Run(word, func(t *testing.T) {
			t.Parallel()

			var kf = new(kiss.Collector)

			var sent = feedKissBytes(h, kf, 0, []byte(word))

			require.Len(t, *sent, 1)
			assert.Equal(t, "\xc0\xc0", string((*sent)[0].body))
		})
	}
}

// With the debug option on, the noise before a frame is shown, so that a
// client that is not being understood can be looked at.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_KissRecByte_noise_is_printed(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = captureKissLog(t, func() {
		feedKissBytes(h, kf, 1, append([]byte("junk"), kiss.FEND))
	})

	assert.Contains(t, output, "Rejected Noise")
}

// FENDs with nothing between them are how some clients idle, and are not
// frames.
func Test_KissRecByte_empty_frames(t *testing.T) {
	t.Parallel()

	var h, tnc = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	feedKissBytes(h, kf, 0, []byte{kiss.FEND, kiss.FEND, kiss.FEND, kiss.FEND})

	assert.Equal(t, 0, tnc.countOn(0))
}

// A client that never sends a closing FEND is told about once, not once for
// every byte past the limit.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_KissRecByte_overlong_frame(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = captureKissLog(t, func() {
		feedKissBytes(h, kf, 0, append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...))
	})

	assert.Equal(t, 1, strings.Count(output, "KISS message exceeded maximum length"))
}

// The client does eventually send its closing FEND, and the byte it used to be
// written to was one past the end of the buffer - which took the whole program
// down, from anything that could talk to the KISS port.  The overlong frame is
// thrown away, and the collector is left ready for the next one.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_KissRecByte_overlong_frame_closing_fend(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var overlong = append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...)

	var output = captureKissLog(t, func() {
		feedKissBytes(h, kf, 0, append(overlong, kiss.FEND))
	})

	assert.Contains(t, output, "KISS message exceeded maximum length, discarded")
	assert.Equal(t, 0, tnc.countOn(0), "a fragment of the overlong frame was acted on")

	// And a well formed frame after it still gets through.
	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	feedKissBytes(h, kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Equal(t, 1, tnc.countOn(0))
}

// With "-d kn" the frame is shown as it arrived and again as it was decoded,
// which is the pair a protocol problem shows up in.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_KissRecByte_debug_prints_both_forms(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	var output = captureKissLog(t, func() {
		feedKissBytes(h, kf, 2, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))
	})

	assert.Contains(t, output, `channel=0 direction=from function="Data frame"`)
	assert.Contains(t, output, "Packet content after removing KISS framing")
}

// FEND FESC FEND is a frame, but escapes nothing, so there is no type byte
// once it is unescaped.  Three bytes from any KISS client used to take the
// whole program down, looking for one.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_KissRecByte_frame_empty_once_unescaped(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = captureKissLog(t, func() {
		feedKissBytes(h, kf, 2, []byte{kiss.FEND, kiss.FESC, kiss.FEND})
	})

	assert.Contains(t, output, "empty once unescaped")
	assert.Equal(t, 0, tnc.countOn(0))
}

// What goes to a client is the type byte - channel in the top nybble, command
// in the bottom - then the frame, with KISS framing and escapes added.
func Test_kissClientFrame(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			assert.Equal(t, c.want, kissClientFrame(c.channel, c.cmd, c.frame, 0, "Test"))
		})
	}
}

// A frame longer than AX.25 allows is cut to length before it is framed, so
// the client is sent what it was told it would get, and the user hears why.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kissClientFrame_truncates(t *testing.T) {
	var frame = bytes.Repeat([]byte{'x'}, ax25.MaxPacketLen+10)

	var got []byte

	var output = captureKissLog(t, func() {
		got = kissClientFrame(0, kiss.CmdDataFrame, frame, 0, "Test")
	})

	assert.Contains(t, output, "KISS buffer too small, frame truncated")
	assert.Contains(t, output, "transport=Test")

	// FEND, the type indicator, the frame, FEND - and 'x' needs no escaping.
	assert.Len(t, got, ax25.MaxPacketLen+3)
}

// An ACKMODE frame for a channel that can't transmit is shown as the client
// sent it, ID and all, not as the data frame it carries.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_process_msg_ackmode_invalid_channel_shows_what_was_sent(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var msg = ackmodeFrame(8, []byte{0x12, 0x34}, newKissTestPacket(t))

	var output = captureKissLog(t, func() {
		h.processMsg(msg, from)
	})

	assert.Contains(t, output, "Invalid transmit channel from KISS client application")
	assert.Contains(t, output, "function=ACKMODE")
	assert.Contains(t, output, fmt.Sprintf("length=%d", len(msg)))
}

// One client's acknowledgements arrive in the order its frames went, however
// many are ready at once.
func Test_kiss_acks_in_order(t *testing.T) {
	t.Parallel()

	var acks = newKissAcks(t.Context())

	var from = replyingClient{replies: make(chan sentToClient, 3)}

	for id := range byte(3) {
		acks.send(from, 0, []byte{0, id})
	}

	for id := range byte(3) {
		select {
		case reply := <-from.replies:
			assert.Equal(t, []byte{0, id}, reply.body)
		case <-time.After(5 * time.Second):
			require.Fail(t, "no acknowledgement", "id %d", id)
		}
	}
}

// A client that has stopped reading gets only so many acknowledgements
// waiting for it; the rest are dropped rather than piling up.  That is said
// once, however many go, and how many went is said once it catches up.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func Test_kiss_acks_dropped_for_a_client_not_reading(t *testing.T) {
	const tooMany = 3

	var acks = newKissAcks(t.Context())

	var from = replyingClient{replies: make(chan sentToClient)} // Nobody reads it, for now.

	var queued = func() int {
		acks.mu.Lock()
		defer acks.mu.Unlock()

		var queue, running = acks.pending[from]
		if !running {
			return -1
		}

		return len(queue.acks)
	}

	var output = captureKissLog(t, func() {
		// The first is taken by the goroutine, which then waits on the
		// client; the queue behind it fills, and then there are too many.
		acks.send(from, 0, []byte{0, 0})

		require.Eventually(t, func() bool { return queued() == 0 },
			5*time.Second, time.Millisecond, "the first should have been taken to send")

		for range maxPendingKissAcks + tooMany {
			acks.send(from, 0, []byte{0, 1})
		}

		assert.Equal(t, maxPendingKissAcks, queued())

		for range maxPendingKissAcks + 1 { // The client catches up.
			<-from.replies
		}

		require.Eventually(t, func() bool { return queued() == -1 },
			5*time.Second, time.Millisecond, "the goroutine should finish once there is nothing left")
	})

	assert.Equal(t, 1, strings.Count(output, "isn't reading its ACKMODE acknowledgements"))
	assert.Contains(t, output, "has caught up with its ACKMODE acknowledgements")
	assert.Contains(t, output, fmt.Sprintf("dropped=%d", tooMany))
}
