package direwolf

/*
 * PTT control.
 */

type ptt_method_t int

const (
	PTT_METHOD_NONE   ptt_method_t = iota /* VOX or no transmit. */
	PTT_METHOD_SERIAL                     /* Serial port RTS or DTR. */
	PTT_METHOD_GPIO                       /* General purpose I/O using sysfs, deprecated after 2020, Linux only. */
	PTT_METHOD_GPIOD                      /* General purpose I/O, using libgpiod, Linux only. */
	PTT_METHOD_LPT                        /* Parallel printer port, Linux only. */
	PTT_METHOD_HAMLIB                     /* HAMLib, Linux only. */
	PTT_METHOD_CM108                      /* GPIO pin of CM108/CM119/etc.  Linux only. */
)

type ptt_line_t int

const (
	PTT_LINE_NONE ptt_line_t = iota //  Important: 0 for neither.
	PTT_LINE_RTS
	PTT_LINE_DTR
)

const OCTYPE_PTT = 0
const OCTYPE_DCD = 1
const OCTYPE_CON = 2

const NUM_OCTYPES = 3 /* number of values above.   i.e. last value +1. */

const MAX_GPIO_NAME_LEN = 20 // 12 would cover any case I've seen so this should be safe

/* Each channel can also have associated input lines. */
/* So far, we just have one for transmit inhibit. */

const ICTYPE_TXINH = 0

const NUM_ICTYPES = 1 /* number of values above. i.e. last value +1. */
