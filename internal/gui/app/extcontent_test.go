package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// The documents an extension provides, and the diffs and documents it opens with them (step 24.7).

// contentRig is an extension that provides the documents of the scheme "tst" from a map, and the app it runs in.
type contentRig struct {
	*groupApp
	api extension.API
	reg extension.ContentRegistration

	// ran are the arguments of the commands of the editor title menu that ran.
	ran []any

	mu    sync.Mutex
	texts map[string]string
	// asked counts the calls of Text by URI.
	asked map[string]int
}

// text is what the provider makes for a URI: the text in the map; "BINARY" is a binary file, "FAIL" a failure, "PANIC" a panic.
func (r *contentRig) text(_ context.Context, uri string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked[uri]++
	switch t := r.texts[uri]; t {
	case "BINARY":
		return "", errors.New(extension.TagContentBinary, "the file looks binary", "uri", uri)
	case "FAIL":
		return "", errors.New("RigFailure", "the rig failed on purpose", "uri", uri)
	case "PANIC":
		panic("the rig panics on purpose")
	default:
		return t, nil
	}
}

func (r *contentRig) set(uri, text string) {
	r.mu.Lock()
	r.texts[uri] = text
	r.mu.Unlock()
}

func tst(path string) string { return extension.ContentURI("tst", path) }

// factory makes the extension "emetos.contenttest" over the rig.
func (r *contentRig) factory() extension.Factory {
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: "emetos.contenttest", Name: "Content test", ActivationEvents: []string{extension.OnStartup},
				Contributes: extension.Contributes{
					Commands: []extension.CommandSpec{{ID: "tst.open", Title: "Open File", Icon: "go-to-file"}, {ID: "tst.other", Title: "Other"}},
					Menus: []extension.MenuItem{
						{Menu: extension.MenuEditorTitle, Command: "tst.open", Group: "navigation", When: "isInDiffEditor && resourceScheme == file"},
						{Menu: extension.MenuEditorTitle, Command: "tst.other", Group: "2_more", When: "resourceScheme == tst"},
					},
				},
			},
			activate: func(_ context.Context, api extension.API) error {
				r.api = api
				for _, id := range []string{"tst.open", "tst.other"} {
					if err := api.Commands().Handle(id, func(_ context.Context, args any) error { r.ran = append(r.ran, args); return nil }); err != nil {
						return err
					}
				}
				reg, err := api.Documents().ProvideContent(extension.ContentProvider{
					Scheme: "tst",
					Label: func(uri string) string {
						_, rest, _ := extension.SplitContentURI(uri)
						return rest[strings.LastIndex(rest, "/")+1:]
					},
					Text: r.text,
				})
				r.reg = reg
				return err
			},
		}
	}
}

func newContentRig(t *testing.T, names ...string) *contentRig {
	t.Helper()
	r := &contentRig{texts: map[string]string{}, asked: map[string]int{}}
	p := extensionProcess(t, r.factory())
	w, err := NewWorkspace(context.Background(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.release)
	r.groupApp = newGroupAppWith(t, w, testWindowSize, names, nil)
	if r.api == nil || r.reg == nil {
		t.Fatal("the extension did not register its provider")
	}
	return r
}

// activeInput is the editor of the active group.
func (r *contentRig) activeInput() editorgroup.EditorInput {
	return r.shell().activeGroup().Model.ActiveEditor()
}

// diffs are the diff inputs of the active group.
func (r *contentRig) diffs() []*editorgroup.DiffInput {
	var out []*editorgroup.DiffInput
	for _, in := range r.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
		if d, ok := in.(*editorgroup.DiffInput); ok {
			out = append(out, d)
		}
	}
	return out
}

func (r *contentRig) waitTabs(what string, n int) {
	r.t.Helper()
	waitFor(r.t, r.ts, what, func() bool { return len(r.tabs()) == n })
}

func (r *contentRig) notifications() []notify.Item { return r.win.Notify.Items() }

func TestAProvidedDocumentOpensInADiffEditorWithItsTitle(t *testing.T) {
	r := newContentRig(t, "a.go")
	r.set(tst("id/HEAD/a.go"), "package x\n// old\n")
	file := document.FileURI(r.files["a.go"]).String()
	if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: tst("id/HEAD/a.go"), Right: file, Title: "a.go (Working Tree)", Preview: true}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the diff", 1)
	ds := r.diffs()
	if len(ds) != 1 {
		t.Fatalf("%d diff editors", len(ds))
	}
	d := ds[0]
	if got := r.tabNames(); !slices.Equal(got, []string{"a.go (Working Tree)"}) {
		t.Errorf("tabs %v", got)
	}
	if got := d.Original().Text(); got != "package x\n// old\n" {
		t.Errorf("original %q", got)
	}
	if !d.Original().ReadOnly() || d.Original().Dirty() {
		t.Errorf("the provided side is read only %v and never dirty %v", d.Original().ReadOnly(), d.Original().Dirty())
	}
	if d.Modified().URI().Path != r.files["a.go"] {
		t.Errorf("modified %v", d.Modified().URI())
	}
	// a preview: not kept
	if m := r.shell().activeGroup().Model; m.IsPinned(d) {
		t.Error("the diff is kept, want a preview")
	}
	// opening it again shows the same tab, and asks the provider again
	before := r.asked[tst("id/HEAD/a.go")]
	r.set(tst("id/HEAD/a.go"), "package x\n// older\n")
	if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: tst("id/HEAD/a.go"), Right: file, Title: "a.go (Working Tree)"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "the text again", func() bool { return d.Original().Text() == "package x\n// older\n" })
	if n := len(r.tabs()); n != 1 {
		t.Errorf("%d tabs after opening the same diff again", n)
	}
	if r.asked[tst("id/HEAD/a.go")] <= before {
		t.Error("the provider was not asked again")
	}
	r.noErrors()
}

func TestAKeptDiffAndTheTwoSidesOfADocumentAreTwoTabs(t *testing.T) {
	r := newContentRig(t, "a.go")
	file := document.FileURI(r.files["a.go"]).String()
	r.set(tst("id/~/a.go"), "one\n")
	r.set(tst("id//a.go"), "two\n")
	for _, left := range []string{tst("id/~/a.go"), tst("id//a.go")} {
		if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: left, Right: file}); err != nil {
			t.Fatal(err)
		}
	}
	r.waitTabs("two diffs", 2)
	for _, d := range r.diffs() {
		if !r.shell().activeGroup().Model.IsPinned(d) {
			t.Errorf("%s is a preview, want kept", d.Name())
		}
	}
	// the same diff of two files with no title is named after them
	if got := r.tabNames(); !slices.Equal(got, []string{"a.go ↔ a.go", "a.go ↔ a.go"}) {
		t.Errorf("tabs %v", got)
	}
}

func TestAProvidedDocumentOpensOnItsOwnWithATitleAndASelection(t *testing.T) {
	r := newContentRig(t)
	r.set(tst("id/HEAD/a.go"), "one\ntwo\nthree\n")
	sel := &extension.Selection{Anchor: extension.Position{Line: 1, Col: 1}, Active: extension.Position{Line: 1, Col: 3}}
	if err := r.api.Documents().OpenDocument(extension.OpenSpec{URI: tst("id/HEAD/a.go"), Title: "a.go (Deleted)", Preview: true, Selection: sel}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the document", 1)
	if got := r.tabNames(); !slices.Equal(got, []string{"a.go (Deleted)"}) {
		t.Errorf("tabs %v", got)
	}
	in, ok := r.activeInput().(*editorgroup.TextInput)
	if !ok || !in.Document().ReadOnly() {
		t.Fatalf("active %T", r.activeInput())
	}
	waitFor(t, r.ts, "the selection", func() bool {
		s := r.vm().Cursors().Selections()
		return len(s) == 1 && s[0].Anchor.Line == 1 && s[0].Anchor.Col == 1 && s[0].Active.Col == 3
	})
}

func TestTheProvidedTextChangesWhenTheProviderSaysSo(t *testing.T) {
	r := newContentRig(t, "a.go")
	r.set(tst("id//a.go"), "v1\n")
	if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: tst("id//a.go"), Right: document.FileURI(r.files["a.go"]).String()}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the diff", 1)
	d := r.diffs()[0]
	r.set(tst("id//a.go"), "v2\n")
	// a URI that is not open, and one of another scheme, are skipped
	r.reg.Changed(tst("id/HEAD/other.go"), "file:///x", "not a uri", tst("id//a.go"))
	waitFor(t, r.ts, "the new text", func() bool { return d.Original().Text() == "v2\n" })
	r.noErrors()
}

func TestAProvidedDocumentThatCannotBeMadeIsReportedAndNothingOpens(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"a binary file", "BINARY", BinaryFileNotice},
		{"a failure", "FAIL", "Cannot open a.go (Working Tree): the rig failed on purpose"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newContentRig(t, "a.go")
			r.set(tst("id/HEAD/a.go"), tc.text)
			file := document.FileURI(r.files["a.go"]).String()
			if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: tst("id/HEAD/a.go"), Right: file, Title: "a.go (Working Tree)"}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, r.ts, "the notification", func() bool { return len(r.notifications()) > 0 })
			if msg := r.notifications()[0].Message; !strings.Contains(msg, tc.want) {
				t.Errorf("notification %q, want %q in it", msg, tc.want)
			}
			if n := len(r.tabs()); n != 0 {
				t.Errorf("%d tabs", n)
			}
			if n := len(r.win.Documents.All()); n != 0 {
				t.Errorf("%d documents are open", n)
			}
		})
	}
}

func TestAPanicInTheProviderFailsTheExtensionAndOpensNothing(t *testing.T) {
	r := newContentRig(t, "a.go")
	r.set(tst("id/HEAD/a.go"), "PANIC")
	if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: tst("id/HEAD/a.go"), Right: document.FileURI(r.files["a.go"]).String()}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "the notification", func() bool { return len(r.notifications()) > 0 })
	waitFor(t, r.ts, "the extension to fail", func() bool {
		return r.win.Extensions().State("emetos.contenttest") == exthost.StateFailed
	})
	if n := len(r.tabs()); n != 0 {
		t.Errorf("%d tabs", n)
	}
	// the provider is gone with the extension
	if r.win.virtual.HasPrefix("/tst/") {
		t.Error("the provider is still registered")
	}
}

func TestOpeningWithTheWrongArgumentsFailsWithTheirTags(t *testing.T) {
	r := newContentRig(t, "a.go")
	file := document.FileURI(r.files["a.go"]).String()
	for _, tc := range []struct {
		name string
		call func() error
		tag  string
	}{
		{"no provider", func() error {
			return r.api.Documents().OpenDiff(extension.DiffSpec{Left: extension.ContentURI("nope", "x"), Right: file})
		}, "ExtContentUnknown"},
		{"an empty side", func() error { return r.api.Documents().OpenDiff(extension.DiffSpec{Left: file}) }, "ExtensionDiffInvalid"},
		{"no URI", func() error { return r.api.Documents().OpenDocument(extension.OpenSpec{}) }, "ExtensionDocumentInvalid"},
		{"a URI that does not parse", func() error { return r.api.Documents().OpenDocument(extension.OpenSpec{URI: "x"}) }, "URIInvalid"},
		{"an untitled document that is not open", func() error { return r.api.Documents().OpenDocument(extension.OpenSpec{URI: "untitled:9"}) }, "ExtContentUnknown"},
		{"the scheme again", func() error {
			_, err := r.api.Documents().ProvideContent(extension.ContentProvider{Scheme: "tst", Text: r.text})
			return err
		}, "ExtContentSchemeTaken"},
		{"a scheme of the core", func() error {
			_, err := r.api.Documents().ProvideContent(extension.ContentProvider{Scheme: "file", Text: r.text})
			return err
		}, "ExtensionContentSchemeInvalid"},
		{"a scheme with capitals", func() error {
			_, err := r.api.Documents().ProvideContent(extension.ContentProvider{Scheme: "Git", Text: r.text})
			return err
		}, "ExtensionContentSchemeInvalid"},
		{"no text function", func() error {
			_, err := r.api.Documents().ProvideContent(extension.ContentProvider{Scheme: "other"})
			return err
		}, "ExtensionContentProviderInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			appErr, ok := errors.FromError(tc.call())
			if !ok || appErr.Tag != tc.tag {
				t.Fatalf("got %v, want the tag %s", appErr, tc.tag)
			}
			if !strings.Contains(appErr.Route(), tc.tag) {
				t.Errorf("route %s", appErr.Route())
			}
		})
	}
	// after the provider is disposed the scheme is free and its documents are unknown
	r.reg.Dispose()
	if err := r.api.Documents().OpenDocument(extension.OpenSpec{URI: tst("id/HEAD/a.go")}); err == nil {
		t.Error("a document of a disposed provider opened")
	}
	reg, err := r.api.Documents().ProvideContent(extension.ContentProvider{Scheme: "tst", Text: r.text})
	if err != nil {
		t.Fatal(err)
	}
	reg.Dispose()
	reg.Dispose() // twice is fine
}

func TestTabsListTheTextAndDiffEditorsAndCloseThem(t *testing.T) {
	r := newContentRig(t, "a.go", "b.go")
	r.set(tst("id//a.go"), "x\n")
	docs := r.api.Documents()
	if err := docs.OpenDiff(extension.DiffSpec{Left: tst("id//a.go"), Right: document.FileURI(r.files["a.go"]).String()}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the diff", 1)
	r.show("b.go", false)
	r.edit("b.go")
	tabs := docs.Tabs()
	if len(tabs) != 2 {
		t.Fatalf("tabs %+v", tabs)
	}
	if tabs[0].Kind != "diff" || tabs[0].Original != tst("id//a.go") || tabs[0].Modified != document.FileURI(r.files["a.go"]).String() || tabs[0].Dirty {
		t.Errorf("first %+v", tabs[0])
	}
	if tabs[1].Kind != "text" || tabs[1].URI != document.FileURI(r.files["b.go"]).String() || !tabs[1].Dirty {
		t.Errorf("second %+v", tabs[1])
	}
	if n := docs.CloseTabs(tabs[0], extension.DocumentTab{ID: "9:gone"}); n != 1 {
		t.Errorf("closed %d, want 1", n)
	}
	r.waitTabs("one tab", 1)
	if got := r.tabNames(); !slices.Equal(got, []string{"b.go"}) {
		t.Errorf("tabs %v", got)
	}
	// a dirty tab asks before it closes
	docs.CloseTabs(docs.Tabs()...)
	r.pump()
	if n := len(r.tabs()); n != 1 {
		t.Errorf("the dirty tab closed without asking: %d tabs", n)
	}
}

func TestTheTitleActionsOfTheEditorTitleMenuGetTheResourceAndTheContext(t *testing.T) {
	r := newContentRig(t, "a.go")
	file := document.FileURI(r.files["a.go"]).String()
	r.set(tst("id//a.go"), "x\n")
	if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: tst("id//a.go"), Right: file}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the diff", 1)
	s := r.shell()
	ctx := s.groupContext(r.win, s.activeGroup())
	for key, want := range map[string]any{"isInDiffEditor": true, "resourceScheme": "file"} {
		if got := ctx.Value(key); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if got := s.resourceOfGroup(s.activeGroup()); got != file {
		t.Errorf("resource %q", got)
	}
	// a provided document has the scheme of its provider, and no diff context
	r.set(tst("id/HEAD/b.go"), "y\n")
	if err := r.api.Documents().OpenDocument(extension.OpenSpec{URI: tst("id/HEAD/b.go")}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the document", 2)
	ctx = s.groupContext(r.win, s.activeGroup())
	if got := ctx.Value("resourceScheme"); got != "tst" {
		t.Errorf("resourceScheme = %v, want tst", got)
	}
	if got := ctx.Value("isInDiffEditor"); got == true {
		t.Error("isInDiffEditor is set for a text editor")
	}
}

func TestASavedDiffOfAProvidedDocumentIsDroppedWhenTheProviderIsGone(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.command("dev.openDiff", map[string]any{"original": document.VirtualURI(DefaultSettingsPath).String(), "modified": "main.go"})
	first.waitDiff()
	first.close()
	saved, _ := env.savedWindow(env.root)
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	// the same diff, with the original a version of a file that nothing provides any more
	env.writeWindowState(env.root, strings.ReplaceAll(string(raw), "/defaultSettings.json", "/git/0123456789ab/HEAD/main.go"))
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("groups %v", got)
	}
	if n := len(second.win.Notify.Items()); n != 0 {
		t.Errorf("%d notifications", n)
	}
}

func TestTheEditorTitleMenuShowsIconsAndAnOverflowAndRunsWithTheResource(t *testing.T) {
	r := newContentRig(t, "a.go")
	file := document.FileURI(r.files["a.go"]).String()
	r.set(tst("id//a.go"), "x\n")
	if err := r.api.Documents().OpenDiff(extension.DiffSpec{Left: tst("id//a.go"), Right: file}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the diff", 1)
	s := r.shell()
	group := s.activeGroup()
	args := map[string]any{"groupId": float64(group.ID()), "uri": s.resourceOfGroup(group)}
	icons, overflow := s.titleMenuActions(r.win, s.groupContext(r.win, group), args)
	if len(icons) != 1 || icons[0].Command != "tst.open" || icons[0].Icon == 0 || icons[0].Tooltip != "Open File" || len(overflow) != 0 {
		t.Fatalf("diff editor: icons %+v overflow %+v", icons, overflow)
	}
	s.runTitleAction(r.win, icons[0].Command, icons[0].Args)
	if len(r.ran) != 1 {
		t.Fatalf("ran %v", r.ran)
	}
	if m, _ := r.ran[0].(map[string]any); m["uri"] != file || m["groupId"] != float64(group.ID()) {
		t.Errorf("the command got %v, want the group and the modified side %s", r.ran[0], file)
	}
	// a provided document: no icon, the other item in the "..." menu
	r.set(tst("id/HEAD/b.go"), "y\n")
	if err := r.api.Documents().OpenDocument(extension.OpenSpec{URI: tst("id/HEAD/b.go")}); err != nil {
		t.Fatal(err)
	}
	r.waitTabs("the document", 2)
	icons, overflow = s.titleMenuActions(r.win, s.groupContext(r.win, group), args)
	if len(icons) != 0 || len(overflow) != 1 || overflow[0].Command != "tst.other" {
		t.Errorf("provided document: icons %+v overflow %+v", icons, overflow)
	}
	// the strip draws them: a button for the icon
	if fn := s.titleActions(r.win, group); fn == nil {
		t.Error("the group has no title actions")
	}
}
