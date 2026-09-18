// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package cm108

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/jochenvg/go-udev"
	"golang.org/x/sys/unix"
)

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
 * Returns:	The items found, at most max_things of them, and an error if the
 *		udev enumeration failed unexpectedly.
 *
 *------------------------------------------------------------------*/

func Inventory(max_things int) ([]*Thing, error) {
	var things []*Thing

	/*
	 * First get a list of the USB audio devices.
	 * This is based on the example in http://www.signal11.us/oss/udev/
	 */
	var u = udev.Udev{}
	var e = u.NewEnumerate()
	e.AddMatchSubsystem("sound")

	var devices, devicesErr = e.Devices()
	if devicesErr != nil {
		return things, fmt.Errorf("INTERNAL ERROR: Can't enumerate udev devices: %w", devicesErr)
	}

	var cardDevpath string
	var pattrsID string
	var pattrsNumber string

	for _, dev := range devices {
		var devnode = dev.Devnode()

		if devnode == "" {
			// I'm not happy with this but couldn't figure out how
			// to get attributes from one level up from the pcmC?D?? node.
			cardDevpath = dev.Syspath()
			pattrsID = dev.SysattrValue("id")
			pattrsNumber = dev.SysattrValue("number")
		} else {
			var parentdev = dev.ParentWithSubsystemDevtype("usb", "usb_device")
			if parentdev != nil {
				var vid int
				var pid int

				var p = parentdev.SysattrValue("idVendor")
				if p != "" {
					var vid64, _ = strconv.ParseInt(p, 16, 0)
					vid = int(vid64)
				}

				p = parentdev.SysattrValue("idProduct")
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
					thing.Product = parentdev.SysattrValue("product")
					thing.DevnodeSound = devnode
					thing.DevnodeUSB = parentdev.Devnode()
					thing.Devpath = cardDevpath

					things = append(things, thing)
				}
			}
		}
	}

	/*
	 * Now merge in all of the USB HID.
	 */
	var e2 = u.NewEnumerate()
	e2.AddMatchSubsystem("hidraw")

	var hidDevices, hidDevicesErr = e2.Devices()
	if hidDevicesErr != nil {
		return nil, fmt.Errorf("INTERNAL ERROR: Can't enumerate udev hidraw devices: %w", hidDevicesErr)
	}

	for _, dev := range hidDevices {
		var devnode = dev.Devnode()
		if devnode != "" {
			var parentdev = dev.ParentWithSubsystemDevtype("usb", "usb_device")
			if parentdev != nil {
				var vid int
				var pid int

				var p = parentdev.SysattrValue("idVendor")
				if p != "" {
					var vid64, _ = strconv.ParseInt(p, 16, 0)
					vid = int(vid64)
				}

				p = parentdev.SysattrValue("idProduct")
				if p != "" {
					var pid64, _ = strconv.ParseInt(p, 16, 0)
					pid = int(pid64)
				}

				var usb = parentdev.Devnode()

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
					thing.Product = parentdev.SysattrValue("product")
					thing.DevnodeHidraw = devnode
					thing.DevnodeUSB = usb
					thing.Devpath = dev.Devpath()

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

	return things, nil
} /* end Inventory */

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
 * Returns:	The matching device, whose DevnodeHidraw is something like
 *		/dev/hidraw2.  Nil, with no error, if nothing matched.
 *
 *		The caller is expected to check GoodDevice on the result: a match
 *		that is not a known-good device can still be used, but deserves a
 *		warning.
 *
 *------------------------------------------------------------------*/

func FindPTT(output_audio_device string) (*Thing, error) {
	// Possible improvement: Skip if inventory already taken.
	var things, inventoryErr = Inventory(MaxThings)
	if inventoryErr != nil {
		return nil, inventoryErr
	}

	var sound_re = regexp.MustCompile(".+:(CARD=)?([A-Za-z0-9_]+)(,.*)?")

	var matches = sound_re.FindStringSubmatch(output_audio_device)
	var num_or_name string

	if matches != nil {
		num_or_name = matches[2]
	}

	if len(num_or_name) == 0 {
		return nil, fmt.Errorf("could not extract card number or name from %s", output_audio_device)
	}

	for _, thing := range things {
		if num_or_name == thing.CardName || num_or_name == thing.CardNumber {
			return thing, nil
		}
	}

	return nil, nil //nolint:nilnil // No match is an ordinary outcome, not an error - the caller may yet be told the device explicitly
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
 * Returns:	Nil for success, otherwise a descriptive error.
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

	var iomask = 1 << (num - 1)     // 0=input, 1=output
	var iodata = state << (num - 1) // 0=low, 1=high

	return write(name, iomask, iodata)
} /* end SetGPIOPin */

/*-------------------------------------------------------------------
 *
 * Name:	write
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
 * Returns:	Nil for success, otherwise a descriptive error.
 *
 * Description:	This is the lowest level function.
 *		An application probably wants to use SetGPIOPin.
 *
 *------------------------------------------------------------------*/

func write(name string, iomask int, iodata int) error {
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
		/* TODO KG UX
		if errno == EACCES { // 13
			dw_printf("Type \"ls -l %s\" and verify that it has audio group rw similar to this:\n", name)
			dw_printf("    crw-rw---- 1 root audio 247, 0 Oct  6 19:24 %s\n", name)
			dw_printf("rather than root-only access like this:\n")
			dw_printf("    crw------- 1 root root 247, 0 Sep 24 09:40 %s\n", name)
		}
		*/
		return fmt.Errorf("could not open %s for write: %w", name, err)
	}
	defer fd.Close()

	// Just for fun, let's get the device information.
	// This is only ever a warning, so it is printed rather than returned - note
	// that direwolf's dw_printf is itself just fmt.Printf.

	var info, ioctlErr = unix.IoctlHIDGetRawInfo(int(fd.Fd()))

	var warning = deviceWarning(name, info, ioctlErr)
	if warning != "" {
		fmt.Print(warning)
	}

	// To make a long story short, I think we need 0 for the first two bytes.

	var data = []byte{0, 0, byte(iodata), byte(iomask), 0}

	// Writing 4 bytes fails with errno 32, EPIPE, "broken pipe."
	// Hamlib writes 5 bytes which I don't understand.
	// Writing 5 bytes works.
	// I have no idea why.  From the CMedia datasheet it looks like we need 4.

	var n, writeErr = fd.Write(data)
	if writeErr != nil || n != len(data) {
		//  Errors observed during development.
		//  as pi		EACCES          13      /* Permission denied */
		//  as root		EPIPE           32      /* Broken pipe - Happens if we send 4 bytes */

		/* TODO KG UX
		if errno == EACCES {
			dw_printf("Type \"ls -l %s\" and verify that it has audio group rw similar to this:\n", name)
			dw_printf("    crw-rw---- 1 root audio 247, 0 Oct  6 19:24 %s\n", name)
			dw_printf("rather than root-only access like this:\n")
			dw_printf("    crw------- 1 root root 247, 0 Sep 24 09:40 %s\n", name)
			dw_printf("This permission should be set by one of:\n")
			dw_printf("/etc/udev/rules.d/99-direwolf-cmedia.rules\n")
			dw_printf("/usr/lib/udev/rules.d/99-direwolf-cmedia.rules\n")
			dw_printf("which should be created by the installation process.\n")
			dw_printf("Your account must be in the 'audio' group.\n")
		}
		*/
		if writeErr == nil {
			writeErr = errors.New("short write")
		}

		return fmt.Errorf("write to %s failed, n=%d: %w", name, n, writeErr)
	}

	return nil
} /* end write */

// deviceWarning describes anything untoward about the HID whose device
// information was just fetched, or returns "" if there is nothing to say.
//
// Dire Wolf's cm108.c has the two messages the wrong way round: it reports an
// ioctl failure when the ioctl succeeded but named an unrecognised device, and
// reports an unsupported device - using the device information the failed
// ioctl never filled in - when the ioctl itself failed.  We deliberately
// differ from upstream here.
func deviceWarning(name string, info *unix.HIDRawDevInfo, ioctlErr error) string {
	if ioctlErr != nil {
		return fmt.Sprintf("ioctl HIDIOCGRAWINFO failed for %s. errno = %v.\n", name, ioctlErr)
	}

	if info == nil || GoodDevice(int(info.Vendor), int(info.Product)) {
		return ""
	}

	// Vendor and Product are int16, matching the kernel's signed hidraw_devinfo
	// fields, but the message promises four hexadecimal digits.
	return fmt.Sprintf("%s is not a supported device type.  Proceed at your own risk.  vid=%04x pid=%04x\n",
		name, uint16(info.Vendor), uint16(info.Product))
}
