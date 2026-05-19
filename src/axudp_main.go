// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// axudpMapEntry holds one MAP line from the config.
type axudpMapEntry struct {
	AX25Addr string       // AX.25 address, i.e. callsign and optional SSID, e.g. "Q1TEST" or "Q1TEST-1"
	Addr     string       // UDP address string for display/logging, e.g. "192.0.2.1:20093"
	UDPAddr  *net.UDPAddr // pre-resolved UDP address for sending
}

// axudpYAMLConfig is the top-level structure of the axudp.yaml config file.
type axudpYAMLConfig struct {
	Maps []axudpYAMLMapEntry `yaml:"maps"`
}

// axudpYAMLMapEntry represents one entry under the "maps" key.
type axudpYAMLMapEntry struct {
	AX25Addr string `yaml:"ax25addr"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
}

// axudpParseConfig reads a YAML config file from path and returns the map entries.
func axudpParseConfig(path string) ([]axudpMapEntry, error) {
	var data, err = os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, err
	}

	var cfg axudpYAMLConfig
	var unmarshalErr = yaml.Unmarshal(data, &cfg)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("parsing YAML config: %w", unmarshalErr)
	}

	var entries = make([]axudpMapEntry, 0, len(cfg.Maps))
	for i, m := range cfg.Maps {
		if m.AX25Addr == "" {
			return nil, fmt.Errorf("map entry %d: ax25addr is empty", i)
		}
		if m.Host == "" {
			return nil, fmt.Errorf("map entry %d: host is empty", i)
		}
		if m.Port < 1 || m.Port > 65535 {
			return nil, fmt.Errorf("map entry %d: port %d out of range (1-65535)", i, m.Port)
		}
		var addr = net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
		var udpAddr, resolveErr = net.ResolveUDPAddr("udp", addr)
		if resolveErr != nil {
			return nil, fmt.Errorf("map entry %d: resolving %s: %w", i, addr, resolveErr)
		}
		entries = append(entries, axudpMapEntry{
			AX25Addr: strings.ToUpper(m.AX25Addr),
			Addr:     addr,
			UDPAddr:  udpAddr,
		})
	}
	return entries, nil
}

// axudpExtractDest extracts the destination AX.25 address from a raw AX.25 frame.
// Returns "" if the frame is too short.
func axudpExtractDest(frame []byte) string {
	if len(frame) < 7 {
		return ""
	}

	var call [6]byte
	for i := range 6 {
		call[i] = frame[i] >> 1
	}

	var callstr = strings.TrimRight(string(call[:]), " ")

	var ssid = (frame[6] >> 1) & 0x0F
	if ssid != 0 {
		callstr = fmt.Sprintf("%s-%d", callstr, ssid)
	}

	return callstr
}

// axudpBridge is the live state of the bridge.
type axudpBridge struct {
	maps    []axudpMapEntry
	udpConn *net.UDPConn
	verbose bool

	mu      sync.Mutex
	clients []net.Conn
}

func (b *axudpBridge) addClient(c net.Conn) {
	b.mu.Lock()
	b.clients = append(b.clients, c)
	b.mu.Unlock()
}

func (b *axudpBridge) removeClient(c net.Conn) {
	b.mu.Lock()
	var next []net.Conn
	for _, cl := range b.clients {
		if cl != c {
			next = append(next, cl)
		}
	}
	b.clients = next
	b.mu.Unlock()
}

// axudpBroadcastWriteTimeout is the per-write deadline applied when forwarding
// KISS frames to TCP clients.  A stalled client is disconnected after this
// duration so it cannot block delivery to other clients.
const axudpBroadcastWriteTimeout = 5 * time.Second

// broadcastKISS sends a KISS-wrapped AX.25 frame to all KISS TCP clients.
func (b *axudpBridge) broadcastKISS(ax25frame []byte) {
	// Prepend type byte 0x00 (channel 0, DATA_FRAME) before KISS-encoding.
	var payload = append([]byte{KISS_CMD_DATA_FRAME}, ax25frame...)
	var kissframe = kiss_encapsulate(payload)

	b.mu.Lock()
	var snapshot = make([]net.Conn, len(b.clients))
	copy(snapshot, b.clients)
	b.mu.Unlock()

	for _, c := range snapshot {
		_ = c.SetWriteDeadline(time.Now().Add(axudpBroadcastWriteTimeout))
		var _, writeErr = c.Write(kissframe)
		if writeErr != nil {
			c.Close()
			b.removeClient(c)
		}
	}
}

// ax25AddrBase returns the AX.25 address without any SSID suffix (i.e. strips "-N").
func ax25AddrBase(cs string) string {
	if idx := strings.LastIndexByte(cs, '-'); idx >= 0 {
		return cs[:idx]
	}
	return cs
}

// lookupMap finds the MAP entry for the given destination AX.25 address.
// A MAP entry with no SSID matches any SSID of that base address;
// a MAP entry with an SSID matches only that exact address-SSID pair.
func (b *axudpBridge) lookupMap(dest string) (axudpMapEntry, bool) {
	for _, e := range b.maps {
		if e.AX25Addr == dest {
			return e, true
		}
		// If the MAP entry has no SSID, match dest regardless of its SSID.
		if !strings.ContainsRune(e.AX25Addr, '-') && ax25AddrBase(dest) == e.AX25Addr {
			return e, true
		}
	}
	return axudpMapEntry{}, false //nolint: exhaustruct
}

// axudpAddCRC appends the 2-byte AXUDP checksum to frame and returns the
// result.  The checksum is CRC-CCITT (poly 0x1021, seed 0xFFFF, final XOR
// 0xFFFF) over the frame bytes, appended little-endian.
func axudpAddCRC(frame []byte) []byte {
	var crc = fcs_calc(frame)
	return append(append([]byte(nil), frame...), byte(crc), byte(crc>>8))
}

// axudpStripCRC validates and strips the 2-byte AXUDP checksum from the
// end of pkt.  Returns the AX.25 frame and true if valid, or nil and false if
// the checksum is wrong or the packet is too short.
func axudpStripCRC(pkt []byte) ([]byte, bool) {
	if len(pkt) < 2 {
		return nil, false
	}
	var frame = pkt[:len(pkt)-2]
	var want = fcs_calc(frame)
	var got = uint16(pkt[len(pkt)-2]) | uint16(pkt[len(pkt)-1])<<8
	if got != want {
		return nil, false
	}
	return frame, true
}

// sendAXUDP sends a raw AX.25 frame to the given UDP address.
// A CRC-CCITT checksum is always appended (per RFC 1226 / AXUDP convention).
func (b *axudpBridge) sendAXUDP(ax25frame []byte, entry axudpMapEntry) {
	var pkt = axudpAddCRC(ax25frame)
	var n, writeErr = b.udpConn.WriteTo(pkt, entry.UDPAddr)
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: UDP send to %s: %v\n", entry.Addr, writeErr)
	} else if b.verbose {
		fmt.Printf("samoyed-axudp: sent %d bytes via AXUDP to %s\n", n, entry.Addr)
	}
}

// handleKISSClient reads KISS frames from one TCP client and routes them as AXUDP.
func (b *axudpBridge) handleKISSClient(conn net.Conn) {
	defer conn.Close()
	defer b.removeClient(conn)

	b.addClient(conn)

	var kf kiss_frame_t
	var overflow bool

	var buf = make([]byte, 2048)
	for {
		var n, readErr = conn.Read(buf)
		if readErr != nil {
			fmt.Printf("samoyed-axudp: KISS client %s disconnected: %v\n", conn.RemoteAddr(), readErr)
			return
		}

		if b.verbose {
			fmt.Printf("samoyed-axudp: received %d bytes from KISS client %s\n", n, conn.RemoteAddr())
		}

		for _, byt := range buf[:n] {
			my_kiss_rec_byte_axudp(&kf, &overflow, byt, b)
		}
	}
}

// my_kiss_rec_byte_axudp accumulates one KISS byte.
// When a complete frame is collected, the AX.25 payload is extracted
// and forwarded to the appropriate AXUDP destination.
// overflow must point to a caller-owned bool that persists across calls for the
// same connection; it is set to true when a frame exceeds MAX_KISS_LEN and
// cleared when collection resets, so the truncated frame is discarded.
func my_kiss_rec_byte_axudp(kf *kiss_frame_t, overflow *bool, b byte, b2 *axudpBridge) {
	switch kf.state {
	default: // KS_SEARCHING
		if b == FEND {
			kf.kiss_len = 0
			kf.kiss_msg[kf.kiss_len] = b
			kf.kiss_len++
			*overflow = false
			kf.state = KS_COLLECTING
		}

	case KS_COLLECTING:
		if b == FEND {
			if kf.kiss_len <= 1 {
				// Empty or double-FEND — restart.
				kf.kiss_msg[0] = b
				kf.kiss_len = 1
				*overflow = false
				return
			}

			if *overflow {
				// Frame exceeded MAX_KISS_LEN; discard it entirely.
				fmt.Fprintf(os.Stderr, "samoyed-axudp: KISS frame exceeded max length (%d bytes), discarding\n", MAX_KISS_LEN)
				kf.kiss_len = 0
				*overflow = false
				kf.state = KS_SEARCHING
				return
			}

			if kf.kiss_len < MAX_KISS_LEN {
				kf.kiss_msg[kf.kiss_len] = b
				kf.kiss_len++
			}

			var unwrapped = kiss_unwrap(kf.kiss_msg[:kf.kiss_len])

			// unwrapped[0] is the type byte (channel << 4 | cmd).
			// We only care about DATA_FRAME commands (lower nibble == 0).
			if b2.verbose {
				fmt.Printf("samoyed-axudp: KISS frame complete, %d bytes unwrapped, type byte 0x%02x\n", len(unwrapped), func() byte {
					if len(unwrapped) > 0 {
						return unwrapped[0]
					}
					return 0
				}())
			}
			if len(unwrapped) >= 2 && (unwrapped[0]&0x0F) == KISS_CMD_DATA_FRAME {
				var ax25frame = unwrapped[1:]
				var dest = axudpExtractDest(ax25frame)
				if b2.verbose {
					fmt.Printf("samoyed-axudp: AX.25 frame dest=%q, %d bytes, forwarding via AXUDP\n", dest, len(ax25frame))
				}
				if dest == "" {
					fmt.Fprintf(os.Stderr, "samoyed-axudp: frame too short to extract destination\n")
				} else if entry, ok := b2.lookupMap(dest); ok {
					b2.sendAXUDP(ax25frame, entry)
				} else {
					fmt.Fprintf(os.Stderr, "samoyed-axudp: no MAP entry for destination %s, dropping\n", dest)
				}
			} else if len(unwrapped) >= 1 && b2.verbose {
				fmt.Printf("samoyed-axudp: ignoring non-data KISS command 0x%02x\n", unwrapped[0]&0x0F)
			}

			kf.kiss_len = 0
			kf.state = KS_SEARCHING
			return
		}

		if kf.kiss_len < MAX_KISS_LEN {
			kf.kiss_msg[kf.kiss_len] = b
			kf.kiss_len++
		} else {
			*overflow = true
		}
	}
}

// maxUDPPayload is the maximum possible UDP payload size; no valid datagram can
// exceed this, so a buffer of this size guarantees ReadFromUDP never truncates.
const maxUDPPayload = 65535

// runUDPListener reads incoming AXUDP datagrams and forwards them as KISS to all clients.
func (b *axudpBridge) runUDPListener() {
	var buf = make([]byte, maxUDPPayload)
	for {
		var n, _, readErr = b.udpConn.ReadFromUDP(buf)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "samoyed-axudp: UDP read: %v\n", readErr)
			return
		}

		if n < 1 {
			continue
		}

		var raw = make([]byte, n)
		copy(raw, buf[:n])

		// RFC 1226 specifies a 16-bit CRC-CCITT checksum for AXIP (TCP/IP);
		// most AXUDP implementations have copied this over even though UDP
		// already includes its own checksum.  There is no protocol negotiation
		// and no way to distinguish such a peer from one that does not append a
		// checksum.  We auto-detect by checking whether the trailing 2 bytes
		// form a valid checksum; if so we strip them.
		var ax25frame []byte
		if stripped, ok := axudpStripCRC(raw); ok {
			ax25frame = stripped
		} else {
			ax25frame = raw
		}

		if b.verbose {
			var src = axudpExtractDest(ax25frame) // dest field of incoming = the remote station
			fmt.Printf("samoyed-axudp: received AXUDP datagram, %d bytes, src address=%q\n", n, src)
		}
		b.broadcastKISS(ax25frame)
	}
}

// runKISSServer accepts TCP connections from KISS clients.
func (b *axudpBridge) runKISSServer(kissPort int) {
	var ln, listenErr = net.Listen("tcp", fmt.Sprintf(":%d", kissPort))
	if listenErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: TCP listen on port %d: %v\n", kissPort, listenErr)
		os.Exit(1)
	}
	fmt.Printf("samoyed-axudp: KISS TCP server listening on port %d\n", kissPort)

	for {
		var conn, acceptErr = ln.Accept()
		if acceptErr != nil {
			fmt.Fprintf(os.Stderr, "samoyed-axudp: accept: %v\n", acceptErr)
			continue
		}
		fmt.Printf("samoyed-axudp: new KISS client %s\n", conn.RemoteAddr())
		go b.handleKISSClient(conn)
	}
}

// AXUDPMain is the entry point for samoyed-axudp.
func AXUDPMain() {
	pflag.Usage = func() {
		fmt.Fprintf(pflag.CommandLine.Output(), `samoyed-axudp [BETA] - AXUDP bridge for samoyed-direwolf

NOTE: samoyed-axudp is beta software. Its behaviour, config file format,
and flags may change in future releases without notice.

Bridges between samoyed-direwolf (via TCP KISS) and remote packet radio
nodes that speak AXUDP (raw AX.25 frames in UDP datagrams, per RFC 1226).
samoyed-direwolf connects to samoyed-axudp using an
NCHANNEL directive in its config file.

Usage:
  samoyed-axudp [--config <file>] [--udpport <n>] [--kissport <n>]

Example config file (axudp.yaml):
  maps:
    - ax25addr: Q1TEST-1
      host: 192.0.2.1
      port: 93

Example samoyed-direwolf config to connect via samoyed-axudp:
  CHANNEL 2
  MYCALL Q1TEST
  NCHANNEL 2 localhost 8002

Flags:
`)
		pflag.PrintDefaults()
	}

	var configFile = pflag.String("config", "axudp.yaml", "Path to YAML config file")
	var udpPort = pflag.Int("udpport", 20093, "UDP port to listen on (and source from)")
	var kissPort = pflag.Int("kissport", 8002, "TCP port for KISS clients (samoyed-direwolf NCHANNEL target)")
	var verbose = pflag.Bool("verbose", false, "Log every packet sent and received")
	pflag.Parse()

	var maps, parseErr = axudpParseConfig(*configFile)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: reading config: %v\n", parseErr)
		os.Exit(1)
	}

	fmt.Printf("samoyed-axudp: WARNING: this is beta software; behaviour may change in future releases\n")
	fmt.Printf("samoyed-axudp: MAP table:\n")
	for _, e := range maps {
		fmt.Printf("  %s -> %s\n", e.AX25Addr, e.Addr)
	}

	var udpAddr, resolveErr = net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", *udpPort))
	if resolveErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: resolve UDP addr: %v\n", resolveErr)
		os.Exit(1)
	}

	var udpConn, listenErr = net.ListenUDP("udp", udpAddr)
	if listenErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: UDP listen on port %d: %v\n", *udpPort, listenErr)
		os.Exit(1)
	}
	fmt.Printf("samoyed-axudp: AXUDP listening on UDP port %d\n", *udpPort)

	var b = new(axudpBridge)
	b.maps = maps
	b.udpConn = udpConn
	b.verbose = *verbose

	go b.runUDPListener()
	b.runKISSServer(*kissPort)
}
