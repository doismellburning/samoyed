// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

// modemResult is the part of an achan_param_s that the modem options set.
type modemResult struct {
	baud     int
	modem    modem_t
	mark     int
	space    int
	profiles string
	v26      v26_e
	decimate int
	upsample int
	layer2   layer2_t
	fx25     int
	il2pFEC  int
	il2pInv  int
}

func modemResultOf(a *achan_param_s) modemResult {
	return modemResult{
		baud:     a.baud,
		modem:    a.modem_type,
		mark:     a.mark_freq,
		space:    a.space_freq,
		profiles: a.profiles,
		v26:      a.v26_alternative,
		decimate: a.decimate,
		upsample: a.upsample,
		layer2:   a.layer2_xmit,
		fx25:     a.fx25_strength,
		il2pFEC:  a.il2p_max_fec,
		il2pInv:  a.il2p_invert_polarity,
	}
}

// direwolfModemOptions applies args over the modem an empty configuration file gives.
func direwolfModemOptions(t *testing.T, args ...string) (modemResult, error) {
	t.Helper()

	return direwolfModemOptionsOver(t, "", args...)
}

// direwolfModemOptionsOver applies args over the modem the configuration file config gives.
func direwolfModemOptionsOver(t *testing.T, config string, args ...string) (modemResult, error) {
	t.Helper()

	var audio, _ = configFromString(t, config)
	var fs = pflag.NewFlagSet("direwolf", pflag.ContinueOnError)
	var f = addDirewolfModemFlags(fs)
	var parseErr = fs.Parse(args)
	if parseErr != nil {
		return modemResult{}, parseErr
	}

	var err = f.apply(&audio.achan[0])

	return modemResultOf(&audio.achan[0]), err
}

// atestModemOptions applies args over atest's defaults.
func atestModemOptions(t *testing.T, args ...string) (modemResult, error) {
	t.Helper()

	var audio = atestDefaultAudio()
	var fs = pflag.NewFlagSet("atest", pflag.ContinueOnError)
	var f = addAtestModemFlags(fs)
	var parseErr = fs.Parse(args)
	if parseErr != nil {
		return modemResult{}, parseErr
	}

	var err = f.apply(&audio.achan[0])
	if err == nil {
		atestSingleSlicer(&audio.achan[0])
	}

	return modemResultOf(&audio.achan[0]), err
}

// genPacketsModemOptions applies args over gen_packets' defaults.
func genPacketsModemOptions(t *testing.T, args ...string) (modemResult, error) {
	t.Helper()

	var audio = genPacketsDefaultAudio()
	var fs = pflag.NewFlagSet("gen_packets", pflag.ContinueOnError)
	var f = addGenPacketsModemFlags(fs)
	var parseErr = fs.Parse(args)
	if parseErr != nil {
		return modemResult{}, parseErr
	}

	var err = f.apply(&audio.achan[0])

	return modemResultOf(&audio.achan[0]), err
}

// configModem is the modem a configuration file's MODEM line gives.
func configModem(t *testing.T, line string) modemResult {
	t.Helper()

	var audio, _ = configFromString(t, line+"\n")

	return modemResultOf(&audio.achan[0])
}

func modemName(m modem_t) string {
	switch m {
	case MODEM_AFSK:
		return "AFSK"
	case MODEM_BASEBAND:
		return "BASEBAND"
	case MODEM_SCRAMBLE:
		return "SCRAMBLE"
	case MODEM_QPSK:
		return "QPSK"
	case MODEM_8PSK:
		return "8PSK"
	case MODEM_OFF:
		return "OFF"
	case MODEM_16_QAM:
		return "16QAM"
	case MODEM_64_QAM:
		return "64QAM"
	case MODEM_AIS:
		return "AIS"
	case MODEM_EAS:
		return "EAS"
	case MODEM_BPSK:
		return "BPSK"
	default:
		return fmt.Sprintf("modem_t(%d)", int(m))
	}
}

func layer2Name(l layer2_t) string {
	switch l {
	case LAYER2_AX25:
		return "AX25"
	case LAYER2_FX25:
		return "FX25"
	case LAYER2_IL2P:
		return "IL2P"
	default:
		return fmt.Sprintf("layer2_t(%d)", int(l))
	}
}

func v26Name(v v26_e) string {
	switch v {
	case V26_UNSPECIFIED:
		return "-"
	case V26_A:
		return "A"
	case V26_B:
		return "B"
	default:
		return fmt.Sprintf("v26_e(%d)", int(v))
	}
}

// String gives a modemResult on one line, so a table of them reads easily.
func (r modemResult) String() string {
	return fmt.Sprintf("%d %s %d/%d profiles=%q v26=%s D=%d U=%d %s fx25=%d fec=%d inv=%d",
		r.baud, modemName(r.modem), r.mark, r.space, r.profiles, v26Name(r.v26),
		r.decimate, r.upsample, layer2Name(r.layer2), r.fx25, r.il2pFEC, r.il2pInv)
}

// describe gives a result, or the error in its place.
func describe(r modemResult, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}

	return r.String()
}

// TestModemOptions pins down what each program makes of its modem options.
// A program missing from want doesn't take the options at all.
func TestModemOptions(t *testing.T) {
	// The programs that take modem options, by the name the table uses.
	var tools = map[string]func(*testing.T, ...string) (modemResult, error){
		"direwolf":    direwolfModemOptions,
		"atest":       atestModemOptions,
		"gen_packets": genPacketsModemOptions,
	}

	var tests = []struct {
		args []string
		want map[string]string
	}{
		{
			args: []string{},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "1200 AFSK 1200/2200 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "100"},
			want: map[string]string{
				"direwolf":    "100 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "100 AFSK 1600/1800 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "100 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "300"},
			want: map[string]string{
				"direwolf":    "300 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "300 AFSK 1600/1800 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "300 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "1200"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "1200 AFSK 1200/2200 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "2400"},
			want: map[string]string{
				"direwolf":    "2400 QPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "2400 QPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "error: either -j or -J must be specified when using 2400 bps QPSK",
			},
		},
		{
			args: []string{"-B", "3000"},
			want: map[string]string{
				"direwolf":    "3000 QPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "3000 QPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "error: either -j or -J must be specified when using 2400 bps QPSK",
			},
		},
		{
			args: []string{"-B", "4800"},
			want: map[string]string{
				"direwolf":    "4800 8PSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "4800 8PSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "4800 8PSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "9600"},
			want: map[string]string{
				"direwolf":    "9600 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "9600 SCRAMBLE 0/0 profiles=\"-\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "9600 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "19200"},
			want: map[string]string{
				"direwolf":    "19200 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "19200 SCRAMBLE 0/0 profiles=\"-\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "19200 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "AIS"},
			want: map[string]string{
				"direwolf":    "9600 AIS 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "9600 AIS 0/0 profiles=\"-\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "9600 AIS 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "EAS"},
			want: map[string]string{
				"direwolf":    "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "ais"},
			want: map[string]string{
				"direwolf":    "9600 AIS 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "9600 AIS 0/0 profiles=\"-\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "9600 AIS 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "eas"},
			want: map[string]string{
				"direwolf":    "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "fish"},
			want: map[string]string{
				"direwolf":    "error: invalid bitrate (should be an integer or 'AIS' or 'EAS'): fish",
				"atest":       "error: invalid bitrate (should be an integer or 'AIS' or 'EAS'): fish",
				"gen_packets": "error: invalid bitrate (should be an integer or 'AIS' or 'EAS'): fish",
			},
		},
		{
			args: []string{"-B", "50"},
			want: map[string]string{
				"direwolf":    "error: use a more reasonable bit rate in range of 100 - 40000",
				"atest":       "error: use a more reasonable bit rate in range of 100 - 40000",
				"gen_packets": "error: use a more reasonable bit rate in range of 100 - 40000",
			},
		},
		{
			args: []string{"-B", "50000"},
			want: map[string]string{
				"direwolf":    "error: use a more reasonable bit rate in range of 100 - 40000",
				"atest":       "error: use a more reasonable bit rate in range of 100 - 40000",
				"gen_packets": "error: use a more reasonable bit rate in range of 100 - 40000",
			},
		},
		{
			args: []string{"-B", "2400", "-g"},
			want: map[string]string{
				"direwolf":    "2400 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "2400 SCRAMBLE 0/0 profiles=\"-\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "2400 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "1200", "-g"},
			want: map[string]string{
				"direwolf":    "1200 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "1200 SCRAMBLE 0/0 profiles=\"-\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "1200 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "300", "-k"},
			want: map[string]string{
				"direwolf":    "300 BPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "300 BPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "300 BPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-j"},
			want: map[string]string{
				"direwolf":    "2400 QPSK 0/0 profiles=\"\" v26=A D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "2400 QPSK 0/0 profiles=\"\" v26=A D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "2400 QPSK 0/0 profiles=\"\" v26=A D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-J"},
			want: map[string]string{
				"direwolf":    "2400 QPSK 0/0 profiles=\"\" v26=B D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "2400 QPSK 0/0 profiles=\"\" v26=B D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "2400 QPSK 0/0 profiles=\"\" v26=B D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "2400", "-J"},
			want: map[string]string{
				"direwolf":    "2400 QPSK 0/0 profiles=\"\" v26=B D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":       "2400 QPSK 0/0 profiles=\"\" v26=B D=0 U=0 AX25 fx25=0 fec=0 inv=0",
				"gen_packets": "2400 QPSK 0/0 profiles=\"\" v26=B D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-P", "E+"},
			want: map[string]string{
				"direwolf": "1200 AFSK 1200/2200 profiles=\"E+\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":    "1200 AFSK 1200/2200 profiles=\"E+\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-B", "9600", "-P", "E+"},
			want: map[string]string{
				"direwolf": "9600 SCRAMBLE 0/0 profiles=\"E+\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":    "9600 SCRAMBLE 0/0 profiles=\"E+\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-D", "3"},
			want: map[string]string{
				"direwolf": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=3 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":    "1200 AFSK 1200/2200 profiles=\"A\" v26=- D=3 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-D", "0"},
			want: map[string]string{
				"direwolf": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0",
				"atest":    "1200 AFSK 1200/2200 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-D", "9"},
			want: map[string]string{
				"direwolf": "error: crazy value for -D: 9",
				"atest":    "error: decimate should be between 0 and 8 inclusive, not 9",
			},
		},
		{
			args: []string{"-D", "-1"},
			want: map[string]string{
				"direwolf": "error: crazy value for -D: -1",
				"atest":    "error: decimate should be between 0 and 8 inclusive, not -1",
			},
		},
		{
			args: []string{"-U", "2"},
			want: map[string]string{
				"direwolf": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=2 AX25 fx25=0 fec=1 inv=0",
				"atest":    "1200 AFSK 1200/2200 profiles=\"A\" v26=- D=0 U=2 AX25 fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-U", "5"},
			want: map[string]string{
				"direwolf": "error: crazy value for -U: 5",
				"atest":    "error: upsample should be between 1 and 4 inclusive, not 5",
			},
		},
		{
			args: []string{"-U", "9"},
			want: map[string]string{
				"direwolf": "error: crazy value for -U: 9",
				"atest":    "error: upsample should be between 1 and 4 inclusive, not 9",
			},
		},
		{
			args: []string{"-X", "16"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 FX25 fx25=16 fec=1 inv=0",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 FX25 fx25=16 fec=0 inv=0",
			},
		},
		{
			args: []string{"-I", "1"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=0",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=0",
			},
		},
		{
			args: []string{"-I", "0"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=0 inv=0",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=0 inv=0",
			},
		},
		{
			args: []string{"-i", "0"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=0 inv=1",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=0 inv=1",
			},
		},
		{
			args: []string{"-i", "1"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=1",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=1",
			},
		},
		{
			args: []string{"-B", "1200", "-i", "1"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=1",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=1",
			},
		},
		{
			args: []string{"-X", "16", "-I", "1"},
			want: map[string]string{
				"direwolf":    "error: can't mix -X with -I or -i",
				"gen_packets": "error: can't mix -X with -I or -i",
			},
		},
		{
			args: []string{"-I", "1", "-i", "1"},
			want: map[string]string{
				"direwolf":    "error: can't use both -I and -i at the same time",
				"gen_packets": "error: can't use both -I and -i at the same time",
			},
		},
		{
			args: []string{"-I", "1", "-i", "-2"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=0",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=0",
			},
		},
		{
			args: []string{"-X", "16", "-I", "-2"},
			want: map[string]string{
				"direwolf":    "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 FX25 fx25=16 fec=1 inv=0",
				"gen_packets": "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 FX25 fx25=16 fec=0 inv=0",
			},
		},
	}

	for _, tt := range tests {
		for tool, want := range tt.want {
			t.Run(tool+" "+strings.Join(tt.args, " "), func(t *testing.T) {
				var r, err = tools[tool](t, tt.args...)
				assert.Equal(t, want, describe(r, err))
			})
		}
	}
}

// TestConfigModem pins down what a MODEM line in the configuration file gives.
func TestConfigModem(t *testing.T) {
	var tests = []struct {
		line string
		want string
	}{
		{"MODEM 100", "100 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 300", "300 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200", "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 2400", "2400 QPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 4800", "4800 8PSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 9600", "9600 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 19200", "19200 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM AIS", "9600 AIS 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM ais", "9600 AIS 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM EAS", "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM EAS G3RUH", "521 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM EAS BPSK", "521 BPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM EAS 0:0", "521 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM EAS 1600:1800", "521 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM EAS B", "521 EAS 2083/1563 profiles=\"B\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 50", "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			assert.Equal(t, tt.want, configModem(t, tt.line).String())
		})
	}
}

// TestDirewolfModemOptionsOverConfig checks what direwolf's options leave
// of a modem the configuration file set up.
func TestDirewolfModemOptionsOverConfig(t *testing.T) {
	var tests = []struct {
		config string
		args   []string
		want   string
	}{
		// A demodulator profile belongs to the modem it was chosen for, so
		// one that picks another modem starts afresh.
		{"MODEM 1200 E+\n", []string{}, "1200 AFSK 1200/2200 profiles=\"E+\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-B", "300"}, "300 AFSK 1600/1800 profiles=\"E+\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-B", "2400"}, "2400 QPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-B", "4800"}, "4800 8PSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-B", "9600"}, "9600 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-B", "AIS"}, "9600 AIS 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-B", "EAS"}, "521 EAS 2083/1563 profiles=\"A\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-g"}, "1200 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-k"}, "1200 BPSK 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-j"}, "2400 QPSK 0/0 profiles=\"\" v26=A D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-J"}, "2400 QPSK 0/0 profiles=\"\" v26=B D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 2400 PQRS\n", []string{"-B", "1200"}, "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 4800 TUVW\n", []string{"-B", "300"}, "300 AFSK 1600/1800 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM EAS\n", []string{"-B", "1200"}, "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 E+\n", []string{"-B", "9600", "-P", "+"}, "9600 SCRAMBLE 0/0 profiles=\"+\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},

		// 0 asks for the automatic choice, over one the configuration file made.
		{"MODEM 1200 /3\n", []string{}, "1200 AFSK 1200/2200 profiles=\"\" v26=- D=3 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 1200 /3\n", []string{"-D", "0"}, "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 9600 *2\n", []string{}, "9600 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=2 AX25 fx25=0 fec=1 inv=0"},
		{"MODEM 9600 *2\n", []string{"-U", "0"}, "9600 SCRAMBLE 0/0 profiles=\"\" v26=- D=0 U=0 AX25 fx25=0 fec=1 inv=0"},

		// -I and -i choose the FEC strength whichever way IL2PTX went.
		{"IL2PTX 0\n", []string{"-I", "1"}, "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=1 inv=0"},
		{"IL2PTX 1\n", []string{"-I", "0"}, "1200 AFSK 1200/2200 profiles=\"\" v26=- D=0 U=0 IL2P fx25=0 fec=0 inv=0"},
	}

	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.config)+" "+strings.Join(tt.args, " "), func(t *testing.T) {
			var r, err = direwolfModemOptionsOver(t, tt.config, tt.args...)
			assert.Equal(t, tt.want, describe(r, err))
		})
	}
}
