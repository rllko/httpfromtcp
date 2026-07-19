// Package request
package request

import (
	"bytes"
	"fmt"
	"io"
	"strconv"

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
}

var (
	ErrorMalforedRequestLine    = fmt.Errorf("bad Request Line")
	ErrorMalforedRequestTarget  = fmt.Errorf("bad Request RequestTarget")
	ErrorUnsupportedHTTPVersion = fmt.Errorf("unsopported HTTP version")
	ErrorNoSeparatorFound       = fmt.Errorf("no Separator Found")
	ErrorInvalidHeaders         = fmt.Errorf("invalid headers")
	ErrorRequestInErrorState    = fmt.Errorf("request in error state")
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

func getInt(headers *headers.Headers, name string, defaultValue int) int {
	valueStr, exists := headers.Get(name)
	if !exists {
		return defaultValue
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}

	return value
}

func (r *Request) hasBody() bool {
	// chunked encoding not ready
	length := getInt(r.Headers, "content-length", 0)
	return length > 0
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
				if r.hasBody() {
					r.state = StateBody
				} else {
					r.state = StateDone
				}
			}
		case StateBody:

			length := getInt(r.Headers, "content-length", 0)
			if length == 0 {
				panic("chunked not implemented")
			}

			remaining := min(length-len(r.Body), len(currentData))
			r.Body = fmt.Sprintf("%s%s", r.Body, currentData[:remaining])
			read += remaining

			if len(r.Body) == length {
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

	// todo: fix this yukk
	if !ok || string(method) != "GET" && string(method) != "POST" {
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
		n, err := reader.Read(buf[bufLen:])
		if err != nil {
			return nil, err
		}

		bufLen += n
		readN, err := request.parse(buf[:bufLen])
		if err != nil {
			return nil, err
		}

		copy(buf, buf[readN:bufLen])
		bufLen -= readN
	}

	return request, nil
}
