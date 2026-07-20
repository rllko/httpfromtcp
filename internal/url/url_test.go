package url

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathDecodeNoEscapes(t *testing.T) {
	s, err := PathDecode([]byte("/abc"))
	require.NoError(t, err)
	assert.Equal(t, "/abc", s)
}

func TestPathDecodeSpace(t *testing.T) {
	s, err := PathDecode([]byte("/a%20b"))
	require.NoError(t, err)
	assert.Equal(t, "/a b", s)
}

func TestPathDecodeInvalidHex(t *testing.T) {
	s, err := PathDecode([]byte("/a%zz"))
	require.Error(t, err)
	assert.Equal(t, "", s)
}

func TestPathDecodeTruncatedEscape(t *testing.T) {
	// Only one hex digit after the '%', and nothing after that.
	_, err := PathDecode([]byte("/a%2"))
	require.Error(t, err)

	// A lone '%' at the very end.
	_, err = PathDecode([]byte("/a%"))
	require.Error(t, err)
}

func TestPathDecodeLiteralPercent(t *testing.T) {
	s, err := PathDecode([]byte("/a%25"))
	require.NoError(t, err)
	assert.Equal(t, "/a%", s)
}

func TestPathDecodeNoDoubleDecode(t *testing.T) {
	// %2525 must decode exactly once: %25 -> '%', then "25" is literal.
	s, err := PathDecode([]byte("/a%2525"))
	require.NoError(t, err)
	assert.Equal(t, "/a%25", s)
}

func TestPathDecodeHexCaseInsensitive(t *testing.T) {
	upper, err := PathDecode([]byte("/a%41b"))
	require.NoError(t, err)
	lower, err2 := PathDecode([]byte("/a%41b"))
	require.NoError(t, err2)
	assert.Equal(t, "/aAb", upper)
	assert.Equal(t, upper, lower)
}

func TestParseQuery(t *testing.T) {
	u, err := Parse([]byte("/a?b=c"))
	require.NoError(t, err)
	assert.Equal(t, "/a", u.Path)
	assert.Equal(t, "b=c", u.RawQuery)
}

func TestParseDecodesPathKeepsQueryRaw(t *testing.T) {
	// The whole point of the split: left side decoded, right side untouched.
	u, err := Parse([]byte("/a%20b?x=%20"))
	require.NoError(t, err)
	assert.Equal(t, "/a b", u.Path)
	assert.Equal(t, "x=%20", u.RawQuery)
}

func TestParseNoQuery(t *testing.T) {
	u, err := Parse([]byte("*"))
	require.NoError(t, err)
	assert.Equal(t, "*", u.Path)
	assert.Equal(t, "", u.RawQuery)
}

func TestParseCutsAtFirstQuestionMark(t *testing.T) {
	// A '?' inside the query is data, not a second separator.
	u, err := Parse([]byte("a?b=c?d"))
	require.NoError(t, err)
	assert.Equal(t, "a", u.Path)
	assert.Equal(t, "b=c?d", u.RawQuery)
}

func TestParseInvalidHexInPath(t *testing.T) {
	u, err := Parse([]byte("/a%zz"))
	require.Error(t, err)
	assert.Nil(t, u)
}

func TestParseInvalidHexInPathWithQuery(t *testing.T) {
	// Proves the decode error survives the cut — the bad escape is in the
	// path half, and Parse must not swallow it.
	u, err := Parse([]byte("/a%zz?b=c"))
	require.Error(t, err)
	assert.Nil(t, u)
}

func TestParseRejectsEncodedNul(t *testing.T) {
	// Policy: a NUL can truncate paths in downstream C-based consumers.
	u, err := Parse([]byte("/a%00"))
	require.Error(t, err)
	assert.Nil(t, u)
}

func TestParseRejectsRawNul(t *testing.T) {
	u, err := Parse([]byte("/a\x00b"))
	require.Error(t, err)
	assert.Nil(t, u)
}

func TestParseRejectsEncodedSlash(t *testing.T) {
	u, err := Parse([]byte("/a%2fb"))
	require.Error(t, err)
	assert.Nil(t, u)

	u, err = Parse([]byte("/a%2Fb"))
	require.Error(t, err)
	assert.Nil(t, u)
}

func TestParseAllowsNormalSlashes(t *testing.T) {
	u, err := Parse([]byte("/a/b/c"))
	require.NoError(t, err)
	assert.Equal(t, "/a/b/c", u.Path)
}
