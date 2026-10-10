// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

// The pieces of the FBB forwarding protocol: the SID each end announces
// itself with, the proposals offering messages, the answers to them, and the
// framing compressed messages travel in.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SID is a system identifier, "[FBB-7.0.10-AB1FHMRX$]": the software, its
// version, and letters saying what it can do.
type SID struct {
	Software string
	Version  string
	Flags    string
}

// ParseSID parses a SID line, reporting whether line is one.
func ParseSID(line string) (SID, bool) {
	var sid SID

	line = strings.TrimSpace(line)
	if len(line) < 3 || line[0] != '[' || line[len(line)-1] != ']' {
		return sid, false
	}

	var parts = strings.Split(line[1:len(line)-1], "-")
	if len(parts) < 2 {
		return sid, false
	}

	sid.Software = parts[0]
	sid.Flags = strings.ToUpper(parts[len(parts)-1])
	sid.Version = strings.Join(parts[1:len(parts)-1], "-")

	return sid, true
}

func (s SID) String() string {
	if s.Version == "" {
		return fmt.Sprintf("[%s-%s]", s.Software, s.Flags)
	}

	return fmt.Sprintf("[%s-%s-%s]", s.Software, s.Version, s.Flags)
}

// protocol is the forwarding protocol two BBSes settle on.
type protocol int

const (
	protoNone protocol = iota // No FBB forwarding: one of them lacks F.
	protoF                    // FBB's, uncompressed.
	protoB                    // Compressed, without CRC.
	protoB1                   // Compressed, with CRC.
	protoB2                   // Winlink's B2F messages, compressed with CRC.
)

func (p protocol) String() string {
	switch p {
	case protoF:
		return "FBB"
	case protoB:
		return "B"
	case protoB1:
		return "B1"
	case protoB2:
		return "B2"
	case protoNone:
		return "none"
	default:
		return "unknown"
	}
}

// can says what the SID's flags offer: F for FBB forwarding, B for
// compression, and the digits after the B for its versions - "B1", "B2", or
// "B12" for both.
func (s SID) can() (bool, bool, bool, bool) {
	var f = strings.ContainsRune(s.Flags, 'F')

	var i = strings.IndexByte(s.Flags, 'B')
	if i < 0 {
		return f, false, false, false
	}

	var versions = s.Flags[i+1:]
	versions = versions[:len(versions)-len(strings.TrimLeft(versions, "0123456789"))]

	return f, true, strings.ContainsRune(versions, '1'), strings.ContainsRune(versions, '2')
}

// negotiate picks the protocol two SIDs share.  B1 is preferred to B2 between
// BBSes, as it carries their messages as they are; B2 is for a partner that
// offers nothing else.
func negotiate(ours SID, theirs SID) protocol {
	var of, ob, ob1, ob2 = ours.can()
	var tf, tb, tb1, tb2 = theirs.can()

	switch {
	case !of || !tf:
		return protoNone
	case ob1 && tb1:
		return protoB1
	case ob2 && tb2:
		return protoB2
	case ob && tb:
		return protoB
	default:
		return protoF
	}
}

// compressed says whether messages travel compressed under p.
func (p protocol) compressed() bool {
	return p == protoB || p == protoB1 || p == protoB2
}

// crc says whether compressed messages carry a CRC under p.
func (p protocol) crc() bool {
	return p == protoB1 || p == protoB2
}

// maxProposals is the most messages one proposal block offers.
const maxProposals = 5

// maxBlockSize is roughly how much one proposal block offers, as FBB's
// default: a block holds more only to hold at least one message.
const maxBlockSize = 10 * 1024

// proposal offers one message.
type proposal struct {
	code byte // 'A' compressed, 'B' plain text, 'C' B2.

	// For codes A and B.
	typ  string
	from string
	at   string
	to   string

	// For code C, "EM" or "CM".
	msgType string

	bid            string // The BID, or for code C the MID.
	size           int
	compressedSize int // For code C.
}

var errBadProposal = errors.New("bbs: malformed proposal")

// parseProposal parses an FA, FB or FC line.
func parseProposal(line string) (proposal, error) {
	var p proposal

	var fields = strings.Fields(line)
	if len(fields) == 0 || len(fields[0]) != 2 || fields[0][0] != 'F' {
		return p, errBadProposal
	}

	p.code = fields[0][1]

	switch p.code {
	case 'A', 'B':
		if len(fields) != 7 {
			return p, fmt.Errorf("%w: %q", errBadProposal, line)
		}

		p.typ = strings.ToUpper(fields[1])
		p.from = strings.ToUpper(fields[2])
		p.at = strings.ToUpper(fields[3])
		p.to = strings.ToUpper(fields[4])
		p.bid = fields[5]

		var size, err = strconv.Atoi(fields[6])
		if err != nil || size < 0 {
			return p, fmt.Errorf("%w: %q", errBadProposal, line)
		}

		p.size = size
	case 'C':
		if len(fields) < 5 {
			return p, fmt.Errorf("%w: %q", errBadProposal, line)
		}

		p.msgType = strings.ToUpper(fields[1])
		p.bid = fields[2]

		var size, err = strconv.Atoi(fields[3])
		var csize, cerr = strconv.Atoi(fields[4])

		if err != nil || cerr != nil || size < 0 || csize < 0 {
			return p, fmt.Errorf("%w: %q", errBadProposal, line)
		}

		p.size = size
		p.compressedSize = csize
	default:
		return p, fmt.Errorf("%w: %q", errBadProposal, line)
	}

	if p.bid == "" || len(p.bid) > maxBIDLen {
		return p, fmt.Errorf("%w: bad BID in %q", errBadProposal, line)
	}

	return p, nil
}

// line is p as it is sent.
func (p proposal) line() string {
	if p.code == 'C' {
		return fmt.Sprintf("FC %s %s %d %d 0", p.msgType, p.bid, p.size, p.compressedSize)
	}

	return fmt.Sprintf("F%c %s %s %s %s %s %d", p.code, p.typ, p.from, p.at, p.to, p.bid, p.size)
}

// blockChecksum is the checksum ending a proposal block, "F> HH": the two's
// complement of the sum of every proposal line's bytes, each with its CR.
func blockChecksum(lines []string) byte {
	var sum = 0

	for _, l := range lines {
		for i := range len(l) {
			sum += int(l[i])
		}

		sum += '\r'
	}

	return byte(-sum & 0xff)
}

// answer is what the receiving BBS says to one proposal.
type answer byte

const (
	answerAccept   answer = '+' // Send it.
	answerHave     answer = '-' // Already have it.
	answerDefer    answer = '=' // Not now; offer it again another time.
	answerReject   answer = 'R' // Will not take it.
	answerHold     answer = 'H' // Will take it, but hold it for the sysop.
	answerError    answer = 'E' // The proposal was malformed.
	answerAcceptAt answer = '!' // Send it from an offset, which this BBS never asks for.
)

// parseAnswers parses an FS line answering n proposals.
func parseAnswers(line string, n int) ([]answer, error) {
	var rest, ok = strings.CutPrefix(strings.TrimSpace(line), "FS ")
	if !ok {
		return nil, fmt.Errorf("bbs: not an FS line: %q", line)
	}

	rest = strings.TrimSpace(rest)

	var answers []answer

	for len(rest) > 0 {
		var c = rest[0]
		rest = rest[1:]

		switch c {
		case '+', 'Y', 'y':
			answers = append(answers, answerAccept)
		case '-', 'N', 'n':
			answers = append(answers, answerHave)
		case '=', 'L', 'l':
			answers = append(answers, answerDefer)
		case 'R', 'r':
			answers = append(answers, answerReject)
		case 'H', 'h':
			answers = append(answers, answerHold)
		case 'E', 'e':
			answers = append(answers, answerError)
		case '!', 'A', 'a':
			// An offset follows: resuming a message half received
			// before.  This BBS never sends half a message, so
			// offers it again whole another time.
			for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
				rest = rest[1:]
			}

			answers = append(answers, answerAcceptAt)
		default:
			return nil, fmt.Errorf("bbs: bad answer %q in %q", c, line)
		}
	}

	if len(answers) != n {
		return nil, fmt.Errorf("bbs: %d answers to %d proposals in %q", len(answers), n, line)
	}

	return answers, nil
}

// The bytes compressed messages are framed with.
const (
	chrNUL = 0x00
	chrSOH = 0x01
	chrSTX = 0x02
	chrEOT = 0x04
	chrSUB = 0x1a // Ctrl-Z, ending an uncompressed message.
)

// maxTitle is the longest a title may be in a compressed message's header.
const maxTitle = 80

// maxBlock is the most data one block of a compressed message carries.
const maxBlock = 250

// frameCompressed frames compressed data for sending, titled title: a header,
// the data in blocks, then a checksum.
func frameCompressed(title string, data []byte) []byte {
	if len(title) > maxTitle {
		title = title[:maxTitle]
	}

	if title == "" {
		title = "(no subject)"
	}

	const offset = "0"

	var out = make([]byte, 0, len(data)+len(data)/maxBlock*2+len(title)+16)
	out = append(out, chrSOH, byte((len(title)+len(offset)+2)&0xff))
	out = append(out, title...)
	out = append(out, chrNUL)
	out = append(out, offset...)
	out = append(out, chrNUL)

	var sum = 0

	for len(data) > 0 {
		var n = min(len(data), maxBlock)
		out = append(out, chrSTX, byte(n&0xff))
		out = append(out, data[:n]...)

		for _, b := range data[:n] {
			sum += int(b)
		}

		data = data[n:]
	}

	return append(out, chrEOT, byte(-sum&0xff))
}

// maxCompressed is the most compressed data one message may carry.
const maxCompressed = 512 * 1024

// blockReader takes a compressed message's framing apart, a byte at a time.
type blockReader struct {
	state   int
	need    int // Bytes left of the header or block being read.
	header  []byte
	data    []byte
	sum     int
	title   string
	done    bool
	failure error
}

const (
	brStart = iota
	brHeaderLen
	brHeader
	brBlockStart
	brBlockLen
	brBlock
	brChecksum
)

var (
	errFraming  = errors.New("bbs: compressed message framing is wrong")
	errChecksum = errors.New("bbs: compressed message checksum is wrong")
	errTooBig   = errors.New("bbs: compressed message too big")
)

// feed takes bytes, returning how many it used: it stops once the message is
// complete or something is wrong, leaving the rest for whatever comes next.
func (r *blockReader) feed(data []byte) int {
	for i, b := range data {
		if r.done || r.failure != nil {
			return i
		}

		r.step(b)
	}

	return len(data)
}

func (r *blockReader) step(b byte) {
	switch r.state {
	case brStart:
		if b != chrSOH {
			r.failure = errFraming

			return
		}

		r.state = brHeaderLen
	case brHeaderLen:
		r.need = int(b)
		r.state = brHeader

		if r.need == 0 {
			r.failure = errFraming
		}
	case brHeader:
		r.header = append(r.header, b)
		r.need--

		if r.need == 0 {
			var title, _, _ = strings.Cut(string(r.header), "\x00")
			r.title = title
			r.state = brBlockStart
		}
	case brBlockStart:
		switch b {
		case chrSTX:
			r.state = brBlockLen
		case chrEOT:
			r.state = brChecksum
		default:
			r.failure = errFraming
		}
	case brBlockLen:
		r.need = int(b)
		if r.need == 0 {
			r.need = 256
		}

		r.state = brBlock
	case brBlock:
		r.data = append(r.data, b)
		r.sum += int(b)
		r.need--

		if len(r.data) > maxCompressed {
			r.failure = errTooBig

			return
		}

		if r.need == 0 {
			r.state = brBlockStart
		}
	case brChecksum:
		if (r.sum+int(b))&0xff != 0 {
			r.failure = errChecksum

			return
		}

		r.done = true
	}
}
