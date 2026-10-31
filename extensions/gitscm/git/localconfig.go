package git

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// MaxConfigSize bounds a configuration file that Emetos reads itself: 1 MiB.
const MaxConfigSize = 1 << 20

// ParseConfigFile reads the text of a git configuration file in git's syntax (git help config) and returns its settings in the order
// they are written: sections "[core]", "[remote "origin"]" and the deprecated "[remote.origin]", keys with or without "=", values
// in double quotes with the escapes \" \\ \n \t \b, comments with # and ;, a backslash at the end of a line continuing the value.
// Section and key names are lower case in the result (they are case-insensitive), a subsection keeps its case. Includes
// ("[include]", "[includeIf ...]") are not followed: this reader runs nothing and reads one file. A line it cannot understand is
// skipped, as git would stop at it.
func ParseConfigFile(text string) []ConfigEntry {
	var out []ConfigEntry
	section := ""
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			name, rest, ok := parseSectionHeader(line)
			if !ok {
				section = ""
				continue
			}
			section = name
			line = strings.TrimSpace(rest)
			if line == "" || line[0] == '#' || line[0] == ';' {
				continue
			}
		}
		if section == "" {
			continue
		}
		name, rest := splitKey(line)
		if name == "" {
			continue
		}
		key := section + "." + strings.ToLower(name)
		rest = strings.TrimSpace(rest)
		if rest == "" || rest[0] == '#' || rest[0] == ';' {
			out = append(out, ConfigEntry{Key: key})
			continue
		}
		if rest[0] != '=' {
			continue
		}
		value := strings.TrimLeft(rest[1:], " \t")
		// a value that ends with a backslash continues on the next line
		for continues(value) && i+1 < len(lines) {
			i++
			value = value[:len(value)-1] + lines[i]
		}
		out = append(out, ConfigEntry{Key: key, Value: parseValue(value), HasValue: true})
	}
	return out
}

// parseSectionHeader reads "[name]", "[name "sub"]" or "[name.sub]" at the start of a line and returns the key prefix
// ("name" or "name.sub") and the rest of the line.
func parseSectionHeader(line string) (prefix, rest string, ok bool) {
	end := -1
	inQuote := false
	for i := 1; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && inQuote:
			i++
		case c == '"':
			inQuote = !inQuote
		case c == ']' && !inQuote:
			end = i
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return "", "", false
	}
	head := strings.TrimSpace(line[1:end])
	name, sub, quoted := strings.Cut(head, " ")
	if !quoted {
		// "[section]" or "[section.subsection]": the part after the first dot is the subsection, in the case written
		sec, subsec, dotted := strings.Cut(head, ".")
		if dotted {
			return strings.ToLower(sec) + "." + subsec, line[end+1:], true
		}
		return strings.ToLower(head), line[end+1:], true
	}
	sub = strings.TrimSpace(sub)
	if len(sub) < 2 || sub[0] != '"' || sub[len(sub)-1] != '"' {
		return "", "", false
	}
	var b strings.Builder
	inner := sub[1 : len(sub)-1]
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		b.WriteByte(inner[i])
	}
	return strings.ToLower(name) + "." + b.String(), line[end+1:], true
}

// splitKey splits "name = value" into the name and what follows it.
func splitKey(line string) (name, rest string) {
	i := 0
	for i < len(line) {
		c := line[i]
		if c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			i++
			continue
		}
		break
	}
	return line[:i], line[i:]
}

// continues says whether a value ends with an odd number of backslashes outside nothing else: a line continuation.
func continues(value string) bool {
	n := 0
	for i := len(value) - 1; i >= 0 && value[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// parseValue unquotes a value: double quotes group text, backslash escapes, an unquoted # or ; starts a comment, and white space at
// the end of an unquoted part is dropped.
func parseValue(v string) string {
	var b strings.Builder
	inQuote := false
	trailing := 0 // unquoted white space written at the end, dropped if nothing follows
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\\' && i+1 < len(v):
			i++
			switch v[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			default:
				b.WriteByte(v[i])
			}
			trailing = 0
		case c == '"':
			inQuote = !inQuote
			trailing = 0
		case !inQuote && (c == '#' || c == ';'):
			s := b.String()
			return s[:len(s)-trailing]
		case !inQuote && (c == ' ' || c == '\t'):
			b.WriteByte(c)
			trailing++
		default:
			b.WriteByte(c)
			trailing = 0
		}
	}
	s := b.String()
	return s[:len(s)-trailing]
}

// readConfigFile reads one file of at most MaxConfigSize bytes; a file that is missing is no file.
func readConfigFile(path string) (text string, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, errors.NewErr("GitConfigReadFailed", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }() // a read only file: closing cannot lose anything
	data, err := io.ReadAll(io.LimitReader(f, MaxConfigSize+1))
	if err != nil {
		return "", false, errors.NewErr("GitConfigReadFailed", err).SetMetadata("path", path)
	}
	if len(data) > MaxConfigSize {
		return "", false, errors.New("GitConfigTooLarge", "the configuration file is larger than Emetos reads", "path", path, "limit", MaxConfigSize)
	}
	return string(data), true, nil
}

// ReadLocalConfig reads the configuration of the repository itself, without running anything (SPEC §10.30 "Trusting a repository"): the
// config of its git directory (of the common directory for a linked worktree, which shares it) and config.worktree. The
// settings come in file order; the global and system files are not read. A file that does not exist is empty, one that is too large
// fails with GitConfigTooLarge and one that cannot be read with GitConfigReadFailed.
func ReadLocalConfig(loc Location) ([]ConfigEntry, error) {
	common := loc.GitDir
	if data, found, err := readConfigFile(filepath.Join(loc.GitDir, "commondir")); err == nil && found {
		if rel := strings.TrimSpace(data); rel != "" {
			common = absolute(loc.GitDir, rel)
		}
	}
	var out []ConfigEntry
	for _, path := range []string{filepath.Join(common, "config"), filepath.Join(loc.GitDir, "config.worktree")} {
		text, found, err := readConfigFile(path)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, ParseConfigFile(text)...)
		}
	}
	return out, nil
}

// Program is a setting of a repository's configuration that names a program git can run.
type Program struct {
	// Key is the setting as it reads in a configuration file ("core.sshCommand", "alias.st", "filter.lfs.clean").
	Key string
	// Value is the command.
	Value string
}

// Programs lists the settings of a repository's configuration that name programs git can run, in the order written (SPEC §10.30
// "Trusting a repository"): core.fsmonitor other than a boolean, core.sshCommand, core.hooksPath, core.pager, core.editor,
// credential.helper starting with "!" or "/", diff.external, filter.*.clean and .smudge, diff.*.textconv and .command, and
// alias.* starting with "!". Keys are matched without regard to case; an empty value names nothing.
func Programs(entries []ConfigEntry) []Program {
	var out []Program
	for _, e := range entries {
		if !e.HasValue || strings.TrimSpace(e.Value) == "" {
			continue // a key with no value is a boolean true, and an empty value names nothing
		}
		section, rest, _ := strings.Cut(e.Key, ".")
		sub, name := "", rest
		if i := strings.LastIndex(rest, "."); i >= 0 {
			sub, name = rest[:i], rest[i+1:]
		}
		named := false
		switch section {
		case "core":
			switch name {
			case "fsmonitor":
				named = sub == "" && !isBoolean(e.Value)
			case "sshcommand", "hookspath", "pager", "editor":
				named = sub == ""
			}
		case "credential":
			named = name == "helper" && (strings.HasPrefix(e.Value, "!") || strings.HasPrefix(e.Value, "/"))
		case "diff":
			named = (sub == "" && name == "external") || (sub != "" && (name == "textconv" || name == "command"))
		case "filter":
			named = sub != "" && (name == "clean" || name == "smudge")
		case "alias":
			named = sub == "" && strings.HasPrefix(strings.TrimSpace(e.Value), "!")
		}
		if named {
			out = append(out, Program{Key: displayKey(e.Key), Value: e.Value})
		}
	}
	return out
}

// displayKey writes a lower-cased key the way git's documentation writes it: core.sshcommand is core.sshCommand. The other keys
// Programs lists are all lower case in the documentation, with the subsection in the case written.
func displayKey(key string) string {
	switch key {
	case "core.sshcommand":
		return "core.sshCommand"
	case "core.hookspath":
		return "core.hooksPath"
	}
	return key
}

// isBoolean says whether a value is one of git's spellings of true or false.
func isBoolean(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "false", "yes", "no", "on", "off", "1", "0", "":
		return true
	}
	return false
}

// RemotesFromConfig lists the remotes of a repository's configuration (remote.<name>.url and .pushurl) from the settings
// ReadLocalConfig returned.
func RemotesFromConfig(entries []ConfigEntry) []Remote {
	var out []Remote
	index := map[string]int{}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Key, "remote.")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, ".")
		if i <= 0 {
			continue
		}
		name, key := rest[:i], rest[i+1:]
		if key != "url" && key != "pushurl" {
			continue
		}
		at, seen := index[name]
		if !seen {
			at = len(out)
			index[name] = at
			out = append(out, Remote{Name: name})
		}
		switch key {
		case "url":
			out[at].FetchURL = e.Value
			if out[at].PushURL == "" {
				out[at].PushURL = e.Value
			}
		case "pushurl":
			out[at].PushURL = e.Value
		}
	}
	for i := range out {
		out[i].ReadOnly = out[i].PushURL == "no_push"
	}
	return out
}
