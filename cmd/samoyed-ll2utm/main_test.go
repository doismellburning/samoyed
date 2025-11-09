package main

import "os"

// Examples from ll2utm.c source, checked against direwolf Debian package

func Example_main() { //nolint:testableexamples
	os.Args = []string{"ll2utm"}

	main()
	// TODO Can we do a partial match in the style of Python doctests?
}

func Example_main_1() {
	os.Args = []string{"ll2utm", "42.662139", "-71.365553"}

	main()
	// Output:
	// UTM zone = 19, hemisphere = N, easting = 306130, northing = 4726010
	// MGRS =  19TCH13  19TCH0626  19TCH061260  19TCH06132601  19TCH0613026010
	// USNG =  19TCH02  19TCH0626  19TCH061260  19TCH06132600  19TCH0613026009
}
