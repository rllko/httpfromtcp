package headers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHeader(t *testing.T) {
	// Test: valid single header
	headers := NewHeaders()
	data := []byte("Host: localhost:42069\r\n\r\n")
	n, done, err := headers.Parse(data)
	require.NoError(t, err)
	require.NotNil(t, headers)
	headerVal, ok := headers.Get("Host")
	assert.True(t, true, ok)
	assert.Equal(t, "localhost:42069", headerVal)
	assert.Equal(t, 25, n)
	assert.True(t, done)

	// Test: valid special character
	headers = NewHeaders()
	data = []byte("Ho$t: localhost:42069\r\n\r\n")
	n, done, err = headers.Parse(data)
	require.NoError(t, err)
	require.NotNil(t, headers)
	assert.Equal(t, 25, n)
	assert.True(t, done)

	headers = NewHeaders()
	data = []byte("Set-Person: lane-loves-go\r\nSet-Person: prime-loves-zig\r\n\r\n")
	_, done, err = headers.Parse(data)
	require.NoError(t, err)
	require.NotNil(t, headers)
	headerVal, ok = headers.Get("Set-Person")
	assert.True(t, true, ok)
	assert.Equal(t, "lane-loves-go, prime-loves-zig", headerVal)
	assert.True(t, done)

	// Test: ivalid special character
	headers = NewHeaders()
	data = []byte("H©st: localhost:42069\r\n\r\n")
	n, done, err = headers.Parse(data)
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)

	// Test: Invalid spacing header prefix
	headers = NewHeaders()
	data = []byte("       Host: localhost:42069\r\n\r\n")
	n, done, err = headers.Parse(data)
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)

	// Test: Invalid spacing header suffix
	headers = NewHeaders()
	data = []byte("Host     : localhost:42069\r\n\r\n")
	n, done, err = headers.Parse(data)
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)
}

// Edge-case tests from EDGE_CASES.md §E2 — Headers.
// Tests marked "⚠ BUG" assert the CORRECT behavior and are expected to fail
// until the corresponding bug in headers.go is fixed.

func TestParseNoSpaceAfterColon(t *testing.T) {
	headers := NewHeaders()
	data := []byte("Host:x\r\n\r\n")
	n, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, len(data), n)
	val, ok := headers.Get("host")
	require.True(t, ok)
	assert.Equal(t, "x", val)
}

func TestParseTrimsOptionalWhitespace(t *testing.T) {
	headers := NewHeaders()
	data := []byte("Host:      x     \r\n\r\n")
	n, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, len(data), n)
	val, ok := headers.Get("host")
	require.True(t, ok)
	assert.Equal(t, "x", val)
}

func TestParsePreservesInternalSpaces(t *testing.T) {
	headers := NewHeaders()
	data := []byte("User-Agent: a b  c\r\n\r\n")
	_, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	val, ok := headers.Get("user-agent")
	require.True(t, ok)
	assert.Equal(t, "a b  c", val)
}

func TestParseValueWithColonsSplitsOnFirst(t *testing.T) {
	headers := NewHeaders()
	data := []byte("Authorization: Basic a:b:c\r\n\r\n")
	_, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	val, ok := headers.Get("authorization")
	require.True(t, ok)
	assert.Equal(t, "Basic a:b:c", val)
}

func TestParseAllTokenSpecialCharsInName(t *testing.T) {
	headers := NewHeaders()
	data := []byte("!#$%&'*+-.^_`|~: ok\r\n\r\n")
	_, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	val, ok := headers.Get("!#$%&'*+-.^_`|~")
	require.True(t, ok)
	assert.Equal(t, "ok", val)
}

func TestParseDigitInName(t *testing.T) {
	headers := NewHeaders()
	data := []byte("X-2-Fast: ok\r\n\r\n")
	_, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	val, ok := headers.Get("x-2-fast")
	require.True(t, ok)
	assert.Equal(t, "ok", val)
}

func TestParseDigitZeroInName(t *testing.T) {
	headers := NewHeaders()
	data := []byte("X-Version-0: ok\r\n\r\n")
	_, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	val, ok := headers.Get("x-version-0")
	require.True(t, ok)
	assert.Equal(t, "ok", val)
}

func TestParseEmptyValue(t *testing.T) {
	headers := NewHeaders()
	data := []byte("X-Empty:\r\n\r\n")
	_, done, err := headers.Parse(data)
	require.NoError(t, err)
	assert.True(t, done)
	val, ok := headers.Get("x-empty")
	assert.True(t, ok, "empty-valued header must still be present")
	assert.Equal(t, "", val)
}

func TestGetIsCaseInsensitive(t *testing.T) {
	headers := NewHeaders()
	_, _, err := headers.Parse([]byte("HoSt: localhost\r\n\r\n"))
	require.NoError(t, err)

	val, ok := headers.Get("host")
	require.True(t, ok)
	assert.Equal(t, "localhost", val)

	val, ok = headers.Get("HOST")
	require.True(t, ok)
	assert.Equal(t, "localhost", val)

	val, ok = headers.Get("hOsT")
	require.True(t, ok)
	assert.Equal(t, "localhost", val)
}

func TestParseSpaceBeforeColon(t *testing.T) {
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte("Host : x\r\n\r\n"))
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)
}

func TestParseTabBeforeColon(t *testing.T) {
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte("Host\t: x\r\n\r\n"))
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)
}

func TestParseMissingColon(t *testing.T) {
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte("Hostx\r\n\r\n"))
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)
}

func TestParseEmptyFieldName(t *testing.T) {
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte(": value\r\n\r\n"))
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)
}

func TestParseNonTokenCharInName(t *testing.T) {
	// Test: paren in field name
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte("H(st: x\r\n\r\n"))
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)

	// Test: comma in field name
	headers = NewHeaders()
	n, done, err = headers.Parse([]byte("Ho,st: x\r\n\r\n"))
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)
}

func TestParseDuplicateHeadersJoined(t *testing.T) {
	headers := NewHeaders()
	_, done, err := headers.Parse([]byte("Set-Person: a\r\nSet-Person: b\r\nSet-Person: c\r\n\r\n"))
	require.NoError(t, err)
	assert.True(t, done)
	val, ok := headers.Get("set-person")
	require.True(t, ok)
	assert.Equal(t, "a, b, c", val)
}

func TestSetJoinsConsistentlyWithParse(t *testing.T) {
	// Set and Parse must join repeated values the same way (", ").
	headers := NewHeaders()
	headers.Set("a", "1")
	headers.Set("a", "2")
	val, ok := headers.Get("a")
	require.True(t, ok)
	assert.Equal(t, "1, 2", val)
}

func TestParsePartialInputConsumesNothing(t *testing.T) {
	// Test: no \r\n yet — nothing consumed, not done, no error
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte("Host: x"))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)

	// Test: data ends in the middle of the \r\n separator
	headers = NewHeaders()
	n, done, err = headers.Parse([]byte("Host: x\r"))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, done)

	// Test: full line but no terminating blank line yet — consumes the line, not done
	headers = NewHeaders()
	n, done, err = headers.Parse([]byte("Host: x\r\n"))
	require.NoError(t, err)
	assert.Equal(t, len("Host: x\r\n"), n)
	assert.False(t, done)
}

func TestParseZeroHeaders(t *testing.T) {
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte("\r\n"))
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, 2, n)
}

func TestParseStopsAtBlankLine(t *testing.T) {
	// Bytes after the blank line belong to the body and must NOT be consumed.
	headers := NewHeaders()
	n, done, err := headers.Parse([]byte("A: b\r\n\r\nBODYBYTES"))
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, len("A: b\r\n\r\n"), n)
}
