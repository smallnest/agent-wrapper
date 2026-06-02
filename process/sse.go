package process

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

type sseScanner struct {
	scanner *bufio.Scanner
	frame   Frame
	err     error
	done    bool
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{scanner: newScanner(r)}
}

func (s *sseScanner) Scan() bool {
	if s.done {
		return false
	}

	var dataLines [][]byte

	for s.scanner.Scan() {
		line := bytes.TrimRight(s.scanner.Bytes(), "\r")

		if len(line) == 0 {
			if len(dataLines) == 0 {
				continue
			}
			s.emitFrame(dataLines)
			return true
		}

		trimmed := bytes.TrimLeft(line, " ")
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			payload := trimmed[5:]
			if len(payload) > 0 && payload[0] == ' ' {
				payload = payload[1:]
			}
			dataLines = append(dataLines, payload)
		}
	}

	if len(dataLines) > 0 {
		s.emitFrame(dataLines)
		return true
	}

	s.err = s.scanner.Err()
	if s.err == nil {
		s.err = io.EOF
	}
	return false
}

func (s *sseScanner) emitFrame(dataLines [][]byte) {
	joined := bytes.Join(dataLines, []byte("\n"))

	if string(joined) == "[DONE]" {
		s.frame = Frame{Data: nil, Raw: joined}
		return
	}

	raw := make([]byte, 0, len(joined)+7)
	raw = append(raw, "data: "...)
	raw = append(raw, joined...)

	s.frame = Frame{Data: joined, Raw: raw}
}

func (s *sseScanner) Frame() Frame { return s.frame }
func (s *sseScanner) Err() error {
	if errors.Is(s.err, io.EOF) {
		return nil
	}
	return s.err
}
