// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// An AXUDP channel (AXUDPCHANNEL) is a virtual channel whose "radio" is a set
// of other packet nodes reached over UDP, each frame a datagram (RFC 1226
// style, with the same auto-detected checksum samoyed-axudp copes with).
// Frames heard go on the received queue like any others, and frames sent go
// to the peer AXUDPMAP says their destination is at, so everything the
// daemon does with a channel - digipeating, connected mode, clients - works
// over it without a KISS bridge in the way.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/sirupsen/logrus"
)

// axudpReadRetryDelay is how long an AXUDP channel waits after a failed read
// before reading again, so that a socket in trouble does not spin the CPU.
const axudpReadRetryDelay = 100 * time.Millisecond

// AXUDPChannel is one AXUDPCHANNEL: a UDP socket and the peers it talks to.
type AXUDPChannel struct {
	axudpRouter

	channel int
	started atomic.Bool
}

// NewAXUDPChannels opens the socket for every AXUDPCHANNEL and starts it
// listening, until ctx is cancelled.  It returns the channel for each AXUDP
// channel number, nil for every other.  Exits if a socket cannot be opened;
// if cancelled part way, returns those opened so far.
func NewAXUDPChannels(ctx context.Context, pa *AudioConfig) [MAX_TOTAL_CHANS]*AXUDPChannel {
	var channels [MAX_TOTAL_CHANS]*AXUDPChannel

	for i := range MAX_TOTAL_CHANS {
		if pa.chan_medium[i] != MEDIUM_AXUDP {
			continue
		}

		var logEntry = logrus.WithFields(logrus.Fields{
			"channel": i,
			"port":    pa.axudp_port[i],
			"peers":   len(pa.axudp_maps[i]),
		})

		var ac, err = NewAXUDPChannel(ctx, i, pa.axudp_port[i], pa.axudp_maps[i])
		if err != nil {
			if ctx.Err() != nil {
				return channels
			}

			logEntry.WithError(err).Error("Could not open AXUDP channel")
			os.Exit(1)
		}

		logEntry.Debug("AXUDP channel listening")

		ac.Start(ctx)

		channels[i] = ac
	}

	return channels
}

// NewAXUDPChannel opens the UDP socket for one AXUDP channel, sending to and
// hearing from maps.  Nothing is read from it until Start is called.
func NewAXUDPChannel(ctx context.Context, channel int, port int, maps []AXUDPMapEntry) (*AXUDPChannel, error) {
	dwutil.Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var pc, listenErr = new(net.ListenConfig).ListenPacket(ctx, "udp", fmt.Sprintf(":%d", port))
	if listenErr != nil {
		return nil, listenErr
	}

	var conn, ok = pc.(*net.UDPConn)
	if !ok {
		pc.Close()

		return nil, fmt.Errorf("UDP port %d is not a UDP socket", port)
	}

	var ac = new(AXUDPChannel)
	ac.channel = channel
	ac.maps = maps
	ac.udpConn = conn

	return ac, nil
}

// Start reads datagrams from the channel's peers, and puts the frames they
// carry on the received queue, until ctx is cancelled, when it closes the
// socket.  A second Start is complained about and ignored, as it is for a
// NetTNC.
func (ac *AXUDPChannel) Start(ctx context.Context) {
	if !ac.started.CompareAndSwap(false, true) {
		logrus.WithField("channel", ac.channel).Error("AXUDP channel started twice; ignoring the second start")

		return
	}

	go ac.listen(ctx)
}

func (ac *AXUDPChannel) listen(ctx context.Context) {
	defer ac.udpConn.Close()

	// The read below blocks until a datagram turns up, which may be never, so
	// closing the socket is what gets us back when we are asked to stop.
	defer closeOnDone(ctx, ac.udpConn)()

	var buf = make([]byte, maxUDPPayload)

	for ctx.Err() == nil {
		var n, from, readErr = ac.udpConn.ReadFromUDP(buf)
		if ctx.Err() != nil {
			return // We closed it ourselves on the way out.
		}

		if readErr != nil {
			logrus.WithField("channel", ac.channel).WithError(readErr).Error("Could not read from AXUDP socket")

			if errors.Is(readErr, net.ErrClosed) {
				return
			}

			if !sleepCtx(ctx, axudpReadRetryDelay) {
				return
			}

			continue
		}

		ac.receive(buf[:n], from)
	}
}

// receive puts the frame one datagram carries on the received queue.
func (ac *AXUDPChannel) receive(datagram []byte, from *net.UDPAddr) {
	var frame = axudpFrame(datagram)

	var alevel ax25.ALevel

	var pp = ax25.FromFrame(frame, alevel)
	if pp == nil {
		logrus.WithFields(logrus.Fields{
			"channel": ac.channel,
			"from":    from,
			"size":    len(datagram),
		}).Warn("Discarding AXUDP datagram that is not an AX.25 frame")

		return
	}

	dataLinkQueue.RecFrame(ac.channel, -3, 0, pp, alevel, fec_type_none, RETRY_NONE, "AXUDP")
}

// sendPacket sends a packet to the peer its destination is mapped to.  ac may
// be nil, for a channel with no AXUDP socket.
func (ac *AXUDPChannel) sendPacket(channel int, pp *ax25.Packet) {
	if ac == nil {
		logrus.WithField("channel", channel).Error("No AXUDP socket for channel. Discarding packet.")

		return
	}

	ac.route(pp.FrameData())
}
