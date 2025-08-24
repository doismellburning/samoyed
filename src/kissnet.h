
/* 
 * Name:	kissnet.h
 */

#ifndef KISSNET_H
#define KISSNET_H

#include "ax25_pad.h"		/* for packet_t */

#include "config.h"

#include "kiss_frame.h"



void kissnet_init (struct misc_config_s *misc_config);

void kiss_net_set_debug (int n);


#endif  // KISSNET_H

/* end kissnet.h */
