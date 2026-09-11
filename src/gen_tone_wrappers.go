package direwolf

// Lightweight wrappers exporting internal symbols needed by
// cmd/samoyed-gen_tone, which was moved out of this package but still needs
// to drive audio output and tone generation.

// GenToneTestOpen opens the default audio device and initialises tone
// generation, without exposing audio_s. numChannels is the number of sound
// card channels, and mediumRadio marks radio channel 0 as MEDIUM_RADIO.
// It returns the baud rate configured for each radio channel.
func GenToneTestOpen(numChannels int, mediumRadio bool) [MAX_RADIO_CHANS]int {
	var my_audio_config audio_s

	my_audio_config.adev[0].adevice_in = DEFAULT_ADEVICE
	my_audio_config.adev[0].adevice_out = DEFAULT_ADEVICE
	my_audio_config.adev[0].num_channels = numChannels

	if mediumRadio {
		my_audio_config.chan_medium[0] = MEDIUM_RADIO // TODO KG ??
	}

	audio_open(&my_audio_config)
	gen_tone_init(&my_audio_config, 100, false)

	var baud [MAX_RADIO_CHANS]int
	for channel := range baud {
		baud[channel] = my_audio_config.achan[channel].baud
	}

	return baud
}

// GenToneTestPutBit is a wrapper around tone_gen_put_bit.
func GenToneTestPutBit(channel int, dat int) {
	tone_gen_put_bit(channel, dat)
}

// GenToneTestClose is a wrapper around audio_close.
func GenToneTestClose() {
	audio_close()
}
