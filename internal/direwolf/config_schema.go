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
}

// KISSPortSettings describes a KISS TCP port - KISSPORT.
type KISSPortSettings struct {
	// Port is the TCP port number.  0 removes the default port.
	Port int `yaml:"port"`

	// Channel, when given, restricts the port to that one radio channel.
	Channel *int `yaml:"channel"`
}
