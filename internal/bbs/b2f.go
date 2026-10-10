// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

// B2F, the message format Winlink's B2 forwarding carries: headers, a blank
// line, then the body and any attached files.  See
// https://winlink.org/B2F.  Attachments are not kept: a BBS message is text.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// b2fDate is how B2F writes a message's date.
const b2fDate = "2006/01/02 15:04"

// maxB2FHeaders is the most header lines a B2F message may have.
const maxB2FHeaders = 100

// encodeB2F writes m as a B2F message, its BID the message ID, from the
// mailbox mbo.
func encodeB2F(m *Message, mbo string) []byte {
	var body = wireBody(m.Body)

	var typ = "Private"
	if !m.personal() {
		typ = "Bulletin"
	}

	var to = m.To
	if m.At != "" {
		to += "@" + m.At
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Mid: %s\r\n", m.BID)
	fmt.Fprintf(&b, "Date: %s\r\n", m.Date.UTC().Format(b2fDate))
	fmt.Fprintf(&b, "Type: %s\r\n", typ)
	fmt.Fprintf(&b, "From: %s\r\n", m.From)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	fmt.Fprintf(&b, "Mbo: %s\r\n", mbo)
	fmt.Fprintf(&b, "Body: %d\r\n", len(body))
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")

	return []byte(b.String())
}

var errBadB2F = errors.New("bbs: malformed B2F message")

// decodeB2F reads a B2F message.  Only the first addressee is kept: a BBS
// message has one.
func decodeB2F(data []byte) (*Message, error) {
	var text = string(data)

	var head, rest, ok = strings.Cut(text, "\r\n\r\n")
	if !ok {
		return nil, fmt.Errorf("%w: no end to the headers", errBadB2F)
	}

	var m = new(Message)
	m.Type = TypePersonal

	var bodyLen = -1

	var lines = strings.Split(head, "\r\n")
	if len(lines) > maxB2FHeaders {
		return nil, fmt.Errorf("%w: too many headers", errBadB2F)
	}

	for _, line := range lines {
		var key, value, found = strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("%w: header %q", errBadB2F, line)
		}

		value = strings.TrimSpace(value)

		switch strings.ToLower(key) {
		case "mid":
			m.BID = value
		case "date":
			var d, err = time.Parse(b2fDate, value)
			if err == nil {
				m.Date = d
			}
		case "type":
			if strings.EqualFold(value, "Bulletin") {
				m.Type = TypeBulletin
			}
		case "from":
			m.From, _ = parseAddress(value)
		case "to":
			if m.To == "" {
				m.To, m.At = parseAddress(strings.TrimPrefix(value, "SMTP:"))
			}
		case "subject":
			m.Subject = value
		case "body":
			var n, err = strconv.Atoi(value)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("%w: body length %q", errBadB2F, value)
			}

			bodyLen = n
		}
	}

	if m.BID == "" || m.From == "" || m.To == "" {
		return nil, fmt.Errorf("%w: missing Mid, From or To", errBadB2F)
	}

	if bodyLen < 0 || bodyLen > len(rest) {
		return nil, fmt.Errorf("%w: body length", errBadB2F)
	}

	m.Body = localBody(rest[:bodyLen])

	return m, nil
}
