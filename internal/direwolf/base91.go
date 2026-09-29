package direwolf

import (
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

/* Range of digits for Base 91 representation. */

const B91_MIN = '!'
const B91_MAX = '{'

func isdigit91(c byte) bool {
	return ((c) >= B91_MIN && (c) <= B91_MAX)
}

func two_base91_to_i(first, second byte) maybe.Maybe[int] {
	var result int

	dwutil.Assert(B91_MAX-B91_MIN == 90)

	if isdigit91(first) {
		result = int(first-B91_MIN) * 91
	} else {
		logrus.WithField("character", string(first)).Debug("Not a valid character for base 91 telemetry data")

		return maybe.Nothing[int]()
	}

	if isdigit91(second) {
		result += int(second - B91_MIN)
	} else {
		logrus.WithField("character", string(second)).Debug("Not a valid character for base 91 telemetry data")

		return maybe.Nothing[int]()
	}

	return maybe.Just(result)
}
