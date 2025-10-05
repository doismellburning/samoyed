package direwolf

import "C"

type position_t struct {
	lat          [8]C.char
	sym_table_id C.char /* / \ 0-9 A-Z */
	lon          [9]C.char
	symbol_code  C.char
}

type compressed_position_t struct {
	sym_table_id C.char /* / \ a-j A-Z */
	/* "The presence of the leading Symbol Table Identifier */
	/* instead of a digit indicates that this is a compressed */
	/* Position Report and not a normal lat/long report." */

	y           [4]C.char /* Compressed Latitude. */
	x           [4]C.char /* Compressed Longitude. */
	symbol_code C.char
	c           C.char /* Course/speed or radio range or altitude. */
	s           C.char
	t           C.char /* Compression type. */
}
