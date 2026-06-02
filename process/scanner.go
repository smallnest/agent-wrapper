package process

import (
	"bufio"
	"io"
)

// Frame is a single protocol frame produced by a scanner.
type Frame struct {
	Data []byte // parsed frame payload (JSON body)
	Raw  []byte // raw line text (for debugging)
}

// FrameScanner scans frames from an io.Reader.
type FrameScanner interface {
	Scan() bool   // advance to next frame
	Frame() Frame // get current frame
	Err() error   // error encountered during scanning
}

func newScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return s
}

// NewJSONRPCScanner creates a line-delimited JSON scanner.
func NewJSONRPCScanner(r io.Reader) FrameScanner {
	return newJSONRPCScanner(r)
}

// NewSSEScanner creates an SSE (text/event-stream) scanner.
func NewSSEScanner(r io.Reader) FrameScanner {
	return newSSEScanner(r)
}
