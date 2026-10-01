package direwolf

// Base91Min and Base91Max are the range of digits for base 91 representation.
const Base91Min = '!'
const Base91Max = '{'

// IsBase91Digit reports whether c is a base 91 digit.
func IsBase91Digit(c byte) bool {
	return ((c) >= Base91Min && (c) <= Base91Max)
}
