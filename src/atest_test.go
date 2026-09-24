package direwolf

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildWAVWithExtraChunks constructs a minimal valid mono 8-bit PCM WAV file
// whose RIFF body contains:
//
//   - an odd-sized JUNK chunk before "fmt " (exercises chunk-skip + odd padding)
//   - a "fmt " chunk
//   - an even-sized LIST chunk between "fmt " and "data" (exercises chunk-skip)
//   - a "data" chunk with silent PCM samples
func buildWAVWithExtraChunks(t *testing.T) []byte {
	t.Helper()

	var body bytes.Buffer

	writeChunk := func(id string, payload []byte) {
		body.WriteString(id)
		binary.Write(&body, binary.LittleEndian, int32(len(payload))) //nolint:errcheck
		body.Write(payload)

		if len(payload)%2 != 0 {
			body.WriteByte(0) // RIFF word-alignment pad
		}
	}

	// Odd-sized extra chunk before "fmt ": Datasize=3 forces the seek to
	// advance by 3+1=4 bytes so the next chunk header is aligned.
	writeChunk("JUNK", make([]byte, 3))

	// "fmt " chunk — 16-byte PCM descriptor.
	var fmtPayload bytes.Buffer
	binary.Write(&fmtPayload, binary.LittleEndian, int16(1))    //nolint:errcheck // PCM
	binary.Write(&fmtPayload, binary.LittleEndian, int16(1))    //nolint:errcheck // mono
	binary.Write(&fmtPayload, binary.LittleEndian, int32(8000)) //nolint:errcheck // 8 kHz
	binary.Write(&fmtPayload, binary.LittleEndian, int32(8000)) //nolint:errcheck // avg bytes/sec
	binary.Write(&fmtPayload, binary.LittleEndian, int16(1))    //nolint:errcheck // block align
	binary.Write(&fmtPayload, binary.LittleEndian, int16(8))    //nolint:errcheck // 8 bits/sample
	writeChunk("fmt ", fmtPayload.Bytes())

	// Even-sized extra chunk between "fmt " and "data".
	writeChunk("LIST", make([]byte, 4))

	// Minimal silent PCM audio (0.1 s at 8 kHz).
	writeChunk("data", make([]byte, 800))

	var wav bytes.Buffer
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, int32(4+body.Len())) //nolint:errcheck
	wav.WriteString("WAVE")
	wav.Write(body.Bytes())

	return wav.Bytes()
}

// Test_atest_fixBits covers the mapping of -F onto the fix_bits level and the
// PASSALL flag.  The top value used to be accepted as a fix_bits level in its
// own right - one past the highest real level, so it behaved exactly like the
// level below it - and never set passall, leaving no way to ask atest for the
// behaviour that value is named after.
func Test_atest_fixBits(t *testing.T) {
	var testCases = []struct {
		arg     int
		level   BitFixLevel
		passall bool
		valid   bool
	}{
		{-1, DEFAULT_FIX_BITS, false, false},
		{0, BitFixNone, false, true},
		{1, BitFixSingle, false, true},
		{int(BitFixLevelHighest), BitFixLevelHighest, false, true},
		{int(BitFixPassall), BitFixLevelHighest, true, true},
		{int(BitFixPassall) + 1, DEFAULT_FIX_BITS, false, false},
	}

	for _, testCase := range testCases {
		var level, passall, valid = atestFixBits(testCase.arg)

		assert.Equal(t, testCase.valid, valid, "-F %d validity", testCase.arg)
		assert.Equal(t, testCase.level, level, "-F %d level", testCase.arg)
		assert.Equal(t, testCase.passall, passall, "-F %d passall", testCase.arg)

		// Whatever -F is given, fix_bits only ever holds a level that the
		// FIX_BITS configuration keyword would also accept.
		assert.LessOrEqual(t, level, BitFixLevelHighest, "-F %d level in range", testCase.arg)
	}
}

// Test_atest_newAtestRejects covers the options NewAtest turns down, which
// used to be reported and exited on in the middle of parsing the command line.
func Test_atest_newAtestRejects(t *testing.T) {
	var testCases = map[string]func(*AtestOptions){
		"fix bits":     func(o *AtestOptions) { o.FixBits = int(BitFixPassall) + 1 },
		"il2p version": func(o *AtestOptions) { o.IL2PVersion = "0.5" },
		"channel":      func(o *AtestOptions) { o.DecodeOnly = 3 },
	}

	for name, modify := range testCases {
		t.Run(name, func(t *testing.T) {
			var opts = atestTestOptions()
			modify(opts)

			var _, err = NewAtest(opts)
			assert.Error(t, err)
		})
	}
}

// Test_atest_decodeWAVErrors checks that a file DecodeWAV can't make sense of
// comes back as an error rather than ending the program.
func Test_atest_decodeWAVErrors(t *testing.T) {
	var atest, err = NewAtest(atestTestOptions())
	require.NoError(t, err)

	var wav = buildWAVWithExtraChunks(t)

	var testCases = map[string][]byte{
		"empty":     {},
		"not RIFF":  append([]byte("RIFX"), wav[4:]...),
		"truncated": wav[:20],
	}

	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			var _, err = atest.DecodeWAV(bytes.NewReader(data), name)
			assert.Error(t, err)
		})
	}
}

// Test_atest_decodeWAV decodes a file without going through the command line.
func Test_atest_decodeWAV(t *testing.T) {
	var atest, err = NewAtest(atestTestOptions())
	require.NoError(t, err)

	var result, decodeErr = atest.DecodeWAV(bytes.NewReader(buildWAVWithExtraChunks(t)), "extra_chunks.wav")
	require.NoError(t, decodeErr)

	assert.Equal(t, 0, result.PacketsDecoded)
	assert.InDelta(t, 0.1, result.Seconds, 1e-9)
}

// atestTestOptions are atest's options with nothing given on the command line.
func atestTestOptions() *AtestOptions {
	var opts = new(AtestOptions)
	opts.IL2PVersion = "0.6"

	return opts
}
