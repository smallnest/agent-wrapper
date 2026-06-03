package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	agentwrapper "github.com/smallnest/agent-wrapper"
	"github.com/smallnest/agent-wrapper/claude"
	"github.com/smallnest/agent-wrapper/codex"
	"github.com/smallnest/agent-wrapper/opencode"
	"github.com/smallnest/agent-wrapper/pi"
	"github.com/smallnest/agent-wrapper/sessionstore/memory"
	"github.com/smallnest/agent-wrapper/types"
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
	case "sessions":
		cmdSessions()
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
  sessions  List all sessions
  version   Show version

Run flags:
  --provider NAME            Provider: claude-code|codex|pi-agent|opencode (required)
  --model NAME               Model name (passed to provider)
  --max-turns N              Maximum turns (default: 10)
  --working-dir PATH         Working directory
  --system-prompt-file PATH  Read system prompt from file
  --approve-all              Auto-approve all tool calls
  --budget-tokens N          Token budget limit
  --session-id UUID          Resume an existing session
  --binary-path PATH         Override agent CLI binary path
  --json                     Output in JSON format (with --stream: NDJSON; without: single object)
  --stream                   Stream output (default true; set --stream=false to disable)

Examples:
  agent-wrapper run --provider claude-code "explain this code"
  agent-wrapper run --provider codex --model gpt-4 "fix the bug" --approve-all
  agent-wrapper run --provider claude-code --json "hello"        # single JSON object
  agent-wrapper run --provider claude-code --json --stream "hi"  # NDJSON stream
  agent-wrapper list
  agent-wrapper sessions
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

	// Set up session store.
	store := memory.New()

	var session *types.Session
	if flags.sessionID != "" {
		session, err = store.Get(flags.sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: session not found: %v\n", err)
			os.Exit(1)
		}
	} else {
		session, err = store.Create()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: create session: %v\n", err)
			os.Exit(1)
		}
	}

	// Build orchestrator options.
	var orchOpts []agentwrapper.OrchestratorOption
	if flags.approveAll {
		orchOpts = append(orchOpts, agentwrapper.WithApprovalHandler(
			func(ctx context.Context, call agentwrapper.ToolCall) (*agentwrapper.Decision, error) {
				return &agentwrapper.Decision{Action: agentwrapper.ActionAllow}, nil
			},
		))
	}
	if flags.budgetTokens > 0 {
		orchOpts = append(orchOpts, agentwrapper.WithBudgetHandler(
			func(ctx context.Context, usage types.TokenUsage) error {
				if usage.TotalTokens > flags.budgetTokens {
					return fmt.Errorf("budget exceeded: used %d of %d tokens", usage.TotalTokens, flags.budgetTokens)
				}
				return nil
			},
		))
	}

	orch := agentwrapper.NewOrchestrator(agent, store, orchOpts...)

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

	// Determine output format from flags.
	outputFormat := types.OutputStream
	if flags.json {
		if flags.stream {
			outputFormat = types.OutputStreamJSON
		} else {
			outputFormat = types.OutputJSON
		}
	}

	input := types.RunInput{
		Session:      session,
		NewMessage:   func() *types.Message { m := types.NewUserMessage(flags.message); return &m }(),
		SystemPrompt: systemPrompt,
		WorkingDir:   flags.workingDir,
		MaxTurns:     maxTurns,
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

		fmt.Fprintf(os.Stderr, "session: %s\n", session.ID)

		for evt := range events {
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
				fmt.Fprintln(os.Stderr)
				if evt.TokenUsage != nil {
					fmt.Fprintf(os.Stderr, "[tokens] in=%d out=%d total=%d\n",
						evt.TokenUsage.InputTokens, evt.TokenUsage.OutputTokens, evt.TokenUsage.TotalTokens)
				}
			case types.EventError:
				fmt.Fprintf(os.Stderr, "[error] %v\n", evt.Error)
			}
		}

		fmt.Fprintln(os.Stderr)
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

func cmdSessions() {
	store := memory.New()
	summaries := store.List()
	if len(summaries) == 0 {
		fmt.Println("No sessions.")
		return
	}
	fmt.Printf("%-38s  %-6s  %-20s  %-20s\n", "ID", "MSGS", "CREATED", "UPDATED")
	for _, s := range summaries {
		fmt.Printf("%-38s  %-6d  %-20s  %-20s\n",
			s.ID, s.MessageCount,
			s.CreatedAt.Format(time.DateTime), s.UpdatedAt.Format(time.DateTime))
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
	f := &runFlags{stream: true}
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--provider":
			i++
			if i < len(args) {
				f.provider = args[i]
			}
		case "--model":
			i++
			if i < len(args) {
				f.model = args[i]
			}
		case "--max-turns":
			i++
			if i < len(args) {
				_, _ = fmt.Sscanf(args[i], "%d", &f.maxTurns)
			}
		case "--working-dir":
			i++
			if i < len(args) {
				f.workingDir = args[i]
			}
		case "--system-prompt-file":
			i++
			if i < len(args) {
				f.systemPromptFile = args[i]
			}
		case "--approve-all":
			f.approveAll = true
		case "--budget-tokens":
			i++
			if i < len(args) {
				_, _ = fmt.Sscanf(args[i], "%d", &f.budgetTokens)
			}
		case "--session-id":
			i++
			if i < len(args) {
				f.sessionID = args[i]
			}
		case "--binary-path":
			i++
			if i < len(args) {
				f.binaryPath = args[i]
			}
		case "--json":
			f.json = true
		case "--stream":
			f.stream = true
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			if !strings.HasPrefix(args[i], "-") {
				f.message = args[i]
			}
		}
		i++
	}
	return f
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
