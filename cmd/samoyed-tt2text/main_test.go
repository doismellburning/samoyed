package main

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
)

func Test_TT2Text(t *testing.T) {
	// From `man tt2text`
	testutils.AssertOutputContains(t, func() { tt2text("2A22A2223A33A33340A00122223333") }, "ABCDEFG 0123")
	testutils.AssertOutputContains(t, func() { tt2text("2A22A2223A33A33340A00122223333") }, "A2A222D3D3334 00122223333")
}
