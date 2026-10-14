package direwolf

import (
	"iter"

	"github.com/doismellburning/samoyed/internal/ptt"
)

/*
 * PTT control.
 */

const (
	PTT_METHOD_NONE   = ptt.MethodNone
	PTT_METHOD_SERIAL = ptt.MethodSerial
	PTT_METHOD_GPIO   = ptt.MethodGPIO
	PTT_METHOD_GPIOD  = ptt.MethodGPIOD
	PTT_METHOD_LPT    = ptt.MethodLPT
	PTT_METHOD_HAMLIB = ptt.MethodHamlib
	PTT_METHOD_CM108  = ptt.MethodCM108
)

type ptt_line_t = ptt.Line

const (
	PTT_LINE_NONE = ptt.LineNone
	PTT_LINE_RTS  = ptt.LineRTS
	PTT_LINE_DTR  = ptt.LineDTR
)

// outputType is one of the outputs the PTT controller drives for a channel.
type outputType = ptt.OutputType

const OCTYPE_PTT = ptt.OutputPTT
const OCTYPE_DCD = ptt.OutputDCD
const OCTYPE_CON = ptt.OutputCon

const NUM_OCTYPES = ptt.NumOutputTypes

// outputTypes yields each output type in turn, from OCTYPE_PTT to OCTYPE_CON.
func outputTypes() iter.Seq[outputType] {
	return ptt.OutputTypes()
}

const ICTYPE_TXINH = ptt.InputTxInhibit

const NUM_ICTYPES = ptt.NumInputTypes
