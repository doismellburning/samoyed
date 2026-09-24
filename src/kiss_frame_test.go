package direwolf

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Whatever KissEncapsulate escaped, KissUnescape should give back.
func Test_KissUnescape_RoundTrip(t *testing.T) {
	var original = []byte{0x00, 0x82, FEND, 0xa0, FESC, FEND, FESC, 0x41}

	var encapsulated = KissEncapsulate(original)

	require.Greater(t, len(encapsulated), len(original)+2) // Something was escaped.

	var unescaped, problems = KissUnescape(encapsulated[1 : len(encapsulated)-1])

	assert.Empty(t, problems)
	assert.Equal(t, original, unescaped)
}

func Test_KissUnescape_NothingEscaped(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{0x00, 0x82, 0xa0})

	assert.Empty(t, problems)
	assert.Equal(t, []byte{0x00, 0x82, 0xa0}, unescaped)
}

// An unexpected byte after FESC is taken literally so the rest of the frame
// can still be decoded.
func Test_KissUnescape_BadEscape(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{0x00, FESC, 0x41, 0x82})

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Error(), "FESC (0xdb) at offset 1 is followed by 0x41")
	assert.Equal(t, []byte{0x00, 0x41, 0x82}, unescaped)
}

func Test_KissUnescape_TruncatedEscape(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{0x00, 0x82, FESC})

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Error(), "frame ends with FESC (0xdb) at offset 2")
	assert.Equal(t, []byte{0x00, 0x82}, unescaped)
}

func Test_KissUnescape_Empty(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{})

	assert.Empty(t, problems)
	assert.Empty(t, unescaped)
}

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

// setupKissProcessMsg gives kiss_process_msg the things it reaches for: the
// channel table it checks a transmit request against, the transmit settings it
// applies parameters to, and the service that copies frames between clients.
func setupKissProcessMsg(t *testing.T) *XmitService {
	t.Helper()

	var origAudio, origXmit, origKissNet = save_audio_config_p, xmitSvc, kissNetSvc

	t.Cleanup(func() {
		save_audio_config_p, xmitSvc, kissNetSvc = origAudio, origXmit, origKissNet

		for c := range MAX_RADIO_CHANS {
			for p := range TQ_NUM_PRIO {
				for transmitQueue.Remove(c, p) != nil { //revive:disable-line:empty-block
				}
			}
		}
	})

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[1] = MEDIUM_RADIO

	kiss_frame_init(audioConfig)

	xmitSvc = new(XmitService)
	kissNetSvc = NewKissNetService(t.Context(), new(misc_config_s))

	transmitQueue.Init(audioConfig)

	return xmitSvc
}

// A data frame from the client is a frame to transmit, and an original - one
// with no used digipeater in it - goes on the low priority queue behind
// anything already being repeated.
func Test_kiss_process_msg_data_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	kiss_process_msg(append([]byte{KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...), 0, nil, -1, sendfun)

	assert.Equal(t, 1, transmitQueue.Count(0, TQ_PRIO_1_LO, "", "", false))
	assert.Equal(t, 0, transmitQueue.Count(0, TQ_PRIO_0_HI, "", "", false))
}

// A frame that has already been through a digipeater is somebody waiting on
// air for it, so it jumps the queue.
func Test_kiss_process_msg_repeated_frame_is_high_priority(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var pp = AX25FromText("Q1TEST>Q2TEST,Q3TEST*:hello", true)
	require.NotNil(t, pp)

	kiss_process_msg(append([]byte{KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...), 0, nil, -1, sendfun)

	assert.Equal(t, 1, transmitQueue.Count(0, TQ_PRIO_0_HI, "", "", false))
}

// The channel is in the top half of the first byte, and a client asking for a
// channel we do not have is the AX.25-for-Linux CRC mode problem, which gets
// an explanation rather than a transmission.
func Test_kiss_process_msg_invalid_channel(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() {
		kiss_process_msg(append([]byte{0x80 | KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...), 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "Invalid transmit channel 8 from KISS client app")
	assert.Contains(t, output, "kissparms -c 1 -p radio")
	assert.Equal(t, 0, transmitQueue.Count(8, -1, "", "", false))
}

// A KISS TCP port carrying a single radio channel ignores the channel in the
// frame: the application thinks it has a one-radio TNC and always says 0.
func Test_kiss_process_msg_port_channel_overrides_the_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var kps = new(kissport_status_s)
	kps.channel = 1

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	kiss_process_msg(append([]byte{KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...), 0, kps, 0, sendfun)

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

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output string

	assert.NotPanics(t, func() {
		output = CaptureOutput(t, func() {
			kiss_process_msg(append([]byte{KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...), 0, kps, 0, sendfun)
		})
	})

	assert.Contains(t, output, "Invalid transmit channel 16 from KISS client app")
}

// Bytes that are not an AX.25 frame cannot be transmitted, and are reported
// rather than passed on.
func Test_kiss_process_msg_undecodable_data_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var output = CaptureOutput(t, func() {
		kiss_process_msg([]byte{KISS_CMD_DATA_FRAME, 'n', 'o'}, 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "Invalid KISS data frame from client app")
}

// The timing parameters are the rest of what a client sets up, and each is one
// byte after the command.
func Test_kiss_process_msg_timing_parameters(t *testing.T) {
	var xs = setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	kiss_process_msg([]byte{KISS_CMD_TXDELAY, 30}, 0, nil, -1, sendfun)
	kiss_process_msg([]byte{KISS_CMD_PERSISTENCE, 63}, 0, nil, -1, sendfun)
	kiss_process_msg([]byte{KISS_CMD_SLOTTIME, 10}, 0, nil, -1, sendfun)
	kiss_process_msg([]byte{KISS_CMD_TXTAIL, 10}, 0, nil, -1, sendfun)
	kiss_process_msg([]byte{KISS_CMD_FULLDUPLEX, 1}, 0, nil, -1, sendfun)

	assert.Equal(t, 30, xs.txdelay[0])
	assert.Equal(t, 63, xs.persist[0])
	assert.Equal(t, 10, xs.slottime[0])
	assert.Equal(t, 10, xs.txtail[0])
	assert.True(t, xs.fulldup[0])
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
		{"TXDELAY", []byte{KISS_CMD_TXDELAY, 200}},
		{"PERSIST", []byte{KISS_CMD_PERSISTENCE, 1}},
		{"SLOTTIME", []byte{KISS_CMD_SLOTTIME, 100}},
		{"TXTAIL", []byte{KISS_CMD_TXTAIL, 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var output = CaptureOutput(t, func() { kiss_process_msg(c.msg, 0, nil, -1, sendfun) })

			assert.Contains(t, output, "Radio Channel - Transmit Timing")
		})
	}

	// Applied all the same.
	assert.Equal(t, 200, xs.txdelay[0])
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
		{"TXDELAY", KISS_CMD_TXDELAY},
		{"PERSISTENCE", KISS_CMD_PERSISTENCE},
		{"SLOTTIME", KISS_CMD_SLOTTIME},
		{"TXTAIL", KISS_CMD_TXTAIL},
		{"FULLDUPLEX", KISS_CMD_FULLDUPLEX},
		{"SET HARDWARE", KISS_CMD_SET_HARDWARE},
	} {
		t.Run(c.name, func(t *testing.T) {
			var output = CaptureOutput(t, func() { kiss_process_msg([]byte{c.cmd}, 0, nil, -1, sendfun) })

			assert.Contains(t, output, "KISS ERROR")
		})
	}

	assert.Equal(t, 0, xs.txdelay[0])
}

// Leaving KISS mode is for a TNC that has another mode to go back to.  We
// don't, so it is noted and ignored.
func Test_kiss_process_msg_end_kiss(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var output = CaptureOutput(t, func() {
		kiss_process_msg([]byte{KISS_CMD_END_KISS}, 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "end KISS mode - Ignored")
}

// A command we don't implement gets the troubleshooting tip, and the two
// XKISS ones additionally say what the application should be configured as
// instead.
func Test_kiss_process_msg_unsupported_commands(t *testing.T) {
	setupKissProcessMsg(t)

	var _, sendfun = recordingSendfun()

	var output = CaptureOutput(t, func() {
		kiss_process_msg([]byte{7}, 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, "KISS Invalid command 7")
	assert.Contains(t, output, `Use "-d kn" option`)
	assert.NotContains(t, output, "XKISS")

	output = CaptureOutput(t, func() {
		kiss_process_msg([]byte{XKISS_CMD_DATA}, 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, `"XKISS" protocol which is not supported`)
	assert.Contains(t, output, "Winlink Express")

	output = CaptureOutput(t, func() {
		kiss_process_msg([]byte{XKISS_CMD_POLL}, 0, nil, -1, sendfun)
	})

	assert.Contains(t, output, `"XKISS" protocol which is not supported`)
}

// "Set hardware" is the one command with an answer: the human readable
// question-and-answer form fldigi established, which several applications
// already speak.
func Test_kiss_set_hardware_tnc_version(t *testing.T) {
	setupKissProcessMsg(t)

	var sent, sendfun = recordingSendfun()

	kiss_process_msg(append([]byte{KISS_CMD_SET_HARDWARE}, []byte("TNC:")...), 0, nil, -1, sendfun)

	require.Len(t, *sent, 1)
	assert.Equal(t, KISS_CMD_SET_HARDWARE, (*sent)[0].cmd)
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

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
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
			var output = CaptureOutput(t, func() {
				kiss_set_hardware(0, []byte(c.command), 0, nil, -1, sendfun)
			})

			assert.Contains(t, output, c.expect)
		})
	}

	// The two queries still answer, wrong parameter or not.
	assert.Len(t, *sent, 2)
}

// KissRecByte is the frame collector: it is fed the client's byte stream one
// byte at a time and acts on each whole frame it finds.

// feedKissBytes hands the bytes to the collector one at a time, as a
// transport does, and returns whatever was sent back to the client.
func feedKissBytes(kf *KISSFrame, debug int, data []byte) *[]sentToClient {
	var sent, sendfun = recordingSendfun()

	for _, b := range data {
		KissRecByte(kf, b, debug, nil, -1, sendfun)
	}

	return sent
}

func Test_KissRecByte_whole_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(KISSFrame)

	feedKissBytes(kf, 0, KissEncapsulate(append([]byte{KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...)))

	assert.Equal(t, 1, transmitQueue.Count(0, TQ_PRIO_1_LO, "", "", false))
	assert.Equal(t, KS_SEARCHING, kf.state, "the collector should be ready for the next frame")
}

// An application that thinks it is talking to an old command-mode TNC sends
// lines of text.  Each one ending in a carriage return is answered with a
// command prompt, which is what stops it trying.
func Test_KissRecByte_command_prompt(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(KISSFrame)

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
			var kf = new(KISSFrame)

			var sent = feedKissBytes(kf, 0, []byte(word))

			require.Len(t, *sent, 1)
			assert.Equal(t, "\xc0\xc0", string((*sent)[0].body))
		})
	}
}

// Noise is kept for the debug output, but only so much of it: a client
// spraying bytes must not be able to make the collector grow without limit.
func Test_KissRecByte_noise_is_bounded(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(KISSFrame)

	feedKissBytes(kf, 0, bytes.Repeat([]byte{'x'}, MAX_NOISE_LEN*2))

	assert.Equal(t, MAX_NOISE_LEN, kf.noise_len)
}

// With the debug option on, the noise before a frame is shown, so that a
// client that is not being understood can be looked at.
func Test_KissRecByte_noise_is_printed(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(KISSFrame)

	var output = CaptureOutput(t, func() {
		feedKissBytes(kf, 1, append([]byte("junk"), FEND))
	})

	assert.Contains(t, output, "Rejected Noise")
}

// FENDs with nothing between them are how some clients idle, and are not
// frames.
func Test_KissRecByte_empty_frames(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(KISSFrame)

	feedKissBytes(kf, 0, []byte{FEND, FEND, FEND, FEND})

	assert.Equal(t, 0, transmitQueue.Count(0, -1, "", "", false))
}

// A client that never sends a closing FEND would otherwise fill the frame
// buffer without limit.
func Test_KissRecByte_overlong_frame(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(KISSFrame)

	var output = CaptureOutput(t, func() {
		feedKissBytes(kf, 0, append([]byte{FEND}, bytes.Repeat([]byte{'x'}, MAX_KISS_LEN+10)...))
	})

	assert.Contains(t, output, "KISS message exceeded maximum length")
	assert.Equal(t, MAX_KISS_LEN, kf.kiss_len)
}

// The client does eventually send its closing FEND, and the byte it used to be
// written to was one past the end of the buffer - which took the whole program
// down, from anything that could talk to the KISS port.  The overlong frame is
// thrown away, and the collector is left ready for the next one.
func Test_KissRecByte_overlong_frame_closing_fend(t *testing.T) {
	setupKissProcessMsg(t)

	var kf = new(KISSFrame)

	var overlong = append([]byte{FEND}, bytes.Repeat([]byte{'x'}, MAX_KISS_LEN+10)...)

	var output = CaptureOutput(t, func() {
		feedKissBytes(kf, 0, append(overlong, FEND))
	})

	assert.Contains(t, output, "KISS message exceeded maximum length.  Discarding it.")
	assert.Equal(t, 0, kf.kiss_len)
	assert.Equal(t, KS_SEARCHING, kf.state)
	assert.Equal(t, 0, transmitQueue.Count(0, -1, "", "", false), "a fragment of the overlong frame was acted on")

	// And a well formed frame after it still gets through.
	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	feedKissBytes(kf, 0, KissEncapsulate(append([]byte{KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...)))

	assert.Equal(t, 1, transmitQueue.Count(0, -1, "", "", false))
}

// With "-d kn" the frame is shown as it arrived and again as it was decoded,
// which is the pair a protocol problem shows up in.
func Test_KissRecByte_debug_prints_both_forms(t *testing.T) {
	setupKissProcessMsg(t)

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var kf = new(KISSFrame)

	var output = CaptureOutput(t, func() {
		feedKissBytes(kf, 2, KissEncapsulate(append([]byte{KISS_CMD_DATA_FRAME}, ax25_get_frame_data(pp)...)))
	})

	assert.Contains(t, output, "<<< Data frame from KISS client application, channel 0")
	assert.Contains(t, output, "Packet content after removing KISS framing")
}

// kissutil is a client, not a TNC, so a collector with OnMessage hands each
// message over rather than trying to transmit it.
func Test_KissRecByte_OnMessage(t *testing.T) {
	setupKissProcessMsg(t)

	var got []byte

	var kf = new(KISSFrame)
	kf.OnMessage = func(msg []byte) { got = msg }

	feedKissBytes(kf, 0, KissEncapsulate([]byte{KISS_CMD_DATA_FRAME, 'h', 'i'}))

	assert.Equal(t, []byte{KISS_CMD_DATA_FRAME, 'h', 'i'}, got)
	assert.Equal(t, 0, transmitQueue.Count(0, -1, "", "", false), "kissutil should not be transmitting")
}

// What kissutil collects came from the TNC, so its debug output says so.
func Test_KissRecByte_OnMessage_debug(t *testing.T) {
	var kf = new(KISSFrame)
	kf.OnMessage = func([]byte) {}

	var output = CaptureOutput(t, func() {
		feedKissBytes(kf, 1, KissEncapsulate([]byte{KISS_CMD_DATA_FRAME, 'h', 'i'}))
	})

	assert.Contains(t, output, "From KISS TNC:")
	assert.NotContains(t, output, "KISS client application")
}

// kiss_unwrap takes the escapes and framing back out, complaining about
// anything malformed but carrying on - a live TNC has to do something with
// what it was given.

func Test_kiss_unwrap_too_short(t *testing.T) {
	var output = CaptureOutput(t, func() {
		assert.Empty(t, kiss_unwrap([]byte{FEND}))
	})

	assert.Contains(t, output, "less than minimum length")
}

func Test_kiss_unwrap_no_trailing_fend(t *testing.T) {
	var unwrapped []byte

	var output = CaptureOutput(t, func() {
		unwrapped = kiss_unwrap([]byte{FEND, 0x00, 'h', 'i'})
	})

	assert.Contains(t, output, "should end with FEND")
	assert.Equal(t, []byte{0x00, 'h', 'i'}, unwrapped)
}

func Test_kiss_unwrap_fend_in_the_middle(t *testing.T) {
	var output = CaptureOutput(t, func() {
		kiss_unwrap([]byte{FEND, 0x00, FEND, 'h', FEND})
	})

	assert.Contains(t, output, "should not have FEND in the middle")
}

// An escape followed by something that is not one of the two transposed bytes
// is a protocol error; the byte is dropped rather than guessed at.
func Test_kiss_unwrap_bad_escape(t *testing.T) {
	var unwrapped []byte

	var output = CaptureOutput(t, func() {
		unwrapped = kiss_unwrap([]byte{0x00, FESC, 'x', 'y', FEND})
	})

	assert.Contains(t, output, "Found 0x78 after FESC")
	assert.Equal(t, []byte{0x00, 'y'}, unwrapped)
}

// The leading FEND is optional, so a frame without one unwraps the same way.
func Test_kiss_unwrap_without_leading_fend(t *testing.T) {
	assert.Equal(t, []byte{0x00, 'h', 'i'}, kiss_unwrap([]byte{0x00, 'h', 'i', FEND}))
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
