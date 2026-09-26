package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:     Quick test for some tt_user functions
 *
 * Description:	Just a smattering, not an organized test.
 *
 *----------------------------------------------------------------*/

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

// assertObjectReport checks the object report for callsign, pinning its
// last heard time so the timestamp in the report is predictable.
func assertObjectReport(t *testing.T, callsign string, firstTime bool, expected string) {
	t.Helper()

	var i = tt_user_search(callsign, 'J')
	if !assert.GreaterOrEqual(t, i, 0, "%s not in user table", callsign) {
		return
	}

	tt_user[i].last_heard = time.Date(2026, 9, 26, 6, 28, 0, 0, time.UTC)

	assert.Equal(t, expected, object_report_text(i, firstTime))
}

func Test_TTUser(t *testing.T) {
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

	my_tt_config.obj_xmit_via = "WIDE2-1"

	tt_user_init(&my_audio_config, &my_tt_config)

	// tt_user_heard (char *callsign, int ssid, char overlay, char symbol, char *loc_text, double latitude,
	//              double longitude, int ambiguity, char *freq, char *ctcss, char *comment, char mic_e, char *dao);

	tt_user_heard("TEST1", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "", "", "", ' ', "!T99!")
	assertObjectReport(t, "TEST1", true, "WB20SZ-15>SMYD00:;TEST1-12 *260628z4237.14NJ07120.83WA!T99!")
	tt_user_heard("TEST2", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "", "", "", ' ', "!T99!")
	assertObjectReport(t, "TEST2", true, "WB20SZ-15>SMYD00:;TEST2-12 *260628z4237.12NJ07120.83WA!T99!")
	tt_user_heard("TEST3", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "", "", "", ' ', "!T99!")
	assertObjectReport(t, "TEST3", true, "WB20SZ-15>SMYD00:;TEST3-12 *260628z4237.10NJ07120.83WA!T99!")
	tt_user_heard("TEST4", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "", "", "", ' ', "!T99!")
	assertObjectReport(t, "TEST4", true, "WB20SZ-15>SMYD00:;TEST4-12 *260628z4237.08NJ07120.83WA!T99!")
	tt_user_heard("WB2OSZ", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "", "", "", ' ', "!T99!")
	assertObjectReport(t, "WB2OSZ", true, "WB20SZ-15>SMYD00:;WB2OSZ-12*260628z4237.06NJ07120.83WA!T99!")
	tt_user_heard("K2H", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "", "", "", ' ', "!T99!")
	assertObjectReport(t, "K2H", true, "WB20SZ-15>SMYD00:;K2H-12   *260628z4237.04NJ07120.83WA!T99!")
	tt_user_dump()

	// Only the later, radio, transmissions go via the configured path.
	assertObjectReport(t, "TEST1", false, "WB20SZ-15>SMYD00,WIDE2-1:;TEST1-12 *260628z4237.14NJ07120.83WA!T99!")

	tt_user_heard("679", 12, 'J', 'A', "", maybe.Just(37.25), maybe.Just(-71.75), maybe.Just(0), "", " ", " ", ' ', "!T99!")
	assertObjectReport(t, "WB2OSZ", true, "WB20SZ-15>SMYD00:;WB2OSZ-12*260628z3715.00NJ07145.00WAToff   !T99!")
	tt_user_heard("WB2OSZ", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "146.520MHz", "", "", ' ', "!T99!")
	assertObjectReport(t, "WB2OSZ", true, "WB20SZ-15>SMYD00:;WB2OSZ-12*260628z3715.00NJ07145.00WA146.520MHz Toff   !T99!")
	tt_user_heard("WB1GOF", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "146.955MHz", "074", "", ' ', "!T99!")
	assertObjectReport(t, "WB1GOF", true, "WB20SZ-15>SMYD00:;WB1GOF-12*260628z4237.06NJ07120.83WA146.955MHz T074 !T99!")
	tt_user_heard("679", 12, 'J', 'A', "", maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Just(0), "", "", "Hello, world", '9', "!T99!")
	assertObjectReport(t, "WB2OSZ", true, "WB20SZ-15>SMYD00:;WB2OSZ-12*260628z3715.00NJ07145.00WA146.520MHz Toff Hello, world / !T99!")
	tt_user_dump()
}
