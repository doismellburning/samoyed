package direwolf

// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <math.h>
// #include <ctype.h>
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func telemetry_test_main(t *testing.T) {
	t.Helper()

	var resultAlloc [120]C.char
	var result = &resultAlloc[0]
	var commentAlloc [40]C.char
	var comment = &commentAlloc[0]
	var sizeofResult = C.size_t(len(resultAlloc))
	var sizeofComment = C.size_t(len(commentAlloc))

	dw_printf("Unit test for telemetry decoding functions...\n")

	dw_printf("part 1\n")

	// From protocol spec.

	telemetry_data_original("WB2OSZ", "T#005,199,000,255,073,123,01101001", 0, result, sizeofResult, comment, sizeofComment)

	assert.Equal(t, "Seq=5, A1=199, A2=0, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1", C.GoString(result), "test 101")
	assert.Empty(t, C.GoString(comment), "test 101")

	// Try adding a comment.

	telemetry_data_original("WB2OSZ", "T#005,199,000,255,073,123,01101001Comment,with,commas", 0, result, sizeofResult, comment, sizeofComment)

	assert.Equal(t, "Seq=5, A1=199, A2=0, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1", C.GoString(result), "test 102")
	assert.Equal(t, "Comment,with,commas", C.GoString(comment), "test 102")

	// Error handling - Try shortening or omitting parts.

	telemetry_data_original("WB2OSZ", "T005,199,000,255,073,123,0110", 0, result, sizeofResult, comment, sizeofComment)

	assert.Empty(t, C.GoString(result), "test 103")
	assert.Empty(t, C.GoString(comment), "test 103")

	telemetry_data_original("WB2OSZ", "T#005,199,000,255,073,123,0110", 0, result, sizeofResult, comment, sizeofComment)

	assert.Equal(t, "Seq=5, A1=199, A2=0, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0", C.GoString(result), "test 104")
	assert.Empty(t, C.GoString(comment), "test 104")

	telemetry_data_original("WB2OSZ", "T#005,199,000,255,073,123", 0, result, sizeofResult, comment, sizeofComment)

	assert.Equal(t, "Seq=5, A1=199, A2=0, A3=255, A4=73, A5=123", C.GoString(result), "test 105")
	assert.Empty(t, C.GoString(comment), "test 105")

	telemetry_data_original("WB2OSZ", "T#005,199,000,255,,123,01101001", 0, result, sizeofResult, comment, sizeofComment)

	assert.Equal(t, "Seq=5, A1=199, A2=0, A3=255, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1", C.GoString(result), "test 106")
	assert.Empty(t, C.GoString(comment), "test 106")

	telemetry_data_original("WB2OSZ", "T#005,199,000,255,073,123,01101009", 0, result, sizeofResult, comment, sizeofComment)

	assert.Equal(t, "Seq=5, A1=199, A2=0, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0", C.GoString(result), "test 107")
	assert.Empty(t, C.GoString(comment), "test 107")

	// Local observation.

	telemetry_data_original("WB2OSZ", "T#491,4.9,0.3,25.0,0.0,1.0,00000000", 0, result, sizeofResult, comment, sizeofComment)

	assert.Equal(t, "Seq=491, A1=4.9, A2=0.3, A3=25.0, A4=0.0, A5=1.0, D1=0, D2=0, D3=0, D4=0, D5=0, D6=0, D7=0, D8=0", C.GoString(result), "test 108")
	assert.Empty(t, C.GoString(comment), "test 108")

	dw_printf("part 2\n")

	// From protocol spec.

	telemetry_data_base91("WB2OSZ", "ss11", result, sizeofResult)

	assert.Equal(t, "Seq=7544, A1=1472", C.GoString(result), "test 201")

	telemetry_data_base91("WB2OSZ", "ss11223344{{!\"", result, sizeofResult)

	assert.Equal(t, "Seq=7544, A1=1472, A2=1564, A3=1656, A4=1748, A5=8280, D1=1, D2=0, D3=0, D4=0, D5=0, D6=0, D7=0, D8=0", C.GoString(result), "test 202")

	// Error cases.  Should not happen in practice because function
	// should be called only with valid data that matches the pattern.

	telemetry_data_base91("WB2OSZ", "ss11223344{{!\"x", result, sizeofResult)

	assert.Empty(t, C.GoString(result), "test 203")

	telemetry_data_base91("WB2OSZ", "ss1", result, sizeofResult)

	assert.Empty(t, C.GoString(result), "test 204")

	telemetry_data_base91("WB2OSZ", "ss11223344{{!", result, sizeofResult)

	assert.Empty(t, C.GoString(result), "test 205")

	telemetry_data_base91("WB2OSZ", "s |1", result, sizeofResult)

	assert.Equal(t, "Seq=?", C.GoString(result), "test 206")

	dw_printf("part 3\n")

	telemetry_name_message("N0QBF-11", "Battery,Btemp,ATemp,Pres,Alt,Camra,Chut,Sun,10m,ATV")

	var pm = t_get_metadata("N0QBF-11")

	assert.Equal(t, "Battery", pm.name[0], "test 301")
	assert.Equal(t, "Btemp", pm.name[1], "test 301")
	assert.Equal(t, "ATemp", pm.name[2], "test 301")
	assert.Equal(t, "Pres", pm.name[3], "test 301")
	assert.Equal(t, "Alt", pm.name[4], "test 301")
	assert.Equal(t, "Camra", pm.name[5], "test 301")
	assert.Equal(t, "Chut", pm.name[6], "test 301")
	assert.Equal(t, "Sun", pm.name[7], "test 301")
	assert.Equal(t, "10m", pm.name[8], "test 301")
	assert.Equal(t, "ATV", pm.name[9], "test 301")
	assert.Equal(t, "D6", pm.name[10], "test 301")
	assert.Equal(t, "D7", pm.name[11], "test 301")
	assert.Equal(t, "D8", pm.name[12], "test 301")

	telemetry_unit_label_message("N0QBF-11", "v/100,deg.F,deg.F,Mbar,Kft,Click,OPEN,on,on,hi")

	pm = t_get_metadata("N0QBF-11")

	assert.Equal(t, "v/100", pm.unit[0], "test 302")
	assert.Equal(t, "deg.F", pm.unit[1], "test 302")
	assert.Equal(t, "deg.F", pm.unit[2], "test 302")
	assert.Equal(t, "Mbar", pm.unit[3], "test 302")
	assert.Equal(t, "Kft", pm.unit[4], "test 302")
	assert.Equal(t, "Click", pm.unit[5], "test 302")
	assert.Equal(t, "OPEN", pm.unit[6], "test 302")
	assert.Equal(t, "on", pm.unit[7], "test 302")
	assert.Equal(t, "on", pm.unit[8], "test 302")
	assert.Equal(t, "hi", pm.unit[9], "test 302")
	assert.Empty(t, pm.unit[10], "test 302")
	assert.Empty(t, pm.unit[11], "test 302")
	assert.Empty(t, pm.unit[12], "test 302")

	telemetry_coefficents_message("N0QBF-11", "0,5.2,0,0,.53,-32,3,4.39,49,-32,3,18,1,2,3", 0)

	pm = t_get_metadata("N0QBF-11")

	if pm.coeff[0][0] != 0 || pm.coeff[0][1] < 5.1999 || pm.coeff[0][1] > 5.2001 || pm.coeff[0][2] != 0 ||
		pm.coeff[1][0] != 0 || pm.coeff[1][1] < .52999 || pm.coeff[1][1] > .53001 || pm.coeff[1][2] != -32 ||
		pm.coeff[2][0] != 3 || pm.coeff[2][1] < 4.3899 || pm.coeff[2][1] > 4.3901 || pm.coeff[2][2] != 49 ||
		pm.coeff[3][0] != -32 || pm.coeff[3][1] != 3 || pm.coeff[3][2] != 18 ||
		pm.coeff[4][0] != 1 || pm.coeff[4][1] != 2 || pm.coeff[4][2] != 3 {
		assert.Fail(t, "Wrong result, test 303c\n")
	}

	if pm.coeff_ndp[0][0] != 0 || pm.coeff_ndp[0][1] != 1 || pm.coeff_ndp[0][2] != 0 ||
		pm.coeff_ndp[1][0] != 0 || pm.coeff_ndp[1][1] != 2 || pm.coeff_ndp[1][2] != 0 ||
		pm.coeff_ndp[2][0] != 0 || pm.coeff_ndp[2][1] != 2 || pm.coeff_ndp[2][2] != 0 ||
		pm.coeff_ndp[3][0] != 0 || pm.coeff_ndp[3][1] != 0 || pm.coeff_ndp[3][2] != 0 ||
		pm.coeff_ndp[4][0] != 0 || pm.coeff_ndp[4][1] != 0 || pm.coeff_ndp[4][2] != 0 {
		assert.Fail(t, "Wrong result, test 303n\n")
	}

	// Error if less than 15 or empty field.
	// Notice that we keep the previous value in this case.

	telemetry_coefficents_message("N0QBF-11", "0,5.2,0,0,.53,-32,3,4.39,49,-32,3,18,1,2", 0)

	pm = t_get_metadata("N0QBF-11")

	if pm.coeff[0][0] != 0 || pm.coeff[0][1] < 5.1999 || pm.coeff[0][1] > 5.2001 || pm.coeff[0][2] != 0 ||
		pm.coeff[1][0] != 0 || pm.coeff[1][1] < .52999 || pm.coeff[1][1] > .53001 || pm.coeff[1][2] != -32 ||
		pm.coeff[2][0] != 3 || pm.coeff[2][1] < 4.3899 || pm.coeff[2][1] > 4.3901 || pm.coeff[2][2] != 49 ||
		pm.coeff[3][0] != -32 || pm.coeff[3][1] != 3 || pm.coeff[3][2] != 18 ||
		pm.coeff[4][0] != 1 || pm.coeff[4][1] != 2 || pm.coeff[4][2] != 3 {
		assert.Fail(t, "Wrong result, test 304c\n")
	}

	if pm.coeff_ndp[0][0] != 0 || pm.coeff_ndp[0][1] != 1 || pm.coeff_ndp[0][2] != 0 ||
		pm.coeff_ndp[1][0] != 0 || pm.coeff_ndp[1][1] != 2 || pm.coeff_ndp[1][2] != 0 ||
		pm.coeff_ndp[2][0] != 0 || pm.coeff_ndp[2][1] != 2 || pm.coeff_ndp[2][2] != 0 ||
		pm.coeff_ndp[3][0] != 0 || pm.coeff_ndp[3][1] != 0 || pm.coeff_ndp[3][2] != 0 ||
		pm.coeff_ndp[4][0] != 0 || pm.coeff_ndp[4][1] != 0 || pm.coeff_ndp[4][2] != 0 {
		assert.Fail(t, "Wrong result, test 304n\n")
	}

	telemetry_coefficents_message("N0QBF-11", "0,5.2,0,0,.53,-32,3,4.39,49,-32,3,18,1,,3", 0)

	pm = t_get_metadata("N0QBF-11")

	if pm.coeff[0][0] != 0 || pm.coeff[0][1] < 5.1999 || pm.coeff[0][1] > 5.2001 || pm.coeff[0][2] != 0 ||
		pm.coeff[1][0] != 0 || pm.coeff[1][1] < .52999 || pm.coeff[1][1] > .53001 || pm.coeff[1][2] != -32 ||
		pm.coeff[2][0] != 3 || pm.coeff[2][1] < 4.3899 || pm.coeff[2][1] > 4.3901 || pm.coeff[2][2] != 49 ||
		pm.coeff[3][0] != -32 || pm.coeff[3][1] != 3 || pm.coeff[3][2] != 18 ||
		pm.coeff[4][0] != 1 || pm.coeff[4][1] != 2 || pm.coeff[4][2] != 3 {
		assert.Fail(t, "Wrong result, test 305c\n")
	}

	if pm.coeff_ndp[0][0] != 0 || pm.coeff_ndp[0][1] != 1 || pm.coeff_ndp[0][2] != 0 ||
		pm.coeff_ndp[1][0] != 0 || pm.coeff_ndp[1][1] != 2 || pm.coeff_ndp[1][2] != 0 ||
		pm.coeff_ndp[2][0] != 0 || pm.coeff_ndp[2][1] != 2 || pm.coeff_ndp[2][2] != 0 ||
		pm.coeff_ndp[3][0] != 0 || pm.coeff_ndp[3][1] != 0 || pm.coeff_ndp[3][2] != 0 ||
		pm.coeff_ndp[4][0] != 0 || pm.coeff_ndp[4][1] != 0 || pm.coeff_ndp[4][2] != 0 {
		assert.Fail(t, "Wrong result, test 305n\n")
	}

	telemetry_bit_sense_message("N0QBF-11", "10110000,N0QBF's Big Balloon", 0)

	pm = t_get_metadata("N0QBF-11")
	if !pm.sense[0] || pm.sense[1] || !pm.sense[2] || !pm.sense[3] ||
		pm.sense[4] || pm.sense[5] || pm.sense[6] || pm.sense[7] {
		assert.Fail(t, "Wrong result, test 306\n")
	}
	assert.Equal(t, "N0QBF's Big Balloon", pm.project, "test 306")

	// Too few and invalid digits.
	telemetry_bit_sense_message("N0QBF-11", "1011000", 0)

	pm = t_get_metadata("N0QBF-11")
	if !pm.sense[0] || pm.sense[1] || !pm.sense[2] || !pm.sense[3] ||
		pm.sense[4] || pm.sense[5] || pm.sense[6] || pm.sense[7] {
		assert.Fail(t, "Wrong result, test 307\n")
	}
	assert.Empty(t, pm.project, "test 307")

	telemetry_bit_sense_message("N0QBF-11", "10110008", 0)

	pm = t_get_metadata("N0QBF-11")
	if !pm.sense[0] || pm.sense[1] || !pm.sense[2] || !pm.sense[3] ||
		pm.sense[4] || pm.sense[5] || pm.sense[6] || pm.sense[7] {
		assert.Fail(t, "Wrong result, test 308\n")
	}
	assert.Empty(t, pm.project, "test 308")

	dw_printf("part 4\n")

	telemetry_coefficents_message("M0XER-3", "0,0.001,0,0,0.001,0,0,0.1,-273.2,0,1,0,0,1,0", 0)
	telemetry_bit_sense_message("M0XER-3", "11111111,10mW research balloon", 0)
	telemetry_name_message("M0XER-3", "Vbat,Vsolar,Temp,Sat")
	telemetry_unit_label_message("M0XER-3", "V,V,C,,m")

	telemetry_data_base91("M0XER-3", "DyR.&^<A!.", result, sizeofResult)

	assert.Equal(t, "10mW research balloon: Seq=3273, Vbat=4.472 V, Vsolar=0.516 V, Temp=-24.3 C, Sat=13", C.GoString(result), "test 401")
	assert.Empty(t, C.GoString(comment), "test 401")

	telemetry_data_base91("M0XER-3", "cNOv'C?=!-", result, sizeofResult)

	assert.Equal(t, "10mW research balloon: Seq=6051, Vbat=4.271 V, Vsolar=0.580 V, Temp=2.6 C, Sat=12", C.GoString(result), "test 402")
	assert.Empty(t, C.GoString(comment), "test 402")

	telemetry_data_base91("M0XER-3", "n0RS(:>b!+", result, sizeofResult)

	assert.Equal(t, "10mW research balloon: Seq=7022, Vbat=4.509 V, Vsolar=0.662 V, Temp=-2.8 C, Sat=10", C.GoString(result), "test 403")
	assert.Empty(t, C.GoString(comment), "test 403")

	telemetry_data_base91("M0XER-3", "x&G=!(8s!,", result, sizeofResult)

	assert.Equal(t, "10mW research balloon: Seq=7922, Vbat=3.486 V, Vsolar=0.007 V, Temp=-55.7 C, Sat=11", C.GoString(result), "test 404")
	assert.Empty(t, C.GoString(comment), "test 404")

	/* final score. */

	dw_printf("\nTEST WAS SUCCESSFUL.\n")
}

/*
	A more complete test can be performed by placing the following
	in a text file and feeding it into the "decode_aprs" utility.

2E0TOY>APRS::M0XER-3  :BITS.11111111,10mW research balloon
2E0TOY>APRS::M0XER-3  :PARM.Vbat,Vsolar,Temp,Sat
2E0TOY>APRS::M0XER-3  :EQNS.0,0.001,0,0,0.001,0,0,0.1,-273.2,0,1,0,0,1,0
2E0TOY>APRS::M0XER-3  :UNIT.V,V,C,,m
M0XER-3>APRS63,WIDE2-1:!//Bap'.ZGO JHAE/A=042496|E@Q0%i;5!-|
M0XER-3>APRS63,WIDE2-1:!/4\;u/)K$O J]YD/A=041216|h`RY(1>q!(|
M0XER-3>APRS63,WIDE2-1:!/23*f/R$UO Jf'x/A=041600|rxR_'J>+!(|

	The interpretation should look something like this:
	10mW research balloon: Seq=3307, Vbat=4.383 V, Vsolar=0.436 V, Temp=-34.6 C, Sat=12
*/
