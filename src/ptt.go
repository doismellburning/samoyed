//nolint:gochecknoglobals,funcorder // funcorder: kept in Dire Wolf's order for now, to keep the diff that gathers the globals readable.
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

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	goHamlib "github.com/xylo04/goHamlib"
	"golang.org/x/sys/unix"
)

func _TIOCM_real(fd int, value int, on bool) {
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

var ptt_debug_level = 0

func ptt_set_debug(debug int) {
	ptt_debug_level = debug
}

// octypeName is the name of an output control type, for messages.
func octypeName(ot int) string {
	switch ot {
	case OCTYPE_PTT:
		return "PTT"
	case OCTYPE_DCD:
		return "DCD"
	case OCTYPE_CON:
		return "CON"
	default:
		return fmt.Sprintf("octype %d", ot)
	}
}

// gpiodOutputLine is the subset of gpiocdev.Line used for PTT output control.
// The interface exists to allow dependency injection in tests.
type gpiodOutputLine interface {
	SetValue(value int) error
	Close() error
}

// PTT drives the output control lines - PTT, DCD and the connected indicator -
// and reads the input lines, for every radio channel.
//
// Its methods are safe to call on a nil *PTT, which does nothing: DCD and the
// connected indicator reach for it whether or not startup has got that far.
type PTT struct {
	audioConfig *audio_s

	/* Serial port handle or fd.  */
	/* Could be the same for two channels */
	/* if using both RTS and DTR. */
	fd [MAX_RADIO_CHANS][NUM_OCTYPES]*os.File

	rig [MAX_RADIO_CHANS][NUM_OCTYPES]*goHamlib.Rig

	/* GPIOD line handles, one per channel/output-type combination. */
	gpiodLine [MAX_RADIO_CHANS][NUM_OCTYPES]gpiodOutputLine
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

// gpio_sysfs_dir is the root of the sysfs GPIO user interface.  It is a
// variable rather than a constant so that a test can point it at a fake tree
// and exercise the GPIO paths without a kernel that offers the real one.
var gpio_sysfs_dir = "/sys/class/gpio"

func (p *PTT) getAccessToGPIO(path string) error {
	/*
	 * Does path even exist?
	 */
	var _, err = os.Stat(path)
	if err != nil {
		return fmt.Errorf("can't get properties of %s: %w"+
			" (this system is not configured with the GPIO user interface;"+
			" use a different method for PTT control)", path, err)
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

	return nil
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

func (p *PTT) exportGPIO(ch int, ot int, invert bool, direction int) error {
	// Raspberry Pi was easy.  GPIO 24 has the name gpio24.
	// Others, such as the Cubieboard, take a little more effort.
	// The name might be gpio24_ph11 meaning connector H, pin 11.
	// When we "export" GPIO number, we will store the corresponding
	// device name for future use when we want to access it.
	var gpio_num int
	var gpio_name string

	if direction > 0 {
		gpio_num = p.audioConfig.achan[ch].octrl[ot].out_gpio_num
		gpio_name = p.audioConfig.achan[ch].octrl[ot].out_gpio_name
	} else {
		gpio_num = p.audioConfig.achan[ch].ictrl[ot].in_gpio_num
		gpio_name = p.audioConfig.achan[ch].ictrl[ot].in_gpio_name
	}

	var gpio_export_path = gpio_sysfs_dir + "/export"

	var accessErr = p.getAccessToGPIO(gpio_export_path)
	if accessErr != nil {
		return accessErr
	}

	var fd, err = os.OpenFile(gpio_export_path, os.O_WRONLY, 0)
	if err != nil {
		// Not expected.  Above should have obtained permission.
		return fmt.Errorf("permissions do not allow access to GPIO: %w", err)
	}

	var stemp = strconv.Itoa(gpio_num)

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
			os.Exit(1)
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
		dw_printf("Contents of %s:\n", gpio_sysfs_dir)
	}

	var dirEntries, readDirErr = os.ReadDir(gpio_sysfs_dir)

	var ok = false

	if readDirErr != nil {
		// Something went wrong.  Fill in the simple expected name and keep going.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR! Could not get directory listing for %s\n", gpio_sysfs_dir)

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
	if !ok {
		return fmt.Errorf("could not find path for gpio number %d", gpio_num)
	}

	// Remember it: everything that drives or reads the line afterwards builds
	// its path from the node name, not from the number.
	if direction > 0 {
		p.audioConfig.achan[ch].octrl[ot].out_gpio_name = gpio_name
	} else {
		p.audioConfig.achan[ch].ictrl[ot].in_gpio_name = gpio_name
	}

	if ptt_debug_level >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("Path for gpio number %d is %s/%s\n", gpio_num, gpio_sysfs_dir, gpio_name)
	}

	/*
	 * Set output direction and initial state
	 */

	var gpio_direction_path = fmt.Sprintf("%s/%s/direction", gpio_sysfs_dir, gpio_name)

	accessErr = p.getAccessToGPIO(gpio_direction_path)
	if accessErr != nil {
		return accessErr
	}

	fd, err = os.OpenFile(gpio_direction_path, os.O_WRONLY, 0) //nolint:gosec
	if err != nil {
		return fmt.Errorf("error opening %s: %w", gpio_direction_path, err)
	}

	var gpio_val string

	if direction != 0 {
		if invert {
			gpio_val = "high"
		} else {
			gpio_val = "low"
		}
	} else {
		gpio_val = "in"
	}

	n, writeErr = fd.WriteString(gpio_val)
	if writeErr != nil {
		fd.Close()

		return fmt.Errorf("error writing initial state to %s: %w", gpio_direction_path, writeErr)
	}

	if n != len(gpio_val) {
		fd.Close()

		return fmt.Errorf("error writing initial state to %s: wrote %d of %d bytes", gpio_direction_path, n, len(gpio_val))
	}

	fd.Close()

	/*
	 * Make sure that we have access to 'value'.
	 * Do it once here, rather than each time we want to use it.
	 */

	var gpio_value_path = fmt.Sprintf("%s/%s/value", gpio_sysfs_dir, gpio_name)

	return p.getAccessToGPIO(gpio_value_path)
}

/*-------------------------------------------------------------------
 *
 * Name:        NewPTT
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
 * Outputs:	A PTT that remembers what it needs for future use, or an error
 *		if the hardware could not be set up, in which case whatever had
 *		been set up before the failure has been released again.
 *
 * Description:
 *
 *--------------------------------------------------------------------*/

func NewPTT(audio_config_p *audio_s) (*PTT, error) {
	var p = new(PTT)
	p.audioConfig = audio_config_p

	var err = p.init()
	if err != nil {
		return nil, err
	}

	return p, nil
}

// init sets up the hardware, and on a failure releases whatever it had set up.
func (p *PTT) init() error {
	var err = p.setup()
	if err != nil {
		// Setting up is incremental, so a failure can come after serial ports
		// have been opened or GPIOD lines requested, and the caller never gets
		// the PTT that holds them.  Put them back before reporting, so a
		// caller that carries on, or tries again, is not left with open
		// descriptors and hardware nobody tracks.
		p.Term()

		return err
	}

	return nil
}

// setup does the work of init, stopping at the first failure.  Call init
// rather than this: it is the one that tidies up after a failure.
func (p *PTT) setup() error {
	var audio_config_p = p.audioConfig

	for ch := range MAX_RADIO_CHANS {
		for ot := range NUM_OCTYPES {
			if ptt_debug_level >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("ch=%d, %s method=%d, device=%s, line=%d, name=%s, gpio=%d, lpt_bit=%d, invert=%t\n",
					ch,
					octypeName(ot),
					audio_config_p.achan[ch].octrl[ot].ptt_method,
					audio_config_p.achan[ch].octrl[ot].ptt_device,
					audio_config_p.achan[ch].octrl[ot].ptt_line,
					audio_config_p.achan[ch].octrl[ot].out_gpio_name,
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

	for ch := range MAX_RADIO_CHANS {
		if audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_SERIAL {
					/* Translate Windows device name into Linux name. */
					/* COM1 -> /dev/ttyS0, etc. */
					if strings.HasPrefix(strings.ToUpper(audio_config_p.achan[ch].octrl[ot].ptt_device), "COM") {
						var n, _ = strconv.Atoi(audio_config_p.achan[ch].octrl[ot].ptt_device[3:])

						text_color_set(DW_COLOR_INFO)
						dw_printf("Converted %s device '%s'", audio_config_p.achan[ch].octrl[ot].ptt_device, octypeName(ot))

						if n < 1 {
							n = 1
						}

						audio_config_p.achan[ch].octrl[ot].ptt_device = fmt.Sprintf("/dev/ttyS%d", n-1)
						dw_printf(" to Linux equivalent '%s'\n", audio_config_p.achan[ch].octrl[ot].ptt_device)
					}
					/* Can't open the same device more than once so we */
					/* need more logic to look for the case of multiple radio */
					/* channels using different pins of the same COM port. */

					/* Did some earlier channel use the same device name? */

					var same_device_used = false

					for j := ch; j >= 0; j-- {
						if audio_config_p.chan_medium[j] == MEDIUM_RADIO {
							var k = NUM_OCTYPES - 1
							if j == ch {
								k = ot - 1
							}

							for ; k >= 0; k-- {
								if audio_config_p.achan[ch].octrl[ot].ptt_device == audio_config_p.achan[j].octrl[k].ptt_device {
									fd = p.fd[j][k]
									same_device_used = true
								}
							}
						}
					}

					if !same_device_used {
						/* O_NONBLOCK added in version 0.9. */
						/* Was hanging with some USB-serial adapters. */
						/* https://bugs.launchpad.net/ubuntu/+source/linux/+bug/661321/comments/12 */
						fd, openErr = os.Open(audio_config_p.achan[ch].octrl[ot].ptt_device)
					}

					if openErr == nil {
						p.fd[ch][ot] = fd
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("ERROR can't open device %s for channel %d PTT control.\n",
							audio_config_p.achan[ch].octrl[ot].ptt_device, ch)
						dw_printf("%s\n", openErr)
						/* Don't try using it later if device open failed. */

						audio_config_p.achan[ch].octrl[ot].ptt_method = PTT_METHOD_NONE
					}

					/*
					 * Set initial state off.
					 * Set will invert output signal if appropriate.
					 */
					p.Set(ot, ch, 0)
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

	for ch := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[ch] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_GPIO {
					using_gpio = true
				}
			}

			for ot := range NUM_ICTYPES {
				if audio_config_p.achan[ch].ictrl[ot].method == PTT_METHOD_GPIO {
					using_gpio = true
				}
			}
		}
	}

	if using_gpio {
		var accessErr = p.getAccessToGPIO(gpio_sysfs_dir + "/export")
		if accessErr != nil {
			return accessErr
		}
	}
	// GPIOD
	for ch := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[ch] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_GPIOD {
					var chip_name = audio_config_p.achan[ch].octrl[ot].out_gpio_name
					var line_number = audio_config_p.achan[ch].octrl[ot].out_gpio_num
					var initialState = IfThenElse(audio_config_p.achan[ch].octrl[ot].ptt_invert, 1, 0) // Using "invert" as initial state means we always start "off"

					var line, lineErr = RequestGPIODLine(chip_name, line_number, initialState)
					if lineErr != nil {
						return fmt.Errorf("can't request GPIOD line %d on %s for channel %d %s: %w",
							line_number, chip_name, ch, octypeName(ot), lineErr)
					}

					p.gpiodLine[ch][ot] = line

					if ptt_debug_level >= 2 {
						text_color_set(DW_COLOR_DEBUG)
						dw_printf("GPIOD init OK. Chip: %s line: %d\n", chip_name, line_number)
					}
					// Set initial state off.  Set will invert output signal if appropriate.
					p.Set(ot, ch, 0)
				}
			}
		}
	}
	/*
	 * We should now be able to create the device nodes for
	 * the pins we want to use.
	 */

	for ch := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[ch] == MEDIUM_RADIO {
			// output control type, PTT, DCD, CON, ...
			for ot := range NUM_OCTYPES {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_GPIO {
					var exportErr = p.exportGPIO(ch, ot, audio_config_p.achan[ch].octrl[ot].ptt_invert, 1)
					if exportErr != nil {
						return fmt.Errorf("channel %d %s: %w", ch, octypeName(ot), exportErr)
					}
				}
			}
			// input control type
			for it := range NUM_ICTYPES {
				if audio_config_p.achan[ch].ictrl[it].method == PTT_METHOD_GPIO {
					var exportErr = p.exportGPIO(ch, it, audio_config_p.achan[ch].ictrl[it].invert, 0)
					if exportErr != nil {
						return fmt.Errorf("channel %d input %d: %w", ch, it, exportErr)
					}
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

	for ch := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[ch] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_LPT {
					/* Can't open the same device more than once so we */
					/* need more logic to look for the case of multiple radio */
					/* channels using different pins of the LPT port. */

					/* Did some earlier channel use the same ptt device name? */
					var same_device_used = false

					for j := ch; j >= 0; j-- {
						if audio_config_p.chan_medium[j] == MEDIUM_RADIO {
							var k = NUM_OCTYPES - 1
							if j == ch {
								k = ot - 1
							}

							for ; k >= 0; k-- {
								if audio_config_p.achan[ch].octrl[ot].ptt_device == audio_config_p.achan[j].octrl[k].ptt_device {
									fd = p.fd[j][k]
									same_device_used = true
								}
							}
						}
					}

					if !same_device_used {
						fd, openErr = os.Open("/dev/port")
					}

					if openErr != nil {
						p.fd[ch][ot] = fd
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
					 * Set will invert output signal if appropriate.
					 */
					p.Set(ot, ch, 0)
				} /* if parallel printer port method. */
			} /* for each output type */
		} /* if valid channel. */
	} /* For each channel. */

	for ch := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[ch] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_HAMLIB {
					if ot == OCTYPE_PTT {
						if audio_config_p.achan[ch].octrl[ot].ptt_model == -1 {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Hamlib error: AUTO rig model detection is not supported. Specify the model number explicitly.\n")
							dw_printf("Run \"rigctl --list\" for a list of model numbers.\n")

							continue
						}

						var r = &goHamlib.Rig{} //nolint:exhaustruct_v5

						var initErr = r.Init(goHamlib.RigModelID(audio_config_p.achan[ch].octrl[ot].ptt_model))
						if initErr != nil {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Hamlib error: Unknown rig model %d. %s\n",
								audio_config_p.achan[ch].octrl[ot].ptt_model, initErr)
							dw_printf("Run \"rigctl --list\" for a list of model numbers.\n")

							continue
						}

						var port = goHamlib.Port{ //nolint:exhaustruct_v5
							Portname:  audio_config_p.achan[ch].octrl[ot].ptt_device,
							Databits:  8,
							Stopbits:  1,
							Parity:    goHamlib.ParityNone,
							Handshake: goHamlib.HandshakeNone,
						}

						// Issue 290.
						// We had a case where hamlib defaulted to 9600 baud for a particular
						// radio model but 38400 was needed.  Add an option for the configuration
						// file to override the hamlib default speed.

						if audio_config_p.achan[ch].octrl[ot].ptt_model == 2 {
							// Model 2 is rigctld network control, not a serial port.
							port.RigPortType = goHamlib.RigPortNetwork
						} else {
							port.RigPortType = goHamlib.RigPortSerial

							if audio_config_p.achan[ch].octrl[ot].ptt_rate > 0 {
								text_color_set(DW_COLOR_INFO)
								dw_printf("User configuration overriding hamlib CAT control speed to %d.\n",
									audio_config_p.achan[ch].octrl[ot].ptt_rate)
								port.Baudrate = audio_config_p.achan[ch].octrl[ot].ptt_rate
							}
						}

						var portErr = r.SetPort(port)
						if portErr != nil {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Hamlib error setting port for channel %d: %s\n", ch, portErr)
							r.Cleanup() //nolint:errcheck

							continue
						}

						var openErr error
						var tries = 0

						for {
							// Retry up to 5 times, Hamlib can take a moment to finish init
							openErr = r.Open()

							tries++
							if openErr == nil || tries > 5 {
								break
							}

							text_color_set(DW_COLOR_INFO)
							dw_printf("Retrying Hamlib Rig open...\n")
							time.Sleep(5 * time.Second)
						}

						if openErr != nil {
							r.Cleanup() //nolint:errcheck

							return fmt.Errorf("hamlib rig open for channel %d: %w", ch, openErr)
						}

						// Successful.  Later code should check for p.rig[ch][ot] not nil.
						p.rig[ch][ot] = r
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("HAMLIB can only be used for PTT.  Not DCD or other output.\n")
					}
				}
			}
		}
	}

	/*
	 * Confirm what is going on with CM108 GPIO output.
	 * Could use some error checking for overlap.
	 */

	for ch := range MAX_RADIO_CHANS {
		if audio_config_p.chan_medium[ch] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if audio_config_p.achan[ch].octrl[ot].ptt_method == PTT_METHOD_CM108 {
					var device = audio_config_p.achan[ch].octrl[ot].ptt_device

					text_color_set(DW_COLOR_INFO)
					dw_printf("Using %s GPIO %d for channel %d %s control.\n",
						device,
						audio_config_p.achan[ch].octrl[ot].out_gpio_num,
						ch,
						octypeName(ot))

					if device == "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Warning: No CM108 HID found for channel %d %s.  Specify one in the config file.\n", ch, octypeName(ot))

						continue
					}

					// Check it now rather than discovering the hard way at the
					// first transmission.  An unfamiliar device may still work.
					var checkErr = CM108CheckDevice(device)
					if checkErr != nil {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Warning: %v.  Proceed at your own risk.\n", checkErr)
						cm108_print_permission_advice(device, checkErr)
					}
				}
			}
		}
	}

	/* Why doesn't it transmit?  Probably forgot to specify PTT option. */

	for ch := range MAX_RADIO_CHANS {
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

	return nil
} /* end setup */

/*-------------------------------------------------------------------
 *
 * Name:        Set
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
 * Assumption:	The PTT came from NewPTT.
 *
 * Description:	Set the RTS or DTR line or GPIO pin.
 *		More positive output corresponds to 1 unless invert is set.
 *
 *--------------------------------------------------------------------*/

// JWL - save status and new get_ptt function.

func (p *PTT) Set(ot int, channel int, ptt_signal int) {
	if p == nil {
		return
	}

	var ptt = ptt_signal
	var ptt2 = ptt_signal

	Assert(ot >= 0 && ot < NUM_OCTYPES)
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	if channel >= MAX_RADIO_CHANS {
		return // NCHANNEL: no physical PTT hardware to drive
	}

	if ptt_debug_level >= 1 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("%s %d = %d\n", octypeName(ot), channel, ptt_signal)
	}

	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	if p.audioConfig.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error, PTT.Set ( %s, %d, %d ), did not expect invalid channel.\n", octypeName(ot), channel, ptt)

		return
	}

	// New in 1.7.
	// A few people have a really bad audio cross talk situation where they receive their own transmissions.
	// It usually doesn't cause a problem but it is confusing to look at.
	// "half duplex" setting applied only to the transmit logic.  i.e. wait for clear channel before sending.
	// Receiving was still active.
	// I think the simplest solution is to mute/unmute the audio input at this point if not full duplex.

	// #ifndef TEST
	if ot == OCTYPE_PTT && !p.audioConfig.achan[channel].fulldup {
		demod_mute_input(channel, ptt_signal)
	}
	// #endif

	/*
	 * The data link state machine has an interest in activity on the radio channel.
	 * This is a very convenient place to get that information.
	 */

	// #ifndef TEST
	dataLinkQueue.ChannelBusy(channel, ot, ptt_signal)
	// #endif

	/*
	 * Inverted output?
	 */

	if p.audioConfig.achan[channel].octrl[ot].ptt_invert {
		ptt = 1 - ptt
	}

	if p.audioConfig.achan[channel].octrl[ot].ptt_invert2 {
		ptt2 = 1 - ptt2
	}

	/*
	 * Using serial port?
	 */
	if p.audioConfig.achan[channel].octrl[ot].ptt_method == PTT_METHOD_SERIAL &&
		p.fd[channel][ot] != nil {
		switch p.audioConfig.achan[channel].octrl[ot].ptt_line {
		case PTT_LINE_RTS:
			if ptt != 0 {
				RTS_ON(p.fd[channel][ot].Fd())
			} else {
				RTS_OFF(p.fd[channel][ot].Fd())
			}
		case PTT_LINE_DTR:
			if ptt != 0 {
				DTR_ON(p.fd[channel][ot].Fd())
			} else {
				DTR_OFF(p.fd[channel][ot].Fd())
			}
		case PTT_LINE_NONE:
		}

		/*
		 * Second serial port control line?  Typically driven with opposite phase but could be in phase.
		 */

		switch p.audioConfig.achan[channel].octrl[ot].ptt_line2 {
		case PTT_LINE_RTS:
			if ptt2 != 0 {
				RTS_ON(p.fd[channel][ot].Fd())
			} else {
				RTS_OFF(p.fd[channel][ot].Fd())
			}
		case PTT_LINE_DTR:
			if ptt2 != 0 {
				DTR_ON(p.fd[channel][ot].Fd())
			} else {
				DTR_OFF(p.fd[channel][ot].Fd())
			}
		case PTT_LINE_NONE:
		}
		/* else neither one */
	}

	/*
	 * Using GPIO?
	 */

	if p.audioConfig.achan[channel].octrl[ot].ptt_method == PTT_METHOD_GPIO {
		var gpio_value_path = fmt.Sprintf("%s/%s/value", gpio_sysfs_dir, p.audioConfig.achan[channel].octrl[ot].out_gpio_name)

		var fd, err = os.OpenFile(gpio_value_path, os.O_WRONLY, 0) //nolint:gosec
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error opening %s to set %s signal.\n", gpio_value_path, octypeName(ot))
			dw_printf("%s\n", err)

			return
		}
		defer fd.Close()

		var stemp = strconv.Itoa(ptt)

		var _, writeErr = fd.WriteString(stemp)
		if writeErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error setting GPIO %d for %s\n", p.audioConfig.achan[channel].octrl[ot].out_gpio_num, octypeName(ot))
			dw_printf("%s\n", writeErr)
		}
	}

	if p.audioConfig.achan[channel].octrl[ot].ptt_method == PTT_METHOD_GPIOD {
		if p.gpiodLine[channel][ot] != nil {
			var err = p.gpiodLine[channel][ot].SetValue(ptt)
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Error setting GPIOD for channel %d %s: %v\n", channel, octypeName(ot), err)
			} else if ptt_debug_level >= 1 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("PTT_METHOD_GPIOD chip: %s line: %d ptt: %d\n",
					p.audioConfig.achan[channel].octrl[ot].out_gpio_name,
					p.audioConfig.achan[channel].octrl[ot].out_gpio_num, ptt)
			}
		}
	}

	/*
	 * Using parallel printer port?
	 */

	if p.audioConfig.achan[channel].octrl[ot].ptt_method == PTT_METHOD_LPT &&
		p.fd[channel][ot] != nil {
		p.fd[channel][ot].Seek(LPT_IO_ADDR, io.SeekStart)

		var lpt_data = make([]byte, 1)
		var n, readErr = p.fd[channel][ot].Read(lpt_data)

		if readErr != nil || n != 1 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error reading current state of LPT for channel %d %s\n", channel, octypeName(ot))
			dw_printf("%s\n", readErr)
		}

		if ptt != 0 {
			lpt_data[0] |= byte(1 << p.audioConfig.achan[channel].octrl[ot].ptt_lpt_bit)
		} else {
			lpt_data[0] &= ^byte(1 << p.audioConfig.achan[channel].octrl[ot].ptt_lpt_bit)
		}

		p.fd[channel][ot].Seek(LPT_IO_ADDR, io.SeekStart)

		var _, writeErr = p.fd[channel][ot].Write(lpt_data)
		if writeErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error writing to LPT for channel %d %s\n", channel, octypeName(ot))
			dw_printf("%s\n", writeErr)
		}
	}

	/*
	 * Using hamlib?
	 */

	if p.audioConfig.achan[channel].octrl[ot].ptt_method == PTT_METHOD_HAMLIB {
		if p.rig[channel][ot] != nil {
			var onoff = goHamlib.RIG_PTT_OFF
			if ptt != 0 {
				onoff = goHamlib.RIG_PTT_ON
			}

			var retcode = p.rig[channel][ot].SetPtt(goHamlib.VFOCurrent, onoff)
			if retcode != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Hamlib error: SetPtt command for channel %d %s\n", channel, octypeName(ot))
				dw_printf("%s\n", retcode)
			}
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Hamlib: Can't use SetPtt for channel %d %s because rig open failed.\n", channel, octypeName(ot))
		}
	}

	/*
	 * Using CM108 USB Audio adapter GPIO?
	 */

	if p.audioConfig.achan[channel].octrl[ot].ptt_method == PTT_METHOD_CM108 {
		var err = CM108SetGPIOPin(p.audioConfig.achan[channel].octrl[ot].ptt_device,
			p.audioConfig.achan[channel].octrl[ot].out_gpio_num, ptt)
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("ERROR:  %s for channel %d has failed: %v\n", octypeName(ot), channel, err)
			dw_printf("See User Guide for troubleshooting tips.\n")
		}
	}
} /* end Set */

/*-------------------------------------------------------------------
 *
 * Name:	cm108_print_permission_advice
 *
 * Purpose:	Explain how to fix the permissions on a CM108 HID, for the
 *		errors where that is the likely cause.
 *
 * Inputs:	name	- Device name, e.g. /dev/hidraw2.
 *
 *		err	- Error returned by one of the CM108 functions.
 *			  Nothing is printed unless it is a permission problem.
 *
 *------------------------------------------------------------------*/

func cm108_print_permission_advice(name string, err error) {
	if !errors.Is(err, fs.ErrPermission) {
		return
	}

	text_color_set(DW_COLOR_ERROR)

	for _, line := range CM108PermissionAdvice(name) {
		dw_printf("%s\n", line)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:	GetInput
 *
 * Purpose:	Read the value of an input line
 *
 * Inputs:	it	- Input type (ICTYPE_TCINH supported so far)
 * 		channel	- Audio channel number
 *
 * Outputs:	0 = inactive, 1 = active, -1 = error
 *
 * ------------------------------------------------------------------*/

func (p *PTT) GetInput(it int, channel int) int {
	Assert(it >= 0 && it < NUM_ICTYPES)
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	if p == nil {
		return -1 /* Not set up, so no method. */
	}

	if p.audioConfig.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error, PTT.GetInput ( %d, %d ), did not expect invalid channel.\n", it, channel)

		return -1
	}

	if p.audioConfig.achan[channel].ictrl[it].method == PTT_METHOD_GPIO {
		var gpio_value_path = fmt.Sprintf("%s/%s/value", gpio_sysfs_dir, p.audioConfig.achan[channel].ictrl[it].in_gpio_name)

		// No need to check access first: this runs on every transmit attempt,
		// export_gpio checked it at startup, and the open below reports the
		// same thing once rather than twice.
		var fd, openErr = os.Open(gpio_value_path) //nolint:gosec
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
			dw_printf("Error getting GPIO %d value\n", p.audioConfig.achan[channel].ictrl[it].in_gpio_num)
			dw_printf("%s\n", readErr)
		}

		fd.Close()

		var v, parseErr = strconv.Atoi(string(vtemp))
		if parseErr != nil {
			dw_printf("Error parsing return value (%s) from GPIO %d: %s\n", vtemp, p.audioConfig.achan[channel].ictrl[it].in_gpio_num, parseErr)

			return -1
		}

		if !p.audioConfig.achan[channel].ictrl[it].invert {
			if v == 0 {
				return 0
			} else {
				return 1
			}
		} else {
			if v == 0 {
				return 1
			} else {
				return 0
			}
		}
	}

	return -1 /* Method was none, or something went wrong */
}

/*-------------------------------------------------------------------
 *
 * Name:        Term
 *
 * Purpose:    	Make sure PTT and others are turned off when we exit.
 *
 * Inputs:	none
 *
 * Description:
 *
 *--------------------------------------------------------------------*/

func (p *PTT) Term() {
	// A stop signal can arrive while we are still starting up, and the
	// shutdown path runs this on its way out.  Nothing has been keyed if the
	// PTT has not been made yet, so there is nothing to release.
	if p == nil {
		return
	}

	for n := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[n] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				p.Set(ot, n, 0)
			}
		}
	}

	for n := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[n] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if p.fd[n][ot] != nil {
					p.fd[n][ot].Close()
					p.fd[n][ot] = nil
				}
			}
		}
	}

	for n := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[n] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if p.gpiodLine[n][ot] != nil {
					p.gpiodLine[n][ot].Close()
					p.gpiodLine[n][ot] = nil
				}
			}
		}
	}

	for n := range MAX_RADIO_CHANS {
		if p.audioConfig.chan_medium[n] == MEDIUM_RADIO {
			for ot := range NUM_OCTYPES {
				if p.rig[n][ot] != nil {
					p.rig[n][ot].Close()   //nolint:errcheck
					p.rig[n][ot].Cleanup() //nolint:errcheck
					p.rig[n][ot] = nil
				}
			}
		}
	}
}

// ptt_set, get_input and ptt_term are what the rest of the package calls, and
// hand on to pttControl.

func ptt_set(ot int, channel int, ptt_signal int) {
	pttControl.Set(ot, channel, ptt_signal)
}

func get_input(it int, channel int) int {
	return pttControl.GetInput(it, channel)
}

func ptt_term() {
	pttControl.Term()
}

/*
 * Quick stand-alone test for above.
 *
 *     gcc -DTEST -o ptest ptt.c textcolor.o misc.a ; ./ptest
 *
 * TODO:  Retest this, add CM108 GPIO to test.
 */

func PTTTestMain() error {
	var my_audio_config audio_s

	my_audio_config.adev[0].num_channels = 2

	my_audio_config.chan_medium[0] = MEDIUM_RADIO
	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	// TODO: device should be command line argument.
	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_device = "COM3"
	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_RTS

	my_audio_config.chan_medium[1] = MEDIUM_RADIO
	my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_device = "COM3"
	my_audio_config.achan[1].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_DTR

	/* initialize - both off */

	var p, initErr = NewPTT(&my_audio_config)
	if initErr != nil {
		return initErr
	}

	SLEEP_SEC(2)

	/* flash each a few times. */
	var channel int

	dw_printf("turn on RTS a few times...\n")

	channel = 0
	for range 3 {
		p.Set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		p.Set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	dw_printf("turn on DTR a few times...\n")

	channel = 1
	for range 3 {
		p.Set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		p.Set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	p.Term()

	/* Same thing again but invert RTS. */

	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_invert = true

	p, initErr = NewPTT(&my_audio_config)
	if initErr != nil {
		return initErr
	}

	SLEEP_SEC(2)

	dw_printf("INVERTED -  RTS a few times...\n")

	channel = 0
	for range 3 {
		p.Set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		p.Set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	dw_printf("turn on DTR a few times...\n")

	channel = 1
	for range 3 {
		p.Set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		p.Set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	p.Term()

	/* Test GPIO */

	// #if __arm__

	my_audio_config = audio_s{} //nolint:exhaustruct_v5
	my_audio_config.adev[0].num_channels = 1
	my_audio_config.chan_medium[0] = MEDIUM_RADIO
	my_audio_config.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	my_audio_config.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25

	dw_printf("Try GPIO %d a few times...\n", my_audio_config.achan[0].octrl[OCTYPE_PTT].out_gpio_num)

	p, initErr = NewPTT(&my_audio_config)
	if initErr != nil {
		return initErr
	}

	SLEEP_SEC(2)

	channel = 0
	for range 3 {
		p.Set(OCTYPE_PTT, channel, 1)
		SLEEP_SEC(1)
		p.Set(OCTYPE_PTT, channel, 0)
		SLEEP_SEC(1)
	}

	p.Term()
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

	return nil
}

/* end ptt.c */
