package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Activate the output control lines for push to talk (PTT) and other purposes.
 *
 * Description:	Traditionally this is done with the RTS signal of the serial port.
 *
 *		If we have two radio channels and only one serial port, DTR
 *		can be used for the second channel.
 *
 *		If __WIN32__ is defined, we use the Windows interface.
 *		Otherwise we use the Linux interface.
 *
 * Version 0.9:	Add ability to use GPIO pins on Linux.
 *
 * Version 1.1: Add parallel printer port for x86 Linux only.
 *
 *		This is hardcoded to use the primary motherboard parallel
 *		printer port at I/O address 0x378.  This might work with
 *		a PCI card configured to use the same address if the
 *		motherboard does not have a built in parallel port.
 *		It won't work with a USB-to-parallel-printer-port adapter.
 *
 * Version 1.2: More than two radio channels.
 *		Generalize for additional signals besides PTT.
 *
 * Version 1.3:	HAMLIB support.
 *
 * Version 1.4:	The spare "future" indicator is now used when connected to another station.
 *
 *		Take advantage of the new 'gpio' group and new /sys/class/gpio protections in Raspbian Jessie.
 *
 *		Handle more complicated gpio node names for CubieBoard, etc.
 *
 * Version 1.5:	Ability to use GPIO pins of CM108/CM119 for PTT signal.
 *
 *
 * References:	http://www.robbayer.com/files/serial-win.pdf
 *
 *		https://www.kernel.org/doc/Documentation/gpio.txt
 *
 *---------------------------------------------------------------*/

/*
	A growing number of people have been asking about support for the DMK URI,
	RB-USB RIM, etc.

	These use a C-Media CM108/CM119 with an interesting addition, a GPIO
	pin is used to drive PTT.  Here is some related information.

	DMK URI:

		http://www.dmkeng.com/URI_Order_Page.htm
		http://dmkeng.com/images/URI%20Schematic.pdf

	RB-USB RIM:

		http://www.repeater-builder.com/products/usb-rim-lite.html
		http://www.repeater-builder.com/voip/pdf/cm119-datasheet.pdf

	RA-35:

		http://www.masterscommunications.com/products/radio-adapter/ra35.html

	DINAH:

		https://hamprojects.info/dinah/


	Homebrew versions of the same idea:

		http://images.ohnosec.org/usbfob.pdf
		http://www.qsl.net/kb9mwr/projects/voip/usbfob-119.pdf
		http://rtpdir.weebly.com/uploads/1/6/8/7/1687703/usbfob.pdf
		http://www.repeater-builder.com/projects/fob/USB-Fob-Construction.pdf

	Applications that have support for this:

		http://docs.allstarlink.org/drupal/
		http://soundmodem.sourcearchive.com/documentation/0.16-1/ptt_8c_source.html
		https://github.com/N0NB/hamlib/blob/master/src/cm108.c#L190
		http://permalink.gmane.org/gmane.linux.hams.hamlib.devel/3420

	Information about the "hidraw" device:

		http://unix.stackexchange.com/questions/85379/dev-hidraw-read-permissions
		http://www.signal11.us/oss/udev/
		http://www.signal11.us/oss/hidapi/
		https://github.com/signal11/hidapi/blob/master/libusb/hid.c
		http://stackoverflow.com/questions/899008/howto-write-to-the-gpio-pin-of-the-cm108-chip-in-linux
		https://www.kernel.org/doc/Documentation/hid/hidraw.txt
		https://github.com/torvalds/linux/blob/master/samples/hidraw/hid-example.c

	Similar chips: SSS1621, SSS1623

		https://irongarment.wordpress.com/2011/03/29/cm108-compatible-chips-with-gpio/

	Here is an attempt to add direct CM108 support.
	Seems to be hardcoded for only a single USB audio adapter.

		https://github.com/donothingloop/direwolf_cm108

	In version 1.3, we add HAMLIB support which should be able to do this in a roundabout way.
	(Linux only at this point.)

	This is documented in the User Guide, section called,
		"Hamlib PTT Example 2: Use GPIO of USB audio adapter.  (e.g. DMK URI)"

	It's rather involved and the explanation doesn't cover the case of multiple
	USB-Audio adapters.  It would be nice to have a little script which lists all
	of the USB-Audio adapters and the corresponding /dev/hidraw device.
	( We now have it.  The included "cm108" application. )

	In version 1.5 we have a flexible, easy to use implementation for Linux.
	Windows would be a lot of extra work because USB devices are nothing like Linux.
	We'd be starting from scratch to figure out how to do it.
*/

// #include "direwolf.h"		// should be first.   This includes windows.h.
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <time.h>
// #include <sys/termios.h>
// #include <sys/ioctl.h>
// #include <fcntl.h>
// #include <sys/types.h>
// #include <sys/stat.h>
// #include <unistd.h>
// #include <errno.h>
// #include <grp.h>
// #include <dirent.h>
// #include <hamlib/rig.h>
// #include <gpiod.h>
// #include "cm108.h"
// #include "audio.h"
// #include "ptt.h"
// #include "dlq.h"
// #include "demod.h"	// to mute recv audio during xmit if half duplex.
import "C"

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func _TIOCM(fd int, value int, on bool) {
	var stuff, _ = unix.IoctlGetInt(fd, unix.TIOCMGET)
	if on {
		stuff |= value
	} else {
		stuff &= ^value
	}
	unix.IoctlSetInt(fd, unix.TIOCMSET, stuff)
}

func RTS_ON(fd uintptr) {
	_TIOCM(int(fd), unix.TIOCM_RTS, true)
}

func RTS_OFF(fd uintptr) {
	_TIOCM(int(fd), unix.TIOCM_RTS, false)
}

func DTR_ON(fd uintptr) {
	_TIOCM(int(fd), unix.TIOCM_DTR, true)
}

func DTR_OFF(fd uintptr) {
	_TIOCM(int(fd), unix.TIOCM_DTR, false)
}

const LPT_IO_ADDR = 0x378

// TODO KG static struct audio_s *save_audio_config_p;	/* Save config information for later use. */

var ptt_debug_level = 0

func ptt_set_debug(debug int) {
	ptt_debug_level = debug
}

/*-------------------------------------------------------------------
 *
 * Name:	get_access_to_gpio
 *
 * Purpose:	Try to get access to the GPIO device.
 *
 * Inputs:	path		- Path to device node.
 *					/sys/class/gpio/export
 *					/sys/class/gpio/unexport
 *					/sys/class/gpio/gpio??/direction
 *					/sys/class/gpio/gpio??/value
 *
 * Description:	First see if we have access thru the usual uid/gid/mode method.
 *		If that fails, we try a hack where we use "sudo chmod ..." to open up access.
 *		That requires that sudo be configured to work without a password.
 *		That's the case for 'pi' user in Raspbian but not not be for other boards / operating systems.
 *
 * Debug:	Use the "-doo" command line option.
 *
 *------------------------------------------------------------------*/

const MAX_GROUPS = 50

func get_access_to_gpio(path string) {

	/*
	 * Does path even exist?
	 */

	var _, err = os.Stat(path)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Can't get properties of %s: %s\n", path, err)
		dw_printf("This system is not configured with the GPIO user interface.\n")
		dw_printf("Use a different method for PTT control.\n")
		os.Exit(1)
	}

	var my_uid = os.Geteuid()
	var my_gid = os.Getegid()
	var my_groups, groupsErr = os.Getgroups()

	if groupsErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Getgroups() failed to get supplementary groups, err=%s\n", groupsErr)
	}

	if ptt_debug_level >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		// TODO KG dw_printf("%s: uid=%d, gid=%d, mode=o%o\n", path, finfo.st_uid, finfo.st_gid, finfo.st_mode)
		dw_printf("my uid=%d, gid=%d, supplementary groups=", my_uid, my_gid)
		for _, g := range my_groups {
			dw_printf(" %d", g)
		}
		dw_printf("\n")
	}

	/*
	 * Do we have permission to access it?
	 *
	 * On Debian 7 (Wheezy) we see this:
	 *
	 *	$ ls -l /sys/class/gpio/export
	 *	--w------- 1 root root 4096 Feb 27 12:31 /sys/class/gpio/export
	 *
	 *
	 * Only root can write to it.
	 * Our work-around is change the protection so that everyone can write.
	 * This requires that the current user can use sudo without a password.
	 * This has been the case for the predefined "pi" user but can be a problem
	 * when people add new user names.
	 * Other operating systems could have different default configurations.
	 *
	 * A better solution is available in Debian 8 (Jessie).  The group is now "gpio"
	 * so anyone in that group can now write to it.
	 *
	 *	$ ls -l /sys/class/gpio/export
	 *	-rwxrwx--- 1 root gpio 4096 Mar  4 21:12 /sys/class/gpio/export
	 *
	 *
	 * First see if we can access it by the usual file protection rules.
	 * If not, we will try the "sudo chmod go+rw ..." hack.
	 *
	 */

	// TODO KG I don't love what was here, but I need to figure out what (if anything) I want to replace it with
}

/*-------------------------------------------------------------------
 *
 * Name:	export_gpio
 *
 * Purpose:	Tell the GPIO subsystem to export a GPIO line for
 * 		us to use, and set the initial state of the GPIO.
 *
 * Inputs:	ch		- Radio Channel.
 *		ot		- Output type.
 *		invert:		- Is the GPIO active low?
 *		direction:	- 0 for input, 1 for output
 *
 * Outputs:	out_gpio_name	- in the audio configuration structure.
 *		in_gpio_name
 *
 *------------------------------------------------------------------*/

func export_gpio(ch C.int, ot C.int, invert C.int, direction C.int) {

	// Raspberry Pi was easy.  GPIO 24 has the name gpio24.
	// Others, such as the Cubieboard, take a little more effort.
	// The name might be gpio24_ph11 meaning connector H, pin 11.
	// When we "export" GPIO number, we will store the corresponding
	// device name for future use when we want to access it.

	var gpio_num C.int
	var gpio_name string

	if direction > 0 {
		gpio_num = save_audio_config_p.achan[ch].octrl[ot].out_gpio_num
		gpio_name = C.GoString(&save_audio_config_p.achan[ch].octrl[ot].out_gpio_name[0])
	} else {
		gpio_num = save_audio_config_p.achan[ch].ictrl[ot].in_gpio_num
		gpio_name = C.GoString(&save_audio_config_p.achan[ch].ictrl[ot].in_gpio_name[0])
	}

	const gpio_export_path = "/sys/class/gpio/export"

	get_access_to_gpio(gpio_export_path)

	var fd, err = os.OpenFile(gpio_export_path, os.O_WRONLY, 0)
	if err != nil {
		// Not expected.  Above should have obtained permission or exited.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Permissions do not allow access to GPIO.\n")
		os.Exit(1)
	}

	var stemp = strconv.Itoa(int(gpio_num))
	var n, writeErr = fd.WriteString(stemp)
	if n != len(stemp) || writeErr != nil { //nolint: staticcheck
		/* TODO KG Figure out write errs here

		// Ignore EBUSY error which seems to mean the device node already exists.
		if err != EBUSY {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error writing \"%s\" to %s, errno=%d\n", stemp, gpio_export_path, e)
			dw_printf("%s\n", strerror(e))

			if e == 22 {
				// It appears that error 22 occurs when sysfs gpio is not available.
				// (See https://github.com/wb2osz/direwolf/issues/503)
				//
				// The solution might be to use the new gpiod approach.

				dw_printf("It looks like gpio with sysfs is not supported on this operating system.\n")
				dw_printf("Rather than the following form, in the configuration file,\n")
				dw_printf("    PTT GPIO  %s\n", stemp)
				dw_printf("try using gpiod form instead.  e.g.\n")
				dw_printf("    PTT GPIOD  gpiochip0  %s\n", stemp)
				dw_printf("You can get a list of gpio chip names and corresponding I/O lines with \"gpioinfo\" command.\n")
			}
			exit(1)
		}
		*/
	}
	/* Wait for udev to adjust permissions after enabling GPIO. */
	/* https://github.com/wb2osz/direwolf/issues/176 */
	SLEEP_MS(250)
	fd.Close()

	/*
	 *	Added in release 1.4.
	 *
	 *	On the RPi, the device path for GPIO number XX is simply /sys/class/gpio/gpioXX.
	 *
	 *	There was a report that it is different for the CubieBoard.  For instance
	 *	GPIO 61 has gpio61_pi13 in the path.  This indicates connector "i" pin 13.
	 *	https://github.com/cubieplayer/Cubian/wiki/GPIO-Introduction
	 *
	 *	For another similar single board computer, we find the same thing:
	 *	https://www.olimex.com/wiki/A20-OLinuXino-LIME#GPIO_under_Linux
	 *
	 *	How should we deal with this?  Some possibilities:
	 *
	 *	(1) The user might explicitly mention the name in direwolf.conf.
	 *	(2) We might be able to find the names in some system device config file.
	 *	(3) Get a directory listing of /sys/class/gpio then search for a
	 *		matching name.  Suppose we wanted GPIO 61.  First look for an exact
	 *		match to "gpio61".  If that is not found, look for something
	 *		matching the pattern "gpio61_*".
	 *
	 *	We are finally implementing the third choice.
	 */

	/*
	 * Then we have the Odroid board with GPIO numbers starting around 480.
	 * Can we simply use those numbers?
	 * Apparently, the export names look like GPIOX.17
	 * https://wiki.odroid.com/odroid-c4/hardware/expansion_connectors#gpio_map_for_wiringpi_library
	 */

	if ptt_debug_level >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("Contents of /sys/class/gpio:\n")
	}

	var dirEntries, readDirErr = os.ReadDir("/sys/class/gpio")

	var ok = false
	if readDirErr != nil {
		// Something went wrong.  Fill in the simple expected name and keep going.

		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR! Could not get directory listing for /sys/class/gpio\n")

		gpio_name = fmt.Sprintf("gpio%d", gpio_num)
		ok = true
	} else {
		if ptt_debug_level >= 2 {

			text_color_set(DW_COLOR_DEBUG)

			for _, entry := range dirEntries {
				dw_printf("\t%s\n", entry.Name())
			}
		}

		// Look for exact name gpioNN

		var lookfor = fmt.Sprintf("gpio%d", gpio_num)

		for _, entry := range dirEntries {
			if lookfor == entry.Name() {
				gpio_name = entry.Name()
				ok = true
			}
		}

		// If not found, Look for gpioNN_*

		lookfor = fmt.Sprintf("gpio%d_", gpio_num)

		for _, entry := range dirEntries {
			if strings.HasPrefix(entry.Name(), lookfor) {
				gpio_name = entry.Name()
				ok = true
			}
		}
	}

	/*
	 * We should now have the corresponding node name.
	 */
	if ok {

		if ptt_debug_level >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Path for gpio number %d is /sys/class/gpio/%s\n", gpio_num, gpio_name)
		}
	} else {

		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR! Could not find Path for gpio number %d.n", gpio_num)
		exit(1)
	}

	/*
	 * Set output direction and initial state
	 */

	var gpio_direction_path = fmt.Sprintf("/sys/class/gpio/%s/direction", gpio_name)
	get_access_to_gpio(gpio_direction_path)

	fd, err = os.OpenFile(gpio_direction_path, os.O_WRONLY, 0)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Error opening %s\n", gpio_direction_path)
		dw_printf("%s\n", err)
		os.Exit(1)
	}

	var gpio_val string
	if direction != 0 {
		if invert != 0 {
			gpio_val = "high"
		} else {
			gpio_val = "low"
		}
	} else {
		gpio_val = "in"
	}

	n, writeErr = fd.WriteString(gpio_val)
	if n != len(gpio_val) || writeErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Error writing initial state to %s\n", gpio_direction_path)
		dw_printf("%s\n", writeErr)
		os.Exit(1)
	}
	fd.Close()

	/*
	 * Make sure that we have access to 'value'.
	 * Do it once here, rather than each time we want to use it.
	 */

	var gpio_value_path = fmt.Sprintf("/sys/class/gpio/%s/value", gpio_name)
	get_access_to_gpio(gpio_value_path)
}

func gpiod_probe(chip_dev_path string, line_number C.int) C.int {
	// chip_dev_path must be complete device path such as /dev/gpiochip3

	var chip = C.gpiod_chip_open(C.CString(chip_dev_path))
	if chip == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Can't open GPIOD chip %s.\n", chip_dev_path)
		return -1
	}

	/* FIXME KG gpiod version compatibility issues :(
	var line = C.gpiod_chip_get_line_info(chip, C.uint(line_number))
	if line == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Can't get GPIOD line %d.\n", line_number)
		return -1
	}
	if ptt_debug_level >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("GPIOD probe OK. Chip: %s line: %d\n", chip_dev_path, line_number)
	}
	*/

	return 0
}

/*-------------------------------------------------------------------
 *
 * Name:        ptt_init
 *
 * Purpose:    	Open serial port(s) used for PTT signals and set to proper state.
 *
 * Inputs:	audio_config_p		- Structure with communication parameters.
 *
 *		    for each channel we have:
 *
 *			ptt_method	Method for PTT signal.
 *					PTT_METHOD_NONE - not configured.  Could be using VOX.
 *					PTT_METHOD_SERIAL - serial (com) port.
 *					PTT_METHOD_GPIO - general purpose I/O (sysfs).
 *					PTT_METHOD_GPIOD - general purpose I/O (libgpiod).
 *					PTT_METHOD_LPT - Parallel printer port.
 *                  			PTT_METHOD_HAMLIB - HAMLib rig control.
 *					PTT_METHOD_CM108 - GPIO pins of CM108 etc. USB Audio.
 *
 *			ptt_device	Name of serial port device.
 *					 e.g. COM1 or /dev/ttyS0.
 *					 HAMLIB can also use hostaddr:port.
 *					 Like /dev/hidraw1 for CM108.
 *
 *			ptt_line	RTS or DTR when using serial port.
 *
 *			out_gpio_num	GPIO number.  Only used for Linux.
 *					 Valid only when ptt_method is PTT_METHOD_GPIO.
 *
 *			ptt_lpt_bit	Bit number for parallel printer port.
 *					 Bit 0 = pin 2, ..., bit 7 = pin 9.
 *					 Valid only when ptt_method is PTT_METHOD_LPT.
 *
 *			ptt_invert	Invert the signal.
 *					 Normally higher voltage means transmit or LED on.
 *
 *			ptt_model	Only for HAMLIB.
 *					2 to communicate with rigctld.
 *					>= 3 for specific radio model.
 *					-1 guess at what is out there.  (AUTO option in config file.)
 *
 * Outputs:	Remember required information for future use.
 *
 * Description:
 *
 *--------------------------------------------------------------------*/

var ptt_fd [MAX_RADIO_CHANS][NUM_OCTYPES]*os.File

/* Serial port handle or fd.  */
/* Could be the same for two channels */
/* if using both RTS and DTR. */
var rig [MAX_RADIO_CHANS][NUM_OCTYPES]*C.RIG

var otnames [NUM_OCTYPES]string

func ptt_init(audio_config_p *C.struct_audio_s) {

	save_audio_config_p = audio_config_p

	otnames[OCTYPE_PTT] = "PTT"
	otnames[OCTYPE_DCD] = "DCD"
	otnames[OCTYPE_CON] = "CON"

	for ch := 0; ch < MAX_RADIO_CHANS; ch++ {
		for ot := 0; ot < NUM_OCTYPES; ot++ {
			if ptt_debug_level >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("ch=%d, %s method=%d, device=%s, line=%d, name=%s, gpio=%d, lpt_bit=%d, invert=%d\n",
					ch,
					otnames[ot],
					audio_config_p.achan[ch].octrl[ot].ptt_method,
					C.GoString(&audio_config_p.achan[ch].octrl[ot].ptt_device[0]),
					audio_config_p.achan[ch].octrl[ot].ptt_line,
					C.GoString(&audio_config_p.achan[ch].octrl[ot].out_gpio_name[0]),
					audio_config_p.achan[ch].octrl[ot].out_gpio_num,
					audio_config_p.achan[ch].octrl[ot].ptt_lpt_bit,
					audio_config_p.achan[ch].octrl[ot].ptt_invert)
			}
		}
	}

	var fd *os.File
	var openErr error

	/*
	 * Set up serial ports.
	 */

	for ch := C.int(0); ch < MAX_RADIO_CHANS; ch++ {

		if audio_config_p.chan_medium[ch] == MEDIUM_RADIO {

			for ot := C.int(0); ot < NUM_OCTYPES; ot++ {

				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_SERIAL {

					/* Translate Windows device name into Linux name. */
					/* COM1 -> /dev/ttyS0, etc. */

					if C.strncasecmp(&audio_config_p.achan[ch].octrl[ot].ptt_device[0], C.CString("COM"), 3) == 0 {
						var n = C.atoi(&audio_config_p.achan[ch].octrl[ot].ptt_device[3])
						text_color_set(DW_COLOR_INFO)
						dw_printf("Converted %s device '%s'", C.GoString(&audio_config_p.achan[ch].octrl[ot].ptt_device[0]), otnames[ot])
						if n < 1 {
							n = 1
						}
						C.strcpy(&audio_config_p.achan[ch].octrl[ot].ptt_device[0], C.CString(fmt.Sprintf("/dev/ttyS%d", n-1)))
						dw_printf(" to Linux equivalent '%s'\n", C.GoString(&audio_config_p.achan[ch].octrl[ot].ptt_device[0]))
					}
					/* Can't open the same device more than once so we */
					/* need more logic to look for the case of multiple radio */
					/* channels using different pins of the same COM port. */

					/* Did some earlier channel use the same device name? */

					var same_device_used = false

					for j := ch; j >= 0; j-- {
						if audio_config_p.chan_medium[j] == MEDIUM_RADIO {
							var k C.int = NUM_OCTYPES - 1
							if j == ch {
								k = ot - 1
							}
							for ; k >= 0; k-- {
								if C.strcmp(&audio_config_p.achan[ch].octrl[ot].ptt_device[0], &audio_config_p.achan[j].octrl[k].ptt_device[0]) == 0 {
									fd = ptt_fd[j][k]
									same_device_used = true
								}
							}
						}
					}

					if !same_device_used {

						/* O_NONBLOCK added in version 0.9. */
						/* Was hanging with some USB-serial adapters. */
						/* https://bugs.launchpad.net/ubuntu/+source/linux/+bug/661321/comments/12 */

						fd, openErr = os.Open(C.GoString(&audio_config_p.achan[ch].octrl[ot].ptt_device[0]))
					}

					if openErr == nil {
						ptt_fd[ch][ot] = fd
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("ERROR can't open device %s for channel %d PTT control.\n",
							C.GoString(&audio_config_p.achan[ch].octrl[ot].ptt_device[0]), ch)
						dw_printf("%s\n", openErr)
						/* Don't try using it later if device open failed. */

						audio_config_p.achan[ch].octrl[ot].ptt_method = PTT_METHOD_NONE
					}

					/*
					 * Set initial state off.
					 * ptt_set will invert output signal if appropriate.
					 */
					ptt_set(ot, ch, 0)

				} /* if serial method. */
			} /* for each output type. */
		} /* if channel valid. */
	} /* For each channel. */

	/*
	 * Set up GPIO - for Linux only.
	 */

	/*
	 * Does any of them use GPIO?
	 */

	var using_gpio = false
	for ch := 0; ch < MAX_RADIO_CHANS; ch++ {
		if save_audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			for ot := 0; ot < NUM_OCTYPES; ot++ {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_GPIO {
					using_gpio = true
				}
			}
			for ot := 0; ot < NUM_ICTYPES; ot++ {
				if audio_config_p.achan[ch].ictrl[ot].method == PTT_METHOD_GPIO {
					using_gpio = true
				}
			}
		}
	}

	if using_gpio {
		get_access_to_gpio("/sys/class/gpio/export")
	}
	// GPIOD
	for ch := C.int(0); ch < MAX_RADIO_CHANS; ch++ {
		if save_audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			for ot := C.int(0); ot < NUM_OCTYPES; ot++ {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_GPIOD {
					var chip_name = audio_config_p.achan[ch].octrl[ot].out_gpio_name
					var line_number = audio_config_p.achan[ch].octrl[ot].out_gpio_num
					var rc = gpiod_probe(C.GoString(&chip_name[0]), line_number)
					if rc < 0 {
						text_color_set(DW_COLOR_ERROR)
						//No, people won't notice the error message and be confused.  Just terminate.
						//dw_printf ("Disable PTT for channel %d\n", ch);
						//audio_config_p.achan[ch].octrl[ot].ptt_method = PTT_METHOD_NONE;
						dw_printf("Terminating due to failed PTT on channel %d\n", ch)
						os.Exit(1)
					} else {
						// Set initial state off ptt_set will invert output signal if appropriate.
						ptt_set(ot, ch, 0)
					}
				}
			}
		}
	}
	/*
	 * We should now be able to create the device nodes for
	 * the pins we want to use.
	 */

	for ch := C.int(0); ch < MAX_RADIO_CHANS; ch++ {
		if save_audio_config_p.chan_medium[ch] == MEDIUM_RADIO {

			// output control type, PTT, DCD, CON, ...
			for ot := C.int(0); ot < NUM_OCTYPES; ot++ {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_GPIO {
					export_gpio(ch, ot, audio_config_p.achan[ch].octrl[ot].ptt_invert, 1)
				}
			}
			// input control type
			for it := C.int(0); it < NUM_ICTYPES; it++ {
				if audio_config_p.achan[ch].ictrl[it].method == PTT_METHOD_GPIO {
					export_gpio(ch, it, audio_config_p.achan[ch].ictrl[it].invert, 0)
				}
			}
		}
	}

	/*
	 * Set up parallel printer port.
	 *
	 * Restrictions:
	 * 	Only the primary printer port.
	 * 	For x86 Linux only.
	 */

	for ch := C.int(0); ch < MAX_RADIO_CHANS; ch++ {
		if save_audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			for ot := C.int(0); ot < NUM_OCTYPES; ot++ {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_LPT {

					/* Can't open the same device more than once so we */
					/* need more logic to look for the case of multiple radio */
					/* channels using different pins of the LPT port. */

					/* Did some earlier channel use the same ptt device name? */

					var same_device_used = false

					for j := ch; j >= 0; j-- {
						if audio_config_p.chan_medium[j] == MEDIUM_RADIO {
							var k C.int = NUM_OCTYPES - 1
							if j == ch {
								k = ot - 1
							}
							for ; k >= 0; k-- {
								if C.strcmp(&audio_config_p.achan[ch].octrl[ot].ptt_device[0], &audio_config_p.achan[j].octrl[k].ptt_device[0]) == 0 {
									fd = ptt_fd[j][k]
									same_device_used = true
								}
							}
						}
					}

					if !same_device_used {
						fd, openErr = os.Open("/dev/port")
					}

					if openErr != nil {
						ptt_fd[ch][ot] = fd
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("ERROR - Can't open /dev/port for parallel printer port PTT control.\n")
						dw_printf("%s\n", openErr)
						dw_printf("You probably don't have adequate permissions to access I/O ports.\n")
						dw_printf("Either run direwolf as root or change these permissions:\n")
						dw_printf("  sudo chmod go+rw /dev/port\n")
						dw_printf("  sudo setcap cap_sys_rawio=ep `which direwolf`\n")

						/* Don't try using it later if device open failed. */

						audio_config_p.achan[ch].octrl[ot].ptt_method = PTT_METHOD_NONE
					}

					/*
					 * Set initial state off.
					 * ptt_set will invert output signal if appropriate.
					 */
					ptt_set(ot, ch, 0)

				} /* if parallel printer port method. */
			} /* for each output type */
		} /* if valid channel. */
	} /* For each channel. */

	for ch := 0; ch < MAX_RADIO_CHANS; ch++ {
		if save_audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			for ot := 0; ot < NUM_OCTYPES; ot++ {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_HAMLIB {
					dw_printf("Hamlib support currently disabled due to mid-stage porting complexity.\n")

					/* FIXME KG
					if ot == OCTYPE_PTT {
						var err C.int = -1
						var tries = 0

						// For "AUTO" model, try to guess what is out there.

						if audio_config_p.achan[ch].octrl[ot].ptt_model == -1 {
							var hport C.hamlib_port_t // http://hamlib.sourceforge.net/manuals/1.2.15/structhamlib__port__t.html

							// FIXME KG memset(&hport, 0, C.sizeof_hport)
							C.strcpy(&hport.pathname[0], &audio_config_p.achan[ch].octrl[ot].ptt_device[0])

							if audio_config_p.achan[ch].octrl[ot].ptt_rate > 0 {
								// Override the default serial port data rate.
								hport.parm.serial.rate = audio_config_p.achan[ch].octrl[ot].ptt_rate
								hport.parm.serial.data_bits = 8
								hport.parm.serial.stop_bits = 1
								hport.parm.serial.parity = C.RIG_PARITY_NONE
								hport.parm.serial.handshake = C.RIG_HANDSHAKE_NONE
							}

							C.rig_load_all_backends()
							audio_config_p.achan[ch].octrl[ot].ptt_model = C.int(C.rig_probe(hport))

							if audio_config_p.achan[ch].octrl[ot].ptt_model == C.RIG_MODEL_NONE {
								text_color_set(DW_COLOR_ERROR)
								dw_printf("Hamlib Error: Couldn't guess rig model number for AUTO option.  Run \"rigctl --list\" for a list of model numbers.\n")
								continue
							}

							text_color_set(DW_COLOR_INFO)
							dw_printf("Hamlib AUTO option detected rig model %d.  Run \"rigctl --list\" for a list of model numbers.\n",
								audio_config_p.achan[ch].octrl[ot].ptt_model)
						}

						rig[ch][ot] = C.rig_init(C.rig_model_t(audio_config_p.achan[ch].octrl[ot].ptt_model))
						if rig[ch][ot] == nil {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Hamlib error: Unknown rig model %d.  Run \"rigctl --list\" for a list of model numbers.\n",
								audio_config_p.achan[ch].octrl[ot].ptt_model)
							continue
						}

						C.strcpy(&rig[ch][ot].state.rigport.pathname[0], &audio_config_p.achan[ch].octrl[ot].ptt_device[0])

						// Issue 290.
						// We had a case where hamlib defaulted to 9600 baud for a particular
						// radio model but 38400 was needed.  Add an option for the configuration
						// file to override the hamlib default speed.

						text_color_set(DW_COLOR_INFO)
						if audio_config_p.achan[ch].octrl[ot].ptt_model != 2 { // 2 is network, not serial port.
							dw_printf("Hamlib determined CAT control serial port rate of %d.\n", rig[ch][ot].state.rigport.parm.serial.rate)
						}

						// Config file can optionally override the rate that hamlib came up with.

						if audio_config_p.achan[ch].octrl[ot].ptt_rate > 0 {
							dw_printf("User configuration overriding hamlib CAT control speed to %d.\n", audio_config_p.achan[ch].octrl[ot].ptt_rate)
							rig[ch][ot].state.rigport.parm.serial.rate = audio_config_p.achan[ch].octrl[ot].ptt_rate

							// Do we want to explicitly set all of these or let it default?
							rig[ch][ot].state.rigport.parm.serial.data_bits = 8
							rig[ch][ot].state.rigport.parm.serial.stop_bits = 1
							rig[ch][ot].state.rigport.parm.serial.parity = C.RIG_PARITY_NONE
							rig[ch][ot].state.rigport.parm.serial.handshake = C.RIG_HANDSHAKE_NONE
						}
						tries = 0
						for {
							// Try up to 5 times, Hamlib can take a moment to finish init
							err = C.rig_open(rig[ch][ot])
							tries++
							if tries > 5 {
								break
							} else if err != C.RIG_OK {
								text_color_set(DW_COLOR_INFO)
								dw_printf("Retrying Hamlib Rig open...\n")
								time.Sleep(5 * time.Second)
							}
							// FIXME KG while (err != C.RIG_OK);
						}
						if err != C.RIG_OK {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Hamlib Rig open error %d: %s\n", err, C.rigerror(err))
							C.rig_cleanup(rig[ch][ot])
							rig[ch][ot] = nil
							os.Exit(1)
						}

						// Successful.  Later code should check for rig[ch][ot] not nil.
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("HAMLIB can only be used for PTT.  Not DCD or other output.\n")
					}
					*/
				}
			}
		}
	}

	/*
	 * Confirm what is going on with CM108 GPIO output.
	 * Could use some error checking for overlap.
	 */

	for ch := 0; ch < MAX_RADIO_CHANS; ch++ {
		if audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			for ot := 0; ot < NUM_OCTYPES; ot++ {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == C.PTT_METHOD_CM108 {
					text_color_set(DW_COLOR_INFO)
					dw_printf("Using %s GPIO %d for channel %d %s control.\n",
						C.GoString(&audio_config_p.achan[ch].octrl[ot].ptt_device[0]),
						audio_config_p.achan[ch].octrl[ot].out_gpio_num,
						ch,
						otnames[ot])
				}
			}
		}
	}

	/* Why doesn't it transmit?  Probably forgot to specify PTT option. */

	for ch := 0; ch < MAX_RADIO_CHANS; ch++ {
		if audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			if audio_config_p.achan[ch].octrl[OCTYPE_PTT].ptt_method == PTT_METHOD_NONE {
				text_color_set(DW_COLOR_INFO)
				dw_printf("\n")
				dw_printf("Note: PTT not configured for channel %d. (OK if using VOX.)\n", ch)
				dw_printf("When using VOX, ensure that it adds very little delay (e.g. 10-20) milliseconds\n")
				dw_printf("between the time that transmit audio ends and PTT is deactivated.\n")
				dw_printf("For example, if using a SignaLink USB, turn the DLY control all the\n")
				dw_printf("way counter clockwise.\n")
				dw_printf("\n")
				dw_printf("Using VOX built in to the radio is a VERY BAD idea.  This is intended\n")
				dw_printf("for voice operation, with gaps in the sound, and typically has a delay of about a\n")
				dw_printf("half second between the time the audio stops and the transmitter is turned off.\n")
				dw_printf("When using APRS your transmitter will be sending a quiet carrier for\n")
				dw_printf("about a half second after your packet ends.  This may interfere with the\n")
				dw_printf("the next station to transmit.  This is being inconsiderate.\n")
				dw_printf("\n")
				dw_printf("If you are trying to use VOX with connected mode packet, expect\n")
				dw_printf("frustration and disappointment.  Connected mode involves rapid responses\n")
				dw_printf("which you will probably miss because your transmitter is still on when\n")
				dw_printf("the response is being transmitted.\n")
				dw_printf("\n")
				dw_printf("Read the User Guide 'Transmit Timing' section for more details.\n")
				dw_printf("\n")
			}
		}
	}

} /* end ptt_init */

/*-------------------------------------------------------------------
 *
 * Name:        ptt_set
 *
 * Purpose:    	Turn output control line on or off.
 *		Originally this was just for PTT, hence the name.
 *		Now that it is more general purpose, it should
 *		probably be renamed something like octrl_set.
 *
 * Inputs:	ot		- Output control type:
 *				   OCTYPE_PTT, OCTYPE_DCD, OCTYPE_FUTURE
 *
 *		channel		- channel, 0 .. (number of channels)-1
 *
 *		ptt_signal	- 1 for transmit, 0 for receive.
 *
 *
 * Assumption:	ptt_init was called first.
 *
 * Description:	Set the RTS or DTR line or GPIO pin.
 *		More positive output corresponds to 1 unless invert is set.
 *
 *--------------------------------------------------------------------*/

// JWL - save status and new get_ptt function.

//export ptt_set_real
func ptt_set_real(ot C.int, channel C.int, ptt_signal C.int) {

	var ptt = ptt_signal
	var ptt2 = ptt_signal

	Assert(ot >= 0 && ot < NUM_OCTYPES)
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	if ptt_debug_level >= 1 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("%s %d = %d\n", otnames[ot], channel, ptt_signal)
	}

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	if save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error, ptt_set ( %s, %d, %d ), did not expect invalid channel.\n", otnames[ot], channel, ptt)
		return
	}

	// New in 1.7.
	// A few people have a really bad audio cross talk situation where they receive their own transmissions.
	// It usually doesn't cause a problem but it is confusing to look at.
	// "half duplex" setting applied only to the transmit logic.  i.e. wait for clear channel before sending.
	// Receiving was still active.
	// I think the simplest solution is to mute/unmute the audio input at this point if not full duplex.

	// #ifndef TEST
	if ot == OCTYPE_PTT && save_audio_config_p.achan[channel].fulldup == 0 {
		demod_mute_input(channel, ptt_signal)
	}
	// #endif

	/*
	 * The data link state machine has an interest in activity on the radio channel.
	 * This is a very convenient place to get that information.
	 */

	// #ifndef TEST
	dlq_channel_busy(channel, ot, ptt_signal)
	// #endif

	/*
	 * Inverted output?
	 */

	if save_audio_config_p.achan[channel].octrl[ot].ptt_invert != 0 {
		ptt = 1 - ptt
	}
	if save_audio_config_p.achan[channel].octrl[ot].ptt_invert2 != 0 {
		ptt2 = 1 - ptt2
	}

	/*
	 * Using serial port?
	 */
	if save_audio_config_p.achan[channel].octrl[ot].ptt_method == PTT_METHOD_SERIAL &&
		ptt_fd[channel][ot] != nil {

		switch save_audio_config_p.achan[channel].octrl[ot].ptt_line {
		case C.PTT_LINE_RTS:
			if ptt != 0 {
				RTS_ON(ptt_fd[channel][ot].Fd())
			} else {
				RTS_OFF(ptt_fd[channel][ot].Fd())
			}
		case C.PTT_LINE_DTR:

			if ptt != 0 {
				DTR_ON(ptt_fd[channel][ot].Fd())
			} else {
				DTR_OFF(ptt_fd[channel][ot].Fd())
			}
		}

		/*
		 * Second serial port control line?  Typically driven with opposite phase but could be in phase.
		 */

		switch save_audio_config_p.achan[channel].octrl[ot].ptt_line2 {
		case C.PTT_LINE_RTS:
			if ptt2 != 0 {
				RTS_ON(ptt_fd[channel][ot].Fd())
			} else {
				RTS_OFF(ptt_fd[channel][ot].Fd())
			}
		case C.PTT_LINE_DTR:
			if ptt2 != 0 {
				DTR_ON(ptt_fd[channel][ot].Fd())
			} else {
				DTR_OFF(ptt_fd[channel][ot].Fd())
			}
		}
		/* else neither one */

	}

	/*
	 * Using GPIO?
	 */

	if save_audio_config_p.achan[channel].octrl[ot].ptt_method == PTT_METHOD_GPIO {

		var gpio_value_path = fmt.Sprintf("/sys/class/gpio/%s/value", C.GoString(&save_audio_config_p.achan[channel].octrl[ot].out_gpio_name[0]))

		var fd, err = os.OpenFile(gpio_value_path, os.O_WRONLY, 0)
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error opening %s to set %s signal.\n", gpio_value_path, otnames[ot])
			dw_printf("%s\n", err)
			return
		}
		defer fd.Close()

		var stemp = fmt.Sprintf("%d", ptt)

		var _, writeErr = fd.WriteString(stemp)
		if writeErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error setting GPIO %d for %s\n", save_audio_config_p.achan[channel].octrl[ot].out_gpio_num, otnames[ot])
			dw_printf("%s\n", writeErr)
		}
	}

	if save_audio_config_p.achan[channel].octrl[ot].ptt_method == PTT_METHOD_GPIOD { //nolint: staticcheck
		dw_printf("Gpiod support currently disabled due to mid-stage porting complexity.\n")

		/* FIXME KG Can't find the gpiod function?
		var chip = save_audio_config_p.achan[channel].octrl[ot].out_gpio_name
		var line = save_audio_config_p.achan[channel].octrl[ot].out_gpio_num
		var rc = C.gpiod_ctxless_set_value(chip, line, ptt, false, "direwolf", nil, nil)
		if ptt_debug_level >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("PTT_METHOD_GPIOD chip: %s line: %d ptt: %d  rc: %d\n", chip, line, ptt, rc)
		}
		*/
	}

	/*
	 * Using parallel printer port?
	 */

	if save_audio_config_p.achan[channel].octrl[ot].ptt_method == PTT_METHOD_LPT &&
		ptt_fd[channel][ot] != nil {

		ptt_fd[channel][ot].Seek(LPT_IO_ADDR, io.SeekStart)

		var lpt_data = make([]byte, 1)
		var n, readErr = ptt_fd[channel][ot].Read(lpt_data)

		if readErr != nil || n != 1 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error reading current state of LPT for channel %d %s\n", channel, otnames[ot])
			dw_printf("%s\n", readErr)
		}

		if ptt != 0 {
			lpt_data[0] |= byte(1 << save_audio_config_p.achan[channel].octrl[ot].ptt_lpt_bit)
		} else {
			lpt_data[0] &= ^byte(1 << save_audio_config_p.achan[channel].octrl[ot].ptt_lpt_bit)
		}

		ptt_fd[channel][ot].Seek(LPT_IO_ADDR, io.SeekStart)

		var _, writeErr = ptt_fd[channel][ot].Write(lpt_data)

		if writeErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error writing to LPT for channel %d %s\n", channel, otnames[ot])
			dw_printf("%s\n", writeErr)
		}
	}

	/*
	 * Using hamlib?
	 */

	if save_audio_config_p.achan[channel].octrl[ot].ptt_method == PTT_METHOD_HAMLIB {
		dw_printf("Hamlib support currently disabled due to mid-stage porting complexity.\n")

		/* FIXME KG
		if rig[channel][ot] != nil {

			var onoff = C.RIG_PTT_OFF
			if ptt {
				onoff = C.RIG_PTT_ON
			}
			var retcode = C.rig_set_ptt(rig[channel][ot], C.RIG_VFO_CURR, onoff)

			if retcode != C.RIG_OK {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Hamlib Error: rig_set_ptt command for channel %d %s\n", channel, otnames[ot])
				dw_printf("%s\n", C.rigerror(retcode))
			}
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Hamlib: Can't use rig_set_ptt for channel %d %s because rig_open failed.\n", channel, otnames[ot])
		}
		*/
	}

	/*
	 * Using CM108 USB Audio adapter GPIO?
	 */

	if save_audio_config_p.achan[channel].octrl[ot].ptt_method == C.PTT_METHOD_CM108 {

		if cm108_set_gpio_pin(&save_audio_config_p.achan[channel].octrl[ot].ptt_device[0],
			save_audio_config_p.achan[channel].octrl[ot].out_gpio_num, ptt) != 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("ERROR:  %s for channel %d has failed.  See User Guide for troubleshooting tips.\n", otnames[ot], channel)
		}
	}

} /* end ptt_set */

/*-------------------------------------------------------------------
 *
 * Name:	get_input
 *
 * Purpose:	Read the value of an input line
 *
 * Inputs:	it	- Input type (ICTYPE_TCINH supported so far)
 * 		channel	- Audio channel number
 *
 * Outputs:	0 = inactive, 1 = active, -1 = error
 *
 * ------------------------------------------------------------------*/

//export get_input_real
func get_input_real(it C.int, channel C.int) C.int {
	Assert(it >= 0 && it < NUM_ICTYPES)
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	if save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error, get_input ( %d, %d ), did not expect invalid channel.\n", it, channel)
		return -1
	}

	if save_audio_config_p.achan[channel].ictrl[it].method == PTT_METHOD_GPIO {
		var gpio_value_path = fmt.Sprintf("/sys/class/gpio/%s/value", C.GoString(&save_audio_config_p.achan[channel].ictrl[it].in_gpio_name[0]))

		get_access_to_gpio(gpio_value_path)

		var fd, openErr = os.Open(gpio_value_path)
		if openErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error opening %s to check input.\n", gpio_value_path)
			dw_printf("%s\n", openErr)
			return -1
		}

		var vtemp = make([]byte, 1)

		var _, readErr = fd.Read(vtemp)

		if readErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error getting GPIO %d value\n", save_audio_config_p.achan[channel].ictrl[it].in_gpio_num)
			dw_printf("%s\n", readErr)
		}
		fd.Close()

		var v, _ = strconv.Atoi(string(vtemp))
		if C.int(v) != save_audio_config_p.achan[channel].ictrl[it].invert {
			return 1
		} else {
			return 0
		}
	}

	return -1 /* Method was none, or something went wrong */
}

/*-------------------------------------------------------------------
 *
 * Name:        ptt_term
 *
 * Purpose:    	Make sure PTT and others are turned off when we exit.
 *
 * Inputs:	none
 *
 * Description:
 *
 *--------------------------------------------------------------------*/

func ptt_term() {

	for n := 0; n < MAX_RADIO_CHANS; n++ {
		if save_audio_config_p.chan_medium[n] == MEDIUM_RADIO {
			for ot := 0; ot < NUM_OCTYPES; ot++ {
				ptt_set(C.int(ot), C.int(n), 0)
			}
		}
	}

	for n := 0; n < MAX_RADIO_CHANS; n++ {
		if save_audio_config_p.chan_medium[n] == MEDIUM_RADIO {
			for ot := 0; ot < NUM_OCTYPES; ot++ {
				if ptt_fd[n][ot] != nil {
					ptt_fd[n][ot].Close()
					ptt_fd[n][ot] = nil
				}
			}
		}
	}

	for n := 0; n < MAX_RADIO_CHANS; n++ {
		if save_audio_config_p.chan_medium[n] == MEDIUM_RADIO {
			for ot := 0; ot < NUM_OCTYPES; ot++ {
				if rig[n][ot] != nil {

					C.rig_close(rig[n][ot])
					C.rig_cleanup(rig[n][ot])
					rig[n][ot] = nil
				}
			}
		}
	}
}

/*
 * Quick stand-alone test for above.
 *
 *     gcc -DTEST -o ptest ptt.c textcolor.o misc.a ; ./ptest
 *
 * TODO:  Retest this, add CM108 GPIO to test.
 */

func PTTTestMain() {

	var my_audio_config C.struct_audio_s

	my_audio_config.adev[0].num_channels = 2

	my_audio_config.chan_medium[0] = MEDIUM_RADIO
	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	// TODO: device should be command line argument.
	C.strcpy(&my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_device[0], C.CString("COM3"))
	//strlcpy (my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_device, "/dev/ttyUSB0", sizeof(my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_device));
	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_line = C.PTT_LINE_RTS

	my_audio_config.chan_medium[1] = MEDIUM_RADIO
	my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	C.strcpy(&my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_device[0], C.CString("COM3"))
	//strlcpy (my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_device, "/dev/ttyUSB0", sizeof(my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_device));
	my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_line = C.PTT_LINE_DTR

	/* initialize - both off */

	ptt_init(&my_audio_config)

	SLEEP_SEC(2)

	/* flash each a few times. */
	var channel C.int

	dw_printf("turn on RTS a few times...\n")

	channel = 0
	for n := 0; n < 3; n++ {
		ptt_set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		ptt_set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	dw_printf("turn on DTR a few times...\n")

	channel = 1
	for n := 0; n < 3; n++ {
		ptt_set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		ptt_set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	ptt_term()

	/* Same thing again but invert RTS. */

	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_invert = 1

	ptt_init(&my_audio_config)

	SLEEP_SEC(2)

	dw_printf("INVERTED -  RTS a few times...\n")

	channel = 0
	for n := 0; n < 3; n++ {
		ptt_set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		ptt_set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	dw_printf("turn on DTR a few times...\n")

	channel = 1
	for n := 0; n < 3; n++ {
		ptt_set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		ptt_set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	ptt_term()

	/* Test GPIO */

	// #if __arm__

	C.memset(unsafe.Pointer(&my_audio_config), 0, C.sizeof_struct_audio_s)
	my_audio_config.adev[0].num_channels = 1
	my_audio_config.chan_medium[0] = MEDIUM_RADIO
	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	my_audio_config.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25

	dw_printf("Try GPIO %d a few times...\n", my_audio_config.achan[0].octrl[OCTYPE_PTT].out_gpio_num)

	ptt_init(&my_audio_config)

	SLEEP_SEC(2)
	channel = 0
	for n := 0; n < 3; n++ {
		ptt_set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		ptt_set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	ptt_term()
	// #endif

	/* Parallel printer port. */

	/*
	   #if  ( defined(__i386__) || defined(__x86_64__) ) && ( defined(__linux__) || defined(__unix__) )

	   	// TODO

	   #if 0

	   	memset (&my_audio_config, 0, sizeof(my_audio_config));
	   	my_audio_config.num_channels = 2;
	   	my_audio_config.chan_medium[0] = MEDIUM_RADIO;
	   	my_audio_config.adev[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_LPT;
	   	my_audio_config.adev[0].octrl[OCTYPE_PTT].ptt_lpt_bit = 0;
	   	my_audio_config.chan_medium[1] = MEDIUM_RADIO;
	   	my_audio_config.adev[1].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_LPT;
	   	my_audio_config.adev[1].octrl[OCTYPE_PTT].ptt_lpt_bit = 1;

	   	dw_printf ("Try LPT bits 0 & 1 a few times...\n");

	   	ptt_init (&my_audio_config);

	   	for (n=0; n<8; n++) {
	   	  ptt_set (OCTYPE_PTT, 0, n & 1);
	   	  ptt_set (OCTYPE_PTT, 1, (n>>1) & 1);
	   	  SLEEP_SEC(1);
	   	}

	   	ptt_term ();

	   #endif

	   #endif
	*/
}

/* end ptt.c */
