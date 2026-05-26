// Package agwlib provides a client for the AGW (AGWPE) TCP/IP API used to
// communicate with software TNCs such as samoyed-direwolf.
//
// Create a Client with New, then call its methods to send commands.
// Responses from the TNC are delivered via the Handlers callbacks, which
// run on an internal goroutine.
package agwlib

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AX25_MAX_INFO_LEN is the maximum information field length in an AX.25 frame.
const AX25_MAX_INFO_LEN = 2048

// MAX_TOTAL_CHANS is the maximum number of radio channels supported.
const MAX_TOTAL_CHANS = 16

// Callsign is a fixed-width AX.25 callsign field (10 bytes, NUL-padded).
// It is a type alias so that [10]byte literals can be used without conversion.
type Callsign = [10]byte

// Handlers holds optional callback functions invoked when the TNC sends a
// response.  Nil fields are silently ignored.
type Handlers struct {
	// OnConnectionReceived is called when an AX.25 connection changes state.
	// incoming is true when the remote station initiated the connect request,
	// false when the local Connect call was accepted by the remote station.
	OnConnectionReceived func(channel byte, callFrom, callTo Callsign, incoming bool, data []byte)
	// OnConnectedData is called with each I-frame payload received over an
	// established connection.
	OnConnectedData func(channel byte, callFrom, callTo Callsign, data []byte)
	// OnDisconnected is called when an AX.25 connection is terminated.
	OnDisconnected func(channel byte, callFrom, callTo Callsign, data []byte)
	// OnPortInformation is called with the list of radio channels available
	// on the TNC (response to AskPortInformation).
	OnPortInformation func(numChan int, descriptions []string)
	// OnOutstandingFrames is called with the transmit-queue depth for a
	// station (response to OutstandingFrames).
	OnOutstandingFrames func(channel byte, callFrom, callTo Callsign, frameCount int)
}

// Client is a connected AGW client.  Use New to create one.
type Client struct {
	host     string
	port     string
	initFunc func() error
	handlers Handlers
	mu       sync.Mutex
	sock     net.Conn // nil while disconnected
}

// agwpeHeader is the 36-byte fixed-size header that precedes every AGW
// message.  All fields are exported so encoding/binary can serialise them.
type agwpeHeader struct {
	Portx        byte
	Reserved1    byte
	Reserved2    byte
	Reserved3    byte
	DataKind     byte
	Reserved4    byte
	PID          byte
	Reserved5    byte
	CallFrom     Callsign
	CallTo       Callsign
	DataLen      uint32
	UserReserved [4]byte
}

type agwpeCommand struct {
	header *agwpeHeader
	data   []byte
}

// New creates a Client, connects to the TNC at host:port, and starts the
// receive loop.  initFunc is stored for re-execution after a reconnect;
// callers are responsible for performing any first-time initialisation
// (e.g. AskPortInformation) explicitly after New returns.
func New(host, port string, initFunc func() error, h Handlers) (*Client, error) {
	var c = new(Client)
	c.host = host
	c.port = port
	c.initFunc = initFunc
	c.handlers = h

	var sock, err = net.Dial("tcp4", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}

	c.sock = sock
	go c.listenLoop()
	return c, nil
}

// RegisterCallsign tells the TNC to accept incoming AX.25 connect requests
// addressed to call on the given channel.
func (c *Client) RegisterCallsign(channel byte, call Callsign) error {
	var h = new(agwpeHeader)
	h.Portx = channel
	h.DataKind = 'X'
	h.CallFrom = call
	return c.writeHeader(h)
}

// UnregisterCallsign tells the TNC to stop accepting connect requests for call.
func (c *Client) UnregisterCallsign(channel byte, call Callsign) error {
	var h = new(agwpeHeader)
	h.Portx = channel
	h.DataKind = 'x'
	h.CallFrom = call
	return c.writeHeader(h)
}

// AskPortInformation requests the list of radio channels from the TNC.
// The response is delivered via Handlers.OnPortInformation.
func (c *Client) AskPortInformation() error {
	var h = new(agwpeHeader)
	h.DataKind = 'G'
	return c.writeHeader(h)
}

// Connect initiates an AX.25 connection to to on the given channel.
// The outcome is delivered via Handlers.OnConnectionReceived.
func (c *Client) Connect(channel byte, from, to Callsign) error {
	var h = new(agwpeHeader)
	h.Portx = channel
	h.DataKind = 'C'
	h.PID = 0xF0
	h.CallFrom = from
	h.CallTo = to
	return c.writeHeader(h)
}

// ConnectVia initiates an AX.25 connection through up to seven digipeaters.
// If via is empty, it falls back to a plain Connect.
// The outcome is delivered via Handlers.OnConnectionReceived.
func (c *Client) ConnectVia(channel byte, from, to Callsign, via []Callsign) error {
	if len(via) == 0 {
		return c.Connect(channel, from, to)
	}

	if len(via) > 7 {
		via = via[:7]
	}

	// AGW 'v' payload: 1-byte count followed by 10 bytes per digipeater.
	var payload = make([]byte, 1+10*len(via))
	payload[0] = byte(len(via))

	for i, digi := range via {
		copy(payload[1+10*i:], digi[:])
	}

	var h = new(agwpeHeader)
	h.Portx = channel
	h.DataKind = 'v'
	h.CallFrom = from
	h.CallTo = to
	h.DataLen = uint32(len(payload))
	return c.writeHeaderAndData(h, payload)
}

// Disconnect terminates an established AX.25 connection.
// Completion is signalled via Handlers.OnDisconnected.
func (c *Client) Disconnect(channel byte, from, to Callsign) error {
	var h = new(agwpeHeader)
	h.Portx = channel
	h.DataKind = 'd'
	h.CallFrom = from
	h.CallTo = to
	return c.writeHeader(h)
}

// SendConnectedData sends data over an established AX.25 connection.
// pid should normally be 0xF0 for standard AX.25 I-frames.
func (c *Client) SendConnectedData(channel byte, pid byte, from, to Callsign, data []byte) error {
	var h = new(agwpeHeader)
	h.Portx = channel
	h.DataKind = 'D'
	h.PID = pid
	h.CallFrom = from
	h.CallTo = to
	h.DataLen = uint32(len(data))
	return c.writeHeaderAndData(h, data)
}

// OutstandingFrames requests the transmit-queue depth for the given station.
// The answer is delivered via Handlers.OnOutstandingFrames.
func (c *Client) OutstandingFrames(channel byte, from, to Callsign) error {
	var h = new(agwpeHeader)
	h.Portx = channel
	h.DataKind = 'Y'
	h.CallFrom = from
	h.CallTo = to
	return c.writeHeader(h)
}

// writeHeader sends a header-only AGW command (DataLen == 0).
func (c *Client) writeHeader(h *agwpeHeader) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sock == nil {
		return fmt.Errorf("not connected to TNC")
	}

	return binary.Write(c.sock, binary.LittleEndian, h)
}

// writeHeaderAndData sends an AGW header followed by a variable-length payload.
func (c *Client) writeHeaderAndData(h *agwpeHeader, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sock == nil {
		return fmt.Errorf("not connected to TNC")
	}

	var headerErr = binary.Write(c.sock, binary.LittleEndian, h)
	if headerErr != nil {
		return headerErr
	}

	var _, dataErr = c.sock.Write(data)
	return dataErr
}

// listenLoop reads AGW messages from the TNC and dispatches them to the
// Handlers callbacks.  On connection loss it attempts to reconnect and
// re-runs initFunc (if set) to re-register any application state.
func (c *Client) listenLoop() {
	for {
		c.mu.Lock()
		var sock = c.sock
		c.mu.Unlock()

		if sock == nil {
			fmt.Printf("Attempting to reattach to network TNC...\n")

			var newSock, dialErr = net.Dial("tcp4", net.JoinHostPort(c.host, c.port))
			if dialErr == nil {
				fmt.Printf("Successfully reattached to network TNC.\n")

				c.mu.Lock()
				c.sock = newSock
				c.mu.Unlock()

				if c.initFunc != nil {
					c.initFunc() //nolint:errcheck
				}
			}

			time.Sleep(5 * time.Second)
			continue
		}

		var header = new(agwpeHeader)
		var readErr = binary.Read(sock, binary.LittleEndian, header)

		if readErr != nil {
			fmt.Printf("Error communicating with network TNC, will try to reattach: %s\n", readErr)

			c.mu.Lock()
			if c.sock != nil {
				c.sock.Close() //nolint:errcheck
				c.sock = nil
			}
			c.mu.Unlock()

			continue
		}

		if int(header.Portx) >= MAX_TOTAL_CHANS {
			fmt.Printf("Invalid channel number %d in command '%c' from network TNC.\n", header.Portx, header.DataKind)
			header.Portx = 0
		}

		header.CallFrom[len(header.CallFrom)-1] = 0
		header.CallTo[len(header.CallTo)-1] = 0

		var cmd = new(agwpeCommand)
		cmd.header = header

		if header.DataLen > 0 {
			var rawData = make([]byte, header.DataLen)
			var n, fullReadErr = io.ReadFull(sock, rawData)

			if uint32(n) != header.DataLen || fullReadErr != nil {
				fmt.Printf("Error reading AGW message data: %s (got %d of %d bytes)\n", fullReadErr, n, header.DataLen)

				c.mu.Lock()
				if c.sock != nil {
					c.sock.Close() //nolint:errcheck
					c.sock = nil
				}
				c.mu.Unlock()

				continue
			}

			cmd.data = rawData
		}

		c.dispatch(cmd)
	}
}

// dispatch calls the appropriate Handlers callback for a received AGW command.
func (c *Client) dispatch(cmd *agwpeCommand) {
	switch cmd.header.DataKind {
	case 'C':
		if c.handlers.OnConnectionReceived == nil {
			return
		}

		var data = cmd.data
		var incoming bool

		if len(data) >= 24 && string(data[:24]) == "*** CONNECTED To Station" {
			incoming = true
		} else if len(data) >= 26 && string(data[:26]) == "*** CONNECTED With Station" {
			incoming = false
		} else {
			return
		}

		c.handlers.OnConnectionReceived(cmd.header.Portx, cmd.header.CallFrom, cmd.header.CallTo, incoming, data)

	case 'D':
		if c.handlers.OnConnectedData != nil {
			c.handlers.OnConnectedData(cmd.header.Portx, cmd.header.CallFrom, cmd.header.CallTo, cmd.data)
		}

	case 'd':
		if c.handlers.OnDisconnected != nil {
			c.handlers.OnDisconnected(cmd.header.Portx, cmd.header.CallFrom, cmd.header.CallTo, cmd.data)
		}

	case 'G':
		if c.handlers.OnPortInformation == nil {
			return
		}

		if cmd.data == nil {
			c.handlers.OnPortInformation(0, nil)
			return
		}

		var s = strings.TrimRight(string(cmd.data), ";\x00")
		var parts = strings.Split(s, ";")

		if len(parts) == 0 {
			return
		}

		var numChan, parseErr = strconv.Atoi(strings.TrimSpace(parts[0]))
		if parseErr != nil {
			return
		}

		var raw = parts[1:]
		var descs = make([]string, 0, len(raw))

		for _, d := range raw {
			if d != "" {
				descs = append(descs, d)
			}
		}

		c.handlers.OnPortInformation(numChan, descs)

	case 'Y':
		if c.handlers.OnOutstandingFrames == nil {
			return
		}

		var frameCount, _ = strconv.Atoi(strings.TrimRight(string(cmd.data), "\x00"))
		c.handlers.OnOutstandingFrames(cmd.header.Portx, cmd.header.CallFrom, cmd.header.CallTo, frameCount)

	default:
		// R, g, K, U, y, etc. — not handled.
	}
}
