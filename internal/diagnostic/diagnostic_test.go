package diagnostic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnderline(t *testing.T) {
	str := "/files/*path/edit"
	strLen := len(str)

	// Test: start < 0
	start := -5
	end := 5
	res := Underline(str, start, end)
	assert.Equal(t, len(res), strLen+1+5)

	// Test: normal use case
	start = 7
	end = 12
	res = Underline(str, start, end)
	assert.Equal(t, len(res), strLen+1+start+(end-start))

	// Test: zero width
	start = 5
	end = 5
	res = Underline(str, start, end)
	assert.Equal(t, len(res), strLen+1+end)

	// Test: backwards
	start = 12
	end = 7
	res = Underline(str, start, end)
	assert.Equal(t, len(res), strLen+1+end)

	// Test: len over the str size
	start = 99
	end = 120
	res = Underline(str, start, end)
	// i set the edge case to make it len(str) as the end....
	assert.Equal(t, len(res), strLen+1+strLen)
}
