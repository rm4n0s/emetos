package git

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// AddRemote adds a remote ("git remote add <name> <url>"). A name or URL that is empty or starts with a dash is refused with the tag
// GitRemoteInvalid before git is started, and a name that exists is refused by git ("remote origin already exists").
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.addRemote)
func (r *Repo) AddRemote(ctx context.Context, name, remoteURL string) error {
	if err := remoteArg(name); err != nil {
		return err
	}
	if err := remoteArg(remoteURL); err != nil {
		return err
	}
	_, err := r.write(ctx, ScopeConfig, Command{Args: []string{"remote", "add", "--", name, remoteURL}})
	return err
}

// RemoveRemote removes a remote and its remote-tracking branches ("git remote remove <name>").
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.removeRemote)
func (r *Repo) RemoveRemote(ctx context.Context, name string) error {
	if err := remoteArg(name); err != nil {
		return err
	}
	_, err := r.write(ctx, ScopeConfig|ScopeRefs, Command{Args: []string{"remote", "remove", "--", name}})
	return err
}

// reScpLike is the form "user@host:path" or "host:path" that git reads as ssh; the part before the colon has no slash.
var reScpLike = regexp.MustCompile(`^(?:[A-Za-z0-9._~-]+@)?(?:\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9._-]+):[^\s]`)

// ValidateRemoteURL checks the URL or path a remote is added with and says what is wrong with it ("" when it is fine): a path (absolute,
// relative or starting with ~, and it may have spaces), a URL with the scheme file, http, https, ssh or git and a host (a file URL needs
// a path), or an scp-like address (user@host:path). It starts no process and touches no file.
func ValidateRemoteURL(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "Provide the URL or the path of the repository"
	case strings.HasPrefix(s, "-"):
		return "The URL cannot start with a dash"
	case strings.ContainsAny(s, "\x00\r\n\t"):
		return "The URL cannot contain control characters"
	case strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "~"):
		return ""
	}
	if scheme, rest, found := strings.Cut(s, "://"); found {
		switch strings.ToLower(scheme) {
		case "http", "https", "ssh", "git":
		case "file":
			if !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "localhost/") {
				return "A file URL needs an absolute path (file:///path/to/repository.git)"
			}
			return ""
		default:
			return "The scheme " + scheme + " is not one git can fetch from here (file, http, https, ssh, git)"
		}
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return "The URL needs a host"
		}
		if strings.ContainsAny(s, " ") {
			return "The URL cannot contain spaces"
		}
		return ""
	}
	if strings.ContainsAny(s, " ") {
		return "The URL cannot contain spaces"
	}
	if reScpLike.MatchString(s) {
		return ""
	}
	// a plain name is a relative path ("../repo.git" is covered above, "repo.git" is a folder of the current directory)
	if !strings.Contains(s, ":") {
		return ""
	}
	return "This is not a URL git can use (https://host/path.git, ssh://user@host/path.git, user@host:path.git or a path)"
}
