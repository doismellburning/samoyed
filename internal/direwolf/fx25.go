package direwolf

const CTAG_MIN = 0x01
const CTAG_MAX = 0x0B

// Maximum sizes of "data" and "check" parts.

const FX25_MAX_DATA = 239   // i.e. RS(255,239)
const FX25_MAX_CHECK = 64   // e.g. RS(255, 191)
const FX25_BLOCK_SIZE = 255 // Block size always 255 for 8 bit symbols.
