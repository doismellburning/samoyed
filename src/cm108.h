/* Dire Wolf cm108.h */

extern void cm108_find_ptt (char *output_audio_device, char *ptt_device, int ptt_device_size);

extern int cm108_set_gpio_pin (char *name, int num, int state);

// The CM108, CM109, and CM119 datasheets all say that idProduct can be in the range 
// of 0008 to 000f programmable by MSEL and MODE pin.  How can we tell the difference?

// CM108B is 0012.
// CM119B is 0013.
// CM108AH is 0139 programmable by MSEL and MODE pin.
// CM119A is 013A programmable by MSEL and MODE pin.

// To make matters even more confusing, these can be overridden
// with an external EEPROM.  Some have 8, rather than 4 GPIO.

#define CMEDIA_VID 0xd8c		// Vendor ID
#define CMEDIA_PID1_MIN 0x0008		// range for CM108, CM109, CM119 (no following letters)
#define CMEDIA_PID1_MAX 0x000f

#define CMEDIA_PID_CM108AH	0x0139		// CM108AH
#define CMEDIA_PID_CM108AH_alt	0x013c		// CM108AH? - see issue 210
#define CMEDIA_PID_CM108B	0x0012		// CM108B
#define CMEDIA_PID_CM119A	0x013a		// CM119A
#define CMEDIA_PID_CM119B	0x0013		// CM119B
#define CMEDIA_PID_HS100	0x013c		// HS100

// The SSS chips seem to be pretty much compatible but they have only two GPIO.
// https://irongarment.wordpress.com/2011/03/29/cm108-compatible-chips-with-gpio/
// Data sheet says VID/PID is from an EEPROM but mentions no default.

#define SSS_VID 0x0c76			// SSS1621, SSS1623
#define SSS_PID1 0x1605
#define SSS_PID2 0x1607
#define SSS_PID3 0x160b

// https://github.com/skuep/AIOC/blob/master/stm32/aioc-fw/Src/usb_descriptors.h

#define AIOC_VID 0x1209
#define AIOC_PID 0x7388


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

// Test for supported devices.

#define GOOD_DEVICE(v,p) 	( (v == CMEDIA_VID && ((p >= CMEDIA_PID1_MIN && p <= CMEDIA_PID1_MAX) \
							|| p == CMEDIA_PID_CM108AH \
							|| p == CMEDIA_PID_CM108AH_alt \
							|| p == CMEDIA_PID_CM108B \
							|| p == CMEDIA_PID_CM119A \
							|| p == CMEDIA_PID_CM119B )) \
				 || \
				  (v == SSS_VID && (p == SSS_PID1 || p == SSS_PID2 || p == SSS_PID3)) \
				 || \
				  (v == AIOC_VID && p == AIOC_PID)  )


// Maximum length of name for PTT HID.
// For Linux, this was originally 17 to handle names like /dev/hidraw3.
// Windows has more complicated names.  The longest I saw was 95 but longer have been reported.
// Then we have this  https://groups.io/g/direwolf/message/9622  where 127 is not enough.

#define MAXX_HIDRAW_NAME_LEN 150

/*
 * Result of taking inventory of USB soundcards and USB HIDs.
 */

struct thing_s {
	int vid;		// vendor id, displayed as four hexadecimal digits.
	int pid;		// product id, displayed as four hexadecimal digits.
	char card_number[8];	// "Card" Number.  e.g.  2 for plughw:2,0
	char card_name[32];	// Audio Card Name, assigned by system (e.g. Device_1) or by udev rule.
	char product[32];	// product name (e.g. manufacturer, model)
	char devnode_sound[22];	// e.g. /dev/snd/pcmC0D0p
	char plughw[72];	// Above in more familiar format e.g. plughw:0,0
				// Oversized to silence a compiler warning.
	char plughw2[72];	// With name rather than number.
	char devpath[128];	// Kernel dev path.  Does not include /sys mount point.
	char devnode_hidraw[MAXX_HIDRAW_NAME_LEN];
				// e.g. /dev/hidraw3  -  for Linux - was length 17
				// The Windows path for a HID looks like this, lengths up to 95 seen.
				// \\?\hid#vid_0d8c&pid_000c&mi_03#8&164d11c9&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}
	char devnode_usb[25];	// e.g. /dev/bus/usb/001/012
				// This is what we use to match up audio and HID.
};
