package agent

import "strings"

func WithCommandAllowlist(commands []string) func(*Runtime) {
	return func(r *Runtime) {
		r.commands = normalizeCommands(commands)
	}
}

func normalizeCommands(commands []string) []string {
	out := make([]string, 0, len(commands))
	seen := map[string]bool{}
	for _, command := range commands {
		command = strings.Join(strings.Fields(command), " ")
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		out = append(out, command)
	}
	return out
}
