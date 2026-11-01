package git

import (
	"context"
	"strings"
)

// varName returns the name of an environment entry "NAME=value".
func varName(kv string) string {
	name, _, _ := strings.Cut(kv, "=")
	return name
}

// removeVars returns env without the entries with the given names.
func removeVars(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, n := range names {
			if varName(kv) == n {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// withVars returns env with the entries of more added; an entry replaces one with the same name (the last of more wins).
func withVars(env []string, more ...string) []string {
	out := append([]string(nil), env...)
	index := make(map[string]int, len(out)+len(more))
	for i, kv := range out {
		index[varName(kv)] = i
	}
	for _, kv := range more {
		name := varName(kv)
		if i, ok := index[name]; ok {
			out[i] = kv
			continue
		}
		index[name] = len(out)
		out = append(out, kv)
	}
	return out
}

// subcommand finds the git command in args, skipping the global options that come before it ("-c key=value", "-C dir",
// "--no-pager" and the like). It returns the name and its index, or "" and -1.
func subcommand(args []string) (string, int) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-c" || a == "-C" || a == "--git-dir" || a == "--work-tree" || a == "--namespace" || a == "--exec-path":
			i++ // the option takes a value
		case strings.HasPrefix(a, "-"):
		default:
			return a, i
		}
	}
	return "", -1
}

// commandsWithDiffOptions are the commands that take --no-ext-diff and --no-textconv; a background command (SPEC §10.30
// "Running git") gets both, so that a status refresh or a blame never starts a program a repository configured.
var commandsWithDiffOptions = map[string]bool{
	"diff": true, "log": true, "show": true, "blame": true, "diff-tree": true, "diff-index": true, "diff-files": true,
}

// commandsWithLogEncoding are the commands that get -c i18n.logOutputEncoding=UTF-8, so that commit messages are UTF-8
// whatever the user's i18n settings.
var commandsWithLogEncoding = map[string]bool{"log": true, "show": true, "blame": true}

// commandsWithPathspecFile are the commands that read paths from a file (--pathspec-from-file).
var commandsWithPathspecFile = map[string]bool{
	"add": true, "rm": true, "reset": true, "restore": true, "checkout": true, "commit": true, "stash": true,
}

// MaxPathsOnTheCommandLine is the number of paths past which a command that supports it gets them on its standard input
// (--pathspec-from-file=- --pathspec-file-nul) instead of after "--".
const MaxPathsOnTheCommandLine = 200

// environ builds the environment of a command of the Runner (SPEC §10.30 "Environment"): the base environment without
// LC_ALL (so the user's LC_CTYPE stays but git's messages are English), plus GIT_TERMINAL_PROMPT=0, GIT_PAGER=cat,
// LC_MESSAGES=C, LANGUAGE=en and EMETOS_GIT_COMMAND; a background command also gets GIT_OPTIONAL_LOCKS=0. The Extra variables
// of the Config and those of its ExtraFor (the askpass and editor programs) follow, and the variables of the command itself come last.
func (r *Runner) environ(ctx context.Context, name string, c Command) []string {
	// GIT_DIR and GIT_WORK_TREE of the environment of Emetos would send the command to another repository, past the trust check
	env := removeVars(r.base(), "LC_ALL", "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR")
	env = withVars(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"LC_MESSAGES=C",
		"LANGUAGE=en",
		"EMETOS_GIT_COMMAND="+name,
	)
	if c.Background {
		env = withVars(env, "GIT_OPTIONAL_LOCKS=0")
	}
	env = withVars(env, r.cfg.Extra...)
	if r.cfg.ExtraFor != nil {
		env = withVars(env, r.cfg.ExtraFor(ctx, name, c.Background)...)
	}
	return withVars(env, c.Env...)
}

// arguments builds the argument list of a command: the global options for a background command and for the commands that
// want UTF-8 messages, the command with its options, --no-ext-diff and --no-textconv for a background command that takes
// them, and last the paths (after "--", or on the standard input past MaxPathsOnTheCommandLine for the commands that read a
// pathspec file). It returns the arguments and the paths to write on the standard input, if any.
func arguments(name string, idx int, c Command) (argv []string, pathInput []byte) {
	if c.Background {
		argv = append(argv, "-c", "core.fsmonitor=false", "-c", "core.pager=cat")
	}
	if commandsWithLogEncoding[name] {
		argv = append(argv, "-c", "i18n.logOutputEncoding=UTF-8")
	}
	if idx < 0 {
		argv = append(argv, c.Args...)
	} else {
		argv = append(argv, c.Args[:idx+1]...)
		if c.Background && commandsWithDiffOptions[name] {
			argv = append(argv, "--no-ext-diff", "--no-textconv")
		}
		argv = append(argv, c.Args[idx+1:]...)
	}
	switch {
	case len(c.Paths) == 0:
	case len(c.Paths) > MaxPathsOnTheCommandLine && commandsWithPathspecFile[name]:
		argv = append(argv, "--pathspec-from-file=-", "--pathspec-file-nul")
		var b strings.Builder
		for _, p := range c.Paths {
			b.WriteString(p)
			b.WriteByte(0)
		}
		pathInput = []byte(b.String())
	default:
		argv = append(argv, "--")
		argv = append(argv, c.Paths...)
	}
	return argv, pathInput
}

// SplitInChunks splits paths into groups whose total length is at most maxChunkLength (a path longer than that gets a group
// of its own), for the commands that cannot read their paths from a file.
//
// Ported from VS Code: extensions/git/src/util.ts (splitInChunks).
func SplitInChunks(paths []string, maxChunkLength int) [][]string {
	var chunks [][]string
	var current []string
	length := 0
	for _, p := range paths {
		newLength := length + len(p)
		if newLength > maxChunkLength && len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			newLength = len(p)
		}
		current = append(current, p)
		length = newLength
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}
