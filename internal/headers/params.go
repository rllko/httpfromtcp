package headers

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var (
	ErrInvalidToken           = errors.New("invalid token")
	ErrInvalidEscape          = errors.New("invalid escape sequence")
	ErrMissingOpeningQuote    = errors.New("string must start with a double quote")
	ErrMissingClosingQuote    = errors.New("missing closing quote")
	ErrInvalidHeaderParameter = errors.New("invalid header parameter")
	ErrDuplicateParameter     = errors.New("duplicate parameter")
)

func isTokenChar(ch byte) bool {
	switch {
	case 'a' <= ch && ch <= 'z':
		return true
	case 'A' <= ch && ch <= 'Z':
		return true
	case '0' <= ch && ch <= '9':
		return true
	}

	switch ch {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}

	return false
}

// RFC 9110 5.6.4 quoted-string starting at s[0].
func ParseQuoted(s string) (parsed string, n int, err error) {
	if len(s) < 2 || s[0] != '"' {
		return "", 0, ErrMissingOpeningQuote
	}

	var sb strings.Builder
	sb.Grow(len(s))

	inEscape := false

	for i := 1; i < len(s); i++ {
		b := s[i]

		if inEscape {

			if (b < 0x20 || b == 0x7F) && b != '\t' {
				return "", 0, fmt.Errorf("%w: 0x%02X", ErrInvalidEscape, b)
			}
			sb.WriteByte(b)
			inEscape = false
			continue
		}

		if b == '\\' {
			inEscape = true
			continue
		}

		if b == '"' {
			return sb.String(), i + 1, nil
		}

		sb.WriteByte(b)
	}

	return "", 0, ErrMissingClosingQuote
}

func ParseToken(s string) (token string, n int, err error) {
	i := 0
	for i < len(s) && isTokenChar(s[i]) {
		i++
	}

	if i == 0 {
		return "", 0, ErrInvalidToken
	}

	return s[:i], i, nil
}

func consumeHeaderParam(s string) (string, string, string) {
	key := ""
	value := ""
	rest := ""
	orig := s

	s = strings.TrimLeftFunc(s, unicode.IsSpace)

	// Expect ';'
	if len(s) == 0 || s[0] != ';' {
		return "", "", orig
	}
	s = s[1:]

	s = strings.TrimLeftFunc(s, unicode.IsSpace)

	key, n, err := ParseToken(s)
	if err != nil {
		return "", "", orig
	}
	s = s[n:]

	s = strings.TrimLeftFunc(s, unicode.IsSpace)

	// Expect '='
	if len(s) == 0 || s[0] != '=' {
		return "", "", orig
	}
	s = s[1:]

	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if len(s) > 0 && s[0] == '"' {
		value, n, err = ParseQuoted(s)
	} else {
		value, n, err = ParseToken(s)
	}
	if err != nil {
		return "", "", orig
	}

	rest = s[n:]
	return key, value, rest
}

// RFC 9110 5.6.6
func ParseHeaderValue(s string) (base string, params map[string]string, err error) {
	before, rest, found := strings.Cut(s, ";")

	base = strings.TrimSpace(before)
	if base == "" {
		return "", nil, ErrInvalidHeaderParameter
	}

	if !found {
		return base, map[string]string{}, nil
	}

	params, err = ParseHeaderParams(";" + rest)
	if err != nil {
		return "", nil, err
	}

	return base, params, nil
}

func ParseHeaderParams(s string) (map[string]string, error) {
	params := make(map[string]string)

	for len(s) > 0 {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)

		if len(s) == 0 {
			break
		}

		key, value, rest := consumeHeaderParam(s)
		if key == "" {
			if strings.TrimSpace(rest) == ";" {
				break
			}
			return nil, ErrInvalidHeaderParameter
		}

		key = strings.ToLower(key)

		if old, ok := params[key]; ok && old != value {
			return nil, ErrDuplicateParameter
		}

		params[key] = value
		s = rest
	}

	return params, nil
}
