package direwolf

import (
	"os"
	"testing"

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

// configFromString writes content to a temp config file, runs config_init, and
// returns the resulting audio and misc config structs.
func configFromString(t *testing.T, content string) (*audio_s, *misc_config_s) {
	t.Helper()

	var tmpFile, err = os.CreateTemp(t.TempDir(), "direwolf*.conf")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	var audioConfig = new(audio_s)
	var digiConfig digi_config_s
	var cdigiConfig cdigi_config_s
	var ttConfig tt_config_s
	var igateConfig igate_config_s
	var miscConfig misc_config_s
	var netromConfig netrom_config_s

	config_init(tmpFile.Name(), audioConfig, &digiConfig, &cdigiConfig,
		&ttConfig, &igateConfig, &miscConfig, &netromConfig)

	return audioConfig, &miscConfig
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
	})

	t.Run("two args set in and out independently", func(t *testing.T) {
		var cfg, _ = configFromString(t, "ADEVICE hw:0,0 hw:1,0\n")
		assert.Equal(t, "hw:0,0", cfg.adev[0].adevice_in)
		assert.Equal(t, "hw:1,0", cfg.adev[0].adevice_out)
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

			var netromConfig netrom_config_s
			config_init(tmpFile.Name(), audioConfig, &digiConfig, &cdigiConfig,
				&ttConfig, &igateConfig, &miscConfig, &netromConfig)

			assert.Equal(t, tt.wantBaud, audioConfig.achan[0].baud)
			assert.Equal(t, tt.wantModemType, audioConfig.achan[0].modem_type)
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
