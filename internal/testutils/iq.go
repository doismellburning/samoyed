// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// FMSignal is audio to FM modulate onto a carrier.
type FMSignal struct {
	// Offset is the carrier's frequency, in Hz from the receiver's centre.
	Offset float64

	// Deviation is how far, in Hz, audio at full scale swings the carrier.
	Deviation float64

	// Audio is the modulating audio, from -1 to 1, at AudioRate samples a
	// second.
	Audio     []float64
	AudioRate int
}

// FMModulate makes sampleRate IQ samples a second of signals, all at once, as
// an RTL-SDR gives them: unsigned 8 bit I and Q, interleaved.  It lasts as
// long as the longest signal.
func FMModulate(sampleRate int, signals ...FMSignal) []byte {
	var length = 0

	for _, s := range signals {
		length = max(length, len(s.Audio)*sampleRate/s.AudioRate)
	}

	var phases = make([]float64, len(signals))
	var iq = make([]byte, 0, 2*length)

	// Each signal gets an equal share of the 8 bits, with room to spare.
	var amplitude = 0.9 / float64(max(1, len(signals)))

	for n := range length {
		var i, q float64

		for k, s := range signals {
			var audio = 0.0
			if a := n * s.AudioRate / sampleRate; a < len(s.Audio) {
				audio = s.Audio[a]
			}

			phases[k] += 2 * math.Pi * (s.Offset + s.Deviation*audio) / float64(sampleRate)
			i += amplitude * math.Cos(phases[k])
			q += amplitude * math.Sin(phases[k])
		}

		iq = append(iq, quantise(i), quantise(q))
	}

	return iq
}

// quantise turns x, from -1 to 1, into an unsigned 8 bit sample.
func quantise(x float64) byte {
	var v = math.Round(127.5 + 127.5*x)

	return byte(int(math.Max(0, math.Min(255, v))) & 0xff)
}

// RTLTCPCommand is a command an RTLTCPServer was sent.
type RTLTCPCommand struct {
	Command byte
	Param   int32
}

// RTLTCPServer is a pretend rtl_tcp server, which sends what it is given to
// the first client to connect, and records the commands that client sends.
type RTLTCPServer struct {
	// Address is the "host:port" to connect to.
	Address string

	// Commands has each command the client sends, in order, and is closed
	// when the client disconnects.
	Commands <-chan RTLTCPCommand
}

// NewRTLTCPServer starts an RTLTCPServer that sends iq, then holds the
// connection open until the client closes it or the test ends.
func NewRTLTCPServer(t *testing.T, iq []byte) *RTLTCPServer {
	t.Helper()

	var ln, port = Listen(t)
	var commands = make(chan RTLTCPCommand, 100)

	// Registered here rather than in the goroutine, which can still be
	// running once the test has finished and can no longer take one.
	var accepted = make(chan net.Conn, 1)

	t.Cleanup(func() {
		select {
		case conn := <-accepted:
			conn.Close()
		default:
		}
	})

	go func() {
		defer close(commands)

		var conn, err = ln.Accept()
		if err != nil {
			return
		}

		accepted <- conn

		// R820T, with 29 gains, as the commonest dongle says.
		var header = append([]byte("RTL0"), 0, 0, 0, 5, 0, 0, 0, 29)

		_, err = conn.Write(header)
		if err != nil {
			// The client has gone, which the test will see for itself.
			return
		}

		go func() {
			_, _ = conn.Write(iq)
		}()

		readRTLTCPCommands(conn, commands)
	}()

	var server = new(RTLTCPServer)
	server.Address = net.JoinHostPort("127.0.0.1", port)
	server.Commands = commands

	return server
}

// readRTLTCPCommands reads the commands from conn onto commands until it
// closes.
func readRTLTCPCommands(conn net.Conn, commands chan<- RTLTCPCommand) {
	for {
		var buf [5]byte

		var _, err = io.ReadFull(conn, buf[:])
		if err != nil {
			return
		}

		var cmd RTLTCPCommand
		cmd.Command = buf[0]

		var parseErr = binary.Read(bytes.NewReader(buf[1:]), binary.BigEndian, &cmd.Param)
		if parseErr != nil {
			return
		}

		commands <- cmd
	}
}

// RequireCommands reads n commands from s, failing the test if they don't come.
func (s *RTLTCPServer) RequireCommands(t *testing.T, n int) []RTLTCPCommand {
	t.Helper()

	var got []RTLTCPCommand

	var timeout = time.After(ProcessTimeout)

	for range n {
		select {
		case cmd, ok := <-s.Commands:
			require.True(t, ok, "rtl_tcp client disconnected after %d commands", len(got))

			got = append(got, cmd)
		case <-timeout:
			require.FailNow(t, "rtl_tcp client stopped sending commands", "after %d", len(got))
		}
	}

	return got
}
