package direwolf

// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <assert.h>
import "C"

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

// Serializing calls this which then simulates the demodulator output.

func tone_gen_put_bit_fake(channel int, data int) {
	il2p_rec_bit(C.int(channel), 0, 0, C.int(data))
}

func tone_gen_put_bit(channel int, data int) {
	if IL2P_TEST {
		tone_gen_put_bit_fake(channel, data)
	} else {
		tone_gen_put_bit_real(channel, data)
	}
}

// This is called when a complete frame has been deserialized.

func multi_modem_process_rec_packet_fake(channel C.int, subchannel C.int, slice C.int, pp *packet_t, alevel alevel_t, retries retry_t, fec_type fec_type_t) {
	if rec_count < 0 {
		return // Skip check before serdes test.
	}

	rec_count++

	// Does it have the the expected content?

	var pinfo = ax25_get_info(pp)
	Assert(len(text) == len(pinfo))
	Assert(text == string(pinfo))

	dw_printf("Number of symbols corrected: %d\n", retries)
	if polarity == 2 { // expecting errors corrected.
		Assert(retries == 10)
	} else { // should be no errors.
		Assert(retries == 0)
	}

	ax25_delete(pp)
}

func multi_modem_process_rec_packet(channel C.int, subchannel C.int, slice C.int, pp *packet_t, alevel alevel_t, retries retry_t, fec_type fec_type_t) {
	if IL2P_TEST {
		multi_modem_process_rec_packet_fake(channel, subchannel, slice, pp, alevel, retries, fec_type)
	} else {
		multi_modem_process_rec_packet_real(channel, subchannel, slice, pp, alevel, retries, fec_type)
	}
}

func demod_get_audio_level_fake(channel C.int, subchannel C.int) alevel_t {
	var alevel alevel_t
	return (alevel)
}

func demod_get_audio_level(channel C.int, subchannel C.int) alevel_t {
	if IL2P_TEST {
		return demod_get_audio_level_fake(channel, subchannel)
	} else {
		return demod_get_audio_level_real(channel, subchannel)
	}
}
