// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package ptt

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
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/cm108"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus"
	goHamlib "github.com/xylo04/goHamlib"
	"golang.org/x/sys/unix"
)

// setSerialControlLine sets or clears one of a serial port's control lines.
func setSerialControlLine(fd int, bit int, on bool) {
	var stuff, _ = unix.IoctlGetInt(fd, unix.TIOCMGET)
	if on {
		stuff |= bit
	} else {
		stuff &= ^bit
	}

	unix.IoctlSetInt(fd, unix.TIOCMSET, stuff)
}

const LPT_IO_ADDR = 0x378

// octypeName is the name of an output control type, for messages.
func octypeName(ot OutputType) string {
	switch ot {
	case OutputPTT:
		return "PTT"
	case OutputDCD:
		return "DCD"
	case OutputCon:
		return "CON"
	default:
		return fmt.Sprintf("octype %d", ot)
	}
}

// defaultGPIOSysfsDir is the root of the sysfs GPIO user interface.
const defaultGPIOSysfsDir = "/sys/class/gpio"

// defaultLPTPortPath is the device giving access to I/O ports, through which
// the parallel printer port is reached.
const defaultLPTPortPath = "/dev/port"

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
	// channels is each radio channel's configuration: PTT's own copy, as it
	// fills in a GPIO line's node name and turns off an output it could not
	// open.
	channels [phy.MaxRadioChans]ChannelConfig

	debugLevel int

	// muteInput silences a channel's received audio, or stops silencing it,
	// so a half duplex channel doesn't hear its own transmissions; nil for
	// nothing to silence.
	muteInput func(channel int, mute bool)

	// channelBusy tells the data link state machine of each change to a
	// channel's outputs - PTT, DCD or connected indicator - and is left to
	// pick out the ones it cares about; nil for nobody to tell.
	channelBusy func(channel int, activity OutputType, busy bool)

	// gpioSysfsDir is a field rather than always defaultGPIOSysfsDir so that
	// a test can point it at a fake tree and exercise the GPIO paths without
	// a kernel that offers the real one.
	gpioSysfsDir string

	// lptPortPath is a field rather than always defaultLPTPortPath for the
	// same reason: a test can stand an ordinary file in for the I/O ports.
	lptPortPath string

	// serialControl sets or clears one of a serial port's control lines.  It
	// is a field rather than always setSerialControlLine so that a test can
	// see which line was driven and at what level: neither a pseudo terminal
	// nor a plain file has modem control lines, and the ioctls simply fail on
	// them, which is why their result is ignored.
	serialControl func(fd int, bit int, on bool)

	// mu guards the handles below.  Set reads them from the transmit and
	// receive threads while Term, run by the shutdown path, closes and
	// clears them.  Set takes it for reading, so channels can still key at
	// the same time, and Term for writing.
	mu sync.RWMutex

	/* Serial port handle or fd.  */
	/* Could be the same for two channels */
	/* if using both RTS and DTR. */
	fd [phy.MaxRadioChans][NumOutputTypes]*os.File

	rig [phy.MaxRadioChans][NumOutputTypes]*goHamlib.Rig

	/* GPIOD line handles, one per channel/output-type combination. */
	gpiodLine [phy.MaxRadioChans][NumOutputTypes]gpiodOutputLine
}

/*-------------------------------------------------------------------
 *
 * Name:        New
 *
 * Purpose:    	Open serial port(s) used for PTT signals and set to proper state.
 *
 * Inputs:	channels		- Each radio channel's configuration.
 *
 *		    for each channel we have:
 *
 *			Method		Method for PTT signal.
 *					MethodNone - not configured.  Could be using VOX.
 *					MethodSerial - serial (com) port.
 *					MethodGPIO - general purpose I/O (sysfs).
 *					MethodGPIOD - general purpose I/O (libgpiod).
 *					MethodLPT - Parallel printer port.
 *                  			MethodHamlib - HAMLib rig control.
 *					MethodCM108 - GPIO pins of CM108 etc. USB Audio.
 *
 *			Device		Name of serial port device.
 *					 e.g. COM1 or /dev/ttyS0.
 *					 HAMLIB can also use hostaddr:port.
 *					 Like /dev/hidraw1 for CM108.
 *
 *			Line		RTS or DTR when using serial port.
 *
 *			GPIONum		GPIO number.  Only used for Linux.
 *					 Valid only when Method is MethodGPIO.
 *
 *			LPTBit		Bit number for parallel printer port.
 *					 Bit 0 = pin 2, ..., bit 7 = pin 9.
 *					 Valid only when Method is MethodLPT.
 *
 *			Invert		Invert the signal.
 *					 Normally higher voltage means transmit or LED on.
 *
 *			Model		Only for HAMLIB.
 *					2 to communicate with rigctld.
 *					>= 3 for specific radio model.
 *					-1 guess at what is out there.  (AUTO option in config file.)
 *
 *		muteInput		- Silences a half duplex channel's received
 *					  audio while it transmits; nil for nothing
 *					  to silence.
 *
 *		channelBusy		- Told of each change to a channel's
 *					  outputs (PTT, DCD or connected
 *					  indicator), for the data link state
 *					  machine to pick from; nil for nobody to
 *					  tell.
 *
 *		debug			- "-d o" level: 1 reports each change of an
 *					  output, 2 also the configuration and GPIO detail.
 *
 * Outputs:	A PTT that remembers what it needs for future use, or an error
 *		if the hardware could not be set up, in which case whatever had
 *		been set up before the failure has been released again.
 *
 * Description:
 *
 *--------------------------------------------------------------------*/

func New(channels [phy.MaxRadioChans]ChannelConfig, muteInput func(channel int, mute bool), channelBusy func(channel int, activity OutputType, busy bool), debug int) (*PTT, error) {
	return newPTT(channels, muteInput, channelBusy, debug, defaultGPIOSysfsDir)
}

// newPTT is New with the root of the sysfs GPIO interface given, so that a
// test can supply a fake one.
func newPTT(
	channels [phy.MaxRadioChans]ChannelConfig,
	muteInput func(channel int, mute bool),
	channelBusy func(channel int, activity OutputType, busy bool),
	debug int,
	gpioSysfsDir string,
) (*PTT, error) {
	var p = new(PTT)
	p.channels = channels
	p.muteInput = muteInput
	p.channelBusy = channelBusy
	p.debugLevel = debug
	p.gpioSysfsDir = gpioSysfsDir
	p.lptPortPath = defaultLPTPortPath
	p.serialControl = setSerialControlLine

	var err = p.init()
	if err != nil {
		return nil, err
	}

	return p, nil
}

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
 *				   OutputPTT, OutputDCD, OutputCon
 *
 *		channel		- channel, 0 .. (number of channels)-1
 *
 *		ptt_signal	- 1 for transmit, 0 for receive.
 *
 *
 * Assumption:	The PTT came from New.
 *
 * Description:	Set the RTS or DTR line or GPIO pin.
 *		More positive output corresponds to 1 unless invert is set.
 *
 *--------------------------------------------------------------------*/

// JWL - save status and new get_ptt function.

func (p *PTT) Set(ot OutputType, channel int, ptt_signal int) {
	if p == nil {
		return
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	p.set(ot, channel, ptt_signal)
}

// SetPTT keys a channel's transmitter, or lets it go.
func (p *PTT) SetPTT(channel int, on bool) {
	p.Set(OutputPTT, channel, dwutil.IfThenElse(on, 1, 0))
}

// SetConnected turns a channel's connected indicator on or off.
func (p *PTT) SetConnected(channel int, on bool) {
	p.Set(OutputCon, channel, dwutil.IfThenElse(on, 1, 0))
}

// SetDCD turns a channel's data carrier detect indicator on or off.
func (p *PTT) SetDCD(channel int, on bool) {
	p.Set(OutputDCD, channel, dwutil.IfThenElse(on, 1, 0))
}

// set is Set for a caller that already holds p.mu, for reading or writing.
func (p *PTT) set(ot OutputType, channel int, ptt_signal int) {
	var ptt = ptt_signal
	var ptt2 = ptt_signal

	dwutil.Assert(ot >= 0 && ot < NumOutputTypes)
	dwutil.Assert(channel >= 0 && channel < phy.MaxTotalChans)

	if channel >= phy.MaxRadioChans {
		return // NCHANNEL: no physical PTT hardware to drive
	}

	if p.debugLevel >= 1 {
		logrus.WithFields(logrus.Fields{
			"output":  octypeName(ot),
			"channel": channel,
			"state":   ptt_signal,
		}).Debug("Set output")
	}

	dwutil.Assert(channel >= 0 && channel < phy.MaxTotalChans)

	if !p.channels[channel].Radio {
		logrus.WithFields(logrus.Fields{
			"output":  octypeName(ot),
			"channel": channel,
			"state":   ptt,
		}).Error("Internal error, PTT.Set did not expect a channel that isn't a radio")

		return
	}

	// New in 1.7.
	// A few people have a really bad audio cross talk situation where they receive their own transmissions.
	// It usually doesn't cause a problem but it is confusing to look at.
	// "half duplex" setting applied only to the transmit logic.  i.e. wait for clear channel before sending.
	// Receiving was still active.
	// I think the simplest solution is to mute/unmute the audio input at this point if not full duplex.

	// #ifndef TEST
	if ot == OutputPTT && !p.channels[channel].FullDuplex && p.muteInput != nil {
		p.muteInput(channel, ptt_signal != 0)
	}
	// #endif

	/*
	 * The data link state machine has an interest in activity on the radio channel.
	 * This is a very convenient place to get that information.
	 */

	if p.channelBusy != nil {
		p.channelBusy(channel, ot, ptt_signal != 0)
	}

	/*
	 * Inverted output?
	 */

	if p.channels[channel].Outputs[ot].Invert {
		ptt = 1 - ptt
	}

	if p.channels[channel].Outputs[ot].Invert2 {
		ptt2 = 1 - ptt2
	}

	/*
	 * Using serial port?
	 */
	if p.channels[channel].Outputs[ot].Method == MethodSerial &&
		p.fd[channel][ot] != nil {
		switch p.channels[channel].Outputs[ot].Line {
		case LineRTS:
			p.serialControl(int(p.fd[channel][ot].Fd()), unix.TIOCM_RTS, ptt != 0)
		case LineDTR:
			p.serialControl(int(p.fd[channel][ot].Fd()), unix.TIOCM_DTR, ptt != 0)
		case LineNone:
		}

		/*
		 * Second serial port control line?  Typically driven with opposite phase but could be in phase.
		 */

		switch p.channels[channel].Outputs[ot].Line2 {
		case LineRTS:
			p.serialControl(int(p.fd[channel][ot].Fd()), unix.TIOCM_RTS, ptt2 != 0)
		case LineDTR:
			p.serialControl(int(p.fd[channel][ot].Fd()), unix.TIOCM_DTR, ptt2 != 0)
		case LineNone:
		}
		/* else neither one */
	}

	/*
	 * Using GPIO?
	 */

	if p.channels[channel].Outputs[ot].Method == MethodGPIO {
		var gpio_value_path = fmt.Sprintf("%s/%s/value", p.gpioSysfsDir, p.channels[channel].Outputs[ot].GPIOName)

		var fd, err = os.OpenFile(gpio_value_path, os.O_WRONLY, 0) //nolint:gosec
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"path":   gpio_value_path,
				"output": octypeName(ot),
			}).Error("Error opening GPIO to set signal")

			return
		}
		defer fd.Close()

		var stemp = strconv.Itoa(ptt)

		var _, writeErr = fd.WriteString(stemp)
		if writeErr != nil {
			logrus.WithError(writeErr).WithFields(logrus.Fields{
				"gpio":   p.channels[channel].Outputs[ot].GPIONum,
				"output": octypeName(ot),
			}).Error("Error setting GPIO")
		}
	}

	if p.channels[channel].Outputs[ot].Method == MethodGPIOD {
		if p.gpiodLine[channel][ot] != nil {
			var err = p.gpiodLine[channel][ot].SetValue(ptt)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"channel": channel,
					"output":  octypeName(ot),
				}).Error("Error setting GPIOD")
			} else if p.debugLevel >= 1 {
				logrus.WithFields(logrus.Fields{
					"chip":  p.channels[channel].Outputs[ot].GPIOName,
					"line":  p.channels[channel].Outputs[ot].GPIONum,
					"state": ptt,
				}).Debug("Set GPIOD line")
			}
		}
	}

	/*
	 * Using parallel printer port?
	 */

	if p.channels[channel].Outputs[ot].Method == MethodLPT &&
		p.fd[channel][ot] != nil {
		p.fd[channel][ot].Seek(LPT_IO_ADDR, io.SeekStart)

		var lpt_data = make([]byte, 1)
		var n, readErr = p.fd[channel][ot].Read(lpt_data)

		if readErr != nil || n != 1 {
			logrus.WithError(readErr).WithFields(logrus.Fields{
				"channel": channel,
				"output":  octypeName(ot),
			}).Error("Error reading current state of LPT")
		}

		if ptt != 0 {
			lpt_data[0] |= byte(1 << p.channels[channel].Outputs[ot].LPTBit)
		} else {
			lpt_data[0] &= ^byte(1 << p.channels[channel].Outputs[ot].LPTBit)
		}

		p.fd[channel][ot].Seek(LPT_IO_ADDR, io.SeekStart)

		var _, writeErr = p.fd[channel][ot].Write(lpt_data)
		if writeErr != nil {
			logrus.WithError(writeErr).WithFields(logrus.Fields{
				"channel": channel,
				"output":  octypeName(ot),
			}).Error("Error writing to LPT")
		}
	}

	/*
	 * Using hamlib?
	 */

	if p.channels[channel].Outputs[ot].Method == MethodHamlib {
		if p.rig[channel][ot] != nil {
			var onoff = goHamlib.RIG_PTT_OFF
			if ptt != 0 {
				onoff = goHamlib.RIG_PTT_ON
			}

			var retcode = p.rig[channel][ot].SetPtt(goHamlib.VFOCurrent, onoff)
			if retcode != nil {
				logrus.WithError(retcode).WithFields(logrus.Fields{
					"channel": channel,
					"output":  octypeName(ot),
				}).Error("Hamlib error: SetPtt command failed")
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"output":  octypeName(ot),
			}).Error("Hamlib: Can't use SetPtt because rig open failed")
		}
	}

	/*
	 * Using CM108 USB Audio adapter GPIO?
	 */

	if p.channels[channel].Outputs[ot].Method == MethodCM108 {
		var err = cm108.SetGPIOPin(p.channels[channel].Outputs[ot].Device,
			p.channels[channel].Outputs[ot].GPIONum, ptt)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"output":  octypeName(ot),
				"channel": channel,
				"hint":    "See User Guide for troubleshooting tips.",
			}).Error("CM108 output failed")
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

	for _, line := range cm108.PermissionAdvice(name) {
		logrus.WithField("device", name).Error(line)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:	GetInput
 *
 * Purpose:	Read the value of an input line
 *
 * Inputs:	it	- Input type (InputTxInhibit supported so far)
 * 		channel	- Audio channel number
 *
 * Outputs:	0 = inactive, 1 = active, -1 = error
 *
 * ------------------------------------------------------------------*/

func (p *PTT) GetInput(it int, channel int) int {
	dwutil.Assert(it >= 0 && it < NumInputTypes)
	dwutil.Assert(channel >= 0 && channel < phy.MaxRadioChans)

	if p == nil {
		return -1 /* Not set up, so no method. */
	}

	if !p.channels[channel].Radio {
		logrus.WithFields(logrus.Fields{
			"input":   it,
			"channel": channel,
		}).Error("Internal error, PTT.GetInput did not expect a channel that isn't a radio")

		return -1
	}

	if p.channels[channel].Inputs[it].Method == MethodGPIO {
		var gpio_value_path = fmt.Sprintf("%s/%s/value", p.gpioSysfsDir, p.channels[channel].Inputs[it].GPIOName)

		// No need to check access first: this runs on every transmit attempt,
		// export_gpio checked it at startup, and the open below reports the
		// same thing once rather than twice.
		var fd, openErr = os.Open(gpio_value_path) //nolint:gosec
		if openErr != nil {
			logrus.WithError(openErr).WithField("path", gpio_value_path).Error("Error opening GPIO to check input")

			return -1
		}

		var vtemp = make([]byte, 1)

		var _, readErr = fd.Read(vtemp)
		if readErr != nil {
			logrus.WithError(readErr).WithField("gpio", p.channels[channel].Inputs[it].GPIONum).Error("Error getting GPIO value")
		}

		fd.Close()

		var v, parseErr = strconv.Atoi(string(vtemp))
		if parseErr != nil {
			logrus.WithError(parseErr).WithFields(logrus.Fields{
				"value": string(vtemp),
				"gpio":  p.channels[channel].Inputs[it].GPIONum,
			}).Error("Error parsing value read from GPIO")

			return -1
		}

		if !p.channels[channel].Inputs[it].Invert {
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

// TransmitInhibited says whether a channel's transmit inhibit input is set.
// With no PTT, or no input configured, it never is.
func (p *PTT) TransmitInhibited(channel int) bool {
	return p.GetInput(InputTxInhibit, channel) == 1
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

	p.mu.Lock()
	defer p.mu.Unlock()

	for n := range phy.MaxRadioChans {
		if p.channels[n].Radio {
			for ot := range OutputTypes() {
				p.set(ot, n, 0)
			}
		}
	}

	for n := range phy.MaxRadioChans {
		if p.channels[n].Radio {
			for ot := range OutputTypes() {
				if p.fd[n][ot] != nil {
					p.fd[n][ot].Close()
					p.fd[n][ot] = nil
				}
			}
		}
	}

	for n := range phy.MaxRadioChans {
		if p.channels[n].Radio {
			for ot := range OutputTypes() {
				if p.gpiodLine[n][ot] != nil {
					p.gpiodLine[n][ot].Close()
					p.gpiodLine[n][ot] = nil
				}
			}
		}
	}

	for n := range phy.MaxRadioChans {
		if p.channels[n].Radio {
			for ot := range OutputTypes() {
				if p.rig[n][ot] != nil {
					p.rig[n][ot].Close()   //nolint:errcheck
					p.rig[n][ot].Cleanup() //nolint:errcheck
					p.rig[n][ot] = nil
				}
			}
		}
	}
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
	for ch := range phy.MaxRadioChans {
		for ot := range OutputTypes() {
			if p.debugLevel >= 2 {
				logrus.WithFields(logrus.Fields{
					"channel": ch,
					"output":  octypeName(ot),
					"method":  p.channels[ch].Outputs[ot].Method,
					"device":  p.channels[ch].Outputs[ot].Device,
					"line":    p.channels[ch].Outputs[ot].Line,
					"name":    p.channels[ch].Outputs[ot].GPIOName,
					"gpio":    p.channels[ch].Outputs[ot].GPIONum,
					"lpt_bit": p.channels[ch].Outputs[ot].LPTBit,
					"invert":  p.channels[ch].Outputs[ot].Invert,
				}).Debug("Output control configuration")
			}
		}
	}

	var fd *os.File
	var openErr error

	/*
	 * Set up serial ports.
	 */

	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			for ot := range OutputTypes() {
				if p.channels[ch].Outputs[ot].Method == MethodSerial {
					/* Translate Windows device name into Linux name. */
					/* COM1 -> /dev/ttyS0, etc. */
					if strings.HasPrefix(strings.ToUpper(p.channels[ch].Outputs[ot].Device), "COM") {
						var n, _ = strconv.Atoi(p.channels[ch].Outputs[ot].Device[3:])

						var windowsName = p.channels[ch].Outputs[ot].Device

						if n < 1 {
							n = 1
						}

						p.channels[ch].Outputs[ot].Device = fmt.Sprintf("/dev/ttyS%d", n-1)

						logrus.WithFields(logrus.Fields{
							"output": octypeName(ot),
							"from":   windowsName,
							"to":     p.channels[ch].Outputs[ot].Device,
						}).Info("Converted device to Linux equivalent")
					}
					/* Can't open the same device more than once so we */
					/* need more logic to look for the case of multiple radio */
					/* channels using different pins of the same COM port. */

					/* Did some earlier channel use the same device name? */

					var same_device_used = false

					for j := ch; j >= 0; j-- {
						if p.channels[j].Radio {
							var k OutputType = NumOutputTypes - 1
							if j == ch {
								k = ot - 1
							}

							for ; k >= 0; k-- {
								if p.channels[ch].Outputs[ot].Device == p.channels[j].Outputs[k].Device {
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
						fd, openErr = os.Open(p.channels[ch].Outputs[ot].Device)
					}

					if openErr == nil {
						p.fd[ch][ot] = fd
					} else {
						logrus.WithError(openErr).WithFields(logrus.Fields{
							"device":  p.channels[ch].Outputs[ot].Device,
							"channel": ch,
						}).Error("Can't open device for PTT control")
						/* Don't try using it later if device open failed. */

						p.channels[ch].Outputs[ot].Method = MethodNone
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

	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			for ot := range OutputTypes() {
				if p.channels[ch].Outputs[ot].Method == MethodGPIO {
					using_gpio = true
				}
			}

			for ot := range NumInputTypes {
				if p.channels[ch].Inputs[ot].Method == MethodGPIO {
					using_gpio = true
				}
			}
		}
	}

	if using_gpio {
		var accessErr = p.getAccessToGPIO(p.gpioSysfsDir + "/export")
		if accessErr != nil {
			return accessErr
		}
	}
	// GPIOD
	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			for ot := range OutputTypes() {
				if p.channels[ch].Outputs[ot].Method == MethodGPIOD {
					var chip_name = p.channels[ch].Outputs[ot].GPIOName
					var line_number = p.channels[ch].Outputs[ot].GPIONum
					var initialState = dwutil.IfThenElse(p.channels[ch].Outputs[ot].Invert, 1, 0) // Using "invert" as initial state means we always start "off"

					var line, lineErr = requestGPIODLine(chip_name, line_number, initialState)
					if lineErr != nil {
						return fmt.Errorf("can't request GPIOD line %d on %s for channel %d %s: %w",
							line_number, chip_name, ch, octypeName(ot), lineErr)
					}

					p.gpiodLine[ch][ot] = line

					if p.debugLevel >= 2 {
						logrus.WithFields(logrus.Fields{
							"chip": chip_name,
							"line": line_number,
						}).Debug("GPIOD init OK")
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

	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			// output control type, PTT, DCD, CON, ...
			for ot := range OutputTypes() {
				if p.channels[ch].Outputs[ot].Method == MethodGPIO {
					var exportErr = p.exportGPIO(ch, int(ot), p.channels[ch].Outputs[ot].Invert, 1)
					if exportErr != nil {
						return fmt.Errorf("channel %d %s: %w", ch, octypeName(ot), exportErr)
					}
				}
			}
			// input control type
			for it := range NumInputTypes {
				if p.channels[ch].Inputs[it].Method == MethodGPIO {
					var exportErr = p.exportGPIO(ch, it, p.channels[ch].Inputs[it].Invert, 0)
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

	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			for ot := range OutputTypes() {
				if p.channels[ch].Outputs[ot].Method == MethodLPT {
					/* Can't open the same device more than once so we */
					/* need more logic to look for the case of multiple radio */
					/* channels using different pins of the LPT port. */

					/* Did some earlier channel use the same ptt device name? */
					var same_device_used = false

					for j := ch; j >= 0; j-- {
						if p.channels[j].Radio {
							var k OutputType = NumOutputTypes - 1
							if j == ch {
								k = ot - 1
							}

							for ; k >= 0; k-- {
								if p.channels[ch].Outputs[ot].Device == p.channels[j].Outputs[k].Device {
									fd = p.fd[j][k]
									same_device_used = true
								}
							}
						}
					}

					if !same_device_used {
						fd, openErr = os.OpenFile(p.lptPortPath, os.O_RDWR, 0)
					}

					if openErr == nil {
						p.fd[ch][ot] = fd
					} else {
						logrus.WithError(openErr).WithFields(logrus.Fields{
							"device": p.lptPortPath,
							"hint": "You probably don't have adequate permissions to access I/O ports. " +
								"Either run direwolf as root or change these permissions: " +
								"sudo chmod go+rw /dev/port; sudo setcap cap_sys_rawio=ep `which direwolf`",
						}).Error("Can't open parallel printer port for PTT control")

						/* Don't try using it later if device open failed. */

						p.channels[ch].Outputs[ot].Method = MethodNone
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

	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			for ot := range OutputTypes() {
				if p.channels[ch].Outputs[ot].Method == MethodHamlib {
					if ot == OutputPTT {
						if p.channels[ch].Outputs[ot].Model == -1 {
							logrus.WithField("hint", `Run "rigctl --list" for a list of model numbers.`).
								Error("Hamlib error: AUTO rig model detection is not supported. Specify the model number explicitly.")

							continue
						}

						var r = &goHamlib.Rig{} //nolint:exhaustruct_v5

						var initErr = r.Init(goHamlib.RigModelID(p.channels[ch].Outputs[ot].Model))
						if initErr != nil {
							logrus.WithError(initErr).WithFields(logrus.Fields{
								"model": p.channels[ch].Outputs[ot].Model,
								"hint":  `Run "rigctl --list" for a list of model numbers.`,
							}).Error("Hamlib error: Unknown rig model")

							continue
						}

						var port = goHamlib.Port{ //nolint:exhaustruct_v5
							Portname:  p.channels[ch].Outputs[ot].Device,
							Databits:  8,
							Stopbits:  1,
							Parity:    goHamlib.ParityNone,
							Handshake: goHamlib.HandshakeNone,
						}

						// Issue 290.
						// We had a case where hamlib defaulted to 9600 baud for a particular
						// radio model but 38400 was needed.  Add an option for the configuration
						// file to override the hamlib default speed.

						if p.channels[ch].Outputs[ot].Model == 2 {
							// Model 2 is rigctld network control, not a serial port.
							port.RigPortType = goHamlib.RigPortNetwork
						} else {
							port.RigPortType = goHamlib.RigPortSerial

							if p.channels[ch].Outputs[ot].Rate > 0 {
								logrus.WithField("rate", p.channels[ch].Outputs[ot].Rate).
									Info("User configuration overriding hamlib CAT control speed")
								port.Baudrate = p.channels[ch].Outputs[ot].Rate
							}
						}

						var portErr = r.SetPort(port)
						if portErr != nil {
							logrus.WithError(portErr).WithField("channel", ch).Error("Hamlib error setting port")
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

							logrus.WithError(openErr).Info("Retrying Hamlib rig open")
							time.Sleep(5 * time.Second)
						}

						if openErr != nil {
							r.Cleanup() //nolint:errcheck

							return fmt.Errorf("hamlib rig open for channel %d: %w", ch, openErr)
						}

						// Successful.  Later code should check for p.rig[ch][ot] not nil.
						p.rig[ch][ot] = r
					} else {
						logrus.WithFields(logrus.Fields{
							"channel": ch,
							"output":  octypeName(ot),
						}).Error("HAMLIB can only be used for PTT, not DCD or other output")
					}
				}
			}
		}
	}

	/*
	 * Confirm what is going on with CM108 GPIO output.
	 * Could use some error checking for overlap.
	 */

	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			for ot := range OutputTypes() {
				if p.channels[ch].Outputs[ot].Method == MethodCM108 {
					var device = p.channels[ch].Outputs[ot].Device

					logrus.WithFields(logrus.Fields{
						"device":  device,
						"gpio":    p.channels[ch].Outputs[ot].GPIONum,
						"channel": ch,
						"output":  octypeName(ot),
					}).Info("Using CM108 GPIO for output control")

					if device == "" {
						logrus.WithFields(logrus.Fields{
							"channel": ch,
							"output":  octypeName(ot),
						}).Warn("No CM108 HID found. Specify one in the config file.")

						continue
					}

					// Check it now rather than discovering the hard way at the
					// first transmission.  An unfamiliar device may still work.
					var checkErr = cm108.CheckDevice(device)
					if checkErr != nil {
						logrus.WithError(checkErr).WithField("device", device).Warn("Unfamiliar CM108 device. Proceed at your own risk.")
						cm108_print_permission_advice(device, checkErr)
					}
				}
			}
		}
	}

	/* Why doesn't it transmit?  Probably forgot to specify PTT option. */

	for ch := range phy.MaxRadioChans {
		if p.channels[ch].Radio {
			if p.channels[ch].Outputs[OutputPTT].Method == MethodNone {
				logrus.WithFields(logrus.Fields{
					"channel": ch,
					"hint":    voxAdvice,
				}).Info("PTT not configured (OK if using VOX)")
			}
		}
	}

	return nil
} /* end setup */

// voxAdvice is what to bear in mind when a channel has no PTT configured, as
// happens when it relies on VOX instead.
const voxAdvice = "When using VOX, ensure that it adds very little delay (e.g. 10-20 milliseconds) " +
	"between the time that transmit audio ends and PTT is deactivated. " +
	"For example, if using a SignaLink USB, turn the DLY control all the way counter clockwise. " +
	"Using VOX built in to the radio is a VERY BAD idea.  This is intended " +
	"for voice operation, with gaps in the sound, and typically has a delay of about a " +
	"half second between the time the audio stops and the transmitter is turned off. " +
	"When using APRS your transmitter will be sending a quiet carrier for " +
	"about a half second after your packet ends.  This may interfere with the " +
	"the next station to transmit.  This is being inconsiderate. " +
	"If you are trying to use VOX with connected mode packet, expect " +
	"frustration and disappointment.  Connected mode involves rapid responses " +
	"which you will probably miss because your transmitter is still on when " +
	"the response is being transmitted. " +
	"Read the User Guide 'Transmit Timing' section for more details."

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
		logrus.WithError(groupsErr).Error("Getgroups() failed to get supplementary groups")
	}

	if p.debugLevel >= 2 {
		// TODO KG dw_printf("%s: uid=%d, gid=%d, mode=o%o\n", path, finfo.st_uid, finfo.st_gid, finfo.st_mode)
		logrus.WithFields(logrus.Fields{
			"uid":    my_uid,
			"gid":    my_gid,
			"groups": my_groups,
		}).Debug("My user and groups")
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
 * Outputs:	GPIOName	- in the output or input configuration
 *			  that PTT keeps.
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
		gpio_num = p.channels[ch].Outputs[ot].GPIONum
		gpio_name = p.channels[ch].Outputs[ot].GPIOName
	} else {
		gpio_num = p.channels[ch].Inputs[ot].GPIONum
		gpio_name = p.channels[ch].Inputs[ot].GPIOName
	}

	var gpio_export_path = p.gpioSysfsDir + "/export"

	var accessErr = p.getAccessToGPIO(gpio_export_path)
	if accessErr != nil {
		return accessErr
	}

	var fd, err = os.OpenFile(gpio_export_path, os.O_WRONLY, 0) //nolint:gosec
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
	time.Sleep(250 * time.Millisecond)
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

	if p.debugLevel >= 2 {
		logrus.WithField("path", p.gpioSysfsDir).Debug("Listing GPIO directory")
	}

	var dirEntries, readDirErr = os.ReadDir(p.gpioSysfsDir)

	var ok = false

	if readDirErr != nil {
		// Something went wrong.  Fill in the simple expected name and keep going.
		logrus.WithError(readDirErr).WithField("path", p.gpioSysfsDir).Error("Could not get GPIO directory listing")

		gpio_name = fmt.Sprintf("gpio%d", gpio_num)
		ok = true
	} else {
		if p.debugLevel >= 2 {
			for _, entry := range dirEntries {
				logrus.WithField("name", entry.Name()).Debug("GPIO directory entry")
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
		p.channels[ch].Outputs[ot].GPIOName = gpio_name
	} else {
		p.channels[ch].Inputs[ot].GPIOName = gpio_name
	}

	if p.debugLevel >= 2 {
		logrus.WithFields(logrus.Fields{
			"gpio": gpio_num,
			"path": p.gpioSysfsDir + "/" + gpio_name,
		}).Debug("Found path for GPIO")
	}

	/*
	 * Set output direction and initial state
	 */

	var gpio_direction_path = fmt.Sprintf("%s/%s/direction", p.gpioSysfsDir, gpio_name)

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

	var gpio_value_path = fmt.Sprintf("%s/%s/value", p.gpioSysfsDir, gpio_name)

	return p.getAccessToGPIO(gpio_value_path)
}

/* end ptt.c */
