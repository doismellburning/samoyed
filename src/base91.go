package direwolf

/* Range of digits for Base 91 representation. */

const B91_MIN = '!'
const B91_MAX = '{'

func isdigit91(c byte) bool {
	return ((c) >= B91_MIN && (c) <= B91_MAX)
}

func two_base91_to_i(c *C.char) C.int {
	var result = 0

	assert(B91_MAX-B91_MIN == 90)

	if isdigit91(c[0]) {
		result = (c[0] - B91_MIN) * 91
	} else {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("\"%c\" is not a valid character for base 91 telemetry data.\n", c[0])
		return (G_UNKNOWN)
	}

	if isdigit91(c[1]) {
		result += (c[1] - B91_MIN)
	} else {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("\"%c\" is not a valid character for base 91 telemetry data.\n", c[1])
		return (G_UNKNOWN)
	}
	return (result)
}
