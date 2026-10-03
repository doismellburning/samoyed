// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
)

// clientApps are the client applications that can attach to us - over AGW,
// and over each of the three KISS transports - all of which are sent a copy of
// what we hear.  Any of them can be nil, for one that isn't there.
type clientApps struct {
	agw        *AGWServer
	kissNet    *KissNetService
	kissSerial *KissSerial
	kissPT     *KissPT
}

// SendRecPacket sends pp, heard on channel, to every attached client
// application.  Safe on a nil receiver, which sends nothing.
func (a *clientApps) SendRecPacket(channel int, pp *ax25.Packet) {
	if a == nil {
		return
	}

	var fbuf = pp.Pack()

	a.agw.SendRecPacket(channel, pp, fbuf)                       // AGW net protocol
	a.kissNet.SendRecPacket(channel, kiss.CmdDataFrame, fbuf)    // KISS TCP
	a.kissSerial.SendRecPacket(channel, kiss.CmdDataFrame, fbuf) // KISS serial port
	a.kissPT.SendRecPacket(channel, kiss.CmdDataFrame, fbuf)     // KISS pseudo terminal
}
