package direwolf

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
import "C"

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
)

/*-------------------------------------------------------------------
 *
 * Name:	main
 *
 * Purpose:	Useful utility to list USB audio and HID devices.
 *
 * Optional command line arguments:
 *
 *		HID path
 *		GPIO number (default 3)
 *
 *		When specified the pin will be set high and low until interrupted.
 *
 *------------------------------------------------------------------*/

func cm108_usage() {
	fmt.Printf("\n")
	fmt.Printf("Usage:    cm108  [ device-path [ gpio-num ] ]\n")
	fmt.Printf("\n")
	fmt.Printf("With no command line arguments, this will produce a list of\n")
	fmt.Printf("Audio devices and Human Interface Devices (HID) and indicate\n")
	fmt.Printf("which ones can be used for GPIO PTT.\n")
	fmt.Printf("\n")
	fmt.Printf("Specify the HID device path to test the PTT function.\n")
	fmt.Printf("Its state should change once per second.\n")
	fmt.Printf("GPIO 3 is the default.  A different number can be optionally specified.\n")
	os.Exit(1)
}

func CM108Main() {
	text_color_init(0) // Turn off text color.

	if len(os.Args) >= 2 {
		var path = os.Args[1]
		var gpio = 3
		if len(os.Args) >= 3 {
			gpio, _ = strconv.Atoi(os.Args[2])
		}
		if gpio < 1 || gpio > 8 {
			fmt.Printf("GPIO number must be in range of 1 - 8.\n")
			cm108_usage()
			os.Exit(1)
		}
		var state = 0
		for {
			fmt.Printf("%d", state)
			var err = cm108_set_gpio_pin(C.CString(path), C.int(gpio), C.int(state))
			if err != 0 {
				fmt.Printf("\nWRITE ERROR for USB Audio Adapter GPIO!\n")
				cm108_usage()
				os.Exit(1)
			}
			SLEEP_SEC(1)
			state = 1 - state
		}
	}

	// Take inventory of USB Audio adapters and other HID devices.

	var things, _ = cm108_inventory(MAXX_THINGS)

	/////////////////////////////////////////////
	//                Linux
	/////////////////////////////////////////////

	fmt.Printf("    VID  PID   %-*s %-*s %-*s %-*s %-*s %-*s\n", len(things[0].product), "Product",
		len(things[0].devnode_sound), "Sound",
		len(things[0].plughw)/5, "ADEVICE",
		len(things[0].plughw2)/4, "ADEVICE",
		17, "HID [ptt]", len(things[0].devnode_usb), "USB")

	fmt.Printf("    ---  ---   %-*s %-*s %-*s %-*s %-*s %-*s\n", len(things[0].product), "-------",
		len(things[0].devnode_sound), "-----",
		len(things[0].plughw)/5, "-------",
		len(things[0].plughw2)/4, "-------",
		17, "---------", len(things[0].devnode_usb), "---")
	for i := 0; i < len(things); i++ {
		var good = "  "
		if GOOD_DEVICE(things[i].vid, things[i].pid) {
			good = "**"
		}
		fmt.Printf("%2s  %04x %04x  %-*s %-*s %-*s %-*s %s %-*s\n",
			good,
			things[i].vid, things[i].pid,
			len(things[i].product), C.GoString(&things[i].product[0]),
			len(things[i].devnode_sound), C.GoString(&things[i].devnode_sound[0]),
			len(things[0].plughw)/5, C.GoString(&things[i].plughw[0]),
			len(things[0].plughw2)/4, C.GoString(&things[i].plughw2[0]),
			C.GoString(&things[i].devnode_hidraw[0]), len(things[i].devnode_usb), C.GoString(&things[i].devnode_usb[0]))
		// fmt.Printf ("             %-*s\n", len(things[i].devpath), things[i].devpath);
	}
	fmt.Printf("\n")
	fmt.Printf("** = Can use Audio Adapter GPIO for PTT.\n")
	fmt.Printf("\n")

	// From example in https://alsa.opensrc.org/Udev

	fmt.Printf("Notice that each USB Audio adapter is assigned a number and a name.  These are not predictable so you could\n")
	fmt.Printf("end up using the wrong adapter after adding or removing other USB devices or after rebooting.  You can assign a\n")
	fmt.Printf("name to each USB adapter so you can refer to the same one each time.  This can be based on any characteristics\n")
	fmt.Printf("that makes them unique such as product id or serial number.  Unfortunately these devices don't have unique serial\n")
	fmt.Printf("numbers so how can we tell them apart?  A name can also be assigned based on the physical USB socket.\n")
	fmt.Printf("Create a file like \"/etc/udev/rules.d/85-my-usb-audio.rules\" with the following contents and then reboot.\n")
	fmt.Printf("\n")
	fmt.Printf("SUBSYSTEM!=\"sound\", GOTO=\"my_usb_audio_end\"\n")
	fmt.Printf("ACTION!=\"add\", GOTO=\"my_usb_audio_end\"\n")

	// Consider only the 'devnode' paths that end with "card" and a number.
	// Replace the number with a question mark.
	var iname = 0
	var suggested_names []string = []string{"Fred", "Wilma", "Pebbles", "Dino", "Barney", "Betty", "Bamm_Bamm", "Chip", "Roxy"}
	// Drop any "/sys" at the beginning.
	var r = regexp.MustCompile("(/devices/.+/card)[0-9]$") // TODO KG Was REG_EXTENDED - may need some fiddling/checking? Can't easily test...
	for i := 0; i < len(things); i++ {
		if i == 0 || C.GoString(&things[i].devpath[0]) != C.GoString(&things[i-1].devpath[0]) {
			var matches = r.FindStringSubmatch(C.GoString(&things[i].devpath[0]))
			if len(matches) > 0 {
				var without_number = matches[0]
				fmt.Printf("DEVPATH==\"%s?\", ATTR{id}=\"%s\"\n", without_number, suggested_names[iname])
				if iname < 6 {
					iname++
				}
			}
		}
	}

	fmt.Printf("LABEL=\"my_usb_audio_end\"\n")
	fmt.Printf("\n")
}
