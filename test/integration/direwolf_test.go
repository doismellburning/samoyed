// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	kissFrameEnd byte = 0xC0
	kissDataCmd  byte = 0x00
)

// encodeCallsign encodes a callsign into 7 AX.25 address bytes.
// Each of the 6 ASCII chars is shifted left by 1 bit. The SSID byte is
// 0x60 for non-terminal addresses, 0xE0 for the last address field.
func encodeCallsign(call string, last bool) []byte {
	var addr [7]byte
	var padded = fmt.Sprintf("%-6s", call)
	for i := 0; i < 6; i++ {
		addr[i] = padded[i] << 1
	}
	if last {
		addr[6] = 0xE0
	} else {
		addr[6] = 0x60
	}
	return addr[:]
}

// buildUIFrame builds a minimal AX.25 UI frame with two address fields
// (destination then source, no digipeaters), control byte 0x03 and PID 0xF0.
func buildUIFrame(dest, src string, info []byte) []byte {
	var buf bytes.Buffer
	buf.Write(encodeCallsign(dest, false))
	buf.Write(encodeCallsign(src, true))
	buf.WriteByte(0x03) // UI frame control
	buf.WriteByte(0xF0) // PID: no layer 3
	buf.Write(info)
	return buf.Bytes()
}

// kissWrap wraps raw AX.25 bytes in a KISS data frame (channel 0) with
// byte-stuffing for FEND (0xC0) and FESC (0xDB) within the payload.
func kissWrap(ax25Frame []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(kissFrameEnd)
	buf.WriteByte(kissDataCmd)
	for _, b := range ax25Frame {
		switch b {
		case 0xC0:
			buf.WriteByte(0xDB)
			buf.WriteByte(0xDC)
		case 0xDB:
			buf.WriteByte(0xDB)
			buf.WriteByte(0xDD)
		default:
			buf.WriteByte(b)
		}
	}
	buf.WriteByte(kissFrameEnd)
	return buf.Bytes()
}

// readKISSFrame reads from conn until a complete FEND…FEND KISS frame is
// received. Returns the full frame bytes including both delimiters.
func readKISSFrame(conn net.Conn, timeout time.Duration) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	var frame []byte
	var inFrame bool
	var oneByte [1]byte
	for {
		var _, err = conn.Read(oneByte[:])
		if err != nil {
			return nil, err
		}
		var b = oneByte[0]
		if b == kissFrameEnd {
			if !inFrame {
				inFrame = true
				frame = []byte{kissFrameEnd}
			} else {
				frame = append(frame, kissFrameEnd)
				return frame, nil
			}
		} else if inFrame {
			frame = append(frame, b)
		}
	}
}

// writeTempConfig writes content to a temporary file and returns its path.
// The caller is responsible for removing it.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	var f, err = os.CreateTemp("", "direwolf-*.conf")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestDirewolfUDPAudioRelay(t *testing.T) {
	var ctx = context.Background()

	// Create a shared Docker network so the two containers can reach each
	// other by alias (direwolf-a / direwolf-b).
	var net_, err = network.New(ctx, network.WithCheckDuplicate())
	require.NoError(t, err)
	t.Cleanup(func() { _ = net_.Remove(ctx) })

	var netName = net_.Name

	// Config for each instance.
	// Container A: receive UDP on 7355, send audio to direwolf-b:7356.
	// Container B: receive UDP on 7356, send audio to direwolf-a:7355.
	var configA = `MYCALL Q1TEST
ADEVICE udp:7355 udp:direwolf-b:7356
MODEM 1200
KISSPORT 8001
`
	var configB = `MYCALL Q2TEST
ADEVICE udp:7356 udp:direwolf-a:7355
MODEM 1200
KISSPORT 8001
`

	var pathA = writeTempConfig(t, configA)
	t.Cleanup(func() { _ = os.Remove(pathA) })

	var pathB = writeTempConfig(t, configB)
	t.Cleanup(func() { _ = os.Remove(pathB) })

	// Both containers need to start concurrently: each one's config references
	// the other's Docker-network alias for UDP audio output, so neither can
	// fully initialise until both aliases are registered in the network's DNS.
	// Starting them in parallel goroutines lets both aliases appear in DNS
	// within a short window; the retry wrapper in the image handles the brief
	// race at startup.
	type result struct {
		ctr testcontainers.Container
		err error
	}

	var chanA = make(chan result, 1)
	var chanB = make(chan result, 1)

	go func() {
		var ctr, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:  "samoyed-direwolf:integration-test",
				Cmd:    []string{"-c", "/etc/direwolf.conf"},
				Networks: []string{netName},
				NetworkAliases: map[string][]string{
					netName: {"direwolf-a"},
				},
				ExposedPorts: []string{"8001/tcp"},
				Files: []testcontainers.ContainerFile{
					{
						HostFilePath:      pathA,
						ContainerFilePath: "/etc/direwolf.conf",
						FileMode:          0644,
					},
				},
				WaitingFor: wait.ForListeningPort("8001/tcp").WithStartupTimeout(60 * time.Second),
			},
			Started: true,
		})
		chanA <- result{ctr, err}
	}()

	go func() {
		var ctr, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:  "samoyed-direwolf:integration-test",
				Cmd:    []string{"-c", "/etc/direwolf.conf"},
				Networks: []string{netName},
				NetworkAliases: map[string][]string{
					netName: {"direwolf-b"},
				},
				ExposedPorts: []string{"8001/tcp"},
				Files: []testcontainers.ContainerFile{
					{
						HostFilePath:      pathB,
						ContainerFilePath: "/etc/direwolf.conf",
						FileMode:          0644,
					},
				},
				WaitingFor: wait.ForListeningPort("8001/tcp").WithStartupTimeout(60 * time.Second),
			},
			Started: true,
		})
		chanB <- result{ctr, err}
	}()

	var resA = <-chanA
	var resB = <-chanB

	require.NoError(t, resA.err)
	var ctrA = resA.ctr
	t.Cleanup(func() { _ = ctrA.Terminate(ctx) })

	require.NoError(t, resB.err)
	var ctrB = resB.ctr
	t.Cleanup(func() { _ = ctrB.Terminate(ctx) })

	// Resolve mapped host:port for each container's KISS TCP server.
	var hostA, portErrA = ctrA.Host(ctx)
	require.NoError(t, portErrA)
	var mappedA, mapErrA = ctrA.MappedPort(ctx, "8001/tcp")
	require.NoError(t, mapErrA)

	var hostB, portErrB = ctrB.Host(ctx)
	require.NoError(t, portErrB)
	var mappedB, mapErrB = ctrB.MappedPort(ctx, "8001/tcp")
	require.NoError(t, mapErrB)

	// Connect KISS TCP clients.
	var connA, dialErrA = net.DialTimeout("tcp", net.JoinHostPort(hostA, mappedA.Port()), 5*time.Second)
	require.NoError(t, dialErrA)
	t.Cleanup(func() { _ = connA.Close() })

	var connB, dialErrB = net.DialTimeout("tcp", net.JoinHostPort(hostB, mappedB.Port()), 5*time.Second)
	require.NoError(t, dialErrB)
	t.Cleanup(func() { _ = connB.Close() })

	// Build and transmit a UI frame via Container A.
	// Destination Q2TEST, source Q1TEST, info "Hello".
	var ax25Frame = buildUIFrame("Q2TEST", "Q1TEST", []byte("Hello"))
	var kissFrame = kissWrap(ax25Frame)

	var _, writeErr = connA.Write(kissFrame)
	require.NoError(t, writeErr)

	// Wait for the decoded frame to appear on Container B's KISS port.
	// 1200-baud AFSK encoding/decoding takes up to a few seconds.
	var received, readErr = readKISSFrame(connB, 15*time.Second)
	require.NoError(t, readErr)

	// received: FEND + cmd byte + AX.25 bytes + FEND
	// Strip delimiters and command byte to get the raw AX.25 frame.
	require.Greater(t, len(received), 3, "KISS frame too short")
	var receivedAX25 = received[2 : len(received)-1]

	// The first 7 bytes of the AX.25 frame must be the destination address.
	var expectedDest = encodeCallsign("Q2TEST", false)
	require.True(t, bytes.HasPrefix(receivedAX25, expectedDest),
		"received AX.25 frame does not start with expected destination Q2TEST")
}
