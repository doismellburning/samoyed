
/* kiss_frame.h */

#ifndef KISS_FRAME_H
#define KISS_FRAME_H


#include "audio.h"		/* for struct audio_s */


/*
 * The first byte of a KISS frame has:
 *	channel in upper nybble.
 *	command in lower nybble.
 */

#define KISS_CMD_DATA_FRAME	0
#define KISS_CMD_TXDELAY	1
#define KISS_CMD_PERSISTENCE	2
#define KISS_CMD_SLOTTIME	3
#define KISS_CMD_TXTAIL		4
#define KISS_CMD_FULLDUPLEX	5
#define KISS_CMD_SET_HARDWARE	6
#define XKISS_CMD_DATA		12	// Not supported. http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf
#define XKISS_CMD_POLL		14	// Not supported.
#define KISS_CMD_END_KISS	15



/*
 * Special characters used by SLIP protocol.
 */

#define FEND 0xC0
#define FESC 0xDB
#define TFEND 0xDC
#define TFESC 0xDD



enum kiss_state_e {
	KS_SEARCHING = 0,	/* Looking for FEND to start KISS frame. */
				/* Must be 0 so we can simply zero whole structure to initialize. */
	KS_COLLECTING};		/* In process of collecting KISS frame. */


#define MAX_KISS_LEN 2048	/* Spec calls for at least 1024. */
				/* Might want to make it longer to accommodate */
				/* maximum packet length. */

#define MAX_NOISE_LEN 100

typedef struct kiss_frame_s {
	
	enum kiss_state_e state;

	unsigned char kiss_msg[MAX_KISS_LEN];
				/* Leading FEND is optional. */
				/* Contains escapes and ending FEND. */
	int kiss_len;

	unsigned char noise[MAX_NOISE_LEN];
	int noise_len;

} kiss_frame_t;


void kiss_frame_init (struct audio_s *pa);

int kiss_encapsulate (unsigned char *in, int ilen, unsigned char *out);

int kiss_unwrap (unsigned char *in, int ilen, unsigned char *out);

typedef enum fromto_e { FROM_CLIENT=0, TO_CLIENT=1 } fromto_t;

void kiss_debug_print (fromto_t fromto, char *special, unsigned char *pmsg, int msg_len);


#endif  // KISS_FRAME_H


/* end kiss_frame.h */
