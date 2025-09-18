
#ifndef HDLC_REC2_H
#define HDLC_REC2_H 1


#include "ax25_pad.h"	/* for packet_t, alevel_t */
#include "rrbb.h"
#include "audio.h"		/* for struct audio_s */
#include "dlq.h"		// for fec_type_t definition.




static const char * retry_text[] = {
		"NONE",
		"SINGLE",
		"DOUBLE",
		"TRIPLE",
		"TWO_SEP",
		"PASSALL" };

void hdlc_rec2_init (struct audio_s *audio_config_p);

void hdlc_rec2_block (rrbb_t block);

int hdlc_rec2_try_to_fix_later (rrbb_t block, int chan, int subchan, int slice, alevel_t alevel);

/* Provided by the top level application to process a complete frame. */

void app_process_rec_packet (int chan, int subchan, int slice, packet_t pp, alevel_t level, fec_type_t fec_type, retry_t retries, char *spectrum);

#endif
