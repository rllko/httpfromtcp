// Edge-case tests from EDGE_CASES.md §E5 — Response writer.
// All catalog bugs are fixed; these tests now pin the correct behavior.
package response

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"httpfromtcp/internal/headers"
)

func TestWriteStatusLine200(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	require.NoError(t, w.WriteStatusLine(StatusOK))
	assert.Equal(t, "HTTP/1.1 200 OK\r\n", buf.String())
}

func TestWriteStatusLine400(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	require.NoError(t, w.WriteStatusLine(StatusBadRequest))
	assert.Equal(t, "HTTP/1.1 400 Bad Request\r\n", buf.String())
}

func TestWriteStatusLine500(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	require.NoError(t, w.WriteStatusLine(StatusInternalServerError))
	assert.Equal(t, "HTTP/1.1 500 Internal Server Error\r\n", buf.String())
}

func TestWriteStatusLineUnknownCode(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	err := w.WriteStatusLine(StatusCode(418))
	require.Error(t, err)
	assert.Empty(t, buf.String(), "nothing must be written on error")
}

// parseHeaderBlock reads "name:value\r\n" lines (format-agnostic about the
// space after the colon) up to the terminating blank line.
func parseHeaderBlock(t *testing.T, out string) map[string]string {
	t.Helper()
	require.True(t, strings.HasSuffix(out, "\r\n\r\n"), "header block must end with a blank line, got %q", out)
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\r\n\r\n"), "\r\n") {
		name, value, ok := strings.Cut(line, ":")
		require.True(t, ok, "header line without colon: %q", line)
		m[strings.ToLower(name)] = strings.TrimSpace(value)
	}
	return m
}

func TestWriteHeaders(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	h := headers.NewHeaders()
	h.Set("Content-Type", "text/plain")
	h.Set("Connection", "close")
	require.NoError(t, w.WriteHeaders(h))

	got := parseHeaderBlock(t, buf.String())
	assert.Equal(t, map[string]string{
		"content-type": "text/plain",
		"connection":   "close",
	}, got)
}

func TestWriteHeadersEmptySet(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	require.NoError(t, w.WriteHeaders(headers.NewHeaders()))
	assert.Equal(t, "\r\n", buf.String(), "empty header set is just the terminating blank line")
}

func TestGetDefaultHeadersContentLength(t *testing.T) {
	h := GetDefaultHeaders(42)
	got, ok := h.Get("content-length")
	require.True(t, ok)
	assert.Equal(t, "42", got)
}

func TestWriteBody(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	n, err := w.WriteBody([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", buf.String())
}

func TestWriteChunkedBody(t *testing.T) {
	// 255 = 0xff: lowercase hex size line, then data, then CRLF.
	payload := strings.Repeat("x", 255)
	var buf bytes.Buffer
	w := NewWriter(&buf)
	_, err := w.WriteChunkedBody([]byte(payload))
	require.NoError(t, err)
	assert.Equal(t, "ff\r\n"+payload+"\r\n", buf.String())
}

func TestWriteChunkedBodySmall(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	_, err := w.WriteChunkedBody([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, "5\r\nhello\r\n", buf.String())
}

func TestWriteChunkedBodyEmptySlice(t *testing.T) {
	// A zero-length chunk is a no-op: the "0 CRLF CRLF" sequence is the
	// stream TERMINATOR and only Done/Trailers may emit it.
	var buf bytes.Buffer
	w := NewWriter(&buf)
	_, _ = w.WriteChunkedBody([]byte{})
	assert.Empty(t, buf.String(), "zero-length chunk must not emit the stream terminator")
}

func TestWriteChunkedBodyDone(t *testing.T) {
	// Without trailers the stream ends with the full "0 CRLF CRLF"
	// terminator (RFC 9112 §7.1).
	var buf bytes.Buffer
	w := NewWriter(&buf)
	_, err := w.WriteChunkedBodyDone()
	require.NoError(t, err)
	assert.Equal(t, "0\r\n\r\n", buf.String())
}

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriterErrorPropagation(t *testing.T) {
	w := NewWriter(errWriter{})

	assert.Error(t, w.WriteStatusLine(StatusOK), "WriteStatusLine")
	assert.Error(t, w.WriteHeaders(GetDefaultHeaders(0)), "WriteHeaders")

	_, err := w.WriteBody([]byte("x"))
	assert.Error(t, err, "WriteBody")

	_, err = w.WriteChunkedBody([]byte("x"))
	assert.Error(t, err, "WriteChunkedBody")

	_, err = w.WriteChunkedBodyDone()
	assert.Error(t, err, "WriteChunkedBodyDone")

	assert.Error(t, w.WriteTrailers(GetDefaultHeaders(0)), "WriteTrailers")
}
