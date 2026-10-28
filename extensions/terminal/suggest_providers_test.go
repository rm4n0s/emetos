package terminal

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension/ui"
)

// suggestRig is a folder tree for the providers: two folders of PATH with programs, a folder the shell is in with files of
// every kind, and a home folder.
type suggestRig struct {
	root, bin1, bin2, cwd, home string
	env                         suggestEnv
	sg                          *suggester
}

func putFile(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
}

func newSuggestRig(t testing.TB) *suggestRig {
	t.Helper()
	root := t.TempDir()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	r := &suggestRig{root: root, bin1: filepath.Join(root, "bin1"), bin2: filepath.Join(root, "bin2"), cwd: filepath.Join(root, "work"), home: filepath.Join(root, "home")}
	for _, f := range []string{"git", "gitk", "ls", "gimp", "cat"} {
		putFile(t, filepath.Join(r.bin1, f), 0o755)
	}
	putFile(t, filepath.Join(r.bin1, "notexec"), 0o644)
	putFile(t, filepath.Join(r.bin2, "git"), 0o755) // hidden by the one of bin1
	putFile(t, filepath.Join(r.bin2, "zzz"), 0o755)
	if err := os.MkdirAll(filepath.Join(r.bin1, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(r.cwd, "a.txt"), 0o644)
	putFile(t, filepath.Join(r.cwd, "run.sh"), 0o755)
	putFile(t, filepath.Join(r.cwd, "my file.txt"), 0o644)
	putFile(t, filepath.Join(r.cwd, ".hidden"), 0o644)
	putFile(t, filepath.Join(r.cwd, "sub", "c.go"), 0o644)
	if err := os.Symlink("a.txt", filepath.Join(r.cwd, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join(r.cwd, "linkdir")); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(r.home, "docs", "notes.md"), 0o644)
	putFile(t, filepath.Join(r.home, "top.txt"), 0o644)
	r.env = suggestEnv{
		shell: "bash", cwd: r.cwd, home: r.home, cdPath: "absolute",
		environ: []string{"PATH=" + r.bin1 + ":" + r.bin2 + ":relative/bin:/does/not/exist"},
	}
	r.sg = newSuggester(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return r
}

// complete is the whole request for a line with the cursor at its end.
func (r *suggestRig) complete(line string, explicit bool, quick quickSettings) []rawItem {
	lc := newLineContext(line, len(line))
	return r.sg.complete(context.Background(), r.env, lc, explicit, quick).items
}

func TestCommandSuggestions(t *testing.T) {
	r := newSuggestRig(t)
	items := r.sg.commandSuggestions(context.Background(), r.env)
	got := map[string]rawItem{}
	for _, it := range items {
		if _, dup := got[it.label]; dup {
			t.Errorf("%s is offered twice", it.label)
		}
		got[it.label] = it
	}
	for _, name := range []string{"git", "gitk", "ls", "gimp", "zzz", "cd", "echo", "history"} {
		if _, ok := got[name]; !ok {
			t.Errorf("%s is not offered", name)
		}
	}
	for _, name := range []string{"notexec", "folder", "relative"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s is offered but cannot be run", name)
		}
	}
	if g := got["git"]; g.detail != "Distributed version control system" || g.docs != filepath.Join(r.bin1, "git") || g.kind != kindMethod {
		t.Errorf("git = %+v: the description is the spec's and the path is kept for the details", g)
	}
	if g := got["gitk"]; g.detail != filepath.Join(r.bin1, "gitk") {
		t.Errorf("gitk has no spec, so its detail is its path: %+v", g)
	}
	if g := got["cd"]; g.detail == "" || g.provider != providerCommands {
		t.Errorf("cd is a builtin of bash with a description: %+v", g)
	}
	// a shell with no list of builtins offers only the programs
	r.env.shell = "pwsh"
	for _, it := range r.sg.commandSuggestions(context.Background(), r.env) {
		if it.label == "history" {
			t.Errorf("pwsh was given the builtins of bash")
		}
	}
}

func TestThePathIndexReadsAChangedFolderAgain(t *testing.T) {
	r := newSuggestRig(t)
	has := func(name string) bool {
		for _, e := range r.sg.path.executables(context.Background(), r.env.get("PATH")) {
			if e.name == name {
				return true
			}
		}
		return false
	}
	if has("newtool") {
		t.Fatal("newtool is there from the start")
	}
	putFile(t, filepath.Join(r.bin1, "newtool"), 0o755)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(r.bin1, future, future); err != nil {
		t.Fatal(err)
	}
	if !has("newtool") {
		t.Errorf("a program added to a folder of PATH is not found")
	}
	// a request that was cancelled keeps nothing half-read
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	putFile(t, filepath.Join(r.bin2, "late"), 0o755)
	if err := os.Chtimes(r.bin2, future, future); err != nil {
		t.Fatal(err)
	}
	r.sg.path.executables(ctx, r.env.get("PATH"))
	if !has("late") {
		t.Errorf("a cancelled read left the folder out")
	}
}

func TestFileSuggestions(t *testing.T) {
	r := newSuggestRig(t)
	tests := []struct {
		name        string
		line        string
		files       bool
		onlyExec    bool
		want        []string
		wantNot     []string
		wantKinds   map[string]itemKind
		wantDetails map[string]string
	}{
		{
			name: "the folder of the shell", line: "cat ", files: true,
			want:      []string{".", "./a.txt", "./run.sh", "./sub/", "./.hidden", `./my\ file.txt`, "./link", "./linkdir/", "../", "~"},
			wantNot:   []string{"./c.go", "./sub/c.go"},
			wantKinds: map[string]itemKind{"./a.txt": kindFile, "./sub/": kindFolder, "./link": kindSymlinkFile, "./linkdir/": kindSymlinkFolder, ".": kindFolder},
			wantDetails: map[string]string{"./a.txt": filepath.Join(r.cwd, "a.txt"), "./sub/": filepath.Join(r.cwd, "sub") + "/", ".": r.cwd + "/", "~": r.home + "/",
				"./link": filepath.Join(r.cwd, "link") + " -> " + filepath.Join(r.cwd, "a.txt")},
		},
		{name: "only folders", line: "cd ", files: false, want: []string{".", "./sub/", "./linkdir/", "../", "~"}, wantNot: []string{"./a.txt", "./link"}},
		{name: "a folder typed", line: "cat sub/", files: true, want: []string{"./sub/", "./sub/c.go", "./sub/../"}, wantNot: []string{"~", "../"}},
		{name: "a folder typed with a dot", line: "cat ./sub/c", files: true, want: []string{"./sub/", "./sub/c.go", "./sub/../"}},
		{name: "the parent folder", line: "cat ../", files: true, want: []string{"../", "../work/", "../home/", "../../"}, wantNot: []string{"~"}},
		{name: "the home folder", line: "cat ~/", files: true, want: []string{"~/", "~/docs/", "~/top.txt"}, wantNot: []string{"~", "../"}},
		{name: "an absolute path", line: "cat " + r.home + "/d", files: true, want: []string{r.home + "/", r.home + "/docs/", r.home + "/top.txt"}},
		{name: "only programs where a command goes", line: "./", files: true, onlyExec: true, want: []string{"./", "./run.sh", "./sub/"}, wantNot: []string{"./a.txt", "./link"}},
		{name: "no escape inside quotes", line: `cat "my`, files: true, want: []string{"./my file.txt"}},
		{name: "a folder that does not exist", line: "cat nope/", files: true, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := newLineContext(tt.line, len(tt.line))
			items := fileSuggestions(context.Background(), r.env, lc, tt.files, true, tt.onlyExec)
			got := map[string]rawItem{}
			var labels []string
			for _, it := range items {
				got[it.label] = it
				labels = append(labels, it.label)
			}
			for _, w := range tt.want {
				if _, ok := got[w]; !ok {
					t.Errorf("%q is not offered; got %q", w, labels)
				}
			}
			for _, w := range tt.wantNot {
				if _, ok := got[w]; ok {
					t.Errorf("%q is offered", w)
				}
			}
			if tt.want == nil && len(items) != 0 {
				t.Errorf("got %q, want nothing", labels)
			}
			for label, kind := range tt.wantKinds {
				if got[label].kind != kind {
					t.Errorf("%q has kind %d, want %d", label, got[label].kind, kind)
				}
			}
			for label, detail := range tt.wantDetails {
				if got[label].detail != detail {
					t.Errorf("%q has detail %q, want %q", label, got[label].detail, detail)
				}
			}
		})
	}
}

func TestCdPathFolders(t *testing.T) {
	r := newSuggestRig(t)
	cdroot := filepath.Join(r.root, "cdroot")
	putFile(t, filepath.Join(cdroot, "project", "x"), 0o644)
	putFile(t, filepath.Join(cdroot, "afile"), 0o644)
	r.env.environ = append(r.env.environ, "CDPATH="+cdroot)
	items := fileSuggestions(context.Background(), r.env, newLineContext("cd ", 3), false, true, false)
	var details = map[string]string{}
	for _, it := range items {
		details[it.label] = it.detail
	}
	if got := details[cdroot+"/project/"]; got != "CDPATH" {
		t.Errorf("absolute CDPATH folder: %v", details)
	}
	if _, ok := details[cdroot+"/afile/"]; ok {
		t.Errorf("a file of CDPATH is offered")
	}
	r.env.cdPath = "relative"
	items = fileSuggestions(context.Background(), r.env, newLineContext("cd ", 3), false, true, false)
	found := false
	for _, it := range items {
		if it.label == "project/" && strings.HasPrefix(it.detail, "CDPATH ") {
			found = true
		}
	}
	if !found {
		t.Errorf("relative CDPATH folder not offered")
	}
	r.env.cdPath = "off"
	for _, it := range fileSuggestions(context.Background(), r.env, newLineContext("cd ", 3), false, true, false) {
		if strings.Contains(it.detail, "CDPATH") {
			t.Errorf("CDPATH is off but %q is offered", it.label)
		}
	}
	// CDPATH is for cd only
	for _, it := range fileSuggestions(context.Background(), r.env, newLineContext("ls ", 3), false, true, false) {
		if strings.Contains(it.detail, "CDPATH") {
			t.Errorf("CDPATH is used by ls")
		}
	}
}

func TestComplete(t *testing.T) {
	r := newSuggestRig(t)
	none := quickSettings{}
	tests := []struct {
		name     string
		line     string
		explicit bool
		quick    quickSettings
		want     []string
		wantNot  []string
	}{
		{"commands at the start", "gi", false, none, []string{"git", "gitk", "gimp", ".", "~"}, []string{"./a.txt"}},
		{"the subcommands of a spec", "git ", false, none, []string{"checkout", "commit"}, []string{"./a.txt", "--version"}},
		{"the options of a spec", "git commit -", false, none, []string{"--amend", "-m"}, []string{"checkout"}},
		{"files of a spec template", "cat ", false, none, []string{"./a.txt", "./sub/"}, nil},
		{"folders of cd", "cd ", false, none, []string{"./sub/"}, []string{"./a.txt"}},
		{"a command without spec: files are only a fallback", "gitk ", false, none, nil, []string{"./a.txt", "./sub/"}},
		{"the fallback when asked", "gitk ", true, none, []string{"./a.txt", "./sub/"}, nil},
		{"the fallback with unknown on", "gitk ", false, quickSettings{unknown: true}, []string{"./a.txt"}, nil},
		{"a command that is not installed has no spec", "npm ", false, none, nil, []string{"install"}},
		{"after a redirection", "ls > ", false, none, []string{"./a.txt"}, nil},
		{"after a pipe commands again", "ls | gi", false, none, []string{"git"}, nil},
		{"a value assigned in front of a command", "FOO=1 git ", false, none, []string{"commit"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := itemLabels(r.complete(tt.line, tt.explicit, tt.quick))
			for _, w := range tt.want {
				if !slices.Contains(got, w) {
					t.Errorf("%q is not offered", w)
				}
			}
			for _, w := range tt.wantNot {
				if slices.Contains(got, w) {
					t.Errorf("%q is offered", w)
				}
			}
		})
	}
}

func TestACommandThatTakesACommandOffersCommands(t *testing.T) {
	r := newSuggestRig(t)
	putFile(t, filepath.Join(r.bin1, "sudo"), 0o755)
	got := itemLabels(r.complete("sudo gi", false, quickSettings{}))
	for _, w := range []string{"git", "gitk"} {
		if !slices.Contains(got, w) {
			t.Errorf("%q is not offered after sudo", w)
		}
	}
}

func TestTheProvidersCanBeTurnedOff(t *testing.T) {
	r := newSuggestRig(t)
	r.env.history = []string{"git status", "git stash"}
	has := func(provider string) bool {
		for _, it := range r.complete("git s", true, quickSettings{}) {
			if it.provider == provider {
				return true
			}
		}
		return false
	}
	for _, id := range []string{providerCommands, providerHistory} {
		if !has(id) {
			t.Errorf("provider %s gives nothing for git s", id)
		}
		r.env.providers = map[string]bool{id: false}
		if has(id) {
			t.Errorf("provider %s is off but gives items", id)
		}
		r.env.providers = nil
	}
	r.env.providers = map[string]bool{providerFiles: false}
	for _, it := range r.complete("cat ", true, quickSettings{}) {
		if it.provider == providerFiles {
			t.Errorf("the path provider is off but gives %q", it.label)
		}
	}
}

func TestHistorySuggestions(t *testing.T) {
	env := suggestEnv{history: []string{"git status", "git stash pop", "git status", "ls -la", "git s", "echo a\nb", "gitk"}}
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"the lines that start with what was typed", "git s", []string{"git status", "git stash pop"}},
		{"a line is not offered for itself", "git status", nil},
		{"nothing typed", "", nil},
		{"only blanks", "  ", nil},
		{"not after a pipe", "ls | git s", nil},
		{"a line with a line break is left out", "echo a", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := itemLabels(historySuggestions(env, newLineContext(tt.line, len(tt.line))))
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0) {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
	// the most recent is first and they are at most ten
	many := suggestEnv{}
	for i := 0; i < 30; i++ {
		many.history = append(many.history, "make "+string(rune('a'+i%26))+strings.Repeat("x", i/26))
	}
	if got := historySuggestions(many, newLineContext("make", 4)); len(got) != 10 || got[0].label != "make a" {
		t.Errorf("history items: %d, first %v", len(got), got)
	}
}

func TestFilterItemsOrder(t *testing.T) {
	r := newSuggestRig(t)
	var m ui.SuggestMatcher
	rank := func(line string, explicit bool) []string {
		lc := newLineContext(line, len(line))
		items := filterItems(&m, r.complete(line, explicit, quickSettings{}), lc)
		var out []string
		for _, it := range items {
			out = append(out, it.label)
		}
		return out
	}
	t.Run("the best match of a command is first and the files are last", func(t *testing.T) {
		got := rank("gi", false)
		// the three programs score the same and are in alphabetical order (VS Code's tie-break)
		if !slices.Equal(got, []string{"gimp", "git", "gitk"}) {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("every item matches the word", func(t *testing.T) {
		for _, l := range rank("gi", false) {
			if !strings.Contains(strings.ToLower(l), "g") {
				t.Errorf("%q does not match gi", l)
			}
		}
	})
	t.Run("subcommands filtered by a prefix", func(t *testing.T) {
		got := rank("git che", false)
		if len(got) == 0 || got[0] != "checkout" || !slices.Contains(got, "cherry-pick") {
			t.Errorf("got %q", got)
		}
		if i, j := slices.Index(got, "checkout"), slices.Index(got, "cherry-pick"); i > j {
			t.Errorf("checkout is after cherry-pick: %q", got)
		}
	})
	t.Run("an empty word keeps the kinds apart", func(t *testing.T) {
		got := rank("cat ", true)
		if len(got) < 4 || got[0] != "./a.txt" {
			t.Fatalf("got %q", got)
		}
		// the labels made of punctuation are penalized: the folder itself, the parent and the home folder are last
		last := slices.Clone(got[len(got)-3:])
		slices.Sort(last)
		if !slices.Equal(last, []string{".", "../", "~"}) {
			t.Errorf("the last ones are %q", got[len(got)-3:])
		}
	})
	t.Run("files and folders are after the arguments", func(t *testing.T) {
		got := rank("git ", true)
		if len(got) == 0 || strings.HasPrefix(got[0], ".") {
			t.Errorf("got %q", got)
		}
	})
}

func TestFilterItemsKeepsThePositionsAndTheHistoryPrefix(t *testing.T) {
	var m ui.SuggestMatcher
	lc := newLineContext("git s", 5)
	items := []rawItem{
		{label: "status", kind: kindMethod},
		{label: "git status", kind: kindHistory, replaceCommand: true},
		{label: "git commit", kind: kindHistory, replaceCommand: true},
		{label: "log", kind: kindMethod},
	}
	got := filterItems(&m, items, lc)
	var labels []string
	for _, it := range got {
		labels = append(labels, it.label)
	}
	if !slices.Equal(labels, []string{"status", "git status"}) {
		t.Fatalf("got %q", labels)
	}
	if !slices.Equal(got[0].positions, []int{0}) {
		t.Errorf("status positions = %v", got[0].positions)
	}
	if !slices.Equal(got[1].positions, []int{0, 1, 2, 3, 4}) {
		t.Errorf("a line of the history is marked up to what was typed: %v", got[1].positions)
	}
}

func TestAcceptText(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		item     rawItem
		addSpace bool
		want     string
	}{
		{"the rest of a word", "gi", rawItem{label: "git"}, false, "t"},
		{"nothing to add", "git", rawItem{label: "git"}, false, ""},
		{"a trailing space", "gi", rawItem{label: "git"}, true, "t "},
		{"a folder has no trailing space", "cd su", rawItem{label: "sub/"}, true, "b/"},
		{"a match that is not a prefix replaces the word", "git cmt", rawItem{label: "commit"}, false, "\x7f\x7f\x7fcommit"},
		{"a path", "cat ./su", rawItem{label: "./sub/"}, false, "b/"},
		{"the word after the cursor is not touched", "git che", rawItem{label: "checkout"}, false, "ckout"},
		{"an insert text", "ec", rawItem{label: "echo", insert: "echo -n"}, false, "ho -n"},
		{"a line of the history replaces the command", "git s", rawItem{label: "git status", replaceCommand: true}, false, "tatus"},
		{"a line of the history after a pipe", "ls | git s", rawItem{label: "git status", replaceCommand: true}, false, "tatus"},
		{"a line of the history that is not the start of the typed", "git sx", rawItem{label: "git status", replaceCommand: true}, false, "\x7f\x7f\x7f\x7f\x7f\x7fgit status"},
		{"the multibyte characters are one Backspace each", "cat é", rawItem{label: "./éa"}, false, "\x7f./éa"},
		{"inside a quote the typed text is the start of the name", `cat "my`, rawItem{label: "my file.txt"}, false, " file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := newLineContext(tt.line, len(tt.line))
			if got := acceptText(sugItem{rawItem: tt.item}, lc, tt.addSpace); got != tt.want {
				t.Errorf("acceptText = %q, want %q", got, tt.want)
			}
		})
	}
}
