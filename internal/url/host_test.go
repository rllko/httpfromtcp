// Tests for M4 — Host validation (RFC 9110 §7.2, RFC 3986 §3.2.2).
// See EDGE_CASES.md §M4 for the full catalog and LEARNING.md for the
// pinned decisions.
package url

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- reg-name ---

func TestHostRegName(t *testing.T) {
	assert.NoError(t, ValidateHost("example.com"))
	assert.NoError(t, ValidateHost("localhost"))
	assert.NoError(t, ValidateHost("sub.domain.example.com"))
}

func TestHostRegNameWithPort(t *testing.T) {
	assert.NoError(t, ValidateHost("example.com:8080"))
	assert.NoError(t, ValidateHost("localhost:42069"))
}

func TestHostTrailingDot(t *testing.T) {
	// A trailing dot is a valid FQDN.
	assert.NoError(t, ValidateHost("example.com."))
}

func TestHostUppercase(t *testing.T) {
	// Host names are case-insensitive; uppercase is legal.
	assert.NoError(t, ValidateHost("EXAMPLE.COM"))
}

func TestHostPunycodeAndDigits(t *testing.T) {
	assert.NoError(t, ValidateHost("xn--nxasmq6b.example"))
	assert.NoError(t, ValidateHost("123.example"))
}

func TestHostUnderscore(t *testing.T) {
	// '_' is in the RFC 3986 unreserved set, so it is legal in a reg-name.
	assert.NoError(t, ValidateHost("my_host"))
}

func TestHostEmpty(t *testing.T) {
	require.ErrorIs(t, ValidateHost(""), ErrInvalidHost)
}

func TestHostIllegalChars(t *testing.T) {
	assert.Error(t, ValidateHost("exa mple.com"), "space")
	assert.Error(t, ValidateHost("ex/ample.com"), "slash")
	assert.Error(t, ValidateHost("ex@mple.com"), "at sign")
	assert.Error(t, ValidateHost("ex#mple.com"), "hash")
}

func TestHostPercentEncodingRejected(t *testing.T) {
	// Pinned decision: pct-encoded bytes in a host are rejected,
	// matching net/http.
	require.ErrorIs(t, ValidateHost("ex%41mple.com"), ErrInvalidHost)
}

// --- port ---

func TestHostPortBounds(t *testing.T) {
	assert.NoError(t, ValidateHost("example.com:0"))
	assert.NoError(t, ValidateHost("example.com:65535"))
	assert.ErrorIs(t, ValidateHost("example.com:65536"), ErrInvalidPort)
	assert.ErrorIs(t, ValidateHost("example.com:99999"), ErrInvalidPort)
}

func TestHostPortNonNumeric(t *testing.T) {
	require.ErrorIs(t, ValidateHost("example.com:abc"), ErrInvalidPort)
}

func TestHostPortEmpty(t *testing.T) {
	// Pinned decision: a trailing ':' with no digits is rejected.
	require.ErrorIs(t, ValidateHost("example.com:"), ErrInvalidPort)
}

// --- IPv4 ---

func TestHostIPv4(t *testing.T) {
	assert.NoError(t, ValidateHost("127.0.0.1"))
	assert.NoError(t, ValidateHost("0.0.0.0"))
	assert.NoError(t, ValidateHost("255.255.255.255"))
	assert.NoError(t, ValidateHost("127.0.0.1:8080"))
}

func TestHostBadIPv4FallsThroughToRegName(t *testing.T) {
	// Pinned decision: "256.1.1.1" is not IPv4, but digits and dots are
	// valid reg-name characters, so it is accepted as a name — the same
	// call the stdlib makes.
	assert.NoError(t, ValidateHost("256.1.1.1"))
	assert.NoError(t, ValidateHost("1.2.3"))
}

// --- IPv6 ---

func TestHostIPv6Bracketed(t *testing.T) {
	assert.NoError(t, ValidateHost("[::1]"))
	assert.NoError(t, ValidateHost("[::1]:8080"))
	assert.NoError(t, ValidateHost("[2001:db8::1]"))
	assert.NoError(t, ValidateHost("[2001:0db8:0000:0000:0000:0000:0000:0001]"))
}

func TestHostIPv6IPv4Mapped(t *testing.T) {
	assert.NoError(t, ValidateHost("[::ffff:127.0.0.1]"))
}

func TestHostIPv6Unbracketed(t *testing.T) {
	// A bare IPv6 literal is ambiguous with the port separator — this is
	// WHY the brackets exist.
	require.ErrorIs(t, ValidateHost("::1"), ErrInvalidHost)
	require.ErrorIs(t, ValidateHost("2001:db8::1"), ErrInvalidHost)
}

func TestHostIPv6UnclosedBracket(t *testing.T) {
	require.ErrorIs(t, ValidateHost("[::1"), ErrInvalidHost)
}

func TestHostIPv6EmptyBrackets(t *testing.T) {
	require.ErrorIs(t, ValidateHost("[]"), ErrInvalidHost)
}

func TestHostBracketedNonIPv6(t *testing.T) {
	// Brackets are for IPv6 literals only.
	require.ErrorIs(t, ValidateHost("[example.com]"), ErrInvalidHost)
	require.ErrorIs(t, ValidateHost("[127.0.0.1]"), ErrInvalidHost)
}

func TestHostIPv6JunkAfterBracket(t *testing.T) {
	require.ErrorIs(t, ValidateHost("[::1]x"), ErrInvalidHost)
	require.ErrorIs(t, ValidateHost("[::1]:"), ErrInvalidPort)
}

func TestHostIPv6DoubleBracket(t *testing.T) {
	require.ErrorIs(t, ValidateHost("[[::1]]"), ErrInvalidHost)
}

func TestHostIPv6ZoneRejected(t *testing.T) {
	// Pinned decision: zone ids (RFC 6874) are not supported.
	require.ErrorIs(t, ValidateHost("[fe80::1%eth0]"), ErrInvalidHost)
}
