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

// --- audio_get from a soundcard's ring buffer ---

// setupSoundcardAdev0 installs a device 0 that reads from a ring buffer, as
// AudioOpen sets up for a soundcard, without any PortAudio stream feeding it.
func setupSoundcardAdev0(t *testing.T) *adev_s {
	t.Helper()

	var dev = setupAdev0(t)
	dev.numChannels = 1
	dev.bytesPerFrame = 2
	dev.g_audio_in_type = AUDIO_IN_TYPE_SOUNDCARD
	dev.inbufSizeInBytes = 256
	dev.inbuf = make([]byte, dev.inbufSizeInBytes)
	dev.inputRingBuf = newAudioRingBuffer(16)

	return dev
}

func TestAudioImpl_audioGet_soundcard(t *testing.T) {
	var dev = setupSoundcardAdev0(t)

	require.True(t, dev.inputRingBuf.write([]byte{10, 20, 30, 40}))

	var src audioDeviceSource

	assert.Equal(t, 10, src.GetByte(0))
	assert.Equal(t, 20, audio_get(0))
	assert.Equal(t, 30, audio_get(0))
	assert.Equal(t, 40, audio_get(0))

	// Once the ring buffer is closed and drained, the stream has ended.
	dev.inputRingBuf.close()
	assert.Equal(t, -1, audio_get(0))
}

func TestAudioImpl_audioGet_soundcardOverflow(t *testing.T) {
	var dev = setupSoundcardAdev0(t)

	// More than the ring buffer holds: the oldest bytes are lost, and
	// audio_get reports that, but carries on with what is left.
	dev.inputRingBuf.write(make([]byte, 12))
	dev.inputRingBuf.write([]byte{1, 2, 3, 4, 5, 6, 7, 8})

	assert.Equal(t, 0, audio_get(0))
	assert.False(t, dev.inputRingBuf.checkOverflow(), "audio_get consumed the overflow")
}

// --- audio_get from standard input ---

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
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		AudioClose()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var r, w = setStdin(t)

	var pa = makeAudioConfig("-", "-")
	pa.adev[0].num_channels = 2

	require.Equal(t, 0, AudioOpen(t.Context(), pa))
	assert.Equal(t, "stdin", pa.adev[0].adevice_in, `"-" is renamed for readability`)
	assert.Equal(t, AUDIO_IN_TYPE_STDIN, adev[0].g_audio_in_type)
	assert.Equal(t, 1024, adev[0].inbufSizeInBytes)

	// Defaults are filled in for everything that wasn't given.
	assert.Equal(t, DEFAULT_SAMPLES_PER_SEC, pa.adev[0].samples_per_sec)
	assert.Equal(t, DEFAULT_BITS_PER_SAMPLE, pa.adev[0].bits_per_sample)
	assert.Equal(t, DEFAULT_BAUD, pa.achan[0].baud)

	var _, err = w.Write([]byte{0x11, 0x22, 0x33, 0x44})
	require.NoError(t, err)

	assert.Equal(t, 0x11, audio_get(0))
	assert.Equal(t, 0x22, audio_get(0))
	assert.Equal(t, 0x33, audio_get(0))
	assert.Equal(t, 0x44, audio_get(0))

	// A read error other than end of file is reported, not fatal.
	require.NoError(t, r.Close())
	assert.Equal(t, -1, audio_get(0))
}

// --- UDP input ---

func TestAudioImpl_audioOpen_udpInput_audioGet(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		AudioClose()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	// Port 0: let the system pick a free one.  The same name on the output
	// side is the listening port copied over, not somewhere to transmit.
	var pa = makeAudioConfig("udp:0", "udp:0")

	require.Equal(t, 0, AudioOpen(t.Context(), pa))
	require.NotNil(t, adev[0].udp_sock)
	assert.Equal(t, AUDIO_IN_TYPE_SDR_UDP, adev[0].g_audio_in_type)
	assert.Equal(t, SDR_UDP_BUF_MAXLEN, adev[0].inbufSizeInBytes)
	assert.Nil(t, adev[0].udp_out_sock)
	assert.False(t, audio_transmit_available(0))

	var addr, isUDP = adev[0].udp_sock.LocalAddr().(*net.UDPAddr)
	require.True(t, isUDP)

	var port = addr.Port

	var conn, err = new(net.Dialer).DialContext(t.Context(), "udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)

	defer conn.Close()

	_, err = conn.Write([]byte{0x01, 0x02, 0x03, 0x04})
	require.NoError(t, err)

	require.NoError(t, adev[0].udp_sock.SetReadDeadline(time.Now().Add(2*time.Second)))

	assert.Equal(t, 0x01, audio_get(0))
	assert.Equal(t, 0x02, audio_get(0))
	assert.Equal(t, 0x03, audio_get(0))
	assert.Equal(t, 0x04, audio_get(0))

	// Nothing more arrives: the read fails, and audio_get says so.
	require.NoError(t, adev[0].udp_sock.SetReadDeadline(time.Now().Add(time.Millisecond)))
	assert.Equal(t, -1, audio_get(0))

	AudioClose()
	assert.Nil(t, adev[0].udp_sock)
	assert.Nil(t, adev[0].inbuf)
}

func TestAudioImpl_audioOpen_udpInput_badAddress(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		AudioClose()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var pa = makeAudioConfig("udp:Q1TEST", "stdin")

	assert.Equal(t, -1, AudioOpen(t.Context(), pa))
}

func TestAudioImpl_audioOpen_udpInput_portInUse(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		AudioClose()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var busy, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", ":0")
	require.NoError(t, err)

	defer busy.Close()

	var addr, isUDP = busy.LocalAddr().(*net.UDPAddr)
	require.True(t, isUDP)

	var port = addr.Port

	var pa = makeAudioConfig("udp:"+strconv.Itoa(port), "stdin")

	assert.Equal(t, -1, AudioOpen(t.Context(), pa))
}

// --- UDP output ---

// Opening a UDP output with AudioOpen also starts audioUDPSilenceKeepalive,
// which AudioClose tells to stop but does not wait for, so a test could not
// then restore the globals it reads without racing it.  Put together the
// device AudioOpen would have instead, less the goroutine.
func TestAudioImpl_udpOutput_putFlushWaitClose(t *testing.T) {
	var prevAdev = adev
	var prevXmitSvc = xmitSvc

	t.Cleanup(func() {
		adev = prevAdev
		xmitSvc = prevXmitSvc
	})

	adev = [MAX_ADEVS]*adev_s{}
	xmitSvc = new(XmitService)

	var listener, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(t.Context(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	adev[0] = new(adev_s)
	adev[0].udp_out_sock = conn
	adev[0].outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	adev[0].outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	adev[0].silenceStopCh = make(chan struct{})

	var stop = adev[0].silenceStopCh

	assert.True(t, audio_transmit_available(0))

	var sink AudioDeviceSink

	assert.Equal(t, 0, sink.Put(0, 0xAB))
	assert.Equal(t, 0, sink.Put(0, 0xCD))

	// audio_wait flushes what is buffered, and that is all it has to do
	// for UDP.
	audio_wait(0)
	assert.Equal(t, 0, adev[0].outbufLen)

	var buf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(2*time.Second)))

	var n int
	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, []byte{0xAB, 0xCD}, buf[:n])

	// A full buffer is sent without waiting for a flush.
	for range UDP_AUDIO_OUT_BUF_MAXLEN {
		require.GreaterOrEqual(t, audio_put(0, 0x55), 0)
	}

	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, UDP_AUDIO_OUT_BUF_MAXLEN, n)

	assert.Equal(t, 0, AudioClose())
	assert.Nil(t, adev[0].udp_out_sock)
	assert.Nil(t, adev[0].silenceStopCh)
	assert.Nil(t, adev[0].outbuf)

	select {
	case <-stop:
	default:
		t.Error("AudioClose did not tell the silence keepalive to stop")
	}
	assert.False(t, audio_transmit_available(0))
}

// A UDP output that can't be dialled leaves a defaulted output receive-only,
// but is fatal when it was named for transmit.
func TestAudioImpl_audioOpen_udpOutput_dialFails(t *testing.T) {
	for _, specified := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaulted", true: "specified"}[specified], func(t *testing.T) {
			var prevAdev = adev
			var prevConfig = save_audio_config_p

			t.Cleanup(func() {
				AudioClose()

				adev = prevAdev
				save_audio_config_p = prevConfig
			})

			// No port, so there is nothing to dial.
			var pa = makeAudioConfig("stdin", "udp:Q1TEST")
			pa.adev[0].adevice_out_specified = specified

			if specified {
				assert.Equal(t, -1, AudioOpen(t.Context(), pa))

				return
			}

			require.Equal(t, 0, AudioOpen(t.Context(), pa))
			assert.Nil(t, adev[0].udp_out_sock)
			assert.False(t, audio_transmit_available(0))
		})
	}
}

// A soundcard input that doesn't exist - or no PortAudio to find it with -
// is fatal, as there is then nothing to receive from.
func TestAudioImpl_audioOpen_missingInputDevice_isFatal(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		AudioClose()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var refsBefore = portaudioRefCount

	var pa = makeAudioConfig(noSuchAudioDevice, noSuchAudioDevice)

	assert.Equal(t, -1, AudioOpen(t.Context(), pa))

	// The failed open gives back the PortAudio reference it took.
	assert.Equal(t, refsBefore, portaudioRefCount)
}

// AudioClose with nothing open has nothing to do.
func TestAudioImpl_audioClose_nothingOpen(t *testing.T) {
	var prevAdev = adev

	t.Cleanup(func() { adev = prevAdev })

	adev = [MAX_ADEVS]*adev_s{}

	assert.Equal(t, 0, AudioClose())
}
