
/* 
 * Name:	kissserial.h
 */


#include "ax25_pad.h"		/* for packet_t */

#include "config.h"

#include "kiss_frame.h"


void kissserial_init (struct misc_config_s *misc_config);


void kissserial_set_debug (int n);


/* end kissserial.h */
