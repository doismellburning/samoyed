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
// out.  kiss_process_msg is where a frame with its escapes already removed is
// acted on, so it can be driven directly, with a recording stand-in for the
// function that answers the client.

// sentToClient is one message the TNC sent back to the client application.
type sentToClient struct {
	channel int
	cmd     int
	body    []byte
	length  int
}

// recordingSendfun hands back a kiss_sendfun that appends to the slice it
// returns, so a test can see what the TNC would have answered.
func recordingSendfun() (*[]sentToClient, kiss_sendfun) {
	var sent = new([]sentToClient)

	return sent, func(channel int, cmd int, body []byte, length int, _ *kissport_status_s, _ int) {
		*sent = append(*sent, sentToClient{channel: channel, cmd: cmd, body: body, length: length})
	}
}

// kissTestRadioConfig is the channel table the KISS tests hand to
// kiss_process_msg, or to the transport that calls it, to check a transmit
// request against: channels 0 and 1 are radios, and nothing else is set up.
func kissTestRadioConfig() *RadioConfig {
	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[1] = MEDIUM_RADIO

	return audioConfig
}

// setupKissProcessMsg gives kiss_process_msg the things it reaches for besides
// the channel table, which each call is handed: the transmit settings it
// applies parameters to, and the service that copies frames between clients.
func setupKissProcessMsg(t *testing.T) *XmitService {
	t.Helper()

	var origXmit, origKissNet = xmitSvc, kissNetSvc

	t.Cleanup(func() {
		xmitSvc, kissNetSvc = origXmit, origKissNet

		for c := range MAX_RADIO_CHANS {
			for p := range TQ_NUM_PRIO {
				for transmitQueue.Remove(c, p) != nil { //revive:disable-line:empty-block
				}
			}
		}
	})

	var audioConfig = kissTestRadioConfig()

	xmitSvc = new(XmitService)
	kissNetSvc = NewKissNetService(new(misc_config_s), audioConfig, 0)

	transmitQueue.Init(audioConfig)

	return xmitSvc
}

// A data frame from the client is a frame to transmit, and an original - one
// with no used digipeater in it - goes on the low priority queue behind
// anything already being repeated.
func Test_kiss_process_msg_data_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	kiss_process_msg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), kissTestRadioConfig(), 0, nil, -1, sendfun)

	assert.Equal(t, 1, transmitQueue.Count(0, TQ_PRIO_1_LO, "", "", false))
	assert.Equal(t, 0, transmitQueue.Count(0, TQ_PRIO_0_HI, "", "", false))
}

// A frame that has already been through a digipeater is somebody waiting on
// air for it, so it jumps the queue.
func Test_kiss_process_msg_repeated_frame_is_high_priority(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var pp = ax25.FromText("Q1TEST>Q2TEST,Q3TEST*:hello", true)
	require.NotNil(t, pp)

	kiss_process_msg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), kissTestRadioConfig(), 0, nil, -1, sendfun)

	assert.Equal(t, 1, transmitQueue.Count(0, TQ_PRIO_0_HI, "", "", false))
}

// The channel is in the top half of the first byte, and a client asking for a
// channel we do not have is the AX.25-for-Linux CRC mode problem, which gets
// an explanation rather than a transmission.
func Test_kiss_process_msg_invalid_channel(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output = testutils.CaptureOutput(t, func() {
		kiss_process_msg(append([]byte{0x80 | kiss.CmdDataFrame}, pp.FrameData()...), kissTestRadioConfig(), 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "Invalid transmit channel 8 from KISS client app")
	assert.Contains(t, output, "kissparms -c 1 -p radio")
	assert.Equal(t, 0, transmitQueue.Count(8, -1, "", "", false))
}

// A frame whose only content is an escaped FEND reads as a request for channel
// 12, and the hex dump that goes with the explanation had nothing left to show
// once it skipped what it took for a leading FEND - it indexed past the end.
// Any client of the KISS ports could send it.
func Test_kiss_process_msg_invalid_channel_escaped_fend_only(t *testing.T) {
	setupKissProcessMsg(t)

	var audioConfig = kissTestRadioConfig()
	var _, sendfun = recordingSendfun()
	var kc kiss.Collector

	var output = testutils.CaptureOutput(t, func() {
		for _, b := range []byte{kiss.FEND, kiss.FESC, kiss.TFEND, kiss.FEND} {
			KissRecByte(&kc, audioConfig, b, 0, nil, -1, sendfun)
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
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var kps = new(kissport_status_s)
	kps.channel = 1

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	kiss_process_msg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), kissTestRadioConfig(), 0, kps, 0, sendfun)

	assert.Equal(t, 1, transmitQueue.Count(1, TQ_PRIO_1_LO, "", "", false), "the port's channel should have been used")
	assert.Equal(t, 0, transmitQueue.Count(0, TQ_PRIO_1_LO, "", "", false))
}

// A KISS TCP port's channel comes from the configuration rather than a
// nibble, so nothing bounds it to the channel table.  The validity check used
// to go on to ask whether an out-of-range channel was the IGate's - indexing
// the table with the very channel it had just found to be out of range.
func Test_kiss_process_msg_port_channel_out_of_range(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var kps = new(kissport_status_s)
	kps.channel = MAX_TOTAL_CHANS

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output string

	assert.NotPanics(t, func() {
		output = testutils.CaptureOutput(t, func() {
			kiss_process_msg(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...), kissTestRadioConfig(), 0, kps, 0, sendfun)
		})
	})

	assert.Contains(t, output, "Invalid transmit channel 16 from KISS client app")
}

// Bytes that are not an AX.25 frame cannot be transmitted, and are reported
// rather than passed on.
func Test_kiss_process_msg_undecodable_data_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var output = testutils.CaptureOutput(t, func() {
		kiss_process_msg([]byte{kiss.CmdDataFrame, 'n', 'o'}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "Invalid KISS data frame from client app")
}

// The timing parameters are the rest of what a client sets up, and each is one
// byte after the command.
func Test_kiss_process_msg_timing_parameters(t *testing.T) {
	var xs = setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	kiss_process_msg([]byte{kiss.CmdTxDelay, 30}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	kiss_process_msg([]byte{kiss.CmdPersistence, 63}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	kiss_process_msg([]byte{kiss.CmdSlotTime, 10}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	kiss_process_msg([]byte{kiss.CmdTxTail, 10}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	kiss_process_msg([]byte{kiss.CmdFullDuplex, 1}, kissTestRadioConfig(), 0, nil, -1, sendfun)

	assert.Equal(t, 30, xs.timing[0].txdelay)
	assert.Equal(t, 63, xs.timing[0].persist)
	assert.Equal(t, 10, xs.timing[0].slottime)
	assert.Equal(t, 10, xs.timing[0].txtail)
	assert.True(t, xs.timing[0].fulldup)
}

// A value nobody would want on purpose is applied, because the client asked,
// but pointed at the part of the guide that explains what it means.
func Test_kiss_process_msg_extreme_timing_parameters(t *testing.T) {
	var xs = setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

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
			var output = testutils.CaptureOutput(t, func() { kiss_process_msg(c.msg, kissTestRadioConfig(), 0, nil, -1, sendfun) })

			assert.Contains(t, output, "Radio Channel - Transmit Timing")
		})
	}

	// Applied all the same.
	assert.Equal(t, 200, xs.timing[0].txdelay)
}

// A parameter command with no parameter is a protocol error, and leaves the
// setting alone rather than applying whatever happened to be next.
func Test_kiss_process_msg_missing_parameter(t *testing.T) {
	var xs = setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

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
			var output = testutils.CaptureOutput(t, func() { kiss_process_msg([]byte{c.cmd}, kissTestRadioConfig(), 0, nil, -1, sendfun) })

			assert.Contains(t, output, "KISS ERROR")
		})
	}

	assert.Equal(t, 0, xs.timing[0].txdelay)
}

// Leaving KISS mode is for a TNC that has another mode to go back to.  We
// don't, so it is noted and ignored.
func Test_kiss_process_msg_end_kiss(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var output = testutils.CaptureOutput(t, func() {
		kiss_process_msg([]byte{kiss.CmdEndKiss}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "end KISS mode - Ignored")
}

// A command we don't implement gets the troubleshooting tip, and the two
// XKISS ones additionally say what the application should be configured as
// instead.
func Test_kiss_process_msg_unsupported_commands(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var output = testutils.CaptureOutput(t, func() {
		kiss_process_msg([]byte{7}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "KISS Invalid command 7")
	assert.Contains(t, output, `Use "-d kn" option`)
	assert.NotContains(t, output, "XKISS")

	output = testutils.CaptureOutput(t, func() {
		kiss_process_msg([]byte{kiss.CmdXKissData}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, `"XKISS" protocol which is not supported`)
	assert.Contains(t, output, "Winlink Express")

	output = testutils.CaptureOutput(t, func() {
		kiss_process_msg([]byte{kiss.CmdXKissPoll}, kissTestRadioConfig(), 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, `"XKISS" protocol which is not supported`)
}

// "Set hardware" is the one command with an answer: the human readable
// question-and-answer form fldigi established, which several applications
// already speak.
func Test_kiss_set_hardware_tnc_version(t *testing.T) {
	setupKissProcessMsg(t)

	var sent, sendfun = recordingSendfun()

	kiss_process_msg(append([]byte{kiss.CmdSetHardware}, []byte("TNC:")...), kissTestRadioConfig(), 0, nil, -1, sendfun)

	require.Len(t, *sent, 1)
	assert.Equal(t, kiss.CmdSetHardware, (*sent)[0].cmd)
	assert.Contains(t, string((*sent)[0].body), "DIREWOLF ")
}

// TXBUF is the one an application actually wanted: how much is still waiting
// to go out, so that it can throttle a long transmission.
func Test_kiss_set_hardware_txbuf(t *testing.T) {
	setupKissProcessMsg(t)

	var sent, sendfun = recordingSendfun()

	kiss_set_hardware(0, []byte("TXBUF:"), 0, nil, -1, sendfun)

	require.Len(t, *sent, 1)
	assert.Equal(t, "TXBUF:0", string((*sent)[0].body))

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	transmitQueue.Append(0, TQ_PRIO_1_LO, pp)

	kiss_set_hardware(0, []byte("TXBUF:"), 0, nil, -1, sendfun)

	require.Len(t, *sent, 2)
	assert.NotEqual(t, "TXBUF:0", string((*sent)[1].body), "a queued frame should count towards TXBUF")
}

// A query with a parameter, a command we don't know, and one that isn't in the
// COMMAND:parameter form at all all get complained about rather than guessed
// at.
func Test_kiss_set_hardware_malformed(t *testing.T) {
	setupKissProcessMsg(t)

	var sent, sendfun = recordingSendfun()

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
				kiss_set_hardware(0, []byte(c.command), 0, nil, -1, sendfun)
			})

			assert.Contains(t, output, c.expect)
		})
	}

	// The two queries still answer, wrong parameter or not.
	assert.Len(t, *sent, 2)
}

// KissRecByte is fed the client's byte stream one byte at a time, and acts on
// each whole frame kiss.Collector finds in it, and on the text around them.

// feedKissBytes hands the bytes to the collector one at a time, as a
// transport does, and returns whatever was sent back to the client.
func feedKissBytes(kf *kiss.Collector, debug int, data []byte) *[]sentToClient {
	var sent, sendfun = recordingSendfun()

	var audioConfig = kissTestRadioConfig()

	for _, b := range data {
		KissRecByte(kf, audioConfig, b, debug, nil, -1, sendfun)
	}

	return sent
}

func Test_KissRecByte_whole_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	feedKissBytes(kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Equal(t, 1, transmitQueue.Count(0, TQ_PRIO_1_LO, "", "", false))
}

// An application that thinks it is talking to an old command-mode TNC sends
// lines of text.  Each one ending in a carriage return is answered with a
// command prompt, which is what stops it trying.
func Test_KissRecByte_command_prompt(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var sent = feedKissBytes(kf, 0, []byte("XFLOW OFF\r"))

	require.Len(t, *sent, 1)
	assert.Equal(t, "\r\ncmd:", string((*sent)[0].body))
	assert.Equal(t, -1, (*sent)[0].length, "a text reply is not a frame")
}

// "RESTART" and "RESET" get an empty KISS frame instead, which is what the
// applications sending them are waiting for.
func Test_KissRecByte_restart(t *testing.T) {
	setupKissProcessMsg(t)

	for _, word := range []string{"RESTART\r", "reset\r"} {
		t.Run(word, func(t *testing.T) {
			var kf = new(kiss.Collector)

			var sent = feedKissBytes(kf, 0, []byte(word))

			require.Len(t, *sent, 1)
			assert.Equal(t, "\xc0\xc0", string((*sent)[0].body))
		})
	}
}

// With the debug option on, the noise before a frame is shown, so that a
// client that is not being understood can be looked at.
func Test_KissRecByte_noise_is_printed(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(kf, 1, append([]byte("junk"), kiss.FEND))
	})

	assert.Contains(t, output, "Rejected Noise")
}

// FENDs with nothing between them are how some clients idle, and are not
// frames.
func Test_KissRecByte_empty_frames(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	feedKissBytes(kf, 0, []byte{kiss.FEND, kiss.FEND, kiss.FEND, kiss.FEND})

	assert.Equal(t, 0, transmitQueue.Count(0, -1, "", "", false))
}

// A client that never sends a closing FEND is told about once, not once for
// every byte past the limit.
func Test_KissRecByte_overlong_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(kf, 0, append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...))
	})

	assert.Equal(t, 1, strings.Count(output, "KISS message exceeded maximum length"))
}

// The client does eventually send its closing FEND, and the byte it used to be
// written to was one past the end of the buffer - which took the whole program
// down, from anything that could talk to the KISS port.  The overlong frame is
// thrown away, and the collector is left ready for the next one.
func Test_KissRecByte_overlong_frame_closing_fend(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var overlong = append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen+10)...)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(kf, 0, append(overlong, kiss.FEND))
	})

	assert.Contains(t, output, "KISS message exceeded maximum length.  Discarding it.")
	assert.Equal(t, 0, transmitQueue.Count(0, -1, "", "", false), "a fragment of the overlong frame was acted on")

	// And a well formed frame after it still gets through.
	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	feedKissBytes(kf, 0, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))

	assert.Equal(t, 1, transmitQueue.Count(0, -1, "", "", false))
}

// With "-d kn" the frame is shown as it arrived and again as it was decoded,
// which is the pair a protocol problem shows up in.
func Test_KissRecByte_debug_prints_both_forms(t *testing.T) {
	setupKissProcessMsg(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(kf, 2, kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...)))
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
	setupKissProcessMsg(t)

	var kf = new(kiss.Collector)

	var output = testutils.CaptureOutput(t, func() {
		feedKissBytes(kf, 2, []byte{kiss.FEND, kiss.FESC, kiss.FEND})
	})

	assert.Contains(t, output, "nothing in it")
	assert.Equal(t, 0, transmitQueue.Count(0, -1, "", "", false))
}
