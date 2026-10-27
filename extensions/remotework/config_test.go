package remotework

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	k := newKey(t)
	line := k.line("laptop")
	write := func(t *testing.T, content string, mode os.FileMode) string {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, configFileName)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	keyJSON := func(l string) string { b, _ := json.Marshal(l); return string(b) }
	tests := []struct {
		name  string
		dir   string
		route string
	}{
		{"missing file is the default", t.TempDir(), ""},
		{"a good file", write(t, `{"version":1,"port":50000,"idleMinutes":10,"workspaces":["/a/b"],"keys":[{"line":`+keyJSON(line)+`,"added":"x"}]}`, 0o600), ""},
		{"readable by others", write(t, `{"version":1,"port":50000,"idleMinutes":10,"workspaces":[],"keys":[]}`, 0o644), ""},
		{"writable by the group", write(t, `{"version":1,"port":50000,"idleMinutes":10}`, 0o660), "Read.StateFileUnsafeMode"},
		{"writable by everybody", write(t, `{"version":1,"port":50000,"idleMinutes":10}`, 0o666), "Read.StateFileUnsafeMode"},
		{"not json", write(t, `{`, 0o600), "loadConfig.RemoteWorkConfigInvalid"},
		{"unknown version", write(t, `{"version":2,"port":50000,"idleMinutes":10}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"port zero", write(t, `{"version":1,"port":0,"idleMinutes":10}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"idle zero", write(t, `{"version":1,"port":5000,"idleMinutes":0}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"relative workspace", write(t, `{"version":1,"port":5000,"idleMinutes":3,"workspaces":["a/b"]}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"unclean workspace", write(t, `{"version":1,"port":5000,"idleMinutes":3,"workspaces":["/a/../b"]}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"a key that is not one", write(t, `{"version":1,"port":5000,"idleMinutes":3,"keys":[{"line":"junk"}]}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"a key with options", write(t, `{"version":1,"port":5000,"idleMinutes":3,"keys":[{"line":`+keyJSON(`command="x" `+line)+`}]}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"a key twice", write(t, `{"version":1,"port":5000,"idleMinutes":3,"keys":[{"line":`+keyJSON(line)+`},{"line":`+keyJSON(k.line("again"))+`}]}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
		{"too many workspaces", write(t, `{"version":1,"port":5000,"idleMinutes":3,"workspaces":[`+strings.TrimSuffix(strings.Repeat(`"/a",`, MaxWorkspaces+1), ",")+`]}`, 0o600), "Config.check.RemoteWorkConfigInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := loadConfig(tt.dir)
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Port < 1 || c.IdleMinutes < 1 {
				t.Errorf("config = %+v", c)
			}
		})
	}
}

func TestConfigValues(t *testing.T) {
	c := DefaultConfig()
	if c.Port != 47123 || c.IdleTimeout() != 30*time.Minute || c.On("/a") || len(c.AllowedKeys()) != 0 {
		t.Errorf("default = %+v", c)
	}
	wantRoute(t, CheckPort(0), "CheckPort.RemoteWorkSettingInvalid")
	wantRoute(t, CheckPort(65536), "CheckPort.RemoteWorkSettingInvalid")
	wantRoute(t, CheckIdle(0), "CheckIdle.RemoteWorkSettingInvalid")
	wantRoute(t, CheckIdle(MaxIdleMinutes+1), "CheckIdle.RemoteWorkSettingInvalid")
	if CheckPort(1) != nil || CheckPort(65535) != nil || CheckIdle(1) != nil || CheckIdle(MaxIdleMinutes) != nil {
		t.Error("a limit was refused")
	}
	// the copy is its own
	c.Workspaces = []string{"/a"}
	cp := c.clone()
	cp.Workspaces[0] = "/b"
	if c.Workspaces[0] != "/a" {
		t.Error("clone shares the slice")
	}
}

func TestSaveConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	c := DefaultConfig()
	c.Port = 50000
	if err := saveConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	back, err := loadConfig(dir)
	if err != nil || back.Port != 50000 {
		t.Fatalf("loadConfig = %+v, %v", back, err)
	}
	// nil lists are written as empty lists, so the file reads back
	if err := saveConfig(dir, Config{Port: 1, IdleMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	// a configuration the file could not be read back from is not written
	before, _ := os.ReadFile(filepath.Join(dir, configFileName))
	wantRoute(t, saveConfig(dir, Config{Port: 0, IdleMinutes: 1}), "Config.check.RemoteWorkConfigInvalid")
	after, _ := os.ReadFile(filepath.Join(dir, configFileName))
	if string(before) != string(after) {
		t.Error("a bad configuration was written")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, saveConfig(filepath.Join(blocker, "x"), DefaultConfig()), "Write.StateFileWriteFailed")
}

func TestMutationErrors(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	k := newKey(t)
	if _, err := r.hub.AddKey(k.line("a")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		run   func() error
		route string
	}{
		{"port 0", func() error { return r.hub.SetPort(0) }, "CheckPort.RemoteWorkSettingInvalid"},
		{"idle 0", func() error { return r.hub.SetIdleMinutes(0) }, "CheckIdle.RemoteWorkSettingInvalid"},
		{"the same key twice", func() error { _, err := r.hub.AddKey(k.line("again")); return err }, "RemoteWorkKeyDuplicate"},
		{"not a key", func() error { _, err := r.hub.AddKey("junk"); return err }, "ParseAuthorizedKey.SSHLoginKeyLineInvalid"},
		{"remove an unknown key", func() error { return r.hub.RemoveKey("SHA256:nobody") }, "RemoteWorkKeyUnknown"},
		{"a relative folder", func() error { return r.hub.SetWorkspace("relative/dir", true) }, "RemoteWorkSettingInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { wantRoute(t, tt.run(), tt.route) })
	}
	// nothing of a refused change was written
	snap := r.hub.Snapshot(folder)
	if snap.Config.Port != DefaultPort || len(snap.Keys) != 1 {
		t.Errorf("config = %+v", snap.Config)
	}
	// the list is bounded
	for i := 1; i < MaxKeys; i++ {
		if _, err := r.hub.AddKey(newKey(t).line("k")); err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
	}
	_, err := r.hub.AddKey(newKey(t).line("one too many"))
	wantRoute(t, err, "RemoteWorkTooManyKeys")
}

func TestAWriteThatFailsChangesNothing(t *testing.T) {
	r := newRig(t)
	r.workspace("project")
	if _, err := r.hub.AddKey(newKey(t).line("a")); err != nil {
		t.Fatal(err)
	}
	// the state folder becomes a file: nothing can be written
	if err := os.RemoveAll(r.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, r.hub.SetPort(50010), "Write.StateFileWriteFailed")
	if got := r.hub.Snapshot("/x").Config.Port; got != DefaultPort {
		t.Errorf("port = %d after a failed write", got)
	}
}
