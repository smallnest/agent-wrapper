package types

import (
	"fmt"
	"time"
)

// ExitError indicates an abnormal agent CLI process exit.
type ExitError struct {
	ExitCode int
	Stderr   string
	Command  string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("agent '%s' exited with code %d: %s", e.Command, e.ExitCode, e.Stderr)
}

// ProtocolError indicates agent CLI output could not be parsed.
type ProtocolError struct {
	Provider    Provider
	RawBytes    []byte
	Description string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("failed to parse agent output from '%s': %s (raw: %s)",
		e.Provider, e.Description, string(e.RawBytes))
}

// BudgetExceededError indicates the token budget has been exhausted.
type BudgetExceededError struct {
	Used  int
	Limit int
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("token budget exceeded: used %d of %d", e.Used, e.Limit)
}

// SessionNotFoundError indicates a session ID does not exist.
type SessionNotFoundError struct {
	ID string
}

func (e *SessionNotFoundError) Error() string {
	return fmt.Sprintf("session '%s' not found", e.ID)
}

// TimeoutError indicates an agent run has timed out.
type TimeoutError struct {
	Duration time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("agent run timed out after %s", e.Duration)
}
