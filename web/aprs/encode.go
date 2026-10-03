// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
)

// EncodeRequest is what the page's encoder form sends, as JSON.  An absent
// optional number (a nil pointer) is left out of the packet.
type EncodeRequest struct {
	Type string `json:"type"` // "position", "object" or "message"

	Source      string `json:"source"`
	Destination string `json:"destination"`
	Path        string `json:"path"` // Comma-separated digipeaters, or empty

	// Position and object.
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	SymbolTable string   `json:"symbolTable"` // One character: / \ 0-9 A-Z
	Symbol      string   `json:"symbol"`      // One character
	Compressed  bool     `json:"compressed"`
	Messaging   bool     `json:"messaging"` // Position only
	Ambiguity   int      `json:"ambiguity"`
	AltitudeFt  *int     `json:"altitudeFt"`
	Course      *int     `json:"course"`
	SpeedKnots  *int     `json:"speedKnots"`
	Power       *int     `json:"power"`
	Height      *int     `json:"height"`
	Gain        *int     `json:"gain"`
	Directivity string   `json:"directivity"`
	Freq        *float64 `json:"freq"`
	Tone        *float64 `json:"tone"`
	Offset      *float64 `json:"offset"`
	Comment     string   `json:"comment"`

	// Object.
	Name      string     `json:"name"`
	Timestamp *time.Time `json:"timestamp"` // Nil for none

	// Message.
	Addressee string `json:"addressee"`
	Text      string `json:"text"`
	MessageID string `json:"messageId"`
}

// Encode returns the packet req describes, in the monitoring format
// (SOURCE>DEST,PATH:info) that the decoder - and most other APRS software -
// takes.
func Encode(req *EncodeRequest) (string, error) {
	var info, infoErr = encodeInfo(req)
	if infoErr != nil {
		return "", infoErr
	}

	if req.Source == "" {
		return "", errors.New("a source callsign is needed")
	}

	if req.Destination == "" {
		return "", errors.New("a destination is needed")
	}

	var addrs = strings.ToUpper(req.Source) + ">" + strings.ToUpper(req.Destination)

	var path = strings.ReplaceAll(req.Path, " ", "")
	if path != "" {
		addrs += "," + strings.ToUpper(path)
	}

	var line = addrs + ":" + info

	if ax25.FromTextWithStrictness(line, ax25.AddrStrict) == nil {
		return "", fmt.Errorf("%q is not a valid set of addresses", addrs)
	}

	return line, nil
}

func encodeInfo(req *EncodeRequest) (string, error) {
	switch req.Type {
	case "position", "object":
		var symtab, symbol, symErr = symbolBytes(req)
		if symErr != nil {
			return "", symErr
		}

		if req.Lat < -90 || req.Lat > 90 {
			return "", errors.New("latitude must be between -90 and 90")
		}

		if req.Lon < -180 || req.Lon > 180 {
			return "", errors.New("longitude must be between -180 and 180")
		}

		if req.Ambiguity < 0 || req.Ambiguity > 4 {
			return "", errors.New("ambiguity must be between 0 and 4")
		}

		if req.Type == "position" {
			return aprs.EncodePosition(req.Messaging, req.Compressed, req.Lat, req.Lon, req.Ambiguity,
				maybe.FromPointer(req.AltitudeFt), symtab, symbol,
				maybe.FromPointer(req.Power), maybe.FromPointer(req.Height), maybe.FromPointer(req.Gain), req.Directivity,
				maybe.FromPointer(req.Course), maybe.FromPointer(req.SpeedKnots),
				maybe.FromPointer(req.Freq), maybe.FromPointer(req.Tone), maybe.FromPointer(req.Offset),
				req.Comment), nil
		}

		if req.Name == "" || len(req.Name) > 9 {
			return "", errors.New("an object name is 1 to 9 characters")
		}

		var when time.Time
		if req.Timestamp != nil {
			when = *req.Timestamp
		}

		return aprs.EncodeObject(req.Name, req.Compressed, when, req.Lat, req.Lon, req.Ambiguity,
			symtab, symbol,
			maybe.FromPointer(req.Power), maybe.FromPointer(req.Height), maybe.FromPointer(req.Gain), req.Directivity,
			maybe.FromPointer(req.Course), maybe.FromPointer(req.SpeedKnots),
			maybe.FromPointer(req.Freq), maybe.FromPointer(req.Tone), maybe.FromPointer(req.Offset),
			req.Comment), nil
	case "message":
		if req.Addressee == "" || len(req.Addressee) > 9 {
			return "", errors.New("an addressee is 1 to 9 characters")
		}

		if len(req.MessageID) > 5 {
			return "", errors.New("a message number is at most 5 characters")
		}

		return aprs.EncodeMessage(strings.ToUpper(req.Addressee), req.Text, req.MessageID), nil
	default:
		return "", fmt.Errorf("unknown packet type %q", req.Type)
	}
}

func symbolBytes(req *EncodeRequest) (byte, byte, error) {
	if len(req.SymbolTable) != 1 {
		return 0, 0, errors.New("the symbol table is one character: / \\ 0-9 or A-Z")
	}

	if len(req.Symbol) != 1 {
		return 0, 0, errors.New("the symbol is one character")
	}

	return req.SymbolTable[0], req.Symbol[0], nil
}
