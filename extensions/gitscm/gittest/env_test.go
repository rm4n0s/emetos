package gittest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// poison sets variables of the user's environment that a test of the harness must not see in git: every one of them would
// change what git does if it leaked.
func poison(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Mallory\n\temail = mallory@example.org\n[alias]\n\tleak = status\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	xdg := filepath.Join(home, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), []byte("[user]\n\tname = Mallory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": xdg,
		"GIT_AUTHOR_NAME": "Mallory", "GIT_COMMITTER_NAME": "Mallory", "GIT_AUTHOR_EMAIL": "mallory@example.org",
		"GIT_DIR": "/nonexistent-git-dir", "GIT_WORK_TREE": "/nonexistent-work-tree", "GIT_INDEX_FILE": "/nonexistent-index",
		"GIT_SSH_COMMAND": "/usr/bin/ssh", "GIT_ASKPASS": "/usr/bin/true", "GIT_CONFIG_GLOBAL": filepath.Join(home, ".gitconfig"),
		"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "user.name", "GIT_CONFIG_VALUE_0": "Mallory",
		"GIT_EXEC_PATH": "/nonexistent-exec-path", "SSH_AUTH_SOCK": "/nonexistent/agent.sock", "GIT_TRACE": "1",
		"http_proxy": "http://203.0.113.1:3128", "HTTPS_PROXY": "http://203.0.113.1:3128", "GPG_TTY": "/dev/tty",
	} {
		t.Setenv(k, v)
	}
}

func TestTheHarnessHidesTheUsersGitConfiguration(t *testing.T) {
	poison(t)
	e := NewEnv(t)
	r := e.Init("repo")

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"global list is empty", []string{"config", "--global", "-l"}, ""},
		{"system list is empty", []string{"config", "--system", "-l"}, ""},
		{"the user's name is Ada, not Mallory", []string{"config", "user.name"}, AuthorName},
		{"the user's alias is unknown", []string{"config", "--get", "alias.leak"}, "FAIL"},
		{"the identity of a commit", []string{"var", "GIT_AUTHOR_IDENT"}, AuthorName + " <" + AuthorEmail + "> 1704110400 +0000"},
		{"the default branch", []string{"symbolic-ref", "--short", "HEAD"}, "main"},
		{"protocol.file.allow", []string{"config", "protocol.file.allow"}, "always"},
		{"core.hooksPath is an empty folder", []string{"config", "core.hooksPath"}, e.HooksDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, err := r.GitErr(tc.args...)
			if tc.want == "FAIL" {
				if err == nil {
					t.Fatalf("git %v succeeded with %q, want a failure", tc.args, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("git %v: %v\n%s", tc.args, err, errOut)
			}
			if out != tc.want {
				t.Errorf("git %v = %q, want %q", tc.args, out, tc.want)
			}
		})
	}

	// Every file that git reads for configuration is inside the world.
	out := r.Git("config", "--list", "--show-origin", "--show-scope")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Mallory") || strings.Contains(line, "mallory") {
			t.Errorf("a setting of the user leaked: %q", line)
		}
		if strings.HasPrefix(line, "global") || strings.HasPrefix(line, "system") {
			t.Errorf("a global or system setting is present: %q", line)
		}
	}
	for _, f := range []string{e.GlobalConfig, e.SystemConfig} {
		data, err := os.ReadFile(f)
		if err != nil || len(data) != 0 {
			t.Errorf("%s is not an empty file: %v %q", f, err, data)
		}
	}
}

func TestNoVariableOfTheUsersEnvironmentLeaks(t *testing.T) {
	poison(t)
	e := NewEnv(t)
	vars := e.Vars()
	byName := map[string]string{}
	for _, kv := range vars {
		name, value, _ := strings.Cut(kv, "=")
		if _, dup := byName[name]; dup {
			t.Errorf("%s is set twice", name)
		}
		byName[name] = value
	}
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "TMPDIR": true, "TZ": true, "LC_CTYPE": true, "LC_MESSAGES": true, "LANGUAGE": true,
	}
	for name, value := range byName {
		switch {
		case allowed[name], strings.HasPrefix(name, "XDG_"):
		case strings.HasPrefix(name, "GIT_"), strings.HasPrefix(name, "SSH_"):
		default:
			t.Errorf("unexpected variable %s=%s", name, value)
		}
		if strings.Contains(value, "Mallory") || strings.Contains(value, "mallory") || strings.Contains(value, "nonexistent") || strings.Contains(value, "203.0.113.1") {
			t.Errorf("%s=%s comes from the user's environment", name, value)
		}
		if name != "PATH" && name != "GIT_CEILING_DIRECTORIES" && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, e.Root) && !strings.HasPrefix(value, "/usr") && value != "/dev/null" {
			t.Errorf("%s=%s points outside the world %s", name, value, e.Root)
		}
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_EXEC_PATH", "GIT_TRACE", "SSH_AUTH_SOCK", "http_proxy", "HTTPS_PROXY", "GPG_TTY", "GIT_CONFIG_COUNT"} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s of the user's environment leaked", name)
		}
	}
	for _, name := range []string{"GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CEILING_DIRECTORIES", "GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "SSH_ASKPASS", "HOME", "XDG_CONFIG_HOME"} {
		if byName[name] == "" {
			t.Errorf("%s is not set", name)
		}
	}
	if byName["GIT_TERMINAL_PROMPT"] != "0" || byName["GIT_CONFIG_NOSYSTEM"] != "1" {
		t.Errorf("GIT_TERMINAL_PROMPT=%q GIT_CONFIG_NOSYSTEM=%q", byName["GIT_TERMINAL_PROMPT"], byName["GIT_CONFIG_NOSYSTEM"])
	}
	if byName["GIT_CEILING_DIRECTORIES"] != filepath.Dir(e.Root) {
		t.Errorf("GIT_CEILING_DIRECTORIES = %q, want the parent of %s", byName["GIT_CEILING_DIRECTORIES"], e.Root)
	}
}

func TestGitFindsNoRepositoryInATempFolderInsideTheProject(t *testing.T) {
	e := NewEnv(t)
	// A temporary folder inside the project (the folder of this package), as a test that makes files next to its code does.
	tmp, err := os.MkdirTemp(".", "iso-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	abs, err := filepath.Abs(tmp)
	if err != nil {
		t.Fatal(err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	// Control: without the ceiling git walks up into the project's own repository, when the project is one.
	control := e.With("GIT_CEILING_DIRECTORIES=")
	projectIsARepository := false
	if _, _, err := control.GitErr(abs, "rev-parse", "--show-toplevel"); err == nil {
		projectIsARepository = true
	}
	// The harness: the ceiling is the parent of the temporary folder, so nothing above it is looked at.
	inside := e.With("GIT_CEILING_DIRECTORIES=" + filepath.Dir(abs))
	out, errOut, err := inside.GitErr(abs, "rev-parse", "--show-toplevel")
	if err == nil {
		t.Fatalf("git found a repository at %q from a temporary folder inside the project (control found one: %v)", out, projectIsARepository)
	}
	if !strings.Contains(strings.ToLower(errOut), "not a git repository") {
		t.Errorf("git failed for another reason: %s", errOut)
	}
	t.Logf("the project is a repository (the control found it): %v", projectIsARepository)
}

func TestTrustWritesTheRepositoryIntoTheIsolatedTrustFile(t *testing.T) {
	e := NewEnv(t)
	a, b := e.Init("a"), e.Init("b")
	again := e.Init("a2")
	e.Trust(a.Dir) // a path that is already there is kept once
	data, err := os.ReadFile(e.TrustFile())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version        int
		Trusted        []string
		TrustedParents []string
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%s: %v\n%s", e.TrustFile(), err, data)
	}
	want := []string{a.Dir, again.Dir, b.Dir}
	if got.Version != 1 || strings.Join(got.Trusted, ",") != strings.Join(want, ",") || got.TrustedParents == nil {
		t.Errorf("trust file = %+v, want version 1 and trusted %v", got, want)
	}
	if info, err := os.Stat(e.TrustFile()); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode of the trust file = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if !strings.HasPrefix(e.TrustFile(), e.StateHome) {
		t.Errorf("the trust file %s is not under the isolated state folder %s", e.TrustFile(), e.StateHome)
	}
}

func TestTrustFollowsTheProcessStateHome(t *testing.T) {
	e := NewEnv(t)
	r := e.Init("repo")
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	Trust(t, r.Dir)
	data, err := os.ReadFile(filepath.Join(state, "emetos", "git-trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), r.Dir) {
		t.Errorf("trust file does not name %s: %s", r.Dir, data)
	}
}

func TestTrustRefusesAStateHomeThatIsNotIsolated(t *testing.T) {
	e := NewEnv(t)
	r := e.Init("repo")
	tests := []struct {
		name  string
		state string
	}{
		{"not set", ""},
		{"outside the temporary folder", filepath.Join(filepath.Dir(filepath.Clean(os.TempDir())), "not-temp", "state")},
		{"the temporary folder itself", os.TempDir()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOTMPDIR", "")
			t.Setenv("XDG_STATE_HOME", tc.state)
			fake := &recordingTB{TB: t}
			func() {
				defer func() {
					if recover() == nil || !strings.Contains(fake.message, "XDG_STATE_HOME") {
						t.Errorf("Trust did not refuse %q: %q", tc.state, fake.message)
					}
				}()
				Trust(fake, r.Dir)
			}()
			if tc.state != "" {
				if _, err := os.Stat(filepath.Join(tc.state, "emetos", "git-trust.json")); err == nil {
					t.Errorf("a trust file was written under %s", tc.state)
				}
			}
		})
	}
}

func TestInsideTempAcceptsTheTemporaryFolderAndGOTMPDIR(t *testing.T) {
	gotmp := filepath.Join(filepath.Dir(filepath.Clean(os.TempDir())), "a-gotmpdir")
	t.Setenv("GOTMPDIR", gotmp)
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"inside the temporary folder", filepath.Join(os.TempDir(), "x", "state"), true},
		{"inside GOTMPDIR", filepath.Join(gotmp, "x", "state"), true},
		{"the temporary folder itself", os.TempDir(), false},
		{"GOTMPDIR itself", gotmp, false},
		{"elsewhere", filepath.Join(filepath.Dir(filepath.Clean(os.TempDir())), "home", "state"), false},
		{"a way out of the temporary folder", filepath.Join(os.TempDir(), "..", "state"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := insideTemp(tc.path); got != tc.want {
				t.Errorf("insideTemp(%s) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestEnvDatesAreFixedAndEachCommitGetsTheNextMinute(t *testing.T) {
	r := New(t)
	r.Write("a.txt", "a\n")
	first := r.Commit("one")
	r.Write("a.txt", "b\n")
	second := r.Commit("two")
	if first == second {
		t.Fatal("two commits have the same hash")
	}
	for _, tc := range []struct{ rev, want string }{
		{first, "2024-01-01T12:00:00Z"},
		{second, "2024-01-01T12:01:00Z"},
	} {
		if got := r.Git("log", "-1", "--format=%aI|%cI|%an|%ae", tc.rev); got != tc.want+"|"+tc.want+"|Ada Example|ada@example.com" {
			t.Errorf("commit %s: %q", tc.rev[:7], got)
		}
	}
	if r.Git("rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Error("the branch is not main")
	}
}

func TestRequireSkipsWithoutAUsableGit(t *testing.T) {
	// The harness's own Require is exercised by every other test; here the version parsing it relies on.
	for _, tc := range []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"git version 2.30.0", 2, 30, true},
		{"git version 2.43.0.windows.1", 2, 43, true},
		{"git version 2.30.1 (Apple Git-130)", 2, 30, true},
		{"git version 2.56.0.rc1", 2, 56, true},
		{"git version 1.9.5", 1, 9, true},
		{"not git", 0, 0, false},
	} {
		major, minor, ok := parseVersion(tc.in)
		if major != tc.major || minor != tc.minor || ok != tc.ok {
			t.Errorf("parseVersion(%q) = %d.%d %v, want %d.%d %v", tc.in, major, minor, ok, tc.major, tc.minor, tc.ok)
		}
	}
}
