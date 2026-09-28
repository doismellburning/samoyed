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
