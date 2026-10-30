package gittest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// TB is the part of testing.TB that the harness uses. *testing.T and *testing.B satisfy it, and so does the small
// implementation of cmd/gitfixture, which has no test.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Skipf(format string, args ...any)
	Cleanup(func())
	TempDir() string
}

// MinVersion is the oldest git that Emetos runs (SPEC §10.30 "Running git", D35): 2.30.
var MinVersion = [2]int{2, 30}

// The identity of every commit a test makes, and the first date; each commit gets the next minute.
const (
	AuthorName  = "Ada Example"
	AuthorEmail = "ada@example.com"
)

// baseDate is the date of the first commit of an Env; Env.NextDate adds one minute per call.
var baseDate = time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC)

// Env is the isolated world of one test: the folders and the environment variables of every process the test starts.
// Create one with NewEnv, or get it from New.
type Env struct {
	// Root is the folder that holds the whole world (the real path of t.TempDir()).
	Root string
	// Home, ConfigHome, StateHome, CacheHome and DataHome are the folders HOME and the XDG variables point at. The
	// trust file of the Git extension lives in StateHome/emetos/git-trust.json.
	Home, ConfigHome, StateHome, CacheHome, DataHome string
	// GlobalConfig and SystemConfig are the empty files GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM name.
	GlobalConfig, SystemConfig string
	// HooksDir is the empty folder core.hooksPath names, so that no hook runs unless a test writes one (Repo.Hook).
	HooksDir string
	// BinDir holds the askpass and ssh stand-ins and the links of FakeGit.
	BinDir string
	// Work is the folder where the repositories of the test are made.
	Work string
	// CeilingDir is GIT_CEILING_DIRECTORIES: the parent of Root, so that git never walks up out of the world into another
	// repository (the project's own included).
	CeilingDir string
	// GitPath is the git program the test runs.
	GitPath string
	// Identity says whether the author and committer variables and user.name/user.email are set. It is true for an Env
	// from NewEnv; a test of the missing identity sets it false (git is then told not to guess one).
	Identity bool
	// Extra are variables ("KEY=VALUE") added last to Vars; a later entry wins over an earlier one.
	Extra []string

	t     TB
	mu    sync.Mutex
	ticks int
	seq   int
}

// Require returns the path of the git program, or skips the test when git is not found or is older than 2.30 (D36). It
// runs "git --version" in an isolated environment, outside any repository.
func Require(t TB) string {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is not installed: %v", err)
		return ""
	}
	out, err := runPlain(path, os.TempDir(), "--version")
	if err != nil {
		t.Skipf("git --version failed: %v", err)
		return ""
	}
	major, minor, ok := parseVersion(out)
	if !ok {
		t.Skipf("cannot read the version of git from %q", strings.TrimSpace(out))
		return ""
	}
	if major < MinVersion[0] || (major == MinVersion[0] && minor < MinVersion[1]) {
		t.Skipf("git %d.%d is older than %d.%d", major, minor, MinVersion[0], MinVersion[1])
		return ""
	}
	return path
}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)`)

// parseVersion reads "git version 2.43.0.windows.1" and the like.
func parseVersion(out string) (major, minor int, ok bool) {
	m := versionPattern.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

// NewEnv makes the world of a test under t.TempDir(): the folders, the empty configuration files and the stand-in programs.
// It skips the test when git is missing or too old (Require).
func NewEnv(t TB) *Env {
	t.Helper()
	gitPath := Require(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("gittest: resolving the temporary folder: %v", err)
	}
	e := &Env{
		Root:         root,
		Home:         filepath.Join(root, "home"),
		ConfigHome:   filepath.Join(root, "config"),
		StateHome:    filepath.Join(root, "state"),
		CacheHome:    filepath.Join(root, "cache"),
		DataHome:     filepath.Join(root, "data"),
		GlobalConfig: filepath.Join(root, "global.gitconfig"),
		SystemConfig: filepath.Join(root, "system.gitconfig"),
		HooksDir:     filepath.Join(root, "hooks"),
		BinDir:       filepath.Join(root, "bin"),
		Work:         filepath.Join(root, "work"),
		CeilingDir:   filepath.Dir(root),
		GitPath:      gitPath,
		Identity:     true,
		t:            t,
	}
	for _, d := range []string{e.Home, e.ConfigHome, e.StateHome, e.CacheHome, e.DataHome, e.HooksDir, e.BinDir, e.Work, filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("gittest: %v", err)
		}
	}
	for _, f := range []string{e.GlobalConfig, e.SystemConfig} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatalf("gittest: %v", err)
		}
	}
	e.writeScript("askpass", "#!/bin/sh\nexit 1\n")
	e.writeScript("ssh-denied", "#!/bin/sh\necho 'ssh is not available in tests' >&2\nexit 255\n")
	return e
}

func (e *Env) writeScript(name, body string) string {
	p := filepath.Join(e.BinDir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		e.t.Fatalf("gittest: %v", err)
	}
	return p
}

// Askpass is the path of the stand-in for GIT_ASKPASS and SSH_ASKPASS: it fails, which cancels the prompt.
func (e *Env) Askpass() string { return filepath.Join(e.BinDir, "askpass") }

// NextDate returns the date of the next commit: 2024-01-01 12:00:00 UTC for the first, and a minute later for each call
// after it, so that the hashes of a repository a test builds are the same in every run.
func (e *Env) NextDate() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	d := baseDate.Add(time.Duration(e.ticks) * time.Minute)
	e.ticks++
	return d
}

// With returns a copy of the Env with the variables ("KEY=VALUE") added at the end of its Extra list; the copy shares the folders and
// counts its own dates. It is for a test that needs one process to see something the others do not.
func (e *Env) With(vars ...string) *Env {
	e.mu.Lock()
	defer e.mu.Unlock()
	return &Env{
		Root: e.Root, Home: e.Home, ConfigHome: e.ConfigHome, StateHome: e.StateHome, CacheHome: e.CacheHome, DataHome: e.DataHome,
		GlobalConfig: e.GlobalConfig, SystemConfig: e.SystemConfig, HooksDir: e.HooksDir, BinDir: e.BinDir, Work: e.Work,
		CeilingDir: e.CeilingDir, GitPath: e.GitPath, Identity: e.Identity, Extra: append(append([]string(nil), e.Extra...), vars...),
		t: e.t, ticks: e.ticks, seq: e.seq,
	}
}

// SkipDates makes the next n calls of NextDate unused: the next commit is dated n minutes later (the fixtures put the commits they
// make with git commit after the ones they import with fast-import).
func (e *Env) SkipDates(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ticks += n
}

// ticksSoFar is how many dates NextDate and SkipDates used.
func (e *Env) ticksSoFar() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ticks
}

// UniqueName returns prefix followed by a number that no other call of this Env returned.
func (e *Env) UniqueName(prefix string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	return prefix + strconv.Itoa(e.seq)
}

// configEntries are the git settings of the Env, given to git in GIT_CONFIG_PARAMETERS (the variable that "git -c" fills
// for the processes it starts, understood by every git version), so that the global and the system files stay empty.
func (e *Env) configEntries() [][2]string {
	entries := [][2]string{
		{"init.defaultbranch", "main"},
		{"protocol.file.allow", "always"},
		{"core.fsmonitor", "false"},
		{"gc.auto", "0"},
		{"gc.autodetach", "false"},
		{"maintenance.auto", "false"},
		{"commit.gpgsign", "false"},
		{"tag.gpgsign", "false"},
		{"advice.detachedhead", "false"},
	}
	if e.Identity {
		entries = append(entries, [2]string{"user.name", AuthorName}, [2]string{"user.email", AuthorEmail})
	} else {
		entries = append(entries, [2]string{"user.useconfigonly", "true"})
	}
	return entries
}

// Vars returns the whole environment of a process of the world, made from scratch: only PATH is taken from the process
// that runs the test. Nothing named GIT_*, SSH_*, GPG_*, *_PROXY or XDG_* of the user's environment gets in.
func (e *Env) Vars() []string {
	var params []string
	for _, kv := range e.configEntries() {
		params = append(params, shellQuote(kv[0])+"="+shellQuote(kv[1]))
	}
	vars := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + e.Home,
		"XDG_CONFIG_HOME=" + e.ConfigHome,
		"XDG_STATE_HOME=" + e.StateHome,
		"XDG_CACHE_HOME=" + e.CacheHome,
		"XDG_DATA_HOME=" + e.DataHome,
		"TMPDIR=" + filepath.Join(e.Root, "tmp"),
		"TZ=UTC",
		"LC_CTYPE=C.UTF-8",
		"LC_MESSAGES=C",
		"LANGUAGE=en",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + e.GlobalConfig,
		"GIT_CONFIG_SYSTEM=" + e.SystemConfig,
		"GIT_CONFIG_PARAMETERS=" + strings.Join(params, " "),
		"GIT_CEILING_DIRECTORIES=" + e.CeilingDir,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=" + e.Askpass(),
		"SSH_ASKPASS=" + e.Askpass(),
		"SSH_ASKPASS_REQUIRE=force",
		"GIT_SSH_COMMAND=" + filepath.Join(e.BinDir, "ssh-denied"),
		"GIT_ALLOW_PROTOCOL=file:http",
		"GIT_EDITOR=true",
		"GIT_SEQUENCE_EDITOR=true",
		"GIT_PAGER=cat",
		"GIT_OPTIONAL_LOCKS=0",
	}
	if e.Identity {
		date := baseDate.Format("2006-01-02T15:04:05-0700")
		vars = append(vars,
			"GIT_AUTHOR_NAME="+AuthorName, "GIT_AUTHOR_EMAIL="+AuthorEmail, "GIT_AUTHOR_DATE="+date,
			"GIT_COMMITTER_NAME="+AuthorName, "GIT_COMMITTER_EMAIL="+AuthorEmail, "GIT_COMMITTER_DATE="+date)
	}
	return mergeVars(vars, e.Extra)
}

// shellQuote quotes a string the way git does for GIT_CONFIG_PARAMETERS: in single quotes, a quote as '\”.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// mergeVars returns base with the entries of more added; an entry of more replaces one of base with the same name.
func mergeVars(base []string, more []string) []string {
	out := append([]string(nil), base...)
	index := map[string]int{}
	for i, kv := range out {
		index[varName(kv)] = i
	}
	for _, kv := range more {
		if i, ok := index[varName(kv)]; ok {
			out[i] = kv
			continue
		}
		index[varName(kv)] = len(out)
		out = append(out, kv)
	}
	return out
}

func varName(kv string) string {
	name, _, _ := strings.Cut(kv, "=")
	return name
}

// Git runs git with args in dir, in the environment of the world, and returns its standard output without the final
// newline. The test fails when git fails.
func (e *Env) Git(dir string, args ...string) string {
	e.t.Helper()
	out, errOut, err := e.GitErr(dir, args...)
	if err != nil {
		e.t.Fatalf("gittest: git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, errOut)
	}
	return out
}

// GitErr is Git for a command that may fail: it returns the standard output and error output (both without the final
// newline) and the error, which is nil when git exited with status 0.
func (e *Env) GitErr(dir string, args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	return e.run(dir, nil, e.Vars(), args...)
}

// GitInput is GitErr with input on the standard input.
func (e *Env) GitInput(dir, input string, args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	return e.run(dir, strings.NewReader(input), e.Vars(), args...)
}

// run starts git under a deadline of one minute, in its own process group that is killed when the deadline passes.
func (e *Env) run(dir string, stdin *strings.Reader, env []string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.GitPath, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return strings.TrimSuffix(stdout.String(), "\n"), strings.TrimSuffix(stderr.String(), "\n"), err
}

// runPlain runs a program with an empty environment but PATH, for Require.
func runPlain(path, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0"}
	out, err := cmd.Output()
	return string(out), err
}

// trustFile is the trust file of the Git extension inside the state folder (SPEC §10.30 "Trusting a repository").
func trustFile(stateHome string) string {
	return filepath.Join(stateHome, "emetos", "git-trust.json")
}

// TrustFile is the path of the isolated trust file of the world.
func (e *Env) TrustFile() string { return trustFile(e.StateHome) }

// Trust writes the real path of the repository at path into the trust file of the world, so that a test of the Git
// extension does not meet the trust question unless it is about it (D37).
func (e *Env) Trust(path string) {
	e.t.Helper()
	trustIn(e.t, e.StateHome, path)
}

// Trust writes the real path of the repository at path into the trust file under $XDG_STATE_HOME, which the test must have
// isolated first (the helper of internal/core/storage does it). The test fails when XDG_STATE_HOME is not set or is not inside
// the temporary folder, so that a test that forgot to isolate its folders never writes into the user's real state.
func Trust(t TB, path string) {
	t.Helper()
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		t.Fatalf("gittest.Trust: XDG_STATE_HOME is not set; isolate the test's folders first")
		return
	}
	if !insideTemp(state) {
		t.Fatalf("gittest.Trust: XDG_STATE_HOME %s is not inside the temporary folder %s; isolate the test's folders first", state, os.TempDir())
		return
	}
	trustIn(t, state, path)
}

// insideTemp tells whether path is strictly inside the temporary folder or inside GOTMPDIR, where t.TempDir puts its
// folders when GOTMPDIR is set (scripts/checks.sh sets it).
func insideTemp(path string) bool {
	for _, root := range []string{os.TempDir(), os.Getenv("GOTMPDIR")} {
		if root == "" {
			continue
		}
		if rel, err := filepath.Rel(root, path); err == nil && rel != "." && filepath.IsLocal(rel) {
			return true
		}
	}
	return false
}

// trustFileContent is the shape of git-trust.json.
type trustFileContent struct {
	Version        int      `json:"version"`
	Trusted        []string `json:"trusted"`
	TrustedParents []string `json:"trustedParents"`
}

func trustIn(t TB, stateHome, path string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("gittest: resolving %s: %v", path, err)
		return
	}
	file := trustFile(stateHome)
	content := trustFileContent{Version: 1, Trusted: []string{}, TrustedParents: []string{}}
	if data, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(data, &content); err != nil {
			t.Fatalf("gittest: %s is not valid: %v", file, err)
			return
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("gittest: %v", err)
		return
	}
	set := map[string]bool{}
	for _, p := range content.Trusted {
		set[p] = true
	}
	set[filepath.Clean(resolved)] = true
	content.Trusted = content.Trusted[:0]
	for p := range set {
		content.Trusted = append(content.Trusted, p)
	}
	sort.Strings(content.Trusted)
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		t.Fatalf("gittest: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatalf("gittest: %v", err)
		return
	}
	tmp := file + fmt.Sprintf(".%d.tmp", os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("gittest: %v", err)
		return
	}
	if err := os.Rename(tmp, file); err != nil {
		t.Fatalf("gittest: %v", err)
	}
}
