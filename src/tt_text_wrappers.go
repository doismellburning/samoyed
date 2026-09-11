package direwolf

// Lightweight wrappers exporting internal symbols needed by cmd/samoyed-tt2text,
// which was moved out of this package but still needs access to a few
// unexported touch-tone text decoding internals.

// TTEncoding is the touch-tone text encoding that a button sequence appears to use.
type TTEncoding = tt_enc_t

// TTGuessType is a wrapper around tt_guess_type, which guesses whether a button
// sequence uses the multi-press or two-key encoding.
func TTGuessType(buttons string) TTEncoding {
	return tt_guess_type(buttons)
}

// TTMultipressToText is a wrapper around tt_multipress_to_text.
func TTMultipressToText(buttons string, quiet bool) (string, int) {
	return tt_multipress_to_text(buttons, quiet)
}

// TTTwoKeyToText is a wrapper around tt_two_key_to_text.
func TTTwoKeyToText(buttons string, quiet bool) (string, int) {
	return tt_two_key_to_text(buttons, quiet)
}

// TTCall10ToText is a wrapper around tt_call10_to_text.
func TTCall10ToText(buttons string, quiet bool) (string, int) {
	return tt_call10_to_text(buttons, quiet)
}

// TTMheadToText is a wrapper around tt_mhead_to_text.
func TTMheadToText(buttons string, quiet bool) (string, int) {
	return tt_mhead_to_text(buttons, quiet)
}

// TTSatsqToText is a wrapper around tt_satsq_to_text.
func TTSatsqToText(buttons string, quiet bool) (string, int) {
	return tt_satsq_to_text(buttons, quiet)
}
