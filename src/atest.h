struct atest_header_t {
        char riff[4];          /* "RIFF" */
        int filesize;          /* file length - 8 */
        char wave[4];          /* "WAVE" */
};

struct atest_chunk_t {
	char id[4];		/* "LIST" or "fmt " */
	int datasize;
};

struct atest_format_t {
        short wformattag;       /* 1 for PCM. */
        short nchannels;        /* 1 for mono, 2 for stereo. */
        int nsamplespersec;    /* sampling freq, Hz. */
        int navgbytespersec;   /* = nblockalign*nsamplespersec. */
        short nblockalign;      /* = wbitspersample/8 * nchannels. */
        short wbitspersample;   /* 16 or 8. */
	char extras[4];
};

struct atest_wav_data_t {
	char data[4];		/* "data" */
	int datasize;
};
