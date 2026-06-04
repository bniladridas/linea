package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

type Runtime struct {
	mu        sync.RWMutex
	rulesPath string
	traces    []Trace
}

type Status struct {
	Mode        string   `json:"mode"`
	Rules       RuleSet  `json:"rules"`
	Tools       []Tool   `json:"tools"`
	Hooks       []Hook   `json:"hooks"`
	Skills      []Skill  `json:"skills"`
	Boundaries  []string `json:"boundaries"`
	Next        []string `json:"next"`
	TraceEvents []Trace  `json:"traceEvents"`
}

type RuleSet struct {
	Source  string   `json:"source"`
	Loaded  bool     `json:"loaded"`
	Summary []string `json:"summary"`
}

type Tool struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Access   string `json:"access"`
	Approval string `json:"approval"`
}

type Hook struct {
	ID    string `json:"id"`
	Event string `json:"event"`
	State string `json:"state"`
}

type Skill struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type Trace struct {
	ID        string    `json:"id"`
	Event     string    `json:"event"`
	State     string    `json:"state"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type TraceInput struct {
	Event  string `json:"event"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

func NewRuntime(rulesPath string) *Runtime {
	if strings.TrimSpace(rulesPath) == "" {
		rulesPath = "AGENTS.md"
	}
	return &Runtime{rulesPath: rulesPath}
}

func (r *Runtime) Status(ctx context.Context) Status {
	return Status{
		Mode:        "local",
		Rules:       r.loadRules(ctx),
		Tools:       defaultTools(),
		Hooks:       defaultHooks(),
		Skills:      defaultSkills(),
		Boundaries:  defaultBoundaries(),
		Next:        defaultNext(),
		TraceEvents: r.statusTraces(),
	}
}

func (r *Runtime) ListTraces(context.Context) []Trace {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Trace(nil), r.traces...)
}

func (r *Runtime) AddTrace(_ context.Context, input TraceInput) (Trace, error) {
	event := strings.TrimSpace(input.Event)
	state := strings.TrimSpace(input.State)
	if event == "" || state == "" {
		return Trace{}, errors.New("Trace event and state are required.")
	}
	detail := strings.TrimSpace(input.Detail)
	if len([]rune(detail)) > 240 {
		detail = string([]rune(detail)[:240])
	}
	trace := Trace{
		ID:        newTraceID(),
		Event:     event,
		State:     state,
		Detail:    detail,
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.traces = append([]Trace{trace}, r.traces...)
	if len(r.traces) > 50 {
		r.traces = r.traces[:50]
	}
	return trace, nil
}

func (r *Runtime) statusTraces() []Trace {
	traces := r.ListTraces(context.Background())
	if len(traces) > 5 {
		return traces[:5]
	}
	if len(traces) > 0 {
		return traces
	}
	return []Trace{{
		ID:        "runtime-ready",
		Event:     "agent runtime",
		State:     "ready",
		CreatedAt: time.Now().UTC(),
	}}
}

func (r *Runtime) loadRules(ctx context.Context) RuleSet {
	select {
	case <-ctx.Done():
		return fallbackRules()
	default:
	}

	data, err := os.ReadFile(r.rulesPath)
	if errors.Is(err, os.ErrNotExist) {
		return fallbackRules()
	}
	if err != nil {
		return RuleSet{Source: r.rulesPath, Loaded: false, Summary: []string{"Rules file could not be read."}}
	}
	summary := summarizeRules(string(data))
	if len(summary) == 0 {
		summary = fallbackRules().Summary
	}
	return RuleSet{Source: r.rulesPath, Loaded: true, Summary: summary}
}

func summarizeRules(content string) []string {
	lines := strings.Split(content, "\n")
	summary := make([]string, 0, 6)
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "local-first") ||
			strings.Contains(lower, "only stop for") ||
			strings.Contains(lower, "never commit secrets") ||
			strings.Contains(lower, "prefer simple") ||
			strings.Contains(lower, "run relevant tests") ||
			strings.Contains(lower, "destructive actions") {
			summary = append(summary, strings.TrimSuffix(line, "."))
		}
		if len(summary) == 6 {
			break
		}
	}
	return summary
}

func fallbackRules() RuleSet {
	return RuleSet{
		Source: "built-in",
		Loaded: true,
		Summary: []string{
			"Local-first",
			"Ask before destructive actions",
			"Use environment variables for secrets",
			"Prefer simple solutions",
			"Run checks after changes",
		},
	}
}

func defaultTools() []Tool {
	return []Tool{
		{ID: "read_file", Name: "Read files", Access: "workspace", Approval: "not required"},
		{ID: "search_files", Name: "Search files", Access: "workspace", Approval: "not required"},
		{ID: "edit_file", Name: "Edit files", Access: "workspace", Approval: "required by boundary"},
		{ID: "run_command", Name: "Run commands", Access: "allowlist", Approval: "required by boundary"},
		{ID: "diagnostics", Name: "Read diagnostics", Access: "workspace", Approval: "not required"},
	}
}

func defaultHooks() []Hook {
	return []Hook{
		{ID: "before_tool", Event: "Before tool calls", State: "planned"},
		{ID: "after_edit", Event: "After file edits", State: "planned"},
		{ID: "before_commit", Event: "Before commits", State: "planned"},
		{ID: "after_check", Event: "After checks", State: "planned"},
	}
}

func defaultSkills() []Skill {
	return []Skill{
		{ID: "debug_test", Name: "Debug failing test", State: "planned"},
		{ID: "review_change", Name: "Review change", State: "planned"},
		{ID: "update_docs", Name: "Update docs", State: "planned"},
	}
}

func defaultBoundaries() []string {
	return []string{
		"No destructive action without approval",
		"No billing action",
		"No secrets in logs or commits",
		"No broad system access",
		"No background autonomous jobs",
	}
}

func defaultNext() []string {
	return []string{
		"Add read-only workspace tools",
		"Add approval-gated edits",
		"Add hook execution",
	}
}

func newTraceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "")
	}
	return hex.EncodeToString(b[:])
}
