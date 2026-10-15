// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustNodeConfig is the node configuration for settings, which must be
// one the channels allow.
func mustNodeConfig(t *testing.T, audio *RadioConfig, settings nodeSettings) node.Config {
	t.Helper()

	var config, err = audio.nodeConfig(settings)
	require.NoError(t, err)

	return config
}

// parseYAMLConfig runs config_init over content, put under the top-level
// "samoyed" key, as a YAML configuration file.  That key takes the first line,
// so a line in content is a line further on in the file.
func parseYAMLConfig(t *testing.T, content string) configs {
	t.Helper()

	return parseConfigNamed(t, "samoyed*.yaml", underSamoyed(content))
}

// underSamoyed puts content under a top-level "samoyed" key.
func underSamoyed(content string) string {
	var lines = strings.Split(strings.TrimPrefix(content, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "  " + line
		}
	}

	return "samoyed:\n" + strings.Join(lines, "\n")
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
			name:   "audio sample rate",
			legacy: "ADEVICE plughw:1,0\nARATE 48000\n",
			yaml:   "audioDevices:\n  - {input: \"plughw:1,0\", rate: 48000}\n",
		},
		{
			name: "channel timing",
			legacy: `
ADEVICE hw
ACHANNELS 2
CHANNEL 0
DWAIT 3
SLOTTIME 20
PERSIST 127
TXTAIL 15
FULLDUP ON
CHANNEL 1
FULLDUP OFF
`,
			yaml: `
audioDevices:
  - {input: hw, channels: 2}
channels:
  - {channel: 0, dwait: 3, slottime: 20, persist: 127, txtail: 15, fulldup: true}
  - {channel: 1, fulldup: false}
`,
		},
		{
			name:   "unreasonable timing is corrected the same way",
			legacy: "DWAIT 300\nSLOTTIME 100\nPERSIST 1\nTXTAIL 300\n",
			yaml:   "channels:\n  - {channel: 0, dwait: 300, slottime: 100, persist: 1, txtail: 300}\n",
		},
		{
			name:   "questionable timing is warned about the same way",
			legacy: "TXTAIL 2\n",
			yaml:   "channels:\n  - {channel: 0, txtail: 2}\n",
		},
		{
			name:   "FX.25",
			legacy: "FX25TX 16\n",
			yaml:   "channels:\n  - {channel: 0, fx25tx: 16}\n",
		},
		{
			name:   "FX.25 off",
			legacy: "FX25TX 16\nFX25TX 0\n",
			yaml:   "channels:\n  - {channel: 0, fx25tx: 0}\n",
		},
		{
			name:   "unreasonable FX.25 is corrected the same way",
			legacy: "FX25TX 999\n",
			yaml:   "channels:\n  - {channel: 0, fx25tx: 999}\n",
		},
		{
			name:   "IL2P with the defaults",
			legacy: "IL2PTX\n",
			yaml:   "channels:\n  - {channel: 0, il2ptx: {}}\n",
		},
		{
			name:   "IL2P with everything changed",
			legacy: "IL2PTX -0c\nIL2PVERSION 0.4\n",
			yaml:   "channels:\n  - {channel: 0, il2ptx: {invert: true, maxfec: false, crc: false}, il2pversion: 0.4}\n",
		},
		{
			name:   "IL2P compatibility",
			legacy: "IL2PVERSION COMPAT\n",
			yaml:   "channels:\n  - {channel: 0, il2pversion: compat}\n",
		},
		{
			name:   "an unknown IL2P version is refused the same way",
			legacy: "IL2PVERSION 0.5\n",
			yaml:   "channels:\n  - {channel: 0, il2pversion: \"0.5\"}\n",
		},
		{
			name:   "network ports",
			legacy: "AGWPORT 8010\nKISSPORT 0\nKISSPORT 8011\nKISSPORT 8012 1\n",
			yaml:   "agwPort: 8010\nkissPorts:\n  - port: 0\n  - port: 8011\n  - {port: 8012, channel: 1}\n",
		},
		{
			name:   "AGW disabled",
			legacy: "AGWPORT 0\n",
			yaml:   "agwPort: 0\n",
		},
		{
			name:   "unreasonable ports are refused the same way",
			legacy: "AGWPORT 99\nKISSPORT 99\nKISSPORT 8011 99\n",
			yaml:   "agwPort: 99\nkissPorts:\n  - port: 99\n  - {port: 8011, channel: 99}\n",
		},
		{
			name:   "a missing PTT device is refused the same way",
			legacy: "PTT RIG 101\n",
			yaml:   "channels:\n  - {channel: 0, ptt: {method: rig, model: \"101\"}}\n",
		},
		{
			name:   "directives without a YAML form yet go in the legacy block",
			legacy: "MYCALL Q1TEST\nDEDUPE 20\nMETRICSPORT 9100\nCHANNEL 0\nTXINH GPIO 5\n",
			yaml: `
channels:
  - {channel: 0, mycall: Q1TEST}
legacy: |
  DEDUPE 20
  # A comment, as in any other configuration file
  METRICSPORT 9100
  CHANNEL 0
  TXINH GPIO 5
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
	t.Run("an AXUDP port makes its channel an AXUDP one", func(t *testing.T) {
		var c = parseYAMLConfig(t, `
axudpPorts:
  - port: 20093
    channel: 10
    broadcast: [nodes]
    maps:
      - {ax25addr: q1test-1, host: 192.0.2.1, port: 93, broadcast: true}
      - {ax25addr: Q2TEST, host: 192.0.2.2, port: 10093}
`)
		assert.Zero(t, c.errors, c.output)
		assert.Equal(t, MEDIUM_AXUDP, c.audio.chan_medium[10])
		assert.Equal(t, 20093, c.audio.axudp_port[10])

		var routes = c.audio.axudp_routes[10]
		assert.Equal(t, []string{"NODES"}, routes.Broadcast)
		if assert.Len(t, routes.Maps, 2) {
			assert.Equal(t, "Q1TEST-1", routes.Maps[0].AX25Addr)
			assert.Equal(t, "192.0.2.1:93", routes.Maps[0].Addr)
			assert.True(t, routes.Maps[0].Broadcast)
			assert.Equal(t, "Q2TEST", routes.Maps[1].AX25Addr)
			assert.False(t, routes.Maps[1].Broadcast)
		}
	})

	t.Run("an AXUDP port can learn routes", func(t *testing.T) {
		var c = parseYAMLConfig(t, "axudpPorts:\n  - {port: 20093, channel: 10, automap: true}\n")
		assert.Zero(t, c.errors, c.output)
		assert.True(t, c.audio.axudp_routes[10].AutoMap)
	})

	t.Run("a bad AXUDP port is refused and claims no channel", func(t *testing.T) {
		for _, tc := range []struct {
			name, entry, want string
		}{
			{"no port", "{channel: 10}", "Invalid UDP port number 0"},
			{"port out of range", "{port: 70000, channel: 10}", "Invalid UDP port number 70000"},
			{"no channel", "{port: 20093}", "Missing channel number"},
			{"radio channel", "{port: 20093, channel: 0}", "must be in range"},
			{"channel out of range", "{port: 20093, channel: 99}", "must be in range"},
			{"map with no address", "{port: 20093, channel: 10, maps: [{host: 192.0.2.1, port: 93}]}", "ax25addr is empty"},
			{"map with no host", "{port: 20093, channel: 10, maps: [{ax25addr: Q1TEST, port: 93}]}", "host is empty"},
			{"map with bad port", "{port: 20093, channel: 10, maps: [{ax25addr: Q1TEST, host: 192.0.2.1}]}", "port 0 out of range"},
			{"empty broadcast address", "{port: 20093, channel: 10, broadcast: ['']}", "broadcast address 0 is empty"},
		} {
			var c = parseYAMLConfig(t, "axudpPorts:\n  - "+tc.entry+"\n")
			assert.Equal(t, 1, c.errors, tc.name)
			assert.Contains(t, c.output, "Line 3", tc.name)
			assert.Contains(t, c.output, tc.want, tc.name)
			assert.NotContains(t, c.audio.chan_medium, MEDIUM_AXUDP, tc.name)
		}
	})

	t.Run("an AXUDP port can't take a channel already in use", func(t *testing.T) {
		var c = parseYAMLConfig(t, "axudpPorts:\n  - {port: 20093, channel: 10}\n  - {port: 20094, channel: 10}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Line 4")
		assert.Contains(t, c.output, "already in use")
		assert.Equal(t, 20093, c.audio.axudp_port[10])
	})

	t.Run("an AXUDP channel can be digipeated and filtered like a network TNC", func(t *testing.T) {
		var c = parseYAMLConfig(t, `
channels:
  - {channel: 0, mycall: Q1TEST}
axudpPorts:
  - {port: 20093, channel: 10}
legacy: |
  DIGIPEAT 10 0 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$
  DIGIPEAT 0 10 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$
  FILTER 10 0 t/m
  FILTER 0 10 t/m
`)
		assert.Zero(t, c.errors, c.output)
		assert.Equal(t, "t/m", c.digi.FilterStr[10][0])
		assert.Equal(t, "t/m", c.digi.FilterStr[0][10])
	})

	t.Run("two AXUDP ports can't share a UDP port", func(t *testing.T) {
		var c = parseYAMLConfig(t, "axudpPorts:\n  - {port: 20093, channel: 10}\n  - {port: 20093, channel: 11}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "already used by the AXUDP port for channel 10")
		assert.Equal(t, MEDIUM_NONE, c.audio.chan_medium[11])
	})

	t.Run("the node listens on every connected mode channel by default", func(t *testing.T) {
		var c = parseYAMLConfig(t, "axudpPorts:\n  - {port: 20093, channel: 10}\nnode:\n  address: q1test-7\n  alias: samnod\n  info: |\n    Test node\n    in a test\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.node.Get()
		require.True(t, ok)
		assert.Equal(t, node.Config{Address: "Q1TEST-7", Alias: "SAMNOD", Channels: []int{0, 10}, Info: "Test node\nin a test\n"}, mustNodeConfig(t, c.audio, got))
	})

	t.Run("the node listens on the channels it is given", func(t *testing.T) {
		var c = parseYAMLConfig(t, "axudpPorts:\n  - {port: 20093, channel: 10}\nnode: {address: Q1TEST-0, channels: [10]}\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.node.Get()
		require.True(t, ok)
		assert.Equal(t, node.Config{Address: "Q1TEST", Alias: "", Channels: []int{10}, Info: ""}, mustNodeConfig(t, c.audio, got))
	})

	// Regression test: the default channels were settled while the file was
	// read, so a channel the command line adds afterwards - -n 2 makes
	// channel 1 a radio channel - went without the node.
	t.Run("the node's default channels include those the command line adds", func(t *testing.T) {
		var c = parseYAMLConfig(t, "node: {address: Q1TEST}\n")
		assert.Zero(t, c.errors, c.output)

		c.audio.chan_medium[1] = MEDIUM_RADIO

		var got, ok = c.misc.node.Get()
		require.True(t, ok)
		assert.Equal(t, []int{0, 1}, mustNodeConfig(t, c.audio, got).Channels)
	})

	// Regression test: a channel given explicitly was checked while the file
	// was read, so one the command line makes - -n 2 makes channel 1 a radio
	// channel - was refused.
	t.Run("the node can listen on a channel the command line makes", func(t *testing.T) {
		var c = parseYAMLConfig(t, "node: {address: Q1TEST, channels: [0, 1]}\n")
		assert.Zero(t, c.errors, c.output)

		c.audio.chan_medium[1] = MEDIUM_RADIO

		var got, ok = c.misc.node.Get()
		require.True(t, ok)

		var config, err = c.audio.nodeConfig(got)
		require.NoError(t, err)
		assert.Equal(t, []int{0, 1}, config.Channels)
	})

	t.Run("the node can't listen on a channel that is not there", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, mycall: Q1TEST}\nnode: {address: Q1TEST, channels: [5]}\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.node.Get()
		require.True(t, ok)

		var _, err = c.audio.nodeConfig(got)
		require.Error(t, err)
		assert.Equal(t, "line 4: Node can't listen on channel 5, which is not a channel connected mode can be used on", err.Error())
	})

	t.Run("the node can listen on a channel the legacy block makes", func(t *testing.T) {
		var c = parseYAMLConfig(t, "node: {address: Q1TEST, channels: [8]}\nlegacy: |\n  NCHANNEL 8 127.0.0.1 8001\n")
		assert.Zero(t, c.errors, c.output)
		assert.True(t, c.misc.node.IsJust())
	})

	t.Run("no node unless asked for", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, mycall: Q1TEST}\n")
		assert.True(t, c.misc.node.IsNothing())
	})

	t.Run("bad node settings are refused", func(t *testing.T) {
		for _, tc := range []struct {
			name, entry, want string
		}{
			{"no address", "{alias: SAMNOD}", "Invalid address"},
			{"bad address", "{address: Q1TEST-99}", "Invalid address"},
			{"bad alias", "{address: Q1TEST, alias: TOOLONGALIAS}", "Invalid alias"},
			{"alias same as address", "{address: Q1TEST, alias: q1test}", "same as its address"},
			{"channel out of range", "{address: Q1TEST, channels: [-1]}", "Invalid channel -1 for node"},
			{"channel too big", "{address: Q1TEST, channels: [99]}", "Invalid channel 99 for node"},
			{"channel twice", "{address: Q1TEST, channels: [0, 0]}", "more than once"},
			{"no channels", "{address: Q1TEST, channels: []}", "No channels"},
		} {
			var c = parseYAMLConfig(t, "node: "+tc.entry+"\n")
			assert.Equal(t, 1, c.errors, tc.name)
			assert.Contains(t, c.output, "Line 2", tc.name)
			assert.Contains(t, c.output, tc.want, tc.name)
			assert.True(t, c.misc.node.IsNothing(), tc.name)
		}
	})

	t.Run("a .yml file is YAML too", func(t *testing.T) {
		var c = parseConfigNamed(t, "samoyed*.yml", "samoyed:\n  channels:\n    - {channel: 0, mycall: Q1TEST}\n")
		assert.Equal(t, "Q1TEST", c.audio.mycall[0])
		assert.Zero(t, c.errors)
	})

	t.Run("a misspelt key is refused, not ignored", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - channel: 0\n    mycal: Q1TEST\n")
		assert.True(t, c.fatal)
		assert.Contains(t, c.output, "line 4")
		assert.Contains(t, c.output, "mycal")
		assert.True(t, IsNoCall(c.audio.mycall[0]))
	})

	t.Run("a value of the wrong type is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, txdelay: soon}\n")
		assert.True(t, c.fatal)
		assert.Contains(t, c.output, "line 3")
	})

	t.Run("a complaint points at the setting's line", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - channel: 0\n    mycall: Q1TEST\n    txdelay: 300\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Line 5: Invalid time for transmit delay")
	})

	t.Run("a complaint in the legacy block points at the file's line", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0}\nlegacy: |\n  AGWPORT 8010\n  BOGUS\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Unrecognized command 'BOGUS' on line 6")
	})

	t.Run("a device defined in both places is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "audioDevices:\n  - input: plughw:1,0\nlegacy: |\n  ADEVICE plughw:2,0\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "ADEVICE0 can't be defined more than once. Line 5")
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
			assert.Equal(t, PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].Method, method)
		}
	})

	// Regression test: invert was only honoured along with a pin, so the
	// default CM108 GPIO was driven high to transmit when asked for low.
	t.Run("CM108 inverts its default GPIO", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, ptt: {method: cm108, device: /dev/hidraw1, invert: true}}\n")
		var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
		assert.Equal(t, PTT_METHOD_CM108, octrl.Method)
		assert.Equal(t, 3, octrl.GPIONum)
		assert.True(t, octrl.Invert)
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
			"---\nsamoyed:\n  channels:\n    - {channel: 0, txdelay: 20}\n",
			"---\n: : :\n",
		} {
			var c = parseConfigNamed(t, "samoyed*.yaml", "samoyed:\n  channels:\n    - {channel: 0, mycall: Q1TEST}\n"+rest)
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
		assert.Contains(t, c.output, "Missing value for MYCALL command on line 3")

		c = parseYAMLConfig(t, "channels:\n  - {channel: 0, ptt: {method: rig, model: auto, device: /dev/ttyS0, rate: -1}}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "can't be negative")
	})

	// Regression test: an entry written as an alias, or merging in another,
	// had no keys to be found, so it was said to have no channel number.
	t.Run("aliases and merges are followed", func(t *testing.T) {
		var want = parseConfig(t, "MYCALL Q1TEST\nTXDELAY 20\n")

		// An anchor has to come before its alias, and there is no spare key
		// to put one under, so these anchor an earlier entry of the same
		// list.  Applying an entry twice changes nothing.
		for _, content := range []string{
			"channels:\n  - &base {channel: 0, mycall: Q1TEST, txdelay: 20}\n  - *base\n",
			"channels:\n  - &base {channel: 0, mycall: Q1TEST}\n  - {<<: *base, txdelay: 20}\n",
		} {
			var got = parseYAMLConfig(t, content)
			assert.Zero(t, got.errors, got.output)
			assert.Equal(t, want.audio, got.audio, content)
		}
	})

	t.Run("a port complaint points at the setting's line", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0}\nagwPort: 99\nkissPorts:\n  - port: 8011\n  - port: 99\n")
		assert.Equal(t, 2, c.errors)
		assert.Contains(t, c.output, "Line 4: Invalid port number for AGW")
		assert.Contains(t, c.output, "Line 7: Invalid TCP port number for KISS")
	})

	t.Run("an IL2P setting of the wrong type is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, il2ptx: yes please}\n")
		assert.True(t, c.fatal)
		assert.Equal(t, LAYER2_AX25, c.audio.achan[0].layer2_xmit)
	})

	t.Run("an unknown PTT method is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, ptt: {method: pigeon}}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Unknown PTT method \"pigeon\"")
		assert.Equal(t, PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].Method)
	})

	t.Run("an unknown modem type is refused", func(t *testing.T) {
		var c = parseYAMLConfig(t, "channels:\n  - {channel: 0, modem: {speed: \"1200\", type: morse}}\n")
		assert.Equal(t, 1, c.errors)
		assert.Contains(t, c.output, "Unrecognized modem type")
	})

	t.Run("a file that is not YAML is refused", func(t *testing.T) {
		var c = parseConfigNamed(t, "samoyed*.yaml", "MYCALL Q1TEST\n")
		assert.True(t, c.fatal)
		assert.True(t, IsNoCall(c.audio.mycall[0]))
	})

	t.Run("settings outside the samoyed key are refused", func(t *testing.T) {
		var c = parseConfigNamed(t, "samoyed*.yaml", "channels:\n  - {channel: 0, mycall: Q1TEST}\n")
		assert.True(t, c.fatal)
		assert.Contains(t, c.output, "line 1")
		assert.Contains(t, c.output, `top-level "samoyed" key`)
		assert.True(t, IsNoCall(c.audio.mycall[0]))
	})

	t.Run("an empty samoyed key changes nothing", func(t *testing.T) {
		var want = parseConfig(t, "")
		var got = parseConfigNamed(t, "samoyed*.yaml", "samoyed:\n")
		assert.Zero(t, got.errors, got.output)
		assert.Equal(t, want.audio, got.audio)
		assert.Equal(t, want.misc, got.misc)
	})
}
