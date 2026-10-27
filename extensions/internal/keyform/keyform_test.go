package keyform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want %s", appErr.Route(), route)
	}
}

func TestThePublicLineOfAKeyFileHasTheTagsOfItsParts(t *testing.T) {
	dir := t.TempDir()
	// a .pub that is a folder: it exists, so the key is not read from the key file, and it cannot be used
	asDir := filepath.Join(dir, "id_dir")
	if err := os.MkdirAll(asDir+".pub", 0o755); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(dir, "junk")
	if err := os.WriteFile(junk, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, route string }{
		{"a .pub that cannot be used", asDir, "PublicLineOfKeyFile->ReadPublicKeyFile.SSHLoginPubFileInvalid"},
		{"nothing there at all", filepath.Join(dir, "missing"), "PublicLineOfKeyFile->PublicKeyOfKeyFile.SSHLoginKeyFileUnreadable"},
		{"a key file that is not a key", junk, "PublicLineOfKeyFile->PublicKeyOfKeyFile->KeyFileAuth.SSHLoginKeyFileInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PublicLineOfKeyFile(tc.path, nil)
			wantRoute(t, err, tc.route)
		})
	}
}

func TestAgentProblemNamesWhatIsWrong(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("SSHLoginAgentUnavailable", "x"), "There is no ssh-agent"},
		{errors.New("SSHLoginAgentNoKey", "x"), "The ssh-agent has no such key"},
		{errors.New("SSHLoginAgentFailed", "x"), "The ssh-agent has no such key"},
		{errors.New("Other", "x"), "The connection failed"},
		{os.ErrNotExist, "The connection failed"},
	} {
		if got := AgentProblem(tc.err); got != tc.want {
			t.Errorf("AgentProblem(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestTheLineOfTheAgentKeyChosen(t *testing.T) {
	f := &Form{}
	for _, tc := range []struct {
		name     string
		set      func()
		line, pr string
	}{
		{"reading", func() { f.AgentLoading = true }, "", "The keys of the ssh-agent are still being read."},
		{"no key", func() { f.AgentLoading = false; f.Agent = nil }, "", "The ssh-agent has no key. Add one with ssh-add."},
		{"a note", func() { f.AgentNote = "There is no ssh-agent" }, "", "There is no ssh-agent"},
		{"one key", func() { f.AgentNote = ""; f.Agent = []AgentKey{{Fingerprint: "a", Line: "L1"}} }, "L1", ""},
		{"two and none chosen", func() { f.Agent = append(f.Agent, AgentKey{Fingerprint: "b", Line: "L2"}) }, "", "The ssh-agent has several keys: choose the one to show."},
		{"one chosen", func() { f.Fingerprint = "b" }, "L2", ""},
		{"chosen but gone", func() { f.Fingerprint = "c" }, "", "That key is not in the ssh-agent any more."},
	} {
		tc.set()
		line, problem := f.AgentLine()
		if line != tc.line || problem != tc.pr {
			t.Errorf("%s: AgentLine = %q, %q; want %q, %q", tc.name, line, problem, tc.line, tc.pr)
		}
	}
}
