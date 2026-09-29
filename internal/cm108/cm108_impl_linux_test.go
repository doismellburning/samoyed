// SPDX-FileCopyrightText: The Samoyed Authors
//
// SPDX-License-Identifier: GPL-2.0-or-later

package cm108

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCM108GoodDevice(t *testing.T) {
	var tests = []struct {
		name string
		vid  int
		pid  int
		want bool
	}{
		{"CM108 lowest", CMEDIA_VID, CMEDIA_PID1_MIN, true},
		{"CM108 highest", CMEDIA_VID, CMEDIA_PID1_MAX, true},
		{"C-Media below range", CMEDIA_VID, CMEDIA_PID1_MIN - 1, false},
		{"C-Media above range", CMEDIA_VID, CMEDIA_PID1_MAX + 1, false},
		{"CM108AH", CMEDIA_VID, CMEDIA_PID_CM108AH, true},
		{"CM108AH alt", CMEDIA_VID, CMEDIA_PID_CM108AH_alt, true},
		{"CM108B", CMEDIA_VID, CMEDIA_PID_CM108B, true},
		{"CM119A", CMEDIA_VID, CMEDIA_PID_CM119A, true},
		{"CM119B", CMEDIA_VID, CMEDIA_PID_CM119B, true},
		{"HS100 shares a PID with CM108AH alt", CMEDIA_VID, CMEDIA_PID_HS100, true},
		{"SSS1621", SSS_VID, SSS_PID1, true},
		{"SSS1623", SSS_VID, SSS_PID2, true},
		{"SSS1623 other", SSS_VID, SSS_PID3, true},
		{"SSS unknown PID", SSS_VID, 0x1606, false},
		{"AIOC", AIOC_VID, AIOC_PID, true},
		{"AIOC VID wrong PID", AIOC_VID, AIOC_PID + 1, false},
		{"C-Media PID with SSS VID", SSS_VID, CMEDIA_PID_CM108B, false},
		{"Dell keyboard", 0x413c, 0x2010, false},
		{"zero", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GOOD_DEVICE(tt.vid, tt.pid))
		})
	}
}

// The inventory comes from the host's udev, which in CI has no USB audio at
// all, so all we can say about it is that it is internally consistent.
func TestInventoryIsConsistent(t *testing.T) {
	var things, err = Inventory(MAXX_THINGS)
	if err != nil {
		t.Skipf("udev enumeration unavailable here: %v", err)
	}

	assert.LessOrEqual(t, len(things), MAXX_THINGS)

	for _, thing := range things {
		require.NotNil(t, thing)
		assert.True(t, thing.DevnodeSound != "" || thing.DevnodeHidraw != "",
			"every item is either a sound device or a HID: %+v", thing)

		if thing.Plughw != "" {
			assert.Regexp(t, `^plughw:[0-9]+,[0-9]+$`, thing.Plughw)
		}
	}
}

func TestInventoryRespectsMaximum(t *testing.T) {
	var things, err = Inventory(0)
	if err != nil {
		t.Skipf("udev enumeration unavailable here: %v", err)
	}

	assert.Empty(t, things)
}

func TestFindPTTRejectsUnparseableDevice(t *testing.T) {
	for _, device := range []string{"", "plughw", "default", ":2,0", "plughw:,0"} {
		t.Run(device, func(t *testing.T) {
			var ptt, err = FindPTT(device)

			require.Error(t, err)
			assert.Empty(t, ptt)
		})
	}
}

func TestFindPTTNoSuchCard(t *testing.T) {
	// No machine has this many sound cards, nor one with this name.
	for _, device := range []string{
		"plughw:987654,0",
		"surround41:CARD=Q1TESTnonexistent,DEV=0",
		"surround41:Q1TESTnonexistent,0",
		"surround41:Q1TESTnonexistent",
	} {
		t.Run(device, func(t *testing.T) {
			var ptt, err = FindPTT(device)

			require.NoError(t, err)
			assert.Empty(t, ptt)
		})
	}
}

// /dev/full accepts the open but fails every write with ENOSPC, which is the
// nearest we can get to a HID refusing a report.
func TestCM108WriteReportsWriteFailure(t *testing.T) {
	var _, statErr = os.Stat("/dev/full")
	if statErr != nil {
		t.Skipf("no /dev/full here: %v", statErr)
	}

	var err = SetGPIOPin("/dev/full", 3, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write to /dev/full failed")
}

func fakeUSB(devnode string, vid string, pid string, product string) *cm108USBDevice {
	var d = new(cm108USBDevice)
	d.devnode = devnode
	d.idVendor = vid
	d.idProduct = pid
	d.product = product

	return d
}

// fakeSoundCard gives the devices udev enumerates for one sound card: the
// card itself (no device node) followed by its capture, playback and control nodes.
func fakeSoundCard(number string, id string, usb *cm108USBDevice) []cm108Device {
	var card cm108Device
	card.syspath = "/sys/devices/q1test/sound/card" + number
	card.id = id
	card.number = number

	var devs = []cm108Device{card}

	for _, node := range []string{"pcmC" + number + "D0c", "pcmC" + number + "D0p", "controlC" + number} {
		var d cm108Device
		d.devnode = "/dev/snd/" + node
		d.usb = usb
		devs = append(devs, d)
	}

	return devs
}

func fakeHID(devnode string, devpath string, usb *cm108USBDevice) cm108Device {
	var d cm108Device
	d.devnode = devnode
	d.devpath = devpath
	d.usb = usb

	return d
}

// Modelled on the example in the comment at the top of cm108_linux.go.
func fakeCM108Devices() ([]cm108Device, []cm108Device) {
	var cmedia1 = fakeUSB("/dev/bus/usb/001/012", "0d8c", "000c", "C-Media USB Headphone Set")
	var codec = fakeUSB("/dev/bus/usb/001/013", "08bb", "2904", "USB Audio CODEC")
	var keyboard = fakeUSB("/dev/bus/usb/001/014", "413c", "2010", "Dell USB Keyboard")

	var sound []cm108Device
	sound = append(sound, fakeSoundCard("1", "Device_1", cmedia1)...)
	sound = append(sound, fakeSoundCard("2", "CODEC", codec)...)
	// Onboard audio, not on USB at all.
	sound = append(sound, fakeSoundCard("3", "PCH", nil)...)

	var hid = []cm108Device{
		fakeHID("/dev/hidraw0", "/devices/q1test/hidraw/hidraw0", cmedia1),
		fakeHID("/dev/hidraw2", "/devices/q1test/hidraw/hidraw2", codec),
		fakeHID("/dev/hidraw4", "/devices/q1test/hidraw/hidraw4", keyboard),
		// A HID with no node, and one that isn't USB, are both passed over.
		fakeHID("", "/devices/q1test/hidraw/hidraw8", keyboard),
		fakeHID("/dev/hidraw9", "/devices/q1test/hidraw/hidraw9", nil),
	}

	return sound, hid
}

func TestInventoryOfFakeDevices(t *testing.T) {
	var sound, hid = fakeCM108Devices()

	var things = cm108_inventory_of(sound, hid, MAXX_THINGS)

	// Three nodes for each of the two USB cards, plus the keyboard.
	require.Len(t, things, 7)

	for i, node := range []string{"pcmC1D0c", "pcmC1D0p", "controlC1"} {
		var thing = things[i]

		assert.Equal(t, CMEDIA_VID, thing.VID)
		assert.Equal(t, 0x000c, thing.PID)
		assert.Equal(t, "1", thing.CardNumber)
		assert.Equal(t, "Device_1", thing.CardName)
		assert.Equal(t, "C-Media USB Headphone Set", thing.Product)
		assert.Equal(t, "/dev/snd/"+node, thing.DevnodeSound)
		assert.Equal(t, "/dev/bus/usb/001/012", thing.DevnodeUSB)
		assert.Equal(t, "/sys/devices/q1test/sound/card1", thing.Devpath)
		assert.Equal(t, "/dev/hidraw0", thing.DevnodeHidraw, "HID merged in by USB device")
	}

	assert.Equal(t, "plughw:1,0", things[0].Plughw)
	assert.Equal(t, "plughw:Device_1,0", things[0].Plughw2)
	assert.Equal(t, "plughw:1,0", things[1].Plughw)
	assert.Empty(t, things[2].Plughw, "control nodes have no plughw form")
	assert.Empty(t, things[2].Plughw2)

	for _, thing := range things[3:6] {
		assert.Equal(t, 0x08bb, thing.VID)
		assert.Equal(t, 0x2904, thing.PID)
		assert.Equal(t, "2", thing.CardNumber)
		assert.Equal(t, "CODEC", thing.CardName)
		assert.Equal(t, "/dev/hidraw2", thing.DevnodeHidraw)
	}

	assert.Equal(t, "plughw:CODEC,0", things[3].Plughw2)

	var keyboard = things[6]
	assert.Equal(t, 0x413c, keyboard.VID)
	assert.Equal(t, 0x2010, keyboard.PID)
	assert.Equal(t, "Dell USB Keyboard", keyboard.Product)
	assert.Equal(t, "/dev/hidraw4", keyboard.DevnodeHidraw)
	assert.Equal(t, "/dev/bus/usb/001/014", keyboard.DevnodeUSB)
	assert.Equal(t, "/devices/q1test/hidraw/hidraw4", keyboard.Devpath)
	assert.Empty(t, keyboard.DevnodeSound)
	assert.Empty(t, keyboard.CardNumber)
	assert.Empty(t, keyboard.Plughw)
}

func TestInventoryOfLimitsItems(t *testing.T) {
	var sound, hid = fakeCM108Devices()

	// The first card's nodes use up the space, so its HID can still merge
	// into them but nothing else gets an entry of its own.
	var things = cm108_inventory_of(sound, hid, 3)

	require.Len(t, things, 3)

	for _, thing := range things {
		assert.Equal(t, "1", thing.CardNumber)
		assert.Equal(t, "/dev/hidraw0", thing.DevnodeHidraw)
	}

	assert.Empty(t, cm108_inventory_of(sound, hid, 0))
}

func TestInventoryOfNothing(t *testing.T) {
	assert.Empty(t, cm108_inventory_of(nil, nil, MAXX_THINGS))
}

func TestInventoryOfMissingIDs(t *testing.T) {
	// Without a vendor or product id, or a USB device node, there is nothing
	// to merge a HID into a sound card by.
	var anonymous = fakeUSB("", "", "", "")

	var sound = fakeSoundCard("5", "Q1TEST", anonymous)
	var hid = []cm108Device{fakeHID("/dev/hidraw5", "/devices/q1test/hidraw/hidraw5", anonymous)}

	var things = cm108_inventory_of(sound, hid, MAXX_THINGS)

	require.Len(t, things, 4)

	for _, thing := range things {
		assert.Zero(t, thing.VID)
		assert.Zero(t, thing.PID)
	}

	for _, thing := range things[:3] {
		assert.Empty(t, thing.DevnodeHidraw)
	}

	assert.Equal(t, "/dev/hidraw5", things[3].DevnodeHidraw)
}

func TestFindPTTIn(t *testing.T) {
	var sound, hid = fakeCM108Devices()
	var things = cm108_inventory_of(sound, hid, MAXX_THINGS)

	for _, device := range []string{
		"plughw:1,0",
		"plughw:Device_1,0",
		"surround41:CARD=Device_1,DEV=0",
		"surround41:Device_1,0",
		"surround41:Device_1",
	} {
		t.Run(device, func(t *testing.T) {
			var ptt, err = cm108_find_ptt_in(things, device)

			require.NoError(t, err)
			assert.Equal(t, "/dev/hidraw0", ptt)
		})
	}

	t.Run("unknown device", func(t *testing.T) {
		var ptt, err = cm108_find_ptt_in(things, "plughw:CODEC,0")

		require.ErrorIs(t, err, ErrUnknownDevice)
		assert.Contains(t, err.Error(), "USB audio card 2 (CODEC)")
		assert.Equal(t, "/dev/hidraw2", ptt, "the device is still returned")
	})

	t.Run("not a USB card", func(t *testing.T) {
		var ptt, err = cm108_find_ptt_in(things, "plughw:3,0")

		require.NoError(t, err)
		assert.Empty(t, ptt)
	})

	t.Run("unparseable", func(t *testing.T) {
		var ptt, err = cm108_find_ptt_in(things, "default")

		require.Error(t, err)
		assert.Empty(t, ptt)
	})
}
