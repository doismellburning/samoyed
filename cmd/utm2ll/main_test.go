package main

import "os"

// Examples from utm2ll.c source, checked against direwolf Debian package

func Example_main() { //nolint:testableexamples
	main()
	// TODO Can we do a partial match in the style of Python doctests?
}

func Example_main_1() {
	os.Args = []string{"utm2ll", "19T", "306130", "4726010"}

	main()
	// Output: from UTM, latitude = 42.662139, longitude = -71.365553
}

func Example_main_2() {
	os.Args = []string{"utm2ll", "19TCH06132600"}

	main()
	// Output:
	// from USNG, latitude = 42.662049, longitude = -71.365550
	// from MGRS, latitude = 42.662049, longitude = -71.365550
}
