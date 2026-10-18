// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bbs is a packet bulletin board: personal mail and bulletins, kept
// on disk, read and written by users from the node's shell, and passed on to
// and taken from neighbouring BBSes with the FBB forwarding protocol.
//
// Like the rest of the node, it runs on the node's one goroutine and is not
// safe for concurrent use.
package bbs

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Message types, as the FBB protocol names them.
const (
	TypePersonal = "P"
	TypeBulletin = "B"
	TypeTraffic  = "T" // NTS traffic, routed as personal mail.
)

// Message is one message: who it is from and to, where it is going, and what
// it says.
type Message struct {
	Number  int       `json:"number"`
	Type    string    `json:"type"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	At      string    `json:"at"` // The BBS, or the area a bulletin is for, with any hierarchical route.
	BID     string    `json:"bid"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"` // Lines ending "\n".
	Date    time.Time `json:"date"`

	Read bool `json:"read"`

	Origin    string   `json:"origin,omitempty"`    // The partner it came from, or "" for here.
	Forward   []string `json:"forward,omitempty"`   // Partners it is still to go to.
	Forwarded []string `json:"forwarded,omitempty"` // Partners it has gone to.
}

// Size is the message's size, as proposals give it: its body with each line
// ending in CR LF.
func (m *Message) Size() int {
	return len(wireBody(m.Body))
}

// personal says whether the message goes to one station rather than many.
func (m *Message) personal() bool {
	return m.Type != TypeBulletin
}

// visibleTo says whether user may read m: anyone may read a bulletin, and a
// personal message is for its sender and its addressee.
func (m *Message) visibleTo(user string) bool {
	return !m.personal() || sameStation(m.To, user) || sameStation(m.From, user)
}

// pending says whether m is still to go to partner.
func (m *Message) pending(partner string) bool {
	return slices.Contains(m.Forward, partner)
}

// forwardedTo moves partner from the list m is still to go to to the list it
// has gone to.
func (m *Message) forwardedTo(partner string) {
	m.Forward = slices.DeleteFunc(m.Forward, func(p string) bool { return p == partner })
	if !slices.Contains(m.Forwarded, partner) {
		m.Forwarded = append(m.Forwarded, partner)
	}
}

// baseCall is call without its SSID.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(strings.TrimSpace(call)), "-")

	return base
}

// sameStation says whether a and b are the same station, whatever SSIDs they
// carry: mail is for an operator, not one of their stations.
func sameStation(a string, b string) bool {
	return baseCall(a) != "" && baseCall(a) == baseCall(b)
}

// atBBS returns the BBS an @ field names: its first element.
func atBBS(at string) string {
	var first, _, _ = strings.Cut(strings.ToUpper(at), ".")

	return first
}

// parseAddress splits "CALL@BBS.ROUTE" into its callsign and @ field.
func parseAddress(addr string) (string, string) {
	var to, at, _ = strings.Cut(strings.ToUpper(strings.TrimSpace(addr)), "@")

	return strings.TrimSpace(to), strings.TrimSpace(at)
}

// wireBody is body as it goes over the air: lines ending CR LF.
func wireBody(body string) string {
	var b strings.Builder

	for line := range strings.Lines(body) {
		b.WriteString(strings.TrimRight(line, "\r\n"))
		b.WriteString("\r\n")
	}

	return b.String()
}

// localBody is body as it came over the air, its lines ending "\n".
func localBody(wire string) string {
	var b strings.Builder

	for line := range strings.Lines(strings.ReplaceAll(wire, "\r\n", "\n")) {
		b.WriteString(strings.TrimRight(line, "\r\n"))
		b.WriteString("\n")
	}

	return strings.ReplaceAll(b.String(), "\r", "\n")
}

// routeLine is the R: line a BBS adds to the top of a message it passes on:
// when, its message number, and where it is.
func routeLine(now time.Time, number int, call string, hroute string) string {
	var where = call
	if hroute != "" {
		where += "." + hroute
	}

	return fmt.Sprintf("R:%s %d@%s", now.UTC().Format("060102/1504Z"), number, where)
}
