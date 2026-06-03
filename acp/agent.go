package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	acp_sdk "github.com/coder/acp-go-sdk"
	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/types"
)

// Options configures an AcpAgent.
type Options struct {
	BinaryPath string // path to ACP binary; empty uses "acpx" from PATH
}

// AcpAgent drives an ACP-compatible agent via JSON-RPC over stdio.
type AcpAgent struct {
	opts Options
}

// New creates an AcpAgent.
func New(opts Options) *AcpAgent {
	return &AcpAgent{opts: opts}
}

func (a *AcpAgent) Name() string             { return "ACP" }
func (a *AcpAgent) Provider() types.Provider { return "acp" }
func (a *AcpAgent) Close() error             { return nil }

// collector implements acp_sdk.Client, buffering streaming updates.
type collector struct {
	events []types.Event
	sid    string
	mu     sync.Mutex
}

func (c *collector) add(evt types.Event) {
	c.mu.Lock()
	c.events = append(c.events, evt)
	c.mu.Unlock()
}

func (c *collector) SessionUpdate(_ context.Context, n acp_sdk.SessionNotification) error {
	c.mu.Lock()
	if sid := string(n.SessionId); sid != "" && c.sid == "" {
		c.sid = sid
	}
	c.mu.Unlock()

	u := n.Update

	switch {
	case u.AgentMessageChunk != nil:
		if u.AgentMessageChunk.Content.Text != nil {
			c.add(types.Event{Type: types.EventTextDelta, TextDelta: u.AgentMessageChunk.Content.Text.Text})
		}
	case u.AgentThoughtChunk != nil:
		if u.AgentThoughtChunk.Content.Text != nil {
			c.add(types.Event{Type: types.EventTextDelta, TextDelta: u.AgentThoughtChunk.Content.Text.Text})
		}
	case u.ToolCall != nil:
		raw, _ := json.Marshal(u.ToolCall.RawInput)
		c.add(types.Event{
			Type:       types.EventToolCall,
			ToolCallID: string(u.ToolCall.ToolCallId),
			ToolName:   string(u.ToolCall.Kind),
			ToolInput:  raw,
		})
	case u.ToolCallUpdate != nil:
		raw, _ := json.Marshal(u.ToolCallUpdate.RawOutput)
		c.add(types.Event{
			Type:             types.EventToolResult,
			ToolResultID:     string(u.ToolCallUpdate.ToolCallId),
			ToolResultOutput: string(raw),
		})
	}
	return nil
}

func (c *collector) RequestPermission(ctx context.Context, params acp_sdk.RequestPermissionRequest) (acp_sdk.RequestPermissionResponse, error) {
	if len(params.Options) > 0 {
		return acp_sdk.RequestPermissionResponse{
			Outcome: acp_sdk.NewRequestPermissionOutcomeSelected(params.Options[0].OptionId),
		}, nil
	}
	return acp_sdk.RequestPermissionResponse{
		Outcome: acp_sdk.NewRequestPermissionOutcomeCancelled(),
	}, nil
}

func (c *collector) ReadTextFile(ctx context.Context, p acp_sdk.ReadTextFileRequest) (acp_sdk.ReadTextFileResponse, error) {
	return acp_sdk.ReadTextFileResponse{}, fmt.Errorf("read not implemented")
}
func (c *collector) WriteTextFile(ctx context.Context, p acp_sdk.WriteTextFileRequest) (acp_sdk.WriteTextFileResponse, error) {
	return acp_sdk.WriteTextFileResponse{}, fmt.Errorf("write not implemented")
}
func (c *collector) CreateTerminal(ctx context.Context, p acp_sdk.CreateTerminalRequest) (acp_sdk.CreateTerminalResponse, error) {
	return acp_sdk.CreateTerminalResponse{}, fmt.Errorf("terminal not implemented")
}
func (c *collector) KillTerminal(ctx context.Context, p acp_sdk.KillTerminalRequest) (acp_sdk.KillTerminalResponse, error) {
	return acp_sdk.KillTerminalResponse{}, fmt.Errorf("terminal not implemented")
}
func (c *collector) ReleaseTerminal(ctx context.Context, p acp_sdk.ReleaseTerminalRequest) (acp_sdk.ReleaseTerminalResponse, error) {
	return acp_sdk.ReleaseTerminalResponse{}, fmt.Errorf("terminal not implemented")
}
func (c *collector) TerminalOutput(ctx context.Context, p acp_sdk.TerminalOutputRequest) (acp_sdk.TerminalOutputResponse, error) {
	return acp_sdk.TerminalOutputResponse{}, fmt.Errorf("terminal not implemented")
}
func (c *collector) WaitForTerminalExit(ctx context.Context, p acp_sdk.WaitForTerminalExitRequest) (acp_sdk.WaitForTerminalExitResponse, error) {
	return acp_sdk.WaitForTerminalExitResponse{}, fmt.Errorf("terminal not implemented")
}

// Run starts an ACP subprocess and returns an event channel.
func (a *AcpAgent) Run(ctx context.Context, input types.RunInput) (<-chan types.Event, error) {
	if input.Prompt == "" {
		return nil, fmt.Errorf("acp: no prompt provided")
	}

	bin := a.opts.BinaryPath
	if bin == "" {
		bin = "acpx"
	}

	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("acp: %s not found: %w", bin, err)
	}

	args := []string{}
	if input.SessionID != "" {
		args = append(args, "--session", input.SessionID)
	}
	if input.WorkingDir != "" {
		args = append(args, "--cwd", input.WorkingDir)
	}

	cmd := exec.CommandContext(ctx, path, args...)
	if input.WorkingDir != "" {
		cmd.Dir = input.WorkingDir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}
	stderr := &strings.Builder{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start %s: %w", path, err)
	}

	col := &collector{}
	conn := acp_sdk.NewClientSideConnection(col, stdin, stdout)

	events := make(chan types.Event, 64)

	go func() {
		defer close(events)
		defer func() { _ = cmd.Process.Kill() }()

		// ACP handshake.
		if _, err := conn.Initialize(ctx, acp_sdk.InitializeRequest{
			ProtocolVersion: acp_sdk.ProtocolVersionNumber,
			ClientCapabilities: acp_sdk.ClientCapabilities{
				Fs:       acp_sdk.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
				Terminal: true,
			},
		}); err != nil {
			send(events, ctx, types.Event{Type: types.EventError, Error: fmt.Errorf("acp initialize: %w", err)})
			return
		}

		cwd := input.WorkingDir
		if cwd == "" {
			cwd = "/"
		}

		var sessID string
		if input.SessionID != "" {
			if _, err := conn.ResumeSession(ctx, acp_sdk.ResumeSessionRequest{
				SessionId: acp_sdk.SessionId(input.SessionID),
				Cwd:       cwd,
			}); err != nil {
				send(events, ctx, types.Event{Type: types.EventError, Error: fmt.Errorf("acp resume: %w", err)})
				return
			}
			sessID = input.SessionID
		} else {
			resp, err := conn.NewSession(ctx, acp_sdk.NewSessionRequest{
				Cwd:        cwd,
				McpServers: []acp_sdk.McpServer{},
			})
			if err != nil {
				send(events, ctx, types.Event{Type: types.EventError, Error: fmt.Errorf("acp new session: %w", err)})
				return
			}
			sessID = string(resp.SessionId)
		}

		// Send prompt.
		promptResp, err := conn.Prompt(ctx, acp_sdk.PromptRequest{
			SessionId: acp_sdk.SessionId(sessID),
			Prompt:    []acp_sdk.ContentBlock{acp_sdk.TextBlock(input.Prompt)},
		})
		if err != nil {
			if agentwrapper.IsContextLengthExceeded(err) {
				wrapped := &agentwrapper.ContextLengthExceededError{Err: fmt.Errorf("acp prompt: %s: %w", stderr.String(), err)}
				send(events, ctx, types.Event{Type: types.EventError, Error: wrapped})
				return
			}
			send(events, ctx, types.Event{Type: types.EventError, Error: fmt.Errorf("acp prompt: %s: %w", stderr.String(), err)})
			return
		}

		// Emit collected events.
		col.mu.Lock()
		for _, evt := range col.events {
			evt.SessionID = sessID
			if !send(events, ctx, evt) {
				col.mu.Unlock()
				return
			}
		}
		col.mu.Unlock()

		// Turn end.
		evt := types.Event{Type: types.EventTurnEnd, StopReason: "end_turn", SessionID: sessID}
		if promptResp.Usage != nil {
			evt.TokenUsage = &types.TokenUsage{
				InputTokens:  promptResp.Usage.InputTokens,
				OutputTokens: promptResp.Usage.OutputTokens,
				TotalTokens:  promptResp.Usage.TotalTokens,
			}
		}
		send(events, ctx, evt)
	}()

	return events, nil
}

func send(ch chan<- types.Event, ctx context.Context, evt types.Event) bool {
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
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
