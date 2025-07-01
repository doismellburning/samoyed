package main

import (
	"testing"

	direwolf "github.com/doismellburning/samoyed/src"
)

func Test_TT2Text(t *testing.T) {
	// From `man tt2text`
	direwolf.AssertOutputContains(t, func() { TT2Text("2A22A2223A33A33340A00122223333") }, "ABCDEFG 0123")
	direwolf.AssertOutputContains(t, func() { TT2Text("2A22A2223A33A33340A00122223333") }, "A2A222D3D3334 00122223333")
}
