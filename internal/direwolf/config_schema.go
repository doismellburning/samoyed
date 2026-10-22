// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

// The settings a configuration directive describes, apart from the syntax it
// was written in.  Each converted directive is read in two steps: a parse step
// that turns the words of a configuration file line into one of these, and an
// apply step that checks it and writes it into the configuration.  The apply
// step is where all the validation lives, so that another way of writing the
// same settings down can share it rather than repeat it.

import "github.com/doismellburning/samoyed/internal/axudp"

// AudioDeviceSettings describes an audio device - ADEVICE.
type AudioDeviceSettings struct {
	// Device is the device number, 0 to MAX_ADEVS-1.  A YAML file gives it
	// as AudioDeviceConfig.Device, where it can be left out.
	Device int `yaml:"-"`

	// Input names the device to receive from.
	Input string `yaml:"input"`

	// Output names the device to transmit to, when that is different from
	// Input.
	Output string `yaml:"output"`
}

// OutputControlSettings describes how a channel drives one of its output
// controls - PTT, DCD or CON.
type OutputControlSettings struct {
	// Method is one of serial, gpio, gpiod, lpt, rig or cm108.
	Method string `yaml:"method"`

	// Device is the serial port for serial, the GPIO chip for gpiod, the
	// port hamlib talks to the rig on for rig, and the HID device for cm108.
	Device string `yaml:"device"`

	// Line is the serial control line, rts or dtr, and Line2 an optional
	// second one on the same port.
	Line  string `yaml:"line"`
	Line2 string `yaml:"line2"`

	// Pin is the GPIO number for gpio, gpiod and cm108, and the bit number
	// for lpt.  Left out for cm108, it is 3, which all known designs use.
	Pin *int `yaml:"pin"`

	// Invert drives Pin, or Line, low rather than high to transmit;
	// Invert2 does the same for Line2.
	Invert  bool `yaml:"invert"`
	Invert2 bool `yaml:"invert2"`

	// Model is the hamlib rig model number, or "auto", for rig.
	Model string `yaml:"model"`

	// Rate is the serial port speed for rig, when hamlib's default will not
	// do.  0 asks for hamlib's default.
	Rate *int `yaml:"rate"`
}

// ModemSettings describes a channel's modem - MODEM.
type ModemSettings struct {
	// Speed is the data rate in bits per second, or AIS or EAS.
	Speed string `yaml:"speed"`

	// Type forces bpsk or g3ruh regardless of the default for Speed.
	Type string `yaml:"type"`

	// Tones are the AFSK tones.  Both 0 means G3RUH.
	Tones *ModemTones `yaml:"tones"`

	// Decoders runs several decoders on slightly different frequencies.
	Decoders *ModemDecoders `yaml:"decoders"`

	// V26 is the V.26 alternative for 2400 bps PSK: a for the original, b
	// for compatibility with the MFJ-2400.
	V26 string `yaml:"v26"`

	// Divide is the sample rate division factor, 1 to 8.
	Divide *int `yaml:"divide"`

	// Upsample is the upsample ratio for G3RUH, 1 to 4.
	Upsample *int `yaml:"upsample"`

	// Profiles are the letters, plus and minus picking demodulator profiles.
	Profiles string `yaml:"profiles"`
}

// ModemTones are an AFSK modem's mark and space tones, in Hz.
type ModemTones struct {
	Mark  int `yaml:"mark"`
	Space int `yaml:"space"`
}

// ModemDecoders runs Count decoders, Offset Hz apart.
type ModemDecoders struct {
	Count  int `yaml:"count"`
	Offset int `yaml:"offset"`
}

// IL2PTXSettings describes a channel's IL2P transmission - IL2PTX.
type IL2PTXSettings struct {
	// Invert inverts the polarity.  Do not use it for 1200 bps.
	Invert bool `yaml:"invert"`

	// MaxFEC asks for the stronger FEC; false asks for the weaker one, which
	// only IL2P v0.4 has.  Left out, it is true.
	MaxFEC *bool `yaml:"maxfec"`

	// CRC adds the trailing CRC.  Left out, it is true.
	CRC *bool `yaml:"crc"`
}

// NetworkTNCSettings describes a network TNC: a virtual channel whose frames
// go to and come from a KISS TNC over TCP - NCHANNEL.
type NetworkTNCSettings struct {
	// Channel is the virtual channel.
	Channel *int `yaml:"channel"`

	// Host is the TNC's hostname or IP address.
	Host string `yaml:"host"`

	// Port is the TNC's KISS TCP port.
	Port *int `yaml:"port"`

	// KISSPort is the KISS port on the TNC, 0 to 15: frames are sent to it,
	// and frames from the TNC's other KISS ports are dropped.  Left out, it
	// is 0.
	KISSPort int `yaml:"kissPort"`
}

// AXUDPPortSettings describes an AXUDP port: a virtual channel whose frames
// go to and come from other nodes as UDP datagrams.
type AXUDPPortSettings struct {
	// Port is the local UDP port, both listened on and sent from.
	Port int `yaml:"port"`

	// Channel is the virtual channel, like NCHANNEL's.
	Channel *int `yaml:"channel"`

	// Broadcast lists the destination addresses, such as NODES, that go to
	// every map marked Broadcast.
	Broadcast []string `yaml:"broadcast"`

	// Maps say which node gets frames for which address.
	Maps []axudp.MapSettings `yaml:"maps"`

	// AutoMap sends frames for an address no map covers to wherever that
	// address was last heard from, like BPQ's AUTOADDMAP.
	AutoMap bool `yaml:"automap"`
}

// KISSPortSettings describes a KISS TCP port - KISSPORT.
type KISSPortSettings struct {
	// Port is the TCP port number.  0 removes the default port.
	Port int `yaml:"port"`

	// Channel, when given, restricts the port to that one radio channel.
	Channel *int `yaml:"channel"`
}

// NodeSettings describes the node shell, which a station reaches by
// connecting to Address or Alias.
type NodeSettings struct {
	// Address is the node's address: its callsign, with any SSID.
	Address string `yaml:"address"`

	// Alias is a second name the node answers to, such as a short
	// mnemonic for where it is.  Left out, there is none.
	Alias string `yaml:"alias"`

	// Channels are the channels the node accepts connections on.  Left
	// out, it is every channel connected mode can be used on.
	Channels []int `yaml:"channels"`

	// InfoMessage is what the INFO command says about the node.
	InfoMessage string `yaml:"infoMessage"`

	// ConnectMessage is what a station is told on connecting to the node.
	ConnectMessage string `yaml:"connectMessage"`

	// Map, when given, reports the node to the UK Packet Radio network
	// map.
	Map *NodeMapSettings `yaml:"map"`

	// NetROM, when given, runs a NET/ROM node under the node's own address
	// and alias, so that users and neighbouring nodes connect to the one
	// node.
	NetROM *NetROMRouting `yaml:"netrom"`
}

// NetROMSettings describes a NET/ROM node on a station without a node shell,
// which carries traffic between nodes over circuits that may cross several of
// them.  With a node shell, NET/ROM is configured with it.
type NetROMSettings struct {
	NetROMRouting `yaml:",inline"`

	// Callsign is the node's AX.25 address, conventionally the station's
	// callsign with an SSID of its own.
	Callsign string `yaml:"callsign"`

	// Alias is the short name, of up to six characters, other nodes know
	// it by.
	Alias string `yaml:"alias"`
}

// NetROMRouting describes how a NET/ROM node routes, whether it has a
// callsign and alias of its own or shares the node shell's.
type NetROMRouting struct {
	// Channel is the channel the node runs on: a radio channel or an
	// NCHANNEL.  Sharing the node shell's names, it can be left out if the
	// shell listens on only one channel.
	Channel *int `yaml:"channel"`

	// TTL is how many nodes a frame the node originates may cross.  Left
	// out, it is 16.
	TTL *int `yaml:"ttl"`

	// NodesInterval is the number of seconds between NODES broadcasts.
	// Left out, it is 3600.
	NodesInterval *int `yaml:"nodesInterval"`

	// Quality, from 0 to 255, is what the node assumes for its link to any
	// neighbour it hears.  Left out, it is 192.
	Quality *int `yaml:"quality"`

	// MinQuality is the least quality a route is worth keeping at.  Left
	// out, it is 10.
	MinQuality *int `yaml:"minQuality"`
}

// NodeMapSettings describes how the node is reported to the UK Packet Radio
// network map.
type NodeMapSettings struct {
	// APIURL is the base URL of the map's API.  Left out, it is the public
	// map's.
	APIURL string `yaml:"apiUrl"`

	// Locator is the Maidenhead locator of where the node is.  Left out,
	// the map is not told.
	Locator string `yaml:"locator"`

	// ASL is the height of the node's antenna above sea level, in metres.
	// It needs Locator.  Left out, the map is not told.
	ASL *int `yaml:"asl"`

	// Contact is how to reach whoever runs the node.
	Contact string `yaml:"contact"`

	// Comment is a short note about the node.
	Comment string `yaml:"comment"`

	// Ports, keyed by channel, say what the map is told about the node's
	// ports where that is not what the channel's configuration suggests,
	// or is more than it can.
	Ports map[int]NodeMapPortSettings `yaml:"ports"`

	// Aliases, keyed by alias, are the services the node offers besides
	// itself, such as a BBS that another program runs over AGW.
	Aliases map[string]NodeMapServiceSettings `yaml:"aliases"`
}

// NodeMapServiceSettings describes a service the node offers to the node
// map.
type NodeMapServiceSettings struct {
	// Service is what it is, such as BBS or CHAT.
	Service string `yaml:"service"`

	// Callsign is the address it answers to, with any SSID.
	Callsign string `yaml:"callsign"`
}

// NodeMapPortSettings describes one of the node's ports to the node map.  A
// setting left out keeps what the channel's configuration suggests.
type NodeMapPortSettings struct {
	// Report, when false, leaves the port off the map.
	Report *bool `yaml:"report"`

	// LinkType is RF, PrivateNet, Internet or Other.
	LinkType *string `yaml:"linkType"`

	// Freq is the port's frequency, in Hz.
	Freq *int64 `yaml:"freq"`

	// Mode is the layer 2 protocol, such as AX.25 or IL2P.
	Mode *string `yaml:"mode"`

	// Modulation is such as AFSK, FSK or QPSK.
	Modulation *string `yaml:"modulation"`

	// Baud is symbols per second.
	Baud *int `yaml:"baud"`

	// Bitrate is bits per second.
	Bitrate *int `yaml:"bitrate"`

	// Usage is what the port is for, such as Access or Backbone.
	Usage string `yaml:"usage"`

	// Comment is a short note about the port.
	Comment string `yaml:"comment"`
}
