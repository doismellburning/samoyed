//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2021  John Langner, WB2OSZ
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
//

#include "direwolf.h"

#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <assert.h>

#include "textcolor.h"
#include "il2p.h"
#include "ax25_pad.h"
#include "ax25_pad2.h"
#include "multi_modem.h"


/*-------------------------------------------------------------
 *
 * Name:	il2p_test.c
 *
 * Purpose:	Mock functions for unit tests for IL2P protocol functions.
 *
 * Errors:	Die if anything goes wrong.
 *
 *--------------------------------------------------------------*/


/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test serialize / deserialize.
//
//	This uses same functions used on the air.	
//
/////////////////////////////////////////////////////////////////////////////////////////////

static char addrs2[] = "AA1AAA-1>ZZ9ZZZ-9";
static char addrs3[] = "AA1AAA-1>ZZ9ZZZ-9,DIGI*";
static char text[] = 
	"'... As I was saying, that seems to be done right - though I haven't time to look it over thoroughly just now - and that shows that there are three hundred and sixty-four days when you might get un-birthday presents -'"
	"\n"
	"'Certainly,' said Alice."
	"\n"
	"'And only one for birthday presents, you know. There's glory for you!'"
	"\n"
	"'I don't know what you mean by \"glory\",' Alice said."
	"\n"
	"Humpty Dumpty smiled contemptuously. 'Of course you don't - till I tell you. I meant \"there's a nice knock-down argument for you!\"'"
	"\n"
	"'But \"glory\" doesn't mean \"a nice knock-down argument\",' Alice objected."
	"\n"
	"'When I use a word,' Humpty Dumpty said, in rather a scornful tone, 'it means just what I choose it to mean - neither more nor less.'"
	"\n"
	"'The question is,' said Alice, 'whether you can make words mean so many different things.'"
	"\n"
	"'The question is,' said Humpty Dumpty, 'which is to be master - that's all.'"
	"\n" ;


static int rec_count = -1;	// disable deserialized packet test.
static int polarity = 0;

int IL2P_TEST = 0;

// Serializing calls this which then simulates the demodulator output.

void tone_gen_put_bit_fake (int chan, int data)
{
	il2p_rec_bit (chan, 0, 0, data);
}

void tone_gen_put_bit_real (int chan, int data);

void tone_gen_put_bit (int chan, int data) {
	if (IL2P_TEST) {
		tone_gen_put_bit_fake(chan, data);
	} else {
		tone_gen_put_bit_real(chan, data);
	}
}

// This is called when a complete frame has been deserialized.

void multi_modem_process_rec_packet_fake (int chan, int subchan, int slice, packet_t pp, alevel_t alevel, retry_t retries, fec_type_t fec_type)
{
	if (rec_count < 0) return;	// Skip check before serdes test.

	rec_count++;

	// Does it have the the expected content?
	
	unsigned char *pinfo;
	int len = ax25_get_info(pp, &pinfo);
	assert (len == strlen(text));
	assert (strcmp(text, (char*)pinfo) == 0);

	dw_printf ("Number of symbols corrected: %d\n", retries);
	if (polarity == 2) {	// expecting errors corrected.
	    assert (retries == 10);
	}
	else {	// should be no errors.
	    assert (retries == 0);
	}

	ax25_delete (pp);
}

void multi_modem_process_rec_packet_real (int chan, int subchan, int slice, packet_t pp, alevel_t alevel, retry_t retries, fec_type_t fec_type);

void multi_modem_process_rec_packet (int chan, int subchan, int slice, packet_t pp, alevel_t alevel, retry_t retries, fec_type_t fec_type) {
	if (IL2P_TEST) {
		multi_modem_process_rec_packet_fake(chan, subchan, slice, pp, alevel, retries, fec_type);
	} else {
		multi_modem_process_rec_packet_real(chan, subchan, slice, pp, alevel, retries, fec_type);
	}
}

alevel_t demod_get_audio_level_fake (int chan, int subchan)
{
	alevel_t alevel;
	memset (&alevel, 0, sizeof(alevel));
	return (alevel);
}

alevel_t demod_get_audio_level_real (int chan, int subchan);

alevel_t demod_get_audio_level (int chan, int subchan) {
	if (IL2P_TEST) {
		return demod_get_audio_level_fake(chan, subchan);
	} else {
		return demod_get_audio_level_real(chan, subchan);
	}
}

// end il2p_test.c
