//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2017,2019,2021  John Langner, WB2OSZ
//
//    Parts of this were adapted from "hamlib" which contains the notice:
//
//	 *  Copyright (c) 2000-2012 by Stephane Fillod
//	 *  Copyright (c) 2011 by Andrew Errington
//	 *  CM108 detection code Copyright (c) Thomas Sailer used with permission
//
//    This program is free software: you can redistribute it and/or modify
//    it under the terms of the GNU General Public License as published by
//    the Free Software Foundation, either version 2 of the License, or
//    (at your option) any later version.
//
//    This program is distributed in the hope that it will be useful,
//    but WITHOUT ANY WARRANTY; without even the implied warranty of
//    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//    GNU General Public License for more details.
//
//    You should have received a copy of the GNU General Public License
//    along with this program.  If not, see <http://www.gnu.org/licenses/>.

/*------------------------------------------------------------------
 *
 * Module:      cm108.c
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

#include "direwolf.h"

#include <stdio.h>
#include <stdlib.h>
#include <locale.h>
#include <unistd.h>
#include <string.h>
#include <regex.h>

#if __WIN32__
#include <wchar.h>
#include "hidapi.h"
#elif __APPLE__
#include "hidapi.h"
#else
#include <libudev.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/ioctl.h>			// ioctl, _IOR
#include <fcntl.h>
#include <errno.h>
#include <linux/hidraw.h>		// for HIDIOCGRAWINFO
#endif

#include "textcolor.h"
#include "cm108.h"

static int cm108_write (char *name, int iomask, int iodata);


// Look out for null source pointer, and avoid buffer overflow on destination.

#define SAFE_STRCPY(to,from)	{ if (from != NULL) { strncpy(to,from,sizeof(to)); to[sizeof(to)-1] = '\0'; } }


// Used to process regular expression matching results.

#if !defined(__WIN32__) && !defined(__APPLE__)

static void substr_se (char *dest, const char *src, int start, int endp1)
{
	int len = endp1 - start;

	if (start < 0 || endp1 < 0 || len <= 0) {
	  dest[0] = '\0';
	  return;
	}
	memcpy (dest, src + start, len);
	dest[len] = '\0';

} /* end substr_se */

#endif

int cm108_inventory (struct thing_s *things, int max_things);

#define MAXX_THINGS 60

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


int cm108_inventory (struct thing_s *things, int max_things)
{
	int num_things = 0;
	memset (things, 0, sizeof(struct thing_s) * max_things);

#if __WIN32__ || __APPLE__

	struct hid_device_info *devs, *cur_dev;

	if (hid_init()) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf("cm108_inventory: hid_init() failed.\n");
	  return (-1);
	}

	devs = hid_enumerate(0x0, 0x0);
	cur_dev = devs;
	while (cur_dev) {
#if 0
	  printf("Device Found\n  type: %04hx %04hx\n  path: %s\n  serial_number: %ls", cur_dev->vendor_id, cur_dev->product_id, cur_dev->path, cur_dev->serial_number);
	  printf("\n");
	  printf("  Manufacturer: %ls\n", cur_dev->manufacturer_string);
	  printf("  Product:      %ls\n", cur_dev->product_string);
	  printf("  Release:      %hx\n", cur_dev->release_number);
	  printf("  Interface:    %d\n",  cur_dev->interface_number);
	  printf("  Usage (page): 0x%hx (0x%hx)\n", cur_dev->usage, cur_dev->usage_page);
	  printf("\n");
#endif
	  if (num_things < max_things && cur_dev->vendor_id != 0x051d) {		// FIXME - remove exception
	    things[num_things].vid = cur_dev->vendor_id;
	    things[num_things].pid = cur_dev->product_id;
	    wcstombs (things[num_things].product, cur_dev->product_string, sizeof(things[num_things].product));
	    things[num_things].product[sizeof(things[num_things].product) - 1] = '\0';
	    strlcpy (things[num_things].devnode_hidraw, cur_dev->path, sizeof(things[num_things].devnode_hidraw));

	    num_things++;
	  }
	  cur_dev = cur_dev->next;
	}
	hid_free_enumeration(devs);

#else // Linux, with udev

	struct udev *udev;
	struct udev_enumerate *enumerate;
	struct udev_list_entry *devices, *dev_list_entry;
	struct udev_device *dev;
	struct udev_device *parentdev;

	char const *pattrs_id = NULL;
	char const *pattrs_number = NULL;
	char card_devpath[128] = "";


/* 
 * First get a list of the USB audio devices.
 * This is based on the example in http://www.signal11.us/oss/udev/
 */
	udev = udev_new();
	if (!udev) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf("INTERNAL ERROR: Can't create udev.\n");
	  return (-1);
	}
	
	enumerate = udev_enumerate_new(udev);
	udev_enumerate_add_match_subsystem(enumerate, "sound");
	udev_enumerate_scan_devices(enumerate);
	devices = udev_enumerate_get_list_entry(enumerate);
	udev_list_entry_foreach(dev_list_entry, devices) {
	  const char *path = udev_list_entry_get_name(dev_list_entry);
	  dev = udev_device_new_from_syspath(udev, path);
	  char const *devnode = udev_device_get_devnode(dev);

	  if (devnode == NULL ) {
	    // I'm not happy with this but couldn't figure out how
	    // to get attributes from one level up from the pcmC?D?? node.
	    strlcpy (card_devpath, path, sizeof(card_devpath));
	    pattrs_id = udev_device_get_sysattr_value(dev,"id");
	    pattrs_number = udev_device_get_sysattr_value(dev,"number");
	    //dw_printf (" >card_devpath = %s\n", card_devpath);
	    //dw_printf (" >>pattrs_id = %s\n", pattrs_id);
	    //dw_printf (" >>pattrs_number = %s\n", pattrs_number);
	  }
	  else {
	    parentdev = udev_device_get_parent_with_subsystem_devtype( dev, "usb", "usb_device"); 
	    if (parentdev != NULL) {
	      char const *p;
	      int vid = 0;
	      int pid = 0;

	      p = udev_device_get_sysattr_value(parentdev,"idVendor");
	      if (p != NULL) vid = strtol(p, NULL, 16);
	      p = udev_device_get_sysattr_value(parentdev,"idProduct");
	      if (p != NULL) pid = strtol(p, NULL, 16);

	      if (num_things < max_things) {
	        things[num_things].vid = vid;
	        things[num_things].pid = pid;
	        SAFE_STRCPY (things[num_things].card_name, pattrs_id);
	        SAFE_STRCPY (things[num_things].card_number, pattrs_number);
	        SAFE_STRCPY (things[num_things].product, udev_device_get_sysattr_value(parentdev,"product"));
	        SAFE_STRCPY (things[num_things].devnode_sound, devnode);
	        SAFE_STRCPY (things[num_things].devnode_usb, udev_device_get_devnode(parentdev));
	        strlcpy (things[num_things].devpath, card_devpath, sizeof(things[num_things].devpath));
	        num_things++;
	      }
	      udev_device_unref(parentdev);
	    }
	  }
	}
	udev_enumerate_unref(enumerate);
	udev_unref(udev);

/* 
 * Now merge in all of the USB HID.
 */
	udev = udev_new();
	if (!udev) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf("INTERNAL ERROR: Can't create udev.\n");
	  return (-1);
	}

	enumerate = udev_enumerate_new(udev);
	udev_enumerate_add_match_subsystem(enumerate, "hidraw");
	udev_enumerate_scan_devices(enumerate);
	devices = udev_enumerate_get_list_entry(enumerate);
	udev_list_entry_foreach(dev_list_entry, devices) {
	  const char *path = udev_list_entry_get_name(dev_list_entry);
	  dev = udev_device_new_from_syspath(udev, path);
	  char const *devnode = udev_device_get_devnode(dev);
	  if (devnode != NULL) {
	    parentdev = udev_device_get_parent_with_subsystem_devtype( dev, "usb", "usb_device"); 
	    if (parentdev != NULL) {
	      char const *p;
	      int vid = 0;
	      int pid = 0;

	      p = udev_device_get_sysattr_value(parentdev,"idVendor");
	      if (p != NULL) vid = strtol(p, NULL, 16);
	      p = udev_device_get_sysattr_value(parentdev,"idProduct");
	      if (p != NULL) pid = strtol(p, NULL, 16);

	      int j, matched = 0;
	      char const *usb = udev_device_get_devnode(parentdev);

	      // Add hidraw name to any matching existing.
	      for (j = 0; j < num_things; j++) {
	        if (things[j].vid == vid && things[j].pid == pid && usb != NULL && strcmp(things[j].devnode_usb,usb) == 0) {
	          matched = 1;
	          SAFE_STRCPY (things[j].devnode_hidraw, devnode);
	        }
	      }

	      // If it did not match to existing, add new entry.
	      if (matched == 0 && num_things < max_things) {
	        things[num_things].vid = vid;
	        things[num_things].pid = pid;
	        SAFE_STRCPY (things[num_things].product, udev_device_get_sysattr_value(parentdev,"product"));
	        SAFE_STRCPY (things[num_things].devnode_hidraw, devnode);
	        SAFE_STRCPY (things[num_things].devnode_usb, usb);
	        SAFE_STRCPY (things[num_things].devpath, udev_device_get_devpath(dev));
	        num_things++;
	      }
	      udev_device_unref(parentdev);
	    }
	  }
	}
	udev_enumerate_unref(enumerate);
	udev_unref(udev);

/*
 * Seeing the form /dev/snd/pcmC4D0p will be confusing to many because we
 * would generally something like plughw:4,0 for in the direwolf configuration file.
 * Construct the more familiar form.
 * Previously we only used the numeric form.  In version 1.6, the name is listed as well
 * and we describe how to assign names based on the physical USB socket for repeatability.
 */
	int i;
	regex_t pcm_re;
	char emsg[100];
	int e = regcomp (&pcm_re, "pcmC([0-9]+)D([0-9]+)[cp]", REG_EXTENDED);
	if (e) {
	  regerror (e, &pcm_re, emsg, sizeof(emsg));
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf("INTERNAL ERROR:  %s:%d: %s\n", __FILE__, __LINE__, emsg);
	  return (-1);
	}

	for (i = 0; i < num_things; i++) {
	  regmatch_t match[3];

	  if (regexec (&pcm_re, things[i].devnode_sound, 3, match, 0) == 0) {
	    char c[32], d[32];
	    substr_se (c, things[i].devnode_sound, match[1].rm_so, match[1].rm_eo);
	    substr_se (d, things[i].devnode_sound, match[2].rm_so, match[2].rm_eo);
	    snprintf (things[i].plughw, sizeof(things[i].plughw), "plughw:%s,%s", c, d);
	    snprintf (things[i].plughw2, sizeof(things[i].plughw), "plughw:%s,%s", things[i].card_name, d);
	  }
	}

#endif  // end Linux

	return (num_things);

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

void cm108_find_ptt (char *output_audio_device, char *ptt_device,  int ptt_device_size)
{
	struct thing_s things[MAXX_THINGS];
	int num_things;

	//dw_printf ("DEBUG: cm108_find_ptt('%s')\n", output_audio_device);

	strlcpy (ptt_device, "", ptt_device_size);

	// Possible improvement: Skip if inventory already taken.
	num_things = cm108_inventory (things, MAXX_THINGS);

#if __WIN32__ || __APPLE__
		//  FIXME - This is just a half baked implementation.
		//  I have not been able to figure out how to find the connection
		//  between the audio device and HID in the same package.
		//  This is fine for a single USB Audio Adapter, good enough for most people.
		//  Those with multiple devices will need to manually configure PTT device path.

	// Count how many good devices we have.

	int good_devices = 0;

	for (int i = 0; i < num_things; i++) {
	  if (GOOD_DEVICE(things[i].vid,things[i].pid) ) {
	    good_devices++;
	    //dw_printf ("DEBUG: success! returning '%s'\n", things[i].devnode_hidraw);
	    strlcpy (ptt_device, things[i].devnode_hidraw, ptt_device_size);
	  }
	}

	if (good_devices == 0) return;		// None found - caller will print a message.

	if (good_devices == 1) return;		// Success - Only one candidate device.

	text_color_set(DW_COLOR_ERROR);
	dw_printf ("There are multiple USB Audio Devices with GPIO capability.\n");
	dw_printf ("Explicitly specify one of them for more predictable results:\n");
	for (int i = 0; i < num_things; i++) {
	  if (GOOD_DEVICE(things[i].vid,things[i].pid) ) {
	    dw_printf ("   \"%s\"\n", things[i].devnode_hidraw);
	  }
	}
	dw_printf ("Run the \"cm108\" utility for more details.\n");
	text_color_set(DW_COLOR_INFO);
#else
	regex_t sound_re;
	char emsg[100];
	int e = regcomp (&sound_re, ".+:(CARD=)?([A-Za-z0-9_]+)(,.*)?", REG_EXTENDED);
	if (e) {
	  regerror (e, &sound_re, emsg, sizeof(emsg));
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf("INTERNAL ERROR:  %s:%d: %s\n", __FILE__, __LINE__, emsg);
	  return;
	}

	char num_or_name[64];
	strcpy (num_or_name, "");
	regmatch_t sound_match[4];
	if (regexec (&sound_re, output_audio_device, 4, sound_match, 0) == 0) {
	  substr_se (num_or_name, output_audio_device, sound_match[2].rm_so, sound_match[2].rm_eo);
	  //dw_printf ("DEBUG: Got '%s' from '%s'\n", num_or_name, output_audio_device);
	}
	if (strlen(num_or_name) == 0) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Could not extract card number or name from %s\n", output_audio_device);
	  dw_printf ("Can't automatically find matching HID for PTT.\n");
	  return;
	}

	for (int i = 0; i < num_things; i++) {
	  //dw_printf ("DEBUG: i=%d, card_name='%s', card_number='%s'\n", i, things[i].card_name, things[i].card_number);
	  if (strcmp(num_or_name,things[i].card_name) == 0 || strcmp(num_or_name,things[i].card_number) == 0) {
	    //dw_printf ("DEBUG: success! returning '%s'\n", things[i].devnode_hidraw);
	    strlcpy (ptt_device, things[i].devnode_hidraw, ptt_device_size);
	    if ( ! GOOD_DEVICE(things[i].vid,things[i].pid) ) {
	      text_color_set(DW_COLOR_ERROR);
	      dw_printf ("Warning: USB audio card %s (%s) is not a device known to work with GPIO PTT.\n",
				things[i].card_number, things[i].card_name);
	    }
	    return;
	  }
	}
#endif
	
}  /* end cm108_find_ptt */



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


int cm108_set_gpio_pin (char *name, int num, int state)
{
	int iomask;
	int iodata;

	if (num < 1 || num > 8) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf("%s CM108 GPIO number %d must be in range of 1 thru 8.\n", name, num);
	  return (-1);
	}

	if (state != 0 && state != 1) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf("%s CM108 GPIO state %d must be 0 or 1.\n", name, state);
	  return (-1);
	}

	iomask = 1 << (num - 1);	// 0=input, 1=output
	iodata = state << (num - 1);	// 0=low, 1=high
	return (cm108_write (name, iomask, iodata));

}  /* end cm108_set_gpio_pin */


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

static int cm108_write (char *name, int iomask, int iodata)
{

#if __WIN32__ || __APPLE__

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("TEMP DEBUG cm108_write:  %s %d %d\n", name, iomask, iodata);

	hid_device *handle = hid_open_path(name);
	if (handle == NULL) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Could not open %s for write\n", name);
	  return (-1);
	}

	unsigned char io[5];
	io[0] = 0;
	io[1] = 0;
	io[2] = iodata;
	io[3] = iomask;
	io[4] = 0;

	int res = hid_write(handle, io, sizeof(io));
	if (res < 0) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Write failed to %s\n", name);
	  return (-1);
	}

	hid_close(handle);

#else
	int fd;
	struct hidraw_devinfo info;
	char io[5];
	int n;

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

	fd = open (name, O_WRONLY);
	if (fd == -1) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Could not open %s for write, errno=%d\n", name, errno);
	  if (errno == EACCES) {		// 13
	    dw_printf ("Type \"ls -l %s\" and verify that it has audio group rw similar to this:\n", name);
	    dw_printf ("    crw-rw---- 1 root audio 247, 0 Oct  6 19:24 %s\n", name);
	    dw_printf ("rather than root-only access like this:\n");
	    dw_printf ("    crw------- 1 root root 247, 0 Sep 24 09:40 %s\n", name);
	  }
	  return (-1);
	}

	// Just for fun, let's get the device information.

#if 1
	n = ioctl(fd, HIDIOCGRAWINFO, &info);
	if (n == 0) {
	  if ( ! GOOD_DEVICE(info.vendor, info.product)) {
	    text_color_set(DW_COLOR_ERROR);
	    dw_printf ("ioctl HIDIOCGRAWINFO failed for %s. errno = %d.\n", name, errno);
	  }
	}
	else {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("%s is not a supported device type.  Proceed at your own risk.  vid=%04x pid=%04x\n", name, info.vendor, info.product);
	}
#endif
	// To make a long story short, I think we need 0 for the first two bytes.

	io[0] = 0;
	io[1] = 0;
// Issue 210 - These were reversed. Fixed in 1.6.
	io[2] = iodata;
	io[3] = iomask;
	io[4] = 0;

	// Writing 4 bytes fails with errno 32, EPIPE, "broken pipe."
	// Hamlib writes 5 bytes which I don't understand.
	// Writing 5 bytes works.
	// I have no idea why.  From the CMedia datasheet it looks like we need 4.

	n = write (fd, io, sizeof(io));
	if (n != sizeof(io)) {
	  //  Errors observed during development.
	  //  as pi		EACCES          13      /* Permission denied */
	  //  as root		EPIPE           32      /* Broken pipe - Happens if we send 4 bytes */

	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Write to %s failed, n=%d, errno=%d\n", name, n, errno);

	  if (errno == EACCES) {
	    dw_printf ("Type \"ls -l %s\" and verify that it has audio group rw similar to this:\n", name);
	    dw_printf ("    crw-rw---- 1 root audio 247, 0 Oct  6 19:24 %s\n", name);
	    dw_printf ("rather than root-only access like this:\n");
	    dw_printf ("    crw------- 1 root root 247, 0 Sep 24 09:40 %s\n", name);
	    dw_printf ("This permission should be set by one of:\n");
	    dw_printf ("/etc/udev/rules.d/99-direwolf-cmedia.rules\n");
	    dw_printf ("/usr/lib/udev/rules.d/99-direwolf-cmedia.rules\n");
	    dw_printf ("which should be created by the installation process.\n");
	    dw_printf ("Your account must be in the 'audio' group.\n");
	  }
	  close (fd);
	  return (-1);
	}

	close (fd);

#endif
	return (0);

}  /* end cm108_write */
