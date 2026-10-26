package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// editAt tells the extension which file the user has open and what is selected in it.
func (r *rig) editAt(file string, sel ...extension.Selection) {
	r.t.Helper()
	uri := "file://" + filepath.ToSlash(filepath.Join(r.folder, file))
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "go"})
	if len(sel) > 0 {
		r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: sel})
	}
}

// userMessages are the messages the fake program received, as the program got them.
func (r *rig) userMessages() []string {
	r.t.Helper()
	f, err := os.Open(r.log)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		var rec struct {
			Kind string `json:"kind"`
			Msg  struct {
				Type    string `json:"type"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"msg"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Kind == "in" && rec.Msg.Type == "user" {
			out = append(out, rec.Msg.Message.Content)
		}
	}
	return out
}

func sel(l1, c1, l2, c2 int) extension.Selection {
	return extension.Selection{Anchor: extension.Position{Line: l1, Col: c1}, Active: extension.Position{Line: l2, Col: c2}}
}

func TestAddSelectionInsertsTheReferenceOfTheSelection(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.input.SetValue("look at")
	r.editAt("util/util.go", sel(9, 4, 11, 2))
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := tab.input.Value(); got != "look at @util/util.go#L10-12 " {
		t.Errorf("the input is %q", got)
	}
	if !r.api.EditorTabs()[0].Revealed {
		t.Error("the session tab was not shown")
	}
	// a backwards selection ending at the start of a line does not include that line; a cursor is one line
	r.editAt("main.go", sel(4, 0, 2, 5))
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.editAt("main.go", sel(7, 3, 7, 3))
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.editAt("main.go", sel(2, 0, 5, 0))
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := tab.input.Value(); got != "look at @util/util.go#L10-12 @main.go#L3-4 @main.go#L8 @main.go#L3-5 " {
		t.Errorf("the input is %q", got)
	}
}

func TestAddSelectionGoesToTheSessionThatHadTheFocusAndOpensOneWhenThereIsNone(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	r.editAt("main.go", sel(0, 0, 1, 3))
	// no session: one is opened (it starts its program) and gets the reference
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.ext.tabs) != 1 || r.ext.tabs[0].input.Value() != "@main.go#L1-2 " {
		t.Fatalf("tabs = %d", len(r.ext.tabs))
	}
	a := r.ext.tabs[0]
	r.ready(a)
	b := r.open()
	r.ready(b)
	// the input of a had the focus last
	r.ext.active = a
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if a.input.Value() != "@main.go#L1-2 @main.go#L1-2 " || b.input.Value() != "" {
		t.Errorf("a %q b %q", a.input.Value(), b.input.Value())
	}
	// closing it, the other one is the target
	a.tab.Close()
	r.ext.wait()
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if b.input.Value() != "@main.go#L1-2 " {
		t.Errorf("b %q", b.input.Value())
	}
}

func TestAddSelectionWithoutAnEditorOrWithAFileThatIsNotAllowedTellsTheUser(t *testing.T) {
	r := newRig(t, "")
	r.openIdle("")
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if n := r.api.Notes(); len(n) != 1 || !strings.Contains(n[0].Message, "Open a file") {
		t.Fatalf("notes = %+v", n)
	}
	for _, file := range []string{".env", "secrets/key.txt"} {
		r.editAt(file, sel(0, 0, 0, 2))
		if err := r.api.Run(AddSelectionCommand, nil); err != nil {
			t.Fatal(err)
		}
		n := r.api.Notes()
		if last := n[len(n)-1]; last.Severity != "warn" || !strings.Contains(last.Message, file) || !strings.Contains(last.Message, "read deny rule") {
			t.Errorf("%s: notes = %+v", file, n)
		}
	}
	if got := r.ext.tabs[0].input.Value(); got != "" {
		t.Errorf("a denied file was added: %q", got)
	}
	// a document that is not a file
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "untitled:1"})
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if n := r.api.Notes(); !strings.Contains(n[len(n)-1].Message, "not a file") {
		t.Errorf("notes = %+v", n)
	}
}

func TestAFileOutsideTheFolderIsReferredToByItsAbsolutePath(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	other := t.TempDir()
	uri := "file://" + filepath.ToSlash(filepath.Join(other, "x.go"))
	r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: []extension.Selection{sel(0, 0, 0, 1)}})
	if err := r.api.Run(AddSelectionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := tab.input.Value(); got != "@"+filepath.ToSlash(filepath.Join(other, "x.go"))+"#L1 " {
		t.Errorf("the input is %q", got)
	}
	// it is not attached to messages, though: only files of the folder are
	if note := r.ext.activeFileNote(context.Background(), "hi"); note != "" {
		t.Errorf("note = %q", note)
	}
}

func TestTheActiveFileGoesWithEachMessageWhenTheSettingIsOn(t *testing.T) {
	r := newRig(t, "turns.jsonl")
	tab := r.open()
	r.ready(tab)
	r.editAt("main.go")
	r.send(tab, "what does it do?")
	r.idle(tab)
	r.api.SetSetting(AttachSetting, false)
	r.send(tab, "and now?")
	r.idle(tab)
	r.api.SetSetting(AttachSetting, true)
	r.editAt(".env")
	r.send(tab, "third")
	r.idle(tab)
	r.editAt("util/util.go")
	r.send(tab, "see @util/util.go please")
	r.idle(tab)
	want := []string{
		"what does it do?\n\n(The file open in the editor: @main.go)",
		"and now?",
		"third",
		"see @util/util.go please",
	}
	if got := r.userMessages(); !slices.Equal(got, want) {
		t.Errorf("the program got\n%q\nwant\n%q", got, want)
	}
	// the conversation shows what the user typed, not the note
	if got := texts(tab)[0]; got != "user:what does it do?" {
		t.Errorf("the conversation shows %q", got)
	}
}

func TestMatchGlobFollowsTheStyleOfTheIgnoreFiles(t *testing.T) {
	for _, tt := range []struct {
		pattern, path string
		want          bool
	}{
		{".env", ".env", true},
		{".env", "app/.env", true},
		{".env", "app/env", false},
		{".env.*", ".env.local", true},
		{"*.pem", "certs/site.pem", true},
		{"*.pem", "certs/site.pem.txt", false},
		{"secrets/**", "secrets/a/b.txt", true},
		{"secrets/**", "mysecrets/a.txt", false},
		{"secrets", "secrets/a/b.txt", true},
		{"**/secret.txt", "secret.txt", true},
		{"**/secret.txt", "a/b/secret.txt", true},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/x/a.go", false},
		{"a?c", "abc", true},
		{"a?c", "a/c", false},
		{".aws/credentials", ".aws/credentials", true},
		{".aws/credentials", "x/.aws/credentials", false},
		{"", "a", false},
	} {
		t.Run(tt.pattern+" "+tt.path, func(t *testing.T) {
			if got := matchGlob(tt.pattern, tt.path); got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestReadDenyRulesTakesTheReadEntriesOfTheFolder(t *testing.T) {
	got := readDenyRules(`{"permissions":{"deny":["Read(./.env)","Read(./secrets/**)","Bash(curl:*)","Read(//etc/passwd)","Read(~/.ssh/**)","Read(**/*.key)","Read()","Read(x"]}}`)
	want := []string{".env", "secrets/**", "**/*.key"}
	if !slices.Equal(got, want) {
		t.Errorf("rules = %q, want %q", got, want)
	}
	for _, bad := range []string{``, `{`, `[]`, `{"permissions":{"deny":3}}`} {
		if got := readDenyRules(bad); len(got) != 0 {
			t.Errorf("rules of %q = %q", bad, got)
		}
	}
}

func TestTheDenyRulesOfTheProjectAreRead(t *testing.T) {
	r := newRig(t, "")
	ctx := context.Background()
	for file, want := range map[string]bool{
		".env": true, "config/.env.local": true, "secrets/key.txt": true, "secrets/deeper/x.txt": true, "main.go": false, "util/util.go": false,
		"keys/server.pem": true, "id_rsa": true,
	} {
		if got := r.ext.denied(ctx, file); got != want {
			t.Errorf("denied(%q) = %v, want %v", file, got, want)
		}
	}
	// the file of the project is read from the folder only: a folder with none has the built-in list
	if err := os.Remove(filepath.Join(r.folder, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	if r.ext.denied(ctx, "secrets/key.txt") || !r.ext.denied(ctx, ".env") {
		t.Error("without the settings file of the project only the built-in list applies")
	}
}

func TestLineRange(t *testing.T) {
	for _, tt := range []struct {
		name string
		sel  extension.Selection
		want string
	}{
		{"a cursor", sel(4, 2, 4, 2), "#L5"},
		{"one line", sel(4, 2, 4, 9), "#L5"},
		{"several lines", sel(4, 2, 8, 1), "#L5-9"},
		{"backwards", sel(8, 1, 4, 2), "#L5-9"},
		{"to the start of a line", sel(4, 0, 8, 0), "#L5-8"},
		{"whole line", sel(4, 0, 5, 0), "#L5"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := lineRange(tt.sel); got != tt.want {
				t.Errorf("lineRange = %q, want %q", got, tt.want)
			}
		})
	}
}
