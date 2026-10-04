// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// An AXUDP channel is a virtual channel whose frames travel to and from other
// nodes as UDP datagrams, as samoyed-axudp does from outside - but here,
// frames arrive on a channel of their own rather than via an NCHANNEL, so the
// rest of the daemon sees them as it would any other received frame.

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/axudp"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/sirupsen/logrus"
)

// axudpReadErrorDelay is how long an AXUDP channel waits after a failed read
// before trying again, so a socket that keeps failing does not spin.
const axudpReadErrorDelay = time.Second

// AXUDPChannel is one AXUDP channel's socket and routes.
type AXUDPChannel struct {
	channel int
	routes  axudp.Routes
	conn    *net.UDPConn
	started atomic.Bool
}

// NewAXUDPChannels opens the socket for every AXUDP channel in pa and starts
// listening on it, until ctx is cancelled.  It returns the channel for each
// AXUDP channel number, nil for every other.  It exits if a socket cannot be
// opened; if cancelled part way, it returns those opened so far.
func NewAXUDPChannels(ctx context.Context, pa *RadioConfig) [MAX_TOTAL_CHANS]*AXUDPChannel {
	var channels [MAX_TOTAL_CHANS]*AXUDPChannel

	for i := range MAX_TOTAL_CHANS {
		if pa.chan_medium[i] != MEDIUM_AXUDP {
			continue
		}

		logrus.WithFields(logrus.Fields{
			"channel": i,
			"port":    pa.axudp_port[i],
		}).Debug("Opening AXUDP channel")

		var ac, err = NewAXUDPChannel(ctx, i, pa.axudp_port[i], pa.axudp_routes[i])
		if err != nil {
			if ctx.Err() != nil {
				return channels
			}

			logrus.WithField("channel", i).WithError(err).Error("Could not open AXUDP channel")
			os.Exit(1)
		}

		ac.Start(ctx)

		channels[i] = ac
	}

	return channels
}

// NewAXUDPChannel opens the UDP socket for channel on port, sending by routes.
// Nothing is read from it until Start is called.
func NewAXUDPChannel(ctx context.Context, channel int, port int, routes axudp.Routes) (*AXUDPChannel, error) {
	dwutil.Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var pc, err = new(net.ListenConfig).ListenPacket(ctx, "udp", net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}

	var ac = new(AXUDPChannel)
	ac.channel = channel
	ac.routes = routes
	ac.conn = pc.(*net.UDPConn) //nolint:forcetypeassert // A UDP listener is a UDPConn.

	return ac, nil
}

// Start reads datagrams and dispatches the frames in them to the received
// queue, until ctx is cancelled, when it closes the socket.  A second Start is
// complained about and ignored, as two readers would each get only some of
// what arrives.
func (ac *AXUDPChannel) Start(ctx context.Context) {
	if !ac.started.CompareAndSwap(false, true) {
		logrus.WithField("channel", ac.channel).Error("AXUDP channel started twice; ignoring the second start")

		return
	}

	go ac.listen(ctx)
}

// listen is Start's goroutine.
func (ac *AXUDPChannel) listen(ctx context.Context) {
	// The read blocks until a datagram turns up, which may be never, so
	// closing the socket is what gets us back when we are asked to stop.
	defer dwutil.CloseOnDone(ctx, ac.conn)()

	var buf = make([]byte, axudp.MaxUDPPayload)
	for ctx.Err() == nil {
		var n, from, err = ac.conn.ReadFromUDP(buf)
		if ctx.Err() != nil {
			return // We closed it ourselves on the way out.
		}

		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				logrus.WithField("channel", ac.channel).WithError(err).Error("AXUDP channel socket closed; no longer receiving")

				return
			}

			logrus.WithField("channel", ac.channel).WithError(err).Error("Could not read AXUDP datagram")

			if !dwutil.SleepCtx(ctx, axudpReadErrorDelay) {
				return
			}

			continue
		}

		ac.receive(buf[:n], from)
	}
}

// receive puts the frame in one AXUDP datagram on the received queue.
func (ac *AXUDPChannel) receive(datagram []byte, from *net.UDPAddr) {
	// As in the bridge's RunUDPListener: a peer may or may not append the RFC 1226
	// checksum, and there is no telling which but to look.
	var frame = datagram
	if stripped, ok := axudp.StripCRC(datagram); ok {
		frame = stripped
	}

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"channel": ac.channel,
			"length":  len(datagram),
			"from":    from.String(),
		}).Trace("Received AXUDP datagram")
	}

	var alevel ax25.ALevel

	// Anyone who can reach the port can send datagrams that are not frames,
	// as fast as they like, so turning one away is not worth a warning each
	// time - and FromFrame warns about a length it can't use, so check that
	// first.
	var pp *ax25.Packet
	if len(frame) >= ax25.MinPacketLen && len(frame) <= ax25.MaxPacketLen {
		pp = ax25.FromFrame(frame, alevel)
	}

	if pp == nil {
		if logrus.IsLevelEnabled(logrus.DebugLevel) {
			logrus.WithFields(logrus.Fields{
				"channel": ac.channel,
				"from":    from.String(),
			}).Debug("Dropping AXUDP datagram that is not an AX.25 frame")
		}

		return
	}

	var retries BitFixLevel

	dataLinkQueue.RecFrame(ac.channel, -4, 0, pp, alevel, fec_type_none, retries, "AXUDP")
}

// sendPacket sends pp to wherever the channel's routes say its destination
// goes.  ac can be nil, for a channel that could not be opened, when the
// packet is discarded.  pp remains the caller's.
func (ac *AXUDPChannel) sendPacket(channel int, pp *ax25.Packet) {
	if ac == nil {
		logrus.WithField("channel", channel).Error("No AXUDP socket for channel; discarding packet")

		return
	}

	var frame = pp.FrameData()
	var dest = axudp.ExtractDest(frame)

	var entries = ac.routes.Route(dest)
	if len(entries) == 0 {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"dest":    dest,
		}).Warn("Dropping AX.25 frame with no AXUDP map entry for its destination")

		return
	}

	var datagram = axudp.AddCRC(frame)

	for _, entry := range entries {
		var _, err = ac.conn.WriteTo(datagram, entry.UDPAddr)
		if errors.Is(err, net.ErrClosed) {
			// The listener closes the socket when we are stopping, and
			// something may still be queueing on the way out.
			logrus.WithField("channel", channel).Debug("AXUDP channel closed; discarding packet")

			return
		} else if err != nil {
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"peer":    entry.Addr,
			}).WithError(err).Error("Could not send AXUDP datagram")
		} else if logrus.IsLevelEnabled(logrus.TraceLevel) {
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"length":  len(datagram),
				"peer":    entry.Addr,
			}).Trace("Sent AXUDP datagram")
		}
	}
}
