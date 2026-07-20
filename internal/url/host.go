package url

import (
	"errors"
	"net"
	"strings"
)

var (
	ErrInvalidHost = errors.New("invalid host")
	ErrInvalidPort = errors.New("invalid port")
)

// ValidateHost checks a Host header value (RFC 9110 §7.2; host syntax
// RFC 3986 §3.2.2): a reg-name, an IPv4 literal, or a bracketed IPv6
func ValidateHost(host string) error {
	if host == "" {
		return ErrInvalidHost
	}

	if host[0] == '[' {
		end := strings.IndexByte(host, ']')
		if end == -1 {
			return ErrInvalidHost // unclosed bracket
		}

		inner := host[1:end]
		if !strings.Contains(inner, ":") || net.ParseIP(inner) == nil {
			return ErrInvalidHost
		}

		rest := host[end+1:]
		if rest == "" {
			return nil
		}
		if rest[0] != ':' {
			return ErrInvalidHost // junk after the closing bracket
		}
		return validatePort(rest[1:])
	}

	switch strings.Count(host, ":") {
	case 0:
		return validateRegName(host)
	case 1:
		name, port, _ := strings.Cut(host, ":")
		if err := validateRegName(name); err != nil {
			return err
		}
		return validatePort(port)
	default:
		return ErrInvalidHost
	}
}

func validateRegName(s string) error {
	if s == "" {
		return ErrInvalidHost
	}

	for i := 0; i < len(s); i++ {
		c := s[i]

		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
			continue
		}

		switch c {
		// unreserved
		case '-', '.', '_', '~':
			continue
		// sub-delims
		case '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=':
			continue
		}

		return ErrInvalidHost
	}

	return nil
}

func validatePort(p string) error {
	if p == "" || len(p) > 5 {
		return ErrInvalidPort
	}

	n := 0
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return ErrInvalidPort
		}
		n = n*10 + int(p[i]-'0')
	}

	if n > 65535 {
		return ErrInvalidPort
	}

	return nil
}
