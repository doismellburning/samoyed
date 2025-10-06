package direwolf

type position_t struct {
	lat          [8]byte
	sym_table_id byte /* / \ 0-9 A-Z */
	lon          [9]byte
	symbol_code  byte
}

type compressed_position_t struct {
	sym_table_id byte /* / \ a-j A-Z */
	/* "The presence of the leading Symbol Table Identifier */
	/* instead of a digit indicates that this is a compressed */
	/* Position Report and not a normal lat/long report." */

	y           [4]byte /* Compressed Latitude. */
	x           [4]byte /* Compressed Longitude. */
	symbol_code byte
	c           byte /* Course/speed or radio range or altitude. */
	s           byte
	t           byte /* Compression type. */
}
