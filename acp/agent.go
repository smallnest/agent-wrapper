package acp

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures an AcpAgent.
type Options struct {
	BinaryPath string // path to acpx; empty = auto-detect
}

// AcpAgent wraps the acpx CLI via subprocess stdout.
type AcpAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates an AcpAgent.
func New(opts Options) *AcpAgent {
	return &AcpAgent{opts: opts}
}

func (a *AcpAgent) Name() string             { return "ACP" }
func (a *AcpAgent) Provider() types.Provider { return "acp" }
func (a *AcpAgent) Close() error             { return nil }

func (a *AcpAgent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}
	var onceErr error
	a.once.Do(func() {
		candidates := []string{"acpx"}
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "acpx"),
				filepath.Join(home, ".npm-global", "bin", "acpx"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf("acpx binary not found in PATH. Install with: npm install -g acpx")
	})
	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// acpxEvent is a single JSON line from `acpx --format json`.
type acpxEvent struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Delta   string `json:"delta,omitempty"`
	Text    string `json:"text,omitempty"`

	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`

	ToolResultID     string `json:"tool_result_id,omitempty"`
	ToolResultOutput string `json:"tool_result_output,omitempty"`
	ToolResultError  bool   `json:"tool_result_error,omitempty"`

	StopReason string     `json:"stop_reason,omitempty"`
	Usage      *acpxUsage `json:"usage,omitempty"`
	SessionID  string     `json:"session_id,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type acpxUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// RegisterIn registers the acp provider.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("acp", func(opts map[string]any) (agentwrapper.Agent, error) {
		o := Options{}
		if v, ok := opts["binaryPath"].(string); ok {
			o.BinaryPath = v
		}
		return New(o), nil
	}, true)
}
