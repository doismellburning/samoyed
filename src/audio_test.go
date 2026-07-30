// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- UDP audio output ---

// setupAdev0 installs a fresh adev_s at index 0 and restores the original on
// test cleanup.  Using index 0 is safe because audio tests are sequential.
func setupAdev0(t *testing.T) *adev_s {
	t.Helper()

	var prev = adev[0]
	t.Cleanup(func() { adev[0] = prev })

	adev[0] = new(adev_s)

	return adev[0]
}

func Test_audioFlushReal_UDP_sendsBytes(t *testing.T) {
	// Start a UDP listener to receive the audio output.
	var listener, err = new(net.ListenConfig).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	// Dial from the "transmitter" side.
	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(context.Background(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	defer conn.Close()

	var dev = setupAdev0(t)
	dev.udp_out_sock = conn
	dev.outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	dev.outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)

	var testData = []byte{0xDE, 0xAD, 0xBE, 0xEF}
	copy(dev.outbuf, testData)
	dev.outbufLen = len(testData)

	var result = audio_flush_real(0)
	assert.Equal(t, 0, result)
	assert.Equal(t, 0, dev.outbufLen, "output buffer should be cleared after flush")

	// Receive and verify the packet contents.
	var buf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))

	var n int
	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf[:n])
}

// --- anyDeviceRequiresAudioBackend ---

func makeAudioConfig(inName, outName string) *audio_s {
	var pa = new(audio_s)
	pa.adev[0].defined = 1
	pa.adev[0].adevice_in = inName
	pa.adev[0].adevice_out = outName

	return pa
}

func Test_anyDeviceRequiresAudioBackend(t *testing.T) {
	tests := []struct {
		name string
		pa   *audio_s
		want bool
	}{
		{
			name: "no devices defined",
			pa:   new(audio_s),
			want: false,
		},
		{
			name: "stdin in, udp out",
			pa:   makeAudioConfig("stdin", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "dash in, udp out",
			pa:   makeAudioConfig("-", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "udp in (uppercase), udp out",
			pa:   makeAudioConfig("UDP:7355", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "stdin in, soundcard out",
			pa:   makeAudioConfig("stdin", "default"),
			want: true,
		},
		{
			name: "soundcard in, udp out",
			pa:   makeAudioConfig("default", "udp:127.0.0.1:1234"),
			want: true,
		},
		{
			name: "soundcard in, soundcard out",
			pa:   makeAudioConfig("default", "default"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, anyDeviceRequiresAudioBackend(tt.pa))
		})
	}
}

func Test_audioFlushReal_UDP_emptyBuffer_isNoop(t *testing.T) {
	var dev = setupAdev0(t)
	dev.udp_out_sock = &net.UDPConn{} // non-nil socket; must not be written to
	dev.outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	dev.outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	dev.outbufLen = 0

	// Should return 0 without attempting a write.
	assert.Equal(t, 0, audio_flush_real(0))
}

func Test_audioUDPSilenceKeepalive_chunkSizeAndCleanShutdown(t *testing.T) {
	// Start a UDP listener to receive the keepalive silence.
	var listener, err = new(net.ListenConfig).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(context.Background(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	defer conn.Close()

	var dev = setupAdev0(t)
	dev.udp_out_sock = conn
	dev.bitsPerSample = 16
	dev.bytesPerFrame = 2  // mono, 16-bit
	dev.sampleRate = 44100 // 20ms of samples at this rate exceeds UDP_AUDIO_OUT_BUF_MAXLEN, so the chunk must be capped

	var prevXmitSvc = xmitSvc
	t.Cleanup(func() { xmitSvc = prevXmitSvc })
	xmitSvc = &XmitService{} //nolint:exhaustruct

	var stop = make(chan struct{})
	var done = make(chan struct{})

	go func() {
		defer close(done)
		audioUDPSilenceKeepalive(0, stop)
	}()

	// Receive a chunk and verify it's frame-aligned and capped. The buffer
	// is deliberately larger than UDP_AUDIO_OUT_BUF_MAXLEN so an oversize
	// datagram would show up as a too-large read rather than being silently
	// truncated by ReadFrom.
	var buf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN*2)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))

	var n int
	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.NotZero(t, n)
	assert.LessOrEqual(t, n, UDP_AUDIO_OUT_BUF_MAXLEN, "chunk length must not exceed UDP_AUDIO_OUT_BUF_MAXLEN")
	assert.Zero(t, n%dev.bytesPerFrame, "chunk length must be a multiple of bytesPerFrame")

	// Closing stop should make the goroutine exit promptly.
	close(stop)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("audioUDPSilenceKeepalive did not stop after stop was closed")
	}
}

// --- deviceIDFromName ---

func Test_deviceIDFromName(t *testing.T) {
	assert.Nil(t, deviceIDFromName(""))
	assert.Nil(t, deviceIDFromName("default"))
	assert.Nil(t, deviceIDFromName("DEFAULT"))

	var id = deviceIDFromName("plughw:Loopback,1,1")
	require.NotNil(t, id)
	assert.Equal(t, "plughw:Loopback,1,1", string(id[:len("plughw:Loopback,1,1")]))
}

// --- playbackRingBuffer ---

func Test_playbackRingBuffer_writeThenRead(t *testing.T) {
	var rb = newPlaybackRingBuffer(8, 0)

	rb.write([]byte{1, 2, 3, 4})

	var dst = make([]byte, 4)
	rb.read(dst)
	assert.Equal(t, []byte{1, 2, 3, 4}, dst)
}

func Test_playbackRingBuffer_readPadsWithSilenceOnUnderrun(t *testing.T) {
	var rb = newPlaybackRingBuffer(8, 128)

	rb.write([]byte{1, 2})

	var dst = make([]byte, 4)
	rb.read(dst)
	assert.Equal(t, []byte{1, 2, 128, 128}, dst)
}

func Test_playbackRingBuffer_waitEmptyReturnsAfterDrain(t *testing.T) {
	var rb = newPlaybackRingBuffer(8, 0)

	rb.write([]byte{1, 2, 3, 4})

	var done = make(chan struct{})
	go func() {
		defer close(done)
		rb.waitEmpty()
	}()

	select {
	case <-done:
		t.Fatal("waitEmpty returned before the buffer was drained")
	case <-time.After(50 * time.Millisecond):
	}

	var dst = make([]byte, 4)
	rb.read(dst)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitEmpty did not return after the buffer was drained")
	}
}

func Test_playbackRingBuffer_writeBlocksUntilSpaceAvailable(t *testing.T) {
	var rb = newPlaybackRingBuffer(4, 0)

	rb.write([]byte{1, 2, 3, 4}) // fill it completely

	var writeDone = make(chan struct{})
	go func() {
		defer close(writeDone)
		rb.write([]byte{5, 6})
	}()

	select {
	case <-writeDone:
		t.Fatal("write returned before space was freed")
	case <-time.After(50 * time.Millisecond):
	}

	var dst = make([]byte, 4)
	rb.read(dst) // frees all 4 bytes of space

	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("write did not return after space became available")
	}
}
