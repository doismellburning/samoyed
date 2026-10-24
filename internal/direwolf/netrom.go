// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/netrom"
)

// maxNetromNodesInterval is the longest interval between NODES broadcasts,
// in seconds: a week, far beyond anything useful.
const maxNetromNodesInterval = 7 * 24 * 60 * 60

// netromSettings is the NET/ROM node as the configuration file describes
// it.
type netromSettings struct {
	config netrom.Config

	// integrated says the node shares the node shell's address and alias,
	// rather than having its own.
	integrated bool

	// line is where the file gave these settings, for complaints.
	line int
}

// applyNETROM checks the settings of a NET/ROM node and turns it on.  Beside
// the node shell it shares the shell's address and alias; without one it has
// its own.  Given no channels, it runs on all the shell's, or without one on
// every channel connected mode can be used on, which netromConfig fills in,
// since the command line can still add to them.  Whether its channels can
// carry NET/ROM waits for netromConfig too.
func (ps *parseState) applyNETROM(settings NetROMSettings) error {
	var config = netrom.DefaultConfig()

	var shell, integrated = ps.misc.node.Get()

	var err error

	if integrated {
		config.Callsign, config.Alias, err = ps.netromSharedNames(shell, settings)
	} else {
		config.Callsign, config.Alias, err = ps.netromOwnNames(settings)
	}

	if err != nil {
		return err
	}

	// Left out, the channels are filled in later; given, there must be
	// some.
	if settings.Channels != nil && len(settings.Channels) == 0 {
		return fmt.Errorf("line %d: No channels for NET/ROM to run on. Leave channels out to run on all of them", ps.line)
	}

	// A channel given twice is refused by the YAML parser, as any
	// duplicate key is, though only when spelt the same both times: 0
	// and 0x0 are two keys to it, and the last wins.
	for _, c := range slices.Sorted(maps.Keys(settings.Channels)) {
		if c < 0 || c >= MAX_TOTAL_CHANS {
			return fmt.Errorf("line %d: Invalid channel %d for NET/ROM. Must be in range of 0 to %d", ps.line, c, MAX_TOTAL_CHANS-1)
		}

		config.Channels = append(config.Channels, c)

		if q := settings.Channels[c].Quality; q != nil {
			var quality, qErr = netromByte(ps.line, fmt.Sprintf("quality for channel %d", c), q, 0, 0)
			if qErr != nil {
				return qErr
			}

			if config.ChannelQuality == nil {
				config.ChannelQuality = make(map[int]byte)
			}

			config.ChannelQuality[c] = quality
		}
	}

	config.TTL, err = netromByte(ps.line, "ttl", settings.TTL, 1, config.TTL)
	if err != nil {
		return err
	}

	config.Quality, err = netromByte(ps.line, "quality", settings.Quality, 0, config.Quality)
	if err != nil {
		return err
	}

	config.MinQuality, err = netromByte(ps.line, "minQuality", settings.MinQuality, 0, config.MinQuality)
	if err != nil {
		return err
	}

	if settings.NodesInterval != nil {
		// Bounded above too, before it is multiplied up into a Duration,
		// which would otherwise wrap round to nonsense.
		if *settings.NodesInterval < 1 || *settings.NodesInterval > maxNetromNodesInterval {
			return fmt.Errorf("line %d: Invalid NET/ROM nodesInterval %d. Must be in range of 1 to %d seconds",
				ps.line, *settings.NodesInterval, maxNetromNodesInterval)
		}

		config.NodesInterval = time.Duration(*settings.NodesInterval) * time.Second
	}

	ps.misc.netrom = maybe.Just(netromSettings{config: config, integrated: integrated, line: ps.line})

	return nil
}

// netromOwnNames are the callsign and alias of a NET/ROM node with no node
// shell, which settings must give.
func (ps *parseState) netromOwnNames(settings NetROMSettings) (string, string, error) {
	var callsign, callErr = netrom.NormaliseCallsign(settings.Callsign)
	if callErr != nil {
		return "", "", fmt.Errorf("line %d: Invalid NET/ROM callsign: %w", ps.line, callErr)
	}

	var alias = strings.ToUpper(settings.Alias)
	if alias == "" || !netrom.ValidAlias(alias) {
		return "", "", fmt.Errorf("line %d: Invalid NET/ROM alias %q. Must be 1 to %d letters, digits or #_&-/",
			ps.line, settings.Alias, netrom.AliasLen)
	}

	return callsign, alias, nil
}

// netromSharedNames are the callsign and alias of a NET/ROM node sharing
// shell's, which settings must leave out.
func (ps *parseState) netromSharedNames(shell nodeSettings, settings NetROMSettings) (string, string, error) {
	if settings.Callsign != "" || settings.Alias != "" {
		return "", "", fmt.Errorf("line %d: With a node, NET/ROM takes the node's address and alias. Leave out callsign and alias", ps.line)
	}

	var callsign, callErr = netrom.NormaliseCallsign(shell.address)
	if callErr != nil {
		return "", "", fmt.Errorf("line %d: Node address %s can't be a NET/ROM callsign: %w", ps.line, shell.address, callErr)
	}

	if shell.alias == "" || !netrom.ValidAlias(shell.alias) {
		return "", "", fmt.Errorf("line %d: NET/ROM needs the node to have an alias of 1 to %d characters, any SSID included",
			ps.line, netrom.AliasLen)
	}

	return callsign, shell.alias, nil
}

// netromByte is the setting named name, if given, which must be from least
// to 255, or otherwise unset.
func netromByte(line int, name string, setting *int, least int, unset byte) (byte, error) {
	if setting == nil {
		return unset, nil
	}

	if *setting < least || *setting > 255 {
		return 0, fmt.Errorf("line %d: Invalid NET/ROM %s %d. Must be in range of %d to 255", line, name, *setting, least)
	}

	return byte(*setting & 0xFF), nil
}

// netromConfig is the NET/ROM node's configuration for settings, with the
// channels as they are now, or what is wrong with it.  One sharing the node
// shell's names must be on the shell's channels, which is where the shell
// answers for both, and given none runs on all of them.  One with names of
// its own, given none, runs on every channel connected mode can be used on.
func (c *RadioConfig) netromConfig(settings netromSettings, shell maybe.Maybe[nodeSettings]) (netrom.Config, error) {
	var none netrom.Config

	var config = settings.config

	if shellSettings, ok := shell.Get(); ok && settings.integrated {
		var channels = shellSettings.channels
		if channels == nil {
			channels = c.connectedModeChannels()
		}

		for _, channel := range config.Channels {
			if !slices.Contains(channels, channel) {
				return none, fmt.Errorf("line %d: NET/ROM can't run on channel %d, which the node does not listen on",
					settings.line, channel)
			}
		}

		if len(config.Channels) == 0 {
			config.Channels = slices.Clone(channels)
		}
	}

	if len(config.Channels) == 0 && !settings.integrated {
		config.Channels = c.connectedModeChannels()
	}

	if len(config.Channels) == 0 {
		return none, fmt.Errorf("line %d: NET/ROM has no channels to run on", settings.line)
	}

	for _, channel := range config.Channels {
		if !c.connectedModeAllowed(channel) {
			return none, fmt.Errorf("line %d: NET/ROM can't run on channel %d, which is not a channel connected mode can be used on",
				settings.line, channel)
		}
	}

	return config, nil
}

// netromAGWDataLink is the data link as the AGW server sees it when there is
// a NET/ROM node: what is the node's - a connect with the NET/ROM PID on one
// of its channels, data for, disconnection of and outstanding frames on a client's
// circuits, registration of the node's callsign, a client going away - goes
// to the node, and everything else to the data link as before.
type netromAGWDataLink struct {
	agwDataLink

	// node is set once the node is made, which is after the AGW server
	// it reports to, and so after this: the one cycle in the wiring.  It is
	// set before the AGW server starts taking clients.
	node *netrom.Node
}

func newNetromAGWDataLink(link agwDataLink) *netromAGWDataLink {
	var l = new(netromAGWDataLink)
	l.agwDataLink = link

	return l
}

func (l *netromAGWDataLink) ConnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int) {
	if numAddr == 2 && l.node.Connect(channel, client, addrs[ax25.Source], addrs[ax25.Destination], pid) {
		return
	}

	l.agwDataLink.ConnectRequest(addrs, numAddr, channel, client, pid)
}

func (l *netromAGWDataLink) XmitDataRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int, xdata []byte) {
	if l.node.Send(channel, client, addrs[ax25.Source], addrs[ax25.Destination], xdata) {
		return
	}

	l.agwDataLink.XmitDataRequest(addrs, numAddr, channel, client, pid, xdata)
}

// OutstandingFramesRequest answers for a client's circuit from the node, as
// the data link answers for its links, so that a client pacing itself by the
// answer is not told its circuit has nothing waiting when it has.
func (l *netromAGWDataLink) OutstandingFramesRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int) {
	if l.node.Outstanding(channel, client, addrs[ax25.Source], addrs[ax25.Destination]) {
		return
	}

	l.agwDataLink.OutstandingFramesRequest(addrs, numAddr, channel, client)
}

func (l *netromAGWDataLink) DisconnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int) {
	if l.node.Disconnect(channel, client, addrs[ax25.Source], addrs[ax25.Destination]) {
		return
	}

	l.agwDataLink.DisconnectRequest(addrs, numAddr, channel, client)
}

// RegisterCallsign keeps a client's registration of the node's callsign
// with the node, which is how the client takes the node's incoming circuits,
// rather than passing it to the data link, which would then hand the
// client the links the node's neighbours make to it.
func (l *netromAGWDataLink) RegisterCallsign(addr string, channel int, client int) {
	if l.node.Register(channel, client, addr) {
		return
	}

	l.agwDataLink.RegisterCallsign(addr, channel, client)
}

func (l *netromAGWDataLink) UnregisterCallsign(addr string, channel int, client int) {
	if l.node.Unregister(channel, client, addr) {
		return
	}

	l.agwDataLink.UnregisterCallsign(addr, channel, client)
}

func (l *netromAGWDataLink) ClientCleanup(client int) {
	l.node.ClientGone(client)
	l.agwDataLink.ClientCleanup(client)
}
