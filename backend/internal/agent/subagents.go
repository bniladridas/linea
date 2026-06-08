package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	maxSubagentRuns         = 50
	maxSubagentPlanRuns     = 30
	maxSubagentPlanChildren = 3
)

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

func (r *Runtime) ListSubagentPlans(context.Context) []SubagentPlanRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.subagentPlans) == 0 {
		return []SubagentPlanRun{}
	}
	return append([]SubagentPlanRun(nil), r.subagentPlans...)
}

func (r *Runtime) RunSubagentPlan(ctx context.Context, input SubagentPlanInput) (SubagentPlanRun, error) {
	ids, err := selectSubagentPlan(input)
	if err != nil {
		return SubagentPlanRun{}, err
	}
	runs := make([]SubagentRun, 0, len(ids))
	state := "completed"
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		run, runErr := r.RunSubagent(ctx, id, SubagentRunInput{Goal: input.Goal, Query: input.Query})
		if runErr != nil {
			return SubagentPlanRun{}, runErr
		}
		runs = append(runs, run)
		parts = append(parts, fmt.Sprintf("%s %s", run.SubagentID, run.State))
		if run.State == "blocked" {
			state = "blocked"
		} else if state == "completed" && (run.State == "waiting_input" || run.State == "attention") {
			state = "attention"
		}
	}
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		goal = strings.TrimSpace(input.Query)
	}
	if goal == "" {
		goal = "Run subagent plan"
	}
	plan := SubagentPlanRun{
		ID:          newTraceID(),
		Goal:        trimRunes(goal, 240),
		State:       state,
		Summary:     trimRunes(fmt.Sprintf("Ran %d subagent(s): %s.", len(runs), strings.Join(parts, "; ")), 240),
		SubagentIDs: ids,
		Runs:        runs,
		CreatedAt:   time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subagentPlans = append([]SubagentPlanRun{plan}, r.subagentPlans...)
	if len(r.subagentPlans) > maxSubagentPlanRuns {
		r.subagentPlans = r.subagentPlans[:maxSubagentPlanRuns]
	}
	return plan, nil
}

func (r *Runtime) statusSubagentPlans() []SubagentPlanRun {
	items := r.ListSubagentPlans(context.Background())
	if len(items) > 5 {
		return items[:5]
	}
	return items
}

func selectSubagentPlan(input SubagentPlanInput) ([]string, error) {
	seen := map[string]bool{}
	ids := []string{}
	add := func(id string) error {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return nil
		}
		if _, ok := findSubagent(id); !ok {
			return errors.New("Unknown subagent ID.")
		}
		if len(ids) >= maxSubagentPlanChildren {
			return nil
		}
		seen[id] = true
		ids = append(ids, id)
		return nil
	}
	for _, id := range input.SubagentIDs {
		if err := add(id); err != nil {
			return nil, err
		}
	}
	if len(ids) > 0 {
		return ids, nil
	}
	goal := strings.ToLower(strings.TrimSpace(input.Goal + " " + input.Query))
	if goal == "" {
		return nil, errors.New("Subagent plan goal or query is required.")
	}
	if strings.Contains(goal, "review") || strings.Contains(goal, "diagnostic") || strings.Contains(goal, "fix") || strings.Contains(goal, "test") || strings.Contains(goal, "check") {
		if err := add("review"); err != nil {
			return nil, err
		}
	}
	if strings.Contains(goal, "doc") || strings.Contains(goal, "readme") {
		if err := add("docs"); err != nil {
			return nil, err
		}
	}
	if input.Query != "" || strings.Contains(goal, "search") || strings.Contains(goal, "find") || strings.Contains(goal, "context") {
		if err := add("search"); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		if err := add("review"); err != nil {
			return nil, err
		}
	}
	return ids, nil
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
