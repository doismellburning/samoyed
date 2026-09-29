// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- audioRingBuffer ---

func TestAudioImpl_ringBuffer_writeThenRead(t *testing.T) {
	var rb = newAudioRingBuffer(8)

	assert.True(t, rb.write([]byte{1, 2, 3}))
	assert.True(t, rb.write(nil), "an empty write is not an overflow")
	assert.False(t, rb.checkOverflow())

	var dst = make([]byte, 8)
	var n, ok = rb.readChunk(dst)
	require.True(t, ok)
	assert.Equal(t, []byte{1, 2, 3}, dst[:n])

	// A read shorter than what is buffered leaves the rest for next time.
	assert.True(t, rb.write([]byte{4, 5, 6, 7}))

	n, ok = rb.readChunk(dst[:2])
	require.True(t, ok)
	assert.Equal(t, []byte{4, 5}, dst[:n])

	n, ok = rb.readChunk(dst)
	require.True(t, ok)
	assert.Equal(t, []byte{6, 7}, dst[:n])
}

func TestAudioImpl_ringBuffer_wrapsAround(t *testing.T) {
	var rb = newAudioRingBuffer(8)
	var dst = make([]byte, 8)

	// Move the read and write positions most of the way along.
	require.True(t, rb.write([]byte{0, 0, 0, 0, 0, 0}))

	var n, ok = rb.readChunk(dst)
	require.True(t, ok)
	require.Equal(t, 6, n)

	// This write straddles the end of the underlying array...
	assert.True(t, rb.write([]byte{1, 2, 3, 4, 5}))

	// ...and so does this read.
	n, ok = rb.readChunk(dst)
	require.True(t, ok)
	assert.Equal(t, []byte{1, 2, 3, 4, 5}, dst[:n])
}

func TestAudioImpl_ringBuffer_dropsOldestWhenFull(t *testing.T) {
	var rb = newAudioRingBuffer(4)
	var dst = make([]byte, 8)

	require.True(t, rb.write([]byte{1, 2, 3}))
	assert.False(t, rb.write([]byte{4, 5}), "one byte had to be dropped")
	assert.True(t, rb.checkOverflow())
	assert.False(t, rb.checkOverflow(), "checkOverflow clears the flag")

	var n, ok = rb.readChunk(dst)
	require.True(t, ok)
	assert.Equal(t, []byte{2, 3, 4, 5}, dst[:n])
}

func TestAudioImpl_ringBuffer_writeLargerThanBuffer(t *testing.T) {
	var rb = newAudioRingBuffer(4)
	var dst = make([]byte, 8)

	require.True(t, rb.write([]byte{9}))
	assert.False(t, rb.write([]byte{1, 2, 3, 4, 5, 6}))
	assert.True(t, rb.checkOverflow())

	var n, ok = rb.readChunk(dst)
	require.True(t, ok)
	assert.Equal(t, []byte{3, 4, 5, 6}, dst[:n], "only the most recent bytes are kept")
}

func TestAudioImpl_ringBuffer_close(t *testing.T) {
	var rb = newAudioRingBuffer(4)
	var dst = make([]byte, 4)

	require.True(t, rb.write([]byte{7}))
	rb.close()

	assert.False(t, rb.write([]byte{8}), "writes after close are refused")

	// What was buffered before the close is still delivered...
	var n, ok = rb.readChunk(dst)
	require.True(t, ok)
	assert.Equal(t, []byte{7}, dst[:n])

	// ...and then the reader is told the stream has ended.
	n, ok = rb.readChunk(dst)
	assert.False(t, ok)
	assert.Equal(t, 0, n)
}

func TestAudioImpl_ringBuffer_closeWakesBlockedReader(t *testing.T) {
	var rb = newAudioRingBuffer(4)
	var done = make(chan bool)

	go func() {
		var _, ok = rb.readChunk(make([]byte, 4))
		done <- ok
	}()

	rb.close()

	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("readChunk did not return after close")
	}
}

// --- GetByte from a soundcard's ring buffer ---

// setupSoundcardAdev0 returns audio devices with a device 0 that reads from a
// ring buffer, as AudioOpen sets up for a soundcard, without any PortAudio
// stream feeding it.
func setupSoundcardAdev0() (*AudioDevices, *adev_s) {
	var d, dev = setupAdev0()
	dev.numChannels = 1
	dev.bytesPerFrame = 2
	dev.g_audio_in_type = AUDIO_IN_TYPE_SOUNDCARD
	dev.inbufSizeInBytes = 256
	dev.inbuf = make([]byte, dev.inbufSizeInBytes)
	dev.inputRingBuf = newAudioRingBuffer(16)

	return d, dev
}

func TestAudioImpl_audioGet_soundcard(t *testing.T) {
	var d, dev = setupSoundcardAdev0()

	require.True(t, dev.inputRingBuf.write([]byte{10, 20, 30, 40}))

	var src SampleSource = d

	assert.Equal(t, 10, src.GetByte(0))
	assert.Equal(t, 20, d.GetByte(0))
	assert.Equal(t, 30, d.GetByte(0))
	assert.Equal(t, 40, d.GetByte(0))

	// Once the ring buffer is closed and drained, the stream has ended.
	dev.inputRingBuf.close()
	assert.Equal(t, -1, d.GetByte(0))
}

func TestAudioImpl_audioGet_soundcardOverflow(t *testing.T) {
	var d, dev = setupSoundcardAdev0()

	// More than the ring buffer holds: the oldest bytes are lost, and
	// GetByte reports that, but carries on with what is left.
	dev.inputRingBuf.write(make([]byte, 12))
	dev.inputRingBuf.write([]byte{1, 2, 3, 4, 5, 6, 7, 8})

	assert.Equal(t, 0, d.GetByte(0))
	assert.False(t, dev.inputRingBuf.checkOverflow(), "GetByte consumed the overflow")
}

// --- GetByte from standard input ---

// setStdin replaces os.Stdin with the read end of a pipe, restoring it on
// cleanup, and returns the write end.
func setStdin(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	var r, w, err = os.Pipe()
	require.NoError(t, err)

	var prev = os.Stdin

	t.Cleanup(func() {
		os.Stdin = prev

		r.Close()
		w.Close()
	})

	os.Stdin = r

	return r, w
}

func TestAudioImpl_audioOpen_stdin_audioGet(t *testing.T) {
	var r, w = setStdin(t)

	var pa = makeAudioConfig("-", "-")
	pa.adev[0].num_channels = 2

	var d = openAudio(t, pa)
	assert.Equal(t, "stdin", pa.adev[0].adevice_in, `"-" is renamed for readability`)
	assert.Equal(t, AUDIO_IN_TYPE_STDIN, d.dev[0].g_audio_in_type)
	assert.Equal(t, 1024, d.dev[0].inbufSizeInBytes)

	// Defaults are filled in for everything that wasn't given.
	assert.Equal(t, DEFAULT_SAMPLES_PER_SEC, pa.adev[0].samples_per_sec)
	assert.Equal(t, DEFAULT_BITS_PER_SAMPLE, pa.adev[0].bits_per_sample)
	assert.Equal(t, DEFAULT_BAUD, pa.achan[0].baud)

	var _, err = w.Write([]byte{0x11, 0x22, 0x33, 0x44})
	require.NoError(t, err)

	assert.Equal(t, 0x11, d.GetByte(0))
	assert.Equal(t, 0x22, d.GetByte(0))
	assert.Equal(t, 0x33, d.GetByte(0))
	assert.Equal(t, 0x44, d.GetByte(0))

	// A read error other than end of file is reported, not fatal.
	require.NoError(t, r.Close())
	assert.Equal(t, -1, d.GetByte(0))
}

// --- UDP input ---

func TestAudioImpl_audioOpen_udpInput_audioGet(t *testing.T) {
	// Port 0: let the system pick a free one.  The same name on the output
	// side is the listening port copied over, not somewhere to transmit.
	var pa = makeAudioConfig("udp:0", "udp:0")

	var d = openAudio(t, pa)
	require.NotNil(t, d.dev[0].udp_sock)
	assert.Equal(t, AUDIO_IN_TYPE_SDR_UDP, d.dev[0].g_audio_in_type)
	assert.Equal(t, SDR_UDP_BUF_MAXLEN, d.dev[0].inbufSizeInBytes)
	assert.Nil(t, d.dev[0].udp_out_sock)
	assert.False(t, d.transmitAvailable(0))

	var addr, isUDP = d.dev[0].udp_sock.LocalAddr().(*net.UDPAddr)
	require.True(t, isUDP)

	var port = addr.Port

	var conn, err = new(net.Dialer).DialContext(t.Context(), "udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)

	defer conn.Close()

	_, err = conn.Write([]byte{0x01, 0x02, 0x03, 0x04})
	require.NoError(t, err)

	require.NoError(t, d.dev[0].udp_sock.SetReadDeadline(time.Now().Add(2*time.Second)))

	assert.Equal(t, 0x01, d.GetByte(0))
	assert.Equal(t, 0x02, d.GetByte(0))
	assert.Equal(t, 0x03, d.GetByte(0))
	assert.Equal(t, 0x04, d.GetByte(0))

	// Nothing more arrives: the read fails, and GetByte says so.
	require.NoError(t, d.dev[0].udp_sock.SetReadDeadline(time.Now().Add(time.Millisecond)))
	assert.Equal(t, -1, d.GetByte(0))

	d.Close()
	assert.Nil(t, d.dev[0].udp_sock)
	assert.Nil(t, d.dev[0].inbuf)
}

func TestAudioImpl_audioOpen_udpInput_badAddress(t *testing.T) {
	var pa = makeAudioConfig("udp:Q1TEST", "stdin")

	var _, openErr = AudioOpen(t.Context(), pa)
	assert.Error(t, openErr)
}

func TestAudioImpl_audioOpen_udpInput_portInUse(t *testing.T) {
	var busy, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", ":0")
	require.NoError(t, err)

	defer busy.Close()

	var addr, isUDP = busy.LocalAddr().(*net.UDPAddr)
	require.True(t, isUDP)

	var port = addr.Port

	var pa = makeAudioConfig("udp:"+strconv.Itoa(port), "stdin")

	var _, openErr = AudioOpen(t.Context(), pa)
	assert.Error(t, openErr)
}

// --- UDP output ---

// Opening a UDP output with AudioOpen also starts udpSilenceKeepalive, whose
// silence would arrive in among what this test sends.  Put together the
// device AudioOpen would have instead, less the goroutine.
func TestAudioImpl_udpOutput_putFlushWaitClose(t *testing.T) {
	var d = new(AudioDevices)

	var listener, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(t.Context(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	d.dev[0] = new(adev_s)
	d.dev[0].udp_out_sock = conn
	d.dev[0].outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	d.dev[0].outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	d.dev[0].silenceStopCh = make(chan struct{})

	var stop = d.dev[0].silenceStopCh

	assert.True(t, d.transmitAvailable(0))

	var sink AudioSink = d

	assert.Equal(t, 0, sink.Put(0, 0xAB))
	assert.Equal(t, 0, sink.Put(0, 0xCD))

	// wait flushes what is buffered, and that is all it has to do
	// for UDP.
	d.wait(0)
	assert.Equal(t, 0, d.dev[0].outbufLen)

	var buf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(2*time.Second)))

	var n int
	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, []byte{0xAB, 0xCD}, buf[:n])

	// A full buffer is sent without waiting for a flush.
	for range UDP_AUDIO_OUT_BUF_MAXLEN {
		require.GreaterOrEqual(t, d.Put(0, 0x55), 0)
	}

	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, UDP_AUDIO_OUT_BUF_MAXLEN, n)

	d.Close()
	assert.Nil(t, d.dev[0].udp_out_sock)
	assert.Nil(t, d.dev[0].silenceStopCh)
	assert.Nil(t, d.dev[0].outbuf)

	select {
	case <-stop:
	default:
		t.Error("Close did not tell the silence keepalive to stop")
	}
	assert.False(t, d.transmitAvailable(0))
}

// A UDP output that can't be dialled leaves a defaulted output receive-only,
// but is fatal when it was named for transmit.
func TestAudioImpl_audioOpen_udpOutput_dialFails(t *testing.T) {
	for _, specified := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaulted", true: "specified"}[specified], func(t *testing.T) {
			// No port, so there is nothing to dial.
			var pa = makeAudioConfig("stdin", "udp:Q1TEST")
			pa.adev[0].adevice_out_specified = specified

			if specified {
				var _, openErr = AudioOpen(t.Context(), pa)
				assert.Error(t, openErr)

				return
			}

			var d = openAudio(t, pa)
			assert.Nil(t, d.dev[0].udp_out_sock)
			assert.False(t, d.transmitAvailable(0))
		})
	}
}

// A soundcard input that doesn't exist - or no PortAudio to find it with -
// is fatal, as there is then nothing to receive from.
func TestAudioImpl_audioOpen_missingInputDevice_isFatal(t *testing.T) {
	var pa = makeAudioConfig(noSuchAudioDevice, noSuchAudioDevice)

	var d, err = AudioOpen(t.Context(), pa)
	require.Error(t, err)
	assert.Nil(t, d)
}

// Close with nothing open has nothing to do.
func TestAudioImpl_audioClose_nothingOpen(t *testing.T) {
	assert.NotPanics(t, new(AudioDevices).Close)
}
