package process

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/smallnest/agent-wrapper/types"
)

type jsonrpcScanner struct {
	scanner *bufio.Scanner
	frame   Frame
	err     error
}

func newJSONRPCScanner(r io.Reader) *jsonrpcScanner {
	return &jsonrpcScanner{scanner: newScanner(r)}
}

func (s *jsonrpcScanner) Scan() bool {
	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		b := bytes.Clone(line)

		if !json.Valid(b) {
			s.err = &types.ProtocolError{
				RawBytes:    b,
				Description: "invalid JSON on line",
			}
			return false
		}

		s.frame = Frame{Data: b, Raw: b}
		return true
	}

	s.err = s.scanner.Err()
	if s.err == nil {
		s.err = io.EOF
	}
	return false
}

func (s *jsonrpcScanner) Frame() Frame { return s.frame }
func (s *jsonrpcScanner) Err() error {
	if errors.Is(s.err, io.EOF) {
		return nil
	}
	return s.err
}
