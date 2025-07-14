struct wav_header {             /* .WAV file header. */
        char riff[4];           /* "RIFF" */
        int filesize;          /* file length - 8 */
        char wave[4];           /* "WAVE" */
        char fmt[4];            /* "fmt " */
        int fmtsize;           /* 16. */
        short wformattag;       /* 1 for PCM. */
        short nchannels;        /* 1 for mono, 2 for stereo. */
        int nsamplespersec;    /* sampling freq, Hz. */
        int navgbytespersec;   /* = nblockalign * nsamplespersec. */
        short nblockalign;      /* = wbitspersample / 8 * nchannels. */
        short wbitspersample;   /* 16 or 8. */
        char data[4];           /* "data" */
        int datasize;          /* number of bytes following. */
} ;

				/* 8 bit samples are unsigned bytes */
				/* in range of 0 .. 255. */

				/* 16 bit samples are signed short */
				/* in range of -32768 .. +32767. */

#define MY_RAND_MAX 0x7fffffff
