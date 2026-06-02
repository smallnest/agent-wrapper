package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/process"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures an OpenCodeAgent.
type Options struct {
	BinaryPath string         // path to opencode binary; empty = auto-detect
	Model      string         // model name passed via OPENCODE_MODEL env
	Extra      map[string]any // provider-specific parameters
}

// OpenCodeAgent drives the OpenCode CLI via non-interactive mode (-p -f json).
//
// OpenCode does not expose a streaming stdio protocol. The agent runs in
// non-interactive mode, collects the full stdout output, parses the JSON
// response, and emits it as a single TextDelta + TurnEnd event pair.
type OpenCodeAgent struct {
	opts   Options
	binary string
	once   sync.Once
}

// New creates an OpenCodeAgent.
func New(opts Options) *OpenCodeAgent {
	return &OpenCodeAgent{opts: opts}
}

func (a *OpenCodeAgent) Name() string { return "OpenCode" }
func (a *OpenCodeAgent) Provider() types.Provider {
	return types.ProviderOpenCode
}
func (a *OpenCodeAgent) Close() error { return nil }

func (a *OpenCodeAgent) resolveBinary() (string, error) {
	if a.opts.BinaryPath != "" {
		return a.opts.BinaryPath, nil
	}

	var onceErr error
	a.once.Do(func() {
		home, _ := os.UserHomeDir()
		candidates := []string{"opencode"}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "opencode"),
				filepath.Join(home, "go", "bin", "opencode"),
			)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				a.binary = p
				return
			}
		}
		onceErr = fmt.Errorf(
			"opencode binary not found in PATH, ~/.local/bin, or ~/go/bin. Install with: go install github.com/opencode-ai/opencode@latest",
		)
	})

	if onceErr != nil {
		return "", onceErr
	}
	return a.binary, nil
}

// opencodeResponse is the JSON output from `opencode -p ... -f json`.
type opencodeResponse struct {
	Response string `json:"response"`
}

// RegisterIn replaces the opencode stub in the registry with a real factory.
func RegisterIn(r *agentwrapper.Registry) error {
	return r.Register("opencode", func(opts map[string]any) (agentwrapper.Agent, error) {
		return New(Options{}), nil
	}, true)
}

// Run starts an opencode subprocess in non-interactive mode and returns an event channel.
func (a *OpenCodeAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	bin, err := a.resolveBinary()
	if err != nil {
		return nil, err
	}

	// Build the prompt from session messages or new message.
	messages := input.Session.Messages
	if input.NewMessage != nil {
		messages = append(messages, *input.NewMessage)
	}
	prompt := messagesToPrompt(messages)
	if prompt == "" {
		return nil, fmt.Errorf("opencode: no user message found in input")
	}

	args := []string{"-p", prompt, "-f", "json", "-q"}

	env := map[string]string{}
	if a.opts.Model != "" {
		env["OPENCODE_MODEL"] = a.opts.Model
	}
	if input.SystemPrompt != "" {
		env["OPENCODE_SYSTEM_PROMPT"] = input.SystemPrompt
	}

	proc, err := process.StartProcess(ctx, process.ProcessConfig{
		Command: bin,
		Args:    args,
		WorkDir: input.WorkingDir,
		Env:     env,
	})
	if err != nil {
		return nil, fmt.Errorf("start opencode process: %w", err)
	}

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)

		// Read all stdout — opencode blocks until complete in non-interactive mode.
		scanner := process.NewJSONRPCScanner(proc.Stdout())
		var responseBody string

		for scanner.Scan() {
			frame := scanner.Frame()
			var resp opencodeResponse
			if err := json.Unmarshal(frame.Data, &resp); err == nil && resp.Response != "" {
				responseBody = resp.Response
				break
			}
		}

		proc.Close()

		if ctx.Err() != nil {
			events <- types.Event{Type: types.EventError, Error: ctx.Err()}
			return
		}

		if responseBody != "" {
			events <- types.Event{Type: types.EventTextDelta, TextDelta: responseBody}
		}

		stopReason := "end_turn"
		if responseBody == "" {
			stopReason = "error"
		}

		events <- types.Event{
			Type:       types.EventTurnEnd,
			TurnNumber: 1,
			StopReason: stopReason,
		}
	}()

	return events, nil
}
