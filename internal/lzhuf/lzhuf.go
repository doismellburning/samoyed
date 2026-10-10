// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package lzhuf implements the LZHUF compression FBB's compressed forwarding
// protocols use: Okumura and Yoshizaki's LZSS with adaptive Huffman coding,
// with FBB's 2KB window.
//
// A compressed message starts with its uncompressed length, four bytes little
// endian.  The B1 and B2 protocols put a CRC-16 of the length and the
// compressed data in front of that; plain B does not.
package lzhuf

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	windowSize = 2048 // N: the window of text matched against.
	lookahead  = 60   // F: the longest match.
	threshold  = 2    // Matches no longer than this are sent as literals.
	nil_       = windowSize

	numChar = 256 - threshold + lookahead // Kinds of character: literals, then match lengths.
	tableT  = numChar*2 - 1               // Size of the Huffman tree.
	root    = tableT - 1
	maxFreq = 0x8000 // The tree is rebuilt when the root's frequency reaches this.

	headerLen = 4 // The uncompressed length.
	crcLen    = 2
)

// The upper six bits of a match position are Huffman coded with these fixed
// tables; the lower six are sent as they are.
var pLen = [64]uint8{ //nolint:gochecknoglobals // A const array, were there such a thing.
	3, 4, 4, 4, 5, 5, 5, 5, 5, 5, 5, 5, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

var pCode = [64]uint8{ //nolint:gochecknoglobals // A const array, were there such a thing.
	0x00, 0x20, 0x30, 0x40, 0x50, 0x58, 0x60, 0x68,
	0x70, 0x78, 0x80, 0x88, 0x90, 0x94, 0x98, 0x9C,
	0xA0, 0xA4, 0xA8, 0xAC, 0xB0, 0xB4, 0xB8, 0xBC,
	0xC0, 0xC2, 0xC4, 0xC6, 0xC8, 0xCA, 0xCC, 0xCE,
	0xD0, 0xD2, 0xD4, 0xD6, 0xD8, 0xDA, 0xDC, 0xDE,
	0xE0, 0xE2, 0xE4, 0xE6, 0xE8, 0xEA, 0xEC, 0xEE,
	0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7,
	0xF8, 0xF9, 0xFA, 0xFB, 0xFC, 0xFD, 0xFE, 0xFF,
}

// dCode and dLen decode the upper six bits of a position from the next byte
// of input: the inverse of pCode and pLen.
var dCode, dLen = decodeTables() //nolint:gochecknoglobals // Const arrays, were there such a thing.

func decodeTables() ([256]uint8, [256]uint8) {
	var code, length [256]uint8

	for k := range pCode {
		var span = 1 << (8 - pLen[k])
		for b := range span {
			code[int(pCode[k])+b] = uint8(k)
			length[int(pCode[k])+b] = pLen[k]
		}
	}

	return code, length
}

// huffman is the adaptive Huffman tree both ends keep in step.
type huffman struct {
	freq [tableT + 1]uint16
	prnt [tableT + numChar]int
	son  [tableT]int
}

func newHuffman() *huffman {
	var h = new(huffman)

	for i := range numChar {
		h.freq[i] = 1
		h.son[i] = i + tableT
		h.prnt[i+tableT] = i
	}

	for i, j := 0, numChar; j <= root; i, j = i+2, j+1 {
		h.freq[j] = h.freq[i] + h.freq[i+1]
		h.son[j] = i
		h.prnt[i] = j
		h.prnt[i+1] = j
	}

	h.freq[tableT] = 0xffff
	h.prnt[root] = 0

	return h
}

// reconst rebuilds the tree with every frequency halved.
func (h *huffman) reconst() {
	var j = 0

	for i := range tableT {
		if h.son[i] >= tableT {
			h.freq[j] = (h.freq[i] + 1) / 2
			h.son[j] = h.son[i]
			j++
		}
	}

	for i, j := 0, numChar; j < tableT; i, j = i+2, j+1 {
		var f = h.freq[i] + h.freq[i+1]
		h.freq[j] = f

		var k = j - 1
		for f < h.freq[k] {
			k--
		}

		k++
		copy(h.freq[k+1:j+1], h.freq[k:j])
		h.freq[k] = f
		copy(h.son[k+1:j+1], h.son[k:j])
		h.son[k] = i
	}

	for i := range tableT {
		var k = h.son[i]
		if k >= tableT {
			h.prnt[k] = i
		} else {
			h.prnt[k] = i
			h.prnt[k+1] = i
		}
	}
}

// update counts one more c, keeping the tree in order.
func (h *huffman) update(c int) {
	if h.freq[root] == maxFreq {
		h.reconst()
	}

	c = h.prnt[c+tableT]

	for {
		h.freq[c]++
		var k = h.freq[c]

		var l = c + 1
		if k > h.freq[l] {
			for k > h.freq[l+1] {
				l++
			}

			h.freq[c] = h.freq[l]
			h.freq[l] = k

			var i = h.son[c]
			h.prnt[i] = l

			if i < tableT {
				h.prnt[i+1] = l
			}

			var j = h.son[l]
			h.son[l] = i
			h.prnt[j] = c

			if j < tableT {
				h.prnt[j+1] = c
			}

			h.son[c] = j
			c = l
		}

		c = h.prnt[c]
		if c == 0 {
			return
		}
	}
}

// ErrCorrupt is returned for compressed data that does not decompress.
var ErrCorrupt = errors.New("lzhuf: corrupt data")

// ErrCRC is returned for compressed data whose CRC does not match.
var ErrCRC = errors.New("lzhuf: CRC mismatch")

// Compress compresses data, with the CRC the B1 and B2 protocols want if crc
// is true.
func Compress(data []byte, crc bool) []byte {
	var e = newEncoder()

	var out = make([]byte, headerLen, headerLen+len(data)/2+16)
	binary.LittleEndian.PutUint32(out, uint32(len(data)&0xffffffff))

	out = append(out, e.encode(data)...)

	if !crc {
		return out
	}

	var sum = crc16(out)

	return append([]byte{byte(sum & 0xff), byte(sum >> 8)}, out...)
}

// maxExpansion is the most decompressed data may outgrow the compressed, as
// FBB judges it: anything claiming more is taken as corrupt rather than
// trusted with that much memory.
const maxExpansion = 100

// ErrTooBig is returned for compressed data that would decompress to more
// than the limit given.
var ErrTooBig = errors.New("lzhuf: too big")

// Decompress decompresses data, checking the CRC the B1 and B2 protocols put
// in front of it if crc is true.
func Decompress(data []byte, crc bool) ([]byte, error) {
	return DecompressMax(data, crc, -1)
}

// DecompressMax is Decompress, refusing data that would decompress to more
// than limit bytes; a negative limit is none.
func DecompressMax(data []byte, crc bool, limit int) ([]byte, error) {
	if crc {
		if len(data) < crcLen {
			return nil, ErrCorrupt
		}

		var want = uint16(data[0]) | uint16(data[1])<<8
		data = data[crcLen:]

		if crc16(data) != want {
			return nil, ErrCRC
		}
	}

	if len(data) < headerLen {
		return nil, ErrCorrupt
	}

	var size = binary.LittleEndian.Uint32(data)
	var code = data[headerLen:]

	if uint64(size) > uint64(maxExpansion)*uint64(len(code)+1) {
		return nil, fmt.Errorf("%w: claims %d bytes from %d", ErrCorrupt, size, len(code))
	}

	if limit >= 0 && uint64(size) > uint64(limit) {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooBig, size)
	}

	return decode(code, int(size)), nil
}

// encoder is LZSS's binary search tree of the window, and the Huffman coder
// its output goes through.
type encoder struct {
	h *huffman

	text          [windowSize + lookahead - 1]byte
	lson          [windowSize + 1]int
	rson          [windowSize + 257]int
	dad           [windowSize + 1]int
	matchPosition int
	matchLength   int

	out    []byte
	putbuf uint16
	putlen uint
}

func newEncoder() *encoder {
	var e = new(encoder)
	e.h = newHuffman()

	for i := windowSize + 1; i <= windowSize+256; i++ {
		e.rson[i] = nil_
	}

	for i := range windowSize {
		e.dad[i] = nil_
	}

	return e
}

func (e *encoder) insertNode(r int) {
	var cmp = 1
	var key = r
	var p = windowSize + 1 + int(e.text[key])

	e.rson[r] = nil_
	e.lson[r] = nil_
	e.matchLength = 0

	for {
		if cmp >= 0 {
			if e.rson[p] == nil_ {
				e.rson[p] = r
				e.dad[r] = p

				return
			}

			p = e.rson[p]
		} else {
			if e.lson[p] == nil_ {
				e.lson[p] = r
				e.dad[r] = p

				return
			}

			p = e.lson[p]
		}

		var i = 1
		for ; i < lookahead; i++ {
			cmp = int(e.text[key+i]) - int(e.text[p+i])
			if cmp != 0 {
				break
			}
		}

		if i > threshold {
			var position = ((r - p) & (windowSize - 1)) - 1

			if i > e.matchLength {
				e.matchPosition = position
				e.matchLength = i

				if i >= lookahead {
					break
				}
			}

			if i == e.matchLength && position < e.matchPosition {
				e.matchPosition = position
			}
		}
	}

	// A match as long as can be: r takes p's place in the tree.
	e.dad[r] = e.dad[p]
	e.lson[r] = e.lson[p]
	e.rson[r] = e.rson[p]
	e.dad[e.lson[p]] = r
	e.dad[e.rson[p]] = r

	if e.rson[e.dad[p]] == p {
		e.rson[e.dad[p]] = r
	} else {
		e.lson[e.dad[p]] = r
	}

	e.dad[p] = nil_
}

func (e *encoder) deleteNode(p int) {
	if e.dad[p] == nil_ {
		return
	}

	var q int

	switch {
	case e.rson[p] == nil_:
		q = e.lson[p]
	case e.lson[p] == nil_:
		q = e.rson[p]
	default:
		q = e.lson[p]
		if e.rson[q] != nil_ {
			for e.rson[q] != nil_ {
				q = e.rson[q]
			}

			e.rson[e.dad[q]] = e.lson[q]
			e.dad[e.lson[q]] = e.dad[q]
			e.lson[q] = e.lson[p]
			e.dad[e.lson[p]] = q
		}

		e.rson[q] = e.rson[p]
		e.dad[e.rson[p]] = q
	}

	e.dad[q] = e.dad[p]

	if e.rson[e.dad[p]] == p {
		e.rson[e.dad[p]] = q
	} else {
		e.lson[e.dad[p]] = q
	}

	e.dad[p] = nil_
}

// putCode outputs the top l bits of c.
func (e *encoder) putCode(l uint, c uint16) {
	e.putbuf |= c >> e.putlen
	e.putlen += l

	if e.putlen < 8 {
		return
	}

	e.out = append(e.out, byte(e.putbuf>>8))
	e.putlen -= 8

	if e.putlen >= 8 {
		e.out = append(e.out, byte(e.putbuf&0xff))
		e.putlen -= 8
		e.putbuf = c << (l - e.putlen)
	} else {
		e.putbuf <<= 8
	}
}

func (e *encoder) encodeChar(c int) {
	var code uint16

	var length uint

	for k := e.h.prnt[c+tableT]; k != root; k = e.h.prnt[k] {
		code >>= 1
		if k&1 != 0 {
			code += 0x8000
		}

		length++
	}

	e.putCode(length, code)
	e.h.update(c)
}

func (e *encoder) encodePosition(c int) {
	var i = c >> 6
	e.putCode(uint(pLen[i]), uint16(pCode[i])<<8)
	e.putCode(6, uint16((c&0x3f)<<10))
}

func (e *encoder) encode(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}

	var s = 0
	var r = windowSize - lookahead

	for i := range r {
		e.text[i] = ' '
	}

	var length = 0
	for length < lookahead && length < len(data) {
		e.text[r+length] = data[length]
		length++
	}

	var next = length

	for i := 1; i <= lookahead; i++ {
		e.insertNode(r - i)
	}

	e.insertNode(r)

	for length > 0 {
		if e.matchLength > length {
			e.matchLength = length
		}

		if e.matchLength <= threshold {
			e.matchLength = 1
			e.encodeChar(int(e.text[r]))
		} else {
			e.encodeChar(255 - threshold + e.matchLength)
			e.encodePosition(e.matchPosition)
		}

		var last = e.matchLength

		var i = 0
		for ; i < last && next < len(data); i++ {
			var c = data[next]
			next++

			e.deleteNode(s)
			e.text[s] = c

			if s < lookahead-1 {
				e.text[s+windowSize] = c
			}

			s = (s + 1) & (windowSize - 1)
			r = (r + 1) & (windowSize - 1)
			e.insertNode(r)
		}

		for ; i < last; i++ {
			e.deleteNode(s)
			s = (s + 1) & (windowSize - 1)
			r = (r + 1) & (windowSize - 1)

			length--
			if length > 0 {
				e.insertNode(r)
			}
		}
	}

	if e.putlen > 0 {
		e.out = append(e.out, byte(e.putbuf>>8))
	}

	return e.out
}

// decoder reads the bits of compressed data.  Past the end, it reads zeros,
// as FBB's does.
type decoder struct {
	in     []byte
	getbuf uint16
	getlen uint
}

func (d *decoder) fill() {
	for d.getlen <= 8 {
		var b uint16
		if len(d.in) > 0 {
			b = uint16(d.in[0])
			d.in = d.in[1:]
		}

		d.getbuf |= b << (8 - d.getlen)
		d.getlen += 8
	}
}

func (d *decoder) bit() int {
	d.fill()

	var b = int(d.getbuf >> 15)
	d.getbuf <<= 1
	d.getlen--

	return b
}

func (d *decoder) byte() int {
	d.fill()

	var b = int(d.getbuf >> 8)
	d.getbuf <<= 8
	d.getlen -= 8

	return b
}

func decode(code []byte, size int) []byte {
	var h = newHuffman()
	var d = decoder{in: code, getbuf: 0, getlen: 0}

	var text [windowSize]byte
	for i := range windowSize - lookahead {
		text[i] = ' '
	}

	var r = windowSize - lookahead

	var out = make([]byte, 0, size)

	for len(out) < size {
		var c = h.son[root]
		for c < tableT {
			c = h.son[c+d.bit()]
		}

		c -= tableT
		h.update(c)

		if c < 256 {
			out = append(out, byte(c))
			text[r] = byte(c)
			r = (r + 1) & (windowSize - 1)

			continue
		}

		var hi = d.byte()
		var position = int(dCode[hi]) << 6

		var low = hi
		for range int(dLen[hi]) - 2 {
			low = (low << 1) + d.bit()
		}

		position |= low & 0x3f

		var from = (r - position - 1) & (windowSize - 1)

		for k := range c - 255 + threshold {
			if len(out) == size {
				break
			}

			var b = text[(from+k)&(windowSize-1)]
			out = append(out, b)
			text[r] = b
			r = (r + 1) & (windowSize - 1)
		}
	}

	return out
}

// crc16 is the CRC FBB puts in front of B1 and B2 compressed data: CRC-16
// with polynomial 0x1021 and nothing to start, as XMODEM uses.
func crc16(data []byte) uint16 {
	var crc uint16

	for _, b := range data {
		crc ^= uint16(b) << 8

		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}

	return crc
}
