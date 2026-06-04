package agent

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
	}
	return Skill{ID: skillIDFromFile(filepath.Base(path)), Name: name}
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
