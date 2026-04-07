package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockSendfun captures the arguments of a single sendfun call.
type sendfunCapture struct {
	called  bool
	channel int
	cmd     int
	fbuf    []byte
	flen    int
	kps     *kissport_status_s
	client  int
}

func (c *sendfunCapture) fn(channel int, cmd int, fbuf []byte, flen int, kps *kissport_status_s, client int) {
	c.called = true
	c.channel = channel
	c.cmd = cmd
	c.fbuf = fbuf
	c.flen = flen
	c.kps = kps
	c.client = client
}

// fakePP returns a non-nil *packet_t pointer without allocating a real packet,
// using the address of a local variable as a unique key.
func fakePP(t *testing.T) *packet_t {
	t.Helper()
	var p packet_t
	return &p
}

func TestAckmode_NotifyAfterRegister(t *testing.T) {
	var capture sendfunCapture
	var pp = fakePP(t)

	ackmode_register(pp, 0x1234, 2, capture.fn, nil, 5)
	ackmode_notify_sent(pp)

	assert.True(t, capture.called)
	assert.Equal(t, 2, capture.channel)
	assert.Equal(t, XKISS_CMD_POLL, capture.cmd)
	assert.Equal(t, []byte{0x12, 0x34}, capture.fbuf)
	assert.Equal(t, 2, capture.flen)
	assert.Nil(t, capture.kps)
	assert.Equal(t, 5, capture.client)
}

func TestAckmode_NotifyUnknownPacket(t *testing.T) {
	// Calling notify on a packet that was never registered must not panic.
	var capture sendfunCapture
	var pp = fakePP(t)

	ackmode_notify_sent(pp)

	assert.False(t, capture.called)
}

func TestAckmode_NotifyIdempotent(t *testing.T) {
	// A second notify on the same packet must not send another ACK.
	var capture sendfunCapture
	var pp = fakePP(t)

	ackmode_register(pp, 0x0001, 0, capture.fn, nil, 0)
	ackmode_notify_sent(pp)
	ackmode_notify_sent(pp)

	assert.True(t, capture.called)
	// Reset and verify second call was a no-op by checking capture was only set once.
	var callCount int
	var pp2 = fakePP(t)
	var counting kiss_sendfun = func(_ int, _ int, _ []byte, _ int, _ *kissport_status_s, _ int) {
		callCount++
	}
	ackmode_register(pp2, 0x0002, 0, counting, nil, 0)
	ackmode_notify_sent(pp2)
	ackmode_notify_sent(pp2)
	assert.Equal(t, 1, callCount)
}

func TestAckmode_DiscardNoSend(t *testing.T) {
	// Discard must remove the entry without invoking the sendfun.
	var capture sendfunCapture
	var pp = fakePP(t)

	ackmode_register(pp, 0xABCD, 1, capture.fn, nil, 3)
	ackmode_discard(pp)
	ackmode_notify_sent(pp) // should be a no-op now

	assert.False(t, capture.called)
}

func TestAckmode_DiscardUnknownPacket(t *testing.T) {
	// Discarding an unregistered packet must not panic.
	var pp = fakePP(t)
	ackmode_discard(pp)
}

func TestAckmode_SeqnoZero(t *testing.T) {
	var capture sendfunCapture
	var pp = fakePP(t)

	ackmode_register(pp, 0x0000, 0, capture.fn, nil, 0)
	ackmode_notify_sent(pp)

	assert.Equal(t, []byte{0x00, 0x00}, capture.fbuf)
}

func TestAckmode_SeqnoMax(t *testing.T) {
	var capture sendfunCapture
	var pp = fakePP(t)

	ackmode_register(pp, 0xFFFF, 0, capture.fn, nil, 0)
	ackmode_notify_sent(pp)

	assert.Equal(t, []byte{0xFF, 0xFF}, capture.fbuf)
}
