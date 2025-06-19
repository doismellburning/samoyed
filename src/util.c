void hex_dump (unsigned char *p, int len) 
{
	int n, i, offset;

	offset = 0;
	while (len > 0) {
	  n = len < 16 ? len : 16; 
	  printf ("  %03x: ", offset);
	  for (i=0; i<n; i++) {
	    printf (" %02x", p[i]);
	  }
	  for (i=n; i<16; i++) {
	    printf ("   ");
	  }
	  printf ("  ");
	  for (i=0; i<n; i++) {
	    printf ("%c", isprint(p[i]) ? p[i] : '.');
	  }
	  printf ("\n");
	  p += 16;
	  offset += 16;
	  len -= 16;
	}
}
