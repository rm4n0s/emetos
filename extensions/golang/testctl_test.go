package golang

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The tree of the tests (step 26.5): the packages, files, functions and subtests found in the _test.go files of the workspace.

// dump writes the items as indented lines: the label, and "@line" (from 1) for the item that has a place in its file.
func dump(c *extension.TestItemCollection) string {
	var sb strings.Builder
	var walk func(items []*extension.TestItem, depth int)
	walk = func(items []*extension.TestItem, depth int) {
		for _, it := range items {
			d := it.Data()
			sb.WriteString(strings.Repeat("  ", depth) + d.Label)
			if d.Range != nil {
				sb.WriteString(" @" + strconv.Itoa(d.Range.Start.Line+1))
			}
			sb.WriteString("\n")
			walk(it.Children().All(), depth+1)
		}
	}
	walk(c.All(), 0)
	return sb.String()
}

// treeRig activates the extension over a folder with the files and waits for the discovery.
func treeRig(t *testing.T, files map[string]string) *rig {
	t.Helper()
	r := newRig(t, rigOptions{withGo: true, files: files})
	r.discovered()
	return r
}

const subtestSource = `package p

import "testing"

func TestTable(t *testing.T) {
	t.Run("small number", func(t *testing.T) {
		t.Run("deeper", func(t *testing.T) {})
	})
	t.Run("again", func(t *testing.T) {})
	t.Run("again", func(t *testing.T) {})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {})
	}
	t.Run("a/b", func(t *testing.T) {})
	t.Run("", func(t *testing.T) {})
}

func BenchmarkSort(b *testing.B) {
	b.Run("n=10", func(b *testing.B) {})
}

func TestNotATable(t *testing.T) {
	u := t
	u.Run("not found", func(t *testing.T) {})
}
`

func TestDiscoveryMakesTheTreeOfPackagesFilesFunctionsAndSubtests(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"one package", map[string]string{"add_test.go": sampleTests}, "p\n  add_test.go\n    TestAdd @5\n      a @6\n    TestSub @11\n    BenchmarkAdd @13\n    ExampleAdd @15\n    FuzzAdd @17\n"},
		{"two packages, the order of the walk", map[string]string{
			"a/a_test.go":   "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
			"b/c/c_test.go": "package c\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n",
		}, "a\n  a_test.go\n    TestA @5\nb/c\n  c_test.go\n    TestC @5\n"},
		{"subtests with a literal name", map[string]string{"s_test.go": subtestSource},
			"p\n  s_test.go\n    TestTable @5\n      small_number @6\n        deeper @7\n      again @9\n      again#01 @10\n    BenchmarkSort @18\n      n=10 @19\n    TestNotATable @22\n"},
		{"a renamed import of testing", map[string]string{"r_test.go": "package p\n\nimport tt \"testing\"\n\nfunc TestR(t *tt.T) {\n\tt.Run(\"x\", func(t *tt.T) {})\n}\n"}, "p\n  r_test.go\n    TestR @5\n      x @6\n"},
		{"files that are not tests or have no tests", map[string]string{
			"code.go":        "package p\n\nfunc F() {}\n",
			"helper_test.go": "package p\n\nfunc helper() {}\n",
			"lower_test.go":  "package p\n\nimport \"testing\"\n\nfunc Testlower(t *testing.T) {}\n",
		}, ""},
		{"folders that go skips", map[string]string{
			"testdata/x/x_test.go": "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
			"vendor/v/v_test.go":   "package v\n\nimport \"testing\"\n\nfunc TestV(t *testing.T) {}\n",
			"_hidden/h_test.go":    "package h\n\nimport \"testing\"\n\nfunc TestH(t *testing.T) {}\n",
			"ok/ok_test.go":        "package ok\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n",
		}, "ok\n  ok_test.go\n    TestOK @5\n"},
		{"a file that does not parse keeps what the parser read", map[string]string{"bad_test.go": "package p\n\nimport \"testing\"\n\nfunc TestGood(t *testing.T) {}\n\nfunc TestBad(t *testing.T) {\n\tx := \n"},
			"p\n  bad_test.go\n    TestGood @5\n    TestBad @7\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := treeRig(t, tt.files)
			got := dump(r.ctl().Items())
			// the root folder's package is labelled with the name of the folder
			got = strings.ReplaceAll(got, filepath.Base(r.folder)+"\n", "p\n")
			if got != tt.want {
				t.Errorf("tree =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestTheIdsOfTheItemsAreTheUriWithTheKindAndTheName(t *testing.T) {
	r := treeRig(t, map[string]string{"s_test.go": subtestSource + "\nfunc ExampleX() {}\n\nfunc FuzzY(f *testing.F) {}\n"})
	pkg := r.ctl().Items().All()[0]
	file := pkg.Children().All()[0]
	uri := fileURI(filepath.Join(r.folder, "s_test.go"))
	ids := []string{pkg.ID(), file.ID()}
	var walk func(it *extension.TestItem)
	walk = func(it *extension.TestItem) {
		for _, c := range it.Children().All() {
			ids = append(ids, c.ID())
			walk(c)
		}
	}
	walk(file)
	want := []string{
		fileURI(r.folder) + "?package", uri + "?file",
		uri + "?test#TestTable", uri + "?test#TestTable/small_number", uri + "?test#TestTable/small_number/deeper", uri + "?test#TestTable/again",
		uri + "?test#TestTable/again#01",
		uri + "?benchmark#BenchmarkSort", uri + "?benchmark#BenchmarkSort/n=10", uri + "?test#TestNotATable",
		uri + "?example#ExampleX", uri + "?fuzz#FuzzY",
	}
	if strings.Join(ids, "\n") != strings.Join(want, "\n") {
		t.Errorf("ids =\n%s\nwant\n%s", strings.Join(ids, "\n"), strings.Join(want, "\n"))
	}
	if d := pkg.Data(); d.URI != fileURI(r.folder) {
		t.Errorf("the package's uri = %q", d.URI)
	}
	if rg := file.Children().All()[0].Data().Range; rg == nil || rg.Start.Line != 4 || rg.End.Line != 15 {
		t.Errorf("the range of TestTable = %+v", rg)
	}
}

func TestAChangeOfATestFileUpdatesTheTreeAndALeftAloneFileKeepsItsItems(t *testing.T) {
	r := treeRig(t, map[string]string{"a_test.go": "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"})
	changed := func(kind, rel string) {
		t.Helper()
		r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: kind, Path: rel}}})
		r.discovered()
	}
	first := r.ctl().Items().All()[0].Children().All()[0].Children().All()[0]
	// the same text again: the items stay
	changed(extension.FileChanged, "a_test.go")
	if again := r.ctl().Items().All()[0].Children().All()[0].Children().All()[0]; again != first {
		t.Error("the items of an unchanged file were made again")
	}
	// a new function and a new file in a new package
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(r.folder, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a_test.go", "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n")
	write("sub/s_test.go", "package s\n\nimport \"testing\"\n\nfunc TestS(t *testing.T) {}\n")
	changed(extension.FileChanged, "a_test.go")
	changed(extension.FileCreated, "sub/s_test.go")
	want := "" + filepath.Base(r.folder) + "\n  a_test.go\n    TestA @5\n    TestB @7\nsub\n  s_test.go\n    TestS @5\n"
	if got := dump(r.ctl().Items()); got != want {
		t.Errorf("tree =\n%s\nwant\n%s", got, want)
	}
	// a file that loses its tests, and one that is deleted: the package goes with its last file
	write("a_test.go", "package p\n")
	if err := os.Remove(filepath.Join(r.folder, "sub", "s_test.go")); err != nil {
		t.Fatal(err)
	}
	changed(extension.FileChanged, "a_test.go")
	changed(extension.FileDeleted, "sub/s_test.go")
	if got := dump(r.ctl().Items()); got != "" {
		t.Errorf("tree =\n%s\nwant none", got)
	}
	// renamed: both paths are read
	write("c_test.go", "package p\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n")
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileRenamed, Path: "c_test.go", OldPath: "old_test.go"}}})
	r.discovered()
	if got := dump(r.ctl().Items()); !strings.Contains(got, "c_test.go\n    TestC @5") {
		t.Errorf("tree =\n%s", got)
	}
}

func TestARescanAndRefreshTestsReadEveryFileAgain(t *testing.T) {
	r := treeRig(t, map[string]string{"a_test.go": "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"})
	path := filepath.Join(r.folder, "b_test.go")
	if err := os.WriteFile(path, []byte("package p\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.folder, "a_test.go")); err != nil {
		t.Fatal(err)
	}
	r.api.RescanFiles()
	// the Refresh Tests command
	if err := r.ctl().Refresh(r.ext.ctx); err != nil {
		t.Fatal(err)
	}
	if got := dump(r.ctl().Items()); !strings.Contains(got, "b_test.go\n    TestB @5") || strings.Contains(got, "TestA") {
		t.Errorf("tree after the refresh =\n%s", got)
	}
	// the files event with a rescan
	if err := os.WriteFile(path, []byte("package p\n\nimport \"testing\"\n\nfunc TestB2(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileRescan, Path: "."}}})
	r.discovered()
	if got := dump(r.ctl().Items()); !strings.Contains(got, "TestB2") || strings.Contains(got, "TestB @") {
		t.Errorf("tree after the rescan =\n%s", got)
	}
	// the resolve handler of the whole tree reads again too
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	r.api.RescanFiles()
	if err := r.ctl().Resolve(r.ext.ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := dump(r.ctl().Items()); got != "" {
		t.Errorf("tree after the resolve =\n%s", got)
	}
}

func TestTheImportPathOfAPackageIsThatOfItsModule(t *testing.T) {
	r := treeRig(t, map[string]string{
		"sub/x_test.go":          "package sub\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		"inner/go.mod":           "module \"example.com/inner\"\n\ngo 1.21\n",
		"inner/deep/d_test.go":   "package deep\n\nimport \"testing\"\n\nfunc TestD(t *testing.T) {}\n",
		"nomod/nested/n_test.go": "package nested\n\nimport \"testing\"\n\nfunc TestN(t *testing.T) {}\n",
	})
	tc := r.ext.tests
	tc.mu.Lock()
	defer tc.mu.Unlock()
	got := map[string]string{}
	for dir, p := range tc.pkgs {
		rel, _ := filepath.Rel(r.folder, dir)
		got[filepath.ToSlash(rel)] = p.importPath
	}
	want := map[string]string{"sub": "example.com/p/sub", "inner/deep": "example.com/inner/deep", "nomod/nested": "example.com/p/nomod/nested"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("import path of %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestSubtestNamesAreGoTestsNames(t *testing.T) {
	for _, tt := range []struct {
		in, want string
		ok       bool
	}{
		{"plain", "plain", true}, {"two words", "two_words", true}, {"tab\there", "tab_here", true}, {"", "", false}, {"a/b", "", false},
		{"bell\a", "", false}, {"ünï", "ünï", true}, {"n=10", "n=10", true},
	} {
		got, ok := subtestName(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("subtestName(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestAFileMissingFromAnUnfinishedListIsNotGoneYet(t *testing.T) {
	r := treeRig(t, map[string]string{"a_test.go": "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"})
	if err := os.Remove(filepath.Join(r.folder, "a_test.go")); err != nil {
		t.Fatal(err)
	}
	r.api.RescanFiles()
	r.api.SetComplete(false)
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileListed}}})
	r.discovered()
	if got := dump(r.ctl().Items()); !strings.Contains(got, "TestA") {
		t.Errorf("the file went with the walk unfinished:\n%s", got)
	}
	r.api.SetComplete(true)
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileListed}}})
	r.discovered()
	if got := dump(r.ctl().Items()); got != "" {
		t.Errorf("the file stayed after the walk ended:\n%s", got)
	}
}

// A command at the cursor loads the file from the editor while the discovery of the workspace is walking: the list the walk made
// does not have the file, and the discovery must not take out what is newer than its list.
func TestDiscoveryKeepsAFileThatACommandLoadedWhileItWasListing(t *testing.T) {
	r := treeRig(t, nil)
	tc := r.ext.tests
	path := filepath.Join(r.folder, "late_test.go")
	tc.listed = func() { tc.loadText(path, "package p\n\nimport \"testing\"\n\nfunc TestLate(t *testing.T) {}\n") }
	tc.discoverAll(t.Context())
	tc.listed = nil
	if tc.itemFor(r.folder, "TestLate") == nil {
		t.Fatal("the test of a file that was loaded during the discovery was taken out")
	}
	// the next discovery has the file after its start: it is not in the list, so it is gone
	tc.discoverAll(t.Context())
	if tc.itemFor(r.folder, "TestLate") != nil {
		t.Error("a file that is not in the list of a discovery that began after it was loaded stayed")
	}
}

// The discovery reads a test file while it is being written (it is empty at that moment), and a command loads the whole file before the
// discovery applies what it read: the empty read must not take out what the command loaded after it.
func TestAnEmptyReadOfAFileBeingWrittenDoesNotTakeOutWhatACommandLoadedAfterIt(t *testing.T) {
	r := treeRig(t, nil)
	tc := r.ext.tests
	path := filepath.Join(r.folder, "writing_test.go")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	read, release := make(chan struct{}), make(chan struct{})
	tc.fileRead = func() {
		tc.fileRead = nil
		close(read)
		<-release
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tc.loadFile(path) // the discovery: it has read the empty file
	}()
	<-read
	if err := os.WriteFile(path, []byte("package p\n\nimport \"testing\"\n\nfunc TestWritten(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := make(chan struct{})
	go func() {
		defer close(command)
		tc.loadFile(path) // the command: it reads the whole file
	}()
	time.Sleep(100 * time.Millisecond) // without the lock the command is done by now
	close(release)
	<-done
	<-command
	if tc.itemFor(r.folder, "TestWritten") == nil {
		t.Error("the empty read took out the test that a command loaded from the whole file")
	}
}

// A command loads a test file from the disk, and the next discovery has a list of the workspace from before the file was written: the
// file is still there, so it is not gone.
func TestDiscoveryKeepsAFileThatIsOnTheDiskWhenItsListIsOlder(t *testing.T) {
	r := treeRig(t, nil)
	tc := r.ext.tests
	path := filepath.Join(r.folder, "fresh_test.go")
	if err := os.WriteFile(path, []byte("package p\n\nimport \"testing\"\n\nfunc TestFresh(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tc.loadFile(path) // the command; the file index of the rig has not been rescanned
	tc.discoverAll(t.Context())
	if tc.itemFor(r.folder, "TestFresh") == nil {
		t.Fatal("a discovery with a list from before the file took it out while it is on the disk")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	r.api.RescanFiles()
	tc.discoverAll(t.Context())
	if tc.itemFor(r.folder, "TestFresh") != nil {
		t.Error("a file that is gone from the disk stayed")
	}
}
