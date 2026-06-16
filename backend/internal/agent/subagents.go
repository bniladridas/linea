package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrSubagentIDRequired    = errors.New("Subagent ID is required.")
	ErrSubagentIDExists      = errors.New("Subagent ID already exists.")
	ErrSubagentUnknown       = errors.New("Unknown subagent ID.")
	ErrSubagentPlanDepth     = errors.New("Subagent plan depth limit reached.")
	ErrSubagentPlanGoal      = errors.New("Subagent plan goal or query is required.")
)

const (
	maxSubagentRuns         = 50
	maxSubagentPlanRuns     = 30
	maxSubagentPlanChildren = 3
	maxSubagentDuration     = 30 * time.Second
)

func (r *Runtime) RunSubagent(ctx context.Context, subagentID string, input SubagentRunInput) (SubagentRun, error) {
	subagent, ok := r.findSubagentCustom(subagentID)
	if !ok {
		return SubagentRun{}, ErrSubagentUnknown
	}
	subCtx, cancel := context.WithTimeout(ctx, maxSubagentDuration)
	defer cancel()
	state := "completed"
	summary := strings.TrimSpace(input.Goal)
	if summary == "" {
		summary = subagent.Purpose
	}
	switch subagent.ID {
	case "review":
		diagnostics, err := r.ListDiagnostics(subCtx)
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
		results, err := r.SearchFiles(subCtx, query)
		if err != nil {
			state = subagentStateForError(err)
			summary = err.Error()
		} else {
			summary = fmt.Sprintf("Found %d result(s) for %q.", len(results), query)
		}
	case "test":
		s, err := r.runSubagentAutoLoop(subCtx, summary)
		if err != nil {
			state = "blocked"
			summary = err.Error()
		} else {
			summary = s
		}
	default:
		s, err := r.runSubagentAutoLoop(subCtx, summary)
		if err != nil {
			state = "blocked"
			summary = err.Error()
		} else {
			summary = s
		}
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

func (r *Runtime) runSubagentAutoLoop(ctx context.Context, goal string) (string, error) {
	loop, err := r.StartAgentLoop(ctx, AgentLoopInput{
		Goal:          goal,
		Mode:          "developer",
		MaxIterations: 3,
		AutoApply:     true,
	})
	if err != nil {
		return "", err
	}
	for i := 0; i < 5; i++ {
		loop, err = r.ContinueAgentLoop(ctx, loop.ID, AgentLoopContinueInput{})
		if err != nil {
			return "", err
		}
		if loop.State == "completed" || loop.State == "canceled" {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if loop.State != "completed" {
		r.CancelAgentLoop(ctx, loop.ID)
	}
	return loop.Summary, nil
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
	customSubagents := r.copyCustomSubagents()
	ids, err := selectSubagentPlan(input, customSubagents, 0)
	if err != nil {
		return SubagentPlanRun{}, err
	}
	runs := make([]SubagentRun, len(ids))
	parts := make([]string, len(ids))
	state := "completed"
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(index int, subagentID string) {
			defer wg.Done()
			run, runErr := r.RunSubagent(ctx, subagentID, SubagentRunInput{Goal: input.Goal, Query: input.Query})
			mu.Lock()
			defer mu.Unlock()
			if runErr != nil {
				runs[index] = SubagentRun{ID: newTraceID(), SubagentID: subagentID, State: "blocked", Summary: runErr.Error(), CreatedAt: time.Now().UTC()}
				parts[index] = fmt.Sprintf("%s blocked", subagentID)
				state = "blocked"
			} else {
				runs[index] = run
				parts[index] = fmt.Sprintf("%s %s", run.SubagentID, run.State)
				if run.State == "blocked" {
					state = "blocked"
				} else if state == "completed" && (run.State == "waiting_input" || run.State == "attention") {
					state = "attention"
				}
			}
		}(i, id)
	}
	wg.Wait()
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

func (r *Runtime) copyCustomSubagents() map[string]Subagent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := make(map[string]Subagent, len(r.customSubagents))
	for k, v := range r.customSubagents {
		m[k] = v
	}
	return m
}

func (r *Runtime) statusSubagentPlans() []SubagentPlanRun {
	items := r.ListSubagentPlans(context.Background())
	if len(items) > 5 {
		return items[:5]
	}
	return items
}

func selectSubagentPlan(input SubagentPlanInput, customSubagents map[string]Subagent, depth int) ([]string, error) {
	if depth > 3 {
		return nil, ErrSubagentPlanDepth
	}
	seen := map[string]bool{}
	ids := []string{}
	add := func(id string) error {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return nil
		}
		if _, ok := findSubagent(id); !ok {
			if _, ok := customSubagents[id]; !ok {
				return ErrSubagentUnknown
			}
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
		return nil, ErrSubagentPlanGoal
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

func (r *Runtime) findSubagentCustom(id string) (Subagent, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Subagent{}, false
	}
	r.mu.RLock()
	sa, ok := r.customSubagents[id]
	r.mu.RUnlock()
	if ok {
		return sa, true
	}
	return findSubagent(id)
}

func subagentStateForError(err error) string {
	if errors.Is(err, ErrWorkspaceDisabled) {
		return "waiting_input"
	}
	return "blocked"
}
