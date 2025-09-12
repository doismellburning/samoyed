#include <ctype.h>
#include "textcolor.h"

void hex_dump (unsigned char *p, int len) 
{
	int n, i, offset;

	offset = 0;
	while (len > 0) {
	  n = len < 16 ? len : 16; 
	  dw_printf ("  %03x: ", offset);
	  for (i=0; i<n; i++) {
	    dw_printf (" %02x", p[i]);
	  }
	  for (i=n; i<16; i++) {
	    dw_printf ("   ");
	  }
	  dw_printf ("  ");
	  for (i=0; i<n; i++) {
	    dw_printf ("%c", isprint(p[i]) ? p[i] : '.');
	  }
	  dw_printf ("\n");
	  p += 16;
	  offset += 16;
	  len -= 16;
	}
}

