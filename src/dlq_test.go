package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCDataNew_Empty(t *testing.T) {
	var cdata = cdata_new(1, nil)

	assert.Empty(t, cdata.data)
}

func TestCDataNew(t *testing.T) {
	// Because sometimes I didn't manage to get the copy right(!)
	var testData = []byte("badger")
	var cdata = cdata_new(1, testData)

	assert.Equal(t, cdata.data, testData)
}
