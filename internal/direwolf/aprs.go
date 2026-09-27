package direwolf

type position_t struct {
	Lat        [8]byte
	SymTableId byte /* / \ 0-9 A-Z */
	Lon        [9]byte
	SymbolCode byte
}

type compressed_position_t struct {
	SymTableId byte /* / \ a-j A-Z */
	/* "The presence of the leading Symbol Table Identifier */
	/* instead of a digit indicates that this is a compressed */
	/* Position Report and not a normal lat/long report." */
	/* "a-j" is not a typographical error. */
	/* The first 10 lower case letters represent the overlay */
	/* characters of 0-9 in the compressed format. */

	Y          [4]byte /* Compressed Latitude. */
	X          [4]byte /* Compressed Longitude. */
	SymbolCode byte
	C          byte /* Course/speed or radio range or altitude. */
	S          byte
	T          byte /* Compression type. */
}
