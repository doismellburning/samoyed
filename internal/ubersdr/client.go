// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ubersdr

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/ubersdr/pcmv4"
	"github.com/sirupsen/logrus"
)

// Sink receives each packet's audio as little-endian signed 16-bit samples,
// with the rate and channel count the packet's header announced.  An error
// drops the connection, which Run then retries.
type Sink func(pcmLE []byte, sampleRate, channels int) error

// pingInterval is how often a ping goes to the server, which UberSDR's own
// clients do to keep proxies and the server from timing an idle-looking
// socket out.
const pingInterval = 30 * time.Second

// readLimit caps one WebSocket message.  A 20 ms packet of 24 kHz audio is
// well under a kilobyte; this is only to bound what a misbehaving server can
// make us allocate.
const readLimit = 1 << 20

// connectionTimeout bounds the /connection request.
const connectionTimeout = 15 * time.Second

// errServerTooOld is a server that answered a request for protocol version 4
// with version 1 - see pcmv4.IsZstdFrame.
var errServerTooOld = errors.New("the server does not support UberSDR audio protocol version 4 (it needs UberSDR 0.1.63 or later)")

// Run receives audio from src, handing each packet to sink, until ctx is
// cancelled.  A connection that fails or drops is retried, backing off
// exponentially, so a station whose receiver is briefly unreachable carries on
// once it is back rather than stopping.
func Run(ctx context.Context, src *Source, userAgent string, sink Sink) {
	var backoff = src.minBackoff
	var log = logrus.WithField("ubersdr", src.String())

	for ctx.Err() == nil {
		var delivered, err = runSession(ctx, src, userAgent, sink, log)

		if ctx.Err() != nil {
			return
		}

		if delivered {
			backoff = src.minBackoff
		}

		log.WithError(err).WithField("retry_in", backoff).Warn("UberSDR audio connection lost")

		if !dwutil.SleepCtx(ctx, backoff) {
			return
		}

		backoff = min(backoff*2, src.maxBackoff)
	}
}

// runSession registers one session, opens its WebSocket and receives audio
// until something goes wrong.  It reports whether any audio arrived, which is
// what tells Run that the server was working and the backoff can start again.
func runSession(ctx context.Context, src *Source, userAgent string, sink Sink, log *logrus.Entry) (bool, error) {
	var sessionID, err = newSessionID()
	if err != nil {
		return false, err
	}

	err = checkConnection(ctx, src, userAgent, sessionID)
	if err != nil {
		return false, err
	}

	var header = http.Header{}
	header.Set("User-Agent", userAgent)

	var conn, resp, dialErr = websocket.Dial(ctx, src.webSocketURL(sessionID), &websocket.DialOptions{ //nolint:exhaustruct_v5 // Defaults otherwise
		HTTPHeader: header,
	})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	if dialErr != nil {
		return false, fmt.Errorf("opening the audio WebSocket: %w", dialErr)
	}

	defer conn.CloseNow()

	conn.SetReadLimit(readLimit)

	log.Info("UberSDR audio connected")

	var sessionCtx, cancel = context.WithCancel(ctx)
	defer cancel()

	go keepAlive(sessionCtx, conn)

	// A decoder per connection: the predictor adapts to what it has decoded,
	// so it is only in step with the server for the socket it started on.
	var dec = pcmv4.NewPCMv4StreamDecoder()
	var delivered = false

	for {
		var msgType, data, readErr = conn.Read(sessionCtx)
		if readErr != nil {
			return delivered, readErr
		}

		if msgType == websocket.MessageText {
			handleText(data, log)

			continue
		}

		if !pcmv4.PCMv4IsHeader(data) {
			if pcmv4.IsZstdFrame(data) {
				return delivered, errServerTooOld
			}

			// Opus, which we did not ask for and cannot decode.
			log.WithField("bytes", len(data)).Trace("Ignoring a binary message that is not a PCM v4 packet")

			continue
		}

		// Every packet is decoded, even one we would rather drop: skipping one
		// would leave the predictor out of step for the rest of the
		// connection.
		var pcmLE, sampleRate, channels, _, _, decErr = dec.DecodePacketLE(data)
		if decErr != nil {
			log.WithError(decErr).Debug("Undecodable UberSDR audio packet")

			continue
		}

		var sinkErr = sink(pcmLE, sampleRate, channels)
		if sinkErr != nil {
			return delivered, sinkErr
		}

		delivered = true
	}
}

// keepAlive pings the server until ctx is cancelled.
func keepAlive(ctx context.Context, conn *websocket.Conn) {
	var ticker = time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`))
			if err != nil {
				return
			}
		}
	}
}

// serverMessage is the part of the server's JSON messages worth reporting.
type serverMessage struct {
	Type   string `json:"type"`
	Error  string `json:"error"`
	Status int    `json:"status"`
}

// handleText logs a JSON message from the server.  Only an error is worth
// telling the operator about; the server follows a fatal one by closing the
// socket, which is what ends the session.
func handleText(data []byte, log *logrus.Entry) {
	var msg serverMessage

	var err = json.Unmarshal(data, &msg)
	if err != nil {
		log.WithError(err).Debug("Unparseable text message from UberSDR")

		return
	}

	switch msg.Type {
	case "error":
		log.WithField("status", msg.Status).Error("UberSDR: " + msg.Error)
	case "pong":
		log.Trace("UberSDR pong")
	default:
		log.WithField("type", msg.Type).Debug("UberSDR message")
	}
}

// connectionRequest and connectionResponse are the body and reply of
// POST /connection, which registers a session ID before the WebSocket will
// accept it.
type connectionRequest struct {
	UserSessionID string `json:"user_session_id"` //nolint:tagliatelle // The server's name for it
	Password      string `json:"password,omitempty"`
}

type connectionResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// checkConnection registers sessionID with the server and reports whether it
// will let us connect.
func checkConnection(ctx context.Context, src *Source, userAgent string, sessionID string) error {
	// Sending the password is the point: it is what lets an instance's
	// operator grant a station access beyond the public limits.
	body, err := json.Marshal(connectionRequest{UserSessionID: sessionID, Password: src.password}) //nolint:gosec // G117: see above; := for errchkjson, which does not follow var
	if err != nil {
		return err
	}

	var reqCtx, cancel = context.WithTimeout(ctx, connectionTimeout)
	defer cancel()

	var req, reqErr = http.NewRequestWithContext(reqCtx, http.MethodPost, src.connectionURL(), bytes.NewReader(body))
	if reqErr != nil {
		return reqErr
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	var resp, doErr = http.DefaultClient.Do(req)
	if doErr != nil {
		return fmt.Errorf("registering a session: %w", doErr)
	}
	defer resp.Body.Close()

	var cr connectionResponse

	var decodeErr = json.NewDecoder(resp.Body).Decode(&cr)
	if decodeErr != nil {
		return fmt.Errorf("registering a session: HTTP %d, and the reply was not understood: %w", resp.StatusCode, decodeErr)
	}

	if !cr.Allowed {
		return fmt.Errorf("the server refused the connection: %s", cr.Reason)
	}

	return nil
}

// newSessionID returns a random (version 4) UUID, which is what the server
// insists a session ID is.
func newSessionID() (string, error) {
	var b [16]byte

	var _, err = rand.Read(b[:])
	if err != nil {
		return "", err
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
