package direwolf

/* Range of digits for Base 91 representation. */

const B91_MIN = '!'
const B91_MAX = '{'

func isdigit91(c byte) bool {
	return ((c) >= B91_MIN && (c) <= B91_MAX)
}
