// Edge-case tests from EDGE_CASES.md §E5 — Response writer.
// All catalog bugs are fixed; these tests now pin the correct behavior.
package response

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"httpfromtcp/internal/headers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestWriteStatusLine414(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	require.NoError(t, w.WriteStatusLine(StatusRequestURITooLong))
	assert.Equal(t, "HTTP/1.1 414 URI Too Long\r\n", buf.String())
}

func TestWriteStatusLine431(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	require.NoError(t, w.WriteStatusLine(StatusRequestFieldsTooLarge))
	assert.Equal(t, "HTTP/1.1 431 Request Header Fields Too Large\r\n", buf.String())
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

	w.Header().Delete("connection")
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Connection", "close")

	require.NoError(t, w.WriteHeaders())

	got := parseHeaderBlock(t, buf.String())
	assert.Equal(t, map[string]string{
		"content-type": "text/plain",
		"connection":   "close",
	}, got)
}

func TestWriteHeadersEmptySet(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.Header().Delete("connection")
	require.NoError(t, w.WriteHeaders())
	assert.Equal(t, "\r\n", buf.String(), "empty header set is just the terminating blank line")
}

func TestGetDefaultHeadersContentLength(t *testing.T) {
	h := headers.NewHeaders()
	h.Set("Content-length", "42")
	got, ok := h.Get("content-length")
	require.True(t, ok)
	assert.Equal(t, "42", got)
}

func TestWriteBody(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	assert.False(t, w.HasSentHeader())

	n, err := w.WriteBody([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "HTTP/1.1 200 OK\r\nconnection:close\r\n\r\nhello", buf.String())
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

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteJSON(map[string]any{"name": "x"}, StatusOK)

	out := buf.String()
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 200 OK\r\n"))
	assert.True(t, strings.HasSuffix(out, `{"name":"x"}`))
	assert.Contains(t, out, "content-type:application/json\r\n")
	assert.Contains(t, out, "content-length:12\r\n")
	assert.Contains(t, out, "connection:close\r\n")
}

func TestError(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.Error("boom", StatusBadRequest, "text/plain")

	out := buf.String()
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 400 Bad Request\r\n"))
	assert.Contains(t, out, "content-type:text/plain\r\n")
	assert.Contains(t, out, "content-length:4\r\n")
	assert.True(t, strings.HasSuffix(out, "boom"))
	assert.True(t, w.Response.IsSent())
	assert.True(t, w.Response.IsError())
}

func TestErrorAfterBodySentWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	_, err := w.WriteBody([]byte("hello"))
	require.NoError(t, err)

	w.Error("boom", StatusInternalServerError, "text/plain")

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "HTTP/1.1 "))
	assert.True(t, strings.HasSuffix(out, "hello"))
}

func TestWriteTrailers(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	h := headers.NewHeaders()
	h.Set("X-Content-SHA256", "abc")
	require.NoError(t, w.WriteTrailers(h))
	assert.Equal(t, "0\r\nx-content-sha256:abc\r\n\r\n", buf.String())
}

func TestHasSentHeader(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	assert.False(t, w.HasSentHeader())

	_, err := w.WriteBody([]byte("hello"))
	require.NoError(t, err)
	assert.True(t, w.HasSentHeader())
}

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriterErrorPropagation(t *testing.T) {
	w := NewWriter(errWriter{})

	assert.Error(t, w.WriteStatusLine(StatusOK), "WriteStatusLine")
	assert.Error(t, w.WriteHeaders(), "WriteHeaders")

	_, err := w.WriteBody([]byte("x"))
	assert.Error(t, err, "WriteBody")

	_, err = w.WriteChunkedBody([]byte("x"))
	assert.Error(t, err, "WriteChunkedBody")

	_, err = w.WriteChunkedBodyDone()
	assert.Error(t, err, "WriteChunkedBodyDone")

	h := headers.NewHeaders()
	w.Header().Set("Content-length", "0")
	assert.Error(t, w.WriteTrailers(h), "WriteTrailers")
}
