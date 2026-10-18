// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package ptt

import "iter"

// Method is how an output or input line is driven or read.
type Method int

const (
	MethodNone   Method = iota /* VOX or no transmit. */
	MethodSerial               /* Serial port RTS or DTR. */
	MethodGPIO                 /* General purpose I/O using sysfs, deprecated after 2020, Linux only. */
	MethodGPIOD                /* General purpose I/O, using libgpiod, Linux only. */
	MethodLPT                  /* Parallel printer port, Linux only. */
	MethodHamlib               /* HAMLib, Linux only. */
	MethodCM108                /* GPIO pin of CM108/CM119/etc.  Linux only. */
)

// Line is a serial port's control line.
type Line int

const (
	LineNone Line = iota //  Important: 0 for neither.
	LineRTS
	LineDTR
)

// OutputType is one of the outputs the PTT controller drives for a channel.
type OutputType int

const (
	OutputPTT OutputType = 0
	OutputDCD OutputType = 1
	OutputCon OutputType = 2 // Connected to another station.
)

const NumOutputTypes = 3 /* number of values above.   i.e. last value +1. */

// OutputTypes yields each output type in turn, from OutputPTT to OutputCon.
func OutputTypes() iter.Seq[OutputType] {
	return func(yield func(OutputType) bool) {
		for ot := range OutputType(NumOutputTypes) {
			if !yield(ot) {
				return
			}
		}
	}
}

/* Each channel can also have associated input lines. */
/* So far, we just have one for transmit inhibit. */

const InputTxInhibit = 0

const NumInputTypes = 1 /* number of values above. i.e. last value +1. */

// OutputConfig is how one of a radio channel's output control lines - PTT,
// DCD or the connected indicator - is driven.
type OutputConfig struct {
	Method Method /* none, serial port, GPIO, LPT, HAMLIB, CM108. */

	Device string /* Serial device name for PTT.  e.g. COM1 or /dev/ttyS0 */
	/* Also used for HAMLIB.  Could be host:port when model is 1. */
	/* For years, 20 characters was plenty then we start getting extreme names like this: */
	/* /dev/serial/by-id/usb-FTDI_Navigator__CAT___2nd_PTT__00000000-if00-port0 */
	/* /dev/serial/by-id/usb-Prolific_Technology_Inc._USB-Serial_Controller_D-if00-port0 */
	/* Issue 104, changed to 100 bytes in version 1.5. */

	/* This same field is also used for CM108/CM119 GPIO PTT which will */
	/* have a name like /dev/hidraw1 for Linux or */
	/* \\?\hid#vid_0d8c&pid_0008&mi_03#8&39d3555&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030} */
	/* for Windows.  Largest observed was 95 but add some extra to be safe. */

	Line  Line /* Control line when using serial port. LineRTS, LineDTR. */
	Line2 Line /* Optional second one:  LineNone when not used. */

	GPIONum int /* GPIO number.  Originally this was only for PTT. */
	/* It is now more general. */
	/* Outputs array is indexed by PTT, DCD, or CONnected indicator. */
	/* For CM108/CM119, this should be in range of 1-8. */

	GPIOName string
	/* originally, gpio number NN was assumed to simply */
	/* have the name gpioNN but this turned out not to be */
	/* the case for CubieBoard where it was longer. */
	/* The PTT fills this in, in its own copy, so we don't have to */
	/* recalculate it each time we access it. */
	/* Also GPIO chip name for GPIOD method. Looks like 'gpiochip4' */

	/* This could probably be collapsed into Device instead of being separate. */

	LPTBit int /* Bit number for parallel printer port.  */
	/* Bit 0 = pin 2, ..., bit 7 = pin 9. */

	Invert  bool /* Invert the output. */
	Invert2 bool /* Invert the secondary output. */

	//#ifdef USE_HAMLIB

	Model int /* HAMLIB model.  -1 for AUTO.  2 for rigctld.  Others are radio model. */
	Rate  int /* Serial port speed when using hamlib CAT control for PTT. */
	/* If zero, hamlib will come up with a default for pariticular rig. */
	//#endif

}

// InputConfig is how one of a radio channel's input lines is read.
type InputConfig struct {
	Method Method /* none, serial port, GPIO, LPT. */

	GPIONum int /* GPIO number */

	GPIOName string
	/* originally, gpio number NN was assumed to simply */
	/* have the name gpioNN but this turned out not to be */
	/* the case for CubieBoard where it was longer. */
	/* The PTT fills this in, in its own copy, so we don't have to */
	/* recalculate it each time we access it. */

	Invert bool /* true = active low */
}

// ChannelConfig is what the PTT needs to know about one radio channel.
type ChannelConfig struct {
	Radio      bool // The channel is a radio, so has lines to drive.
	FullDuplex bool // Keep listening while transmitting.
	// ReceiveOnly is set for a channel with no audio output to transmit
	// through, so nothing to key a transmitter for.
	ReceiveOnly bool
	Outputs     [NumOutputTypes]OutputConfig
	Inputs      [NumInputTypes]InputConfig
}
