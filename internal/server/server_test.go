// Edge-case tests from EDGE_CASES.md §E6 — Server.
// All catalog bugs are fixed; these tests now pin the correct behavior.
//
// Not covered on purpose (add after the fixes): a panicking handler currently
// crashes the whole test binary (no recover in handle), and slow-loris /
// timeout tests need read deadlines first.
package server

import (
	"context"
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

func helloHandler(w *response.Writer, req *request.Request) {
	_ = w.WriteStatusLine(response.StatusOK)
	w.Header().Set("Content-length", "5")
	_ = w.WriteHeaders()
	_, _ = w.WriteBody([]byte("hello"))
}

func TestHandleWritesExactlyOneResponse(t *testing.T) {
	// The handler owns the entire response — handle() must not write a
	// second status line or header block after it.
	a, b := net.Pipe()

	s := &Server{handler: HandlerFunc(helloHandler), ReadTimeout: time.Second, WriteTimeout: time.Second}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))

	buff, _ := io.ReadAll(io.LimitReader(a, 1024))
	out := string(buff)

	s.ActiveConnections.Wait()
	assert.Equal(t, 1, strings.Count(out, "HTTP/1.1 "),
		"response must contain exactly one status line, got:\n%q", out)
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 200 OK\r\n"))
	assert.True(t, strings.HasSuffix(out, "hello"),
		"body must be the last thing written, got:\n%q", out)
}

func TestHandleMalformedRequestGets400(t *testing.T) {
	a, b := net.Pipe()
	s := &Server{handler: HandlerFunc(helloHandler), ReadTimeout: time.Second, WriteTimeout: time.Second}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte("this is not http\r\n\r\n"))

	buff, _ := io.ReadAll(io.Reader(a))
	out := string(buff)

	s.ActiveConnections.Wait()

	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 400 Bad Request\r\n"),
		"malformed request must yield a 400, got:\n%q", out)
}

func TestHandleEmptyConnection(t *testing.T) {
	// Client connects and immediately closes: no panic, connection closed.
	a, b := net.Pipe()
	s := &Server{handler: HandlerFunc(helloHandler), ReadTimeout: time.Second, WriteTimeout: time.Second}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte(""))

	buff, _ := io.ReadAll(io.Reader(a))
	out := string(buff)

	s.ActiveConnections.Wait()
	t.Log(out)
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 408"),
		"an empty request is malformed and must get a 400")
}

func TestHandleErrRequestMapsStatus(t *testing.T) {
	a, b := net.Pipe()
	s := &Server{handler: HandlerFunc(helloHandler), ReadTimeout: time.Second, WriteTimeout: time.Second}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte("POST / HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: gzip\r\n\r\n"))

	buff, _ := io.ReadAll(io.Reader(a))
	out := string(buff)

	s.ActiveConnections.Wait()
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 501 Not Implemented\r\n"),
		"an ErrRequest must map to its own status code, got:\n%q", out)
}

func TestHandlePassesParsedRequestToHandler(t *testing.T) {
	var method, path, host string
	var ok bool
	h := func(w *response.Writer, req *request.Request) {
		method = req.RequestLine.Method
		path = req.URL.Path
		host, ok = req.Headers.Get("host")
	}
	a, b := net.Pipe()
	s := &Server{handler: HandlerFunc(h), ReadTimeout: time.Second, WriteTimeout: time.Second}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte("GET /a?b=c HTTP/1.1\r\nHost: example.com\r\n\r\n"))

	s.ActiveConnections.Wait()
	assert.Equal(t, "GET", method)
	assert.Equal(t, "/a", path)
	assert.True(t, ok)
	assert.Equal(t, "example.com", host)
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
	srv, err := Serve(port, HandlerFunc(helloHandler))
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
	srv, err := Serve(port, HandlerFunc(helloHandler))
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
	srv, err := Serve(port, HandlerFunc(helloHandler))
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

func TestHandlerObservesCancellation(t *testing.T) {
	a, b := net.Pipe()
	baseCtx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	s := &Server{
		handler: HandlerFunc(func(w *response.Writer, req *request.Request) {
			close(started)
			<-req.Context().Done()
		}),
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		ctx:          baseCtx,
		cancel:       cancel,
	}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()

	drained := make(chan struct{})
	go func() {
		s.ActiveConnections.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not observe cancellation")
	}
}

func TestShutdownReturnsWhenHandlerIgnoresCancellation(t *testing.T) {
	a, b := net.Pipe()
	baseCtx, cancel := context.WithCancel(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	started := make(chan struct{})
	s := &Server{
		handler: HandlerFunc(func(w *response.Writer, req *request.Request) {
			close(started)
			time.Sleep(300 * time.Millisecond) // ignores cancellation on purpose
		}),
		listener:     listener,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		ctx:          baseCtx,
		cancel:       cancel,
	}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	ctx, cancelShutdown := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShutdown()

	begin := time.Now()
	err = s.Shutdown(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(begin), 300*time.Millisecond,
		"Shutdown must return at its deadline, not wait for the handler")

	drained := make(chan struct{})
	go func() {
		s.ActiveConnections.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("connection goroutine leaked after forced shutdown")
	}
}

func TestShutdownForceClosesStuckConnection(t *testing.T) {
	a, b := net.Pipe()
	baseCtx, cancel := context.WithCancel(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &Server{
		handler:      HandlerFunc(helloHandler),
		listener:     listener,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Hour, // the deadline must NOT be what saves us here
		ctx:          baseCtx,
		cancel:       cancel,
	}
	s.ActiveConnections.Add(1)
	go s.handle(b)
	a.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))

	ctx, cancelShutdown := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShutdown()
	assert.ErrorIs(t, s.Shutdown(ctx), context.DeadlineExceeded)

	drained := make(chan struct{})
	go func() {
		s.ActiveConnections.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("handler stuck on the write was not released by the force-close")
	}
}
