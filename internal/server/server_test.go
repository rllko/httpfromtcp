// Edge-case tests from EDGE_CASES.md §E6 — Server.
// All catalog bugs are fixed; these tests now pin the correct behavior.
//
// Not covered on purpose (add after the fixes): a panicking handler currently
// crashes the whole test binary (no recover in handle), and slow-loris /
// timeout tests need read deadlines first.
package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
)

// fakeConn is an in-memory io.ReadWriteCloser so handle() can be tested
// without a real socket.
type fakeConn struct {
	in     *strings.Reader
	out    bytes.Buffer
	closed bool
}

func newFakeConn(request string) *fakeConn {
	return &fakeConn{in: strings.NewReader(request)}
}

func (c *fakeConn) Read(p []byte) (int, error)  { return c.in.Read(p) }
func (c *fakeConn) Write(p []byte) (int, error) { return c.out.Write(p) }
func (c *fakeConn) Close() error                { c.closed = true; return nil }

func helloHandler(w response.Writer, req *request.Request) {
	_ = w.WriteStatusLine(response.StatusOK)
	h := response.GetDefaultHeaders(len("hello"))
	_ = w.WriteHeaders(h)
	_, _ = w.WriteBody([]byte("hello"))
}

func TestHandleWritesExactlyOneResponse(t *testing.T) {
	// The handler owns the entire response — handle() must not write a
	// second status line or header block after it.
	s := &Server{handler: helloHandler}
	conn := newFakeConn("GET / HTTP/1.1\r\nHost: x\r\n\r\n")
	s.handle(conn)

	out := conn.out.String()
	assert.Equal(t, 1, strings.Count(out, "HTTP/1.1 "),
		"response must contain exactly one status line, got:\n%q", out)
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 200 OK\r\n"))
	assert.True(t, strings.HasSuffix(out, "hello"),
		"body must be the last thing written, got:\n%q", out)
	assert.True(t, conn.closed, "connection must be closed after handling")
}

func TestHandleMalformedRequestGets400(t *testing.T) {
	s := &Server{handler: helloHandler}
	conn := newFakeConn("this is not http\r\n\r\n")
	s.handle(conn)

	out := conn.out.String()
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 400 Bad Request\r\n"),
		"malformed request must yield a 400, got:\n%q", out)
	assert.True(t, conn.closed)
}

func TestHandleEmptyConnection(t *testing.T) {
	// Client connects and immediately closes: no panic, connection closed.
	s := &Server{handler: helloHandler}
	conn := newFakeConn("")
	s.handle(conn)
	assert.True(t, conn.closed)
	assert.True(t, strings.HasPrefix(conn.out.String(), "HTTP/1.1 400"),
		"an empty request is malformed and must get a 400")
}

// freePort grabs an ephemeral port from the kernel and releases it so
// Serve can bind it. (Racy in theory, fine for tests.)
func freePort(t *testing.T) uint16 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return uint16(port)
}

// doRequest performs one raw HTTP exchange and returns everything the server
// sent before closing the connection.
func doRequest(t *testing.T, addr string) string {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	b, _ := io.ReadAll(conn)
	return string(b)
}

func TestServeRespondsOverTCP(t *testing.T) {
	port := freePort(t)
	srv, err := Serve(port, helloHandler)
	require.NoError(t, err)
	defer srv.Close()

	out := doRequest(t, fmt.Sprintf("127.0.0.1:%d", port))
	assert.Contains(t, out, "HTTP/1.1 200 OK")
	assert.Contains(t, out, "hello")
}

func TestCloseStopsAccepting(t *testing.T) {
	// Close() closes the listener, which unblocks Accept and stops the
	// server accepting new connections.
	port := freePort(t)
	srv, err := Serve(port, helloHandler)
	require.NoError(t, err)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	// Prove it's up first.
	out := doRequest(t, addr)
	require.Contains(t, out, "HTTP/1.1")

	require.NoError(t, srv.Close())
	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err == nil {
		conn.Close()
	}
	assert.Error(t, err, "dialing after Close() must fail — listener was never closed")
}

func TestConcurrentConnections(t *testing.T) {
	port := freePort(t)
	srv, err := Serve(port, helloHandler)
	require.NoError(t, err)
	defer srv.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	const clients = 5

	var wg sync.WaitGroup
	results := make(chan string, clients)
	for range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- doRequest(t, addr)
		}()
	}
	wg.Wait()
	close(results)

	got := 0
	for out := range results {
		assert.Contains(t, out, "HTTP/1.1", "every client must get a response")
		got++
	}
	assert.Equal(t, clients, got)
}
