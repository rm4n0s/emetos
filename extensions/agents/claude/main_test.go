package claude

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/agents/claude/claudetest"
)

// fakeProgram is the path of the fake claude program, built once per test run by TestMain.
var fakeProgram string

// TestMain builds the fake program, then shuts every way the tests could reach the user's own claude program or account:
// PATH is an empty folder (the adapter is given the fake's path, and the tests that look in PATH put the fake there), HOME and the
// XDG folders are temporary, and no variable that names a configuration folder, a key or a token is left in the
// environment. A sentinel variable proves that the rest of the environment reaches the program unchanged. With the build
// tag emetos_claude (the manual test against the real program) the environment is left alone.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "claude-adapter-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temporary folder:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	fakeProgram, err = claudetest.Build(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "building the fake claude:", err)
		return 1
	}
	if !realClaudeTests {
		isolate(dir)
	}
	return m.Run()
}

// isolate empties PATH and moves HOME and the XDG folders into dir.
func isolate(dir string) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "ANTHROPIC") || strings.HasPrefix(name, "XDG_") {
			_ = os.Unsetenv(name)
		}
	}
	empty := dir + "/empty-path"
	if err := os.Mkdir(empty, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "empty PATH folder:", err)
		os.Exit(1)
	}
	for k, v := range map[string]string{
		"PATH": empty, "HOME": dir + "/home", "XDG_CONFIG_HOME": dir + "/config", "XDG_DATA_HOME": dir + "/data",
		"XDG_CACHE_HOME": dir + "/cache", "XDG_STATE_HOME": dir + "/state", sentinelName: sentinelValue,
	} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, "setenv:", err)
			os.Exit(1)
		}
	}
}

// The sentinel variable of the user's environment that must reach the program unchanged.
const (
	sentinelName  = "CLAUDE_TEST_SENTINEL"
	sentinelValue = "passes-through"
)
