// Package url contains URL parsing and unparsing.
package url

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type URL struct {
	Path     string
	RawQuery string
}

var (
	ErrIlegalEncodedSlash = errors.New("encoded slash is not legal")
	ErrInvalidPath        = errors.New("invalid path")
)

func Parse(s []byte) (*URL, error) {
	path, query, _ := bytes.Cut(s, []byte("?"))

	if bytes.Contains(path, []byte("%2F")) {
		return nil, ErrIlegalEncodedSlash
	}

	if bytes.Contains(path, []byte("%2f")) {
		return nil, ErrIlegalEncodedSlash
	}

	pathDecoded, err := PathDecode(path)
	if err != nil {
		return nil, err
	}

	if strings.ContainsRune(pathDecoded, 0x00) {
		return nil, fmt.Errorf("invalid character")
	}

	return &URL{
		Path:     pathDecoded,
		RawQuery: string(query),
	}, nil
}

func PathDecode(s []byte) (string, error) {
	var sb strings.Builder

	sb.Grow(len(s))

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case byte('%'):
			if i+2 >= len(s) {
				return "", ErrInvalidPath
			}

			ch, valid := hex(s[i+1], s[i+2])
			if !valid {
				return "", ErrInvalidPath
			}

			sb.WriteByte(ch)
			i += 2
		default:
			sb.WriteByte(s[i])

		}
	}
	return sb.String(), nil
}

func hex(hi, lo byte) (byte, bool) {
	var a, b byte

	switch {
	case '0' <= hi && hi <= '9':
		a = hi - '0'
	case 'a' <= hi && hi <= 'f':
		a = hi - 'a' + 10
	case 'A' <= hi && hi <= 'F':
		a = hi - 'A' + 10
	default:
		return 0, false
	}

	switch {
	case '0' <= lo && lo <= '9':
		b = lo - '0'
	case 'a' <= lo && lo <= 'f':
		b = lo - 'a' + 10
	case 'A' <= lo && lo <= 'F':
		b = lo - 'A' + 10
	default:
		return 0, false
	}

	return a*16 + b, true
}
