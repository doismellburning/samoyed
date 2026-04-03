package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
