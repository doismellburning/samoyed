// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwclient

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/agwpe"
)

// The frame kinds the client sends or acts on.  The AGWPE API names each
// frame by the ASCII letter in the header's DataKind.
const (
	kindLogin       = 'P' // Application login; the server never replies
	kindVersion     = 'R' // Version request and reply
	kindPorts       = 'G' // Port information request and reply
	kindRegister    = 'X' // Register a callsign, and the reply saying whether it took
	kindConnect     = 'C' // Connect request, and the notice that a link is up
	kindConnectVia  = 'v' // Connect request through digipeaters
	kindData        = 'D' // Connected data, either way
	kindDisconnect  = 'd' // Disconnect request, and the notice that a link is down
	kindOutstanding = 'Y' // Outstanding frames on a link, asked and answered
)

const (
	// DefaultMaxData is the largest data a frame from the server may claim
	// before the client gives up on the connection rather than allocate it.
	// The largest a well-behaved server sends is a little over 2KB of
	// connected data, so this is generous.
	DefaultMaxData = 64 * 1024

	// loginFieldLen is the size of each of the two NUL-padded fields of an
	// application login frame.
	loginFieldLen = 255

	// callsignLen is the size of a header's callsign fields.  The server
	// clears the last byte, so a callsign has one fewer than this to fit in.
	callsignLen = len(agwpe.Callsign{})

	// maxVia is the most digipeaters a connect request can name.
	maxVia = 7
)

// ErrFrameTooLarge says a frame claimed more data than the client accepts.
var ErrFrameTooLarge = errors.New("AGW frame too large")

// readFrame reads one frame from r.  It refuses a frame claiming more than
// maxData bytes of data before allocating anything for it, so a confused or
// hostile server can't make the client allocate gigabytes.
//
// It returns io.EOF only when r ended cleanly between frames.
func readFrame(r io.Reader, maxData uint32) (*agwpe.Message, error) {
	var msg = new(agwpe.Message)

	var headerErr = binary.Read(r, binary.LittleEndian, &msg.Header)
	if headerErr != nil {
		return nil, headerErr
	}

	if msg.Header.DataLen > maxData {
		return nil, fmt.Errorf("%w: %d bytes of %q data, limit %d", ErrFrameTooLarge, msg.Header.DataLen, msg.Header.DataKind, maxData)
	}

	msg.Data = make([]byte, msg.Header.DataLen)

	var _, dataErr = io.ReadFull(r, msg.Data)
	if errors.Is(dataErr, io.EOF) {
		// A header promising data that never came is a truncated frame, not
		// a clean end of stream.
		return nil, io.ErrUnexpectedEOF
	}

	if dataErr != nil {
		return nil, fmt.Errorf("reading %q frame data: %w", msg.Header.DataKind, dataErr)
	}

	return msg, nil
}

// writeFrame writes msg to w in the server's byte order.
func writeFrame(w io.Writer, msg *agwpe.Message) error {
	var _, err = msg.Write(w, binary.LittleEndian)
	if err != nil {
		return fmt.Errorf("writing %q frame: %w", msg.Header.DataKind, err)
	}

	return nil
}

// newFrame builds a frame for port, from and to the given callsigns, either of
// which may be empty.
func newFrame(port int, kind byte, from string, to string, data []byte) (*agwpe.Message, error) {
	var portx, portErr = safecast.Convert[byte](port)
	if portErr != nil {
		return nil, fmt.Errorf("port %d does not fit an AGW frame: %w", port, portErr)
	}

	var dataLen, lenErr = safecast.Convert[uint32](len(data))
	if lenErr != nil {
		return nil, fmt.Errorf("%d bytes do not fit an AGW frame: %w", len(data), lenErr)
	}

	var msg = new(agwpe.Message)

	msg.Header.Portx = portx
	msg.Header.DataKind = kind

	var fromErr = putCallsign(&msg.Header.CallFrom, from)
	if fromErr != nil {
		return nil, fromErr
	}

	var toErr = putCallsign(&msg.Header.CallTo, to)
	if toErr != nil {
		return nil, toErr
	}

	msg.Header.DataLen = dataLen
	msg.Data = data

	return msg, nil
}

// putCallsign copies call into a header field, leaving room for the NUL the
// server insists on.
func putCallsign(field *agwpe.Callsign, call string) error {
	if len(call) > callsignLen-1 {
		return fmt.Errorf("callsign %q is longer than %d characters", call, callsignLen-1)
	}

	copy(field[:], call)

	return nil
}

// loginFrame builds an application login frame.
func loginFrame(user string, password string) (*agwpe.Message, error) {
	if len(user) >= loginFieldLen || len(password) >= loginFieldLen {
		return nil, fmt.Errorf("AGW login user name and password must each be under %d bytes", loginFieldLen)
	}

	var data = make([]byte, 2*loginFieldLen)

	copy(data, user)
	copy(data[loginFieldLen:], password)

	return newFrame(0, kindLogin, "", "", data)
}

// connectFrame builds a request to connect from one callsign to another on
// port, through the given digipeaters if there are any.
func connectFrame(port int, from string, to string, via []string) (*agwpe.Message, error) {
	if len(via) == 0 {
		return newFrame(port, kindConnect, from, to, nil)
	}

	if len(via) > maxVia {
		return nil, fmt.Errorf("%d digipeaters, but a connect request can name at most %d", len(via), maxVia)
	}

	var data = make([]byte, 1+callsignLen*len(via))

	data[0] = byte(len(via) & 0xff)

	for i, digi := range via {
		var field agwpe.Callsign

		var err = putCallsign(&field, digi)
		if err != nil {
			return nil, err
		}

		copy(data[1+callsignLen*i:], field[:])
	}

	return newFrame(port, kindConnectVia, from, to, data)
}

// outstandingCount reads the count from an outstanding frames reply.
func outstandingCount(msg *agwpe.Message) (uint32, error) {
	if len(msg.Data) < 4 {
		return 0, fmt.Errorf("outstanding frames reply has %d bytes of data, want 4", len(msg.Data))
	}

	return binary.LittleEndian.Uint32(msg.Data), nil
}

// normaliseCall puts a callsign the way the server reports it: upper case,
// and without an SSID of 0, which AX.25 addresses carry implicitly.
func normaliseCall(call string) string {
	call = strings.ToUpper(strings.TrimSpace(call))

	return strings.TrimSuffix(call, "-0")
}
