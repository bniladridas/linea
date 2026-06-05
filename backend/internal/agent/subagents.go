package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxSubagentRuns = 50

func (r *Runtime) RunSubagent(ctx context.Context, subagentID string, input SubagentRunInput) (SubagentRun, error) {
	subagent, ok := findSubagent(subagentID)
	if !ok {
		return SubagentRun{}, errors.New("Unknown subagent ID.")
	}
	state := "completed"
	summary := strings.TrimSpace(input.Goal)
	if summary == "" {
		summary = subagent.Purpose
	}
	switch subagent.ID {
	case "review":
		diagnostics, err := r.ListDiagnostics(ctx)
		if err != nil {
			state = subagentStateForError(err)
			summary = err.Error()
		} else {
			summary = fmt.Sprintf("Reviewed diagnostics: %d issue(s).", len(diagnostics))
		}
	case "search", "docs":
		query := strings.TrimSpace(input.Query)
		if query == "" {
			query = strings.TrimSpace(input.Goal)
		}
		if query == "" {
			state = "waiting_input"
			summary = "Provide a search query."
			break
		}
		results, err := r.SearchFiles(ctx, query)
		if err != nil {
			state = subagentStateForError(err)
			summary = err.Error()
		} else {
			summary = fmt.Sprintf("Found %d result(s) for %q.", len(results), query)
		}
	case "test":
		summary = "Use command approval before running checks."
		state = "waiting_input"
	default:
		state = "waiting_input"
		summary = "Subagent is planned."
	}
	run := SubagentRun{
		ID:         newTraceID(),
		SubagentID: subagent.ID,
		State:      state,
		Summary:    trimRunes(summary, 240),
		CreatedAt:  time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subagentRuns = append([]SubagentRun{run}, r.subagentRuns...)
	if len(r.subagentRuns) > maxSubagentRuns {
		r.subagentRuns = r.subagentRuns[:maxSubagentRuns]
	}
	return run, nil
}

func findSubagent(id string) (Subagent, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Subagent{}, false
	}
	for _, subagent := range defaultSubagents() {
		if subagent.ID == id {
			return subagent, true
		}
	}
	return Subagent{}, false
}

func subagentStateForError(err error) string {
	if errors.Is(err, ErrWorkspaceDisabled) {
		return "waiting_input"
	}
	return "blocked"
}
