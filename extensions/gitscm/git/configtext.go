package git

import (
	"regexp"
	"strings"
)

// configSection is a [section "subsection"] of a git configuration file with its properties.
type configSection struct {
	name       string
	subSection string
	properties map[string]string
}

var (
	configSectionPattern  = regexp.MustCompile(`^\s*\[\s*([^\]]+?)\s*("[^"]+")*\]\s*$`)
	configPropertyPattern = regexp.MustCompile(`^\s*(\w+)\s*=\s*"?([^"]+)"?$`)
)

// parseConfigText reads the text of a git configuration file (.gitmodules, .git/config) the way VS Code does: sections and
// "key = value" lines, the first value of a repeated key, no includes, no escapes. It is deliberately forgiving; a section
// line that is not closed is not a section.
//
// Ported from VS Code: extensions/git/src/git.ts (GitConfigParser).
func parseConfigText(raw string) []configSection {
	var sections []configSection
	current := configSection{name: "DEFAULT", properties: map[string]string{}}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if m := configSectionPattern.FindStringSubmatch(line); m != nil {
			sections = append(sections, current)
			current = configSection{name: m[1], subSection: strings.ReplaceAll(m[2], `"`, ""), properties: map[string]string{}}
			continue
		}
		if m := configPropertyPattern.FindStringSubmatch(line); m != nil {
			if _, ok := current.properties[m[1]]; !ok {
				current.properties[m[1]] = m[2]
			}
		}
	}
	return append(sections, current)
}

// Submodule is an entry of .gitmodules.
type Submodule struct {
	Name, Path, URL string
}

// ParseGitmodules reads the text of a .gitmodules file: the [submodule "name"] sections that have both a path and a url.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitmodules).
func ParseGitmodules(raw string) []Submodule {
	var out []Submodule
	for _, s := range parseConfigText(raw) {
		if s.name == "submodule" && s.subSection != "" && s.properties["path"] != "" && s.properties["url"] != "" {
			out = append(out, Submodule{Name: s.subSection, Path: s.properties["path"], URL: s.properties["url"]})
		}
	}
	return out
}

// Remote is a configured remote.
type Remote struct {
	Name string
	// FetchURL and PushURL are the URLs; PushURL is the fetch URL when no pushurl is set.
	FetchURL, PushURL string
	// ReadOnly says that pushing is disabled (the push URL is "no_push", or there is none).
	ReadOnly bool
}

// ParseRemotesConfig reads the [remote "name"] sections of the text of a .git/config file; it is the fast path that needs
// no git process.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitRemotes).
func ParseRemotesConfig(raw string) []Remote {
	var out []Remote
	for _, s := range parseConfigText(raw) {
		if s.name != "remote" || s.subSection == "" {
			continue
		}
		r := Remote{Name: s.subSection, FetchURL: s.properties["url"], PushURL: s.properties["url"]}
		if p, ok := s.properties["pushurl"]; ok {
			r.PushURL = p
		}
		r.ReadOnly = r.PushURL == "" || r.PushURL == "no_push"
		out = append(out, r)
	}
	return out
}

// ParseRemoteVerbose reads the output of "git remote -v": lines "name<TAB>url (fetch)" and "name<TAB>url (push)".
//
// Ported from VS Code: extensions/git/src/git.ts (getRemotesGit).
func ParseRemoteVerbose(out string) []Remote {
	var remotes []Remote
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, rest, ok := strings.Cut(line, "\t")
		if !ok || name == "" {
			continue
		}
		var kind string
		url := rest
		switch {
		case strings.HasSuffix(rest, " (fetch)"):
			kind, url = "fetch", strings.TrimSuffix(rest, " (fetch)")
		case strings.HasSuffix(rest, " (push)"):
			kind, url = "push", strings.TrimSuffix(rest, " (push)")
		}
		i := -1
		for j := range remotes {
			if remotes[j].Name == name {
				i = j
			}
		}
		if i < 0 {
			remotes = append(remotes, Remote{Name: name})
			i = len(remotes) - 1
		}
		switch kind {
		case "fetch":
			remotes[i].FetchURL = url
		case "push":
			remotes[i].PushURL = url
		default:
			remotes[i].FetchURL, remotes[i].PushURL = url, url
		}
	}
	for i := range remotes {
		remotes[i].ReadOnly = remotes[i].PushURL == "" || remotes[i].PushURL == "no_push"
	}
	return remotes
}
