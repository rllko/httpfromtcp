// Package request
package request

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

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

type Request struct {
	ID             uuid.UUID
	RequestLine    RequestLine
	state          parserState
	Headers        *headers.Headers
	Body           string
	URL            *url.URL
	Trailers       headers.Headers
	trailersLength int
	// bodyLength is the validated Content-Length, computed once when the
	// headers complete. Only meaningful in StateBody.
	bodyLength int

	// chunked encoding state tracking
	currentChunkSize int
	chunkBytesRead   int

	PathValues map[string]string

	// ctx is set by the server right before the handler runs. A request
	// built by hand (tests, parsing) has none and reads Background.
	ctx context.Context
}

// Context returns the request-local context. It is cancelled when the
// server shuts down (or the handler deadline passes); it is never nil.
func (r *Request) Context() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// WithContext attaches a request-local context and returns the same
// request so the server can chain the call at dispatch time.
func (r *Request) WithContext(ctx context.Context) *Request {
	r.ctx = ctx
	return r
}

type StatusCode int

type ErrRequest struct {
	Status  StatusCode
	Message string
	Err     error
}

func (e *ErrRequest) Error() string {
	return e.Message
}

func (e *ErrRequest) Unwrap() error {
	return e.Err
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
	ErrorInvalidTransferEncoding        = errors.New("invalid transfer-encoding")
	ErrorTEAndContentLength             = errors.New("transfer-encoding with content-length")
	SEPARATOR                           = []byte("\r\n")
)

type parserState string

const (
	maxChunkLine = 8 << 10  // 8192
	maxBodySize  = 10 << 20 // 10485760
)

const (
	StateInit                parserState = "init"
	StateDone                parserState = "done"
	StateError               parserState = "error"
	StateHeaders             parserState = "headers"
	StateBody                parserState = "body"
	StateConsumeChunkTrailer parserState = "chunkedTrailer"
)

func NewRequest() *Request {
	return &Request{
		ID:         uuid.New(),
		state:      StateInit,
		Headers:    headers.NewHeaders(),
		Body:       "",
		URL:        &url.URL{},
		Trailers:   *headers.NewHeaders(),
		PathValues: map[string]string{},
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
	//decodedBody := make([]byte, 1000000)
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

				if data, hasTE := r.Headers.Get("transfer-encoding"); hasTE {
					// RFC 9112 §6.1: TE+CL is ambiguous framing (a
					// smuggling vector) and must be rejected before any
					// body byte is read.
					if _, hasCL := r.Headers.Get("content-length"); hasCL {
						r.state = StateError
						return 0, &ErrRequest{
							Status:  400,
							Message: ErrorTEAndContentLength.Error(),
							Err:     ErrorTEAndContentLength,
						}
					}

					str, exists := r.Headers.Get("Trailer")
					if exists {
						for idx, t := range strings.Split(str, ",") {
							if idx > 50 {
								break
							}

							r.Trailers.Set(t, "")
						}
					}

					// transfer-coding names are case-insensitive (RFC 9110 §5.6.2)
					params := strings.Split(data, ",")
					for i := range params {
						params[i] = strings.ToLower(strings.TrimSpace(params[i]))
					}

					occurChunked := 0
					for _, param := range params {
						if param == "chunked" {
							occurChunked++
						}
					}

					if occurChunked > 1 ||
						(occurChunked == 1 && params[len(params)-1] != "chunked") {
						r.state = StateError
						return 0, &ErrRequest{
							Status:  400,
							Message: ErrorInvalidTransferEncoding.Error(),
							Err:     ErrorInvalidTransferEncoding,
						}
					}

					// only exactly one "chunked" coding is decodable;
					// anything else fails closed
					if len(params) != 1 || occurChunked != 1 {
						r.state = StateError
						return 0, &ErrRequest{
							Status:  501,
							Message: ErrorTransferEncodingNotImplemented.Error(),
							Err:     ErrorTransferEncodingNotImplemented,
						}
					}

					r.state = StateChunkSize
					break
				}

				length, err := r.contentLength()
				if err != nil {
					r.state = StateError
					return 0, err
				}
				r.bodyLength = length

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
		case StateChunkSize:

			size, idx, err := beginChunk(currentData)
			if err != nil {
				r.state = StateError
				return 0, err
			}

			// (0, 0, nil) is beginChunk's "no full size line yet". A real
			// terminal chunk ("0\r\n") always has idx >= 1, so this cannot
			// collide: wait for more bytes instead of mis-framing.
			if size == 0 && idx == 0 {
				break outer
			}

			read += idx + 2
			r.currentChunkSize = int(size)
			r.chunkBytesRead = 0

			if r.currentChunkSize == 0 {
				r.state = StateConsumeTrailers
			} else {
				r.state = StateChunkData
			}
		case StateChunkData:
			needed := r.currentChunkSize - r.chunkBytesRead
			available := len(currentData)
			toRead := min(needed, available)

			if len(r.Body)+toRead > maxBodySize {
				return 0, &ErrRequest{
					Status:  413,
					Message: "Body too Large",
				}
			}

			r.Body += string(currentData[:toRead])
			read += toRead
			r.chunkBytesRead += toRead

			if r.chunkBytesRead == r.currentChunkSize {
				r.state = StateChunkCRLF
			}

		case StateChunkCRLF:
			if len(currentData) < 2 {
				break outer
			}

			// i couldn do bytes.Index here but checking is cheaper
			if currentData[0] != '\r' || currentData[1] != '\n' {
				r.state = StateError
				return 0, &ErrRequest{
					Status:  400,
					Message: "missing CRLF after chunk data",
				}
			}

			read += 2
			if r.currentChunkSize == 0 {
				r.state = StateDone
			} else {
				r.state = StateChunkSize
			}
		case StateConsumeTrailers:
			idx := bytes.Index(currentData, []byte("\r\n"))
			if idx == -1 {
				break outer
			}

			if idx == 0 {
				read += 2
				r.state = StateDone
				break
			}

			before, after, ok := bytes.Cut(currentData[:idx], []byte(":"))
			if !ok {
				return 0, &ErrRequest{
					Status:  400,
					Message: "invalid Trailers",
				}
			}

			// checking never hurts, this would be a bug otherwise
			if _, exist := r.Trailers.Get(string(before)); exist {
				r.Trailers.Replace(string(before), strings.TrimSpace(string(after)))
			}

			if r.trailersLength+idx > maxChunkLine {
				return 0, &ErrRequest{
					Status: 413,
				}
			}

			r.trailersLength += idx + 2
			read += idx + 2

			r.state = StateChunkCRLF
		case StateDone:
			break outer

		default:
			panic("somehow im here")
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

func (r *Request) SetPathValue(name string, value string) {
	r.PathValues[name] = value
}

func (r *Request) PathValue(name string) (string, bool) {
	val, ok := r.PathValues[name]
	return val, ok
}
