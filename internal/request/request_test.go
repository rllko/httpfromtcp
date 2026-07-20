package request

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type chunkReader struct {
	data            string
	numBytesPerRead int
	pos             int
}

// Read reads up to len(p) or numBytesPerRead bytes from the string per call
// its useful for simulating reading a variable number of bytes per chunk from a network connection
func (cr *chunkReader) Read(p []byte) (n int, err error) {
	if cr.pos >= len(cr.data) {
		return 0, io.EOF
	}
	endIndex := min(cr.pos+cr.numBytesPerRead, len(cr.data))
	n = copy(p, cr.data[cr.pos:endIndex])
	cr.pos += n

	return n, nil
}

func TestRequestLineParse(t *testing.T) {
	// Test: Good GET Request line
	reader := &chunkReader{
		data:            "GET / HTTP/1.1\r\nHost: localhost:42069\r\nUser-Agent: curl/7.81.0\r\nAccept: */*\r\n\r\n",
		numBytesPerRead: 3,
	}
	r, err := RequestFromReader(reader)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "GET", r.RequestLine.Method)
	assert.Equal(t, "/", r.RequestLine.RequestTarget)
	assert.Equal(t, "1.1", r.RequestLine.HTTPVersion)

	// Test: Good GET Request line with path
	reader = &chunkReader{
		data:            "GET /coffee HTTP/1.1\r\nHost: localhost:42069\r\nUser-Agent: curl/7.81.0\r\nAccept: */*\r\n\r\n",
		numBytesPerRead: 1,
	}
	r, err = RequestFromReader(reader)
	require.NoError(t, err)
	require.NotNil(t, r)

	assert.Equal(t, "GET", r.RequestLine.Method)
	assert.Equal(t, "/coffee", r.RequestLine.RequestTarget)
	assert.Equal(t, "1.1", r.RequestLine.HTTPVersion)
	// Test: Invalid number of parts in request line
	_, err = RequestFromReader(strings.NewReader("/coffee HTTP/1.1\r\nHost: localhost:42069\r\nUser-Agent: curl/7.81.0\r\nAccept: */*\r\n\r\n"))
	require.Error(t, err)
}

func TestParseFunction(t *testing.T) {
	// Test: Standard Headers
	reader := &chunkReader{
		data:            "GET /a?b=3 HTTP/1.1\r\nHost: localhost:42069\r\nUser-Agent: curl/7.81.0\r\nAccept: */*\r\n\r\n",
		numBytesPerRead: 3,
	}
	r, err := RequestFromReader(reader)
	require.NoError(t, err)
	require.NotNil(t, r)
	hostStr, _ := r.Headers.Get("host")
	userAgentStr, _ := r.Headers.Get("user-agent")
	acceptStr, _ := r.Headers.Get("accept")
	assert.Equal(t, "localhost:42069", hostStr)
	assert.Equal(t, "curl/7.81.0", userAgentStr)
	assert.Equal(t, "*/*", acceptStr)
	assert.Equal(t, r.URL.Path, "/a")
	assert.Equal(t, r.URL.RawQuery, "b=3")

	// Test: Malformed Header
	reader = &chunkReader{
		data:            "GET / HTTP/1.1\r\nHost localhost:42069\r\n\r\n",
		numBytesPerRead: 3,
	}
	_, err = RequestFromReader(reader)
	require.Error(t, err)
}

func TestParseBody(t *testing.T) {
	// Test: Standard Body
	reader := &chunkReader{
		data: "POST /submit HTTP/1.1\r\n" +
			"Host: localhost:42069\r\n" +
			"Content-Length: 13\r\n" +
			"\r\n" +
			"hello world!\n",
		numBytesPerRead: 3,
	}
	r, err := RequestFromReader(reader)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "hello world!\n", string(r.Body))

	// Test: Body shorter than reported content length
	reader = &chunkReader{
		data: "POST /submit HTTP/1.1\r\n" +
			"Host: localhost:42069\r\n" +
			"Content-Length: 20\r\n" +
			"\r\n" +
			"partial content",
		numBytesPerRead: 3,
	}
	_, err = RequestFromReader(reader)
	require.Error(t, err)
}

// Edge-case tests from EDGE_CASES.md §E1 (request line), §E3 (body/Content-Length)
// and §E4 (RequestFromReader loop).
// All catalog bugs are fixed; these tests now pin the correct behavior.

// parseWithTimeout guards against the parser hanging (see §E4 fixed-buffer bug):
// a hang becomes a test failure instead of a stuck `go test`.
func parseWithTimeout(t *testing.T, r io.Reader) (*Request, error) {
	t.Helper()
	type result struct {
		req *Request
		err error
	}
	ch := make(chan result, 1)
	go func() {
		req, err := RequestFromReader(r)
		ch <- result{req, err}
	}()
	select {
	case res := <-ch:
		return res.req, res.err
	case <-time.After(2 * time.Second):
		t.Fatal("RequestFromReader hung — likely the fixed 1024-byte buffer bug (EDGE_CASES.md §E4)")
		return nil, nil
	}
}

func TestRequestTargetWithQueryString(t *testing.T) {
	// Query string is kept verbatim on the target (no decoding until M1).
	reader := &chunkReader{
		data:            "GET /a?b=c&d=%20 HTTP/1.1\r\nHost: x\r\n\r\n",
		numBytesPerRead: 3,
	}
	r, err := parseWithTimeout(t, reader)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "GET", r.RequestLine.Method)
	assert.Equal(t, "/a?b=c&d=%20", r.RequestLine.RequestTarget)
	assert.Equal(t, "1.1", r.RequestLine.HTTPVersion)
}

func TestRequestLineAsteriskForm(t *testing.T) {
	// A method is any RFC 9110 token — asterisk-form OPTIONS included.
	reader := &chunkReader{
		data:            "OPTIONS * HTTP/1.1\r\nHost: x\r\n\r\n",
		numBytesPerRead: 1,
	}
	r, err := parseWithTimeout(t, reader)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "OPTIONS", r.RequestLine.Method)
	assert.Equal(t, "*", r.RequestLine.RequestTarget)
}

func TestRequestLineCustomTokenMethod(t *testing.T) {
	// Any RFC 9110 token is a valid method — no whitelist.
	reader := &chunkReader{
		data:            "PURGE /cache HTTP/1.1\r\nHost: x\r\n\r\n",
		numBytesPerRead: 3,
	}
	r, err := parseWithTimeout(t, reader)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "PURGE", r.RequestLine.Method)
	assert.Equal(t, "/cache", r.RequestLine.RequestTarget)
}

func TestRequestLineDeepPath(t *testing.T) {
	reader := &chunkReader{
		data:            "GET /a/b/c/d/e/f/g HTTP/1.1\r\nHost: x\r\n\r\n",
		numBytesPerRead: 1,
	}
	r, err := parseWithTimeout(t, reader)
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "/a/b/c/d/e/f/g", r.RequestLine.RequestTarget)
}

func TestRequestLineEmptyFirstLine(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("\r\nGET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineTwoParts(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineFourParts(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / extra HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineDoubleSpace(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET  / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineLeadingSpace(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader(" GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineVersion10(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.0\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineVersion2(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/2\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineLowercaseScheme(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / http/1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineVersion111(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineMissingSlashInVersion(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineNonTokenMethod(t *testing.T) {
	// '@' is not a token char, so "G@T" can never be a valid method.
	_, err := parseWithTimeout(t, strings.NewReader("G@T / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.Error(t, err)
}

func TestRequestLineNeverArrives(t *testing.T) {
	// Only bare \n line endings, so the \r\n separator never appears and the
	// stream ends: must error, not hang.
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.1\nHost: x\n\n"))
	require.Error(t, err)
}

func TestContentLengthZero(t *testing.T) {
	r, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\n\r\n"))
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "", r.Body)
}

func TestContentLengthExact(t *testing.T) {
	r, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhello"))
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "hello", r.Body)
}

func TestContentLengthExtraBytesNotInBody(t *testing.T) {
	r, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhelloEXTRA"))
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "hello", r.Body)
}

func TestContentLengthNonNumeric(t *testing.T) {
	// A non-numeric Content-Length is a 400-class error, never a silent
	// "no body" — that silent fallback is a request-smuggling vector.
	_, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: abc\r\n\r\nhello"))
	require.Error(t, err)
}

func TestContentLengthTrailingJunk(t *testing.T) {
	// Trailing junk after the digits is rejected.
	_, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 5x\r\n\r\nhello"))
	require.Error(t, err)
}

func TestContentLengthNegative(t *testing.T) {
	// Negative values are rejected: the grammar is 1*DIGIT.
	_, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: -1\r\n\r\nhello"))
	require.Error(t, err)
}

func TestContentLengthPlusPrefixed(t *testing.T) {
	// Atoi would accept a leading '+' but RFC 9110 is digit-only.
	_, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: +5\r\n\r\nhello"))
	require.Error(t, err)
}

func TestContentLengthOverflow(t *testing.T) {
	// Overflow is an error, never a wrap or silent zero.
	_, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 18446744073709551616\r\n\r\nhello"))
	require.Error(t, err)
}

func TestContentLengthDuplicateIdentical(t *testing.T) {
	// Duplicate identical Content-Length headers (merged to "5, 5" by the
	// header map) collapse to one value per RFC 9110 §8.6.
	r, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\nContent-Length: 5\r\n\r\nhello"))
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "hello", r.Body)
}

func TestContentLengthConflicting(t *testing.T) {
	// Conflicting values MUST be rejected (RFC 9112 §6.3).
	_, err := parseWithTimeout(t, strings.NewReader("POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\nContent-Length: 6\r\n\r\nhelloX"))
	require.Error(t, err)
}

func TestBodySplitAcrossReads(t *testing.T) {
	// Test: 1 byte per read
	reader := &chunkReader{
		data:            "POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 13\r\n\r\nhello world!\n",
		numBytesPerRead: 1,
	}
	r, err := parseWithTimeout(t, reader)
	require.NoError(t, err)
	assert.Equal(t, "hello world!\n", r.Body)

	// Test: 5 bytes per read (splits the \r\n separators)
	reader = &chunkReader{
		data:            "POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 13\r\n\r\nhello world!\n",
		numBytesPerRead: 5,
	}
	r, err = parseWithTimeout(t, reader)
	require.NoError(t, err)
	assert.Equal(t, "hello world!\n", r.Body)
}

func TestEOFMidRequestLine(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / HT"))
	require.Error(t, err)
}

func TestEOFMidHeaders(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.1\r\nHost: x\r\n"))
	require.Error(t, err)
}

func TestEOFMidBody(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 10\r\n\r\nhal"))
	require.Error(t, err)
}

func TestReaderReturningDataWithEOF(t *testing.T) {
	// io.Reader may return (n > 0, io.EOF) in one call; the final bytes
	// must be parsed before the error is judged.
	data := "GET / HTTP/1.1\r\nHost: x\r\n\r\n"
	r, err := parseWithTimeout(t, iotest.DataErrReader(strings.NewReader(data)))
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "GET", r.RequestLine.Method)
}

func TestOneByteReader(t *testing.T) {
	data := "POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhello"
	r, err := parseWithTimeout(t, iotest.OneByteReader(strings.NewReader(data)))
	require.NoError(t, err)
	assert.Equal(t, "hello", r.Body)
}

func TestHalfReader(t *testing.T) {
	data := "POST /submit HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhello"
	r, err := parseWithTimeout(t, iotest.HalfReader(strings.NewReader(data)))
	require.NoError(t, err)
	assert.Equal(t, "hello", r.Body)
}

func TestRequestLargerThanInternalBuffer(t *testing.T) {
	// The read buffer grows when full; requests larger than the initial
	// 1024 bytes parse fine. parseWithTimeout stays as a hang tripwire.
	big := strings.Repeat("a", 2000)
	data := "GET / HTTP/1.1\r\nHost: x\r\nX-Big: " + big + "\r\n\r\n"
	r, err := parseWithTimeout(t, strings.NewReader(data))
	require.NoError(t, err)
	require.NotNil(t, r)
	got, ok := r.Headers.Get("x-big")
	require.True(t, ok)
	assert.Equal(t, big, got)
}

func TestChunkedTransferEncoding(t *testing.T) {
	// Until the M5 decoder lands, Transfer-Encoding is refused with an
	// error (501-shape); after M5 the decoded body is the expectation.
	// Silent body drop is never acceptable.
	data := "POST /upload HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n0\r\n\r\n"
	r, err := parseWithTimeout(t, strings.NewReader(data))
	if err == nil {
		require.NotNil(t, r)
		assert.Equal(t, "hello", r.Body, "chunked body silently dropped")
	}
}

func TestParseAfterErrorStateFails(t *testing.T) {
	r := NewRequest()
	_, err := r.parse([]byte("BAD\r\n"))
	require.Error(t, err)

	// Once in the error state, further parse calls must refuse to run.
	_, err = r.parse([]byte("GET / HTTP/1.1\r\n"))
	require.ErrorIs(t, err, ErrorRequestInErrorState)
}

func TestParseEmptyInputIsNoop(t *testing.T) {
	r := NewRequest()
	n, err := r.parse([]byte{})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// --- M4: Host header enforcement (RFC 9112 §3.2) ---

func TestMissingHostHeader(t *testing.T) {
	// HTTP/1.1 requires a Host header; a request without one is a 400.
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.1\r\nAccept: */*\r\n\r\n"))
	require.ErrorIs(t, err, ErrorMissingHost)
}

func TestDuplicateHostHeader(t *testing.T) {
	// Two Host headers are a 400 even when identical. The header map merges
	// them with ", " and a valid host can never contain a comma.
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.1\r\nHost: a.com\r\nHost: b.com\r\n\r\n"))
	require.ErrorIs(t, err, ErrorDuplicateHost)
}

func TestInvalidHostHeader(t *testing.T) {
	_, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.1\r\nHost: ex ample.com\r\n\r\n"))
	require.Error(t, err)
}

func TestValidIPv6HostHeader(t *testing.T) {
	r, err := parseWithTimeout(t, strings.NewReader("GET / HTTP/1.1\r\nHost: [::1]:8080\r\n\r\n"))
	require.NoError(t, err)
	require.NotNil(t, r)
}
