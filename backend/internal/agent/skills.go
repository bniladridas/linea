package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

func WithSkillsDir(dir string) func(*Runtime) {
	return func(r *Runtime) {
		r.skillsDir = strings.TrimSpace(dir)
	}
}

func (r *Runtime) skills(ctx context.Context) []Skill {
	if strings.TrimSpace(r.skillsDir) == "" {
		return defaultSkills()
	}
	select {
	case <-ctx.Done():
		return []Skill{{ID: "skills", Name: "Skills", State: "unavailable"}}
	default:
	}
	entries, err := os.ReadDir(r.skillsDir)
	if err != nil {
		return []Skill{{ID: "skills", Name: "Skills", State: "unavailable"}}
	}
	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		skill := readSkillFile(filepath.Join(r.skillsDir, entry.Name()))
		if skill.ID == "" {
			skill.ID = skillIDFromFile(entry.Name())
		}
		if skill.Name == "" {
			skill.Name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		skill.State = "ready"
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].ID < skills[j].ID
	})
	if len(skills) == 0 {
		return []Skill{{ID: "skills", Name: "Skills", State: "empty"}}
	}
	return skills
}

func readSkillFile(path string) Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}
	}
	lines := strings.Split(string(data), "\n")
	var name string
	var command string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "command") {
			command = strings.TrimSpace(value)
		}
	}
	return Skill{ID: skillIDFromFile(filepath.Base(path)), Name: name, Command: command}
}

func (r *Runtime) findSkill(ctx context.Context, id string) (Skill, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Skill{}, false
	}
	for _, skill := range r.skills(ctx) {
		if skill.ID == id {
			return skill, true
		}
	}
	return Skill{}, false
}

func (r *Runtime) RunSkill(ctx context.Context, skillID string, input SkillExecutionInput) (SkillExecution, error) {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return SkillExecution{}, errors.New("Skill ID is required.")
	}
	skill, ok := r.findSkill(ctx, skillID)
	if !ok {
		return SkillExecution{}, errors.New("Unknown skill ID.")
	}
	if skill.State != "ready" {
		return SkillExecution{}, errors.New("Skill is not ready.")
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		command = strings.TrimSpace(skill.Command)
	}
	if command == "" {
		run, err := r.addSkillRun(skillID, "completed", input.Detail)
		if err != nil {
			return SkillExecution{}, err
		}
		return SkillExecution{SkillRun: run}, nil
	}
	commandRun, err := r.RunCommand(ctx, CommandCheckInput{Command: command})
	state := "completed"
	if err != nil {
		state = "blocked"
	} else if commandRun.ExitCode != 0 {
		state = "failed"
	}
	detail := input.Detail
	if strings.TrimSpace(detail) == "" {
		detail = command
	}
	skillRun, runErr := r.addSkillRun(skillID, state, detail)
	if runErr != nil {
		return SkillExecution{}, runErr
	}
	if err != nil {
		return SkillExecution{SkillRun: skillRun}, err
	}
	return SkillExecution{SkillRun: skillRun, CommandRun: &commandRun}, nil
}

func (r *Runtime) addSkillRun(skillID string, state string, detail string) (SkillRun, error) {
	skillID = strings.TrimSpace(skillID)
	state = strings.TrimSpace(state)
	if skillID == "" || state == "" {
		return SkillRun{}, errors.New("Skill ID and state are required.")
	}
	detail = strings.TrimSpace(detail)
	if len([]rune(detail)) > 240 {
		detail = string([]rune(detail)[:240])
	}
	run := SkillRun{
		ID:        newTraceID(),
		SkillID:   skillID,
		State:     state,
		Detail:    detail,
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skillRuns = append([]SkillRun{run}, r.skillRuns...)
	if len(r.skillRuns) > 50 {
		r.skillRuns = r.skillRuns[:50]
	}
	return run, nil
}

func skillIDFromFile(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.ToLower(strings.TrimSpace(base))
	var out strings.Builder
	lastUnderscore := false
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			out.WriteByte('_')
			lastUnderscore = true
		}
	}
	id := strings.Trim(out.String(), "_")
	if id == "" {
		return "skill"
	}
	return id
}
