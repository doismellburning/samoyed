// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// The settings a configuration directive describes, apart from the syntax it
// was written in.  Each converted directive is read in two steps: a parse step
// that turns the words of a configuration file line into one of these, and an
// apply step that checks it and writes it into the configuration.  The apply
// step is where all the validation lives, so that another way of writing the
// same settings down can share it rather than repeat it.

// AudioDeviceSettings describes an audio device - ADEVICE.
type AudioDeviceSettings struct {
	// Device is the device number, 0 to MAX_ADEVS-1.
	Device int `yaml:"device"`

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
