// Package request
package request

import "errors"

const maxLineLength = 4096

var ErrLineTooLong = errors.New("line too long")

var (
	StateChunkSize parserState = "StateChunkSize"
	StateChunkData parserState = "StateChunkData"
	StateChunkCRLF parserState = "StateChunkCRLF"
)

type chunkedReader struct {
	n        uint64 // unread bytes in chunk
	checkEnd bool   // need to check for a /r/n chunk footer
}

func NewChunkedReader() *chunkedReader {
	return &chunkedReader{
		n:        0,
		checkEnd: false,
	}
}

func (cr *chunkedReader) beginChunk() {
}

// since this is push-based code we need to send back how many were read
func readChunkLine(b []byte) (int, error) {
	return len(b), nil
}
