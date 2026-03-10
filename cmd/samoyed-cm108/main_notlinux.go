//go:build !linux

package main

import (
	"fmt"
)

func main() {
	fmt.Println("cm108 not supported on !linux")
}
