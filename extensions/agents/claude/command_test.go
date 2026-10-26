package claude

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const fixedFlags = "-p --input-format stream-json --output-format stream-json --verbose --include-partial-messages " +
	"--replay-user-messages --permission-prompt-tool stdio"

func TestArgs(t *testing.T) {
	const id = "11111111-2222-4333-8444-555555555555"
	tests := []struct {
		name string
		opts SessionOptions
		want string
	}{
		{"defaults", SessionOptions{}, fixedFlags + " --permission-mode default"},
		{"plan with a new session", SessionOptions{PermissionMode: ModePlan, SessionID: id}, fixedFlags + " --permission-mode plan --session-id " + id},
		{"accept edits resuming with a model", SessionOptions{PermissionMode: ModeAcceptEdits, Resume: "abc-123", Model: "opus"},
			fixedFlags + " --permission-mode acceptEdits --resume abc-123 --model opus"},
		{"a model only", SessionOptions{Model: "sonnet"}, fixedFlags + " --permission-mode default --model sonnet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Args(tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.Fields(tt.want); !reflect.DeepEqual(got, want) {
				t.Errorf("Args = %q, want %q", got, want)
			}
			for _, banned := range []string{"--bare", "--dangerously-skip-permissions", "--settings", "--mcp-config", "--api-key"} {
				if slices.Contains(got, banned) {
					t.Errorf("the command line has %s", banned)
				}
			}
		})
	}
}

func TestArgsInvalid(t *testing.T) {
	tests := []struct {
		name string
		opts SessionOptions
	}{
		{"unknown mode", SessionOptions{PermissionMode: "bypassPermissions"}},
		{"new and resumed", SessionOptions{SessionID: "11111111-2222-4333-8444-555555555555", Resume: "x"}},
		{"session ID that is not a UUID", SessionOptions{SessionID: "not-a-uuid"}},
		{"resume that looks like a flag", SessionOptions{Resume: "--bare"}},
		{"model that looks like a flag", SessionOptions{Model: "--settings"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Args(tt.opts)
			wantRoute(t, err, "Args.InvalidOptions")
		})
	}
}

func TestEnvironment(t *testing.T) {
	base := []string{
		"PATH=/usr/bin", "HOME=/home/user", "CLAUDE_CONFIG_DIR=/the/users/own", "ANTHROPIC_API_KEY=users-own-key",
		CheckpointingVariable + "=false", "EMPTY=",
	}
	got := Environment(base)
	want := []string{"PATH=/usr/bin", "HOME=/home/user", "CLAUDE_CONFIG_DIR=/the/users/own", "ANTHROPIC_API_KEY=users-own-key", "EMPTY=",
		CheckpointingVariable + "=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Environment = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(base[4], CheckpointingVariable+"=false") {
		t.Error("Environment changed the slice it was given")
	}

	// With a clean environment the adapter adds the one variable and nothing that names a configuration folder or a key.
	clean := Environment([]string{"PATH=/usr/bin"})
	if len(clean) != 2 {
		t.Fatalf("Environment of a clean environment = %q", clean)
	}
	for _, kv := range clean {
		for _, banned := range []string{"CLAUDE_CONFIG_DIR", "ANTHROPIC", "TOKEN", "API_KEY"} {
			if strings.Contains(strings.ToUpper(kv), banned) {
				t.Errorf("the environment has %s", kv)
			}
		}
	}
}

func TestFind(t *testing.T) {
	t.Run("the setting", func(t *testing.T) {
		got, err := (&Adapter{Path: fakeProgram}).Find()
		if err != nil || got != fakeProgram {
			t.Errorf("Find = %q, %v; want %q", got, err, fakeProgram)
		}
	})
	t.Run("PATH", func(t *testing.T) {
		dir := t.TempDir()
		data, err := os.ReadFile(fakeProgram)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ProgramName), data, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		got, err := (&Adapter{}).Find()
		if err != nil || got != filepath.Join(dir, ProgramName) {
			t.Errorf("Find = %q, %v", got, err)
		}
	})
	t.Run("not in PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := (&Adapter{}).Find()
		appErr := routeOf(t, err, "Adapter.Find.ProgramNotFound")
		if appErr.Metadata["fromSetting"] != false {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
	t.Run("the setting names a missing file", func(t *testing.T) {
		_, err := (&Adapter{Path: filepath.Join(t.TempDir(), "nope")}).Find()
		appErr := routeOf(t, err, "Adapter.Find.ProgramNotFound")
		if appErr.Metadata["fromSetting"] != true {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
	t.Run("the setting names a file that cannot run", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "claude")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := (&Adapter{Path: f}).Find()
		wantRoute(t, err, "Adapter.Find.ProgramNotFound")
	})
}
