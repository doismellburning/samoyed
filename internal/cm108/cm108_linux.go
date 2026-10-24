// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package cm108

/*------------------------------------------------------------------
 *
 * Purpose:   	Use the CM108/CM119 (or compatible) GPIO pins for the Push To Talk (PTT) Control.
 *
 * Description:
 *
 *	There is an increasing demand for using the GPIO pins of USB audio devices for PTT.
 *	We have a few commercial products:
 *
 *		DINAH		https://hamprojects.info/dinah/
 *		PAUL		https://hamprojects.info/paul/
 *		DMK URI		http://www.dmkeng.com/URI_Order_Page.htm
 *		RB-USB RIM	http://www.repeater-builder.com/products/usb-rim-lite.html
 *		RA-35		http://www.masterscommunications.com/products/radio-adapter/ra35.html
 *
 *	and homebrew projects which are all very similar.
 *
 *		http://www.qsl.net/kb9mwr/projects/voip/usbfob-119.pdf
 *		http://rtpdir.weebly.com/uploads/1/6/8/7/1687703/usbfob.pdf
 *		http://www.repeater-builder.com/projects/fob/USB-Fob-Construction.pdf
 *		https://irongarment.wordpress.com/2011/03/29/cm108-compatible-chips-with-gpio/
 *
 *	Homebrew plans all use GPIO 3 because it is easier to tack solder a wire to a pin on the end.
 *	All of the products, that I have seen, also use the same pin so this is the default.
 *
 *	Soundmodem and hamlib paved the way but didn't get too far.
 *	Dire Wolf 1.3 added HAMLIB support (Linux only) which theoretically allows this in a
 *	painful roundabout way.  This is documented in the User Guide, section called,
 *		 "Hamlib PTT Example 2: Use GPIO of USB audio adapter.  (e.g. DMK URI)"
 *
 *	It's rather involved and the explanation doesn't cover the case of multiple
 *	USB-Audio adapters.  It is not as straightforward as you might expect.  Here we have
 *	an example of 3 C-Media USB adapters, a SignaLink USB, a keyboard, and a mouse.
 *
 *
 *	    VID  PID   Product                          Sound                  ADEVICE         HID [ptt]
 *	    ---  ---   -------                          -----                  -------         ---------
 *	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC1D0c      plughw:1,0      /dev/hidraw0
 *	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC1D0p      plughw:1,0      /dev/hidraw0
 *	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/controlC1                     /dev/hidraw0
 *	    08bb 2904  USB Audio CODEC                  /dev/snd/pcmC2D0c      plughw:2,0      /dev/hidraw2
 *	    08bb 2904  USB Audio CODEC                  /dev/snd/pcmC2D0p      plughw:2,0      /dev/hidraw2
 *	    08bb 2904  USB Audio CODEC                  /dev/snd/controlC2                     /dev/hidraw2
 *	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC0D0c      plughw:0,0      /dev/hidraw1
 *	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC0D0p      plughw:0,0      /dev/hidraw1
 *	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/controlC0                     /dev/hidraw1
 *	**  0d8c 0008  C-Media USB Audio Device         /dev/snd/pcmC4D0c      plughw:4,0      /dev/hidraw6
 *	**  0d8c 0008  C-Media USB Audio Device         /dev/snd/pcmC4D0p      plughw:4,0      /dev/hidraw6
 *	**  0d8c 0008  C-Media USB Audio Device         /dev/snd/controlC4                     /dev/hidraw6
 *	    413c 2010  Dell USB Keyboard                                                       /dev/hidraw4
 *	    0461 4d15  USB Optical Mouse                                                       /dev/hidraw5
 *
 *
 *	The USB soundcards (/dev/snd/pcm...) have an associated Human Interface Device (HID)
 *	corresponding to the GPIO pins which are sometimes connected to pushbuttons.
 *	The mapping has no obvious pattern.
 *
 *		Sound Card 0		HID 1
 *		Sound Card 1		HID 0
 *		Sound Card 2		HID 2
 *		Sound Card 4		HID 6
 *
 *	That would be a real challenge if you had to figure that all out and configure manually.
 *	Dire Wolf version 1.5 makes this much more flexible and easier to use by supporting multiple
 *	sound devices and automatically determining the corresponding HID for the PTT signal.
 *
 *	In version 1.7, we add a half-backed solution for Windows.  It's fine for situations
 *	with a single USB Audio Adapter, but does not automatically handle the multiple device case.
 *	Manual configuration needs to be used in this case.
 *
 *	Here is something new and interesting.  The All in One cable (AIOC).
 *	https://github.com/skuep/AIOC/tree/master
 *
 *	A microcontroller is used to emulate a CM108-compatible soundcard
 *	and a serial port.  It fits right on the side of a Bao Feng or similar.
 *
 *---------------------------------------------------------------*/

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

/* Dire Wolf cm108.h */

// The CM108, CM109, and CM119 datasheets all say that idProduct can be in the range
// of 0008 to 000f programmable by MSEL and MODE pin.  How can we tell the difference?

// CM108B is 0012.
// CM119B is 0013.
// CM108AH is 0139 programmable by MSEL and MODE pin.
// CM119A is 013A programmable by MSEL and MODE pin.

// To make matters even more confusing, these can be overridden
// with an external EEPROM.  Some have 8, rather than 4 GPIO.

const CMEDIA_VID = 0xd8c       // Vendor ID
const CMEDIA_PID1_MIN = 0x0008 // range for CM108, CM109, CM119 (no following letters)
const CMEDIA_PID1_MAX = 0x000f

const CMEDIA_PID_CM108AH = 0x0139     // CM108AH
const CMEDIA_PID_CM108AH_alt = 0x013c // CM108AH? - see issue 210
const CMEDIA_PID_CM108B = 0x0012      // CM108B
const CMEDIA_PID_CM119A = 0x013a      // CM119A
const CMEDIA_PID_CM119B = 0x0013      // CM119B
const CMEDIA_PID_HS100 = 0x013c       // HS100

// The SSS chips seem to be pretty much compatible but they have only two GPIO.
// https://irongarment.wordpress.com/2011/03/29/cm108-compatible-chips-with-gpio/
// Data sheet says VID/PID is from an EEPROM but mentions no default.

const SSS_VID = 0x0c76 // SSS1621, SSS1623
const SSS_PID1 = 0x1605
const SSS_PID2 = 0x1607
const SSS_PID3 = 0x160b

// https://github.com/skuep/AIOC/blob/master/stm32/aioc-fw/Src/usb_descriptors.h

const AIOC_VID = 0x1209
const AIOC_PID = 0x7388

//	Device		VID	PID		Number of GPIO
//	------		---	---		--------------
//	CM108		0d8c	0008-000f *	4
//	CM108AH		0d8c	0139 *		3	Has GPIO 1,3,4 but not 2
//	CM108B		0d8c	0012		3	Has GPIO 1,3,4 but not 2
//	CM109		0d8c	0008-000f *	8
//	CM119		0d8c	0008-000f *	8
//	CM119A		0d8c	013a *		8
//	CM119B		0d8c	0013		8
//	HS100		0d8c	013c		0		(issue 210 reported 013c
//								 being seen for CM108AH)
//
//	SSS1621		0c76	1605		2 	per ZL3AME, Can't find data sheet
//	SSS1623		0c76	1607,160b	2	per ZL3AME, Not in data sheet.
//
//				* idProduct programmable by MSEL and MODE pin.
//

// 	CMedia pin	GPIO	Notes
//	----------	----	-----
//	43		1
//	11		2	N.C. for CM108AH, CM108B
//	13		3	Most popular for PTT because it is on the end.
//	15		4
//	16		5	CM109, CM119, CM119A, CM119B only
//	17		6	"
//	20		7	"
//	22		8	"

// Maximum length of name for PTT HID.
// For Linux, this was originally 17 to handle names like /dev/hidraw3.
// Windows has more complicated names.  The longest I saw was 95 but longer have been reported.
// Then we have this  https://groups.io/g/direwolf/message/9622  where 127 is not enough.

const MAXX_HIDRAW_NAME_LEN = 150

/*
 * Result of taking inventory of USB soundcards and USB HIDs.
 */

type Thing struct {
	VID          int    // vendor id, displayed as four hexadecimal digits.
	PID          int    // product id, displayed as four hexadecimal digits.
	CardNumber   string // "Card" Number.  e.g.  2 for plughw:2,0
	CardName     string // Audio Card Name, assigned by system (e.g. Device_1) or by udev rule.
	Product      string // product name (e.g. manufacturer, model)
	DevnodeSound string // e.g. /dev/snd/pcmC0D0p
	Plughw       string // Above in more familiar format e.g. plughw:0,0
	// Oversized to silence a compiler warning.
	Plughw2       string // With name rather than number.
	Devpath       string // Kernel dev path.  Does not include /sys mount point.
	DevnodeHidraw string
	// e.g. /dev/hidraw3  -  for Linux - was length 17
	// The Windows path for a HID looks like this, lengths up to 95 seen.
	// \\?\hid#vid_0d8c&pid_000c&mi_03#8&164d11c9&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}
	DevnodeUSB string // e.g. /dev/bus/usb/001/012
	// This is what we use to match up audio and HID.
}

// GOOD_DEVICE tests for supported devices.
func GOOD_DEVICE(v, p int) bool {
	return (v == CMEDIA_VID && ((p >= CMEDIA_PID1_MIN && p <= CMEDIA_PID1_MAX) || p == CMEDIA_PID_CM108AH ||
		p == CMEDIA_PID_CM108AH_alt || p == CMEDIA_PID_CM108B || p == CMEDIA_PID_CM119A || p == CMEDIA_PID_CM119B)) ||
		(v == SSS_VID && (p == SSS_PID1 || p == SSS_PID2 || p == SSS_PID3)) ||
		(v == AIOC_VID && p == AIOC_PID)
}

const MAXX_THINGS = 60

/*-------------------------------------------------------------------
 *
 * Name:	Inventory
 *
 * Purpose:	Take inventory of USB audio and HID.
 *
 * Inputs:	max_things	- Maximum number of items to collect.
 *
 * Outputs:	things		- Array of items collected.
 *				  Corresponding sound device and HID are merged into one item.
 *
 * Returns:	The items collected, and an error if sysfs could not be read.
 *
 *------------------------------------------------------------------*/

func Inventory(max_things int) ([]*Thing, error) {
	return inventoryFromSysfs("/sys", max_things)
} /* end Inventory */

// inventoryFromSysfs does the work of Inventory against the sysfs mounted at
// sysRoot, so that a test can hand it a tree of its own.
func inventoryFromSysfs(sysRoot string, max_things int) ([]*Thing, error) {
	/*
	 * First get a list of the USB audio devices, then all of the USB HID.
	 * Dire Wolf asked libudev for these; we read the same sysfs it does.
	 */
	var devices, devicesErr = readSysfsDevices(sysRoot, "sound")
	if devicesErr != nil {
		return nil, devicesErr
	}

	var hidDevices, hidDevicesErr = readSysfsDevices(sysRoot, "hidraw")
	if hidDevicesErr != nil {
		return nil, hidDevicesErr
	}

	return cm108_inventory_of(devices, hidDevices, max_things), nil
}

// cm108Device is what the inventory needs to know of a device, read out of
// sysfs up front so that the inventory can also be taken of devices that
// are not really there.
type cm108Device struct {
	devnode string
	devpath string // syspath without the sysfs mount point.
	syspath string
	id      string // Sound card name, from sysattr "id".
	number  string // Sound card number, from sysattr "number".
	usb     *cm108USBDevice
}

// cm108USBDevice is the "usb" "usb_device" ancestor of a cm108Device.
type cm108USBDevice struct {
	devnode   string
	idVendor  string
	idProduct string
	product   string
}

// readSysfsDevices lists the devices of a subsystem from <sysRoot>/class, in
// the order libudev would have enumerated them.  A subsystem with no class
// directory (its driver not loaded, or no sysfs in a container) has no devices.
func readSysfsDevices(sysRoot string, subsystem string) ([]cm108Device, error) {
	var root, rootErr = filepath.EvalSymlinks(sysRoot)
	if rootErr != nil {
		return nil, fmt.Errorf("could not read sysfs at %s: %w", sysRoot, rootErr)
	}

	var classDir = filepath.Join(root, "class", subsystem)

	var entries, entriesErr = os.ReadDir(classDir)
	if errors.Is(entriesErr, fs.ErrNotExist) {
		return nil, nil
	}

	if entriesErr != nil {
		return nil, fmt.Errorf("could not list %s: %w", classDir, entriesErr)
	}

	var read = make([]cm108Device, 0, len(entries))

	for _, entry := range entries {
		var syspath, syspathErr = filepath.EvalSymlinks(filepath.Join(classDir, entry.Name()))
		if syspathErr != nil {
			return nil, fmt.Errorf("could not resolve %s device %s: %w", subsystem, entry.Name(), syspathErr)
		}

		var d cm108Device

		d.syspath = syspath
		d.devpath = strings.TrimPrefix(syspath, root)
		d.devnode = sysfsDevnode(syspath)
		d.id = readSysattr(syspath, "id")
		d.number = readSysattr(syspath, "number")
		d.usb = findUSBDevice(root, syspath)

		read = append(read, d)
	}

	sort.Slice(read, func(i, j int) bool {
		return sysfsEnumerationLess(read[i].syspath, read[j].syspath)
	})

	return read, nil
}

// sysfsEnumerationLess orders devices as libudev does: by syspath, except that
// a sound card's controlC device comes after its siblings.  Either way a card
// comes before the devices under it, which cm108_inventory_of relies on.
func sysfsEnumerationLess(a string, b string) bool {
	var cardA, controlA = soundCardOf(a)
	var cardB, controlB = soundCardOf(b)

	if cardA != cardB {
		return cardA < cardB
	}

	if controlA != controlB {
		return controlB
	}

	return a < b
}

// soundCardPath splits a syspath into the sound card it is, or is under, and
// the rest.
var soundCardPath = regexp.MustCompile(`^(.*/sound/card[^/]+)(/.*)?$`)

// soundCardOf gives the syspath of the sound card a device is, or is under,
// and whether it is the card's control device.  A path not under a sound card
// is its own key.
func soundCardOf(syspath string) (string, bool) {
	var m = soundCardPath.FindStringSubmatch(syspath)
	if m == nil {
		return syspath, false
	}

	return m[1], strings.HasPrefix(m[2], "/controlC")
}

// findUSBDevice walks up from a device to the nearest USB device (as opposed to
// a USB interface) above it, as libudev's
// udev_device_get_parent_with_subsystem_devtype(dev, "usb", "usb_device") does.
func findUSBDevice(root string, syspath string) *cm108USBDevice {
	for dir := filepath.Dir(syspath); strings.HasPrefix(dir, root+"/"); dir = filepath.Dir(dir) {
		var subsystem, linkErr = os.Readlink(filepath.Join(dir, "subsystem"))
		if linkErr != nil || filepath.Base(subsystem) != "usb" {
			continue
		}

		if readUevent(dir)["DEVTYPE"] != "usb_device" {
			continue
		}

		var u = new(cm108USBDevice)
		u.devnode = sysfsDevnode(dir)
		u.idVendor = readSysattr(dir, "idVendor")
		u.idProduct = readSysattr(dir, "idProduct")
		u.product = readSysattr(dir, "product")

		return u
	}

	return nil
}

// sysfsDevnode gives the /dev node of a device, from the DEVNAME in its
// uevent, or "" if it has none (a sound card itself, for one).
func sysfsDevnode(syspath string) string {
	var name = readUevent(syspath)["DEVNAME"]
	if name == "" {
		return ""
	}

	return "/dev/" + name
}

// readUevent reads the KEY=value lines of a device's uevent file.
func readUevent(syspath string) map[string]string {
	var uevent = make(map[string]string)

	var content, err = os.ReadFile(filepath.Join(syspath, "uevent")) //nolint:gosec // Reading sysfs is the point.
	if err != nil {
		return uevent
	}

	for line := range strings.SplitSeq(string(content), "\n") {
		var key, value, found = strings.Cut(line, "=")
		if found {
			uevent[key] = value
		}
	}

	return uevent
}

// readSysattr reads a device attribute with its trailing newline trimmed, as
// libudev does, or "" if the device does not have it.
func readSysattr(syspath string, name string) string {
	var content, err = os.ReadFile(filepath.Join(syspath, name)) //nolint:gosec // Reading sysfs is the point.
	if err != nil {
		return ""
	}

	return strings.TrimRight(string(content), " \t\n\r")
}

// cm108_inventory_of does the work of Inventory on the devices of the
// "sound" and "hidraw" subsystems, in libudev's enumeration order.
func cm108_inventory_of(devices []cm108Device, hidDevices []cm108Device, max_things int) []*Thing {
	var things []*Thing

	var cardDevpath string
	var pattrsID string
	var pattrsNumber string

	for _, dev := range devices {
		var devnode = dev.devnode

		if devnode == "" { //nolint:nestif
			// I'm not happy with this but couldn't figure out how
			// to get attributes from one level up from the pcmC?D?? node.
			cardDevpath = dev.syspath
			pattrsID = dev.id
			pattrsNumber = dev.number
		} else {
			var parentdev = dev.usb
			if parentdev != nil {
				var vid int
				var pid int

				var p = parentdev.idVendor
				if p != "" {
					var vid64, _ = strconv.ParseInt(p, 16, 0)
					vid = int(vid64)
				}

				p = parentdev.idProduct
				if p != "" {
					var pid64, _ = strconv.ParseInt(p, 16, 0)
					pid = int(pid64)
				}

				if len(things) < max_things {
					var thing = new(Thing)

					thing.VID = vid
					thing.PID = pid
					thing.CardName = pattrsID
					thing.CardNumber = pattrsNumber
					thing.Product = parentdev.product
					thing.DevnodeSound = devnode
					thing.DevnodeUSB = parentdev.devnode
					thing.Devpath = cardDevpath

					things = append(things, thing)
				}
			}
		}
	}

	/*
	 * Now merge in all of the USB HID.
	 */
	for _, dev := range hidDevices {
		var devnode = dev.devnode
		if devnode != "" { //nolint:nestif
			var parentdev = dev.usb
			if parentdev != nil {
				var vid int
				var pid int

				var p = parentdev.idVendor
				if p != "" {
					var vid64, _ = strconv.ParseInt(p, 16, 0)
					vid = int(vid64)
				}

				p = parentdev.idProduct
				if p != "" {
					var pid64, _ = strconv.ParseInt(p, 16, 0)
					pid = int(pid64)
				}

				var usb = parentdev.devnode

				// Add hidraw name to any matching existing.
				var matched = false

				for _, thing := range things {
					if thing.VID == vid && thing.PID == pid && usb != "" && thing.DevnodeUSB == usb {
						matched = true
						thing.DevnodeHidraw = devnode
					}
				}

				// If it did not match to existing, add new entry.
				if !matched && len(things) < max_things {
					var thing = new(Thing)

					thing.VID = vid
					thing.PID = pid
					thing.Product = parentdev.product
					thing.DevnodeHidraw = devnode
					thing.DevnodeUSB = usb
					thing.Devpath = dev.devpath

					things = append(things, thing)
				}
			}
		}
	}

	/*
	 * Seeing the form /dev/snd/pcmC4D0p will be confusing to many because we
	 * would generally something like plughw:4,0 for in the direwolf configuration file.
	 * Construct the more familiar form.
	 * Previously we only used the numeric form.  In version 1.6, the name is listed as well
	 * and we describe how to assign names based on the physical USB socket for repeatability.
	 */
	var pcm_re = regexp.MustCompile("pcmC([0-9]+)D([0-9]+)[cp]")

	for _, thing := range things {
		var matches = pcm_re.FindStringSubmatch(thing.DevnodeSound)

		if matches != nil {
			var c = matches[1]
			var d = matches[2]

			thing.Plughw = fmt.Sprintf("plughw:%s,%s", c, d)
			thing.Plughw2 = fmt.Sprintf("plughw:%s,%s", thing.CardName, d)
		}
	}

	return things
} /* end cm108_inventory_of */

/*-------------------------------------------------------------------
 *
 * Name:	FindPTT
 *
 * Purpose:	Try to find /dev/hidraw corresponding to a USB audio "card."
 *
 * Inputs:	output_audio_device
 *				- Used in the ADEVICE configuration.
 *				  This can take many forms such as:
 *					surround41:CARD=Fred,DEV=0
 *					surround41:Fred,0
 *					surround41:Fred
 *					plughw:2,3
 *				  In our case we just need to extract the card number or name.
 *
 * Returns:	ptt_device	- Device name, something like /dev/hidraw2.
 *				  Will be empty string if no match found.
 *
 *		err		- Non-nil if the search could not be carried
 *				  out, or if the device found is not one known
 *				  to work for GPIO PTT.  In the latter case the
 *				  device name is returned as well and the error
 *				  wraps ErrUnknownDevice, so a caller can
 *				  warn and carry on.
 *
 *------------------------------------------------------------------*/

func FindPTT(output_audio_device string) (string, error) {
	//dw_printf ("DEBUG: FindPTT('%s')\n", output_audio_device);

	// Possible improvement: Skip if inventory already taken.
	var things, inventoryErr = Inventory(MAXX_THINGS)
	if inventoryErr != nil {
		return "", fmt.Errorf("could not take inventory of USB audio devices: %w", inventoryErr)
	}

	return cm108_find_ptt_in(things, output_audio_device)
}

// cm108_find_ptt_in does the work of FindPTT on an inventory already taken.
func cm108_find_ptt_in(things []*Thing, output_audio_device string) (string, error) {
	var sound_re = regexp.MustCompile(".+:(CARD=)?([A-Za-z0-9_]+)(,.*)?")

	var matches = sound_re.FindStringSubmatch(output_audio_device)
	var num_or_name string

	if matches != nil {
		num_or_name = matches[2]
		//dw_printf ("DEBUG: Got '%s' from '%s'\n", num_or_name, output_audio_device);
	}

	if len(num_or_name) == 0 {
		return "", fmt.Errorf("could not extract card number or name from %s", output_audio_device)
	}

	for _, thing := range things {
		//dw_printf ("DEBUG: i=%d, card_name='%s', card_number='%s'\n", i, things[i].CardName, things[i].CardNumber);
		if num_or_name == thing.CardName || num_or_name == thing.CardNumber {
			//dw_printf ("DEBUG: success! returning '%s'\n", things[i].DevnodeHidraw);
			if !GOOD_DEVICE(thing.VID, thing.PID) {
				return thing.DevnodeHidraw, fmt.Errorf("USB audio card %s (%s) is %w",
					thing.CardNumber, thing.CardName, ErrUnknownDevice)
			}

			return thing.DevnodeHidraw, nil
		}
	}

	return "", nil
}

/*-------------------------------------------------------------------
 *
 * Name:	SetGPIOPin
 *
 * Purpose:	Set one GPIO pin of the CM108 or similar.
 *
 * Inputs:	name		- Name of device such as /dev/hidraw2 or
 *					\\?\hid#vid_0d8c&pid_0008&mi_03#8&39d3555&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}
 *
 *		num		- GPIO number, range 1 thru 8.
 *
 *		state		- 1 for on, 0 for off.
 *
 * Returns:	nil for success, otherwise an error describing the problem.
 *		It is for the caller to report it.
 *
 * Shortcut:	For our initial implementation we are making the simplifying
 *		restriction of using only one GPIO pin per device and limit
 *		configuration to PTT only.
 *		Longer term, we might want to have DCD, and maybe other
 *		controls thru the same chip.
 *		In this case, we would need to retain bit masks for each
 *		device so new data can be merged with old before sending it out.
 *
 *------------------------------------------------------------------*/

func SetGPIOPin(name string, num int, state int) error {
	if num < 1 || num > 8 {
		return fmt.Errorf("%s CM108 GPIO number %d must be in range of 1 thru 8", name, num)
	}

	if state != 0 && state != 1 {
		return fmt.Errorf("%s CM108 GPIO state %d must be 0 or 1", name, state)
	}

	var iomask = byte(1) << (num - 1) // 0=input, 1=output

	var iodata byte // 0=low, 1=high
	if state == 1 {
		iodata = iomask
	}

	return cm108_write(name, iomask, iodata)
} /* end SetGPIOPin */

/*-------------------------------------------------------------------
 *
 * Name:	CheckDevice
 *
 * Purpose:	Check that a HID is one of the USB audio adapters known to
 *		work for GPIO PTT.
 *
 * Inputs:	name		- Name of device such as /dev/hidraw2.
 *
 * Returns:	nil if the device reports a known good vendor and product id.
 *
 *		Otherwise an error, which wraps ErrUnknownDevice when the
 *		device could be interrogated but is an unfamiliar type.  That
 *		is advisory - such a device may still work - so a caller will
 *		generally want to warn rather than give up.  A permission
 *		problem wraps fs.ErrPermission, for which
 *		PermissionAdvice has something to say.
 *
 * Description:	This is worth doing once, when setting up, rather than on
 *		every write, which would be one warning per transmission.
 *
 *------------------------------------------------------------------*/

func CheckDevice(name string) error {
	var fd, err = os.OpenFile(name, os.O_RDWR, 0000) //nolint:gosec // This comes from user-supplied config, all we can really do is trust it
	if err != nil {
		return fmt.Errorf("could not open %s: %w", name, err)
	}
	defer fd.Close()

	var info, ioctlErr = unix.IoctlHIDGetRawInfo(int(fd.Fd()))
	if ioctlErr != nil {
		return fmt.Errorf("ioctl HIDIOCGRAWINFO failed for %s: %w", name, ioctlErr)
	}

	if !GOOD_DEVICE(int(info.Vendor), int(info.Product)) {
		return fmt.Errorf("%s (vid=%04x pid=%04x) is %w", name, info.Vendor, info.Product, ErrUnknownDevice)
	}

	return nil
} /* end CheckDevice */

/*-------------------------------------------------------------------
 *
 * Name:	cm108_write
 *
 * Purpose:	Set the GPIO pins of the CM108 or similar.
 *
 * Inputs:	name		- Name of device such as /dev/hidraw2.
 *
 *		iomask		- Bit mask for I/O direction.
 *				  LSB is GPIO1, bit 1 is GPIO2, etc.
 *				  1 for output, 0 for input.
 *
 *		iodata		- Output data, same bit order as iomask.
 *
 * Returns:	nil for success, otherwise an error describing the problem.
 *		It is for the caller to report it.  A permission problem wraps
 *		fs.ErrPermission, for which PermissionAdvice has
 *		something to say.
 *
 * Description:	This is the lowest level function.
 *		An application probably wants to use SetGPIOPin.
 *
 *------------------------------------------------------------------*/

func cm108_write(name string, iomask byte, iodata byte) error {
	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("TEMP DEBUG cm108_write:  %s %d %d\n", name, iomask, iodata);

	/*
	 * By default, the USB HID are accessible only by root:
	 *
	 *	crw------- 1 root root 249, 1 ... /dev/hidraw1
	 *
	 * How should we handle this?
	 * Manually changing it will revert back on the next reboot or
	 * when the device is removed and reinserted.
	 *
	 * According to various articles on the Internet, we should be able to
	 * add a file to /etc/udev/rules.d.  "99-direwolf-cmedia.rules" would be a
	 * suitable name.  The leading number is the order.  We want this to be
	 * near the end.  I think the file extension must be ".rules."
	 *
	 * We could completely open it up to everyone like this:
	 *
	 *	# Allow ordinary user to access CMedia GPIO for PTT.
	 *	SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0d8c", MODE="0666"
	 *
	 * Whenever we have CMedia USB audio adapter, it should be accessible by everyone.
	 * This would not apply to other /dev/hidraw* corresponding to keyboard, mouse, etc.
	 *
	 * Notice the == (double =) for testing and := for setting a property.
	 *
	 * If you are concerned about security, you could restrict access to
	 * a particular group, something like this:
	 *
	 *	SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0d8c", GROUP="audio", MODE="0660"
	 *
	 * I figure "audio" makes more sense than "gpio" because we need to be part of
	 * audio group to use the USB Audio adapter for sound.
	 */
	var fd, err = os.OpenFile(name, os.O_RDWR, 0000) //nolint:gosec // This comes from user-supplied config, all we can really do is trust it
	if err != nil {
		return fmt.Errorf("could not open %s for write: %w", name, err)
	}
	defer fd.Close()

	// To make a long story short, I think we need 0 for the first two bytes.

	var data = []byte{0, 0, iodata, iomask, 0}

	// Writing 4 bytes fails with errno 32, EPIPE, "broken pipe."
	// Hamlib writes 5 bytes which I don't understand.
	// Writing 5 bytes works.
	// I have no idea why.  From the CMedia datasheet it looks like we need 4.

	//  Errors observed during development.
	//  as pi		EACCES          13      /* Permission denied */
	//  as root		EPIPE           32      /* Broken pipe - Happens if we send 4 bytes */
	var n, writeErr = fd.Write(data)
	if writeErr != nil {
		return fmt.Errorf("write to %s failed after %d bytes: %w", name, n, writeErr)
	}

	if n != len(data) {
		return fmt.Errorf("write to %s was short, %d of %d bytes", name, n, len(data))
	}

	return nil
} /* end cm108_write */
