// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"bytes"
	"testing"

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

// recordingTNC is a kissTNC that keeps what it is asked to do, so a test can
// see what the handler made of a client's messages.  Channels 0 and 1 are the
// ones it has, and queued is what it says is waiting to go out.
type recordingTNC struct {
	transmitted []tncTransmission
	timing      map[int]*tncTiming
	queued      int
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

// setupKissProcessMsg hands back a KissHandler for the KISS tests to drive,
// and the recording TNC it hands what it is asked to do.
func setupKissProcessMsg(t *testing.T) (*KissHandler, *recordingTNC) {
	t.Helper()

	var tnc = new(recordingTNC)
	tnc.timing = make(map[int]*tncTiming)

	return NewKissHandler(tnc, nil), tnc
}

// copiedFrame is a data frame the handler had copied to the other clients.
type copiedFrame struct {
	msg     []byte
	channel int
	cmd     int
	from    kissClient
}

// recordingCopier is a kissCopier that keeps what it is asked to copy.
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
	var h, tnc = setupKissProcessMsg(t)

	var peers = new(recordingCopier)

	h.peers = peers

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var _, from = recordingKissClient()

	var msg = append([]byte{0x10 | kiss.CmdDataFrame}, pp.FrameData()...)

	h.processMsg(msg, from)

	assert.Equal(t, []copiedFrame{{msg: msg, channel: 1, cmd: kiss.CmdDataFrame, from: from}}, peers.copied)
	assert.Len(t, tnc.transmitted, 1)
}

// Nor is anything but a data frame copied.
func Test_kiss_process_msg_copies_only_data_frames(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var peers = new(recordingCopier)

	h.peers = peers

	var _, from = recordingKissClient()

	h.processMsg([]byte{kiss.CmdTxDelay, 30}, from)

	assert.Empty(t, peers.copied)
}

// A data frame from the client is a frame to transmit, and an original - one
// with no used digipeater in it - goes on the low priority queue behind
// anything already being repeated.
func Test_kiss_process_msg_data_frame(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), from)

	require.Len(t, tnc.transmitted, 1)
	assert.Equal(t, 0, tnc.transmitted[0].channel)
	assert.False(t, tnc.transmitted[0].urgent)
	assert.Equal(t, pp.FrameData(), tnc.transmitted[0].pp.FrameData())
}

// A frame that has already been through a digipeater is somebody waiting on
// air for it, so it jumps the queue.
func Test_kiss_process_msg_repeated_frame_is_high_priority(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST,Q3TEST*:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), from)

	require.Len(t, tnc.transmitted, 1)
	assert.True(t, tnc.transmitted[0].urgent)
}

// The channel is in the top half of the first byte, and a client asking for a
// channel we do not have is the AX.25-for-Linux CRC mode problem, which gets
// an explanation rather than a transmission.
func Test_kiss_process_msg_invalid_channel(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

	h.processMsg(append([]byte{0x80 | kiss.CmdDataFrame}, pp.FrameData()...), from)

	var entry = testutils.LoggedEntry(t, hook, "Invalid transmit channel from KISS client application")
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
	assert.Equal(t, 8, entry.Data["channel"])
	assert.Contains(t, entry.Data["hint"], "kissparms -c 1 -p radio")
	assert.Empty(t, tnc.transmitted)
}

// A frame whose only content is an escaped FEND reads as a request for channel
// 12, and the hex dump that goes with the explanation had nothing left to show
// once it skipped what it took for a leading FEND - it indexed past the end.
// Any client of the KISS ports could send it.
func Test_kiss_process_msg_invalid_channel_escaped_fend_only(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()
	var kc kiss.Collector

	var hook = testutils.CaptureLogrus(t, logrus.DebugLevel)

	for _, b := range []byte{kiss.FEND, kiss.FESC, kiss.TFEND, kiss.FEND} {
		h.RecByte(&kc, b, 0, from)
	}

	assert.Equal(t, 12, testutils.LoggedEntry(t, hook, "Invalid transmit channel from KISS client application").Data["channel"])
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
	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	h.processMsg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), singleChannelClient{recordingClient: from, channel: 1})

	require.Len(t, tnc.transmitted, 1)
	assert.Equal(t, 1, tnc.transmitted[0].channel, "the client's channel should have been used")
}

// Bytes that are not an AX.25 frame cannot be transmitted, and are reported
// rather than passed on.
func Test_kiss_process_msg_undecodable_data_frame(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

	h.processMsg([]byte{kiss.CmdDataFrame, 'n', 'o'}, from)

	assert.Equal(t, logrus.ErrorLevel, testutils.LoggedEntry(t, hook, "Invalid KISS data frame from client application").Level)
}

// The timing parameters are the rest of what a client sets up, and each is one
// byte after the command.
func Test_kiss_process_msg_timing_parameters(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var hook = testutils.CaptureLogrus(t, logrus.DebugLevel)

	h.processMsg([]byte{kiss.CmdTxDelay, 30}, from)
	h.processMsg([]byte{kiss.CmdPersistence, 63}, from)
	h.processMsg([]byte{kiss.CmdSlotTime, 10}, from)
	h.processMsg([]byte{kiss.CmdTxTail, 10}, from)
	h.processMsg([]byte{kiss.CmdFullDuplex, 1}, from)

	assert.Equal(t, tncTiming{txdelay: 30, persist: 63, slottime: 10, txtail: 10, fulldup: true}, *tnc.timingFor(0))

	// Each is noted, for anyone wondering where a setting came from.
	var txdelay = testutils.LoggedEntry(t, hook, "KISS protocol set TXDELAY")
	assert.Equal(t, logrus.DebugLevel, txdelay.Level)
	assert.Equal(t, byte(30), txdelay.Data["txdelay"])
	assert.Equal(t, 300, txdelay.Data["ms"])
	assert.Equal(t, true, testutils.LoggedEntry(t, hook, "KISS protocol set FullDuplex").Data["fullduplex"])
}

// A value nobody would want on purpose is applied, because the client asked,
// but pointed at the part of the guide that explains what it means.
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
			var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

			h.processMsg(c.msg, from)

			require.NotNil(t, hook.LastEntry())
			assert.Equal(t, logrus.WarnLevel, hook.LastEntry().Level)
			assert.Contains(t, hook.LastEntry().Message, "Radio Channel - Transmit Timing")
		})
	}

	// Applied all the same.
	assert.Equal(t, 200, tnc.timingFor(0).txdelay)
}

// A parameter command with no parameter is a protocol error, and leaves the
// setting alone rather than applying whatever happened to be next.
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
			var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

			h.processMsg([]byte{c.cmd}, from)

			var entry = testutils.LoggedEntry(t, hook, "Missing value for KISS command")
			assert.Equal(t, logrus.ErrorLevel, entry.Level)
			assert.Equal(t, c.name, entry.Data["command"])
		})
	}

	assert.Empty(t, tnc.timing)
}

// Leaving KISS mode is for a TNC that has another mode to go back to.  We
// don't, so it is noted and ignored.
func Test_kiss_process_msg_end_kiss(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	var hook = testutils.CaptureLogrus(t, logrus.DebugLevel)

	h.processMsg([]byte{kiss.CmdEndKiss}, from)

	testutils.LoggedEntry(t, hook, "KISS protocol end KISS mode, ignoring it")
}

// A command we don't implement gets the troubleshooting tip, and the two
// XKISS ones additionally say what the application should be configured as
// instead.
func Test_kiss_process_msg_unsupported_commands(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var _, from = recordingKissClient()

	// The hint for the command processMsg is handed.
	var hint = func(cmd byte) string {
		var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

		h.processMsg([]byte{cmd}, from)

		var entry = testutils.LoggedEntry(t, hook, "Invalid KISS command from client application")
		assert.Equal(t, logrus.ErrorLevel, entry.Level)
		assert.Equal(t, cmd, entry.Data["command"])

		var text, ok = entry.Data["hint"].(string)
		require.True(t, ok)

		return text
	}

	assert.Contains(t, hint(7), `Use the "-d kn" option`)
	assert.NotContains(t, hint(7), "XKISS")

	assert.Contains(t, hint(kiss.CmdXKissData), `"XKISS" protocol which is not supported`)
	assert.Contains(t, hint(kiss.CmdXKissData), "Winlink Express")
	assert.Contains(t, hint(kiss.CmdXKissData), `Use the "-d kn" option`)

	assert.Contains(t, hint(kiss.CmdXKissPoll), `"XKISS" protocol which is not supported`)
}

// "Set hardware" is the one command with an answer: the human readable
// question-and-answer form fldigi established, which several applications
// already speak.
func Test_kiss_set_hardware_tnc_version(t *testing.T) {
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
	var _, tnc = setupKissProcessMsg(t)

	var sent, from = recordingKissClient()

	kiss_set_hardware(tnc, 0, []byte("TXBUF:"), from)

	require.Len(t, *sent, 1)
	assert.Equal(t, "TXBUF:0", string((*sent)[0].body))

	tnc.queued = 42

	kiss_set_hardware(tnc, 0, []byte("TXBUF:"), from)

	require.Len(t, *sent, 2)
	assert.Equal(t, "TXBUF:42", string((*sent)[1].body))
}

// A query with a parameter, a command we don't know, and one that isn't in the
// COMMAND:parameter form at all all get complained about rather than guessed
// at.
func Test_kiss_set_hardware_malformed(t *testing.T) {
	var _, tnc = setupKissProcessMsg(t)

	var sent, from = recordingKissClient()

	for _, c := range []struct {
		command string
		level   logrus.Level
		expect  string
	}{
		{"TNC:9", logrus.WarnLevel, "KISS Set Hardware query did not expect a parameter"},
		{"TXBUF:9", logrus.WarnLevel, "KISS Set Hardware query did not expect a parameter"},
		{"NOSUCH:", logrus.ErrorLevel, "KISS Set Hardware command not recognised"},
		{"TNC", logrus.ErrorLevel, "KISS Set Hardware expected the form COMMAND:[parameter[,parameter...]]"},
	} {
		t.Run(c.command, func(t *testing.T) {
			var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

			kiss_set_hardware(tnc, 0, []byte(c.command), from)

			assert.Equal(t, c.level, testutils.LoggedEntry(t, hook, c.expect).Level)
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
	var h, tnc = setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	feedKissBytes(h, kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	require.Len(t, tnc.transmitted, 1)
	assert.Equal(t, pp.FrameData(), tnc.transmitted[0].pp.FrameData())
}

// An application that thinks it is talking to an old command-mode TNC sends
// lines of text.  Each one ending in a carriage return is answered with a
// command prompt, which is what stops it trying.
func Test_KissRecByte_command_prompt(t *testing.T) {
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
	var h, _ = setupKissProcessMsg(t)

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
	var h, _ = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var hook = testutils.CaptureLogrus(t, logrus.DebugLevel)

	feedKissBytes(h, kf, 1, append([]byte("junk"), kiss.FEND))

	testutils.LoggedEntry(t, hook, "<<< Rejected Noise from KISS client application")
}

// FENDs with nothing between them are how some clients idle, and are not
// frames.
func Test_KissRecByte_empty_frames(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	feedKissBytes(h, kf, 0, []byte{kiss.FEND, kiss.FEND, kiss.FEND, kiss.FEND})

	assert.Empty(t, tnc.transmitted)
}

// A client that never sends a closing FEND is told about once, not once for
// every byte past the limit.
func Test_KissRecByte_overlong_frame(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

	feedKissBytes(h, kf, 0, append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...))

	assert.Equal(t, logrus.ErrorLevel,
		testutils.LoggedEntry(t, hook, "KISS message from client application exceeded maximum length, discarding it").Level)
}

// The client does eventually send its closing FEND, and the byte it used to be
// written to was one past the end of the buffer - which took the whole program
// down, from anything that could talk to the KISS port.  The overlong frame is
// thrown away, and the collector is left ready for the next one.
func Test_KissRecByte_overlong_frame_closing_fend(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var overlong = append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...)

	var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

	feedKissBytes(h, kf, 0, append(overlong, kiss.FEND))

	testutils.LoggedEntry(t, hook, "KISS message from client application exceeded maximum length, discarding it")
	assert.Empty(t, tnc.transmitted, "a fragment of the overlong frame was acted on")

	// And a well formed frame after it still gets through.
	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	feedKissBytes(h, kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Len(t, tnc.transmitted, 1)
}

// With "-d kn" the frame is shown as it arrived and again as it was decoded,
// which is the pair a protocol problem shows up in.
func Test_KissRecByte_debug_prints_both_forms(t *testing.T) {
	var h, _ = setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	var hook = testutils.CaptureLogrus(t, logrus.DebugLevel)

	feedKissBytes(h, kf, 2, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Equal(t, 0, testutils.LoggedEntry(t, hook, "<<< Data frame from KISS client application").Data["channel"])
	assert.Equal(t, len(pp.FrameData()),
		testutils.LoggedEntry(t, hook, "Packet content after removing KISS framing and any escapes").Data["length"])
}

// FEND FESC FEND is a frame, but escapes nothing, so there is no type byte
// once it is unescaped.  Three bytes from any KISS client used to take the
// whole program down, looking for one.
func Test_KissRecByte_frame_empty_once_unescaped(t *testing.T) {
	var h, tnc = setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

	feedKissBytes(h, kf, 2, []byte{kiss.FEND, kiss.FESC, kiss.FEND})

	testutils.LoggedEntry(t, hook, "KISS frame from client application has nothing in it once unescaped, ignoring it")
	assert.Empty(t, tnc.transmitted)
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

	var hook = testutils.CaptureLogrus(t, logrus.InfoLevel)

	var got = kissClientFrame(0, kiss.CmdDataFrame, frame, 0, "Test")

	var entry = testutils.LoggedEntry(t, hook, "KISS frame for client application too long, truncating it")
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
	assert.Equal(t, "Test", entry.Data["transport"])

	// FEND, the type indicator, the frame, FEND - and 'x' needs no escaping.
	assert.Len(t, got, ax25.MaxPacketLen+3)
}
