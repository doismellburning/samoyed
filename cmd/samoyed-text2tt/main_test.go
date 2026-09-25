package main

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
)

func Test_Text2TT(t *testing.T) {
	// From `man text2tt`
	testutils.AssertOutputContains(t, func() { text2tt([]string{"abcdefg", "0123"}) }, "2A22A2223A33A33340A00122223333")
	testutils.AssertOutputContains(t, func() { text2tt([]string{"abcdefg", "0123"}) }, "2A2B2C3A3B3C4A0A0123")
}
