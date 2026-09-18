// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package cm108 drives the GPIO pins of CM108/CM119 (or compatible) USB audio
// adapters, which is how most amateur radio interfaces of that sort provide a
// Push To Talk (PTT) signal.
//
// [Inventory] takes stock of the USB audio devices and Human Interface Devices
// (HID) attached to the machine and pairs them up, [FindPTT] picks out the HID
// belonging to a given audio device, and [SetGPIOPin] drives a pin on it.
// Only Linux is supported; elsewhere the functions are stubs that report that
// they are unavailable.
//
// # Background
//
//	There is an increasing demand for using the GPIO pins of USB audio devices for PTT.
//	We have a few commercial products:
//
//		DINAH		https://hamprojects.info/dinah/
//		PAUL		https://hamprojects.info/paul/
//		DMK URI		http://www.dmkeng.com/URI_Order_Page.htm
//		RB-USB RIM	http://www.repeater-builder.com/products/usb-rim-lite.html
//		RA-35		http://www.masterscommunications.com/products/radio-adapter/ra35.html
//
//	and homebrew projects which are all very similar.
//
//		http://www.qsl.net/kb9mwr/projects/voip/usbfob-119.pdf
//		http://rtpdir.weebly.com/uploads/1/6/8/7/1687703/usbfob.pdf
//		http://www.repeater-builder.com/projects/fob/USB-Fob-Construction.pdf
//		https://irongarment.wordpress.com/2011/03/29/cm108-compatible-chips-with-gpio/
//
//	Homebrew plans all use GPIO 3 because it is easier to tack solder a wire to a pin on the end.
//	All of the products, that I have seen, also use the same pin so this is the default.
//
//	Soundmodem and hamlib paved the way but didn't get too far.
//	Dire Wolf 1.3 added HAMLIB support (Linux only) which theoretically allows this in a
//	painful roundabout way.  This is documented in the User Guide, section called,
//		 "Hamlib PTT Example 2: Use GPIO of USB audio adapter.  (e.g. DMK URI)"
//
//	It's rather involved and the explanation doesn't cover the case of multiple
//	USB-Audio adapters.  It is not as straightforward as you might expect.  Here we have
//	an example of 3 C-Media USB adapters, a SignaLink USB, a keyboard, and a mouse.
//
//
//	    VID  PID   Product                          Sound                  ADEVICE         HID [ptt]
//	    ---  ---   -------                          -----                  -------         ---------
//	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC1D0c      plughw:1,0      /dev/hidraw0
//	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC1D0p      plughw:1,0      /dev/hidraw0
//	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/controlC1                     /dev/hidraw0
//	    08bb 2904  USB Audio CODEC                  /dev/snd/pcmC2D0c      plughw:2,0      /dev/hidraw2
//	    08bb 2904  USB Audio CODEC                  /dev/snd/pcmC2D0p      plughw:2,0      /dev/hidraw2
//	    08bb 2904  USB Audio CODEC                  /dev/snd/controlC2                     /dev/hidraw2
//	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC0D0c      plughw:0,0      /dev/hidraw1
//	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/pcmC0D0p      plughw:0,0      /dev/hidraw1
//	**  0d8c 000c  C-Media USB Headphone Set        /dev/snd/controlC0                     /dev/hidraw1
//	**  0d8c 0008  C-Media USB Audio Device         /dev/snd/pcmC4D0c      plughw:4,0      /dev/hidraw6
//	**  0d8c 0008  C-Media USB Audio Device         /dev/snd/pcmC4D0p      plughw:4,0      /dev/hidraw6
//	**  0d8c 0008  C-Media USB Audio Device         /dev/snd/controlC4                     /dev/hidraw6
//	    413c 2010  Dell USB Keyboard                                                       /dev/hidraw4
//	    0461 4d15  USB Optical Mouse                                                       /dev/hidraw5
//
//
//	The USB soundcards (/dev/snd/pcm...) have an associated Human Interface Device (HID)
//	corresponding to the GPIO pins which are sometimes connected to pushbuttons.
//	The mapping has no obvious pattern.
//
//		Sound Card 0		HID 1
//		Sound Card 1		HID 0
//		Sound Card 2		HID 2
//		Sound Card 4		HID 6
//
//	That would be a real challenge if you had to figure that all out and configure manually.
//	Dire Wolf version 1.5 makes this much more flexible and easier to use by supporting multiple
//	sound devices and automatically determining the corresponding HID for the PTT signal.
//
//	In version 1.7, we add a half-backed solution for Windows.  It's fine for situations
//	with a single USB Audio Adapter, but does not automatically handle the multiple device case.
//	Manual configuration needs to be used in this case.
//
//	Here is something new and interesting.  The All in One cable (AIOC).
//	https://github.com/skuep/AIOC/tree/master
//
//	A microcontroller is used to emulate a CM108-compatible soundcard
//	and a serial port.  It fits right on the side of a Bao Feng or similar.
package cm108

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

// MaxHidrawNameLen is the maximum length of name for PTT HID.
// For Linux, this was originally 17 to handle names like /dev/hidraw3.
// Windows has more complicated names.  The longest I saw was 95 but longer have been reported.
// Then we have this  https://groups.io/g/direwolf/message/9622  where 127 is not enough.
const MaxHidrawNameLen = 150

// MaxThings is the default limit on the number of devices [Inventory] collects.
const MaxThings = 60

// Thing is the result of taking inventory of USB soundcards and USB HIDs.
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

// GoodDevice tests for supported devices.
func GoodDevice(v, p int) bool {
	return (v == CMEDIA_VID && ((p >= CMEDIA_PID1_MIN && p <= CMEDIA_PID1_MAX) || p == CMEDIA_PID_CM108AH ||
		p == CMEDIA_PID_CM108AH_alt || p == CMEDIA_PID_CM108B || p == CMEDIA_PID_CM119A || p == CMEDIA_PID_CM119B)) ||
		(v == SSS_VID && (p == SSS_PID1 || p == SSS_PID2 || p == SSS_PID3)) ||
		(v == AIOC_VID && p == AIOC_PID)
}
