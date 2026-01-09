package direwolf

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

// #include "direwolf.h"
// #include <stdio.h>
// #include <stdlib.h>
// #include <locale.h>
// #include <unistd.h>
// #include <string.h>
// #include <regex.h>
// #include <libudev.h>
// #include <sys/types.h>
// #include <sys/stat.h>
// #include <sys/ioctl.h>			// ioctl, _IOR
// #include <fcntl.h>
// #include <errno.h>
// #include <linux/hidraw.h>		// for HIDIOCGRAWINFO
// #include "textcolor.h"
// #include "cm108.h"
import "C"

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"golang.org/x/sys/unix"
)

/*
 * Result of taking inventory of USB soundcards and USB HIDs.
 */

type thing_s struct {
	vid           C.int      // vendor id, displayed as four hexadecimal digits.
	pid           C.int      // product id, displayed as four hexadecimal digits.
	card_number   [8]C.char  // "Card" Number.  e.g.  2 for plughw:2,0
	card_name     [32]C.char // Audio Card Name, assigned by system (e.g. Device_1) or by udev rule.
	product       [32]C.char // product name (e.g. manufacturer, model)
	devnode_sound [22]C.char // e.g. /dev/snd/pcmC0D0p
	plughw        [72]C.char // Above in more familiar format e.g. plughw:0,0
	// Oversized to silence a compiler warning.
	plughw2        [72]C.char  // With name rather than number.
	devpath        [128]C.char // Kernel dev path.  Does not include /sys mount point.
	devnode_hidraw [C.MAXX_HIDRAW_NAME_LEN]C.char
	// e.g. /dev/hidraw3  -  for Linux - was length 17
	// The Windows path for a HID looks like this, lengths up to 95 seen.
	// \\?\hid#vid_0d8c&pid_000c&mi_03#8&164d11c9&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}
	devnode_usb [25]C.char // e.g. /dev/bus/usb/001/012
	// This is what we use to match up audio and HID.
}

const MAXX_THINGS = 60

/*-------------------------------------------------------------------
 *
 * Name:	cm108_inventory
 *
 * Purpose:	Take inventory of USB audio and HID.
 *
 * Inputs:	max_things	- Maximum number of items to collect.
 *
 * Outputs:	things		- Array of items collected.
 *				  Corresponding sound device and HID are merged into one item.
 *
 * Returns:	Number of items placed in things array.
 *		Should be in the range of 0 thru max_things.
 *		-1 for a bad unexpected error.
 *
 *------------------------------------------------------------------*/

func cm108_inventory(max_things C.int) ([]*thing_s, error) {

	var things []*thing_s

	/*
	 * First get a list of the USB audio devices.
	 * This is based on the example in http://www.signal11.us/oss/udev/
	 */
	var udev = C.udev_new()
	if udev == nil {
		text_color_set(DW_COLOR_ERROR)
		var msg = "INTERNAL ERROR: Can't create udev"
		dw_printf("%s.\n", msg)
		return things, errors.New(msg)
	}

	var enumerate = C.udev_enumerate_new(udev)
	C.udev_enumerate_add_match_subsystem(enumerate, C.CString("sound"))
	C.udev_enumerate_scan_devices(enumerate)
	var devices = C.udev_enumerate_get_list_entry(enumerate)

	var card_devpath [128]C.char
	var pattrs_id *C.char
	var pattrs_number *C.char

	/* KG Taken from udev.h:
		#define udev_list_entry_foreach(list_entry, first_entry) \
	        for (list_entry = first_entry; \
	             list_entry; \
	             list_entry = udev_list_entry_get_next(list_entry))
	*/
	// udev_list_entry_foreach(dev_list_entry, devices) {

	for dev_list_entry := devices; dev_list_entry != nil; dev_list_entry = C.udev_list_entry_get_next(dev_list_entry) {
		var path = C.udev_list_entry_get_name(dev_list_entry)
		var dev = C.udev_device_new_from_syspath(udev, path)
		var devnode = C.udev_device_get_devnode(dev)

		if devnode == nil {
			// I'm not happy with this but couldn't figure out how
			// to get attributes from one level up from the pcmC?D?? node.
			C.strcpy(&card_devpath[0], path)
			pattrs_id = C.udev_device_get_sysattr_value(dev, C.CString("id"))
			pattrs_number = C.udev_device_get_sysattr_value(dev, C.CString("number"))
			//dw_printf (" >card_devpath = %s\n", card_devpath);
			//dw_printf (" >>pattrs_id = %s\n", pattrs_id);
			//dw_printf (" >>pattrs_number = %s\n", pattrs_number);
		} else {
			var parentdev = C.udev_device_get_parent_with_subsystem_devtype(dev, C.CString("usb"), C.CString("usb_device"))
			if parentdev != nil {
				var vid C.int = 0
				var pid C.int = 0

				var p = C.udev_device_get_sysattr_value(parentdev, C.CString("idVendor"))
				if p != nil {
					vid = C.int(C.strtol(p, nil, 16))
				}

				p = C.udev_device_get_sysattr_value(parentdev, C.CString("idProduct"))
				if p != nil {
					pid = C.int(C.strtol(p, nil, 16))
				}

				if C.int(len(things)) < max_things {
					var thing = new(thing_s)

					thing.vid = vid
					thing.pid = pid
					C.strcpy(&thing.card_name[0], pattrs_id)
					C.strcpy(&thing.card_number[0], pattrs_number)
					C.strcpy(&thing.product[0], C.udev_device_get_sysattr_value(parentdev, C.CString("product")))
					C.strcpy(&thing.devnode_sound[0], devnode)
					C.strcpy(&thing.devnode_usb[0], C.udev_device_get_devnode(parentdev))
					C.strcpy(&thing.devpath[0], &card_devpath[0])

					things = append(things, thing)
				}
				C.udev_device_unref(parentdev)
			}
		}
	}
	C.udev_enumerate_unref(enumerate)
	C.udev_unref(udev)

	/*
	 * Now merge in all of the USB HID.
	 */
	udev = C.udev_new()
	if udev == nil {
		text_color_set(DW_COLOR_ERROR)
		var msg = "INTERNAL ERROR: Can't create udev"
		dw_printf("%s.\n", msg)
		return nil, errors.New(msg)
	}

	enumerate = C.udev_enumerate_new(udev)
	C.udev_enumerate_add_match_subsystem(enumerate, C.CString("hidraw"))
	C.udev_enumerate_scan_devices(enumerate)
	devices = C.udev_enumerate_get_list_entry(enumerate)
	for dev_list_entry := devices; dev_list_entry != nil; dev_list_entry = C.udev_list_entry_get_next(dev_list_entry) {
		var path = C.udev_list_entry_get_name(dev_list_entry)
		var dev = C.udev_device_new_from_syspath(udev, path)
		var devnode = C.udev_device_get_devnode(dev)
		if devnode != nil {
			var parentdev = C.udev_device_get_parent_with_subsystem_devtype(dev, C.CString("usb"), C.CString("usb_device"))
			if parentdev != nil {
				var vid C.int = 0
				var pid C.int = 0

				var p = C.udev_device_get_sysattr_value(parentdev, C.CString("idVendor"))
				if p != nil {
					vid = C.int(C.strtol(p, nil, 16))
				}
				p = C.udev_device_get_sysattr_value(parentdev, C.CString("idProduct"))
				if p != nil {
					pid = C.int(C.strtol(p, nil, 16))
				}

				var usb = C.udev_device_get_devnode(parentdev)

				// Add hidraw name to any matching existing.
				var matched = false
				for _, thing := range things {
					if thing.vid == vid && thing.pid == pid && usb != nil && C.strcmp(&thing.devnode_usb[0], usb) == 0 {
						matched = true
						C.strcpy(&thing.devnode_hidraw[0], devnode)
					}
				}

				// If it did not match to existing, add new entry.
				if !matched && C.int(len(things)) < max_things {
					var thing = new(thing_s)

					thing.vid = vid
					thing.pid = pid
					C.strcpy(&thing.product[0], C.udev_device_get_sysattr_value(parentdev, C.CString("product")))
					C.strcpy(&thing.devnode_hidraw[0], devnode)
					C.strcpy(&thing.devnode_usb[0], usb)
					C.strcpy(&thing.devpath[0], C.udev_device_get_devpath(dev))

					things = append(things, thing)
				}
				C.udev_device_unref(parentdev)
			}
		}
	}
	C.udev_enumerate_unref(enumerate)
	C.udev_unref(udev)

	/*
	 * Seeing the form /dev/snd/pcmC4D0p will be confusing to many because we
	 * would generally something like plughw:4,0 for in the direwolf configuration file.
	 * Construct the more familiar form.
	 * Previously we only used the numeric form.  In version 1.6, the name is listed as well
	 * and we describe how to assign names based on the physical USB socket for repeatability.
	 */
	var pcm_re = regexp.MustCompile("pcmC([0-9]+)D([0-9]+)[cp]")

	for _, thing := range things {
		var matches = pcm_re.FindStringSubmatch(C.GoString(&thing.devnode_sound[0]))

		if matches != nil {
			var c = matches[1]
			var d = matches[2]

			C.strcpy(&thing.plughw[0], C.CString(fmt.Sprintf("plughw:%s,%s", c, d)))
			C.strcpy(&thing.plughw2[0], C.CString(fmt.Sprintf("plughw:%s,%s", C.GoString(&thing.card_name[0]), d)))
		}
	}

	return things, nil

} /* end cm108_inventory */

/*-------------------------------------------------------------------
 *
 * Name:	cm108_find_ptt
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
 *		ptt_device_size	- Size of result area to avoid buffer overflow.
 *
 * Outputs:	ptt_device	- Device name, something like /dev/hidraw2.
 *				  Will be empty string if no match found.
 *
 * Returns:	none
 *
 *------------------------------------------------------------------*/

//export cm108_find_ptt
func cm108_find_ptt(output_audio_device *C.char, ptt_device *C.char, ptt_device_size C.int) {

	//dw_printf ("DEBUG: cm108_find_ptt('%s')\n", output_audio_device);

	C.strlcpy(ptt_device, C.CString(""), C.size_t(ptt_device_size))

	// Possible improvement: Skip if inventory already taken.
	var things, _ = cm108_inventory(MAXX_THINGS)

	var sound_re = regexp.MustCompile(".+:(CARD=)?([A-Za-z0-9_]+)(,.*)?")

	var matches = sound_re.FindStringSubmatch(C.GoString(output_audio_device))
	var num_or_name string

	if matches != nil {
		num_or_name = matches[2]
		//dw_printf ("DEBUG: Got '%s' from '%s'\n", num_or_name, output_audio_device);
	}

	if len(num_or_name) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not extract card number or name from %s\n", C.GoString(output_audio_device))
		dw_printf("Can't automatically find matching HID for PTT.\n")
		return
	}

	for _, thing := range things {
		//dw_printf ("DEBUG: i=%d, card_name='%s', card_number='%s'\n", i, things[i].card_name, things[i].card_number);
		if num_or_name == C.GoString(&thing.card_name[0]) || num_or_name == C.GoString(&thing.card_number[0]) {
			//dw_printf ("DEBUG: success! returning '%s'\n", things[i].devnode_hidraw);
			C.strlcpy(ptt_device, &thing.devnode_hidraw[0], C.size_t(ptt_device_size))
			if !GOOD_DEVICE(thing.vid, thing.pid) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Warning: USB audio card %s (%s) is not a device known to work with GPIO PTT.\n",
					C.GoString(&thing.card_number[0]), C.GoString(&thing.card_name[0]))
			}
			return
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:	cm108_set_gpio_pin
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
 * Returns:	0 for success.  -1 for error.
 *
 * Errors:	A descriptive error message will be printed for any problem.
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

//export cm108_set_gpio_pin
func cm108_set_gpio_pin(name *C.char, num C.int, state C.int) C.int {

	if num < 1 || num > 8 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("%s CM108 GPIO number %d must be in range of 1 thru 8.\n", C.GoString(name), num)
		return (-1)
	}

	if state != 0 && state != 1 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("%s CM108 GPIO state %d must be 0 or 1.\n", C.GoString(name), state)
		return (-1)
	}

	var iomask = 1 << (num - 1)     // 0=input, 1=output
	var iodata = state << (num - 1) // 0=low, 1=high
	return (cm108_write(name, C.int(iomask), iodata))
} /* end cm108_set_gpio_pin */

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
 * Returns:	0 for success.  -1 for error.
 *
 * Errors:	A descriptive error message will be printed for any problem.
 *
 * Description:	This is the lowest level function.
 *		An application probably wants to use cm108_set_gpio_pin.
 *
 *------------------------------------------------------------------*/

func cm108_write(name *C.char, iomask C.int, iodata C.int) C.int {

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

	var fd, err = os.OpenFile(C.GoString(name), os.O_RDWR, 0000)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not open %s for write: %s\n", C.GoString(name), err)
		/* TODO KG UX
		if errno == EACCES { // 13
			dw_printf("Type \"ls -l %s\" and verify that it has audio group rw similar to this:\n", name)
			dw_printf("    crw-rw---- 1 root audio 247, 0 Oct  6 19:24 %s\n", name)
			dw_printf("rather than root-only access like this:\n")
			dw_printf("    crw------- 1 root root 247, 0 Sep 24 09:40 %s\n", name)
		}
		*/
		return (-1)
	}
	defer fd.Close()

	// Just for fun, let's get the device information.

	var info, ioctlErr = unix.IoctlHIDGetRawInfo(int(fd.Fd()))
	if ioctlErr == nil {
		if !GOOD_DEVICE(C.int(info.Vendor), C.int(info.Product)) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("ioctl HIDIOCGRAWINFO failed for %s. errno = %s.\n", C.GoString(name), ioctlErr)
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%s is not a supported device type.  Proceed at your own risk.  vid=%04x pid=%04x\n", C.GoString(name), info.Vendor, info.Product)
		}
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

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Write to %s failed, n=%d, err=%d\n", C.GoString(name), n, err)

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
		return (-1)
	}

	return (0)
} /* end cm108_write */
