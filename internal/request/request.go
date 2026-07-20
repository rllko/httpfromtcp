// Package request
package request

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"httpfromtcp/internal/headers"
	"httpfromtcp/internal/url"
)

const (
	INITIALIZED = iota
	DONE
)

type RequestLine struct {
	HTTPVersion   string
	RequestTarget string
	Method        string
}

type parserState string

type Request struct {
	RequestLine RequestLine
	state       parserState
	Headers     *headers.Headers
	Body        string
	URL         *url.URL

	// bodyLength is the validated Content-Length, computed once when the
	// headers complete. Only meaningful in StateBody.
	bodyLength int
}

var (
	ErrorMalforedRequestLine    = errors.New("bad Request Line")
	ErrorMalforedRequestTarget  = errors.New("bad Request RequestTarget")
	ErrorUnsupportedHTTPVersion = errors.New("unsupported HTTP version")
	ErrorNoSeparatorFound       = errors.New("no Separator Found")
	ErrorInvalidHeaders         = errors.New("invalid headers")
	ErrorRequestInErrorState    = errors.New("request in error state")
	ErrorMissingHost            = errors.New("missing Host header")
	ErrorDuplicateHost          = errors.New("duplicate Host header")
	ErrorInvalidContentLength   = errors.New("invalid Content-Length")
	// ErrorTransferEncodingNotImplemented: until the M5 chunked decoder
	// lands, a Transfer-Encoding body must be REFUSED (501), never
	// silently dropped — dropping it is a smuggling-shaped data loss.
	ErrorTransferEncodingNotImplemented = errors.New("transfer-encoding not implemented")
	SEPARATOR                   = []byte("\r\n")
)

const (
	StateInit    parserState = "init"
	StateDone    parserState = "done"
	StateError   parserState = "error"
	StateHeaders             = "headers"
	StateBody                = "body"
)

func NewRequest() *Request {
	return &Request{
		state:   StateInit,
		Headers: headers.NewHeaders(),
		Body:    "",
		URL:     &url.URL{},
	}
}

// RFC 9112 §6.3
func (r *Request) contentLength() (int, error) {
	value, exists := r.Headers.Get("content-length")
	if !exists {
		return 0, nil
	}

	parts := strings.Split(value, ",")
	first := strings.TrimSpace(parts[0])
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) != first {
			return 0, ErrorInvalidContentLength
		}
	}

	if first == "" {
		return 0, ErrorInvalidContentLength
	}
	for i := 0; i < len(first); i++ {
		if first[i] < '0' || first[i] > '9' {
			return 0, ErrorInvalidContentLength
		}
	}

	n, err := strconv.Atoi(first)
	if err != nil {
		// Digits-only input can only fail on overflow.
		return 0, ErrorInvalidContentLength
	}

	return n, nil
}

// RFC 9112 §3.2
func (r *Request) validateHost() error {
	host, ok := r.Headers.Get("host")
	if !ok {
		return ErrorMissingHost
	}

	if strings.Contains(host, ",") {
		return ErrorDuplicateHost
	}

	return url.ValidateHost(host)
}

func (r *Request) parse(data []byte) (int, error) {
	read := 0
outer:
	for {
		currentData := data[read:]
		if len(currentData) == 0 {
			break outer
		}

		switch r.state {
		case StateError:
			return 0, ErrorRequestInErrorState
		case StateInit:
			rl, n, err := parseRequestLine(currentData)
			if err != nil {
				r.state = StateError
				return 0, err
			}

			if n == 0 {
				break outer
			}

			u, err := url.Parse([]byte(rl.RequestTarget))
			if err != nil {
				r.state = StateError
				return 0, err
			}
			r.URL = u

			r.RequestLine = *rl
			read += n
			r.state = StateHeaders
		case StateHeaders:
			n, done, err := r.Headers.Parse(currentData)
			if err != nil {
				r.state = StateError
				return 0, err
			}

			if n == 0 {
				break outer
			}

			read += n
			// todo: change this
			// in the real world you dont get EOF, you would just transition to body
			if done {
				if err := r.validateHost(); err != nil {
					r.state = StateError
					return 0, err
				}

				// M5 pending: refuse chunked (and any other coding)
				// instead of silently ignoring the body.
				if _, hasTE := r.Headers.Get("transfer-encoding"); hasTE {
					r.state = StateError
					return 0, ErrorTransferEncodingNotImplemented
				}

				length, err := r.contentLength()
				if err != nil {
					r.state = StateError
					return 0, err
				}
				r.bodyLength = length

				// chunked decoding (M5) not built yet: a chunked body
				// currently has no Content-Length and falls through to Done.
				if length > 0 {
					r.state = StateBody
				} else {
					r.state = StateDone
				}
			}
		case StateBody:
			// bodyLength was validated at the headers boundary and is > 0
			// here, otherwise we would have gone straight to StateDone.
			remaining := min(r.bodyLength-len(r.Body), len(currentData))
			r.Body = fmt.Sprintf("%s%s", r.Body, currentData[:remaining])
			read += remaining

			if len(r.Body) == r.bodyLength {
				r.state = StateDone
			}
		case StateDone:
			break outer

		default:
			panic("somehow i'm here")
		}
	}
	return read, nil
}

func parseRequestLine(b []byte) (*RequestLine, int, error) {
	idx := bytes.Index(b, SEPARATOR)
	if idx == -1 {
		return nil, 0, nil
	}

	startLine := b[:idx]
	read := idx + len(SEPARATOR)
	method, rest, ok := bytes.Cut(startLine, []byte(" "))

	// RFC 9110 §9: a method is any token — no whitelist. The whole field
	// must be one token, so "G@T" (partial token) fails on the length check.
	token, n, tokenErr := headers.ParseToken(string(method))
	if !ok || tokenErr != nil || n != len(method) || token == "" {
		return nil, 0, ErrorMalforedRequestLine
	}

	requestTarget, HTTPVersion, ok := bytes.Cut(rest, []byte(" "))
	if !ok {
		return nil, 0, ErrorMalforedRequestLine
	}

	protocol, version, ok := bytes.Cut(HTTPVersion, []byte("/"))
	if !ok || string(protocol) != "HTTP" || string(version) != "1.1" {
		return nil, 0, ErrorMalforedRequestLine
	}

	rl := &RequestLine{
		HTTPVersion:   string(version),
		RequestTarget: string(requestTarget),
		Method:        string(method),
	}

	return rl, read, nil
}

func (r *Request) done() bool {
	return r.state == StateDone || r.state == StateError
}

func RequestFromReader(reader io.Reader) (*Request, error) {
	request := NewRequest()

	// note: buffer could overrun, a buffer that exceeds 1k would do that
	// or the body
	buf := make([]byte, 1024)
	bufLen := 0
	for !request.done() {
		if bufLen == len(buf) {
			newBuf := make([]byte, len(buf)*2)
			copy(newBuf, buf[:bufLen])
			buf = newBuf
		}

		n, readErr := reader.Read(buf[bufLen:])
		bufLen += n

		readN, err := request.parse(buf[:bufLen])
		if err != nil {
			return nil, err
		}

		copy(buf, buf[readN:bufLen])
		bufLen -= readN

		// EOF (or any read error) is only a failure if the request is
		// still incomplete — a request that just finished parsing is fine.
		if readErr != nil && !request.done() {
			return nil, readErr
		}
	}

	return request, nil
}
