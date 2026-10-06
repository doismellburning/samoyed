// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ubersdr

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ubersdr/ubersdrtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// packet is what a Sink was handed.
type packet struct {
	pcmLE      []byte
	sampleRate int
	channels   int
}

// startClient runs a client against server until the test ends, returning
// what its sink receives.
func startClient(t *testing.T, server *ubersdrtest.Server, query string, sinkErr error) (chan packet, context.CancelFunc, chan struct{}) {
	t.Helper()

	var src, err = ParseSource("ubersdr:" + server.URL + "/?" + query)
	require.NoError(t, err)

	src.minBackoff = 10 * time.Millisecond
	src.maxBackoff = 40 * time.Millisecond

	var packets = make(chan packet, 64)
	var ctx, cancel = context.WithCancel(t.Context())
	var done = make(chan struct{})

	go func() {
		defer close(done)

		Run(ctx, src, "samoyed-test", func(pcmLE []byte, sampleRate, channels int) error {
			packets <- packet{pcmLE, sampleRate, channels}

			return sinkErr
		})
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return packets, cancel, done
}

func receive(t *testing.T, packets chan packet) packet {
	t.Helper()

	select {
	case p := <-packets:
		return p
	case <-time.After(10 * time.Second):
		t.Fatal("no audio arrived")

		return packet{} //nolint:exhaustruct_v5
	}
}

func littleEndian(samples []int16) []byte {
	var b = make([]byte, 0, 2*len(samples))
	for _, s := range samples {
		b = binary.LittleEndian.AppendUint16(b, uint16(s)) //nolint:gosec // G115: reinterpreting the bits is the point
	}

	return b
}

func TestRun_receivesAudio(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var packets, _, _ = startClient(t, server, "frequency=10147600&mode=lsb&bandwidthLow=-2800&bandwidthHigh=-200", nil)

	var conn = server.Accept(t)
	assert.True(t, conn.Registered, "the session was registered at /connection first")
	assert.Equal(t, "10147600", conn.Query.Get("frequency"))
	assert.Equal(t, "lsb", conn.Query.Get("mode"))
	assert.Equal(t, "4", conn.Query.Get("version"))
	assert.Equal(t, "pcm-zstd", conn.Query.Get("format"))
	assert.Equal(t, "-2800", conn.Query.Get("bandwidthLow"))
	assert.Equal(t, "-200", conn.Query.Get("bandwidthHigh"))

	// A text message along the way is logged, not mistaken for audio.
	conn.SendText(t, `{"type":"status","frequency":10147600}`)

	var first = []int16{0, 1000, -1000, 32767, -32768, 12345}
	var second = []int16{7, 6, 5, 4, 3, 2, 1, 0}

	conn.SendAudio(t, first, 12000)
	conn.SendAudio(t, second, 12000)

	var p = receive(t, packets)
	assert.Equal(t, littleEndian(first), p.pcmLE)
	assert.Equal(t, 12000, p.sampleRate)
	assert.Equal(t, 1, p.channels)

	p = receive(t, packets)
	assert.Equal(t, littleEndian(second), p.pcmLE, "the predictor stays in step from one packet to the next")
}

func TestRun_reconnects(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var packets, _, _ = startClient(t, server, "frequency=10147600", nil)

	var first = server.Accept(t)
	first.SendAudio(t, []int16{1, 2, 3}, 12000)
	receive(t, packets)
	first.Close()

	var second = server.Accept(t)
	assert.True(t, second.Registered)
	assert.NotEqual(t, first.Query.Get("user_session_id"), second.Query.Get("user_session_id"), "each connection is a new session")

	// The new connection has a decoder of its own, in step with the new
	// server-side encoder rather than the old one.
	second.SendAudio(t, []int16{4, 5, 6}, 12000)
	assert.Equal(t, littleEndian([]int16{4, 5, 6}), receive(t, packets).pcmLE)
}

func TestRun_refusedThenAllowed(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	server.Refuse("Q1TEST is banned")

	var packets, _, _ = startClient(t, server, "frequency=10147600", nil)

	time.Sleep(100 * time.Millisecond)
	server.Refuse("")

	var conn = server.Accept(t)
	conn.SendAudio(t, []int16{1}, 12000)
	receive(t, packets)
}

func TestRun_sinkErrorDropsTheConnection(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var packets, _, _ = startClient(t, server, "frequency=10147600", errors.New("wrong sample rate"))

	var first = server.Accept(t)
	first.SendAudio(t, []int16{1}, 12000)
	receive(t, packets)
	first.WaitClosed(t)

	server.Accept(t)
}

func TestRun_legacyServerDropsTheConnection(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	startClient(t, server, "frequency=10147600", nil)

	var first = server.Accept(t)
	first.SendBinary(t, []byte{0x28, 0xb5, 0x2f, 0xfd, 0, 0, 0, 0}) // A zstd frame: protocol version 1
	first.WaitClosed(t)

	server.Accept(t)
}

func TestRun_ignoresUndecodablePackets(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var packets, _, _ = startClient(t, server, "frequency=10147600", nil)

	var conn = server.Accept(t)
	conn.SendBinary(t, []byte{1, 2, 3}) // Neither PCM v4 nor zstd: Opus, say
	conn.SendAudio(t, []int16{9, 8, 7}, 12000)

	assert.Equal(t, littleEndian([]int16{9, 8, 7}), receive(t, packets).pcmLE)
}

func TestRun_stopsWhenCancelled(t *testing.T) {
	var server = ubersdrtest.NewServer(t)
	var _, cancel, done = startClient(t, server, "frequency=10147600", nil)

	var conn = server.Accept(t)

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return once cancelled")
	}

	conn.WaitClosed(t)
}

func TestRun_stopsWhenCancelledWhileBackingOff(t *testing.T) {
	var src, err = ParseSource("ubersdr:http://127.0.0.1:1/?frequency=10147600") // Nothing listens on port 1
	require.NoError(t, err)

	src.minBackoff = time.Hour

	var ctx, cancel = context.WithCancel(t.Context())
	var done = make(chan struct{})

	go func() {
		defer close(done)

		Run(ctx, src, "samoyed-test", func([]byte, int, int) error { return nil })
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return once cancelled")
	}
}
