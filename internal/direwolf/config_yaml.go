// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// A configuration file can also be written in YAML.  It describes the same
// settings as the directives of the line-at-a-time format, read into the same
// settings types and checked and applied by the same code, so the two formats
// cannot come to disagree about what a setting means.  Directives that have not
// been given a YAML form yet can be written, as they would be in the older
// format, in a "legacy" block.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ConfigFile is the whole of a YAML configuration file.
type ConfigFile struct {
	AudioDevices []AudioDeviceConfig `yaml:"audioDevices"`
	Channels     []ChannelConfig     `yaml:"channels"`

	// AGWPort is the port for the AGW TCPIP Socket Interface, or 0 for none.
	AGWPort *int `yaml:"agwPort"`

	// KISSPorts are the KISS TCP ports, taken in order as successive KISSPORT
	// lines would be.
	KISSPorts []KISSPortSettings `yaml:"kissPorts"`

	// AXUDPPorts are the AXUDP ports, each its own virtual channel.
	AXUDPPorts []AXUDPPortSettings `yaml:"axudpPorts"`

	// NetROM is the NET/ROM node, if there is one.
	NetROM *NetROMSettings `yaml:"netrom"`

	// Node is how the node greets and serves the users who connect to it.
	Node *NodeSettings `yaml:"node"`

	// Legacy holds directives in the line-at-a-time format, read after
	// everything else, for anything not yet given a YAML form.
	Legacy string `yaml:"legacy"`
}

// AudioDeviceConfig is an audio device.
//
// A setting that can be left out is a pointer, so that leaving it out is not
// mistaken for asking for zero.
type AudioDeviceConfig struct {
	AudioDeviceSettings `yaml:",inline"`

	// Device is the device number; left out, it is the device's position in
	// the list.
	Device *int `yaml:"device"`

	// Channels is the number of audio channels, 1 for mono or 2 for stereo.
	Channels *int `yaml:"channels"`

	// Rate is the sample rate, in samples per second.
	Rate *int `yaml:"rate"`
}

// ChannelConfig is a radio channel.
type ChannelConfig struct {
	Channel *int                   `yaml:"channel"`
	MyCall  *string                `yaml:"mycall"`
	Modem   *ModemSettings         `yaml:"modem"`
	TXDelay *int                   `yaml:"txdelay"`
	PTT     *OutputControlSettings `yaml:"ptt"`
	DCD     *OutputControlSettings `yaml:"dcd"`
	CON     *OutputControlSettings `yaml:"con"`

	DWait    *int  `yaml:"dwait"`
	SlotTime *int  `yaml:"slottime"`
	Persist  *int  `yaml:"persist"`
	TXTail   *int  `yaml:"txtail"`
	FullDup  *bool `yaml:"fulldup"`

	FX25TX      *int            `yaml:"fx25tx"`
	IL2PTX      *IL2PTXSettings `yaml:"il2ptx"`
	IL2PVersion *string         `yaml:"il2pversion"`
}

// isYAMLConfig says whether the configuration file at path is YAML, going by
// its name.
func isYAMLConfig(path string) bool {
	var ext = strings.ToLower(filepath.Ext(path))

	return ext == ".yaml" || ext == ".yml"
}

// readYAML reads a YAML configuration file from r.  name says where it came
// from, for messages.
func (ps *parseState) readYAML(r io.Reader, name string) {
	var content, readErr = io.ReadAll(r)
	if readErr != nil {
		ps.errorf("config file: Could not read %s: %v", name, readErr)
		ps.fatal = true

		return
	}

	var file = new(ConfigFile)

	var decoder = yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	var decodeErr = decoder.Decode(file)
	if errors.Is(decodeErr, io.EOF) {
		return // An empty file, which leaves every default alone.
	}

	if decodeErr != nil {
		// Nothing in a file we could not make sense of can be trusted, so
		// none of it is applied, and there is no configuration to start on.
		if typeErr, ok := errors.AsType[*yaml.TypeError](decodeErr); ok {
			for _, e := range typeErr.Errors {
				ps.errorf("config file %s: %s", name, e)
			}
		} else {
			ps.errorf("config file %s: %v", name, decodeErr)
		}

		ps.fatal = true

		return
	}

	// A second document would go unread, and whatever it says unapplied.
	var extraErr = decoder.Decode(new(yaml.Node))
	if !errors.Is(extraErr, io.EOF) {
		if extraErr == nil {
			ps.errorf("config file %s: Only one YAML document is allowed, but there is more after the first", name)
		} else {
			ps.errorf("config file %s: %v", name, extraErr)
		}

		ps.fatal = true

		return
	}

	// Decode a second time, keeping the document's structure, only to find
	// what line each setting is on - the settings are checked as they are
	// applied, and a complaint should point at the right place.
	var root yaml.Node

	var nodeErr = yaml.Unmarshal(content, &root)
	if nodeErr != nil {
		ps.errorf("config file %s: %v", name, nodeErr)
		ps.fatal = true

		return
	}

	var top = yamlKeys(yamlDocument(&root))

	ps.applyYAMLAudioDevices(file.AudioDevices, top["audioDevices"])
	ps.applyYAMLChannels(file.Channels, top["channels"])
	ps.applyYAMLPorts(file, top)

	if file.NetROM != nil {
		ps.line = top["netrom"].line(new(yaml.Node))
		ps.reportIfError(ps.applyNETROM(*file.NetROM))
	}

	if file.Node != nil {
		ps.line = top["node"].line(new(yaml.Node))
		ps.reportIfError(ps.applyNODE(*file.Node))
	}

	if file.Legacy != "" {
		// Start the legacy block afresh, as though it were a file of its
		// own, rather than carrying on with whatever the YAML left current.
		ps.channel = 0
		ps.adevice = 0

		var block = yamlResolve(top["legacy"].value)

		// A block scalar ("|") starts on the line after its indicator.
		ps.line = block.Line - 1
		if block.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			ps.line++
		}

		ps.readLegacy(strings.NewReader(file.Legacy), name)
	}
}

// applyYAMLAudioDevices applies the audio devices, whose entries in the file
// are in node.
func (ps *parseState) applyYAMLAudioDevices(devices []AudioDeviceConfig, node yamlEntry) {
	for i, device := range devices {
		var item = yamlItem(node.value, i)
		var keys = yamlKeys(item)

		device.AudioDeviceSettings.Device = i
		if device.Device != nil {
			device.AudioDeviceSettings.Device = *device.Device
		}

		ps.line = item.Line
		var err = ps.applyADEVICE(device.AudioDeviceSettings)
		if err != nil {
			ps.report(err)
		}

		// A device that was not defined leaves some other device current,
		// which the rest of this entry is not for - as with a channel that
		// is not there.
		var number = device.AudioDeviceSettings.Device
		var defined = err == nil && device.Input != "" && ps.adevice == number &&
			ps.audio.adev[number].defined == 1

		if defined && device.Channels != nil {
			ps.line = keys["channels"].line(item)
			ps.reportIfError(ps.applyACHANNELS(*device.Channels))
		}

		if defined && device.Rate != nil {
			ps.line = keys["rate"].line(item)
			ps.reportIfError(ps.applyARATE(*device.Rate))
		}
	}
}

// applyYAMLChannels applies the radio channels, whose entries in the file are
// in node.
func (ps *parseState) applyYAMLChannels(channels []ChannelConfig, node yamlEntry) {
	for i, channel := range channels {
		var item = yamlItem(node.value, i)
		var keys = yamlKeys(item)

		if channel.Channel == nil {
			ps.line = item.Line
			ps.errorf("line %d: Missing channel number for channel", ps.line)

			continue
		}

		var number = *channel.Channel

		ps.line = keys["channel"].line(item)
		ps.reportIfError(ps.applyCHANNEL(number))

		if number < 0 || number >= MAX_RADIO_CHANS ||
			ps.audio.chan_medium[number] != MEDIUM_RADIO {
			// Not a radio channel we have, which applyCHANNEL has already
			// said.  The rest of this entry is for that channel, and applying
			// it to some other one would only make things worse.
			continue
		}

		// at applies setting, with any complaint pointing at key's line.
		var at = func(key string, setting func() error) {
			ps.line = keys[key].line(item)
			ps.reportIfError(setting())
		}

		if channel.MyCall != nil {
			at("mycall", func() error {
				if *channel.MyCall == "" {
					return fmt.Errorf("config file: Missing value for MYCALL command on line %d", ps.line)
				}

				return ps.applyMYCALL(*channel.MyCall)
			})
		}

		if channel.Modem != nil {
			at("modem", func() error { return ps.applyModem(*channel.Modem) })
		}

		if channel.TXDelay != nil {
			at("txdelay", func() error { return ps.applyTXDELAY(*channel.TXDelay) })
		}

		for _, timing := range []struct {
			key   string
			value *int
			apply func(int) error
		}{
			{"dwait", channel.DWait, ps.applyDWAIT},
			{"slottime", channel.SlotTime, ps.applySLOTTIME},
			{"persist", channel.Persist, ps.applyPERSIST},
			{"txtail", channel.TXTail, ps.applyTXTAIL},
			{"fx25tx", channel.FX25TX, ps.applyFX25TX},
		} {
			if timing.value != nil {
				at(timing.key, func() error { return timing.apply(*timing.value) })
			}
		}

		if channel.FullDup != nil {
			at("fulldup", func() error { return ps.applyFULLDUP(*channel.FullDup) })
		}

		if channel.IL2PTX != nil {
			at("il2ptx", func() error { return ps.applyIL2PTX(*channel.IL2PTX) })
		}

		if channel.IL2PVersion != nil {
			at("il2pversion", func() error { return ps.applyIL2PVERSION(*channel.IL2PVersion) })
		}

		for _, control := range []struct {
			key      string
			ot       int
			settings *OutputControlSettings
		}{
			{"ptt", OCTYPE_PTT, channel.PTT},
			{"dcd", OCTYPE_DCD, channel.DCD},
			{"con", OCTYPE_CON, channel.CON},
		} {
			if control.settings != nil {
				at(control.key, func() error { return ps.applyOutputControl(control.ot, *control.settings) })
			}
		}
	}
}

// applyYAMLPorts applies the network ports, whose keys in the file are top.
func (ps *parseState) applyYAMLPorts(file *ConfigFile, top map[string]yamlEntry) {
	if file.AGWPort != nil {
		ps.line = top["agwPort"].line(new(yaml.Node))
		ps.reportIfError(ps.applyAGWPORT(*file.AGWPort))
	}

	for i, port := range file.KISSPorts {
		ps.line = yamlItem(top["kissPorts"].value, i).Line
		ps.reportIfError(ps.applyKISSPORT(port))
	}

	for i, port := range file.AXUDPPorts {
		ps.line = yamlItem(top["axudpPorts"].value, i).Line
		ps.reportIfError(ps.applyAXUDPPORT(port))
	}
}

// reportIfError reports err, if there is one.
func (ps *parseState) reportIfError(err error) {
	if err != nil {
		ps.report(err)
	}
}

// yamlEntry is one key of a YAML mapping, and its value.
type yamlEntry struct {
	key   *yaml.Node
	value *yaml.Node
}

// line is the line the entry's key is on, or item's line if the key is not
// there to be found - say, it came from somewhere yamlKeys does not look.
func (e yamlEntry) line(item *yaml.Node) int {
	if e.key != nil {
		return e.key.Line
	}

	return item.Line
}

// yamlDocument is the top level node of the document root holds.
func yamlDocument(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return root.Content[0]
	}

	return root
}

// yamlResolve follows node to what it stands for, if it is an alias.
func yamlResolve(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}

	return node
}

// yamlKeys maps each key of the mapping node to its entry, including those it
// takes from mappings merged in with "<<".  Anything else has no keys.
//
// This only finds where settings are, for messages.  What the settings are
// comes from decoding the document, which does not depend on it.
func yamlKeys(node *yaml.Node) map[string]yamlEntry {
	var keys = make(map[string]yamlEntry)

	node = yamlResolve(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return keys
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		var key, value = node.Content[i], node.Content[i+1]

		if key.Tag != "!!merge" {
			keys[key.Value] = yamlEntry{key: key, value: value}

			continue
		}

		// A merge takes one mapping, or a list of them, and keys of the
		// mapping itself win over merged ones.
		var merged = []*yaml.Node{value}
		if resolved := yamlResolve(value); resolved != nil && resolved.Kind == yaml.SequenceNode {
			merged = resolved.Content
		}

		for _, m := range merged {
			for k, e := range yamlKeys(m) {
				if _, ok := keys[k]; !ok {
					keys[k] = e
				}
			}
		}
	}

	return keys
}

// yamlItem is item i of the sequence node, or an empty node if there is no
// such item.
func yamlItem(node *yaml.Node, i int) *yaml.Node {
	node = yamlResolve(node)
	if node == nil || node.Kind != yaml.SequenceNode || i >= len(node.Content) {
		return new(yaml.Node)
	}

	return node.Content[i]
}
