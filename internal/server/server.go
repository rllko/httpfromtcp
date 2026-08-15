// Package server
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
)

type HandlerFunc func(w *response.Writer, req *request.Request)

func (f HandlerFunc) ServeHTTP(w *response.Writer, r *request.Request) {
	f(w, r)
}

type Server struct {
	handler           Handler
	listener          net.Listener
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	ActiveConnections sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
}

type HandlerError struct {
	StatusCode response.StatusCode
	Message    string
}

type Handler interface {
	ServeHTTP(w *response.Writer, req *request.Request)
}

func (h *HandlerError) Write(w *response.Writer) {
	err := w.WriteStatusLine(h.StatusCode)
	if err != nil {
		log.Printf("error writing status line: %v\n", err)
		return
	}

	w.Header().Replace("content-length", strconv.Itoa(len(h.Message)))
	err = w.WriteHeaders()
	if err != nil {
		log.Printf("error writing headers: %v\n", err)
		return
	}

	n, err := w.WriteBody([]byte(h.Message))
	if err != nil || n != len(h.Message) {
		log.Printf("error writing body: %v\n", err)
		return
	}
}

// Close stops the server immediately: it stops accepting and cancels
// active requests. Unlike Shutdown it does not wait for them to finish.
func (s *Server) Close() error {
	s.cancel()
	return s.listener.Close()
}

func (s *Server) handle(conn net.Conn) {
	defer s.ActiveConnections.Done()
	defer conn.Close()

	s.mu.Lock()
	if s.conns == nil {
		s.conns = make(map[net.Conn]struct{})
	}
	s.conns[conn] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

	conn.SetReadDeadline(time.Now().Add(s.ReadTimeout))

	responseWriter := response.NewWriter(conn)

	r, err := request.RequestFromReader(conn)
	if err != nil {

		hErr := &HandlerError{
			StatusCode: response.StatusBadRequest,
			Message:    err.Error(),
		}

		// RFC 9110 §15.5.9
		if errors.Is(err, os.ErrDeadlineExceeded) {
			hErr.StatusCode = response.StatusRequestTimeout
			hErr.Message = "Request Timeout"
		}

		if err, ok := errors.AsType[*request.ErrRequest](err); ok {
			hErr.StatusCode = response.StatusCode(err.Status)
			hErr.Message = err.Error()
		}

		conn.SetWriteDeadline(time.Now().Add(s.WriteTimeout))
		hErr.Write(responseWriter)

		return
	}

	base := s.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	defer cancel()

	conn.SetWriteDeadline(time.Now().Add(s.WriteTimeout))
	s.handler.ServeHTTP(responseWriter, r.WithContext(ctx))
}

func (s *Server) runServer() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		s.ActiveConnections.Add(1)
		go s.handle(conn)
	}
}

func Serve(port uint16, handle Handler) (*Server, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		handler:      handle,
		listener:     listener,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
		conns:        make(map[net.Conn]struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}

	go server.runServer()

	return server, nil
}

// Shutdown stops accepting, cancels active requests, then waits for them
// to finish. If ctx expires first, the remaining connections are
// force-closed so blocked I/O can unblock and Shutdown returns.
func (s *Server) Shutdown(ctx context.Context) error {
	s.listener.Close()
	s.cancel()

	done := make(chan struct{})
	go func() {
		s.ActiveConnections.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		for conn := range s.conns {
			conn.Close()
		}
		s.mu.Unlock()
		return ctx.Err()
	}
}
