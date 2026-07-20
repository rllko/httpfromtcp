// Tests for M2 (ParseQuoted, RFC 9110 §5.6.4) and M3 (ParseHeaderParams,
// RFC 9110 §5.6.6). See EDGE_CASES.md §M2/§M3.
package headers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ParseQuoted ---
// quoted-pair = "\" ( HTAB / SP / VCHAR / obs-text ): the backslash escapes
// the NEXT BYTE LITERALLY. There are no letter-escapes — `\n` is the letter n.

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
	// ';' and ',' inside quotes are data, not separators.
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
	require.ErrorIs(t, err, ErrMissingOpeningQuote)
	assert.Equal(t, 0, n)
}

func TestParseQuotedUnterminated(t *testing.T) {
	_, n, err := ParseQuoted(`"abc`)
	require.ErrorIs(t, err, ErrMissingClosingQuote)
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
	require.ErrorIs(t, err, ErrInvalidEscape)

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

// --- ParseToken ---

func TestParseTokenStopsAtDelimiter(t *testing.T) {
	token, n, err := ParseToken("utf-8; q=1")
	require.NoError(t, err)
	assert.Equal(t, "utf-8", token)
	assert.Equal(t, 5, n)
}

func TestParseTokenConsumesWholeInput(t *testing.T) {
	token, n, err := ParseToken("boundary--x")
	require.NoError(t, err)
	assert.Equal(t, "boundary--x", token)
	assert.Equal(t, len("boundary--x"), n)
}

func TestParseTokenEmptyInput(t *testing.T) {
	_, _, err := ParseToken("")
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseTokenNonTokenFirstChar(t *testing.T) {
	_, _, err := ParseToken("=abc")
	require.ErrorIs(t, err, ErrInvalidToken)
}

// --- ParseHeaderParams ---
// Contract: input is the part AFTER the base value ("; k=v; k2=v2").
// The caller splits the base value off first.

func TestParamsSingle(t *testing.T) {
	params, err := ParseHeaderParams("; charset=utf-8")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"charset": "utf-8"}, params)
}

func TestParamsMultiple(t *testing.T) {
	params, err := ParseHeaderParams("; charset=utf-8; q=0.9")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"charset": "utf-8", "q": "0.9"}, params)
}

func TestParamsSemicolonInsideQuotes(t *testing.T) {
	// The marquee case: a naive strings.Split on ';' breaks here.
	params, err := ParseHeaderParams(`; filename="a;b.pdf"; x=1`)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"filename": "a;b.pdf", "x": "1"}, params)
}

func TestParamsQuotedValueUnwrapped(t *testing.T) {
	params, err := ParseHeaderParams(`; q="0.5"`)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"q": "0.5"}, params)
}

func TestParamsEscapesInsideQuotedValue(t *testing.T) {
	params, err := ParseHeaderParams(`; name="a\"b"`)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"name": `a"b`}, params)
}

func TestParamsKeyCaseInsensitive(t *testing.T) {
	params, err := ParseHeaderParams("; CHARSET=utf-8")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"charset": "utf-8"}, params)
}

func TestParamsValueCasePreserved(t *testing.T) {
	// Values are NOT lowercased — matters for boundary= tokens.
	params, err := ParseHeaderParams("; charset=UTF-8")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"charset": "UTF-8"}, params)
}

func TestParamsDuplicateSameValue(t *testing.T) {
	// Pinned decision: identical duplicates collapse to one.
	params, err := ParseHeaderParams("; a=1; a=1")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "1"}, params)
}

func TestParamsDuplicateConflictingValue(t *testing.T) {
	// Pinned decision: conflicting duplicates are an error.
	_, err := ParseHeaderParams("; a=1; a=2")
	require.ErrorIs(t, err, ErrDuplicateParameter)
}

func TestParamsTrailingSemicolon(t *testing.T) {
	// Pinned decision: a trailing ';' is tolerated.
	params, err := ParseHeaderParams("; charset=utf-8;")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"charset": "utf-8"}, params)
}

func TestParamsWhitespaceTolerance(t *testing.T) {
	// Pinned decision: OWS around ';' and '=' is accepted (lenient — the RFC
	// forbids space around '=', we choose to allow it).
	params, err := ParseHeaderParams(" ;  charset = utf-8 ; q=1")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"charset": "utf-8", "q": "1"}, params)
}

func TestParamsEmptyInput(t *testing.T) {
	params, err := ParseHeaderParams("")
	require.NoError(t, err)
	assert.Empty(t, params)
}

func TestParamsEmptyValue(t *testing.T) {
	// Grammar: value must be a token or quoted-string; "a=" has neither.
	_, err := ParseHeaderParams("; a=")
	require.ErrorIs(t, err, ErrInvalidHeaderParameter)
}

func TestParamsKeyWithoutEquals(t *testing.T) {
	_, err := ParseHeaderParams("; foo")
	require.ErrorIs(t, err, ErrInvalidHeaderParameter)
}

func TestParamsMissingLeadingSemicolon(t *testing.T) {
	// Contract: the base value must be split off by the caller first.
	_, err := ParseHeaderParams("text/html; charset=utf-8")
	require.ErrorIs(t, err, ErrInvalidHeaderParameter)
}

func TestParamsUnterminatedQuotedValue(t *testing.T) {
	_, err := ParseHeaderParams(`; a="bc`)
	require.ErrorIs(t, err, ErrInvalidHeaderParameter)
}

func TestParamsRealisticContentType(t *testing.T) {
	params, err := ParseHeaderParams("; boundary=----WebKitFormBoundary7MA4")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"boundary": "----WebKitFormBoundary7MA4"}, params)
}

// --- ParseHeaderValue ---
// The full "base; params" entry point: splits the base value off, then
// delegates the rest to ParseHeaderParams.

func TestHeaderValueBaseOnly(t *testing.T) {
	base, params, err := ParseHeaderValue("text/html")
	require.NoError(t, err)
	assert.Equal(t, "text/html", base)
	assert.Empty(t, params)
}

func TestHeaderValueBaseAndParams(t *testing.T) {
	base, params, err := ParseHeaderValue("text/html; charset=utf-8")
	require.NoError(t, err)
	assert.Equal(t, "text/html", base)
	assert.Equal(t, map[string]string{"charset": "utf-8"}, params)
}

func TestHeaderValueSemicolonInsideQuotes(t *testing.T) {
	// End-to-end through the wrapper: the quoted ';' must survive.
	base, params, err := ParseHeaderValue(`attachment; filename="a;b.pdf"`)
	require.NoError(t, err)
	assert.Equal(t, "attachment", base)
	assert.Equal(t, map[string]string{"filename": "a;b.pdf"}, params)
}

func TestHeaderValueEmptyBase(t *testing.T) {
	_, _, err := ParseHeaderValue("; a=b")
	require.ErrorIs(t, err, ErrInvalidHeaderParameter)
}

func TestHeaderValueBaseTrimmed(t *testing.T) {
	base, params, err := ParseHeaderValue("  text/html  ; a=b")
	require.NoError(t, err)
	assert.Equal(t, "text/html", base)
	assert.Equal(t, map[string]string{"a": "b"}, params)
}

func TestHeaderValueParamErrorPropagates(t *testing.T) {
	// A bad param section must fail the whole parse, not silently drop.
	_, _, err := ParseHeaderValue(`text/html; a="unterminated`)
	require.Error(t, err)
}
