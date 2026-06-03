package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/claude"
	"github.com/smallnest/agent-wrapper/codex"
	"github.com/smallnest/agent-wrapper/opencode"
	"github.com/smallnest/agent-wrapper/pi"
	"github.com/smallnest/agent-wrapper/types"
	"github.com/spf13/pflag"
)

var version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "list":
		cmdList()
	case "version":
		cmdVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`agent-wrapper — Unified coding agent wrapper

Usage:
  agent-wrapper <command> [flags]

Commands:
  run       Start an agent turn
  list      List registered providers
  version   Show version

Run flags:
  --provider NAME            Provider: claude-code|codex|pi-agent|opencode (required)
  --model NAME               Model name (passed to provider)
  --max-turns N              Maximum turns (default: 10)
  --working-dir PATH         Working directory
  --system-prompt-file PATH  Read system prompt from file
  --approve-all              Auto-approve all tool calls
  --budget-tokens N          Token budget limit
  --session-id UUID          Resume a session by ID
  --binary-path PATH         Override agent CLI binary path
  --json                     Output in JSON format (with --stream: NDJSON; without: single object)
  --stream                   Stream output (default true; set --stream=false to disable)

Examples:
  agent-wrapper run --provider claude-code "explain this code"
  agent-wrapper run --provider codex --model gpt-4 "fix the bug" --approve-all
  agent-wrapper run --provider claude-code --json "hello"        # single JSON object
  agent-wrapper run --provider claude-code --json --stream "hi"  # NDJSON stream
  agent-wrapper run --provider claude-code --session-id abc123 "continue"  # resume session
  agent-wrapper list
  agent-wrapper version`)
}

func cmdRun(args []string) {
	flags := parseRunFlags(args)
	if flags.provider == "" {
		fmt.Fprintln(os.Stderr, "error: --provider is required")
		os.Exit(1)
	}
	if flags.message == "" {
		fmt.Fprintln(os.Stderr, "error: MESSAGE argument is required")
		os.Exit(1)
	}

	// Set up registry with all providers.
	registry := agentwrapper.NewRegistry()
	if err := claude.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: register claude-code: %v\n", err)
	}
	if err := codex.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: register codex: %v\n", err)
	}
	if err := pi.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: register pi-agent: %v\n", err)
	}
	if err := opencode.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: register opencode: %v\n", err)
	}

	// Create agent from registry.
	agentOpts := map[string]any{}
	if flags.binaryPath != "" {
		agentOpts["binaryPath"] = flags.binaryPath
	}
	if flags.model != "" {
		agentOpts["model"] = flags.model
	}

	agent, err := registry.Get(flags.provider, agentOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Read system prompt from file if specified.
	var systemPrompt string
	if flags.systemPromptFile != "" {
		data, err := os.ReadFile(flags.systemPromptFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: read system prompt file: %v\n", err)
			os.Exit(1)
		}
		systemPrompt = string(data)
	}

	// Handle cancellation on SIGINT.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		cancel()
	}()

	maxTurns := flags.maxTurns
	if maxTurns == 0 {
		maxTurns = 10
	}

	outputFormat := types.OutputStream
	if flags.json {
		if flags.stream {
			outputFormat = types.OutputStreamJSON
		} else {
			outputFormat = types.OutputJSON
		}
	}

	orch := agentwrapper.NewOrchestrator(agent)

	input := types.RunInput{
		Prompt:       flags.message,
		SystemPrompt: systemPrompt,
		WorkingDir:   flags.workingDir,
		MaxTurns:     maxTurns,
		SessionID:    flags.sessionID,
		OutputFormat: outputFormat,
	}

	switch outputFormat {
	case types.OutputJSON:
		result, err := orch.RunSync(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		data, _ := json.Marshal(result)
		fmt.Println(string(data))

	case types.OutputStreamJSON:
		events, err := orch.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		for evt := range events {
			data, _ := json.Marshal(evt)
			fmt.Println(string(data))
		}

	default: // OutputStream
		events, err := orch.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		var sid string
		for evt := range events {
			if evt.SessionID != "" {
				sid = evt.SessionID
			}
			switch evt.Type {
			case types.EventTextDelta:
				fmt.Print(evt.TextDelta)
			case types.EventToolCall:
				fmt.Fprintf(os.Stderr, "[tool_call] %s(%s)\n", evt.ToolName, string(evt.ToolInput))
			case types.EventToolResult:
				prefix := "[tool_result]"
				if evt.ToolResultError {
					prefix = "[tool_error]"
				}
				fmt.Fprintf(os.Stderr, "%s %s\n", prefix, truncate(evt.ToolResultOutput, 200))
			case types.EventTurnEnd:
				if evt.TokenUsage != nil {
					fmt.Fprintf(os.Stderr, "[tokens] in=%d out=%d total=%d\n",
						evt.TokenUsage.InputTokens, evt.TokenUsage.OutputTokens, evt.TokenUsage.TotalTokens)
				}
			case types.EventError:
				fmt.Fprintf(os.Stderr, "[error] %v\n", evt.Error)
			}
		}
		if sid != "" {
			fmt.Fprintf(os.Stderr, "session: %s\n", sid)
		}
	}
}

func cmdList() {
	registry := agentwrapper.NewRegistry()
	if err := claude.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	if err := codex.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	if err := pi.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	if err := opencode.RegisterIn(registry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	for _, name := range registry.List() {
		agent, _ := registry.Get(name, nil)
		provider := string(agent.Provider())
		fmt.Printf("%-15s %s\n", name, provider)
	}
}

func cmdVersion() {
	fmt.Printf("agent-wrapper %s\n", version)
}

type runFlags struct {
	provider         string
	model            string
	maxTurns         int
	workingDir       string
	systemPromptFile string
	approveAll       bool
	budgetTokens     int
	sessionID        string
	binaryPath       string
	json             bool
	stream           bool
	message          string
}

func parseRunFlags(args []string) *runFlags {
	f := &runFlags{}
	fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
	fs.SetInterspersed(true)
	fs.StringVar(&f.provider, "provider", "", "Provider: claude-code|codex|pi-agent|opencode")
	fs.StringVar(&f.model, "model", "", "Model name")
	fs.IntVar(&f.maxTurns, "max-turns", 0, "Maximum turns")
	fs.StringVar(&f.workingDir, "working-dir", "", "Working directory")
	fs.StringVar(&f.systemPromptFile, "system-prompt-file", "", "Read system prompt from file")
	fs.BoolVar(&f.approveAll, "approve-all", false, "Auto-approve all tool calls")
	fs.IntVar(&f.budgetTokens, "budget-tokens", 0, "Token budget limit")
	fs.StringVar(&f.binaryPath, "binary-path", "", "Override agent CLI binary path")
	fs.StringVar(&f.sessionID, "session-id", "", "Resume a session by ID")
	fs.BoolVar(&f.json, "json", false, "Output in JSON format")
	fs.BoolVar(&f.stream, "stream", true, "Stream output (set --stream=false to disable)")
	_ = fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) > 0 {
		f.message = remaining[0]
	}

	return f
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
