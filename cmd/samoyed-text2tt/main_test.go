package main

import (
	"testing"

	direwolf "github.com/doismellburning/samoyed/src"
)

func Test_Text2TT(t *testing.T) {
	// From `man text2tt`
	direwolf.AssertOutputContains(t, func() { Text2TT([]string{"abcdefg", "0123"}) }, "2A22A2223A33A33340A00122223333")
	direwolf.AssertOutputContains(t, func() { Text2TT([]string{"abcdefg", "0123"}) }, "2A2B2C3A3B3C4A0A0123")
}
