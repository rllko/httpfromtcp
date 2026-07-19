// Package server
package server

import (
	"bytes"
	"fmt"
	"io"
	"net"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
)

type Handler func(w response.Writer, req *request.Request)

type Server struct {
	handler Handler
	closed  bool
}

type HandlerError struct {
	StatusCode response.StatusCode
	Message    string
}

func (h *HandlerError) Write(w response.Writer) {
	_ = w.WriteStatusLine(h.StatusCode)

	headers := response.GetDefaultHeaders(len(h.Message))
	w.WriteHeaders(headers)

	_, _ = w.WriteBody([]byte(h.Message))
}

func (s *Server) Close() error {
	s.closed = true
	return nil
}

func (s *Server) handle(conn io.ReadWriteCloser) {
	defer conn.Close()
	responseWriter := response.NewWriter(conn)

	r, err := request.RequestFromReader(conn)
	if err != nil {
		hErr := &HandlerError{
			StatusCode: response.StatusBadRequest,
			Message:    err.Error(),
		}
		hErr.Write(*responseWriter)

		return
	}

	buf := bytes.NewBuffer([]byte{})
	s.handler(*responseWriter, r)

	b := buf.Bytes()
	responseWriter.WriteStatusLine(response.StatusOK)
	headers := response.GetDefaultHeaders(len(b))
	responseWriter.WriteHeaders(headers)
	conn.Write(b)
}

func (s *Server) runServer(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if s.closed {
			return
		}

		if err != nil {
			return
		}

		go s.handle(conn)
	}
}

func Serve(port uint16, handle Handler) (*Server, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	server := &Server{
		closed:  false,
		handler: handle,
	}

	go server.runServer(listener)
	return server, nil
}
