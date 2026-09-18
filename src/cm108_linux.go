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
 * Name:	CM108Inventory
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

func CM108Inventory(max_things int) ([]*CM108Thing, error) {
	var things []*CM108Thing

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
					var thing = new(CM108Thing)

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
					var thing = new(CM108Thing)

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
} /* end CM108Inventory */

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
 * Returns:	The matching device, whose DevnodeHidraw is something like
 *		/dev/hidraw2.  Nil, with no error, if nothing matched.
 *
 *		The caller is expected to check GOOD_DEVICE on the result: a match
 *		that is not a known-good device can still be used, but deserves a
 *		warning.
 *
 *------------------------------------------------------------------*/

func cm108_find_ptt(output_audio_device string) (*CM108Thing, error) {
	// Possible improvement: Skip if inventory already taken.
	var things, inventoryErr = CM108Inventory(MAXX_THINGS)
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
 * Name:	CM108SetGPIOPin
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

func CM108SetGPIOPin(name string, num int, state int) error {
	if num < 1 || num > 8 {
		return fmt.Errorf("%s CM108 GPIO number %d must be in range of 1 thru 8", name, num)
	}

	if state != 0 && state != 1 {
		return fmt.Errorf("%s CM108 GPIO state %d must be 0 or 1", name, state)
	}

	var iomask = 1 << (num - 1)     // 0=input, 1=output
	var iodata = state << (num - 1) // 0=low, 1=high

	return cm108_write(name, iomask, iodata)
} /* end CM108SetGPIOPin */

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
 * Returns:	Nil for success, otherwise a descriptive error.
 *
 * Description:	This is the lowest level function.
 *		An application probably wants to use CM108SetGPIOPin.
 *
 *------------------------------------------------------------------*/

func cm108_write(name string, iomask int, iodata int) error {
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

	var info, ioctlErr = unix.IoctlHIDGetRawInfo(int(fd.Fd()))
	if ioctlErr == nil {
		if !GOOD_DEVICE(int(info.Vendor), int(info.Product)) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("ioctl HIDIOCGRAWINFO failed for %s. errno = %s.\n", name, ioctlErr)
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%s is not a supported device type.  Proceed at your own risk.  vid=%04x pid=%04x\n", name, info.Vendor, info.Product)
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
} /* end cm108_write */
