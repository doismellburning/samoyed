package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:     Quick test for some tt_user functions
 *
 * Description:	Just a smattering, not an organized test.
 *
 *----------------------------------------------------------------*/

// #include <stdlib.h>
// #include <string.h>
// #include <stdio.h>
// #include <unistd.h>
// #include <ctype.h>
// #include <time.h>
// #include <assert.h>
// extern int TT_TESTS_RUNNING;
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func tt_user_test_main(t *testing.T) {
	t.Helper()

	TT_TESTS_RUNNING = true
	defer func() {
		TT_TESTS_RUNNING = false
	}()

	/* Fake audio config - All we care about is mycall for constructing object report packet. */

	var my_audio_config audio_s

	my_audio_config.mycall[0] = "WB20SZ-15"

	/* Fake TT gateway config. */

	var my_tt_config tt_config_s

	/* Don't care about the location translation here. */

	my_tt_config.retain_time = 20 /* Normally 80 minutes. */
	my_tt_config.num_xmits = 3
	assert.LessOrEqual(t, my_tt_config.num_xmits, TT_MAX_XMITS)
	my_tt_config.xmit_delay[0] = 3 /* Before initial transmission. */
	my_tt_config.xmit_delay[1] = 5
	my_tt_config.xmit_delay[2] = 5

	my_tt_config.corral_lat = 42.61900
	my_tt_config.corral_lon = -71.34717
	my_tt_config.corral_offset = 0.02 / 60
	my_tt_config.corral_ambiguity = 0

	tt_user_init(&my_audio_config, &my_tt_config)

	// tt_user_heard (char *callsign, int ssid, char overlay, char symbol, char *loc_text, double latitude,
	//              double longitude, int ambiguity, char *freq, char *ctcss, char *comment, char mic_e, char *dao);

	tt_user_heard("TEST1", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "", "", "", ' ', "!T99!")
	SLEEP_SEC(1)
	tt_user_heard("TEST2", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "", "", "", ' ', "!T99!")
	SLEEP_SEC(1)
	tt_user_heard("TEST3", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "", "", "", ' ', "!T99!")
	SLEEP_SEC(1)
	tt_user_heard("TEST4", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "", "", "", ' ', "!T99!")
	SLEEP_SEC(1)
	tt_user_heard("WB2OSZ", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "", "", "", ' ', "!T99!")
	tt_user_heard("K2H", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "", "", "", ' ', "!T99!")
	tt_user_dump()

	tt_user_heard("679", 12, 'J', 'A', "", 37.25, -71.75, 0, "", " ", " ", ' ', "!T99!")
	tt_user_heard("WB2OSZ", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "146.520MHz", "", "", ' ', "!T99!")
	tt_user_heard("WB1GOF", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "146.955MHz", "074", "", ' ', "!T99!")
	tt_user_heard("679", 12, 'J', 'A', "", G_UNKNOWN, G_UNKNOWN, 0, "", "", "Hello, world", '9', "!T99!")
	tt_user_dump()

	for range 30 {
		SLEEP_SEC(1)
		tt_user_background()
	}
}
