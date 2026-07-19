// Package headers
package headers

import (
	"bytes"
	"fmt"
	"strings"
)

var (
	ErrorMalformedFieldName = fmt.Errorf("malfored field name")
	ErrorMalformedHeader    = fmt.Errorf("malformed header")
)

func isToken(str []byte) bool {
	if len(str) == 0 {
		return false
	}

	for _, ch := range str {
		found := false

		if ch >= 'a' && ch <= 'z' ||
			ch >= 'A' && ch <= 'Z' ||
			ch >= '0' && ch <= '9' {
			found = true
		}

		switch ch {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			found = true
		}

		if len(str) == 0 {
			found = true
		}

		if !found {
			return false
		}
	}

	return true
}

var rn = []byte("\r\n")

type Headers struct {
	headers map[string]string
}

func NewHeaders() *Headers {
	return &Headers{
		headers: map[string]string{},
	}
}

func (h *Headers) Get(name string) (string, bool) {
	str, exists := h.headers[strings.ToLower(name)]
	return str, exists
}

func (h *Headers) Delete(name string) {
	name = strings.ToLower(name)
	delete(h.headers, name)
}

func (h *Headers) Replace(name, value string) {
	name = strings.ToLower(name)
	h.headers[name] = value
}

func (h *Headers) Set(name, value string) {
	name = strings.ToLower(name)

	if v, ok := h.headers[name]; ok {
		h.headers[name] = fmt.Sprintf("%s, %s", v, value)
	} else {
		h.headers[name] = value
	}
}

func parseHeader(fieldLine []byte) (string, string, error) {
	before, after, ok := bytes.Cut(fieldLine, []byte(":"))
	if !ok {
		return "", "", ErrorMalformedHeader
	}

	value := bytes.TrimSpace(after)

	if bytes.Contains(before, []byte(" ")) {
		return "", "", ErrorMalformedFieldName
	}

	return string(before), string(value), nil
}

func (h *Headers) ForEach(cb func(n, v string)) {
	for n, v := range h.headers {
		cb(n, v)
	}
}

func (h *Headers) Parse(data []byte) (int, bool, error) {
	read := 0
	done := false
	for {
		idx := bytes.Index(data[read:], rn)

		if idx == -1 {
			break
		}

		if idx == 0 {
			done = true
			read += len(rn)
			break
		}

		name, value, err := parseHeader(data[read : read+idx])
		if err != nil {
			return 0, false, err
		}

		if !isToken([]byte(name)) {
			return 0, false, fmt.Errorf("malformed error name")
		}

		read += idx + len(rn)
		if existingVal, ok := h.headers[strings.ToLower(name)]; !ok {
			h.headers[strings.ToLower(name)] = value
		} else {
			h.headers[strings.ToLower(name)] = fmt.Sprintf("%s, %s", existingVal, value)
		}
	}

	return read, done, nil
}
