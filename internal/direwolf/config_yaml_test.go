// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// parseYAMLConfig runs config_init over content as a YAML configuration file.
func parseYAMLConfig(t *testing.T, content string) configs {
	t.Helper()

	return parseConfigNamed(t, "samoyed*.yaml", content)
}

// Each YAML configuration here says the same as the legacy one beside it, so
// both must come out as the same configuration, with the same complaints.
func Test_config_yaml_matches_legacy(t *testing.T) {
	var tests = []struct {
		name   string
		legacy string
		yaml   string
	}{
		{
			name:   "empty",
			legacy: "",
			yaml:   "",
		},
		{
			name:   "MYCALL fills every channel",
			legacy: "MYCALL Q1TEST-1\n",
			yaml: `
channels:
  - channel: 0
    mycall: q1test-1
`,
		},
		{
			name: "stereo device with two channels",
			legacy: `
ADEVICE plughw:1,0 plughw:2,0
ACHANNELS 2
CHANNEL 0
MYCALL Q1TEST
MODEM 1200 1600:1800 3@30 A+
TXDELAY 30
PTT /dev/ttyUSB0 RTS -DTR
CHANNEL 1
MYCALL Q2TEST
MODEM 9600 G3RUH /2 *3
PTT GPIO -25
DCD GPIOD gpiochip0 4
CON LPT 2
`,
			yaml: `
audioDevices:
  - input: plughw:1,0
    output: plughw:2,0
    channels: 2
channels:
  - channel: 0
    mycall: Q1TEST
    modem:
      speed: "1200"
      tones: {mark: 1600, space: 1800}
      decoders: {count: 3, offset: 30}
      profiles: A+
    txdelay: 30
    ptt: {method: serial, device: /dev/ttyUSB0, line: rts, line2: dtr, invert2: true}
  - channel: 1
    mycall: Q2TEST
    modem: {speed: "9600", type: g3ruh, divide: 2, upsample: 3}
    ptt: {method: gpio, pin: 25, invert: true}
    dcd: {method: gpiod, device: gpiochip0, pin: 4}
    con: {method: lpt, pin: 2}
`,
		},
		{
			name:   "second audio device by number",
			legacy: "ADEVICE1 plughw:3,0\nCHANNEL 2\nMYCALL Q1TEST\n",
			yaml: `
audioDevices:
  - device: 1
    input: plughw:3,0
channels:
  - {channel: 2, mycall: Q1TEST}
`,
		},
		{
			name:   "hamlib PTT",
			legacy: "PTT RIG AUTO /dev/ttyS0 9600\n",
			yaml:   "channels:\n  - {channel: 0, ptt: {method: rig, model: auto, device: /dev/ttyS0, rate: 9600}}\n",
		},
		{
			name:   "CM108 PTT",
			legacy: "PTT CM108 -5 /dev/hidraw1\n",
			yaml:   "channels:\n  - {channel: 0, ptt: {method: cm108, pin: 5, invert: true, device: /dev/hidraw1}}\n",
		},
		{
			name:   "AIS",
			legacy: "MODEM AIS\n",
			yaml:   "channels:\n  - {channel: 0, modem: {speed: AIS}}\n",
		},
		{
			name:   "EAS",
			legacy: "MODEM EAS\n",
			yaml:   "channels:\n  - {channel: 0, modem: {speed: EAS}}\n",
		},
		{
			name:   "V.26 alternative",
			legacy: "MODEM 2400 V26B\n",
			yaml:   "channels:\n  - {channel: 0, modem: {speed: \"2400\", v26: b}}\n",
		},
		{
			name:   "BPSK",
			legacy: "MODEM 1200 BPSK\n",
			yaml:   "channels:\n  - {channel: 0, modem: {speed: \"1200\", type: bpsk}}\n",
		},
		{
			name:   "unreasonable values are corrected the same way",
			legacy: "MYCALL !BAD!\nTXDELAY 300\nMODEM 1200 99:9999 9@1 /9 *9\n",
			yaml: `
channels:
  - channel: 0
    mycall: "!BAD!"
    txdelay: 300
    modem:
      speed: "1200"
      tones: {mark: 99, space: 9999}
      decoders: {count: 9, offset: 1}
      divide: 9
      upsample: 9
`,
		},
		{
			name:   "a missing PTT device is refused the same way",
			legacy: "PTT RIG 101\n",
			yaml:   "channels:\n  - {channel: 0, ptt: {method: rig, model: \"101\"}}\n",
		},
		{
			name:   "directives without a YAML form yet go in the legacy block",
			legacy: "MYCALL Q1TEST\nAGWPORT 8010\nKISSPORT 8011\nCHANNEL 0\nDWAIT 5\n",
			yaml: `
channels:
  - {channel: 0, mycall: Q1TEST}
legacy: |
  AGWPORT 8010
  # A comment, as in any other configuration file
  KISSPORT 8011
  CHANNEL 0
  DWAIT 5
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var want = parseConfig(t, tt.legacy)
			var got = parseYAMLConfig(t, tt.yaml)

			assert.Equal(t, want.audio, got.audio)
			assert.Equal(t, want.misc, got.misc)
			assert.Equal(t, want.errors, got.errors, "errors\nlegacy:\n%s\nyaml:\n%s", want.output, got.output)
			assert.Equal(t, want.warnings, got.warnings, "warnings\nlegacy:\n%s\nyaml:\n%s", want.output, got.output)
			assert.Equal(t, want.fatal, got.fatal)
		})
	}
}

func Test_config_yaml(t *testing.T) {
	t.Run("a .yml file is YAML too", func(t *testing.T) {
		var c = parseConfigNamed(t, "samoyed*.yml", "channels:\n  - {channel: 0, mycall: Q1TEST}\n")
		assert.Equal(t, "Q1TEST", c.audio.mycall[0])
		assert.Zero(t, c.errors)
	})

	t.Run("a misspelt key is refused, not ignored", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - channel: 0\n    mycal: Q1TEST\n")
		assert.True(t, c.fatal)
		assert.Contains(t, c.output, "line 3")
		assert.Contains(t, c.output, "mycal")
		assert.True(t, IsNoCall(c.audio.mycall[0]))
	})

	t.Run("a value of the wrong type is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, txdelay: soon}\n")
		assert.True(t, c.fatal)
		assert.Contains(t, c.output, "line 2")
	})

	t.Run("a complaint points at the setting's line", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - channel: 0\n    mycall: Q1TEST\n    txdelay: 300\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Line 4: Invalid time for transmit delay")
	})

	t.Run("a complaint in the legacy block points at the file's line", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0}\nlegacy: |\n  AGWPORT 8010\n  BOGUS\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Unrecognized command 'BOGUS' on line 5")
	})

	t.Run("a device defined in both places is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "audioDevices:\n  - input: plughw:1,0\nlegacy: |\n  ADEVICE plughw:2,0\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "ADEVICE0 can't be defined more than once. Line 4")
		assert.Equal(t, "plughw:1,0", c.audio.adev[0].adevice_in)
	})

	t.Run("settings for a channel that is not there go nowhere", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 1, mycall: Q1TEST, txdelay: 20}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Channel number 1 is not valid")
		assert.True(t, IsNoCall(c.audio.mycall[0]))
		assert.NotEqual(t, 20, c.audio.achan[0].txdelay)
	})

	t.Run("a channel needs a number", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {mycall: Q1TEST}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Missing channel number")
		assert.True(t, IsNoCall(c.audio.mycall[0]))
	})

	// Regression test: a negative pin reached the configuration as it was,
	// and PTT shifts by the LPT bit, which panics when it is negative.
	t.Run("a negative pin is refused", func(t *testing.T) {
		for _, method := range []string{"gpio", "lpt"} {
			var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, ptt: {method: "+method+", pin: -2}}\n")
			assert.Equal(t, 1, c.errors, method)
			assert.Contains(t, c.output, "can't be negative", method)
			assert.Equal(t, PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method, method)
		}
	})

	// Regression test: invert was only honoured along with a pin, so the
	// default CM108 GPIO was driven high to transmit when asked for low.
	t.Run("CM108 inverts its default GPIO", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, ptt: {method: cm108, device: /dev/hidraw1, invert: true}}\n")
		var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
		assert.Equal(t, PTT_METHOD_CM108, octrl.ptt_method)
		assert.Equal(t, 3, octrl.out_gpio_num)
		assert.True(t, octrl.ptt_invert)
	})

	// Regression test: a rejected device's channels were still applied, to
	// whichever device the rejection left current.
	t.Run("a rejected device's channels go nowhere", func(t *testing.T) {
		for _, device := range []string{
			"  - {device: 9, input: hw, channels: 2}\n",
			"  - {input: hw}\n  - {device: 0, input: hw2, channels: 2}\n",
			"  - {channels: 2}\n",
		} {
			var c = parseYAMLConfig(t, "audioDevices:\n"+device)
			assert.Equal(t, 1, c.errors, device)
			assert.Equal(t, 1, c.audio.adev[0].num_channels, device)
			assert.Equal(t, MEDIUM_NONE, c.audio.chan_medium[1], device)
		}
	})

	// Regression test: only the first document was read, so anything after
	// it - settings, or even a mistake - went unnoticed.
	t.Run("a second document is refused", func(t *testing.T) {
		for _, rest := range []string{
			"---\nchannels:\n  - {channel: 0, txdelay: 20}\n",
			"---\n: : :\n",
		} {
			var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, mycall: Q1TEST}\n"+rest)
			assert.True(t, c.fatal, rest)
			assert.True(t, IsNoCall(c.audio.mycall[0]), rest)
		}
	})

	// Regression test: a zero or empty value was taken for a setting that was
	// left out, rather than checked like any other.
	t.Run("a zero or empty setting is checked, not ignored", func(t *testing.T) {
		var c = parseYAMLConfig(t, "audioDevices:\n  - {input: hw, channels: 0}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Number of audio channels must be 1 or 2")

		c = parseYAMLConfig(t, "channels:\n  - {channel: 0, mycall: \"\"}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Missing value for MYCALL command on line 2")

		c = parseYAMLConfig(t, "channels:\n  - {channel: 0, ptt: {method: rig, model: auto, device: /dev/ttyS0, rate: -1}}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "can't be negative")
	})

	// Regression test: an entry written as an alias, or merging in another,
	// had no keys to be found, so it was said to have no channel number.
	t.Run("aliases and merges are followed", func(t *testing.T) {
		var want = parseConfig(t, "MYCALL Q1TEST\nTXDELAY 20\n")

		// An anchor has to come before its alias, and the top level has no
		// spare key to put one under, so these anchor an earlier entry of
		// the same list.  Applying an entry twice changes nothing.
		for _, content := range []string{
			"channels:\n  - &base {channel: 0, mycall: Q1TEST, txdelay: 20}\n  - *base\n",
			"channels:\n  - &base {channel: 0, mycall: Q1TEST}\n  - {<<: *base, txdelay: 20}\n",
		} {
			var got = parseYAMLConfig(t, content)
			assert.Zero(t, got.errors, got.output)
			assert.Equal(t, want.audio, got.audio, content)
		}
	})

	t.Run("an unknown PTT method is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, ptt: {method: pigeon}}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Unknown PTT method \"pigeon\"")
		assert.Equal(t, PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
	})

	t.Run("an unknown modem type is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, modem: {speed: \"1200\", type: morse}}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Unrecognized modem type")
	})

	t.Run("a file that is not YAML is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "MYCALL Q1TEST\n")
		assert.True(t, c.fatal)
		assert.True(t, IsNoCall(c.audio.mycall[0]))
	})
}
