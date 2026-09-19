package direwolf

import (
	"os"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- alldigits ---

func Test_alldigits(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"all digits", "12345", true},
		{"single digit", "0", true},
		{"empty string", "", true},
		{"letter present", "123a4", false},
		{"space present", "123 4", false},
		{"symbol present", "123+4", false},
		{"all letters", "ABCDE", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, alldigits(tt.input))
		})
	}
}

// --- alllettersorpm ---

func Test_alllettersorpm(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"all letters", "ABCDE", true},
		{"lowercase letters", "abcde", true},
		{"plus sign only", "+", true},
		{"minus sign only", "-", true},
		{"mixed letters and signs", "A+B-C", true},
		{"empty string", "", true},
		{"digit present", "AB3C", false},
		{"space present", "AB C", false},
		{"symbol present", "AB*C", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, alllettersorpm(tt.input))
		})
	}
}

// --- parse_ll ---

func Test_parse_ll(t *testing.T) {
	tests := []struct {
		name  string
		input string
		which parse_ll_which_e
		want  float64
		delta float64
	}{
		{
			name:  "positive latitude decimal degrees",
			input: "42.36",
			which: LAT,
			want:  42.36,
			delta: 0.0001,
		},
		{
			name:  "negative sign for latitude",
			input: "-42.36",
			which: LAT,
			want:  -42.36,
			delta: 0.0001,
		},
		{
			name:  "N hemisphere suffix",
			input: "42.36N",
			which: LAT,
			want:  42.36,
			delta: 0.0001,
		},
		{
			name:  "S hemisphere suffix negates",
			input: "42.36S",
			which: LAT,
			want:  -42.36,
			delta: 0.0001,
		},
		{
			name:  "E hemisphere suffix longitude",
			input: "71.5E",
			which: LON,
			want:  71.5,
			delta: 0.0001,
		},
		{
			name:  "W hemisphere suffix negates longitude",
			input: "71.5W",
			which: LON,
			want:  -71.5,
			delta: 0.0001,
		},
		{
			name:  "degrees and minutes with caret separator",
			input: "42^30N",
			which: LAT,
			want:  42.5,
			delta: 0.0001,
		},
		{
			name:  "negative sign with S hemisphere double-negates to positive",
			input: "-42.36S",
			which: LAT,
			want:  42.36,
			delta: 0.0001,
		},
		{
			name:  "zero degrees",
			input: "0",
			which: LAT,
			want:  0.0,
			delta: 0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result = parse_ll(tt.input, tt.which, 0)
			assert.InDelta(t, tt.want, result, tt.delta)
		})
	}
}

// --- split ---

func Test_split(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		restOfLine bool
		want       string
	}{
		{"simple token", "hello world", false, "hello"},
		{"quoted token", `"hello world"`, false, "hello world"},
		{"quoted token at end of string", `"hello"`, false, "hello"},
		{"doubled quote inside quotes", `"say ""hi"""`, false, `say "hi"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result = split(tt.input, tt.restOfLine)
			assert.Equal(t, tt.want, result)
		})
	}
}

// --- IsNoCall ---

func Test_IsNoCall(t *testing.T) {
	tests := []struct {
		name     string
		callsign string
		want     bool
	}{
		{"empty string", "", true},
		{"NOCALL uppercase", "NOCALL", true},
		{"nocall lowercase", "nocall", true},
		{"NoCAll mixed case", "NoCAll", true},
		{"N0CALL with zero", "N0CALL", true},
		{"n0call lowercase", "n0call", true},
		{"valid callsign", "W1AW", false},
		{"valid callsign with SSID", "W1AW-9", false},
		{"partial match NOCALLX", "NOCALLX", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsNoCall(tt.callsign))
		})
	}
}

// --- config_init helpers ---

// configs is the set of structures config_init fills in, along with what it
// reported while doing so.
type configs struct {
	audio *audio_s
	digi  *digi_config_s
	cdigi *cdigi_config_s
	tt    *tt_config_s
	igate *igate_config_s
	misc  *misc_config_s

	// output is everything config_init printed.  A handler that rejects a line
	// usually has nothing else to show for it, so this is the only way to tell
	// a line that was reported from one that was quietly ignored.
	output string
}

// parseConfig writes content to a temp config file and runs config_init over it.
func parseConfig(t *testing.T, content string) configs {
	t.Helper()

	var tmpFile, err = os.CreateTemp(t.TempDir(), "direwolf*.conf")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	var c = configs{
		audio:  new(audio_s),
		digi:   new(digi_config_s),
		cdigi:  new(cdigi_config_s),
		tt:     new(tt_config_s),
		igate:  new(igate_config_s),
		misc:   new(misc_config_s),
		output: "",
	}

	c.output = CaptureOutput(t, func() {
		config_init(tmpFile.Name(), c.audio, c.digi, c.cdigi, c.tt, c.igate, c.misc)
	})

	return c
}

// configFromString runs config_init over content and returns the resulting
// audio and misc config structs.
func configFromString(t *testing.T, content string) (*audio_s, *misc_config_s) {
	t.Helper()

	var c = parseConfig(t, content)

	return c.audio, c.misc
}

// --- parse_ll_maybe ---

func Test_parse_ll_maybe(t *testing.T) {
	// Regression test: parse_ll logged the ParseFloat error and carried on with
	// the zero it returns, so "LAT=abc" read as a perfectly good 0 degrees and
	// a beacon went out from Null Island.
	// Regression test: parse_ll indexed str[0] before looking at its length, so
	// "LAT=" in a beacon line panicked rather than being rejected.
	t.Run("an empty coordinate is Nothing", func(t *testing.T) {
		assert.Equal(t, maybe.Nothing[float64](), parse_ll_maybe("", LAT, 0))
	})

	t.Run("a sign on its own is Nothing", func(t *testing.T) {
		assert.Equal(t, maybe.Nothing[float64](), parse_ll_maybe("-", LON, 0))
	})

	t.Run("unreadable degrees are Nothing", func(t *testing.T) {
		assert.Equal(t, maybe.Nothing[float64](), parse_ll_maybe("abc", LAT, 0))
	})

	t.Run("unreadable minutes are Nothing", func(t *testing.T) {
		assert.Equal(t, maybe.Nothing[float64](), parse_ll_maybe("42^ab", LAT, 0))
	})

	t.Run("a non-finite coordinate is Nothing", func(t *testing.T) {
		assert.Equal(t, maybe.Nothing[float64](), parse_ll_maybe("NaN^0", LAT, 0))
	})

	// Regression test: an out-of-range coordinate only logged and was returned
	// as though it were usable, so a beacon with LAT=200 passed the "latitude
	// and longitude are required" check and EncodePosition clamped it to
	// "!9000.00N" - the station transmitted from the North Pole.
	t.Run("an out-of-range latitude is Nothing", func(t *testing.T) {
		assert.Equal(t, maybe.Nothing[float64](), parse_ll_maybe("200", LAT, 0))
	})

	t.Run("an out-of-range longitude is Nothing", func(t *testing.T) {
		assert.Equal(t, maybe.Nothing[float64](), parse_ll_maybe("181W", LON, 0))
	})

	t.Run("the limits themselves are Just", func(t *testing.T) {
		assert.InDelta(t, 90.0, maybe.FromJust(parse_ll_maybe("90N", LAT, 0)), 0.0001)
		assert.InDelta(t, -180.0, maybe.FromJust(parse_ll_maybe("180W", LON, 0)), 0.0001)
	})

	t.Run("a readable coordinate is Just", func(t *testing.T) {
		assert.InDelta(t, -71.5, maybe.FromJust(parse_ll_maybe("71.5W", LON, 0)), 0.0001)
	})
}

// --- config_init beacon LAT and LONG ---

func Test_config_init_beacon_empty_lat(t *testing.T) {
	t.Run("LAT= with no value does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			var _, misc = configFromString(t, "MYCALL Q1TEST\nPBEACON LAT= LONG=71W\n")
			assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].lat)
		})
	})
}

func Test_config_init_beacon_unreadable_interval(t *testing.T) {
	// Regression test: parse_interval ignored the Atoi error and returned the
	// zero alongside it, so EVERY=abc gave an interval of 0 seconds.  With a
	// SLOT configured that reached IS_GOOD, which divides 3600 by it and
	// brought the whole program down on startup; without one, the next
	// transmission time never advanced.
	var _, misc = configFromString(t, "MYCALL Q1TEST\nPBEACON LAT=42N LONG=71W SLOT=1 EVERY=abc\n")

	require.Equal(t, 1, misc.num_beacons)
	assert.Equal(t, 600, misc.beacon[0].every)

	var modem = new(audio_s)
	modem.chan_medium[0] = MEDIUM_RADIO
	modem.mycall[0] = "Q1TEST"

	assert.NotPanics(t, func() {
		NewBeaconService(modem, misc, new(igate_config_s))
	})
}

func Test_config_init_beacon_out_of_range_interval(t *testing.T) {
	t.Run("EVERY=0 keeps the default", func(t *testing.T) {
		var _, misc = configFromString(t, "MYCALL Q1TEST\nPBEACON LAT=42N LONG=71W EVERY=0\n")
		assert.Equal(t, 600, misc.beacon[0].every)
	})

	t.Run("a readable interval is stored", func(t *testing.T) {
		var _, misc = configFromString(t, "MYCALL Q1TEST\nPBEACON LAT=42N LONG=71W EVERY=2:30 DELAY=0:05\n")
		assert.Equal(t, 150, misc.beacon[0].every)
		assert.Equal(t, 5, misc.beacon[0].delay)
	})
}

func Test_config_init_beacon_non_finite_numbers(t *testing.T) {
	// Regression test: ParseFloat happily reads "NaN" and "Inf", so a beacon
	// option could hold a value no arithmetic survives.  int(NaN) is the
	// smallest int64, which frequency_spec put on the air as
	// "T-9223372036854775808".
	var config = "MYCALL Q1TEST\n" +
		"PBEACON LAT=NaN^0 LONG=71W FREQ=NaN TONE=NaN OFFSET=Inf ALT=-Inf\n"

	var _, misc = configFromString(t, config)

	require.Equal(t, 1, misc.num_beacons)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].lat)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].freq)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].tone)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].offset)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].alt_m)

	assert.Empty(t, frequency_spec(misc.beacon[0].freq, misc.beacon[0].tone, misc.beacon[0].offset))
}

func Test_config_init_beacon_out_of_range_lat(t *testing.T) {
	var _, misc = configFromString(t, "MYCALL Q1TEST\nPBEACON LAT=200 LONG=71W\n")

	require.Equal(t, 1, misc.num_beacons)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].lat)

	// With no position the beacon is dropped rather than transmitted from
	// wherever the clamp lands.
	var modem = new(audio_s)
	modem.chan_medium[0] = MEDIUM_RADIO
	modem.mycall[0] = "Q1TEST"

	var bs = NewBeaconService(modem, misc, new(igate_config_s))
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_config_init_beacon_unparseable_lat_long(t *testing.T) {
	// An unreadable coordinate must leave the beacon without a position, so
	// that NewBeaconService rejects it rather than transmitting 0 degrees.
	var _, misc = configFromString(t, "MYCALL Q1TEST\nPBEACON LAT=abc LONG=71W\n")

	require.Equal(t, 1, misc.num_beacons)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].lat)
	assert.InDelta(t, -71.0, maybe.FromJust(misc.beacon[0].lon), 0.0001)
}

// --- config_init MYCALL directive ---

func Test_config_init_mycall(t *testing.T) {
	t.Run("basic callsign stored on channel 0", func(t *testing.T) {
		var cfg, _ = configFromString(t, "MYCALL Q1TEST\n")
		assert.Equal(t, "Q1TEST", cfg.mycall[0])
	})

	t.Run("lowercase input is silently uppercased", func(t *testing.T) {
		var cfg, _ = configFromString(t, "MYCALL q1test\n")
		assert.Equal(t, "Q1TEST", cfg.mycall[0])
	})

	t.Run("callsign with SSID", func(t *testing.T) {
		var cfg, _ = configFromString(t, "MYCALL Q1TEST-9\n")
		assert.Equal(t, "Q1TEST-9", cfg.mycall[0])
	})

	t.Run("invalid callsign leaves all channels as NOCALL", func(t *testing.T) {
		var cfg, _ = configFromString(t, "MYCALL !INVALID!\n")
		assert.True(t, IsNoCall(cfg.mycall[0]))
	})

	t.Run("MYCALL propagates to all unset channels", func(t *testing.T) {
		var cfg, _ = configFromString(t, "MYCALL Q1TEST\n")
		// Channel 0 and all other channels that were not explicitly set should share it.
		for c := range MAX_TOTAL_CHANS {
			assert.Equal(t, "Q1TEST", cfg.mycall[c],
				"expected Q1TEST on channel %d", c)
		}
	})

	t.Run("per-channel MYCALL does not overwrite explicitly set channel", func(t *testing.T) {
		// MYCALL Q1TEST sets all channels; then CHANNEL 1 + MYCALL Q2TEST
		// should overwrite channel 1 but leave channel 0 as Q1TEST.
		var cfg, _ = configFromString(t,
			"ADEVICE hw:0,0\n"+
				"ARATE 44100\n"+
				"MYCALL Q1TEST\n"+
				"CHANNEL 1\n"+
				"MYCALL Q2TEST\n",
		)
		assert.Equal(t, "Q1TEST", cfg.mycall[0])
		assert.Equal(t, "Q2TEST", cfg.mycall[1])
	})
}

// --- config_init case-insensitive keyword dispatch ---

func Test_config_init_keyword_case_insensitive(t *testing.T) {
	t.Run("lowercase directive name works", func(t *testing.T) {
		var cfg, _ = configFromString(t, "mycall Q1TEST\n")
		assert.Equal(t, "Q1TEST", cfg.mycall[0])
	})

	t.Run("mixed-case directive name works", func(t *testing.T) {
		var cfg, _ = configFromString(t, "MyCall Q1TEST\n")
		assert.Equal(t, "Q1TEST", cfg.mycall[0])
	})
}

// --- config_init TXDELAY directive ---

func Test_config_init_txdelay(t *testing.T) {
	t.Run("valid value stored", func(t *testing.T) {
		var cfg, _ = configFromString(t, "TXDELAY 50\n")
		assert.Equal(t, 50, cfg.achan[0].txdelay)
	})

	t.Run("out-of-range value falls back to default", func(t *testing.T) {
		var cfg, _ = configFromString(t, "TXDELAY 999\n")
		assert.Equal(t, DEFAULT_TXDELAY, cfg.achan[0].txdelay)
	})

	// Regression test: the value went through an Atoi whose error was ignored,
	// so "TXDELAY abc" read as 0 - in range, and a transmit delay too short for
	// another station to hear - rather than being rejected.
	t.Run("unreadable value leaves the configured one alone", func(t *testing.T) {
		var cfg, _ = configFromString(t, "TXDELAY 20\nTXDELAY abc\n")
		assert.Equal(t, 20, cfg.achan[0].txdelay)
	})
}

// --- config_init SLOTTIME directive ---

func Test_config_init_slottime(t *testing.T) {
	t.Run("valid value stored", func(t *testing.T) {
		var cfg, _ = configFromString(t, "SLOTTIME 20\n")
		assert.Equal(t, 20, cfg.achan[0].slottime)
	})

	t.Run("out-of-range value falls back to default", func(t *testing.T) {
		// 0 is outside the accepted range 5..49
		var cfg, _ = configFromString(t, "SLOTTIME 0\n")
		assert.Equal(t, DEFAULT_SLOTTIME, cfg.achan[0].slottime)
	})
}

// --- config_init FRACK directive ---

func Test_config_init_frack(t *testing.T) {
	t.Run("valid value stored", func(t *testing.T) {
		var _, misc = configFromString(t, "FRACK 5\n")
		assert.Equal(t, 5, misc.frack)
	})

	t.Run("out-of-range value keeps default", func(t *testing.T) {
		var _, misc = configFromString(t, "FRACK 999\n")
		assert.Equal(t, AX25_T1V_FRACK_DEFAULT, misc.frack)
	})
}

// --- config_init ADEVICE directive ---

func Test_config_init_adevice(t *testing.T) {
	t.Run("single arg sets both in and out to same device", func(t *testing.T) {
		var cfg, _ = configFromString(t, "ADEVICE hw:0,0\n")
		assert.Equal(t, "hw:0,0", cfg.adev[0].adevice_in)
		assert.Equal(t, "hw:0,0", cfg.adev[0].adevice_out)
		// One name is a source, not a choice of transmit device.
		assert.False(t, cfg.adev[0].adevice_out_specified)
	})

	t.Run("two args set in and out independently", func(t *testing.T) {
		var cfg, _ = configFromString(t, "ADEVICE hw:0,0 hw:1,0\n")
		assert.Equal(t, "hw:0,0", cfg.adev[0].adevice_in)
		assert.Equal(t, "hw:1,0", cfg.adev[0].adevice_out)
		assert.True(t, cfg.adev[0].adevice_out_specified)
	})

	t.Run("PAODEVICE names a transmit device", func(t *testing.T) {
		var cfg, _ = configFromString(t, "PAODEVICE Some Sound Card\n")
		assert.Equal(t, "Some Sound Card", cfg.adev[0].adevice_out)
		assert.True(t, cfg.adev[0].adevice_out_specified)
	})

	t.Run("no ADEVICE at all leaves the default, which is no choice either", func(t *testing.T) {
		var cfg, _ = configFromString(t, "MYCALL Q1TEST\n")
		assert.Equal(t, DEFAULT_ADEVICE, cfg.adev[0].adevice_out)
		assert.False(t, cfg.adev[0].adevice_out_specified)
	})

	t.Run("ADEVICE1 numeric suffix sets device 1", func(t *testing.T) {
		var cfg, _ = configFromString(t, "ADEVICE hw:0,0\nADEVICE1 hw:1,0\n")
		assert.Equal(t, "hw:1,0", cfg.adev[1].adevice_in)
		assert.Equal(t, "hw:1,0", cfg.adev[1].adevice_out)
	})

	t.Run("ADEVICE1 = n mapping syntax is rejected and leaves device 1 undefined", func(t *testing.T) {
		var cfg, _ = configFromString(t, "ADEVICE1 = 0\n")
		// The = (copy-from) mapping syntax is unimplemented; the handler must
		// return early without marking device 1 as defined or assigning any
		// channel medium for its first channel (channel 2 = ADEVFIRSTCHAN(1)).
		assert.Equal(t, 0, cfg.adev[1].defined)
		assert.Equal(t, MEDIUM_NONE, cfg.chan_medium[ADEVFIRSTCHAN(1)])
	})
}

// --- config_init CHANNEL directive ---

func Test_config_init_channel(t *testing.T) {
	t.Run("CHANNEL 1 routes subsequent TXDELAY to channel 1", func(t *testing.T) {
		// Need stereo ADEVICE so channel 1 is valid.
		var cfg, _ = configFromString(t,
			"ADEVICE hw:0,0\n"+
				"ARATE 44100\n"+
				"CHANNEL 1\n"+
				"TXDELAY 42\n",
		)
		// Channel 0 should have the default; channel 1 should have 42.
		assert.Equal(t, DEFAULT_TXDELAY, cfg.achan[0].txdelay)
		assert.Equal(t, 42, cfg.achan[1].txdelay)
	})
}

// --- config_init MODEM directive ---

func Test_config_init_modem_directive(t *testing.T) {
	tests := []struct {
		name          string
		configContent string
		wantBaud      int
		wantModemType modem_t
	}{
		{
			name:          "1200 baud AFSK",
			configContent: "MODEM 1200\n",
			wantBaud:      1200,
			wantModemType: MODEM_AFSK,
		},
		{
			name:          "9600 baud G3RUH implicit",
			configContent: "MODEM 9600\n",
			wantBaud:      9600,
			wantModemType: MODEM_SCRAMBLE,
		},
		{
			name:          "9600 baud G3RUH explicit option",
			configContent: "MODEM 9600 g3ruh\n",
			wantBaud:      9600,
			wantModemType: MODEM_SCRAMBLE,
		},
		{
			name:          "300 baud HF AFSK",
			configContent: "MODEM 300\n",
			wantBaud:      300,
			wantModemType: MODEM_AFSK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp(t.TempDir(), "direwolf*.conf")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.configContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			var audioConfig = new(audio_s)
			var digiConfig digi_config_s
			var cdigiConfig cdigi_config_s
			var ttConfig tt_config_s
			var igateConfig igate_config_s
			var miscConfig misc_config_s

			config_init(tmpFile.Name(), audioConfig, &digiConfig, &cdigiConfig,
				&ttConfig, &igateConfig, &miscConfig)

			assert.Equal(t, tt.wantBaud, audioConfig.achan[0].baud)
			assert.Equal(t, tt.wantModemType, audioConfig.achan[0].modem_type)
		})
	}
}

// --- config_init FILTER / CFILTER syntax validation ---

func Test_config_init_filter_syntax_validation(t *testing.T) {
	var tests = []struct {
		name          string
		configContent string
		wantSet       bool
	}{
		{
			name:          "valid FILTER expression is stored",
			configContent: "FILTER 0 0 t/p\n",
			wantSet:       true,
		},
		{
			name:          "FILTER with unrecognized filter type is rejected",
			configContent: "FILTER 0 0 x/\n",
			wantSet:       false,
		},
		{
			name:          "FILTER with unbalanced parentheses is rejected",
			configContent: "FILTER 0 0 t/w & ( t/w | t/w \n",
			wantSet:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tmpFile, err = os.CreateTemp(t.TempDir(), "direwolf*.conf")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.configContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			var audioConfig = new(audio_s)
			var digiConfig digi_config_s
			var cdigiConfig cdigi_config_s
			var ttConfig tt_config_s
			var igateConfig igate_config_s
			var miscConfig misc_config_s

			config_init(tmpFile.Name(), audioConfig, &digiConfig, &cdigiConfig,
				&ttConfig, &igateConfig, &miscConfig)

			if tt.wantSet {
				assert.NotEmpty(t, digiConfig.filter_str[0][0])
			} else {
				assert.Empty(t, digiConfig.filter_str[0][0])
			}
		})
	}
}

func Test_config_init_cfilter_syntax_validation(t *testing.T) {
	var tests = []struct {
		name          string
		configContent string
		wantSet       bool
	}{
		{
			name:          "valid CFILTER expression is stored",
			configContent: "CFILTER 0 0 b/Q1TEST\n",
			wantSet:       true,
		},
		{
			name:          "CFILTER with a filter type only valid for APRS is rejected",
			configContent: "CFILTER 0 0 t/p\n",
			wantSet:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tmpFile, err = os.CreateTemp(t.TempDir(), "direwolf*.conf")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.configContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			var audioConfig = new(audio_s)
			var digiConfig digi_config_s
			var cdigiConfig cdigi_config_s
			var ttConfig tt_config_s
			var igateConfig igate_config_s
			var miscConfig misc_config_s

			config_init(tmpFile.Name(), audioConfig, &digiConfig, &cdigiConfig,
				&ttConfig, &igateConfig, &miscConfig)

			if tt.wantSet {
				assert.NotEmpty(t, cdigiConfig.cfilter_str[0][0])
			} else {
				assert.Empty(t, cdigiConfig.cfilter_str[0][0])
			}
		})
	}
}

// --- config_init ADEVICE multi-digit suffix ---

func Test_config_init_adevice_multi_digit_suffix(t *testing.T) {
	t.Run("ADEVICE11 two-digit suffix is parsed as 11 not 1", func(t *testing.T) {
		// Regression test: handleADEVICE used string(ps.keyword[7]) which reads
		// only one byte, so "ADEVICE11" would parse suffix "1" instead of "11".
		// With the fix, suffix "11" is out of range and must be reported as an error
		// rather than silently configuring device 1.
		assert.NotPanics(t, func() {
			configFromString(t, "ADEVICE11 hw:0,0\n")
		})
		// Device 1 must remain undefined (suffix 11 is out of range).
		var cfg, _ = configFromString(t, "ADEVICE11 hw:0,0\n")
		assert.Equal(t, 0, cfg.adev[1].defined)
	})
}

// --- config_init CHANNEL non-numeric ---

func Test_config_init_channel_non_numeric(t *testing.T) {
	t.Run("CHANNEL with non-numeric value is rejected", func(t *testing.T) {
		// Regression test: CHANNEL used strconv.Atoi with ignored error; non-numeric
		// input would silently treat the channel as 0.  Now it must log an error and
		// leave the channel unchanged.
		assert.NotPanics(t, func() {
			configFromString(t, "CHANNEL notanumber\n")
		})
	})
}

// --- config_init AGWPORT directive ---

func Test_config_init_agwport(t *testing.T) {
	t.Run("AGWPORT with non-numeric value is rejected without panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			configFromString(t, "AGWPORT notanumber\n")
		})
	})

	t.Run("AGWPORT with valid port sets agwpe_port", func(t *testing.T) {
		var _, misc = configFromString(t, "AGWPORT 8000\n")
		assert.Equal(t, 8000, misc.agwpe_port)
	})
}

// --- config_init AGWLOGIN directive ---

func Test_config_init_agwlogin(t *testing.T) {
	t.Run("AGWLOGIN sets user name and password", func(t *testing.T) {
		var _, misc = configFromString(t, "AGWLOGIN Q1TEST hunter2\n")
		require.Len(t, misc.agwpe_logins, 1)
		assert.Equal(t, "Q1TEST", misc.agwpe_logins[0].user)
		assert.Equal(t, "hunter2", misc.agwpe_logins[0].password)
	})

	t.Run("AGWLOGIN keeps spaces within a quoted password", func(t *testing.T) {
		var _, misc = configFromString(t, "AGWLOGIN Q1TEST \"correct horse battery staple\"\n")
		require.Len(t, misc.agwpe_logins, 1)
		assert.Equal(t, "correct horse battery staple", misc.agwpe_logins[0].password)
	})

	// AGWPE keeps a list of users, so each line adds to it rather than
	// replacing what came before.
	t.Run("repeated AGWLOGIN accumulates credentials", func(t *testing.T) {
		var _, misc = configFromString(t,
			"AGWLOGIN Q1TEST hunter2\nAGWLOGIN Q2TEST \"correct horse\"\n")
		require.Len(t, misc.agwpe_logins, 2)
		assert.Equal(t, "Q1TEST", misc.agwpe_logins[0].user)
		assert.Equal(t, "hunter2", misc.agwpe_logins[0].password)
		assert.Equal(t, "Q2TEST", misc.agwpe_logins[1].user)
		assert.Equal(t, "correct horse", misc.agwpe_logins[1].password)
	})

	t.Run("no AGWLOGIN means no login required", func(t *testing.T) {
		var _, misc = configFromString(t, "AGWPORT 8000\n")
		assert.Empty(t, misc.agwpe_logins)
	})

	t.Run("AGWLOGIN without a password is rejected", func(t *testing.T) {
		var _, misc = configFromString(t, "AGWLOGIN Q1TEST\n")
		assert.Empty(t, misc.agwpe_logins)
	})

	t.Run("AGWLOGIN without a user name is rejected", func(t *testing.T) {
		var _, misc = configFromString(t, "AGWLOGIN\n")
		assert.Empty(t, misc.agwpe_logins)
	})

	t.Run("AGWLOGIN too long to fit in a login frame is rejected", func(t *testing.T) {
		var long = strings.Repeat("x", AGW_LOGIN_FIELD_LEN+1)
		var _, misc = configFromString(t, "AGWLOGIN Q1TEST "+long+"\n")
		assert.Empty(t, misc.agwpe_logins)
	})

	// A rejected line must not take a good one down with it.
	t.Run("a rejected AGWLOGIN leaves earlier ones in place", func(t *testing.T) {
		var _, misc = configFromString(t, "AGWLOGIN Q1TEST hunter2\nAGWLOGIN Q2TEST\n")
		require.Len(t, misc.agwpe_logins, 1)
		assert.Equal(t, "Q1TEST", misc.agwpe_logins[0].user)
	})
}

// --- config_init KISSPORT directive ---

func Test_config_init_kissport(t *testing.T) {
	t.Run("KISSPORT with non-numeric value is rejected without panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			configFromString(t, "KISSPORT notanumber\n")
		})
	})
}

// --- config_init MODEM all-options success ---

func Test_config_init_modem_returns_success(t *testing.T) {
	t.Run("MODEM with all options parsed successfully does not block subsequent directives", func(t *testing.T) {
		// Regression test: handleMODEM returned true (error/stop) when all options
		// were parsed and split returned "".  This caused subsequent directives like
		// MYCALL to be skipped.  The correct return when successful is false.
		var cfg, _ = configFromString(t, "MODEM 1200\nMYCALL Q1TEST\n")
		assert.Equal(t, "Q1TEST", cfg.mycall[0])
	})
}

// --- config_init DNSSD directive ---

func Test_config_init_dnssd(t *testing.T) {
	t.Run("DNSSD with non-numeric value is rejected and disabled", func(t *testing.T) {
		var _, misc = configFromString(t, "DNSSD notanumber\n")
		assert.False(t, misc.dns_sd_enabled)
	})

	t.Run("DNSSD 1 enables dns-sd", func(t *testing.T) {
		var _, misc = configFromString(t, "DNSSD 1\n")
		assert.True(t, misc.dns_sd_enabled)
	})

	t.Run("DNSSD 0 disables dns-sd", func(t *testing.T) {
		var _, misc = configFromString(t, "DNSSD 0\n")
		assert.False(t, misc.dns_sd_enabled)
	})
}

// --- config_init SENDTO non-numeric channel suffix ---

func Test_config_init_beacon_sendto_non_numeric(t *testing.T) {
	t.Run("SENDTO=rXYZ with non-numeric channel suffix is rejected", func(t *testing.T) {
		assert.NotPanics(t, func() {
			configFromString(t, "PBEACON SENDTO=rXYZ\n")
		})
	})

	t.Run("SENDTO=tXYZ with non-numeric channel suffix is rejected", func(t *testing.T) {
		assert.NotPanics(t, func() {
			configFromString(t, "PBEACON SENDTO=tXYZ\n")
		})
	})

	t.Run("SENDTO=XYZ with non-numeric value is rejected", func(t *testing.T) {
		assert.NotPanics(t, func() {
			configFromString(t, "PBEACON SENDTO=XYZ\n")
		})
	})
}

// --- config_init SENDTO beacon option (empty value) ---

func Test_config_init_beacon_sendto_empty(t *testing.T) {
	t.Run("SENDTO= with empty value does not panic", func(t *testing.T) {
		// Regression test: beacon_options accessed value[0] without first checking
		// len(value), which would panic with an index out of range when value is empty
		// (i.e. SENDTO= with nothing after the equals sign).
		assert.NotPanics(t, func() {
			configFromString(t, "PBEACON SENDTO=\n")
		})
	})
}

// --- config_init beacon numeric options ---

func Test_config_init_beacon_unparseable_numbers(t *testing.T) {
	// Regression test: the numeric beacon options ignored the ParseFloat error
	// and stored the zero it returns, so a typo became a value rather than
	// nothing at all.  TONE=0 goes out as "Toff", OFFSET=0 as "+000" and ALT=0
	// as "/A=000000", so "TONE=abc" transmitted a tone setting nobody asked
	// for.
	var config = "MYCALL Q1TEST\nPBEACON LAT=42N LONG=71W FREQ=abc TONE=def OFFSET=ghi ALT=jkl\n"

	var _, misc = configFromString(t, config)

	require.Equal(t, 1, misc.num_beacons)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].freq)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].tone)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].offset)
	assert.Equal(t, maybe.Nothing[float64](), misc.beacon[0].alt_m)

	assert.Empty(t, frequency_spec(misc.beacon[0].freq, misc.beacon[0].tone, misc.beacon[0].offset))
}

func Test_config_init_beacon_numbers_with_units(t *testing.T) {
	var _, misc = configFromString(t, "MYCALL Q1TEST\nPBEACON LAT=42N LONG=71W ALT=100foot FREQ=146.52 TONE=100\n")

	require.Equal(t, 1, misc.num_beacons)
	assert.InDelta(t, 30.48, maybe.FromJust(misc.beacon[0].alt_m), 0.001)
	assert.InDelta(t, 146.52, maybe.FromJust(misc.beacon[0].freq), 0.001)
	assert.InDelta(t, 100.0, maybe.FromJust(misc.beacon[0].tone), 0.001)
}

// --- config_init beacon line rejected part way through ---

func Test_config_init_beacon_rejected_line_does_not_leak(t *testing.T) {
	// Regression test: a beacon line whose options don't parse does not count
	// towards num_beacons, so the next beacon line is parsed into the same
	// array slot.  beacon_options reset only some of the fields, so the good
	// line inherited the rest - here the rejected line's COMMENT and POWER.
	var config = "MYCALL Q1TEST\n" +
		"PBEACON LAT=42N LONG=71W COMMENT=\"leaked\" POWER=50 BOGUS=1\n" +
		"PBEACON LAT=43N LONG=72W\n"

	var _, misc = configFromString(t, config)

	require.Equal(t, 1, misc.num_beacons)
	assert.Empty(t, misc.beacon[0].comment)
	assert.Zero(t, misc.beacon[0].power)
}

// --- config_init PBEACON directive (no options) ---

func Test_config_init_pbeacon_no_options(t *testing.T) {
	t.Run("PBEACON with no options does not panic", func(t *testing.T) {
		// Regression test: handleXBEACON used ps.text[len("xBEACON")+1:] which
		// would panic with an index out of range when the line had no trailing
		// space or options (e.g. just "PBEACON").
		assert.NotPanics(t, func() {
			configFromString(t, "PBEACON\n")
		})
	})
}

// --- config_init IL2PVERSION directive ---

func Test_config_init_il2pversion(t *testing.T) {
	t.Run("0.6 by default", func(t *testing.T) {
		var audio, _ = configFromString(t, "")
		assert.Equal(t, IL2P_VERSION_0_6, audio.achan[0].il2p_version)
	})

	t.Run("0.4 stored", func(t *testing.T) {
		var audio, _ = configFromString(t, "CHANNEL 0\nIL2PVERSION 0.4\n")
		assert.Equal(t, IL2P_VERSION_0_4, audio.achan[0].il2p_version)
	})

	t.Run("0.6 stored", func(t *testing.T) {
		var audio, _ = configFromString(t, "CHANNEL 0\nIL2PVERSION 0.6\n")
		assert.Equal(t, IL2P_VERSION_0_6, audio.achan[0].il2p_version)
	})

	t.Run("compat stored", func(t *testing.T) {
		var audio, _ = configFromString(t, "CHANNEL 0\nIL2PVERSION compat\n")
		assert.Equal(t, IL2P_VERSION_COMPAT, audio.achan[0].il2p_version)
	})

	t.Run("unrecognised version leaves the default", func(t *testing.T) {
		var audio, _ = configFromString(t, "CHANNEL 0\nIL2PVERSION 0.5\n")
		assert.Equal(t, IL2P_VERSION_0_6, audio.achan[0].il2p_version)
	})

	t.Run("missing version leaves the default", func(t *testing.T) {
		var audio, _ = configFromString(t, "CHANNEL 0\nIL2PVERSION\n")
		assert.Equal(t, IL2P_VERSION_0_6, audio.achan[0].il2p_version)
	})
}

// --- config_init METRICSPORT directive ---

func Test_config_init_metricsport(t *testing.T) {
	t.Run("valid value stored", func(t *testing.T) {
		var _, misc = configFromString(t, "METRICSPORT 9099\n")
		assert.Equal(t, 9099, misc.metrics_port)
	})

	t.Run("zero disables", func(t *testing.T) {
		var _, misc = configFromString(t, "METRICSPORT 0\n")
		assert.Equal(t, 0, misc.metrics_port)
	})

	t.Run("disabled by default", func(t *testing.T) {
		var _, misc = configFromString(t, "")
		assert.Equal(t, 0, misc.metrics_port)
	})

	t.Run("out-of-range value disables", func(t *testing.T) {
		var _, misc = configFromString(t, "METRICSPORT 99999\n")
		assert.Equal(t, 0, misc.metrics_port)
	})

	t.Run("non-numeric value is rejected", func(t *testing.T) {
		var _, misc = configFromString(t, "METRICSPORT nine\n")
		assert.Equal(t, 0, misc.metrics_port)
	})

	// Matches handleAGWPORT: trailing junk is a typo, and accepting it silently
	// would leave the station behaving in a way its config does not describe.
	t.Run("trailing token is rejected", func(t *testing.T) {
		var _, misc = configFromString(t, "METRICSPORT 9099 junk\n")
		assert.Equal(t, 0, misc.metrics_port)
	})
}

// --- config_init FIX_BITS directive ---

func Test_config_init_fix_bits(t *testing.T) {
	t.Run("valid level stored", func(t *testing.T) {
		var cfg, _ = configFromString(t, "FIX_BITS 1\n")
		assert.Equal(t, BitFixSingle, cfg.achan[0].fix_bits)
		assert.False(t, cfg.achan[0].passall)
	})

	t.Run("highest level stored", func(t *testing.T) {
		var cfg, _ = configFromString(t, "FIX_BITS 4\n")
		assert.Equal(t, BitFixLevelHighest, cfg.achan[0].fix_bits)
	})

	// PASSALL is not a level of effort, so the value it decodes as is not a
	// level FIX_BITS accepts.
	t.Run("passall value falls back to default", func(t *testing.T) {
		var cfg, _ = configFromString(t, "FIX_BITS 5\n")
		assert.Equal(t, DEFAULT_FIX_BITS, cfg.achan[0].fix_bits)
		assert.False(t, cfg.achan[0].passall)
	})

	t.Run("PASSALL keyword sets passall independently of the level", func(t *testing.T) {
		var cfg, _ = configFromString(t, "FIX_BITS 0 PASSALL\n")
		assert.Equal(t, BitFixNone, cfg.achan[0].fix_bits)
		assert.True(t, cfg.achan[0].passall)
	})

	t.Run("passall off by default", func(t *testing.T) {
		var cfg, _ = configFromString(t, "")
		assert.Equal(t, DEFAULT_FIX_BITS, cfg.achan[0].fix_bits)
		assert.False(t, cfg.achan[0].passall)
	})
}

// --- config directive coverage ---
//
// The config file is the whole of the user interface, and a parse bug in it is
// silent by nature, so every keyword in configHandlers is expected to have
// tests: that a valid line sets what it claims to set, that a malformed or
// out-of-range one is rejected rather than quietly wrapped or clamped, and that
// a rejected line neither mutates the configuration nor leaks into the next
// line.
//
// directiveTests holds them as a table, Test_config_directives runs it, and
// Test_config_directive_coverage fails for a keyword with neither an entry here
// nor a test of its own, so a newly added handler cannot arrive untested.

// directiveCase is one configuration to parse and what it should leave behind.
type directiveCase struct {
	name   string
	config string
	check  func(a *assert.Assertions, c configs)
}

func directiveTests() map[string][]directiveCase {
	return map[string][]directiveCase{
		"ACHANNELS": {
			{
				name:   "two channels makes the second one a radio channel",
				config: "ACHANNELS 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(2, c.audio.adev[0].num_channels)
					a.Equal(MEDIUM_RADIO, c.audio.chan_medium[0])
					a.Equal(MEDIUM_RADIO, c.audio.chan_medium[1])
				},
			},
			{
				name:   "one channel leaves the second one unused",
				config: "ACHANNELS 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.audio.adev[0].num_channels)
					a.Equal(MEDIUM_RADIO, c.audio.chan_medium[0])
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[1])
				},
			},
			{
				name:   "it applies to the audio device the line follows",
				config: "ADEVICE1 hw:1,0\nACHANNELS 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_NUM_CHANNELS, c.audio.adev[0].num_channels)
					a.Equal(2, c.audio.adev[1].num_channels)
					a.Equal(MEDIUM_RADIO, c.audio.chan_medium[ADEVFIRSTCHAN(1)])
					a.Equal(MEDIUM_RADIO, c.audio.chan_medium[ADEVFIRSTCHAN(1)+1])
				},
			},
			{
				name:   "a count other than 1 or 2 keeps the default",
				config: "ACHANNELS 3\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_NUM_CHANNELS, c.audio.adev[0].num_channels)
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[1])
				},
			},
			{
				name:   "an unreadable count keeps the default",
				config: "ACHANNELS two\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_NUM_CHANNELS, c.audio.adev[0].num_channels)
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[1])
				},
			},
			{
				name:   "a missing count keeps the default and does not eat the next line",
				config: "ACHANNELS\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_NUM_CHANNELS, c.audio.adev[0].num_channels)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		// Connected mode digipeating needs an internal modem at both ends, so a
		// network TNC channel is not allowed here even though DIGIPEAT allows one.
		"CDIGIPEAT": {
			{
				name:   "a valid line enables connected mode digipeating",
				config: "MYCALL Q1TEST\nACHANNELS 2\nCDIGIPEAT 0 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.cdigi.enabled[0][1])
					a.False(c.cdigi.has_alias[0][1])
				},
			},
			{
				name:   "nothing is digipeated by default",
				config: "MYCALL Q1TEST\nACHANNELS 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.cdigi.enabled[0][1])
				},
			},
			{
				name:   "an alias pattern is stored",
				config: "MYCALL Q1TEST\nACHANNELS 2\nCDIGIPEAT 0 1 ^Q[12]TEST$\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.cdigi.enabled[0][1])
					a.True(c.cdigi.has_alias[0][1])
					a.NotNil(c.cdigi.alias[0][1])
				},
			},
			{
				name:   "an unusable alias pattern is rejected and nothing is enabled",
				config: "MYCALL Q1TEST\nACHANNELS 2\nCDIGIPEAT 0 1 ^Q([12]TEST$\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.cdigi.enabled[0][1])
					a.Contains(c.output, "Invalid alias matching pattern")
				},
			},
			{
				name:   "a non-numeric channel is rejected",
				config: "MYCALL Q1TEST\nACHANNELS 2\nCDIGIPEAT zero 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.cdigi.enabled[0][1])
					a.Contains(c.output, "is not allowed for FROM-channel")
				},
			},
			{
				name:   "a network TNC channel is not allowed",
				config: "MYCALL Q1TEST\nNCHANNEL 6 localhost 8001\nCDIGIPEAT 0 6\n",
				check: func(a *assert.Assertions, c configs) {
					a.Contains(c.output, "TO-channel must be in range")
				},
			},
			{
				name:   "anything after the alias is reported",
				config: "MYCALL Q1TEST\nACHANNELS 2\nCDIGIPEAT 0 1 ^Q[12]TEST$ extra\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.cdigi.enabled[0][1])
					a.Contains(c.output, "where end of line was expected")
				},
			},
			{
				name:   "digipeating to a channel with no callsign is switched off again",
				config: "ACHANNELS 2\nCDIGIPEAT 0 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.cdigi.enabled[0][1])
					a.Contains(c.output, "MYCALL must be set for transmit channel")
				},
			},
			{
				name:   "a missing TO-channel does not eat the next line",
				config: "ACHANNELS 2\nCDIGIPEAT 0\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.cdigi.enabled[0][1])
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"CDIGIPEATER": {
			{
				name:   "the longer name is the same handler as CDIGIPEAT",
				config: "MYCALL Q1TEST\nACHANNELS 2\nCDIGIPEATER 0 1 ^Q[12]TEST$\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.cdigi.enabled[0][1])
					a.True(c.cdigi.has_alias[0][1])
				},
			},
		},
		"CON": {
			{
				name:   "it configures the connected indicator output",
				config: "CON /dev/ttyS0 DTR\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_SERIAL, c.audio.achan[0].octrl[OCTYPE_CON].ptt_method)
					a.Equal(PTT_LINE_DTR, c.audio.achan[0].octrl[OCTYPE_CON].ptt_line)
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
				},
			},
			{
				name:   "CM108 is rejected for it as well",
				config: "CON CM108 /dev/hidraw9\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_CON].ptt_method)
					a.Contains(c.output, "only valid for PTT, not CON")
				},
			},
			{
				name:   "a missing device does not eat the next line",
				config: "CON\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_CON].ptt_method)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"DCD": {
			{
				name:   "it configures the data carrier detect output, not PTT",
				config: "DCD /dev/ttyS0 RTS\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_SERIAL, c.audio.achan[0].octrl[OCTYPE_DCD].ptt_method)
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
				},
			},
			{
				name:   "a GPIO number is stored for it too",
				config: "DCD GPIO 24\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_GPIO, c.audio.achan[0].octrl[OCTYPE_DCD].ptt_method)
					a.Equal(24, c.audio.achan[0].octrl[OCTYPE_DCD].out_gpio_num)
				},
			},
			{
				// A single CM108 GPIO byte cannot be updated a bit at a time, so only
				// PTT may use it.
				name:   "CM108 is rejected for anything but PTT",
				config: "DCD CM108 /dev/hidraw9\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_DCD].ptt_method)
					a.Contains(c.output, "only valid for PTT, not DCD")
				},
			},
			{
				name:   "a missing device does not eat the next line",
				config: "DCD\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_DCD].ptt_method)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"DEDUPE": {
			{
				name:   "a valid time is stored",
				config: "DEDUPE 45\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(45, c.digi.dedupe_time)
				},
			},
			{
				name:   "no DEDUPE leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DEDUPE, c.digi.dedupe_time)
				},
			},
			{
				name:   "zero turns duplicate suppression off",
				config: "DEDUPE 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.digi.dedupe_time)
				},
			},
			{
				name:   "an unreasonable time falls back to the default",
				config: "DEDUPE 600\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DEDUPE, c.digi.dedupe_time)
					a.Contains(c.output, "Unreasonable value for dedupe time")
				},
			},
			{
				name:   "a negative time falls back to the default",
				config: "DEDUPE -1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DEDUPE, c.digi.dedupe_time)
				},
			},
			{
				name:   "a missing time does not eat the next line",
				config: "DEDUPE\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DEDUPE, c.digi.dedupe_time)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the time went through an Atoi whose error was ignored,
			// so "DEDUPE abc" read as the 0 returned alongside it - in range, and
			// duplicate suppression switched off - rather than being rejected.
			{
				name:   "an unreadable time keeps the configured one",
				config: "DEDUPE 45\nDEDUPE abc\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(45, c.digi.dedupe_time)
				},
			},
		},
		// A digipeater needs two radio channels and a callsign on the transmit one,
		// so these lines are longer than most: config_init switches digipeating off
		// again for a channel that is still NOCALL.
		"DIGIPEAT": {
			{
				name:   "a valid line enables digipeating with both patterns",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.digi.enabled[0][1])
					a.NotNil(c.digi.alias[0][1])
					a.NotNil(c.digi.wide[0][1])
					a.Equal(PREEMPT_OFF, c.digi.preempt[0][1])
				},
			},
			{
				name:   "nothing is digipeated by default",
				config: "MYCALL Q1TEST\nACHANNELS 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.enabled[0][1])
				},
			},
			{
				name:   "TRACE asks for preemptive digipeating",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$ TRACE\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PREEMPT_TRACE, c.digi.preempt[0][1])
				},
			},
			{
				name:   "PREEMPT is the same thing under a better name",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$ PREEMPT\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PREEMPT_TRACE, c.digi.preempt[0][1])
				},
			},
			{
				name:   "DROP is accepted and discouraged",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$ DROP\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PREEMPT_DROP, c.digi.preempt[0][1])
					a.Contains(c.output, "DROP option is discouraged")
				},
			},
			{
				name:   "MARK is accepted and discouraged",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$ MARK\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PREEMPT_MARK, c.digi.preempt[0][1])
					a.Contains(c.output, "MARK option is discouraged")
				},
			},
			{
				name:   "ATGP takes the alias after the equals sign",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$ ATGP=Q2TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q2TEST", c.digi.atgp[0][1])
				},
			},
			{
				name:   "a non-numeric channel is rejected",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT zero 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.enabled[0][1])
					a.Contains(c.output, "is not allowed for FROM-channel")
				},
			},
			{
				name:   "a channel beyond the last one is rejected",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 16 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$\n",
				check: func(a *assert.Assertions, c configs) {
					a.Contains(c.output, "TO-channel must be in range")
				},
			},
			{
				name:   "a channel that is not a radio channel is rejected",
				config: "MYCALL Q1TEST\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.enabled[0][1])
					a.Contains(c.output, "TO-channel 1 is not valid")
				},
			},
			{
				name:   "an unusable pattern is rejected and nothing is digipeated",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE([3-7]-[1-7]$ ^WIDE[12]-[12]$\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.enabled[0][1])
					a.Contains(c.output, "Invalid alias matching pattern")
				},
			},
			{
				name:   "anything after the options is reported",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$ TRACE extra\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.digi.enabled[0][1])
					a.Contains(c.output, "where end of line was expected")
				},
			},
			{
				name:   "digipeating a channel with no callsign is switched off again",
				config: "ACHANNELS 2\nDIGIPEAT 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.enabled[0][1])
					a.Contains(c.output, "MYCALL must be set for transmit channel")
				},
			},
			{
				name:   "a missing TO-channel does not eat the next line",
				config: "ACHANNELS 2\nDIGIPEAT 0\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.enabled[0][1])
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"DIGIPEATER": {
			{
				name:   "the longer name is the same handler as DIGIPEAT",
				config: "MYCALL Q1TEST\nACHANNELS 2\nDIGIPEATER 0 1 ^WIDE[3-7]-[1-7]$ ^WIDE[12]-[12]$\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.digi.enabled[0][1])
					a.NotNil(c.digi.alias[0][1])
					a.NotNil(c.digi.wide[0][1])
				},
			},
		},
		"DNSSDNAME": {
			{
				name:   "a service name is stored",
				config: "DNSSDNAME Shack TNC\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Shack TNC", c.misc.dns_sd_name)
				},
			},
			{
				name:   "no DNSSDNAME leaves the name to be derived later",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.dns_sd_name)
				},
			},
			{
				name:   "a missing name does not eat the next line",
				config: "DNSSDNAME\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.dns_sd_name)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"DTMF": {
			{
				name:   "the decoder is off by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DTMF_DECODE_OFF, c.audio.achan[0].dtmf_decode)
				},
			},
			{
				name:   "the directive enables the decoder",
				config: "DTMF\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DTMF_DECODE_ON, c.audio.achan[0].dtmf_decode)
				},
			},
			{
				name:   "it enables the decoder on the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nDTMF\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DTMF_DECODE_OFF, c.audio.achan[0].dtmf_decode)
					a.Equal(DTMF_DECODE_ON, c.audio.achan[1].dtmf_decode)
				},
			},
		},
		"DWAIT": {
			{
				name:   "a valid delay is stored",
				config: "DWAIT 20\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(20, c.audio.achan[0].dwait)
				},
			},
			{
				name:   "no DWAIT leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DWAIT, c.audio.achan[0].dwait)
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nDWAIT 20\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DWAIT, c.audio.achan[0].dwait)
					a.Equal(20, c.audio.achan[1].dwait)
				},
			},
			{
				name:   "a delay beyond the byte it is sent in falls back to the default",
				config: "DWAIT 256\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DWAIT, c.audio.achan[0].dwait)
				},
			},
			{
				name:   "a negative delay falls back to the default",
				config: "DWAIT -1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DWAIT, c.audio.achan[0].dwait)
				},
			},
			{
				name:   "a missing delay does not eat the next line",
				config: "DWAIT\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_DWAIT, c.audio.achan[0].dwait)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the value went through an Atoi whose error was
			// ignored, so an unreadable one read as the 0 it returns alongside it -
			// which is in range - and silently replaced whatever was configured.
			{
				name:   "an unreadable delay leaves the configured one alone",
				config: "DWAIT 20\nDWAIT abc\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(20, c.audio.achan[0].dwait)
				},
			},
		},
		"EMAXFRAME": {
			{
				name:   "a valid window size is stored",
				config: "EMAXFRAME 16\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(16, c.misc.maxframe_extended)
				},
			},
			{
				name:   "no EMAXFRAME leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_EXTENDED_DEFAULT, c.misc.maxframe_extended)
				},
			},
			{
				name:   "the largest window we will send is accepted",
				config: "EMAXFRAME 63\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_EXTENDED_MAX, c.misc.maxframe_extended)
				},
			},
			{
				name:   "a larger window falls back to the default",
				config: "EMAXFRAME 64\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_EXTENDED_DEFAULT, c.misc.maxframe_extended)
				},
			},
			{
				name:   "a window of no frames at all falls back to the default",
				config: "EMAXFRAME 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_EXTENDED_DEFAULT, c.misc.maxframe_extended)
				},
			},
			{
				name:   "an unreadable window size falls back to the default",
				config: "EMAXFRAME sixteen\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_EXTENDED_DEFAULT, c.misc.maxframe_extended)
					a.Contains(c.output, "Invalid EMAXFRAME value")
				},
			},
			{
				name:   "a missing window size does not eat the next line",
				config: "EMAXFRAME\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_EXTENDED_DEFAULT, c.misc.maxframe_extended)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"FULLDUP": {
			{
				name:   "ON selects full duplex",
				config: "FULLDUP ON\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.audio.achan[0].fulldup)
				},
			},
			{
				name:   "the keyword is not case sensitive",
				config: "FULLDUP on\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.audio.achan[0].fulldup)
				},
			},
			{
				name:   "OFF selects half duplex",
				config: "FULLDUP ON\nFULLDUP OFF\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.audio.achan[0].fulldup)
				},
			},
			{
				name:   "half duplex by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_FULLDUP, c.audio.achan[0].fulldup)
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nFULLDUP ON\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.audio.achan[0].fulldup)
					a.True(c.audio.achan[1].fulldup)
				},
			},
			{
				name:   "anything other than ON or OFF leaves half duplex",
				config: "FULLDUP maybe\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.audio.achan[0].fulldup)
				},
			},
			{
				name:   "a missing setting does not eat the next line",
				config: "FULLDUP\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.audio.achan[0].fulldup)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"FX25AUTO": {
			{
				name:   "a repeat count is stored",
				config: "FX25AUTO 3\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(3, c.audio.fx25_auto_enable)
				},
			},
			{
				name:   "half of the default retry count by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/2, c.audio.fx25_auto_enable)
				},
			},
			{
				name:   "zero disables the feature",
				config: "FX25AUTO 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.audio.fx25_auto_enable)
				},
			},
			{
				name:   "an unreasonable count falls back to the default",
				config: "FX25AUTO 20\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/2, c.audio.fx25_auto_enable)
				},
			},
			{
				name:   "a negative count falls back to the default",
				config: "FX25AUTO -1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/2, c.audio.fx25_auto_enable)
				},
			},
			{
				name:   "a missing count does not eat the next line",
				config: "FX25AUTO\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/2, c.audio.fx25_auto_enable)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the count went through an Atoi whose error was
			// ignored, so "FX25AUTO abc" read as the 0 returned alongside it and
			// silently disabled the feature.
			{
				name:   "an unreadable count leaves the configured one alone",
				config: "FX25AUTO 3\nFX25AUTO abc\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(3, c.audio.fx25_auto_enable)
				},
			},
		},
		"FX25TX": {
			{
				name:   "a parity byte count selects FX.25 transmission",
				config: "FX25TX 16\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(16, c.audio.achan[0].fx25_strength)
					a.Equal(LAYER2_FX25, c.audio.achan[0].layer2_xmit)
				},
			},
			{
				name:   "AX.25 by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_AX25, c.audio.achan[0].layer2_xmit)
				},
			},
			{
				name:   "1 selects the automatic mode",
				config: "FX25TX 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.audio.achan[0].fx25_strength)
					a.Equal(LAYER2_FX25, c.audio.achan[0].layer2_xmit)
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nFX25TX 16\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_AX25, c.audio.achan[0].layer2_xmit)
					a.Equal(LAYER2_FX25, c.audio.achan[1].layer2_xmit)
				},
			},
			{
				name:   "an unreasonable count falls back to the automatic mode",
				config: "FX25TX 200\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.audio.achan[0].fx25_strength)
					a.Equal(LAYER2_FX25, c.audio.achan[0].layer2_xmit)
				},
			},
			{
				name:   "a missing mode does not eat the next line",
				config: "FX25TX\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_AX25, c.audio.achan[0].layer2_xmit)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the mode went through an Atoi whose error was
			// ignored, so "FX25TX abc" read as the 0 returned alongside it and
			// silently replaced a configured number of parity bytes.
			{
				name:   "an unreadable mode leaves the configured one alone",
				config: "FX25TX 16\nFX25TX abc\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(16, c.audio.achan[0].fx25_strength)
					a.Equal(LAYER2_FX25, c.audio.achan[0].layer2_xmit)
				},
			},
			// Regression test: 0 means off, as it does for the -X command line
			// option, but the handler still switched the channel to LAYER2_FX25.
			// Every frame then asked for an FX.25 mode that does not exist,
			// complained twice and fell back to AX.25.
			{
				name:   "zero leaves the channel on AX.25",
				config: "FX25TX 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.audio.achan[0].fx25_strength)
					a.Equal(LAYER2_AX25, c.audio.achan[0].layer2_xmit)
				},
			},
			{
				name:   "zero turns off what an earlier line turned on",
				config: "FX25TX 16\nFX25TX 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_AX25, c.audio.achan[0].layer2_xmit)
				},
			},
			{
				name:   "zero does not disturb a channel transmitting IL2P",
				config: "IL2PTX 1\nFX25TX 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_IL2P, c.audio.achan[0].layer2_xmit)
				},
			},
		},
		"GPSD": {
			{
				name:   "the directive on its own uses the local gpsd on its usual port",
				config: "GPSD\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("localhost", c.misc.gpsd_host)
					a.Equal(DEFAULT_GPSD_PORT, c.misc.gpsd_port)
				},
			},
			{
				name:   "no GPSD means no gpsd",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.gpsd_host)
				},
			},
			{
				name:   "a host of its own keeps the usual port",
				config: "GPSD gps.example.com\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("gps.example.com", c.misc.gpsd_host)
					a.Equal(DEFAULT_GPSD_PORT, c.misc.gpsd_port)
				},
			},
			{
				name:   "a host and port are both stored",
				config: "GPSD gps.example.com 2948\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("gps.example.com", c.misc.gpsd_host)
					a.Equal(2948, c.misc.gpsd_port)
				},
			},
			{
				name:   "an out-of-range port falls back to the usual one",
				config: "GPSD gps.example.com 99999\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_GPSD_PORT, c.misc.gpsd_port)
					a.Contains(c.output, "Invalid port number for GPSD")
				},
			},
			// Regression test: the port went through an Atoi whose error was ignored,
			// and the handler accepts 0 alongside the usual range, so "GPSD host
			// two-nine-four-seven" configured port 0 and every connection attempt
			// failed.
			{
				name:   "an unreadable port falls back to the usual one",
				config: "GPSD gps.example.com twentynineoneseven\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("gps.example.com", c.misc.gpsd_host)
					a.Equal(DEFAULT_GPSD_PORT, c.misc.gpsd_port)
				},
			},
		},
		"GPSNMEA": {
			{
				name:   "a serial port is stored with the traditional speed",
				config: "GPSNMEA /dev/ttyUSB0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyUSB0", c.misc.gpsnmea_port)
					a.Equal(4800, c.misc.gpsnmea_speed)
				},
			},
			{
				name:   "no GPSNMEA means no directly attached receiver",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.gpsnmea_port)
				},
			},
			{
				name:   "a speed after the port is stored",
				config: "GPSNMEA /dev/ttyUSB0 9600\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(9600, c.misc.gpsnmea_speed)
				},
			},
			{
				name:   "an unreadable speed is rejected",
				config: "GPSNMEA /dev/ttyUSB0 fast\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyUSB0", c.misc.gpsnmea_port)
					a.Equal(0, c.misc.gpsnmea_speed)
					a.Contains(c.output, "Invalid speed")
				},
			},
			{
				name:   "a missing port name does not eat the next line",
				config: "GPSNMEA\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.gpsnmea_port)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"ICHANNEL": {
			{
				name:   "a virtual channel becomes the IGate channel",
				config: "ICHANNEL 6\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_IGATE, c.audio.chan_medium[6])
					a.Equal(6, c.audio.igate_vchannel)
				},
			},
			{
				name:   "there is no IGate channel by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.audio.igate_vchannel)
				},
			},
			{
				name:   "a channel below the virtual range is rejected",
				config: "ICHANNEL 5\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[5])
					a.Equal(-1, c.audio.igate_vchannel)
				},
			},
			{
				name:   "a channel beyond the virtual range is rejected",
				config: "ICHANNEL 16\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.audio.igate_vchannel)
				},
			},
			{
				name:   "an unreadable channel number is rejected",
				config: "ICHANNEL six\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.audio.igate_vchannel)
				},
			},
			{
				name:   "a channel already in use is left as it was",
				config: "NCHANNEL 6 localhost 8001\nICHANNEL 6\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NETTNC, c.audio.chan_medium[6])
					a.Equal(-1, c.audio.igate_vchannel)
				},
			},
			{
				name:   "a missing channel number does not eat the next line",
				config: "ICHANNEL\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.audio.igate_vchannel)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"IGFILTER": {
			{
				name:   "a filter expression is stored as the rest of the line",
				config: "IGFILTER m/50 t/m\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("m/50 t/m", c.igate.t2_filter)
				},
			},
			{
				name:   "no IGFILTER means no server side filter",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_filter)
				},
			},
			{
				name:   "an expression with no filter at all leaves none",
				config: "IGFILTER\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_filter)
				},
			},
			{
				// One subscription is sent to the server, so a second line would
				// otherwise silently replace the first.
				name:   "a second line is reported and ignored",
				config: "IGFILTER m/50\nIGFILTER t/m\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("m/50", c.igate.t2_filter)
					a.Contains(c.output, "IGFILTER already configured")
				},
			},
			{
				name:   "an expression of only a filter is accepted and warned about",
				config: "IGFILTER m/50\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("m/50", c.igate.t2_filter)
					a.Contains(c.output, "rarely needed expert level feature")
				},
			},
			{
				name:   "an empty line does not eat the next one",
				config: "IGFILTER\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		// MYCALL comes first in each of these: config_init drops the login again if
		// any radio channel is still NOCALL, since an IGate has to identify itself.
		"IGLOGIN": {
			{
				name:   "a callsign and passcode are stored",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST 12345\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q1TEST", c.igate.t2_login)
					a.Equal("12345", c.igate.t2_passcode)
				},
			},
			{
				name:   "no IGLOGIN leaves no credentials",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_login)
					a.Empty(c.igate.t2_passcode)
				},
			},
			{
				name:   "a receive-only passcode is just another passcode here",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST-15 -1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q1TEST-15", c.igate.t2_login)
					a.Equal("-1", c.igate.t2_passcode)
				},
			},
			{
				name:   "a later line replaces the credentials rather than adding to them",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST 12345\nIGLOGIN Q2TEST 54321\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q2TEST", c.igate.t2_login)
					a.Equal("54321", c.igate.t2_passcode)
				},
			},
			{
				// igate_init insists on both, so a login with no passcode leaves the
				// gateway switched off rather than half configured.
				name:   "a missing passcode leaves nothing to log in with",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_passcode)
				},
			},
			{
				name:   "a login without a callsign for the channel is dropped again",
				config: "IGLOGIN Q1TEST 12345\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_login)
					a.Contains(c.output, "MYCALL must be set for receive channel")
				},
			},
			{
				name:   "a missing callsign does not eat the next line",
				config: "IGLOGIN\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_login)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"IGMSP": {
			{
				name:   "a count is stored",
				config: "IGMSP 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(2, c.igate.igmsp)
				},
			},
			{
				name:   "once by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.igate.igmsp)
				},
			},
			{
				name:   "zero sends no position for the message sender",
				config: "IGMSP 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.igate.igmsp)
				},
			},
			{
				name:   "an unreasonable count falls back to once",
				config: "IGMSP 11\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.igate.igmsp)
					a.Contains(c.output, "Unreasonable number of times")
				},
			},
			{
				name:   "a missing count is reported and falls back to once",
				config: "IGMSP 2\nIGMSP\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.igate.igmsp)
					a.Contains(c.output, "Missing number of times")
				},
			},
			{
				name:   "a missing count does not eat the next line",
				config: "IGMSP\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the count went through an Atoi whose error was
			// ignored, so "IGMSP two" read as the 0 returned alongside it - a valid
			// count, meaning send no position at all - rather than being rejected.
			{
				name:   "an unreadable count keeps the configured one",
				config: "IGMSP 2\nIGMSP two\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(2, c.igate.igmsp)
				},
			},
		},
		"IGSERVER": {
			{
				name:   "a server name is stored with the default port",
				config: "IGSERVER rotate.aprs2.net\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("rotate.aprs2.net", c.igate.t2_server_name)
					a.Equal(DEFAULT_IGATE_PORT, c.igate.t2_server_port)
				},
			},
			{
				name:   "no IGSERVER leaves no server to gate to",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_server_name)
				},
			},
			{
				name:   "a port after a colon is split out",
				config: "IGSERVER rotate.aprs2.net:14579\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("rotate.aprs2.net", c.igate.t2_server_name)
					a.Equal(14579, c.igate.t2_server_port)
				},
			},
			{
				name:   "a port as a separate token is accepted too",
				config: "IGSERVER rotate.aprs2.net 14579\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("rotate.aprs2.net", c.igate.t2_server_name)
					a.Equal(14579, c.igate.t2_server_port)
				},
			},
			{
				name:   "a bracketed IPv6 address keeps its colons",
				config: "IGSERVER [2001:db8::1]:14579\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("2001:db8::1", c.igate.t2_server_name)
					a.Equal(14579, c.igate.t2_server_port)
				},
			},
			{
				name:   "an out-of-range port after a colon falls back to the default",
				config: "IGSERVER rotate.aprs2.net:99999\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("rotate.aprs2.net", c.igate.t2_server_name)
					a.Equal(DEFAULT_IGATE_PORT, c.igate.t2_server_port)
				},
			},
			{
				name:   "an unreadable separate port falls back to the default",
				config: "IGSERVER rotate.aprs2.net fourteen\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_IGATE_PORT, c.igate.t2_server_port)
				},
			},
			{
				name:   "a name with a trailing colon and no port keeps the name",
				config: "IGSERVER rotate.aprs2.net:\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("rotate.aprs2.net", c.igate.t2_server_name)
					a.Equal(DEFAULT_IGATE_PORT, c.igate.t2_server_port)
				},
			},
			{
				name:   "a missing server name does not eat the next line",
				config: "IGSERVER\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.igate.t2_server_name)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"IGTXLIMIT": {
			{
				name:   "both limits are stored",
				config: "IGTXLIMIT 3 10\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(3, c.igate.tx_limit_1)
					a.Equal(10, c.igate.tx_limit_5)
				},
			},
			{
				name:   "no IGTXLIMIT leaves the defaults",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(IGATE_TX_LIMIT_1_DEFAULT, c.igate.tx_limit_1)
					a.Equal(IGATE_TX_LIMIT_5_DEFAULT, c.igate.tx_limit_5)
				},
			},
			{
				name:   "a limit of none at all becomes one, since a limit of zero would gate nothing",
				config: "IGTXLIMIT 0 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.igate.tx_limit_1)
					a.Equal(1, c.igate.tx_limit_5)
				},
			},
			{
				name:   "limits that would make no friends are reduced to the maximum",
				config: "IGTXLIMIT 100 200\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(IGATE_TX_LIMIT_1_MAX, c.igate.tx_limit_1)
					a.Equal(IGATE_TX_LIMIT_5_MAX, c.igate.tx_limit_5)
					a.Contains(c.output, "You won't make friends")
				},
			},
			{
				name:   "a missing five minute limit leaves that one at its default",
				config: "IGTXLIMIT 3\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(3, c.igate.tx_limit_1)
					a.Equal(IGATE_TX_LIMIT_5_DEFAULT, c.igate.tx_limit_5)
				},
			},
			{
				name:   "a missing one minute limit does not eat the next line",
				config: "IGTXLIMIT\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(IGATE_TX_LIMIT_1_DEFAULT, c.igate.tx_limit_1)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: both limits went through an Atoi whose error was
			// ignored, and the handler read anything below 1 as 1.  A typo therefore
			// clamped the gateway to a single transmission per interval without a
			// word about it.
			{
				name:   "an unreadable one minute limit keeps that default, and the five minute one still counts",
				config: "IGTXLIMIT three 15\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(IGATE_TX_LIMIT_1_DEFAULT, c.igate.tx_limit_1)
					a.Equal(15, c.igate.tx_limit_5)
				},
			},
			{
				name:   "an unreadable five minute limit keeps that default only",
				config: "IGTXLIMIT 3 ten\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(3, c.igate.tx_limit_1)
					a.Equal(IGATE_TX_LIMIT_5_DEFAULT, c.igate.tx_limit_5)
				},
			},
		},
		"IGTXVIA": {
			{
				name:   "a transmit channel is stored",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST 12345\nIGTXVIA 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.igate.tx_chan)
					a.Empty(c.igate.tx_via)
				},
			},
			{
				name:   "no IGTXVIA means nothing is gated to RF",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.igate.tx_chan)
				},
			},
			{
				name:   "a via path is stored with the comma the header needs",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST 12345\nIGTXVIA 0 WIDE1-1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(",WIDE1-1", c.igate.tx_via)
					a.Equal(1, c.igate.max_digi_hops)
				},
			},
			{
				name:   "the hop count comes from the SSID of a path ending in a digit",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST 12345\nIGTXVIA 0 WIDE2-2\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(2, c.igate.max_digi_hops)
				},
			},
			{
				name:   "an unusable via path is rejected but the channel still stands",
				config: "MYCALL Q1TEST\nIGLOGIN Q1TEST 12345\nIGTXVIA 0 WIDE1-1-1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.igate.tx_chan)
					a.Empty(c.igate.tx_via)
					a.Contains(c.output, "invalid via path")
				},
			},
			{
				name:   "a channel beyond the last one is rejected",
				config: "MYCALL Q1TEST\nIGTXVIA 16\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.igate.tx_chan)
				},
			},
			{
				name:   "an unreadable channel number is rejected",
				config: "MYCALL Q1TEST\nIGTXVIA zero\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.igate.tx_chan)
				},
			},
			{
				name:   "a transmit channel with no callsign is dropped again",
				config: "IGLOGIN Q1TEST 12345\nIGTXVIA 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.igate.tx_chan)
				},
			},
			{
				name:   "a missing channel number does not eat the next line",
				config: "IGTXVIA\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.igate.tx_chan)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"IL2PTX": {
			{
				name:   "with no options it selects IL2P with max FEC, normal polarity and a CRC",
				config: "IL2PTX\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_IL2P, c.audio.achan[0].layer2_xmit)
					a.Equal(1, c.audio.achan[0].il2p_max_fec)
					a.Equal(0, c.audio.achan[0].il2p_invert_polarity)
					a.True(c.audio.achan[0].il2p_crc)
				},
			},
			{
				name:   "AX.25 by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_AX25, c.audio.achan[0].layer2_xmit)
				},
			},
			{
				name:   "a minus inverts the polarity",
				config: "IL2PTX -\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.audio.achan[0].il2p_invert_polarity)
				},
			},
			{
				name:   "a plus is the normal polarity it already had",
				config: "IL2PTX +\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.audio.achan[0].il2p_invert_polarity)
				},
			},
			{
				name:   "0 asks for the weaker FEC",
				config: "IL2PTX 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.audio.achan[0].il2p_max_fec)
				},
			},
			{
				name:   "a lower case c drops the CRC",
				config: "IL2PTX c\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.audio.achan[0].il2p_crc)
				},
			},
			{
				name:   "options can be run together or given separately",
				config: "IL2PTX -0c\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.audio.achan[0].il2p_invert_polarity)
					a.Equal(0, c.audio.achan[0].il2p_max_fec)
					a.False(c.audio.achan[0].il2p_crc)
				},
			},
			{
				name:   "separate options have the same effect as run-together ones",
				config: "IL2PTX - 0 c\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.audio.achan[0].il2p_invert_polarity)
					a.Equal(0, c.audio.achan[0].il2p_max_fec)
					a.False(c.audio.achan[0].il2p_crc)
				},
			},
			{
				name:   "a later line starts again from the defaults",
				config: "IL2PTX -0c\nIL2PTX\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.audio.achan[0].il2p_invert_polarity)
					a.Equal(1, c.audio.achan[0].il2p_max_fec)
					a.True(c.audio.achan[0].il2p_crc)
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nIL2PTX\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_AX25, c.audio.achan[0].layer2_xmit)
					a.Equal(LAYER2_IL2P, c.audio.achan[1].layer2_xmit)
				},
			},
			{
				name:   "an unrecognised option is reported and the rest still apply",
				config: "IL2PTX x-\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(LAYER2_IL2P, c.audio.achan[0].layer2_xmit)
					a.Equal(1, c.audio.achan[0].il2p_invert_polarity)
				},
			},
		},
		"KISSCOPY": {
			{
				name:   "the directive turns copying on",
				config: "KISSCOPY\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.misc.kiss_copy)
				},
			},
			{
				name:   "off by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.misc.kiss_copy)
				},
			},
		},
		"LOGDIR": {
			{
				name:   "a directory is stored and daily names asked for",
				config: "LOGDIR /var/log/samoyed\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/var/log/samoyed", c.misc.log_path)
					a.True(c.misc.log_daily_names)
				},
			},
			{
				name:   "no LOGDIR means no logging",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.log_path)
					a.False(c.misc.log_daily_names)
				},
			},
			{
				name:   "it replaces an earlier LOGFILE and says so",
				config: "LOGFILE /var/log/samoyed.log\nLOGDIR /var/log/samoyed\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/var/log/samoyed", c.misc.log_path)
					a.True(c.misc.log_daily_names)
					a.Contains(c.output, "replacing an earlier LOGDIR or LOGFILE")
				},
			},
			{
				name:   "anything after the directory is reported",
				config: "LOGDIR /var/log/samoyed extra\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/var/log/samoyed", c.misc.log_path)
					a.Contains(c.output, "should have directory path and nothing more")
				},
			},
			{
				name:   "a missing directory does not eat the next line",
				config: "LOGDIR\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.log_path)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"LOGFILE": {
			{
				name:   "a file name is stored and daily names turned off",
				config: "LOGFILE /var/log/samoyed.log\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/var/log/samoyed.log", c.misc.log_path)
					a.False(c.misc.log_daily_names)
				},
			},
			{
				name:   "it replaces an earlier LOGDIR and says so",
				config: "LOGDIR /var/log/samoyed\nLOGFILE /var/log/samoyed.log\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/var/log/samoyed.log", c.misc.log_path)
					a.False(c.misc.log_daily_names)
					a.Contains(c.output, "replacing an earlier LOGDIR or LOGFILE")
				},
			},
			{
				name:   "anything after the file name is reported",
				config: "LOGFILE /var/log/samoyed.log extra\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/var/log/samoyed.log", c.misc.log_path)
					a.Contains(c.output, "should have file name and nothing more")
				},
			},
			{
				name:   "a missing file name does not eat the next line",
				config: "LOGFILE\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.log_path)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"MAXFRAME": {
			{
				name:   "a valid window size is stored",
				config: "MAXFRAME 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(2, c.misc.maxframe_basic)
				},
			},
			{
				name:   "no MAXFRAME leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_BASIC_DEFAULT, c.misc.maxframe_basic)
				},
			},
			{
				name:   "the largest window a modulo 8 sequence number allows is accepted",
				config: "MAXFRAME 7\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_BASIC_MAX, c.misc.maxframe_basic)
				},
			},
			{
				name:   "a window that will not fit a modulo 8 sequence number falls back to the default",
				config: "MAXFRAME 8\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_BASIC_DEFAULT, c.misc.maxframe_basic)
				},
			},
			{
				name:   "a window of no frames at all falls back to the default",
				config: "MAXFRAME 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_BASIC_DEFAULT, c.misc.maxframe_basic)
				},
			},
			{
				name:   "an unreadable window size falls back to the default",
				config: "MAXFRAME two\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_BASIC_DEFAULT, c.misc.maxframe_basic)
					a.Contains(c.output, "Invalid MAXFRAME value")
				},
			},
			{
				name:   "a missing window size does not eat the next line",
				config: "MAXFRAME\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_K_MAXFRAME_BASIC_DEFAULT, c.misc.maxframe_basic)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"MAXV22": {
			{
				name:   "a valid count is stored",
				config: "MAXV22 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(2, c.misc.maxv22)
				},
			},
			{
				name:   "a third of the retry count by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/3, c.misc.maxv22)
				},
			},
			{
				name:   "zero means never offering v2.2 at all",
				config: "MAXV22 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.misc.maxv22)
				},
			},
			{
				name:   "a count beyond the retry maximum keeps the default",
				config: "MAXV22 16\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/3, c.misc.maxv22)
				},
			},
			{
				name:   "a negative count keeps the default",
				config: "MAXV22 -1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/3, c.misc.maxv22)
				},
			},
			{
				name:   "a missing count does not eat the next line",
				config: "MAXV22\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT/3, c.misc.maxv22)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the count went through an Atoi whose error was
			// ignored, so "MAXV22 two" read as the 0 returned alongside it - a valid
			// count, meaning never offer v2.2 - rather than being rejected.
			{
				name:   "an unreadable count keeps the configured one",
				config: "MAXV22 2\nMAXV22 two\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(2, c.misc.maxv22)
				},
			},
		},
		"NCHANNEL": {
			{
				name:   "a virtual channel, address and port are stored",
				config: "NCHANNEL 6 localhost 8001\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NETTNC, c.audio.chan_medium[6])
					a.Equal("localhost", c.audio.nettnc_addr[6])
					a.Equal(8001, c.audio.nettnc_port[6])
				},
			},
			{
				name:   "a channel below the virtual range is rejected",
				config: "NCHANNEL 5 localhost 8001\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[5])
					a.Empty(c.audio.nettnc_addr[5])
				},
			},
			{
				name:   "a channel beyond the virtual range is rejected",
				config: "NCHANNEL 16 localhost 8001\n",
				check: func(a *assert.Assertions, c configs) {
					for channel := range MAX_TOTAL_CHANS {
						a.NotEqual(MEDIUM_NETTNC, c.audio.chan_medium[channel])
					}
				},
			},
			{
				name:   "an unreadable channel number is rejected",
				config: "NCHANNEL six localhost 8001\n",
				check: func(a *assert.Assertions, c configs) {
					for channel := range MAX_TOTAL_CHANS {
						a.NotEqual(MEDIUM_NETTNC, c.audio.chan_medium[channel])
					}
				},
			},
			{
				name:   "a channel already in use is left as it was",
				config: "ICHANNEL 6\nNCHANNEL 6 localhost 8001\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_IGATE, c.audio.chan_medium[6])
					a.Empty(c.audio.nettnc_addr[6])
				},
			},
			{
				name:   "two network TNCs can be configured at once",
				config: "NCHANNEL 6 localhost 8001\nNCHANNEL 7 192.0.2.1 8002\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("localhost", c.audio.nettnc_addr[6])
					a.Equal(8001, c.audio.nettnc_port[6])
					a.Equal("192.0.2.1", c.audio.nettnc_addr[7])
					a.Equal(8002, c.audio.nettnc_port[7])
				},
			},
			{
				name:   "a missing channel number does not eat the next line",
				config: "NCHANNEL\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the handler marked the channel MEDIUM_NETTNC and
			// stored the address before it had read the port, so a line that was
			// then rejected left a network TNC channel behind with port 0.
			// nettnc_init attaches to every such channel at startup and exits if it
			// cannot, so a typo took the whole program down.
			{
				name:   "a missing port leaves the channel alone",
				config: "NCHANNEL 6 localhost\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[6])
					a.Empty(c.audio.nettnc_addr[6])
				},
			},
			{
				name:   "an out-of-range port leaves the channel alone",
				config: "NCHANNEL 6 localhost 99999\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[6])
					a.Empty(c.audio.nettnc_addr[6])
					a.Zero(c.audio.nettnc_port[6])
				},
			},
			{
				name:   "an unreadable port leaves the channel alone",
				config: "NCHANNEL 6 localhost eightthousandandone\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[6])
					a.Empty(c.audio.nettnc_addr[6])
				},
			},
			{
				name:   "a missing address leaves the channel alone",
				config: "NCHANNEL 6\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(MEDIUM_NONE, c.audio.chan_medium[6])
				},
			},
		},
		"NOXID": {
			{
				name:   "an address is stored",
				config: "NOXID Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q1TEST"}, c.misc.noxid_addrs)
					a.Equal(1, c.misc.noxid_count)
				},
			},
			{
				name:   "no NOXID means XID is tried with everyone",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.noxid_addrs)
					a.Equal(0, c.misc.noxid_count)
				},
			},
			{
				name:   "several addresses on one line are all stored",
				config: "NOXID Q1TEST Q2TEST-5\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q1TEST", "Q2TEST-5"}, c.misc.noxid_addrs)
					a.Equal(2, c.misc.noxid_count)
				},
			},
			{
				name:   "repeated lines are cumulative",
				config: "NOXID Q1TEST\nNOXID Q2TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q1TEST", "Q2TEST"}, c.misc.noxid_addrs)
					a.Equal(2, c.misc.noxid_count)
				},
			},
			{
				name:   "an unusable address is rejected and the rest of the line still counts",
				config: "NOXID Q1TEST-99 Q2TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q2TEST"}, c.misc.noxid_addrs)
					a.Equal(1, c.misc.noxid_count)
					a.Contains(c.output, "Invalid station address for NOXID")
				},
			},
			{
				name:   "a missing address does not eat the next line",
				config: "NOXID\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.noxid_addrs)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		// NULLMODEM and SERIALKISS are the same handler under two names, the second
		// being the one that says what it does.
		"NULLMODEM": {
			{
				name:   "a port name is stored",
				config: "NULLMODEM /dev/ttyS0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyS0", c.misc.kiss_serial_port)
					a.Equal(0, c.misc.kiss_serial_speed)
					a.Equal(0, c.misc.kiss_serial_poll)
				},
			},
			{
				name:   "no NULLMODEM means no serial KISS port",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.kiss_serial_port)
				},
			},
			{
				name:   "a speed after the name is stored",
				config: "NULLMODEM /dev/ttyS0 9600\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyS0", c.misc.kiss_serial_port)
					a.Equal(9600, c.misc.kiss_serial_speed)
				},
			},
			{
				name:   "an unreadable speed is rejected",
				config: "NULLMODEM /dev/ttyS0 fast\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.misc.kiss_serial_speed)
					a.Contains(c.output, "Invalid speed")
				},
			},
			{
				// There is only one serial KISS port, so a second line replaces the
				// first rather than adding to it.
				name:   "a second line replaces the first and says so",
				config: "NULLMODEM /dev/ttyS0\nNULLMODEM /dev/ttyS1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyS1", c.misc.kiss_serial_port)
					a.Contains(c.output, "replaces earlier value")
				},
			},
			{
				name:   "a missing port name does not eat the next line",
				config: "NULLMODEM\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.kiss_serial_port)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"PACLEN": {
			{
				name:   "a valid length is stored",
				config: "PACLEN 128\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(128, c.misc.paclen)
				},
			},
			{
				name:   "no PACLEN leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N1_PACLEN_DEFAULT, c.misc.paclen)
				},
			},
			{
				name:   "the limits themselves are accepted",
				config: "PACLEN 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N1_PACLEN_MIN, c.misc.paclen)
				},
			},
			{
				name:   "a length longer than an information field can hold keeps the default",
				config: "PACLEN 9999\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N1_PACLEN_DEFAULT, c.misc.paclen)
				},
			},
			{
				name:   "a length of nothing at all keeps the default",
				config: "PACLEN 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N1_PACLEN_DEFAULT, c.misc.paclen)
				},
			},
			{
				name:   "an unreadable length keeps the default",
				config: "PACLEN long\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N1_PACLEN_DEFAULT, c.misc.paclen)
					a.Contains(c.output, "Invalid PACLEN value")
				},
			},
			{
				name:   "a missing length does not eat the next line",
				config: "PACLEN\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N1_PACLEN_DEFAULT, c.misc.paclen)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"PERSIST": {
			{
				name:   "a valid probability is stored",
				config: "PERSIST 100\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(100, c.audio.achan[0].persist)
				},
			},
			{
				name:   "no PERSIST leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_PERSIST, c.audio.achan[0].persist)
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nPERSIST 100\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_PERSIST, c.audio.achan[0].persist)
					a.Equal(100, c.audio.achan[1].persist)
				},
			},
			{
				name:   "a probability below the accepted range falls back to the default",
				config: "PERSIST 4\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_PERSIST, c.audio.achan[0].persist)
				},
			},
			{
				name:   "a probability that would not fit the byte it is sent in falls back to the default",
				config: "PERSIST 256\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_PERSIST, c.audio.achan[0].persist)
				},
			},
			{
				name:   "an unreadable probability falls back to the default",
				config: "PERSIST abc\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_PERSIST, c.audio.achan[0].persist)
				},
			},
			{
				name:   "a missing probability does not eat the next line",
				config: "PERSIST\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_PERSIST, c.audio.achan[0].persist)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		// PTT, DCD and CON are one handler, keyed on the keyword; the cases here use
		// PTT, with the other two covering what differs.
		"PTT": {
			{
				name:   "a serial port and control line are stored",
				config: "PTT /dev/ttyS0 RTS\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_SERIAL, octrl.ptt_method)
					a.Equal("/dev/ttyS0", octrl.ptt_device)
					a.Equal(PTT_LINE_RTS, octrl.ptt_line)
					a.False(octrl.ptt_invert)
				},
			},
			{
				name:   "nothing keys the radio by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
				},
			},
			{
				name:   "a minus in front of the control line inverts it",
				config: "PTT /dev/ttyS0 -DTR\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_LINE_DTR, octrl.ptt_line)
					a.True(octrl.ptt_invert)
				},
			},
			{
				name:   "a second control line on the same port is stored too",
				config: "PTT /dev/ttyS0 RTS -DTR\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_LINE_RTS, octrl.ptt_line)
					a.Equal(PTT_LINE_DTR, octrl.ptt_line2)
					a.True(octrl.ptt_invert2)
				},
			},
			{
				name:   "the same control line twice is reported",
				config: "PTT /dev/ttyS0 RTS RTS\n",
				check: func(a *assert.Assertions, c configs) {
					a.Contains(c.output, "control line twice")
				},
			},
			{
				name:   "something that is neither RTS nor DTR is rejected",
				config: "PTT /dev/ttyS0 XYZ\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.Contains(c.output, "Expected RTS or DTR")
				},
			},
			{
				name:   "a GPIO number is stored",
				config: "PTT GPIO 25\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_GPIO, octrl.ptt_method)
					a.Equal(25, octrl.out_gpio_num)
					a.False(octrl.ptt_invert)
				},
			},
			{
				name:   "a negative GPIO number is the same line, active low",
				config: "PTT GPIO -25\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(25, octrl.out_gpio_num)
					a.True(octrl.ptt_invert)
				},
			},
			{
				name:   "a GPIOD chip name becomes a device path",
				config: "PTT GPIOD gpiochip3 12\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_GPIOD, octrl.ptt_method)
					a.Equal("/dev/gpiochip3", octrl.out_gpio_name)
					a.Equal(12, octrl.out_gpio_num)
				},
			},
			{
				name:   "a GPIOD chip number becomes a device path as well",
				config: "PTT GPIOD 3 12\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/gpiochip3", c.audio.achan[0].octrl[OCTYPE_PTT].out_gpio_name)
				},
			},
			{
				name:   "a GPIOD device path is taken as given",
				config: "PTT GPIOD /dev/gpiochip3 12\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/gpiochip3", c.audio.achan[0].octrl[OCTYPE_PTT].out_gpio_name)
				},
			},
			{
				name:   "an LPT bit number is stored",
				config: "PTT LPT 3\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_LPT, octrl.ptt_method)
					a.Equal(3, octrl.ptt_lpt_bit)
					a.False(octrl.ptt_invert)
				},
			},
			{
				name:   "a negative LPT bit number inverts it",
				config: "PTT LPT -3\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(3, octrl.ptt_lpt_bit)
					a.True(octrl.ptt_invert)
				},
			},
			{
				name:   "a hamlib model, port and rate are stored",
				config: "PTT RIG 101 /dev/ttyS0 9600\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_HAMLIB, octrl.ptt_method)
					a.Equal(101, octrl.ptt_model)
					a.Equal("/dev/ttyS0", octrl.ptt_device)
					a.Equal(9600, octrl.ptt_rate)
				},
			},
			{
				name:   "AUTO asks hamlib to work the model out",
				config: "PTT RIG AUTO /dev/ttyS0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(-1, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_model)
				},
			},
			{
				name:   "a rig name where the model number belongs is rejected",
				config: "PTT RIG FT-847 /dev/ttyS0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.Contains(c.output, "A rig number, not a name")
				},
			},
			{
				name:   "an unreasonable model number is rejected",
				config: "PTT RIG 0 /dev/ttyS0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.Contains(c.output, "Unreasonable model number")
				},
			},
			{
				name:   "an unreadable CAT rate is rejected",
				config: "PTT RIG 101 /dev/ttyS0 fast\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.Contains(c.output, "optional number is required here")
				},
			},
			{
				name:   "a CM108 device and its GPIO bit are stored",
				config: "PTT CM108 /dev/hidraw9\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_CM108, octrl.ptt_method)
					a.Equal("/dev/hidraw9", octrl.ptt_device)
					a.Equal(3, octrl.out_gpio_num)
				},
			},
			{
				name:   "a CM108 GPIO bit outside 1 to 8 is rejected",
				config: "PTT CM108 9 /dev/hidraw9\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.Contains(c.output, "is not in range of 1 thru 8")
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nPTT GPIO 25\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.Equal(PTT_METHOD_GPIO, c.audio.achan[1].octrl[OCTYPE_PTT].ptt_method)
				},
			},
			{
				name:   "a missing device does not eat the next line",
				config: "PTT\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the port kept both halves of the C file's
			// "#ifdef USE_HAMLIB" and dropped the #ifdef, so a RIG line configured
			// hamlib PTT and then told the operator that hamlib was not supported
			// and that they would have to rebuild.  Hamlib is supported.
			{
				name:   "a hamlib line does not claim hamlib is unsupported",
				config: "PTT RIG 101 /dev/ttyS0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_HAMLIB, c.audio.achan[0].octrl[OCTYPE_PTT].ptt_method)
					a.NotContains(c.output, "only available when hamlib support is enabled")
					a.NotContains(c.output, "must rebuild")
				},
			},
			// Regression test: the GPIO, GPIOD and LPT numbers went through an Atoi
			// whose error was ignored, so a typo configured GPIO line 0 or LPT bit 0
			// as the transmit control - which is how a station ends up keying
			// something that isn't the radio, or not keying at all.
			{
				name:   "an unreadable GPIO number leaves the radio unkeyed",
				config: "PTT GPIO twentyfive\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_NONE, octrl.ptt_method)
					a.Zero(octrl.out_gpio_num)
				},
			},
			{
				name:   "an unreadable GPIOD line number leaves the radio unkeyed",
				config: "PTT GPIOD gpiochip3 twelve\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_NONE, octrl.ptt_method)
					a.Zero(octrl.out_gpio_num)
				},
			},
			{
				name:   "an unreadable LPT bit number leaves the radio unkeyed",
				config: "PTT LPT three\n",
				check: func(a *assert.Assertions, c configs) {
					var octrl = c.audio.achan[0].octrl[OCTYPE_PTT]
					a.Equal(PTT_METHOD_NONE, octrl.ptt_method)
					a.Zero(octrl.ptt_lpt_bit)
				},
			},
		},
		"REGEN": {
			{
				name:   "a valid line turns regeneration on",
				config: "MYCALL Q1TEST\nACHANNELS 2\nREGEN 0 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.True(c.digi.regen[0][1])
				},
			},
			{
				name:   "nothing is regenerated by default",
				config: "MYCALL Q1TEST\nACHANNELS 2\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.regen[0][1])
				},
			},
			{
				name:   "a non-numeric channel is rejected",
				config: "MYCALL Q1TEST\nACHANNELS 2\nREGEN zero 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.regen[0][1])
					a.Contains(c.output, "is not allowed for FROM-channel")
				},
			},
			{
				name:   "a channel beyond the last radio channel is rejected",
				config: "MYCALL Q1TEST\nACHANNELS 2\nREGEN 0 6\n",
				check: func(a *assert.Assertions, c configs) {
					a.Contains(c.output, "TO-channel must be in range")
				},
			},
			{
				name:   "a channel that is not a radio channel is rejected",
				config: "MYCALL Q1TEST\nREGEN 0 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.regen[0][1])
					a.Contains(c.output, "TO-channel 1 is not valid")
				},
			},
			{
				name:   "a missing TO-channel does not eat the next line",
				config: "ACHANNELS 2\nREGEN 0\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.False(c.digi.regen[0][1])
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"RETRY": {
			{
				name:   "a valid count is stored",
				config: "RETRY 5\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(5, c.misc.retry)
				},
			},
			{
				name:   "no RETRY leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT, c.misc.retry)
				},
			},
			{
				name:   "the limits themselves are accepted",
				config: "RETRY 15\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_MAX, c.misc.retry)
				},
			},
			{
				name:   "a count beyond the range keeps the default",
				config: "RETRY 16\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT, c.misc.retry)
				},
			},
			{
				name:   "never retrying at all is not on offer",
				config: "RETRY 0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT, c.misc.retry)
				},
			},
			{
				name:   "an unreadable count keeps the default",
				config: "RETRY five\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT, c.misc.retry)
					a.Contains(c.output, "Invalid RETRY number")
				},
			},
			{
				name:   "a missing count does not eat the next line",
				config: "RETRY\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(AX25_N2_RETRY_DEFAULT, c.misc.retry)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"SATGATE": {
			{
				name:   "a delay is stored",
				config: "SATGATE 20\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(20, c.igate.satgate_delay)
				},
			},
			{
				name:   "no SATGATE means no delay and no SATgate mode",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.igate.satgate_delay)
				},
			},
			{
				name:   "the directive on its own takes the default delay",
				config: "SATGATE\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_SATGATE_DELAY, c.igate.satgate_delay)
				},
			},
			{
				name:   "a delay shorter than the minimum falls back to the default",
				config: "SATGATE 4\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_SATGATE_DELAY, c.igate.satgate_delay)
					a.Contains(c.output, "Unreasonable SATgate delay")
				},
			},
			{
				name:   "a delay longer than the maximum falls back to the default",
				config: "SATGATE 31\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_SATGATE_DELAY, c.igate.satgate_delay)
				},
			},
			{
				name:   "the directive says it is on its way out",
				config: "SATGATE 20\n",
				check: func(a *assert.Assertions, c configs) {
					a.Contains(c.output, "will be removed in a future version")
				},
			},
		},
		"SERIALKISS": {
			{
				name:   "a port name and speed are stored, as for NULLMODEM",
				config: "SERIALKISS /dev/ttyS0 9600\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyS0", c.misc.kiss_serial_port)
					a.Equal(9600, c.misc.kiss_serial_speed)
					a.Equal(0, c.misc.kiss_serial_poll)
				},
			},
			{
				name:   "it replaces a port name given under the other name",
				config: "NULLMODEM /dev/ttyS0\nSERIALKISS /dev/ttyS1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyS1", c.misc.kiss_serial_port)
				},
			},
		},
		"SERIALKISSPOLL": {
			{
				name:   "a port name is stored and marked for polling",
				config: "SERIALKISSPOLL /dev/rfcomm0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/rfcomm0", c.misc.kiss_serial_port)
					a.Equal(1, c.misc.kiss_serial_poll)
					a.Equal(0, c.misc.kiss_serial_speed)
				},
			},
			{
				name:   "nothing is polled by default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(0, c.misc.kiss_serial_poll)
				},
			},
			{
				name:   "it takes no speed, so a second token is left for the next directive to trip over",
				config: "SERIALKISSPOLL /dev/rfcomm0 9600\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/rfcomm0", c.misc.kiss_serial_port)
					a.Equal(0, c.misc.kiss_serial_speed)
				},
			},
			{
				name:   "it replaces a port configured for a fixed speed, and stops polling when replaced in turn",
				config: "SERIALKISSPOLL /dev/rfcomm0\nSERIALKISS /dev/ttyS0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyS0", c.misc.kiss_serial_port)
					a.Equal(0, c.misc.kiss_serial_poll)
				},
			},
			{
				name:   "a missing port name does not eat the next line",
				config: "SERIALKISSPOLL\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.kiss_serial_port)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"SPEECH": {
			{
				name:   "a script name is accepted and does not derail the next line",
				config: "SPEECH /bin/echo\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			{
				name:   "a missing script name does not eat the next line",
				config: "SPEECH\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the port left the assignment commented out, so the
			// script was read, checked for being present, and thrown away.  xmit
			// skips speaking when tts_script is empty, so SPEECH did nothing at all.
			{
				name:   "the script is stored",
				config: "SPEECH /bin/echo\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/bin/echo", c.audio.tts_script)
				},
			},
			{
				name:   "a script name with spaces can be quoted",
				config: "SPEECH \"/usr/local/bin/say it\"\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/usr/local/bin/say it", c.audio.tts_script)
				},
			},
			{
				name:   "no SPEECH leaves nothing to speak with",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.audio.tts_script)
				},
			},
			{
				name:   "a missing script name leaves nothing to speak with",
				config: "SPEECH\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.audio.tts_script)
				},
			},
		},
		"TXINH": {
			{
				name:   "a GPIO number becomes the transmit inhibit input",
				config: "TXINH GPIO 25\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_GPIO, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
					a.Equal(25, c.audio.achan[0].ictrl[ICTYPE_TXINH].in_gpio_num)
					a.False(c.audio.achan[0].ictrl[ICTYPE_TXINH].invert)
				},
			},
			{
				name:   "a negative GPIO number is the same line, active low",
				config: "TXINH GPIO -25\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_GPIO, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
					a.Equal(25, c.audio.achan[0].ictrl[ICTYPE_TXINH].in_gpio_num)
					a.True(c.audio.achan[0].ictrl[ICTYPE_TXINH].invert)
				},
			},
			{
				name:   "the type name is not case sensitive",
				config: "TXINH gpio 25\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_GPIO, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
				},
			},
			{
				name:   "no TXINH means nothing can hold off the transmitter",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nTXINH GPIO 25\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
					a.Equal(PTT_METHOD_GPIO, c.audio.achan[1].ictrl[ICTYPE_TXINH].method)
				},
			},
			{
				name:   "a missing type name does not eat the next line",
				config: "TXINH\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			{
				name:   "a missing GPIO number leaves the input unconfigured",
				config: "TXINH GPIO\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the number went through an Atoi whose error was
			// ignored, so "TXINH GPIO ab" configured GPIO 0 as the transmit inhibit
			// input.  Whatever that pin happens to be doing then decides whether the
			// station may transmit at all.
			{
				name:   "an unreadable GPIO number leaves the input unconfigured",
				config: "TXINH GPIO twentyfive\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
					a.Zero(c.audio.achan[0].ictrl[ICTYPE_TXINH].in_gpio_num)
				},
			},
			// Regression test: an input type other than GPIO fell through the
			// handler without a word, so a typo left the transmitter with nothing
			// holding it off and nothing said about it.
			{
				name:   "an unrecognised input type is reported",
				config: "TXINH SERIAL 25\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(PTT_METHOD_NONE, c.audio.achan[0].ictrl[ICTYPE_TXINH].method)
					a.Contains(c.output, "Unrecognized input type name")
				},
			},
		},
		"TXTAIL": {
			{
				name:   "a valid time is stored",
				config: "TXTAIL 20\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(20, c.audio.achan[0].txtail)
				},
			},
			{
				name:   "no TXTAIL leaves the default",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_TXTAIL, c.audio.achan[0].txtail)
				},
			},
			{
				name:   "it applies to the current channel only",
				config: "ACHANNELS 2\nCHANNEL 1\nTXTAIL 20\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_TXTAIL, c.audio.achan[0].txtail)
					a.Equal(20, c.audio.achan[1].txtail)
				},
			},
			{
				name:   "an ill-advised but usable time is still stored, with a warning",
				config: "TXTAIL 1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(1, c.audio.achan[0].txtail)
				},
			},
			{
				name:   "a time beyond the byte it is sent in falls back to the default",
				config: "TXTAIL 256\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_TXTAIL, c.audio.achan[0].txtail)
				},
			},
			{
				name:   "a negative time falls back to the default",
				config: "TXTAIL -1\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_TXTAIL, c.audio.achan[0].txtail)
				},
			},
			{
				name:   "a missing time does not eat the next line",
				config: "TXTAIL\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(DEFAULT_TXTAIL, c.audio.achan[0].txtail)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
			// Regression test: the value went through an Atoi whose error was
			// ignored, so "TXTAIL abc" read as 0 - in range, and a transmit tail of
			// nothing at all - rather than being rejected.
			{
				name:   "an unreadable time leaves the configured one alone",
				config: "TXTAIL 20\nTXTAIL abc\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(20, c.audio.achan[0].txtail)
				},
			},
		},
		"V20": {
			{
				name:   "an address is stored",
				config: "V20 Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q1TEST"}, c.misc.v20_addrs)
					a.Equal(1, c.misc.v20_count)
				},
			},
			{
				name:   "no V20 means every station is offered v2.2 first",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.v20_addrs)
					a.Equal(0, c.misc.v20_count)
				},
			},
			{
				name:   "several addresses on one line are all stored",
				config: "V20 Q1TEST Q2TEST-5\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q1TEST", "Q2TEST-5"}, c.misc.v20_addrs)
					a.Equal(2, c.misc.v20_count)
				},
			},
			{
				name:   "repeated lines are cumulative",
				config: "V20 Q1TEST\nV20 Q2TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q1TEST", "Q2TEST"}, c.misc.v20_addrs)
					a.Equal(2, c.misc.v20_count)
				},
			},
			{
				name:   "an unusable address is rejected and the rest of the line still counts",
				config: "V20 Q1TEST-99 Q2TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal([]string{"Q2TEST"}, c.misc.v20_addrs)
					a.Equal(1, c.misc.v20_count)
					a.Contains(c.output, "Invalid station address for V20")
				},
			},
			{
				name:   "a missing address does not eat the next line",
				config: "V20\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.v20_addrs)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
		"WAYPOINT": {
			{
				name:   "a serial port is stored",
				config: "WAYPOINT /dev/ttyS0\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("/dev/ttyS0", c.misc.waypoint_serial_port)
					a.Empty(c.misc.waypoint_udp_hostname)
					a.Zero(c.misc.waypoint_formats)
				},
			},
			{
				name:   "no WAYPOINT means no waypoints are sent anywhere",
				config: "MYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.waypoint_serial_port)
					a.Empty(c.misc.waypoint_udp_hostname)
				},
			},
			{
				name:   "a host and UDP port are split out",
				config: "WAYPOINT mapper.example.com:8123\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("mapper.example.com", c.misc.waypoint_udp_hostname)
					a.Equal(8123, c.misc.waypoint_udp_portnum)
					a.Empty(c.misc.waypoint_serial_port)
				},
			},
			{
				name:   "a port with no host in front of it means this machine",
				config: "WAYPOINT :8123\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal("localhost", c.misc.waypoint_udp_hostname)
					a.Equal(8123, c.misc.waypoint_udp_portnum)
				},
			},
			{
				name:   "the formats after the device are all enabled",
				config: "WAYPOINT /dev/ttyS0 NGA\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(WPL_FORMAT_NMEA_GENERIC|WPL_FORMAT_GARMIN|WPL_FORMAT_AIS,
						c.misc.waypoint_formats)
				},
			},
			{
				name:   "format letters may be lower case and separated by commas",
				config: "WAYPOINT /dev/ttyS0 m,k\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(WPL_FORMAT_MAGELLAN|WPL_FORMAT_KENWOOD, c.misc.waypoint_formats)
				},
			},
			{
				name:   "an unrecognised format letter is reported and the rest still apply",
				config: "WAYPOINT /dev/ttyS0 NX\n",
				check: func(a *assert.Assertions, c configs) {
					a.Equal(WPL_FORMAT_NMEA_GENERIC, c.misc.waypoint_formats)
					a.Contains(c.output, "Invalid output format")
				},
			},
			{
				name:   "an out-of-range UDP port is rejected",
				config: "WAYPOINT mapper.example.com:99999\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.waypoint_udp_hostname)
					a.Zero(c.misc.waypoint_udp_portnum)
					a.Contains(c.output, "Invalid UDP port number")
				},
			},
			{
				name:   "a missing device does not eat the next line",
				config: "WAYPOINT\nMYCALL Q1TEST\n",
				check: func(a *assert.Assertions, c configs) {
					a.Empty(c.misc.waypoint_serial_port)
					a.Equal("Q1TEST", c.audio.mycall[0])
				},
			},
		},
	}
}

func Test_config_directives(t *testing.T) {
	for keyword, cases := range directiveTests() {
		t.Run(keyword, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					tc.check(assert.New(t), parseConfig(t, tc.config))
				})
			}
		})
	}
}

// directivesTestedSeparately names the keywords whose tests predate the table,
// and the test that covers each of them.
func directivesTestedSeparately() map[string]string {
	return map[string]string{
		"AGWLOGIN":    "Test_config_init_agwlogin",
		"AGWPORT":     "Test_config_init_agwport",
		"ARATE":       "Test_config_init_channel",
		"CFILTER":     "Test_config_init_cfilter_syntax_validation",
		"CHANNEL":     "Test_config_init_channel",
		"DNSSD":       "Test_config_init_dnssd",
		"FILTER":      "Test_config_init_filter_syntax_validation",
		"FIX_BITS":    "Test_config_init_fix_bits",
		"FRACK":       "Test_config_init_frack",
		"IL2PVERSION": "Test_config_init_il2pversion",
		"KISSPORT":    "Test_config_init_kissport",
		"METRICSPORT": "Test_config_init_metricsport",
		"MODEM":       "Test_config_init_modem_directive",
		"MYCALL":      "Test_config_init_mycall",
		"PBEACON":     "Test_config_init_pbeacon_no_options",
		"SLOTTIME":    "Test_config_init_slottime",
		"TXDELAY":     "Test_config_init_txdelay",
	}
}

// directivesNotYetTested is the backlog from issue #648: keywords that have no
// tests at all yet.  Entries leave as their tests arrive, and the list goes with
// the last of them.
func directivesNotYetTested() []string {
	return []string{
		"BEACON",
		"CBEACON",
		"IBEACON",
		"OBEACON",
		"SMARTBEACON",
		"SMARTBEACONING",
		"TBEACON",
		"TTAMBIG",
		"TTCMD",
		"TTCORRAL",
		"TTERR",
		"TTGRID",
		"TTMACRO",
		"TTMGRS",
		"TTMHEAD",
		"TTOBJ",
		"TTPOINT",
		"TTSATSQ",
		"TTSTATUS",
		"TTUSNG",
		"TTUTM",
		"TTVECTOR",
	}
}

func Test_config_directive_coverage(t *testing.T) {
	var untested = directivesNotYetTested()
	var tested = directivesTestedSeparately()
	var table = directiveTests()

	var backlog = make(map[string]bool, len(untested))
	for _, keyword := range untested {
		backlog[keyword] = true
	}

	for keyword := range configHandlers {
		switch {
		case len(table[keyword]) > 0:
		case tested[keyword] != "":
		case backlog[keyword]:
			// Awaiting tests - see issue #648.
		default:
			t.Errorf("directive %s has no tests: give it a directiveTests entry", keyword)
		}
	}

	// A name that is covered now, or that is not a directive at all, comes out
	// of the lists, so neither can rot into a hole in the coverage check.
	for _, keyword := range untested {
		assert.Contains(t, configHandlers, keyword,
			"%s is listed as untested but is not a directive", keyword)
		assert.Empty(t, table[keyword],
			"%s has table tests now: take it out of directivesNotYetTested", keyword)
		assert.Empty(t, tested[keyword],
			"%s has tests now: take it out of directivesNotYetTested", keyword)
	}

	for keyword := range tested {
		assert.Contains(t, configHandlers, keyword,
			"%s is listed as tested elsewhere but is not a directive", keyword)
	}
}
