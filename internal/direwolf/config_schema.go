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
