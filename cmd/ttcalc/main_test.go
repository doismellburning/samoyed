package main

import "testing"

func test_calculator(t *testing.T) {
	assert(calculator("12a34#") == 46)
	assert(calculator("2*3A4#") == 10)
	assert(calculator("5*100A3#") == 503)
	assert(calculator("6a4*5#") == 50)
}
