package direwolf

const CTAG_MIN = 0x01
const CTAG_MAX = 0x0B

// Maximum sizes of "data" and "check" parts.

const FX25_MAX_DATA = 239   // i.e. RS(255,239)
const FX25_MAX_CHECK = 64   // e.g. RS(255, 191)
const FX25_BLOCK_SIZE = 255 // Block size always 255 for 8 bit symbols.

/* Reed-Solomon codec control block */
type rs_t struct {
	mm       uint   /* Bits per symbol */
	nn       uint   /* Symbols per block (= (1<<mm)-1) */
	alpha_to []byte /* log lookup table */
	index_of []byte /* Antilog lookup table */
	genpoly  []byte /* Generator polynomial */
	nroots   uint   /* Number of generator roots = number of parity symbols */
	fcr      byte   /* First consecutive root, index form */
	prim     byte   /* Primitive element, index form */
	iprim    byte   /* prim-th root of 1, index form */
}

func modnn(rs *rs_t, _x int) int {
	var x = uint(_x)

	for x >= rs.nn {
		x -= rs.nn
		x = (x >> rs.mm) + (x & rs.nn)
	}

	return int(x)
}
