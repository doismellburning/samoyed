package direwolf

// #include "direwolf.h"
// #include <unistd.h>
// #include <assert.h>
// #include <string.h>
// #include <stdlib.h>
// #include <stdio.h>
// #include <ctype.h>
// #include "ax25_pad.h"
// #include "textcolor.h"
// #include "decode_aprs.h"
// #include "latlong.h"
// #include "pfilter.h"
// #include "mheard.h"
// int pftest_running;
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var pftest_error_count int

func pf_test_main(t *testing.T) {
	t.Helper()

	// Setup
	var p_igate_config C.struct_igate_config_s
	p_igate_config.max_digi_hops = 2
	C.pfilter_init(&p_igate_config, 0)
	C.pftest_running = C.int(1) // Change behaviour in pfilter.c to terminate early for test convenience

	dw_printf("Quick test for packet filtering.\n")
	dw_printf("Some error messages are normal.  Look at the final success/fail message.\n")

	pftest(t, 1, "", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 2, "0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 3, "1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)

	pftest(t, 10, "0 | 0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 11, "0 | 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 12, "1 | 0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 13, "1 | 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 14, "0 | 0 | 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)

	pftest(t, 20, "0 & 0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 21, "0 & 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 22, "1 & 0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 23, "1 & 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 24, "1 & 1 & 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 24, "1 & 0 & 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 24, "1 & 1 & 0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)

	pftest(t, 30, "0 | ! 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 31, "! 1 | ! 0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 32, "! ! 1 | 0", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 33, "1 | ! ! 1", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)

	pftest(t, 40, "1 &(!0 |0 )", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 41, "0 |(!0 )", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 42, "1 |(!!0 )", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 42, "(!(1 ) & (1 ))", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)

	pftest(t, 50, "b/W2UB/WB2OSZ-5/N2GH", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 51, "b/W2UB/WB2OSZ-14/N2GH", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 52, "b#W2UB#WB2OSZ-5#N2GH", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 53, "b#W2UB#WB2OSZ-14#N2GH", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)

	pftest(t, 60, "o/HOME", "WB2OSZ>APDW12,WIDE1-1,WIDE2-1:;home     *111111z4237.14N/07120.83W-Chelmsford MA", 0)
	pftest(t, 61, "o/home", "WB2OSZ>APDW12,WIDE1-1,WIDE2-1:;home     *111111z4237.14N/07120.83W-Chelmsford MA", 1)
	pftest(t, 62, "o/HOME", "HOME>APDW12,WIDE1-1,WIDE2-1:;AWAY     *111111z4237.14N/07120.83W-Chelmsford MA", 0)
	pftest(t, 63, "o/WB2OSZ-5", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 64, "o/HOME", "WB2OSZ>APDW12,WIDE1-1,WIDE2-1:)home!4237.14N/07120.83W-Chelmsford MA", 0)
	pftest(t, 65, "o/home", "WB2OSZ>APDW12,WIDE1-1,WIDE2-1:)home!4237.14N/07120.83W-Chelmsford MA", 1)

	pftest(t, 70, "d/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 71, "d/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1*,DIGI2,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 72, "d/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 73, "d/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2,DIGI3*,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 74, "d/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2,DIGI3,DIGI4*:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 75, "d/DIGI9/DIGI2", "WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)

	pftest(t, 80, "g/W2UB", "WB2OSZ-5>APDW12::W2UB     :text", 1)
	pftest(t, 81, "g/W2UB/W2UB-*", "WB2OSZ-5>APDW12::W2UB-9   :text", 1)
	pftest(t, 82, "g/W2UB/*", "WB2OSZ-5>APDW12::XXX      :text", 1)
	pftest(t, 83, "g/W2UB/W*UB", "WB2OSZ-5>APDW12::W2UB-9   :text", -1)
	pftest(t, 84, "g/W2UB*", "WB2OSZ-5>APDW12::W2UB-9   :text", 1)
	pftest(t, 85, "g/W2UB*", "WB2OSZ-5>APDW12::W2UBZZ   :text", 1)
	pftest(t, 86, "g/W2UB", "WB2OSZ-5>APDW12::W2UB-9   :text", 0)
	pftest(t, 87, "g/*", "WB2OSZ-5>APDW12:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 88, "g/W*", "WB2OSZ-5>APDW12:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)

	pftest(t, 90, "u/APWW10", "WA1PLE-5>APWW10,W1MHL,N8VIM,WIDE2*:@022301h4208.75N/07115.16WoAPRS-IS for Win32", 1)
	pftest(t, 91, "u/TRSY3T", "W1WRA-7>TRSY3T,WIDE1-1,WIDE2-1:`c-:l!hK\\>\"4b}=<0x0d>", 0)
	pftest(t, 92, "u/APDW11/APDW12", "WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 93, "u/APDW", "WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)

	// rather sparse coverage of the cases
	pftest(t, 100, "t/mqt", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 101, "t/mqtp", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 102, "t/mqtp", "WB2OSZ>APDW12,WIDE1-1,WIDE2-1:;home     *111111z4237.14N/07120.83W-Chelmsford MA", 0)
	pftest(t, 103, "t/mqop", "WB2OSZ>APDW12,WIDE1-1,WIDE2-1:;home     *111111z4237.14N/07120.83W-Chelmsford MA", 1)
	pftest(t, 104, "t/p", "W1WRA-7>TRSY3T,WIDE1-1,WIDE2-1:`c-:l!hK\\>\"4b}=<0x0d>", 1)
	pftest(t, 104, "t/s", "KB1CHU-13>APWW10,W1CLA-1*,WIDE2-1:>FN42pb/_DX: W1MHL 36.0mi 306<0xb0> 13:24 4223.32N 07115.23W", 1)

	pftest(t, 110, "t/p", "N8VIM>APN391,AB1OC-10,W1MRA*,WIDE2:$ULTW0000000001110B6E27F4FFF3897B0001035E004E04DD00030000<0x0d><0x0a>", 0)
	pftest(t, 111, "t/w", "N8VIM>APN391,AB1OC-10,W1MRA*,WIDE2:$ULTW0000000001110B6E27F4FFF3897B0001035E004E04DD00030000<0x0d><0x0a>", 1)
	pftest(t, 112, "t/t", "WM1X>APU25N:@210147z4235.39N/07106.58W_359/000g000t027r000P000p000h89b10234/WX REPORT {UIV32N}<0x0d>", 0)
	pftest(t, 113, "t/w", "WM1X>APU25N:@210147z4235.39N/07106.58W_359/000g000t027r000P000p000h89b10234/WX REPORT {UIV32N}<0x0d>", 1)

	/* Telemetry metadata should not be classified as message. */
	pftest(t, 114, "t/t", "KJ4SNT>APMI04::KJ4SNT   :PARM.Vin,Rx1h,Dg1h,Eff1h,Rx10m,O1,O2,O3,O4,I1,I2,I3,I4", 1)
	pftest(t, 115, "t/m", "KJ4SNT>APMI04::KJ4SNT   :PARM.Vin,Rx1h,Dg1h,Eff1h,Rx10m,O1,O2,O3,O4,I1,I2,I3,I4", 0)
	pftest(t, 116, "t/t", "KB1GKN-10>APRX27,UNCAN,WIDE1*:T#491,4.9,0.3,25.0,0.0,1.0,00000000", 1)

	/* Bulletins should not be considered to be messages.  Was bug in 1.6. */
	pftest(t, 117, "t/m", "A>B::W1AW     :test", 1)
	pftest(t, 118, "t/m", "A>B::BLN      :test", 0)
	pftest(t, 119, "t/m", "A>B::NWS      :test", 0)

	// https://www.aprs-is.net/WX/
	pftest(t, 121, "t/p", "CWAPID>APRS::NWS-TTTTT:DDHHMMz,ADVISETYPE,zcs{seq#", 0)
	pftest(t, 122, "t/p", "CWAPID>APRS::SKYCWA   :DDHHMMz,ADVISETYPE,zcs{seq#", 0)
	pftest(t, 123, "t/p", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", 0)
	pftest(t, 124, "t/n", "CWAPID>APRS::NWS-TTTTT:DDHHMMz,ADVISETYPE,zcs{seq#", 1)
	pftest(t, 125, "t/n", "CWAPID>APRS::SKYCWA   :DDHHMMz,ADVISETYPE,zcs{seq#", 1)
	// pftest (t, 126, "t/n", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", 1);
	pftest(t, 127, "t/", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", 0)

	pftest(t, 128, "t/c", "S0RCE>DEST:<stationcapabilities", 1)
	pftest(t, 129, "t/h", "S0RCE>DEST:<stationcapabilities", 0)
	pftest(t, 130, "t/h", "S0RCE>DEST:}WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 131, "t/c", "S0RCE>DEST:}WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)

	pftest(t, 140, "r/42.6/-71.3/10", "WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 141, "r/42.6/-71.3/10", "WA1PLE-5>APWW10,W1MHL,N8VIM,WIDE2*:@022301h4208.75N/07115.16WoAPRS-IS for Win32", 0)

	pftest(t, 145, "( t/t & b/WB2OSZ ) | ( t/o & ! r/42.6/-71.3/1 )", "WB2OSZ>APDW12:;home     *111111z4237.14N/07120.83W-Chelmsford MA", 1)

	pftest(t, 150, "s/->", "WB2OSZ-5>APDW12:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 151, "s/->", "WB2OSZ-5>APDW12:!4237.14N/07120.83W-PHG7140Chelmsford MA", 1)
	pftest(t, 152, "s/->", "WB2OSZ-5>APDW12:!4237.14N/07120.83W>PHG7140Chelmsford MA", 1)
	pftest(t, 153, "s/->", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W>PHG7140Chelmsford MA", 0)

	pftest(t, 154, "s//#", "WB2OSZ-5>APDW12:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 155, "s//#", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 156, "s//#", "WB2OSZ-5>APDW12:!4237.14N/07120.83W#PHG7140Chelmsford MA", 0)

	pftest(t, 157, "s//#/\\", "WB2OSZ-5>APDW12:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 158, "s//#/\\", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 159, "s//#/\\", "WB2OSZ-5>APDW12:!4237.14N/07120.83W#PHG7140Chelmsford MA", 0)

	pftest(t, 160, "s//#/LS1", "WB2OSZ-5>APDW12:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 161, "s//#/LS1", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 162, "s//#/LS1", "WB2OSZ-5>APDW12:!4237.14N/07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 163, "s//#/LS\\", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W#PHG7140Chelmsford MA", 1)

	pftest(t, 170, "s:/", "WB2OSZ-5>APDW12:!4237.14N/07120.83W/PHG7140Chelmsford MA", 1)
	pftest(t, 171, "s:/", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W/PHG7140Chelmsford MA", 0)
	pftest(t, 172, "s::/", "WB2OSZ-5>APDW12:!4237.14N/07120.83W/PHG7140Chelmsford MA", 0)
	pftest(t, 173, "s::/", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W/PHG7140Chelmsford MA", 1)
	pftest(t, 174, "s:/:/", "WB2OSZ-5>APDW12:!4237.14N/07120.83W/PHG7140Chelmsford MA", 1)
	pftest(t, 175, "s:/:/", "WB2OSZ-5>APDW12:!4237.14N\\07120.83W/PHG7140Chelmsford MA", 1)
	pftest(t, 176, "s:/:/", "WB2OSZ-5>APDW12:!4237.14NX07120.83W/PHG7140Chelmsford MA", 1)
	pftest(t, 177, "s:/:/:X", "WB2OSZ-5>APDW12:!4237.14NX07120.83W/PHG7140Chelmsford MA", 1)

	// FIXME: Different on Windows and  64 bit Linux.
	// pftest (t, 178, "s:/:/:", "WB2OSZ-5>APDW12:!4237.14NX07120.83W/PHG7140Chelmsford MA", 1);

	pftest(t, 179, "s:/:/:\\", "WB2OSZ-5>APDW12:!4237.14NX07120.83W/PHG7140Chelmsford MA", 0)

	pftest(t, 180, "v/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 181, "v/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1*,DIGI2,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 182, "v/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 183, "v/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2,DIGI3*,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 184, "v/DIGI2/DIGI3", "WB2OSZ-5>APDW12,DIGI1,DIGI2,DIGI3,DIGI4*:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)
	pftest(t, 185, "v/DIGI9/DIGI2", "WB2OSZ-5>APDW12,DIGI1,DIGI2*,DIGI3,DIGI4:!4237.14NS07120.83W#PHG7140Chelmsford MA", 0)

	/* Test error reporting. */

	pftest(t, 200, "x/", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", -1)
	pftest(t, 201, "t/w & ( t/w | t/w ", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", -1)
	pftest(t, 202, "t/w ) ", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", -1)
	pftest(t, 203, "!", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", -1)
	pftest(t, 203, "t/w t/w", "CWAPID>APRS:;CWAttttz *DDHHMMzLATLONICONADVISETYPE{seq#", -1)
	pftest(t, 204, "r/42.6/-71.3", "WA1PLE-5>APWW10,W1MHL,N8VIM,WIDE2*:@022301h4208.75N/07115.16WoAPRS-IS for Win32", -1)

	pftest(t, 210, "i/30/8/42.6/-71.3/50", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1)
	pftest(t, 212, "i/30/8/42.6/-71.3/", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", -1)
	pftest(t, 213, "i/30/8/42.6/-71.3", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", -1)
	pftest(t, 214, "i/30/8/42.6/", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", -1)
	pftest(t, 215, "i/30/8/42.6", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", -1)
	pftest(t, 216, "i/30/8/", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1)
	pftest(t, 217, "i/30/8", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1)

	// FIXME: behaves differently on Windows and Linux.  Why?
	// On Windows we have our own version of strsep because it's not in the MS library.
	// It must behave differently than the Linux version when nothing follows the last separator.
	// pftest (t, 228, "i/30/",                "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1);

	pftest(t, 229, "i/30", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1)
	pftest(t, 230, "i/30", "X>X:}WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1)
	pftest(t, 231, "i/", "WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", -1)

	// Besure bulletins and telemetry metadata don't get included.
	pftest(t, 234, "i/30", "KJ4SNT>APMI04::KJ4SNT   :PARM.Vin,Rx1h,Dg1h,Eff1h,Rx10m,O1,O2,O3,O4,I1,I2,I3,I4", 0)
	pftest(t, 235, "i/30", "A>B::BLN      :test", 0)

	pftest(t, 240, "s/", "WB2OSZ-5>APDW12:!4237.14N/07120.83WOPHG7140Chelmsford MA", -1)
	pftest(t, 241, "s/'/O/-/#/_", "WB2OSZ-5>APDW12:!4237.14N/07120.83WOPHG7140Chelmsford MA", -1)
	pftest(t, 242, "s/O/O/c", "WB2OSZ-5>APDW12:!4237.14N/07120.83WOPHG7140Chelmsford MA", -1)
	pftest(t, 243, "s/O/O/1/2", "WB2OSZ-5>APDW12:!4237.14N/07120.83WOPHG7140Chelmsford MA", -1)
	pftest(t, 244, "s/O/|/1", "WB2OSZ-5>APDW12:!4237.14N/07120.83WOPHG7140Chelmsford MA", -1)
	pftest(t, 245, "s//", "WB2OSZ-5>APDW12:!4237.14N/07120.83WOPHG7140Chelmsford MA", -1)
	pftest(t, 246, "s///", "WB2OSZ-5>APDW12:!4237.14N/07120.83WOPHG7140Chelmsford MA", -1)

	// Third party header - done properly in 1.7.
	// Packet filter t/h is no longer a mutually exclusive packet type.
	// Now it is an independent attribute and the encapsulated part is evaluated.

	pftest(t, 250, "o/home", "A>B:}WB2OSZ>APDW12,WIDE1-1,WIDE2-1:;home     *111111z4237.14N/07120.83W-Chelmsford MA", 1)
	pftest(t, 251, "t/p", "A>B:}W1WRA-7>TRSY3T,WIDE1-1,WIDE2-1:`c-:l!hK\\>\"4b}=<0x0d>", 1)
	pftest(t, 252, "i/180", "A>B:}WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1)
	pftest(t, 253, "t/m", "A>B:}WB2OSZ-5>APDW14::W2UB     :Happy Birthday{001", 1)
	pftest(t, 254, "r/42.6/-71.3/10", "A>B:}WB2OSZ-5>APDW12,WIDE1-1,WIDE2-1:!4237.14NS07120.83W#PHG7140Chelmsford MA", 1)
	pftest(t, 254, "r/42.6/-71.3/10", "A>B:}WA1PLE-5>APWW10,W1MHL,N8VIM,WIDE2*:@022301h4208.75N/07115.16WoAPRS-IS for Win32", 0)
	pftest(t, 255, "t/h", "KB1GKN-10>APRX27,UNCAN,WIDE1*:T#491,4.9,0.3,25.0,0.0,1.0,00000000", 0)
	pftest(t, 256, "t/h", "A>B:}KB1GKN-10>APRX27,UNCAN,WIDE1*:T#491,4.9,0.3,25.0,0.0,1.0,00000000", 1)
	pftest(t, 258, "t/t", "A>B:}KB1GKN-10>APRX27,UNCAN,WIDE1*:T#491,4.9,0.3,25.0,0.0,1.0,00000000", 1)
	pftest(t, 259, "t/t", "A>B:}KJ4SNT>APMI04::KJ4SNT   :PARM.Vin,Rx1h,Dg1h,Eff1h,Rx10m,O1,O2,O3,O4,I1,I2,I3,I4", 1)

	pftest(t, 270, "g/BLN*", "WB2OSZ>APDW17::BLN1xxxxx:bulletin text", 1)
	pftest(t, 271, "g/BLN*", "A>B:}WB2OSZ>APDW17::BLN1xxxxx:bulletin text", 1)
	pftest(t, 272, "g/BLN*", "A>B:}WB2OSZ>APDW17::W1AW     :xxxx", 0)

	pftest(t, 273, "g/NWS*", "WB2OSZ>APDW17::NWS-xxxxx:weather bulletin", 1)
	pftest(t, 274, "g/NWS*", "A>B:}WB2OSZ>APDW17::NWS-xxxxx:weather bulletin", 1)
	pftest(t, 275, "g/NWS*", "A>B:}WB2OSZ>APDW17::W1AW     :xxxx", 0)

	// TODO: add b/ with 3rd party header.

	// TODO: to be continued...  directed query ...

	if pftest_error_count > 0 {
		C.text_color_set(C.DW_COLOR_ERROR)
		dw_printf("\nPacket Filtering Test - FAILED!     %d errors\n", pftest_error_count)
		t.Fail()
	}
	C.text_color_set(C.DW_COLOR_REC)
	dw_printf("\nPacket Filtering Test - SUCCESS!\n")
}

func pftest(t *testing.T, test_num int, filter string, monitor string, expected int) {
	t.Helper()

	C.text_color_set(C.DW_COLOR_DEBUG)
	dw_printf("test number %d\n", test_num)

	var pp = C.ax25_from_text(C.CString(monitor), 1)
	assert.NotNil(t, pp)

	var result = C.pfilter(0, 0, C.CString(filter), pp, 1)
	if !assert.Equal(t, result, C.int(expected), "Unexpected result for test number %d", test_num) {
		pftest_error_count++
	}

	C.ax25_delete(pp)
}
