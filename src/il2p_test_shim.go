package direwolf

// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <assert.h>
import "C"

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

/*-------------------------------------------------------------
 *
 * Purpose:	Unit tests for IL2P protocol functions.
 *
 * Errors:	Die if anything goes wrong.
 *
 *--------------------------------------------------------------*/

func il2p_test_main(t *testing.T) {
	t.Helper()

	IL2P_TEST = true

	var enable_color = 1
	text_color_init(enable_color)

	var enable_debug_out = 0
	il2p_init(enable_debug_out)

	fmt.Println("Begin IL2P unit tests.")

	// These start simple and later complex cases build upon earlier successes.

	// Test scramble and descramble.

	test_scramble(t)

	// Test Reed Solomon error correction.

	test_rs(t)

	// Test payload functions.

	test_payload(t)

	// Try encoding the example headers in the protocol spec.

	test_example_headers(t)

	// Convert all of the AX.25 frame types to IL2P and back again.

	all_frame_types(t)

	// Use same serialize / deserialize functions used on the air.

	test_serdes(t)

	// Decode bitstream from demodulator if data file is available.
	// TODO:  Very large info parts.  Appropriate error if too long.
	// TODO:  More than 2 addresses.

	decode_bitstream(t)

	IL2P_TEST = false
}

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test scrambling and descrambling.
//
/////////////////////////////////////////////////////////////////////////////////////////////

func test_scramble(t *testing.T) {
	t.Helper()

	fmt.Println("Test scrambling...")

	// First an example from the protocol specification to make sure I'm compatible.

	var scramin1 []C.uchar = []C.uchar{0x63, 0xf1, 0x40, 0x40, 0x40, 0x00, 0x6b, 0x2b, 0x54, 0x28, 0x25, 0x2a, 0x0f}
	var scramout1 []C.uchar = []C.uchar{0x6a, 0xea, 0x9c, 0xc2, 0x01, 0x11, 0xfc, 0x14, 0x1f, 0xda, 0x6e, 0xf2, 0x53}
	var scramout []C.uchar = make([]C.uchar, len(scramin1))

	il2p_scramble_block(&scramin1[0], &scramout[0], C.int(len(scramin1)))
	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&scramout[0]), unsafe.Pointer(&scramout1[0]), C.ulong(len(scramout1))))
} // end test_scramble.

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test Reed Solomon encode/decode examples found in the protocol spec.
//	The data part is scrambled but that does not matter here because.
//	We are only concerned abound adding the parity and verifying.
//
/////////////////////////////////////////////////////////////////////////////////////////////

func test_rs(t *testing.T) {
	t.Helper()

	fmt.Println("Test Reed Solomon functions...")

	var example_s []C.uchar = []C.uchar{0x26, 0x57, 0x4d, 0x57, 0xf1, 0x96, 0xcc, 0x85, 0x42, 0xe7, 0x24, 0xf7, 0x2e, 0x8a, 0x97}
	var parity_out [2]C.uchar

	il2p_encode_rs(&example_s[0], 13, 2, &parity_out[0])
	// dw_printf ("DEBUG RS encode %02x %02x\n", parity_out[0], parity_out[1]);
	assert.Equal(t, example_s[13], parity_out[0])
	assert.Equal(t, example_s[14], parity_out[1])

	var example_u []C.uchar = []C.uchar{0x6a, 0xea, 0x9c, 0xc2, 0x01, 0x11, 0xfc, 0x14, 0x1f, 0xda, 0x6e, 0xf2, 0x53, 0x91, 0xbd}
	il2p_encode_rs(&example_u[0], 13, 2, &parity_out[0])
	// dw_printf ("DEBUG RS encode %02x %02x\n", parity_out[0], parity_out[1]);
	assert.Equal(t, example_u[13], parity_out[0])
	assert.Equal(t, example_u[14], parity_out[1])

	// See if we can go the other way.

	var received [15]C.uchar
	var corrected [15]C.uchar
	var e C.int

	e = il2p_decode_rs(&example_s[0], 13, 2, &corrected[0])
	assert.Zero(t, e)
	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&example_s[0]), unsafe.Pointer(&corrected[0]), 13))

	C.memcpy(unsafe.Pointer(&received[0]), unsafe.Pointer(&example_s[0]), 15)
	received[0] = '?'
	e = il2p_decode_rs(&received[0], 13, 2, &corrected[0])
	assert.Equal(t, C.int(1), e)
	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&example_s[0]), unsafe.Pointer(&corrected[0]), 13))

	e = il2p_decode_rs(&example_u[0], 13, 2, &corrected[0])
	assert.Zero(t, e)
	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&example_u[0]), unsafe.Pointer(&corrected[0]), 13))

	C.memcpy(unsafe.Pointer(&received[0]), unsafe.Pointer(&example_u[0]), 15)
	received[12] = '?'
	e = il2p_decode_rs(&received[0], 13, 2, &corrected[0])
	assert.Equal(t, C.int(1), e)
	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&example_u[0]), unsafe.Pointer(&corrected[0]), 13))

	received[1] = '?'
	received[2] = '?'
	e = il2p_decode_rs(&received[0], 13, 2, &corrected[0])
	assert.Equal(t, C.int(-1), e)
}

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test payload functions.
//
/////////////////////////////////////////////////////////////////////////////////////////////

func test_payload(t *testing.T) {
	t.Helper()

	fmt.Println("Test payload functions...")

	var e C.int
	var ipp *il2p_payload_properties_t

	// Examples in specification.

	ipp, e = il2p_payload_compute(100, 0)
	assert.Equal(t, C.int(100), ipp.small_block_size)
	assert.Equal(t, C.int(101), ipp.large_block_size)
	assert.Equal(t, C.int(0), ipp.large_block_count)
	assert.Equal(t, C.int(1), ipp.small_block_count)
	assert.Equal(t, C.int(4), ipp.parity_symbols_per_block)
	assert.GreaterOrEqual(t, e, C.int(0))

	ipp, e = il2p_payload_compute(236, 0)
	assert.Equal(t, C.int(236), ipp.small_block_size)
	assert.Equal(t, C.int(237), ipp.large_block_size)
	assert.Equal(t, C.int(0), ipp.large_block_count)
	assert.Equal(t, C.int(1), ipp.small_block_count)
	assert.Equal(t, C.int(8), ipp.parity_symbols_per_block)
	assert.GreaterOrEqual(t, e, C.int(0))

	ipp, e = il2p_payload_compute(512, 0)
	assert.Equal(t, C.int(170), ipp.small_block_size)
	assert.Equal(t, C.int(171), ipp.large_block_size)
	assert.Equal(t, C.int(2), ipp.large_block_count)
	assert.Equal(t, C.int(1), ipp.small_block_count)
	assert.Equal(t, C.int(6), ipp.parity_symbols_per_block)
	assert.GreaterOrEqual(t, e, C.int(0))

	ipp, e = il2p_payload_compute(1023, 0)
	assert.Equal(t, C.int(204), ipp.small_block_size)
	assert.Equal(t, C.int(205), ipp.large_block_size)
	assert.Equal(t, C.int(3), ipp.large_block_count)
	assert.Equal(t, C.int(2), ipp.small_block_count)
	assert.Equal(t, C.int(8), ipp.parity_symbols_per_block)
	assert.GreaterOrEqual(t, e, C.int(0))

	// Now try all possible sizes for Baseline FEC Parity.

	for n := C.int(1); n <= IL2P_MAX_PAYLOAD_SIZE; n++ {
		ipp, e = il2p_payload_compute(n, 0)
		// dw_printf ("bytecount=%d, smallsize=%d, largesize=%d, largecount=%d, smallcount=%d\n", n,
		//		ipp.small_block_size, ipp.large_block_size,
		//		ipp.large_block_count, ipp.small_block_count);
		// fflush (stdout);

		assert.GreaterOrEqual(t, e, C.int(0))
		assert.GreaterOrEqual(t, ipp.payload_block_count, C.int(1))
		assert.LessOrEqual(t, ipp.payload_block_count, C.int(IL2P_MAX_PAYLOAD_BLOCKS))
		assert.Equal(t, ipp.small_block_count+ipp.large_block_count, ipp.payload_block_count)
		assert.Equal(t, n, ipp.small_block_count*ipp.small_block_size+ipp.large_block_count*ipp.large_block_size)
		assert.True(t, ipp.parity_symbols_per_block == 2 ||
			ipp.parity_symbols_per_block == 4 ||
			ipp.parity_symbols_per_block == 6 ||
			ipp.parity_symbols_per_block == 8)

		// Data and parity must fit in RS block size of 255.
		// Size test does not apply if block count is 0.
		assert.True(t, ipp.small_block_count == 0 || ipp.small_block_size+ipp.parity_symbols_per_block <= 255)
		assert.True(t, ipp.large_block_count == 0 || ipp.large_block_size+ipp.parity_symbols_per_block <= 255)
	}

	// All sizes for MAX FEC.

	for n := C.int(1); n <= IL2P_MAX_PAYLOAD_SIZE; n++ {
		ipp, e = il2p_payload_compute(n, 1) // 1 for max fec.
		// dw_printf ("bytecount=%d, smallsize=%d, largesize=%d, largecount=%d, smallcount=%d\n", n,
		//		ipp.small_block_size, ipp.large_block_size,
		//		ipp.large_block_count, ipp.small_block_count);
		// fflush (stdout);

		assert.GreaterOrEqual(t, e, C.int(0))
		assert.GreaterOrEqual(t, ipp.payload_block_count, C.int(1))
		assert.LessOrEqual(t, ipp.payload_block_count, C.int(IL2P_MAX_PAYLOAD_BLOCKS))
		assert.Equal(t, ipp.small_block_count+ipp.large_block_count, ipp.payload_block_count)
		assert.Equal(t, ipp.small_block_count*ipp.small_block_size+
			ipp.large_block_count*ipp.large_block_size, n)
		assert.Equal(t, C.int(16), ipp.parity_symbols_per_block)

		// Data and parity must fit in RS block size of 255.
		// Size test does not apply if block count is 0.
		assert.True(t, ipp.small_block_count == 0 || ipp.small_block_size+ipp.parity_symbols_per_block <= 255)
		assert.True(t, ipp.large_block_count == 0 || ipp.large_block_size+ipp.parity_symbols_per_block <= 255)
	}

	// Now let's try encoding payloads and extracting original again.
	// This will also provide exercise for scrambling and Reed Solomon under more conditions.

	var original_payload [IL2P_MAX_PAYLOAD_SIZE]C.uchar
	for n := C.int(0); n < IL2P_MAX_PAYLOAD_SIZE; n++ {
		original_payload[n] = C.uchar(C.uint(n) & 0xff)
	}
	for max_fec := C.int(0); max_fec <= 1; max_fec++ {
		for payload_length := C.int(1); payload_length <= IL2P_MAX_PAYLOAD_SIZE; payload_length++ {
			// dw_printf ("\n--------- max_fec = %d, payload_length = %d\n", max_fec, payload_length);
			var encoded [IL2P_MAX_ENCODED_PAYLOAD_SIZE]C.uchar
			var k = il2p_encode_payload(&original_payload[0], payload_length, max_fec, &encoded[0])

			// dw_printf ("payload length %d %s -> %d\n", payload_length, max_fec ? "M" : "", k);
			assert.True(t, k > payload_length && k <= IL2P_MAX_ENCODED_PAYLOAD_SIZE)

			// Now extract.

			var extracted [IL2P_MAX_PAYLOAD_SIZE]C.uchar
			var symbols_corrected C.int = 0
			var e = il2p_decode_payload(&encoded[0], payload_length, max_fec, &extracted[0], &symbols_corrected)
			// dw_printf ("e = %d, payload_length = %d\n", e, payload_length);
			assert.Equal(t, payload_length, e)

			// if (memcmp (original_payload, extracted, payload_length) != 0) {
			//  dw_printf ("********** Received message not as expected. **********\n");
			//  fx_hex_dump(extracted, payload_length);
			// }
			assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&original_payload[0]), unsafe.Pointer(&extracted[0]), C.ulong(payload_length)))
		}
	}
} // end test_payload

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test header examples found in protocol specification.
//
/////////////////////////////////////////////////////////////////////////////////////////////

func test_example_headers(t *testing.T) {
	t.Helper()

	//----------- Example 1:  AX.25 S-Frame   --------------

	//	This frame sample only includes a 15 byte header, without PID field.
	//	Destination Callsign: ?KA2DEW-2
	//	Source Callsign: ?KK4HEJ-7
	//	N(R): 5
	//	P/F: 1
	//	C: 1
	//	Control Opcode: 00 (Receive Ready)
	//
	//	AX.25 data:
	//	96 82 64 88 8a ae e4 96 96 68 90 8a 94 6f b1
	//
	//	IL2P Data Prior to Scrambling and RS Encoding:
	//	2b a1 12 24 25 77 6b 2b 54 68 25 2a 27
	//
	//	IL2P Data After Scrambling and RS Encoding:
	//	26 57 4d 57 f1 96 cc 85 42 e7 24 f7 2e 8a 97

	dw_printf("Example 1: AX.25 S-Frame...\n")

	var example1 []C.uchar = []C.uchar{0x96, 0x82, 0x64, 0x88, 0x8a, 0xae, 0xe4, 0x96, 0x96, 0x68, 0x90, 0x8a, 0x94, 0x6f, 0xb1}
	var header1 []C.uchar = []C.uchar{0x2b, 0xa1, 0x12, 0x24, 0x25, 0x77, 0x6b, 0x2b, 0x54, 0x68, 0x25, 0x2a, 0x27}
	var header [IL2P_HEADER_SIZE]C.uchar
	var sresult [32]C.uchar
	C.memset(unsafe.Pointer(&header[0]), 0, IL2P_HEADER_SIZE)
	C.memset(unsafe.Pointer(&sresult[0]), 0, 32)
	var check [2]C.uchar
	var alevel alevel_t

	var pp = ax25_from_frame(&example1[0], C.int(len(example1)), alevel)
	assert.NotNil(t, pp)
	var e = il2p_type_1_header(pp, 0, &header[0])
	assert.Equal(t, C.int(0), e)
	ax25_delete(pp)

	// dw_printf ("Example 1 header:\n");
	// for (int i = 0 ; i < sizeof(header); i++) {
	//     dw_printf (" %02x", header[i]);
	// }
	// dw_printf ("\n");

	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&header[0]), unsafe.Pointer(&header1[0]), IL2P_HEADER_SIZE))

	il2p_scramble_block(&header[0], &sresult[0], 13)
	// dw_printf ("Expect scrambled  26 57 4d 57 f1 96 cc 85 42 e7 24 f7 2e\n");
	// for (int i = 0 ; i < sizeof(sresult); i++) {
	//    dw_printf (" %02x", sresult[i]);
	// }
	// dw_printf ("\n");

	il2p_encode_rs(&sresult[0], 13, 2, &check[0])

	// dw_printf ("check = ");
	// for (int i = 0 ; i < sizeof(check); i++) {
	//     dw_printf (" %02x", check[i]);
	// }
	// dw_printf ("\n");
	assert.Equal(t, C.uchar(0x8a), check[0])
	assert.Equal(t, C.uchar(0x97), check[1])

	// Can we go from IL2P back to AX.25?

	pp = il2p_decode_header_type_1(&header[0], 0)
	assert.NotNil(t, pp)

	var dst_addr [AX25_MAX_ADDR_LEN]C.char
	var src_addr [AX25_MAX_ADDR_LEN]C.char

	ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dst_addr[0])
	ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src_addr[0])

	var cr cmdres_t // command or response.
	var description [64]C.char
	var pf C.int     // Poll/Final.
	var nr, ns C.int // Sequence numbers.

	var frame_type = ax25_frame_type(pp, &cr, &description[0], &pf, &nr, &ns)
	_ = frame_type // TODO Check this?

	// TODO: compare binary.
	ax25_delete(pp)

	dw_printf("Example 1 header OK\n")

	// -------------- Example 2 - UI frame, no info part  ------------------

	//	This is an AX.25 Unnumbered Information frame, such as APRS.
	//	Destination Callsign: ?CQ    -0
	//	Source Callsign: ?KK4HEJ-15
	//	P/F: 0
	//	C: 0
	//	Control Opcode:  3 Unnumbered Information
	//	PID: 0xF0 No L3
	//
	//	AX.25 Data:
	//	86 a2 40 40 40 40 60 96 96 68 90 8a 94 7f 03 f0
	//
	//	IL2P Data Prior to Scrambling and RS Encoding:
	//	63 f1 40 40 40 00 6b 2b 54 28 25 2a 0f
	//
	//	IL2P Data After Scrambling and RS Encoding:
	//	6a ea 9c c2 01 11 fc 14 1f da 6e f2 53 91 bd

	// dw_printf ("---------- example 2 ------------\n");
	var example2 []C.uchar = []C.uchar{0x86, 0xa2, 0x40, 0x40, 0x40, 0x40, 0x60, 0x96, 0x96, 0x68, 0x90, 0x8a, 0x94, 0x7f, 0x03, 0xf0}
	var header2 []C.uchar = []C.uchar{0x63, 0xf1, 0x40, 0x40, 0x40, 0x00, 0x6b, 0x2b, 0x54, 0x28, 0x25, 0x2a, 0x0f}
	C.memset(unsafe.Pointer(&header[0]), 0, C.ulong(len(header)))
	C.memset(unsafe.Pointer(&sresult[0]), 0, C.ulong(len(sresult)))
	alevel = alevel_t{} //nolint:exhaustruct

	pp = ax25_from_frame(&example2[0], C.int(len(example2)), alevel)
	assert.NotNil(t, pp)
	e = il2p_type_1_header(pp, 0, &header[0])
	assert.Equal(t, C.int(0), e)
	ax25_delete(pp)

	// dw_printf ("Example 2 header:\n");
	// for (int i = 0 ; i < sizeof(header); i++) {
	//     dw_printf (" %02x", header[i]);
	// }
	// dw_printf ("\n");

	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&header[0]), unsafe.Pointer(&header2[0]), C.ulong(len(header2))))

	il2p_scramble_block(&header[0], &sresult[0], 13)
	// dw_printf ("Expect scrambled  6a ea 9c c2 01 11 fc 14 1f da 6e f2 53\n");
	// for (int i = 0 ; i < sizeof(sresult); i++) {
	//    dw_printf (" %02x", sresult[i]);
	// }
	// dw_printf ("\n");

	il2p_encode_rs(&sresult[0], 13, 2, &check[0])

	// dw_printf ("expect checksum = 91 bd\n");
	// dw_printf ("check = ");
	// for (int i = 0 ; i < sizeof(check); i++) {
	//     dw_printf (" %02x", check[i]);
	// }
	// dw_printf ("\n");
	assert.Equal(t, C.uchar(0x91), check[0])
	assert.Equal(t, C.uchar(0xbd), check[1])

	// Can we go from IL2P back to AX.25?

	pp = il2p_decode_header_type_1(&header[0], 0)
	assert.NotNil(t, pp)

	ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dst_addr[0])
	ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src_addr[0])

	frame_type = ax25_frame_type(pp, &cr, &description[0], &pf, &nr, &ns)
	_ = frame_type

	// TODO: compare binary.

	ax25_delete(pp)
	// TODO: more examples

	dw_printf("Example 2 header OK\n")

	// -------------- Example 3 - I Frame  ------------------

	//	This is an AX.25 I-Frame with 9 bytes of information after the 16 byte header.
	//
	//	Destination Callsign: ?KA2DEW-2
	//	Source Callsign: ?KK4HEJ-2
	//	P/F: 1
	//	C: 1
	//	N(R): 5
	//	N(S): 4
	//	AX.25 PID: 0xCF TheNET
	//	IL2P Payload Byte Count: 9
	//
	//	AX.25 Data:
	//	96 82 64 88 8a ae e4 96 96 68 90 8a 94 65 b8 cf 30 31 32 33 34 35 36 37 38
	//
	//	IL2P Scrambled and Encoded Data:
	//	26 13 6d 02 8c fe fb e8 aa 94 2d 6a 34 43 35 3c 69 9f 0c 75 5a 38 a1 7f f3 fc

	// dw_printf ("---------- example 3 ------------\n");
	var example3 []C.uchar = []C.uchar{0x96, 0x82, 0x64, 0x88, 0x8a, 0xae, 0xe4, 0x96, 0x96, 0x68, 0x90, 0x8a, 0x94, 0x65, 0xb8, 0xcf, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38}
	var header3 []C.uchar = []C.uchar{0x2b, 0xe1, 0x52, 0x64, 0x25, 0x77, 0x6b, 0x2b, 0xd4, 0x68, 0x25, 0xaa, 0x22}
	var complete3 []C.uchar = []C.uchar{0x26, 0x13, 0x6d, 0x02, 0x8c, 0xfe, 0xfb, 0xe8, 0xaa, 0x94, 0x2d, 0x6a, 0x34, 0x43, 0x35, 0x3c, 0x69, 0x9f, 0x0c, 0x75, 0x5a, 0x38, 0xa1, 0x7f, 0xf3, 0xfc}
	C.memset(unsafe.Pointer(&header[0]), 0, C.ulong(len(header)))
	C.memset(unsafe.Pointer(&sresult[0]), 0, C.ulong(len(sresult)))
	alevel = alevel_t{} //nolint:exhaustruct

	pp = ax25_from_frame(&example3[0], C.int(len(example3)), alevel)
	assert.NotNil(t, pp)
	e = il2p_type_1_header(pp, 0, &header[0])
	assert.Equal(t, C.int(9), e)
	ax25_delete(pp)

	// dw_printf ("Example 3 header:\n");
	// for (int i = 0 ; i < sizeof(header); i++) {
	//     dw_printf (" %02x", header[i]);
	// }
	// dw_printf ("\n");

	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&header[0]), unsafe.Pointer(&header3[0]), C.ulong(len(header))))

	il2p_scramble_block(&header[0], &sresult[0], 13)
	// dw_printf ("Expect scrambled  26 13 6d 02 8c fe fb e8 aa 94 2d 6a 34\n");
	// for (int i = 0 ; i < sizeof(sresult); i++) {
	//    dw_printf (" %02x", sresult[i]);
	// }
	// dw_printf ("\n");

	il2p_encode_rs(&sresult[0], 13, 2, &check[0])

	// dw_printf ("expect checksum = 43 35\n");
	// dw_printf ("check = ");
	// for (int i = 0 ; i < sizeof(check); i++) {
	//     dw_printf (" %02x", check[i]);
	// }
	// dw_printf ("\n");

	assert.Equal(t, C.uchar(0x43), check[0])
	assert.Equal(t, C.uchar(0x35), check[1])

	// That was only the header.  We will get to the info part in a later test.

	// Can we go from IL2P back to AX.25?

	pp = il2p_decode_header_type_1(&header[0], 0)
	assert.NotNil(t, pp)

	ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dst_addr[0])
	ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src_addr[0])

	frame_type = ax25_frame_type(pp, &cr, &description[0], &pf, &nr, &ns)
	_ = frame_type

	// TODO: compare binary.

	ax25_delete(pp)
	dw_printf("Example 3 header OK\n")

	// Example 3 again, this time the Information part is included.

	pp = ax25_from_frame(&example3[0], C.int(len(example3)), alevel)
	assert.NotNil(t, pp)

	var max_fec C.int = 0
	var iout [IL2P_MAX_PACKET_SIZE]C.uchar
	e = il2p_encode_frame(pp, max_fec, &iout[0])

	// dw_printf ("expected for example 3:\n");
	// fx_hex_dump(complete3, sizeof(complete3));
	// dw_printf ("actual result for example 3:\n");
	// fx_hex_dump(iout, e);
	// Does it match the example in the protocol spec?
	assert.Equal(t, C.int(len(complete3)), e)
	assert.Equal(t, C.int(0), C.memcmp(unsafe.Pointer(&iout[0]), unsafe.Pointer(&complete3[0]), C.ulong(len(complete3))))
	ax25_delete(pp)

	dw_printf("Example 3 with info OK\n")
} // end test_example_headers

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test all of the frame types.
//
//	Encode to IL2P format, decode, and verify that the result is the same as the original.
//
/////////////////////////////////////////////////////////////////////////////////////////////

func enc_dec_compare(t *testing.T, pp1 *packet_t) {
	t.Helper()

	for max_fec := C.int(0); max_fec <= 1; max_fec++ {
		var encoded [IL2P_MAX_PACKET_SIZE]C.uchar
		var enc_len = il2p_encode_frame(pp1, max_fec, &encoded[0])
		assert.GreaterOrEqual(t, enc_len, C.int(0))

		var pp2 = il2p_decode_frame(&encoded[0])
		assert.NotNil(t, pp2)

		// Is it the same after encoding to IL2P and then decoding?

		var len1 = ax25_get_frame_len(pp1)
		var data1 = ax25_get_frame_data_ptr(pp1)

		var len2 = ax25_get_frame_len(pp2)
		var data2 = ax25_get_frame_data_ptr(pp2)

		if len1 != len2 || C.memcmp(unsafe.Pointer(data1), unsafe.Pointer(data2), C.ulong(len1)) != 0 {
			dw_printf("\nEncode/Decode Error.  Original:\n")
			ax25_hex_dump(pp1)

			dw_printf("IL2P encoded as:\n")
			fx_hex_dump(&encoded[0], enc_len)

			dw_printf("Got turned into this:\n")
			ax25_hex_dump(pp2)
		}

		assert.True(t, len1 == len2 && C.memcmp(unsafe.Pointer(data1), unsafe.Pointer(data2), C.ulong(len1)) == 0)

		ax25_delete(pp2)
	}
}

func all_frame_types(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char
	var pinfo *C.uchar
	var pid C.int = 0xf0
	var info_len C.int

	C.strcpy(&addrs[0][0], C.CString("W2UB"))
	C.strcpy(&addrs[1][0], C.CString("WB2OSZ-12"))
	var num_addr C.int = 2

	dw_printf("Testing all frame types.\n")

	/* U frame */

	dw_printf("\nU frames...\n")

	for ftype := frame_type_U_SABME; ftype <= frame_type_U_TEST; ftype++ {
		for pf := C.int(0); pf <= 1; pf++ {
			var cmin, cmax cmdres_t

			switch ftype {
			// 0 = response, 1 = command
			case frame_type_U_SABME:
				cmin = 1
				cmax = 1
			case frame_type_U_SABM:
				cmin = 1
				cmax = 1
			case frame_type_U_DISC:
				cmin = 1
				cmax = 1
			case frame_type_U_DM:
				cmin = 0
				cmax = 0
			case frame_type_U_UA:
				cmin = 0
				cmax = 0
			case frame_type_U_FRMR:
				cmin = 0
				cmax = 0
			case frame_type_U_UI:
				cmin = 0
				cmax = 1
			case frame_type_U_XID:
				cmin = 0
				cmax = 1
			case frame_type_U_TEST:
				cmin = 0
				cmax = 1
			default:
				panic(fmt.Sprintf("Surprising frame type found: %d", ftype))
			}

			for cr := cmin; cr <= cmax; cr++ {
				dw_printf("\nConstruct U frame, cr=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_u_frame(addrs, num_addr, cr, ftype, pf, pid, pinfo, info_len)
				ax25_hex_dump(pp)
				enc_dec_compare(t, pp)
				ax25_delete(pp)
			}
		}
	}

	/* S frame */

	// strcpy (addrs[2], "DIGI1-1");
	// num_addr = 3;

	dw_printf("\nS frames...\n")

	for ftype := frame_type_S_RR; ftype <= frame_type_S_SREJ; ftype++ {
		for pf := C.int(0); pf <= 1; pf++ {
			var modulo = modulo_8
			var nr = C.int(modulo/2 + 1)

			for cr := cmdres_t(0); cr <= cr_cmd; cr++ {
				// SREJ can only be response.
				if ftype == frame_type_S_SREJ && cr != cr_res {
					continue
				}

				dw_printf("\nConstruct S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil, 0)

				ax25_hex_dump(pp)
				enc_dec_compare(t, pp)
				ax25_delete(pp)
			}

			modulo = modulo_128
			nr = C.int(modulo/2 + 1)

			for cr := cmdres_t(0); cr <= cr_cmd; cr++ {
				// SREJ can only be response.
				if ftype == frame_type_S_SREJ && cr != cr_res {
					continue
				}

				dw_printf("\nConstruct S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil, 0)

				ax25_hex_dump(pp)
				enc_dec_compare(t, pp)
				ax25_delete(pp)
			}
		}
	}

	/* SREJ is only S frame which can have information part. */

	var srej_info []C.uchar = []C.uchar{1 << 1, 2 << 1, 3 << 1, 4 << 1}

	var ftype = frame_type_S_SREJ
	for pf := C.int(0); pf <= 1; pf++ {
		var modulo = modulo_128
		var nr C.int = 127
		var cr cmdres_t = cr_res

		dw_printf("\nConstruct Multi-SREJ S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

		var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, &srej_info[0], C.int(len(srej_info)))

		ax25_hex_dump(pp)
		enc_dec_compare(t, pp)
		ax25_delete(pp)
	}

	/* I frame */

	dw_printf("\nI frames...\n")

	pinfo = (*C.uchar)(unsafe.Pointer(C.strdup(C.CString("The rain in Spain stays mainly on the plain."))))
	info_len = C.int(C.strlen((*C.char)(unsafe.Pointer(pinfo))))

	for pf := C.int(0); pf <= 1; pf++ {
		var modulo = modulo_8
		var nr = 0x55 & C.int(modulo-1)
		var ns = 0xaa & C.int(modulo-1)

		for cr := cmdres_t(1); cr <= 1; cr++ { // can only be command
			dw_printf("\nConstruct I frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

			var pp = ax25_i_frame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, pinfo, info_len)

			ax25_hex_dump(pp)
			enc_dec_compare(t, pp)
			ax25_delete(pp)
		}

		modulo = modulo_128
		nr = 0x55 & C.int(modulo-1)
		ns = 0xaa & C.int(modulo-1)

		for cr := cmdres_t(1); cr <= 1; cr++ {
			dw_printf("\nConstruct I frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

			var pp = ax25_i_frame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, pinfo, info_len)

			ax25_hex_dump(pp)
			enc_dec_compare(t, pp)
			ax25_delete(pp)
		}
	}
} // end all_frame_types

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test bitstream tapped off from demodulator.
//
//	5 frames were sent to Nino TNC and a recording was made.
//	This was demodulated and the resulting bit stream saved to a file.
//
//	No automatic test here - must be done manually with audio recording.
//
/////////////////////////////////////////////////////////////////////////////////////////////

var decoding_bitstream = 0

func decode_bitstream(t *testing.T) {
	t.Helper()

	dw_printf("-----\nReading il2p-bitstream.txt if available...\n")

	var fp = C.fopen(C.CString("il2p-bitstream.txt"), C.CString("r"))
	if fp == nil {
		dw_printf("Bitstream test file not available.\n")
		return
	}

	decoding_bitstream = 1
	var save_previous = il2p_get_debug()
	il2p_set_debug(1)

	var ch C.int
	for ch != C.EOF {
		ch = C.fgetc(fp)
		if ch == '0' || ch == '1' {
			il2p_rec_bit(0, 0, 0, ch-'0')
		}
	}
	C.fclose(fp)
	il2p_set_debug(save_previous)
	decoding_bitstream = 0
} // end decode_bitstream

/////////////////////////////////////////////////////////////////////////////////////////////
//
//	Test serialize / deserialize.
//
//	This uses same functions used on the air.
//
/////////////////////////////////////////////////////////////////////////////////////////////

var addrs2 = "AA1AAA-1>ZZ9ZZZ-9"
var addrs3 = "AA1AAA-1>ZZ9ZZZ-9,DIGI*"

var text = `'... As I was saying, that seems to be done right - though I haven't time to look it over thoroughly just now - and that shows that there are three hundred and sixty-four days when you might get un-birthday presents -'
'Certainly,' said Alice.
'And only one for birthday presents, you know. There's glory for you!'
'I don't know what you mean by \"glory\",' Alice said.
Humpty Dumpty smiled contemptuously. 'Of course you don't - till I tell you. I meant \"there's a nice knock-down argument for you!\"'
'But \"glory\" doesn't mean \"a nice knock-down argument\",' Alice objected.
'When I use a word,' Humpty Dumpty said, in rather a scornful tone, 'it means just what I choose it to mean - neither more nor less.'
'The question is,' said Alice, 'whether you can make words mean so many different things.'
'The question is,' said Humpty Dumpty, 'which is to be master - that's all.'
`

var rec_count = -1 // disable deserialized packet test.
var polarity C.int = 0

func test_serdes(t *testing.T) {
	t.Helper()

	dw_printf("\nTest serialize / deserialize...\n")
	rec_count = 0

	// try combinations of header type, max_fec, polarity, errors.

	for hdr_type := range 1 {
		var packet string
		if hdr_type == 1 {
			packet = fmt.Sprintf("%s:%s", addrs2, text)
		} else {
			packet = fmt.Sprintf("%s:%s", addrs3, text)
		}
		var pp = ax25_from_text(packet, true)
		assert.NotNil(t, pp)

		var channel C.int

		for max_fec := C.int(0); max_fec <= 1; max_fec++ {
			for polarity = C.int(0); polarity <= 2; polarity++ { // 2 means throw in some errors.
				var num_bits_sent = il2p_send_frame(channel, pp, max_fec, polarity)
				dw_printf("%d bits sent.\n", num_bits_sent)

				// Need extra bit at end to flush out state machine.
				il2p_rec_bit(0, 0, 0, 0)
			}
		}
		ax25_delete(pp)
	}

	dw_printf("Serdes receive count = %d\n", rec_count)
	// FIXME KG Relies on multi_modem_process_rec_packet_fake: assert.True(t, rec_count == 12)
	rec_count = -1 // disable deserialized packet test.
}
