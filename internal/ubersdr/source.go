// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ubersdr receives demodulated audio from an UberSDR instance
// (https://ubersdr.org/), which serves its receivers over a WebSocket.
//
// README.md describes the protocol, the package's layout, the code in it that
// is copied from UberSDR, and how that code is licensed.
package ubersdr

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Prefix introduces an UberSDR audio device name, e.g.
// "ubersdr:https://sdr.example.org/?frequency=10147600&mode=usb".
const Prefix = "ubersdr:"

// HasPrefix reports whether an audio device name names an UberSDR instance.
func HasPrefix(name string) bool {
	return len(name) >= len(Prefix) && strings.EqualFold(name[:len(Prefix)], Prefix)
}

// modeSampleRates are the audio modes we can demodulate from, and the rate
// UberSDR streams each at.  The server fixes these to match ka9q-radio's
// presets (GetSampleRateForMode in its config.go), and announces the rate in
// each packet's header too, so a server that changes them is noticed rather
// than played at the wrong speed.  The IQ modes are left out: they are two
// channels of baseband, not audio.
var modeSampleRates = map[string]int{ //nolint:gochecknoglobals // Constant lookup table
	"usb": 12000,
	"lsb": 12000,
	"cwu": 12000,
	"cwl": 12000,
	"am":  24000,
	"sam": 24000,
	"fm":  24000,
	"nfm": 24000,
}

const defaultMode = "usb"

// Source is an UberSDR instance and what to tune it to.
type Source struct {
	base *url.URL // http or https, with any path prefix the instance is served under

	Frequency  uint64 // Hz
	Mode       string
	SampleRate int // what Mode streams at
	Channels   int // always 1: every mode we accept is mono audio

	password      string
	bandwidthLow  string
	bandwidthHigh string

	minBackoff time.Duration
	maxBackoff time.Duration
}

// ParseSource parses an audio device name of the form
//
//	ubersdr:https://host[:port][/path]/?frequency=Hz[&mode=usb][&bandwidthLow=Hz&bandwidthHigh=Hz][&password=...]
//
// An unknown query parameter is an error rather than being passed on, so that
// a misspelt one is noticed rather than quietly having no effect.
func ParseSource(name string) (*Source, error) {
	if !HasPrefix(name) {
		return nil, fmt.Errorf("UberSDR source %q does not start with %q", name, Prefix)
	}

	var u, err = url.Parse(name[len(Prefix):])
	if err != nil {
		return nil, fmt.Errorf("UberSDR source: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("UberSDR source %q: the URL must be http:// or https://", name)
	}

	if u.Host == "" {
		return nil, fmt.Errorf("UberSDR source %q: no host", name)
	}

	if u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("UberSDR source %q: user information and #fragments are not supported - pass a password as password=", name)
	}

	var src = &Source{ //nolint:exhaustruct_v5 // The optional parameters are filled in below
		Mode:       defaultMode,
		Channels:   1,
		minBackoff: time.Second,
		maxBackoff: time.Minute,
	}

	var query = u.Query()

	for key, values := range query {
		if len(values) != 1 {
			return nil, fmt.Errorf("UberSDR source: %s given %d times", key, len(values))
		}

		var value = values[0]

		switch key {
		case "frequency":
			var f, err = strconv.ParseUint(value, 10, 64)
			if err != nil || f == 0 {
				return nil, fmt.Errorf("UberSDR source: frequency must be a whole number of Hz, not %q", value)
			}

			src.Frequency = f
		case "mode":
			src.Mode = strings.ToLower(value)
		case "bandwidthLow", "bandwidthHigh":
			var _, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("UberSDR source: %s must be a whole number of Hz, not %q", key, value)
			}

			if key == "bandwidthLow" {
				src.bandwidthLow = value
			} else {
				src.bandwidthHigh = value
			}
		case "password":
			src.password = value
		default:
			return nil, fmt.Errorf("UberSDR source: unknown parameter %q (expected frequency, mode, bandwidthLow, bandwidthHigh or password)", key)
		}
	}

	if src.Frequency == 0 {
		return nil, errors.New("UberSDR source: frequency=<Hz> is required")
	}

	var rate, ok = modeSampleRates[src.Mode]
	if !ok {
		return nil, fmt.Errorf("UberSDR source: mode must be one of usb, lsb, cwu, cwl, am, sam, fm or nfm, not %q", src.Mode)
	}

	src.SampleRate = rate

	u.RawQuery = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	src.base = u

	return src, nil
}

// String describes the source for logging, without its password.
func (s *Source) String() string {
	return fmt.Sprintf("%s %d Hz %s", s.base.String(), s.Frequency, s.Mode)
}

// connectionURL is where a client registers its session before opening the
// WebSocket.
func (s *Source) connectionURL() string {
	var u = *s.base
	u.Path += "/connection"

	return u.String()
}

// webSocketURL is the audio WebSocket for one session.
//
// format=pcm-zstd is the lossless format's historical name: from protocol
// version 4 it carries the predictive codec, with no zstd anywhere.
func (s *Source) webSocketURL(sessionID string) string {
	var u = *s.base
	u.Path += "/ws"

	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}

	var q = url.Values{}
	q.Set("frequency", strconv.FormatUint(s.Frequency, 10))
	q.Set("mode", s.Mode)
	q.Set("format", "pcm-zstd")
	q.Set("version", "4")
	q.Set("user_session_id", sessionID)

	if s.bandwidthLow != "" {
		q.Set("bandwidthLow", s.bandwidthLow)
	}

	if s.bandwidthHigh != "" {
		q.Set("bandwidthHigh", s.bandwidthHigh)
	}

	if s.password != "" {
		q.Set("password", s.password)
	}

	u.RawQuery = q.Encode()

	return u.String()
}
