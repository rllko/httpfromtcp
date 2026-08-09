// Package response
package response

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"httpfromtcp/internal/headers"
)

type Response struct{}

type StatusCode int

const httpVersion = "HTTP/1.1 "

var (
	StatusOK                  StatusCode = 200
	StatusBadRequest          StatusCode = 400
	StatusInternalServerError StatusCode = 500
	StatusNotImplemented      StatusCode = 501
	StatusLengthRequired      StatusCode = 411
	StatusNotFound            StatusCode = 404
	StatusMethodNotAllowed    StatusCode = 405
)

type Writer struct {
	writer     io.Writer
	header     *headers.Headers
	sentHeader bool
}

func NewWriter(w io.Writer) *Writer {
	h := headers.NewHeaders()
	h.Set("Connection", "close")

	return &Writer{
		writer:     w,
		header:     h,
		sentHeader: false,
	}
}

func (w *Writer) HasSentHeader() bool {
	return w.sentHeader
}

func (w *Writer) Header() *headers.Headers {
	return w.header
}

func (w *Writer) WriteJSON(data map[string]any, code StatusCode) {
	err := w.WriteStatusLine(code)
	if err != nil {
		w.Error("something went wrong", code, "text/plain")
		return
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		w.Error("something went wrong", code, "text/plain")
		return
	}

	w.Header().Replace("content-type", "application/json")
	w.Header().Replace("content-length", fmt.Sprintf("%d", len(jsonData)))

	err = w.WriteHeaders()
	if err != nil {
		w.Error("something went wrong", code, "text/plain")
		return
	}

	_, err = w.WriteBody(jsonData)
	if err != nil {
		w.Error("something went wrong", code, "text/plain")
	}
}

func (w *Writer) Error(msg string, code StatusCode, contentType string) {
	_ = w.WriteStatusLine(code)

	if contentType != "" {
		w.Header().Replace("content-type", contentType)
	}

	w.Header().Replace("content-length", strconv.Itoa(len(msg)))

	_ = w.WriteHeaders()

	_, _ = w.WriteBody([]byte(msg))
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
	case StatusNotImplemented:
		statusLine = fmt.Append(statusLine, "501 Not Implemented")
	case StatusLengthRequired:
		statusLine = fmt.Append(statusLine, "411 Length Required")
	case StatusNotFound:
		statusLine = fmt.Append(statusLine, "404 Not Found")
	case StatusMethodNotAllowed:
		statusLine = fmt.Append(statusLine, "405 Method Not Allowed")
	default:
		return fmt.Errorf("unrecognized status code")
	}
	statusLine = fmt.Append(statusLine, "\r\n")

	_, err := w.writer.Write(statusLine)
	return err
}

func (w *Writer) WriteHeaders() error {
	var err error
	out := []byte{}
	w.header.ForEach(func(n string, v string) {
		if err != nil {
			return
		}
		out = fmt.Appendf(out, "%s:%s\r\n", n, v)
	})

	out = fmt.Append(out, "\r\n")

	_, err = w.writer.Write(out)
	w.sentHeader = true
	return err
}

func (w *Writer) WriteBody(body []byte) (int, error) {
	if !w.sentHeader {
		_ = w.WriteStatusLine(200)
		_ = w.WriteHeaders()
	}
	n, err := w.writer.Write(body)
	return n, err
}

func (w *Writer) WriteChunkedBody(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	_, err := fmt.Fprintf(w.writer, "%x\r\n", len(p))
	if err != nil {
		return 0, err
	}

	_, err = w.writer.Write(p)
	if err != nil {
		return 0, err
	}

	n, err := w.writer.Write([]byte("\r\n"))
	return n, err
}

// WriteChunkedBodyDone - RFC 9112 7.1
func (w *Writer) WriteChunkedBodyDone() (int, error) {
	n, err := w.writer.Write([]byte("0\r\n\r\n"))
	return n, err
}

func (w *Writer) WriteTrailers(h *headers.Headers) error {
	if _, err := w.writer.Write([]byte("0\r\n")); err != nil {
		return err
	}

	var err error
	out := []byte{}
	h.ForEach(func(n string, v string) {
		if err != nil {
			return
		}
		out = fmt.Appendf(out, "%s:%s\r\n", n, v)
	})

	out = fmt.Append(out, "\r\n")

	_, err = w.writer.Write(out)
	return err
}
