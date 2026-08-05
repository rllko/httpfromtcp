// Package request
package request

import (
	"bytes"
	"strconv"
)

const maxLineLength = 8 * 1024 * 1024

var (
	StateChunkSize       parserState = "StateChunkSize"
	StateChunkData       parserState = "StateChunkData"
	StateChunkCRLF       parserState = "StateChunkCRLF"
	StateConsumeTrailers parserState = "StateConsumeTrailers"
)

func beginChunk(currentData []byte) (int, int, error) {
	idx := bytes.Index(currentData, []byte("\r\n"))
	if idx == -1 {
		// incomplete line, wait for more
		return 0, 0, nil
	}
	rawSize := string(removeChunkExtension(currentData[:idx]))

	size, err := strconv.ParseUint(rawSize, 16, 64)

	if size > maxLineLength {
		return 0, 0, &ErrRequest{
			Status:  413,
			Message: "line too long",
		}
	}

	if err != nil {
		return 0, 0, &ErrRequest{
			Status:  400,
			Message: "invalid chunk size format:\n" + err.Error(),
		}
	}

	return int(size), idx, nil
}

func removeChunkExtension(p []byte) []byte {
	p, _, _ = bytes.Cut(p, []byte(";"))

	return p
}

// since this is push-based code we need to send back how many were read
func readChunkLine(b []byte) (int, error) {
	return len(b), nil
}
