//nolint:gochecknoglobals
package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

/* From Figure 4.6. Typical XID frame, from AX.25 protocol spec, v. 2.2 */
/* This is the info part after a control byte of 0xAF. */

var xid_example = []byte{

	/* FI */ 0x82, /* Format indicator */
	/* GI */ 0x80, /* Group Identifier - parameter negotiation */
	/* GL */ 0x00, /* Group length - all of the PI/PL/PV fields */
	/* GL */ 0x17, /* (2 bytes) */
	/* PI */ 0x02, /* Parameter Indicator - classes of procedures */
	/* PL */ 0x02, /* Parameter Length */

	// Erratum: Example in the protocol spec looks wrong.
	///* PV */	0x00,	/* Parameter Variable - Half Duplex, Async, Balanced Mode */
	///* PV */	0x20,	/*  */
	// I think it should be like this instead.
	/* PV */ 0x21, /* Parameter Variable - Half Duplex, Async, Balanced Mode */
	/* PV */ 0x00, /* Reserved */

	/* PI */ 0x03, /* Parameter Indicator - optional functions */
	/* PL */ 0x03, /* Parameter Length */
	/* PV */ 0x86, /* Parameter Variable - SREJ/REJ, extended addr */
	/* PV */ 0xA8, /* 16-bit FCS, TEST cmd/resp, Modulo 128 */
	/* PV */ 0x02, /* synchronous transmit */
	/* PI */ 0x06, /* Parameter Indicator - Rx I field length (bits) */
	/* PL */ 0x02, /* Parameter Length */

	// Erratum: The text does not say anything about the byte order for multibyte
	// numeric values.  In the example, we have two cases where 16 bit numbers are
	// sent with the more significant byte first.

	/* PV */ 0x04, /* Parameter Variable - 1024 bits (128 octets) */
	/* PV */ 0x00, /* */
	/* PI */ 0x08, /* Parameter Indicator - Rx window size */
	/* PL */ 0x01, /* Parameter length */
	/* PV */ 0x02, /* Parameter Variable - 2 frames */
	/* PI */ 0x09, /* Parameter Indicator - Timer T1 */
	/* PL */ 0x02, /* Parameter Length */
	/* PV */ 0x10, /* Parameter Variable - 4096 MSec */
	/* PV */ 0x00, /* */
	/* PI */ 0x0A, /* Parameter Indicator - Retries (N1) */
	/* PL */ 0x01, /* Parameter Length */
	/* PV */ 0x03, /* Parameter Variable - 3 retries */
}

func Test_XID(t *testing.T) {
	/*
		struct xid_param_s param;
		struct xid_param_s param2;
		int n;
		unsigned char info[40];	// Currently max of 27 but things can change.
		char desc[150];		// I've seen 109.
	*/

	var param2 *xid_param_s

	/* parse example. */

	var param, desc, n = xid_parse(xid_example)

	t.Logf("%d: %s", 0, desc)

	assert.True(t, n)
	assert.Equal(t, maybe.Just(false), param.FullDuplex)
	assert.Equal(t, SREJSingle, param.SREJ)
	assert.Equal(t, ax25.Modulo128, param.Modulo)
	assert.Equal(t, maybe.Just(128), param.IFieldLengthRx)
	assert.Equal(t, maybe.Just(2), param.WindowSizeRx)
	assert.Equal(t, maybe.Just(4096), param.AckTimer)
	assert.Equal(t, maybe.Just(3), param.Retries)

	/* encode and verify it comes out the same. */

	var info = xid_encode(param, ax25.CRCmd)
	assert.Len(t, info, len(xid_example))

	assert.Equal(t, info, xid_example, "n: %v, info: %v, xid_example[0]: %v", n, info, xid_example)

	/* try a couple different values, no srej. */

	param.FullDuplex = maybe.Just(true)
	param.SREJ = SREJNone
	param.Modulo = ax25.Modulo8
	param.IFieldLengthRx = maybe.Just(2048)
	param.WindowSizeRx = maybe.Just(3)
	param.AckTimer = maybe.Just(1234)
	param.Retries = maybe.Just(12)

	info = xid_encode(param, ax25.CRCmd)
	param2, desc, _ = xid_parse(info)

	t.Logf("%d: %s", 0, desc)

	assert.Equal(t, maybe.Just(true), param2.FullDuplex)
	assert.Equal(t, SREJNone, param2.SREJ)
	assert.Equal(t, ax25.Modulo8, param2.Modulo)
	assert.Equal(t, maybe.Just(2048), param2.IFieldLengthRx)
	assert.Equal(t, maybe.Just(3), param2.WindowSizeRx)
	assert.Equal(t, maybe.Just(1234), param2.AckTimer)
	assert.Equal(t, maybe.Just(12), param2.Retries)

	/* Other values, single srej. */

	param.FullDuplex = maybe.Just(false)
	param.SREJ = SREJSingle
	param.Modulo = ax25.Modulo8
	param.IFieldLengthRx = maybe.Just(61)
	param.WindowSizeRx = maybe.Just(4)
	param.AckTimer = maybe.Just(5555)
	param.Retries = maybe.Just(9)

	info = xid_encode(param, ax25.CRCmd)
	param2, desc, _ = xid_parse(info)

	t.Logf("%d: %s", 0, desc)

	assert.Equal(t, maybe.Just(false), param2.FullDuplex)
	assert.Equal(t, SREJSingle, param2.SREJ)
	assert.Equal(t, ax25.Modulo8, param2.Modulo)
	assert.Equal(t, maybe.Just(61), param2.IFieldLengthRx)
	assert.Equal(t, maybe.Just(4), param2.WindowSizeRx)
	assert.Equal(t, maybe.Just(5555), param2.AckTimer)
	assert.Equal(t, maybe.Just(9), param2.Retries)

	/* Other values, multi srej. */

	param.FullDuplex = maybe.Just(false)
	param.SREJ = SREJMulti
	param.Modulo = ax25.Modulo128
	param.IFieldLengthRx = maybe.Just(61)
	param.WindowSizeRx = maybe.Just(4)
	param.AckTimer = maybe.Just(5555)
	param.Retries = maybe.Just(9)

	info = xid_encode(param, ax25.CRCmd)
	param2, desc, _ = xid_parse(info)

	t.Logf("%d: %s", 0, desc)

	assert.Equal(t, maybe.Just(false), param2.FullDuplex)
	assert.Equal(t, SREJMulti, param2.SREJ)
	assert.Equal(t, ax25.Modulo128, param2.Modulo)
	assert.Equal(t, maybe.Just(61), param2.IFieldLengthRx)
	assert.Equal(t, maybe.Just(4), param2.WindowSizeRx)
	assert.Equal(t, maybe.Just(5555), param2.AckTimer)
	assert.Equal(t, maybe.Just(9), param2.Retries)

	/* Specify some and not others. */

	param.FullDuplex = maybe.Just(false)
	param.SREJ = SREJSingle
	param.Modulo = ax25.Modulo8
	param.IFieldLengthRx = maybe.Nothing[int]()
	param.WindowSizeRx = maybe.Nothing[int]()
	param.AckTimer = maybe.Just(999)
	param.Retries = maybe.Nothing[int]()

	info = xid_encode(param, ax25.CRCmd)
	param2, desc, _ = xid_parse(info)

	t.Logf("%d: %s", 0, desc)

	assert.Equal(t, maybe.Just(false), param2.FullDuplex)
	assert.Equal(t, SREJSingle, param2.SREJ)
	assert.Equal(t, ax25.Modulo8, param2.Modulo)
	assert.Equal(t, maybe.Nothing[int](), param2.IFieldLengthRx)
	assert.Equal(t, maybe.Nothing[int](), param2.WindowSizeRx)
	assert.Equal(t, maybe.Just(999), param2.AckTimer)
	assert.Equal(t, maybe.Nothing[int](), param2.Retries)

	/* Default values for empty info field. */

	info = []byte{}
	param2, desc, _ = xid_parse(info)

	t.Logf("%d: %s", 0, desc)

	assert.Equal(t, maybe.Nothing[bool](), param2.FullDuplex)
	assert.Equal(t, SREJNotSpecified, param2.SREJ)
	assert.Equal(t, ax25.ModuloUnknown, param2.Modulo)
	assert.Equal(t, maybe.Nothing[int](), param2.IFieldLengthRx)
	assert.Equal(t, maybe.Nothing[int](), param2.WindowSizeRx)
	assert.Equal(t, maybe.Nothing[int](), param2.AckTimer)
	assert.Equal(t, maybe.Nothing[int](), param2.Retries)
}
