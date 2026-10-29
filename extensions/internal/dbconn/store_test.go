package dbconn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := stateHome(t)
	path := filepath.Join(dir, FileName)
	return NewStore(path), path
}

func sample(name string) Connection {
	return Connection{Name: name, Engine: "postgres", Scope: ScopeUser, Host: "localhost", Port: 5432, User: "me", Database: "app"}
}

func TestStoreWritesWithModes(t *testing.T) {
	st, path := newStore(t)
	if _, err := st.Save(sample("a")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("folder mode = %v, want 0700", dirInfo.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the folder holds %d entries, want only the file (no temporary left)", len(entries))
	}
}

func TestStoreSaveListGetDelete(t *testing.T) {
	st, path := newStore(t)
	a, err := st.Save(sample("a"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" {
		t.Fatal("no ID was made")
	}
	b := sample("b")
	b.Scope, b.Workspace = ScopeWorkspace, "/work/one"
	b, err = st.Save(b)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		folder string
		want   []string
	}{
		{"", []string{"a"}}, {"/work/one", []string{"a", "b"}}, {"/work/two", []string{"a"}},
	}
	for _, tt := range tests {
		var got []string
		for _, c := range st.List(tt.folder) {
			got = append(got, c.Name)
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("List(%q) = %v, want %v", tt.folder, got, tt.want)
		}
	}
	// edit keeps the ID and the place
	a.Name = "a2"
	a.Options = map[string]string{"k": "v"}
	if _, err := st.Save(a); err != nil {
		t.Fatal(err)
	}
	if got, ok := st.Get(a.ID); !ok || got.Name != "a2" || got.Options["k"] != "v" {
		t.Errorf("Get = %+v, %v", got, ok)
	}
	if all := st.All(); len(all) != 2 || all[0].Name != "a2" {
		t.Errorf("All = %+v", all)
	}
	// a second store over the file sees the same
	again := NewStore(path)
	if err := again.Err(); err != nil {
		t.Fatal(err)
	}
	if len(again.All()) != 2 {
		t.Errorf("the file holds %d connections, want 2", len(again.All()))
	}
	// the copies share nothing with the store
	list := st.List("")
	list[0].Options["k"] = "changed"
	if got, _ := st.Get(a.ID); got.Options["k"] != "v" {
		t.Error("a listed connection shares its options with the store")
	}
	gone, err := st.Delete(b.ID)
	if err != nil || gone.Name != "b" {
		t.Fatalf("Delete = %+v, %v", gone, err)
	}
	if _, ok := st.Get(b.ID); ok {
		t.Error("the connection is still there")
	}
	_, err = st.Delete(b.ID)
	wantRoute(t, err, "Store.Delete.ConnectionNotFound")
}

func TestStoreReadPolicy(t *testing.T) {
	good, err := os.ReadFile("testdata/good.json")
	if err != nil {
		t.Fatal(err)
	}
	tooMany := `{"version":1,"connections":[` + strings.TrimSuffix(strings.Repeat(`{"id":"x","name":"n","engine":"e","scope":"user"},`, MaxConnections+1), ",") + `]}`
	tests := []struct {
		name  string
		setup func(t *testing.T, dir, path string)
		route string
	}{
		{"a missing file is an empty store", func(*testing.T, string, string) {}, ""},
		{"a good file", func(t *testing.T, _, path string) { writeFile(t, path, string(good), 0o600) }, ""},
		{"a readable file", func(t *testing.T, _, path string) { writeFile(t, path, string(good), 0o644) }, ""},
		{"a file the group can write", func(t *testing.T, _, path string) { writeFile(t, path, string(good), 0o660) }, "StateFileUnsafeMode"},
		{"a file everyone can write", func(t *testing.T, _, path string) { writeFile(t, path, string(good), 0o666) }, "StateFileUnsafeMode"},
		{"a link", func(t *testing.T, dir, path string) {
			writeFile(t, filepath.Join(dir, "real.json"), string(good), 0o600)
			if err := os.Symlink(filepath.Join(dir, "real.json"), path); err != nil {
				t.Fatal(err)
			}
		}, "StateFileReadFailed"},
		{"a folder", func(t *testing.T, _, path string) {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, "StateFileNotRegular"},
		{"a file that is too large", func(t *testing.T, _, path string) {
			writeFile(t, path, `{"version":1,"connections":[],"pad":"`+strings.Repeat("x", maxFileSize)+`"}`, 0o600)
		}, "StateFileTooLarge"},
		{"text that is not JSON", func(t *testing.T, _, path string) { writeFile(t, path, "[connections]", 0o600) }, "Store.read.ConnectionsFileInvalid"},
		{"a version from the future", func(t *testing.T, _, path string) { writeFile(t, path, `{"version":9,"connections":[]}`, 0o600) }, "Store.read.ConnectionsFileVersion"},
		{"two connections with one ID", func(t *testing.T, _, path string) {
			writeFile(t, path, `{"version":1,"connections":[{"id":"x","name":"a","engine":"e","scope":"user"},{"id":"x","name":"b","engine":"e","scope":"user"}]}`, 0o600)
		}, "Store.read.ConnectionsFileInvalid"},
		{"a connection with no ID", func(t *testing.T, _, path string) {
			writeFile(t, path, `{"version":1,"connections":[{"name":"a","engine":"e","scope":"user"}]}`, 0o600)
		}, "Store.read.ConnectionsFileInvalid"},
		{"more connections than are kept", func(t *testing.T, _, path string) { writeFile(t, path, tooMany, 0o600) }, "Store.read.ConnectionsFileTooMany"},
		{"a connection that fails its check", func(t *testing.T, _, path string) {
			writeFile(t, path, `{"version":1,"connections":[{"id":"x","name":"","engine":"e","scope":"user"}]}`, 0o600)
		}, "Connection.Check.ConnectionNameMissing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := stateHome(t)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, FileName)
			tt.setup(t, dir, path)
			st := NewStore(path)
			if tt.route == "" {
				if err := st.Err(); err != nil {
					t.Fatalf("Err = %v", err)
				}
				return
			}
			wantRoute(t, st.Err(), tt.route)
			if len(st.All()) != 0 {
				t.Error("a store that could not read its file shows connections")
			}
			// and it does not write over the file it did not understand
			_, saveErr := st.Save(sample("new"))
			if saveErr == nil {
				t.Error("Save wrote over a file the store could not read")
			}
			_, delErr := st.Delete("x")
			if delErr == nil {
				t.Error("Delete wrote over a file the store could not read")
			}
			if err := st.Load(); err == nil {
				t.Error("Load of the same file succeeded")
			}
		})
	}
}

func writeFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // the umask may have cleared bits
		t.Fatal(err)
	}
}

func TestStoreSaveRefusesTooMany(t *testing.T) {
	st, _ := newStore(t)
	st.mu.Lock()
	for i := range MaxConnections {
		st.conns = append(st.conns, Connection{ID: "id" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676)), Name: "n", Engine: "e", Scope: ScopeUser})
	}
	st.mu.Unlock()
	_, err := st.Save(sample("one more"))
	wantRoute(t, err, "Store.Save.ConnectionsFileTooMany")
}

func TestStoreLoadFollowsChangesOfTheFile(t *testing.T) {
	st, path := newStore(t)
	if _, err := st.Save(sample("a")); err != nil {
		t.Fatal(err)
	}
	other := NewStore(path)
	if _, err := other.Save(sample("b")); err != nil {
		t.Fatal(err)
	}
	if len(st.All()) != 1 {
		t.Fatal("the first store changed by itself")
	}
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if len(st.All()) != 2 {
		t.Errorf("Load shows %d connections, want 2", len(st.All()))
	}
}

func TestStorePathUsesTheStateFolder(t *testing.T) {
	dir := stateHome(t)
	got, err := StorePath()
	if err != nil || got != filepath.Join(dir, FileName) {
		t.Errorf("StorePath = %q, %v; want %q", got, err, filepath.Join(dir, FileName))
	}
}

func TestStoreSaveChecksTheConnection(t *testing.T) {
	st, _ := newStore(t)
	bad := sample("")
	_, err := st.Save(bad)
	wantRoute(t, err, "Connection.Check.ConnectionNameMissing")
}
