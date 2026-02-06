package direwolf

import (
	"testing"
)

func Test_DecodeAPRSLine1(t *testing.T) {
	deviceid_init()

	DECODE_APRS_UTIL = true

	defer func() { DECODE_APRS_UTIL = false }()

	var expected = "Yaesu"

	AssertOutputContains(t, func() { DecodeAPRSLine("N1EDF-9>T2QT8Y,W1CLA-1,WIDE1*,WIDE2-2,00000:`bSbl!Mv/`\"4%}_ <0x0d>") }, expected)
}

func Test_DecodeAPRSLine2(t *testing.T) {
	deviceid_init()

	DECODE_APRS_UTIL = true

	defer func() { DECODE_APRS_UTIL = false }()

	var expected = "Kantronics"

	AssertOutputContains(t, func() { DecodeAPRSLine("WB2OSZ-1>APN383,qAR,N1EDU-2:!4237.14NS07120.83W#PHG7130Chelmsford, MA") }, expected)
}

func Test_DecodeAPRSLine3(t *testing.T) {
	deviceid_init()

	DECODE_APRS_UTIL = true

	defer func() { DECODE_APRS_UTIL = false }()

	var expected = "Echolink"

	AssertOutputContains(t, func() {
		DecodeAPRSLine(
			"00 82 a0 ae ae 62 60 e0 82 96 68 84 40 40 60 9c 68 b0 ae 86 40 e0 40 ae 92 88 8a 64 63 03 f0 3e 45 4d 36 34 6e 65 2f 23 20 45 63 68 6f 6c 69 6e 6b 20 31 34 35 2e 33 31 30 2f 31 30 30 68 7a 20 54 6f 6e 65",
		)
	}, expected)
}

func Test_DecodeAPRSLine3NoSpaces(t *testing.T) {
	deviceid_init()

	DECODE_APRS_UTIL = true

	defer func() { DECODE_APRS_UTIL = false }()

	var expected = "Echolink"

	AssertOutputContains(t, func() {
		DecodeAPRSLine(
			"0082a0aeae6260e0829668844040609c68b0ae8640e040ae92888a646303f03e454d36346e652f23204563686f6c696e6b203134352e3331302f313030687a20546f6e65",
		)
	}, expected)
}
