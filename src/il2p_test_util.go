//nolint:gochecknoglobals
package direwolf

/*-------------------------------------------------------------
 *
 * Purpose:	Mock functions for unit tests for IL2P protocol functions.
 *
 * Errors:	Die if anything goes wrong.
 *
 *--------------------------------------------------------------*/

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test serialize / deserialize.
//
//	This uses same functions used on the air.
//
/////////////////////////////////////////////////////////////////////////////////////////////

var IL2P_TEST = false

const il2pTestText = `'... As I was saying, that seems to be done right - though I haven't time to look it over thoroughly just now - and that shows that there are three hundred and sixty-four days when you might get un-birthday presents -'
'Certainly,' said Alice.
'And only one for birthday presents, you know. There's glory for you!'
'I don't know what you mean by \"glory\",' Alice said.
Humpty Dumpty smiled contemptuously. 'Of course you don't - till I tell you. I meant \"there's a nice knock-down argument for you!\"'
'But \"glory\" doesn't mean \"a nice knock-down argument\",' Alice objected.
'When I use a word,' Humpty Dumpty said, in rather a scornful tone, 'it means just what I choose it to mean - neither more nor less.'
'The question is,' said Alice, 'whether you can make words mean so many different things.'
'The question is,' said Humpty Dumpty, 'which is to be master - that's all.'
`

var il2pSerdesRecCount = -1 // disable deserialized packet test.
var il2pSerdesPolarity = 0

// Serializing calls this which then simulates the demodulator output.

func tone_gen_put_bit_fake(channel int, data int) {
	il2p_rec_bit(channel, 0, 0, data)
}

func tone_gen_put_bit(channel int, data int) {
	if IL2P_TEST {
		tone_gen_put_bit_fake(channel, data)
	} else {
		tone_gen_put_bit_real(channel, data)
	}
}

// This is called when a complete frame has been deserialized.

func multi_modem_process_rec_packet_fake(channel int, subchannel int, slice int, pp *packet_t, alevel ALevel, retries BitFixLevel, fec_type fec_type_t) { //nolint:unparam
	if il2pSerdesRecCount < 0 {
		return // Skip check before serdes test.
	}

	il2pSerdesRecCount++

	// Does it have the the expected content?

	var pinfo = AX25GetInfo(pp)
	Assert(len(il2pTestText) == len(pinfo))
	Assert(il2pTestText == string(pinfo))

	dw_printf("Number of symbols corrected: %d\n", retries)

	if il2pSerdesPolarity == 2 { // expecting errors corrected.
		Assert(retries == 10)
	} else { // should be no errors.
		Assert(retries == 0)
	}

	ax25_delete(pp)
}

func multi_modem_process_rec_packet(channel int, subchannel int, slice int, pp *packet_t, alevel ALevel, retries BitFixLevel, fec_type fec_type_t) {
	if IL2P_TEST {
		multi_modem_process_rec_packet_fake(channel, subchannel, slice, pp, alevel, retries, fec_type)
	} else {
		multi_modem_process_rec_packet_real(channel, subchannel, slice, pp, alevel, retries, fec_type)
	}
}

func demod_get_audio_level_fake(channel int, subchannel int) ALevel {
	var alevel ALevel
	return (alevel)
}

func demod_get_audio_level(channel int, subchannel int) ALevel {
	if IL2P_TEST {
		return demod_get_audio_level_fake(channel, subchannel)
	} else {
		return demod_get_audio_level_real(channel, subchannel)
	}
}
