package terminal

import (
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The environment the other extensions add to a shell (SPEC §10.25 "The process", §10.30 "Passwords and editors", step 24.23): VS Code's
// environment variable collections of extensions, in the one form Emetos has, `api.TerminalEnvironment()`. The contributions are read when
// a terminal starts and replace the variable of the same name; a terminal that runs keeps the environment it started with.

// applyContributions puts the variables of the contributions into an environment, the contributions in the order they were given and the
// variables of one in the order of their names, so that a later contribution wins and the environment is the same every time.
func applyContributions(env []string, list []extension.TerminalContribution) []string {
	for _, c := range list {
		names := make([]string, 0, len(c.Variables))
		for name := range c.Variables {
			names = append(names, name)
		}
		sort.Strings(names)
		add := make([]string, len(names))
		for i, name := range names {
			add[i] = name + "=" + c.Variables[name]
		}
		env = setEnv(env, add)
	}
	return env
}

// environmentHover is the lines of a tab's hover about the extensions that changed the environment of the terminal, VS Code's
// EnvironmentVariableInfoChangesActive: who, and what they said it enables. It is empty for a terminal no extension touched. The values of
// the variables are not shown (a token is one).
func environmentHover(list []extension.TerminalContribution) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nThe following extensions have contributed to this terminal's environment:")
	for _, c := range list {
		name := c.Name
		if name == "" {
			name = c.Extension
		}
		b.WriteString("\n- " + name)
		if c.Description != "" {
			b.WriteString(": " + c.Description)
		}
	}
	return b.String()
}
