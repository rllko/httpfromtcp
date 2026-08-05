// Package server
package server

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
)

type Handler func(w response.Writer, req *request.Request)

type Server struct {
	handler  Handler
	listener net.Listener
}

type HandlerError struct {
	StatusCode response.StatusCode
	Message    string
}

func (h *HandlerError) Write(w response.Writer) {
	err := w.WriteStatusLine(h.StatusCode)
	if err != nil {
		log.Fatalf("error writing status line: %v", err)
		return
	}

	headers := response.GetDefaultHeaders(len(h.Message))
	err = w.WriteHeaders(headers)
	if err != nil {
		log.Fatalf("error writing headers: %v", err)
		return
	}

	n, err := w.WriteBody([]byte(h.Message))
	if err != nil || n != len(h.Message) {
		log.Fatalf("error writing body: %v", err)
		return
	}
}

func (s *Server) Close() error {
	return s.listener.Close()
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

		if err, ok := errors.AsType[*request.ErrRequest](err); ok {
			hErr.StatusCode = response.StatusCode(err.Status)
			hErr.Message = err.Error()
		}

		hErr.Write(*responseWriter)

		return
	}

	s.handler(*responseWriter, r)
}

func (s *Server) runServer() {
	for {
		conn, err := s.listener.Accept()
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
		handler:  handle,
		listener: listener,
	}

	go server.runServer()
	return server, nil
}
