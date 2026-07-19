// Package response
package response

import (
	"fmt"
	"io"

	"httpfromtcp/internal/headers"
)

type Response struct{}

type StatusCode int

const httpVersion = "HTTP/1.1 "

var (
	StatusOK                  StatusCode = 200
	StatusBadRequest          StatusCode = 400
	StatusInternalServerError StatusCode = 500
)

type Writer struct {
	writer io.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{
		writer: w,
	}
}

func (w *Writer) WriteStatusLine(status StatusCode) error {
	statusLine := []byte(httpVersion)
	switch status {
	case StatusOK:
		statusLine = fmt.Append(statusLine, "200 OK")
	case StatusBadRequest:
		statusLine = fmt.Append(statusLine, "400 Bad Request")
	case StatusInternalServerError:
		statusLine = fmt.Append(statusLine, "500 Internal Server Error")
	default:
		return fmt.Errorf("unrecognized status code")
	}
	statusLine = fmt.Append(statusLine, "\r\n")

	_, err := w.writer.Write(statusLine)
	return err
}

func GetDefaultHeaders(contentLen int) *headers.Headers {
	h := headers.NewHeaders()
	h.Set("Content-type", "text/plain")
	h.Set("Connection", "close")
	h.Set("Content-length", fmt.Sprintf("%d", contentLen))
	return h
}

func (w *Writer) WriteHeaders(headers *headers.Headers) error {
	var err error
	out := []byte{}
	headers.ForEach(func(n string, v string) {
		if err != nil {
			return
		}
		out = fmt.Appendf(out, "%s:%s\r\n", n, v)
	})

	out = fmt.Append(out, "\r\n")

	_, err = w.writer.Write(out)
	return err
}

func (w *Writer) WriteBody(body []byte) (int, error) {
	n, err := w.writer.Write(body)
	return n, err
}

func (w *Writer) WriteChunkedBody(p []byte) (int, error) {
	_, err := fmt.Fprintf(w.writer, "%x\r\n", len(p))
	if err != nil {
		return 0, err
	}

	_, err = w.WriteBody(p)
	if err != nil {
		return 0, err
	}

	n, err := w.WriteBody([]byte("\r\n"))
	return n, err
}

func (w *Writer) WriteChunkedBodyDone() (int, error) {
	n, err := w.writer.Write([]byte("0\r\n"))
	return n, err
}

func (w *Writer) WriteTrailers(h *headers.Headers) error {
	err := w.WriteHeaders(h)
	return err
}
