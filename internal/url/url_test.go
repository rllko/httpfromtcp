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
	// Same policy, unescaped form — this is why the NUL check runs AFTER
	// decoding, so both spellings are caught by one rule.
	u, err := Parse([]byte("/a\x00b"))
	require.Error(t, err)
	assert.Nil(t, u)
}

func TestParseRejectsEncodedSlash(t *testing.T) {
	// Policy: an encoded '/' would forge a segment boundary once decoded.
	// Both hex spellings must be rejected — %2F is well-formed, we refuse it
	// deliberately (see LEARNING.md M1).
	u, err := Parse([]byte("/a%2fb"))
	require.Error(t, err)
	assert.Nil(t, u)

	u, err = Parse([]byte("/a%2Fb"))
	require.Error(t, err)
	assert.Nil(t, u)
}

func TestParseAllowsNormalSlashes(t *testing.T) {
	// The encoded-slash policy must not reject ordinary paths.
	u, err := Parse([]byte("/a/b/c"))
	require.NoError(t, err)
	assert.Equal(t, "/a/b/c", u.Path)
}

// RFC 9110 §5.6.4:
//
//	quoted-string = DQUOTE *( qdtext / quoted-pair ) DQUOTE
//	quoted-pair   = "\" ( HTAB / SP / VCHAR / obs-text )
//
// A backslash escapes the NEXT BYTE LITERALLY — there are no letter-escapes
// like JSON's \n or \t. `\n` means the letter n.

func TestParseQuotedSimple(t *testing.T) {
	parsed, n, err := ParseQuoted(`"abc"`)
	require.NoError(t, err)
	assert.Equal(t, "abc", parsed)
	assert.Equal(t, 5, n, "n counts both quotes")
}

func TestParseQuotedEmpty(t *testing.T) {
	parsed, n, err := ParseQuoted(`""`)
	require.NoError(t, err)
	assert.Equal(t, "", parsed)
	assert.Equal(t, 2, n)
}

func TestParseQuotedEscapedQuote(t *testing.T) {
	parsed, n, err := ParseQuoted(`"a\"b"`)
	require.NoError(t, err)
	assert.Equal(t, `a"b`, parsed)
	assert.Equal(t, 6, n)
}

func TestParseQuotedEscapedBackslash(t *testing.T) {
	parsed, _, err := ParseQuoted(`"a\\b"`)
	require.NoError(t, err)
	assert.Equal(t, `a\b`, parsed)
}

func TestParseQuotedEscapedOrdinaryChar(t *testing.T) {
	// Escaping a plain char is legal; the backslash is dropped, the char kept.
	parsed, _, err := ParseQuoted(`"a\bc"`)
	require.NoError(t, err)
	assert.Equal(t, "abc", parsed)
}

func TestParseQuotedBackslashNIsLetterN(t *testing.T) {
	// NOT a newline — this is the JSON habit the RFC does not share.
	parsed, _, err := ParseQuoted(`"a\nb"`)
	require.NoError(t, err)
	assert.Equal(t, "anb", parsed)

	// Same for \t: the letter t, not a tab.
	parsed, _, err = ParseQuoted(`"a\tb"`)
	require.NoError(t, err)
	assert.Equal(t, "atb", parsed)
}

func TestParseQuotedPreservesWhitespace(t *testing.T) {
	parsed, _, err := ParseQuoted(`"  a  b  "`)
	require.NoError(t, err)
	assert.Equal(t, "  a  b  ", parsed)
}

func TestParseQuotedDelimitersAreLiteralInside(t *testing.T) {
	// The M3 case: ';' and ',' inside quotes are data, not separators.
	parsed, _, err := ParseQuoted(`"a;b,c"`)
	require.NoError(t, err)
	assert.Equal(t, "a;b,c", parsed)
}

func TestParseQuotedReportsRemainder(t *testing.T) {
	// M3 needs to keep parsing after the string ends: n says where it stopped.
	input := `"0.5"; charset=utf-8`
	parsed, n, err := ParseQuoted(input)
	require.NoError(t, err)
	assert.Equal(t, "0.5", parsed)
	assert.Equal(t, `"0.5"`, input[:n])
	assert.Equal(t, "; charset=utf-8", input[n:])
}

func TestParseQuotedStopsAtFirstUnescapedQuote(t *testing.T) {
	parsed, n, err := ParseQuoted(`"ab"cd`)
	require.NoError(t, err)
	assert.Equal(t, "ab", parsed)
	assert.Equal(t, "cd", `"ab"cd`[n:])
}

func TestParseQuotedNoOpeningQuote(t *testing.T) {
	_, n, err := ParseQuoted(`abc`)
	require.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestParseQuotedUnterminated(t *testing.T) {
	_, n, err := ParseQuoted(`"abc`)
	require.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestParseQuotedTrailingEscape(t *testing.T) {
	// Backslash with nothing after it: the closing quote never arrives.
	_, _, err := ParseQuoted(`"abc\`)
	require.Error(t, err)
}

func TestParseQuotedEscapedControlChar(t *testing.T) {
	// quoted-pair allows HTAB/SP/VCHAR/obs-text — not control characters.
	_, _, err := ParseQuoted("\"a\\\x00b\"")
	require.Error(t, err)

	// But an escaped real tab IS legal.
	parsed, _, err := ParseQuoted("\"a\\\tb\"")
	require.NoError(t, err)
	assert.Equal(t, "a\tb", parsed)
}

func TestParseQuotedTooShort(t *testing.T) {
	_, _, err := ParseQuoted(`"`)
	require.Error(t, err)

	_, _, err = ParseQuoted(``)
	require.Error(t, err)
}
