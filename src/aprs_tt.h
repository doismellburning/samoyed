
/* aprs_tt.h */

#ifndef APRS_TT_H
#define APRS_TT_H 1



/* Error codes for sending responses to user. */

#define TT_ERROR_OK		0	/* Success. */
#define TT_ERROR_D_MSG		1	/* D was first char of field.  Not implemented yet. */
#define TT_ERROR_INTERNAL	2	/* Internal error.  Shouldn't be here. */
#define TT_ERROR_MACRO_NOMATCH	3	/* No definition for digit sequence. */
#define TT_ERROR_BAD_CHECKSUM	4	/* Bad checksum on call. */
#define TT_ERROR_INVALID_CALL	5	/* Invalid callsign. */
#define TT_ERROR_INVALID_OBJNAME 6	/* Invalid object name. */
#define TT_ERROR_INVALID_SYMBOL	7	/* Invalid symbol specification. */
#define TT_ERROR_INVALID_LOC	8	/* Invalid location. */
#define TT_ERROR_NO_CALL	9	/* No call or object name included. */
#define TT_ERROR_INVALID_MHEAD	10	/* Invalid Maidenhead Locator. */
#define TT_ERROR_INVALID_SATSQ	11	/* Satellite square must be 4 digits. */
#define TT_ERROR_SUFFIX_NO_CALL 12	/* No known callsign for suffix. */

#define TT_ERROR_MAXP1		13	/* Number of items above.  i.e. Last number plus 1. */


#if CONFIG_C		/* Is this being included from config.c? */

/* Must keep in sync with above !!! */

static const char *tt_msg_id[TT_ERROR_MAXP1] = {
	"OK",
	"D_MSG",
	"INTERNAL",
	"MACRO_NOMATCH",
	"BAD_CHECKSUM",
	"INVALID_CALL",
	"INVALID_OBJNAME",
	"INVALID_SYMBOL",
	"INVALID_LOC",
	"NO_CALL",
	"INVALID_MHEAD",
	"INVALID_SATSQ",
	"SUFFIX_NO_CALL"
};

#endif

/* 
 * Configuration options for APRStt.
 */

#define TT_MAX_XMITS 10

#define TT_MTEXT_LEN 64

#define APRSTT_LOC_DESC_LEN 32		/* Need at least 26 */

#define APRSTT_DEFAULT_SYMTAB '\\'
#define APRSTT_DEFAULT_SYMBOL 'A'


#endif

/* end aprs_tt.h */