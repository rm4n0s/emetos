package trust

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
)

// folders makes real folders in a temporary folder and returns its (resolved) path.
func folders(t *testing.T, names ...string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func wantTag(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestTrustIsKeptAfterARestartAndDontTrustIsNot(t *testing.T) {
	root := folders(t, "a", "b", "c")
	file := filepath.Join(root, "state", FileName)
	s := open(t, file)
	if s.IsTrusted(filepath.Join(root, "a")) {
		t.Fatal("a repository nobody answered about is trusted")
	}
	if err := s.Trust(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	s.Decline(filepath.Join(root, "b"))
	if !s.Declined(filepath.Join(root, "b")) || s.Declined(filepath.Join(root, "a")) {
		t.Error("Declined does not follow Decline")
	}
	again := open(t, file) // a restart
	if !again.IsTrusted(filepath.Join(root, "a")) {
		t.Error("Trust is not in the file")
	}
	if again.IsTrusted(filepath.Join(root, "b")) || again.Declined(filepath.Join(root, "b")) {
		t.Error("Don't Trust was kept")
	}
	// trusting what was declined clears the decline
	if err := s.Trust(filepath.Join(root, "b")); err != nil || s.Declined(filepath.Join(root, "b")) || !s.IsTrusted(filepath.Join(root, "b")) {
		t.Errorf("Trust after Decline: %v", err)
	}
}

func TestTheFileIsPrivateAndWrittenAtomically(t *testing.T) {
	root := folders(t, "a")
	file := filepath.Join(root, "state", "emetos", FileName)
	s := open(t, file)
	if err := s.Trust(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", info, err)
	}
	dir, err := os.Stat(filepath.Dir(file))
	if err != nil || dir.Mode().Perm() != 0o700 {
		t.Errorf("folder: %v %v", dir, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	if len(entries) != 1 {
		t.Errorf("a temporary file is left: %v", entries)
	}
	data, _ := os.ReadFile(file)
	for _, want := range []string{`"version": 1`, `"trusted": [`, `"trustedParents": []`, filepath.Join(root, "a")} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the file lacks %s:\n%s", want, data)
		}
	}
}

func TestAParentFolderCoversWhatIsBelowItAndNotASiblingWithTheSamePrefix(t *testing.T) {
	root := folders(t, "work/a/b", "work/ab", "work/a")
	s := open(t, filepath.Join(root, "state", FileName))
	if err := s.TrustParent(filepath.Join(root, "work", "a")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		rel  string
		want bool
	}{
		{"work/a", true}, {"work/a/b", true}, {"work/ab", false}, {"work", false},
	}
	for _, tc := range tests {
		if got := s.IsTrusted(filepath.Join(root, tc.rel)); got != tc.want {
			t.Errorf("IsTrusted(%s) = %v", tc.rel, got)
		}
	}
	if got := ParentOf(filepath.Join(root, "work", "a", "b")); got != filepath.Join(root, "work", "a") {
		t.Errorf("ParentOf = %s", got)
	}
	// a parent that was trusted before the repository was declined covers it
	s.Decline(filepath.Join(root, "work", "ab"))
	if err := s.TrustParent(filepath.Join(root, "work")); err != nil {
		t.Fatal(err)
	}
	if s.Declined(filepath.Join(root, "work", "ab")) || !s.IsTrusted(filepath.Join(root, "work", "ab")) {
		t.Error("trusting the parent did not cover the declined repository")
	}
}

func TestASymbolicLinkToATrustedPathIsTheSameRepository(t *testing.T) {
	root := folders(t, "real")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	s := open(t, filepath.Join(root, "state", FileName))
	if err := s.Trust(filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if !s.IsTrusted(filepath.Join(root, "real")) || !s.IsTrusted(filepath.Join(root, "link")) {
		t.Error("the link and its target are not the same repository")
	}
	entries := s.Entries()
	if len(entries) != 1 || entries[0].Path != filepath.Join(root, "real") || entries[0].Kind != Repository {
		t.Errorf("entries = %+v", entries)
	}
}

func TestAFileThatCannotBeUsedIsIgnoredWithItsTag(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		mode      os.FileMode
		wantRoute string
		// overwritten says whether the next answer replaces the file
		overwritten bool
	}{
		{"corrupt", "{not json", 0o600, "Open.GitTrustFileCorrupt", true},
		{"a future version", `{"version": 2, "trusted": ["/x"], "trustedParents": []}`, 0o600, "Open.GitTrustVersionUnsupported", false},
		{"writable by others", `{"version": 1, "trusted": ["/x"], "trustedParents": []}`, 0o666, "Open.GitTrustFileUnreadable", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := folders(t, "repo")
			file := filepath.Join(root, "state", FileName)
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(tc.content), tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(file, tc.mode); err != nil {
				t.Fatal(err)
			}
			s, err := Open(file)
			wantTag(t, err, tc.wantRoute)
			if s == nil || s.IsTrusted("/x") || len(s.Entries()) != 0 {
				t.Fatalf("the unusable file gave %+v", s)
			}
			// the question is asked, and the answer holds for the session
			werr := s.Trust(filepath.Join(root, "repo"))
			if !s.IsTrusted(filepath.Join(root, "repo")) {
				t.Error("the answer does not hold for the session")
			}
			data, _ := os.ReadFile(file)
			if tc.overwritten {
				if werr != nil || !strings.Contains(string(data), "repo") {
					t.Errorf("the corrupt file was not replaced: %v\n%s", werr, data)
				}
				return
			}
			wantTag(t, werr, "Store.save.GitTrustWriteRefused")
			if string(data) != tc.content {
				t.Errorf("the file was changed:\n%s", data)
			}
		})
	}
}

func TestAMissingFileIsAnEmptyStoreAndAFailedWriteKeepsTheAnswer(t *testing.T) {
	root := folders(t, "repo")
	// the state folder cannot be written to
	dir := filepath.Join(root, "state")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read only folder")
	}
	s := open(t, filepath.Join(dir, FileName))
	err := s.Trust(filepath.Join(root, "repo"))
	wantTag(t, err, "StateFileWriteFailed")
	if !s.IsTrusted(filepath.Join(root, "repo")) {
		t.Error("a failed write lost the answer")
	}
	if len(s.Entries()) != 1 {
		t.Errorf("entries = %+v", s.Entries())
	}
}

func TestRemoveStopsTrustAtOnce(t *testing.T) {
	root := folders(t, "a", "b")
	file := filepath.Join(root, "state", FileName)
	s := open(t, file)
	_ = s.Trust(filepath.Join(root, "a"))
	_ = s.TrustParent(filepath.Join(root, "b"))
	ok, err := s.Remove(Entry{Kind: Repository, Path: filepath.Join(root, "a")})
	if err != nil || !ok || s.IsTrusted(filepath.Join(root, "a")) {
		t.Errorf("Remove repository: %v %v", ok, err)
	}
	ok, err = s.Remove(Entry{Kind: Repository, Path: filepath.Join(root, "a")})
	if err != nil || ok {
		t.Errorf("Remove twice: %v %v", ok, err)
	}
	ok, err = s.Remove(Entry{Kind: Parent, Path: filepath.Join(root, "b")})
	if err != nil || !ok || s.IsTrusted(filepath.Join(root, "b")) {
		t.Errorf("Remove parent: %v %v", ok, err)
	}
	if got := open(t, file).Entries(); len(got) != 0 {
		t.Errorf("the file still has %+v", got)
	}
}

func TestMemoryStoreWritesNothingAndIsSafeForConcurrentUse(t *testing.T) {
	root := folders(t, "a", "b")
	s := NewMemory()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = s.Trust(filepath.Join(root, "a"))
				_ = s.IsTrusted(filepath.Join(root, "b"))
				s.Decline(filepath.Join(root, "b"))
				_ = s.Entries()
			}
		}()
	}
	wg.Wait()
	if !s.IsTrusted(filepath.Join(root, "a")) {
		t.Error("lost an answer")
	}
}

func TestAnAnswerThatCannotBeEncodedKeepsHoldingForTheSession(t *testing.T) {
	root := folders(t, "repo")
	file := filepath.Join(root, "state", "git-trust.json")
	s := open(t, file)
	s.encode = func(any, string, string) ([]byte, error) { return nil, errors.New("Boom", "cannot encode") }
	werr := s.Trust(filepath.Join(root, "repo"))
	wantTag(t, werr, "Store.save.GitTrustEncodeFailed")
	if !s.IsTrusted(filepath.Join(root, "repo")) {
		t.Error("the answer does not hold for the session")
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("a file was written")
	}
}
