// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func positionRequest() *EncodeRequest {
	var req = new(EncodeRequest)
	req.Type = "position"
	req.Source = "q1test-9"
	req.Destination = "APDW18"
	req.Path = "WIDE1-1, WIDE2-1"
	req.Lat = 42.619
	req.Lon = -71.34717
	req.SymbolTable = "/"
	req.Symbol = ">"
	req.Comment = "Testing"

	return req
}

func TestEncodePosition(t *testing.T) {
	var line, err = Encode(positionRequest())
	require.NoError(t, err)

	assert.Equal(t, "Q1TEST-9>APDW18,WIDE1-1,WIDE2-1:!4237.14N/07120.83W>Testing", line)
}

func TestEncodePositionWithExtras(t *testing.T) {
	var req = positionRequest()
	req.Path = ""
	req.Messaging = true
	req.Course = new(int)
	*req.Course = 90
	req.SpeedKnots = new(int)
	*req.SpeedKnots = 25
	req.AltitudeFt = new(int)
	*req.AltitudeFt = 1234

	var line, err = Encode(req)
	require.NoError(t, err)

	assert.Equal(t, "Q1TEST-9>APDW18:=4237.14N/07120.83W>090/025/A=001234Testing", line)
}

func TestEncodeObject(t *testing.T) {
	var req = positionRequest()
	req.Type = "object"
	req.Name = "Q2TEST"
	req.Timestamp = new(time.Time)
	*req.Timestamp = time.Date(2026, 10, 3, 12, 34, 56, 0, time.UTC)

	var line, err = Encode(req)
	require.NoError(t, err)

	assert.Equal(t, "Q1TEST-9>APDW18,WIDE1-1,WIDE2-1:;Q2TEST   *031234z4237.14N/07120.83W>Testing", line)
}

func TestEncodeMessage(t *testing.T) {
	var req = new(EncodeRequest)
	req.Type = "message"
	req.Source = "Q1TEST"
	req.Destination = "APDW18"
	req.Addressee = "q2test-1"
	req.Text = "Hello"
	req.MessageID = "42"

	var line, err = Encode(req)
	require.NoError(t, err)

	assert.Equal(t, "Q1TEST>APDW18::Q2TEST-1 :Hello{42", line)
}

func TestEncodeRejects(t *testing.T) {
	var tests = map[string]func(*EncodeRequest){
		"no source":           func(r *EncodeRequest) { r.Source = "" },
		"no destination":      func(r *EncodeRequest) { r.Destination = "" },
		"bad source":          func(r *EncodeRequest) { r.Source = "Q1TEST-99" },
		"latitude too big":    func(r *EncodeRequest) { r.Lat = 91 },
		"longitude too small": func(r *EncodeRequest) { r.Lon = -181 },
		"ambiguity":           func(r *EncodeRequest) { r.Ambiguity = 5 },
		"no symbol":           func(r *EncodeRequest) { r.Symbol = "" },
		"object without name": func(r *EncodeRequest) { r.Type = "object" },
		"long message id":     func(r *EncodeRequest) { r.Type = "message"; r.Addressee = "Q2TEST"; r.MessageID = "123456" },
		"unknown type":        func(r *EncodeRequest) { r.Type = "telemetry" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			var req = positionRequest()
			mutate(req)

			var _, err = Encode(req)
			assert.Error(t, err)
		})
	}
}

// What the encoder makes, the decoder should read back.
func TestEncodeThenDecode(t *testing.T) {
	var line, err = Encode(positionRequest())
	require.NoError(t, err)

	testutils.AssertOutputContains(t, func() {
		Decode(newDecoder(), line)
	}, "N 42°37.1400, W 071°20.8300")
}
