package framing

import (
	"bufio"
	"errors"
	"fmt"
)

var ErrFrameTooLarge = errors.New("framing: frame exceeds the maximum size")

// ReadFrame reads one newline-delimited frame without ever buffering more than max,
// so an upstream that never sends a newline cannot grow the reader without bound.
func ReadFrame(br *bufio.Reader, max int) ([]byte, error) {
	var frame []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(frame)+len(chunk) > max {
			return nil, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, max)
		}
		frame = append(frame, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return frame, err
	}
}

// ReadLineCapped keeps at most max bytes of a line and discards the rest of it, for
// text where losing the tail beats losing every line that follows.
func ReadLineCapped(br *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if room := max - len(line); room > 0 {
			if len(chunk) > room {
				line = append(line, chunk[:room]...)
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}
