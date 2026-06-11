package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendfunRecorder records every call made to its sendfun, so tests can assert
// both the contents of an acknowledgement and how many were sent.
type sendfunRecorder struct {
	calls []sendfunCall
}

type sendfunCall struct {
	channel int
	cmd     int
	fbuf    []byte
	flen    int
	kps     *kissport_status_s
	client  int
}

func (r *sendfunRecorder) fn(channel int, cmd int, fbuf []byte, flen int, kps *kissport_status_s, client int) {
	// Copy fbuf - callers reuse/free the backing array.
	var buf = make([]byte, len(fbuf))
	copy(buf, fbuf)
	r.calls = append(r.calls, sendfunCall{channel: channel, cmd: cmd, fbuf: buf, flen: flen, kps: kps, client: client})
}

// -------------------------------------------------------------------------
// Side-table unit tests (no global state).
// -------------------------------------------------------------------------

func TestAckmode_NotifyAfterRegister(t *testing.T) {
	var rec sendfunRecorder
	var pp = new(packet_t)
	var kps = new(kissport_status_s)

	ackmode_register(pp, [2]byte{0x12, 0x34}, 2, rec.fn, kps, 5)
	ackmode_notify_sent(pp)

	require.Len(t, rec.calls, 1)
	var c = rec.calls[0]
	assert.Equal(t, 2, c.channel)
	assert.Equal(t, XKISS_CMD_DATA, c.cmd) // 0x0C, NOT the 0x0E poll command
	assert.Equal(t, []byte{0x12, 0x34}, c.fbuf)
	assert.Equal(t, 2, c.flen)
	assert.Same(t, kps, c.kps)
	assert.Equal(t, 5, c.client)
}

func TestAckmode_NotifyUnknownPacket(t *testing.T) {
	// Notifying a packet that was never registered must not send or panic.
	var rec sendfunRecorder
	var pp = new(packet_t)

	ackmode_notify_sent(pp)

	assert.Empty(t, rec.calls)
}

func TestAckmode_NotifyIdempotent(t *testing.T) {
	// A second notify on the same packet must not send another ack.
	var rec sendfunRecorder
	var pp = new(packet_t)

	ackmode_register(pp, [2]byte{0x00, 0x01}, 0, rec.fn, nil, 0)
	ackmode_notify_sent(pp)
	ackmode_notify_sent(pp)

	assert.Len(t, rec.calls, 1)
}

func TestAckmode_DiscardNoSend(t *testing.T) {
	// Discard must remove the entry without invoking the sendfun.
	var rec sendfunRecorder
	var pp = new(packet_t)

	ackmode_register(pp, [2]byte{0xAB, 0xCD}, 1, rec.fn, nil, 3)
	ackmode_discard(pp)
	ackmode_notify_sent(pp) // now a no-op

	assert.Empty(t, rec.calls)
}

func TestAckmode_DiscardUnknownPacket(t *testing.T) {
	// Discarding an unregistered packet must not panic.
	var pp = new(packet_t)
	ackmode_discard(pp)
}

// TestAckmode_IdBytesEchoedVerbatim proves the two id bytes are treated as
// completely opaque and returned in the same order - including values that
// collide with the KISS framing bytes FEND (0xC0) and FESC (0xDB), which are
// transparency-escaped further down the stack (SendRecPacket), not here.
func TestAckmode_IdBytesEchoedVerbatim(t *testing.T) {
	var cases = [][2]byte{
		{0x00, 0x00},
		{0xFF, 0xFF},
		{0x12, 0x34},
		{0xC0, 0xDB}, // FEND, FESC
		{0xDB, 0xC0},
	}

	for _, id := range cases {
		var rec sendfunRecorder
		var pp = new(packet_t)

		ackmode_register(pp, id, 0, rec.fn, nil, 0)
		ackmode_notify_sent(pp)

		require.Len(t, rec.calls, 1)
		assert.Equal(t, []byte{id[0], id[1]}, rec.calls[0].fbuf, "id %#v must echo verbatim", id)
	}
}

// TestAckmode_DistinctPacketsIndependent proves entries are keyed per packet.
func TestAckmode_DistinctPacketsIndependent(t *testing.T) {
	var rec sendfunRecorder
	var pp1 = new(packet_t)
	var pp2 = new(packet_t)

	ackmode_register(pp1, [2]byte{0x11, 0x11}, 0, rec.fn, nil, 1)
	ackmode_register(pp2, [2]byte{0x22, 0x22}, 0, rec.fn, nil, 2)

	ackmode_notify_sent(pp2)
	ackmode_notify_sent(pp1)

	require.Len(t, rec.calls, 2)
	assert.Equal(t, []byte{0x22, 0x22}, rec.calls[0].fbuf)
	assert.Equal(t, 2, rec.calls[0].client)
	assert.Equal(t, []byte{0x11, 0x11}, rec.calls[1].fbuf)
	assert.Equal(t, 1, rec.calls[1].client)
}

// -------------------------------------------------------------------------
// kiss_process_msg integration tests (exercise the real parse + register path).
// -------------------------------------------------------------------------

func setupAckmodeEnv(t *testing.T) {
	t.Helper()

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	ptt_init(cfg)
	tq_init(cfg)
	save_audio_config_p = cfg

	var mc = new(misc_config_s) // kiss_copy defaults to false, no ports -> no sockets
	kissNetSvc = NewKissNetService(mc)

	t.Cleanup(func() {
		save_audio_config_p = nil
		kissNetSvc = nil
	})
}

// ackmodeFrame builds a KISS ACKMODE data frame: command byte 0x0C, two id
// bytes, then a raw (FCS-less) AX.25 frame.
func ackmodeFrame(t *testing.T, id0, id1 byte) []byte {
	t.Helper()

	var src = ax25_from_text("Q1TEST>Q2TEST:hello ackmode", true)
	require.NotNil(t, src)
	var raw = ax25_pack(src)
	ax25_delete(src)

	var msg = []byte{XKISS_CMD_DATA, id0, id1}
	msg = append(msg, raw...)
	return msg
}

func TestAckmode_ProcessMsgRoundTrip(t *testing.T) {
	setupAckmodeEnv(t)

	var rec sendfunRecorder
	var msg = ackmodeFrame(t, 0xAB, 0xCD)

	// Serial/pty style: kps nil, client -1, channel taken from the frame nibble (0).
	kiss_process_msg(msg, 0, nil, -1, rec.fn)

	// The frame must have been queued for transmission (low priority - no digi).
	var pp = tq_remove(0, TQ_PRIO_1_LO)
	require.NotNil(t, pp, "ACKMODE frame should have been queued for transmit")

	// No ack should have been sent yet - it has not been transmitted.
	assert.Empty(t, rec.calls, "ack must not be sent before transmission")

	// Simulate the transmitter keying the frame out.
	ackmode_notify_sent(pp)
	ax25_delete(pp)

	require.Len(t, rec.calls, 1)
	var c = rec.calls[0]
	assert.Equal(t, 0, c.channel)
	assert.Equal(t, XKISS_CMD_DATA, c.cmd)
	assert.Equal(t, []byte{0xAB, 0xCD}, c.fbuf)
	assert.Equal(t, -1, c.client)
}

// A normal (command 0x00) data frame must NOT be registered for an ack.
func TestAckmode_NormalDataFrameNotRegistered(t *testing.T) {
	setupAckmodeEnv(t)

	var rec sendfunRecorder

	var src = ax25_from_text("Q1TEST>Q2TEST:plain frame", true)
	require.NotNil(t, src)
	var raw = ax25_pack(src)
	ax25_delete(src)

	var msg = []byte{KISS_CMD_DATA_FRAME} // command 0x00, channel 0
	msg = append(msg, raw...)

	kiss_process_msg(msg, 0, nil, -1, rec.fn)

	var pp = tq_remove(0, TQ_PRIO_1_LO)
	require.NotNil(t, pp)

	ackmode_notify_sent(pp) // would send an ack only if registered
	ax25_delete(pp)

	assert.Empty(t, rec.calls, "a normal data frame must not produce an ACKMODE ack")
}

// A too-short ACKMODE frame must be rejected without queueing or panicking.
func TestAckmode_ProcessMsgTooShort(t *testing.T) {
	setupAckmodeEnv(t)

	var rec sendfunRecorder

	// command + 2 id bytes but no AX.25 frame at all.
	kiss_process_msg([]byte{XKISS_CMD_DATA, 0x01, 0x02}, 0, nil, -1, rec.fn)

	assert.Nil(t, tq_remove(0, TQ_PRIO_1_LO), "too-short ACKMODE frame must not be queued")
	assert.Empty(t, rec.calls)
}
