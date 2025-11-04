package direwolf

/* Range of digits for Base 91 representation. */

const B91_MIN = '!'
const B91_MAX = '{'

func isdigit91(c byte) bool {
	return ((c) >= B91_MIN && (c) <= B91_MAX)
}

func two_base91_to_i(first, second byte) int {
	var result int

	Assert(B91_MAX-B91_MIN == 90)

	if isdigit91(first) {
		result = int(first-B91_MIN) * 91
	} else {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("\"%c\" is not a valid character for base 91 telemetry data.\n", first)

		return (G_UNKNOWN)
	}

	if isdigit91(second) {
		result += int(second - B91_MIN)
	} else {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("\"%c\" is not a valid character for base 91 telemetry data.\n", second)

		return (G_UNKNOWN)
	}

	return (result)
}
