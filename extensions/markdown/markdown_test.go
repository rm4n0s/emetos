package markdown

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rig is the extension activated over the fake API, with a folder that holds a Markdown file and what it links to.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	folder string
	doc    *fakeapi.Document
	ext    *md
}

func newRig(t *testing.T, text string) *rig {
	t.Helper()
	folder := t.TempDir()
	for name, content := range map[string]string{"README.md": text, "docs/guide.md": "# Guide", "docs/pic.png": "not really a picture"} {
		path := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := New()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	if err := e.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	doc := fakeapi.NewDocument("file://"+filepath.Join(folder, "README.md"), "markdown", folder, text)
	return &rig{t: t, api: api, folder: folder, doc: doc, ext: e.(*md)}
}

// open makes the preview of the document, as a tab does when it shows the view.
func (r *rig) open() extension.FileViewContent {
	r.t.Helper()
	c, err := r.api.OpenFileView(PreviewView, r.doc)
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

func TestTheManifestDeclaresThePreviewAndItsActionAndKeepsTheLanguagePack(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	c := m.Contributes
	if c.Languages == nil || len(c.FileViews) != 1 || c.FileViews[0].ID != PreviewView || !slices.Equal(c.FileViews[0].Languages, []string{"markdown"}) {
		t.Errorf("languages %v, file views %+v", c.Languages != nil, c.FileViews)
	}
	if len(c.EditorActions) != 1 || c.EditorActions[0].Command != OpenPreviewToSideCommand || c.EditorActions[0].When != "resourceLangId == markdown" {
		t.Errorf("editor actions %+v", c.EditorActions)
	}
	if len(c.Commands) != 1 || c.Commands[0].ID != OpenPreviewToSideCommand || c.Commands[0].Title != "Open Preview to the Side" {
		t.Errorf("commands %+v", c.Commands)
	}
	if m.ID != ID || !slices.Contains(m.ActivationEvents, extension.OnStartup) {
		t.Errorf("id %q events %v", m.ID, m.ActivationEvents)
	}
}

func TestOpenPreviewToTheSideOpensTheDocumentOfTheGroupOfTheIconInThePreview(t *testing.T) {
	r := newRig(t, "# Title")
	if err := r.api.Run(OpenPreviewToSideCommand, map[string]any{"groupId": float64(2)}); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(OpenPreviewToSideCommand, nil); err != nil {
		t.Fatal(err)
	}
	want := []fakeapi.FileViewOpen{{View: PreviewView, Side: true, Group: 2}, {View: PreviewView, Side: true}}
	if got := r.api.FileViewOpens(); !slices.Equal(got, want) {
		t.Errorf("the window was asked %+v, want %+v", got, want)
	}
}

func TestThePreviewFollowsTheEditsAndTheScrollOfTheTextAndLetsGoWhenItEnds(t *testing.T) {
	r := newRig(t, "# First")
	content := r.open()
	if change, scroll := r.doc.Listeners(); change != 1 || scroll != 1 {
		t.Fatalf("listeners: %d change, %d scroll", change, scroll)
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 200}, widgets.NewBuilder(content.Build))
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("First", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the heading is not shown")
	}
	r.doc.SetText("# Second")
	if r.api.FileViewRefreshes() != 1 {
		t.Errorf("an edit asked for %d refreshes, want 1", r.api.FileViewRefreshes())
	}
	again := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 200}, widgets.NewBuilder(content.Build))
	again.Pump()
	if !again.Exists(gluttertest.ByText("Second", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the preview does not show the new text")
	}
	r.doc.Scroll(5) // no view is laid out for the scroll to find: it waits, without failing
	content.Close()
	if change, scroll := r.doc.Listeners(); change != 0 || scroll != 0 {
		t.Errorf("after Close: %d change, %d scroll listeners", change, scroll)
	}
}

func TestLinksGoToTheDesktopOrOpenFilesOfTheWorkspace(t *testing.T) {
	r := newRig(t, "x")
	r.open()
	cases := []struct {
		name, dest string
		uris       []string
		files      []string
		notes      int
	}{
		{"a web address", "https://example.com/a?b=c", []string{"https://example.com/a?b=c"}, nil, 0},
		{"a mail address", "mailto:me@example.com", []string{"mailto:me@example.com"}, nil, 0},
		{"a file next to the document", "docs/guide.md#top", nil, []string{filepath.Join(r.folder, "docs", "guide.md")}, 0},
		{"a file from the root of the folder", "/docs/guide.md", nil, []string{filepath.Join(r.folder, "docs", "guide.md")}, 0},
		{"a place in the document", "#intro", nil, nil, 0},
		{"a file that is not there", "docs/missing.md", nil, nil, 1},
		{"a file outside the folder", "../../etc/passwd", nil, nil, 1},
		{"another scheme", "ftp://example.com/x", nil, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.doc.Opened = nil
			notes := len(r.api.Notes())
			opened := len(r.api.OpenedURIs())
			r.ext.openLink(r.doc, tc.dest)
			r.api.WaitFor("the link to be followed", func() bool { return len(r.api.OpenedURIs()) >= opened+len(tc.uris) })
			if got := r.api.OpenedURIs(); len(tc.uris) > 0 && !slices.Equal(got[len(got)-len(tc.uris):], tc.uris) {
				t.Errorf("opened %v, want %v", got, tc.uris)
			}
			if !slices.Equal(r.doc.Opened, tc.files) {
				t.Errorf("files %v, want %v", r.doc.Opened, tc.files)
			}
			if got := len(r.api.Notes()) - notes; got != tc.notes {
				t.Errorf("%d notifications, want %d", got, tc.notes)
			}
		})
	}
}

func TestImagesAreLocalFilesOfTheFolder(t *testing.T) {
	r := newRig(t, "x")
	cases := []struct {
		dest string
		want string
	}{
		{"docs/pic.png", filepath.Join("docs", "pic.png")},
		{"/docs/pic.png?size=1", filepath.Join("docs", "pic.png")},
		{"https://example.com/pic.png", ""},
		{"docs/none.png", ""},
		{"../pic.png", ""},
		{"docs", ""},
	}
	for _, tc := range cases {
		got := localImage(r.doc, tc.dest)
		if tc.want == "" && got != "" || tc.want != "" && got != filepath.Join(r.folder, tc.want) {
			t.Errorf("localImage(%q) = %q, want %q", tc.dest, got, tc.want)
		}
	}
	// a document that is not a file has no folder to look in
	if got := localImage(fakeapi.NewDocument("untitled:1", "markdown", "", "x"), "docs/pic.png"); got != "" {
		t.Errorf("an untitled document found %q", got)
	}
}

func TestCodeIsHighlightedThroughTheLanguagesOfTheEditor(t *testing.T) {
	r := newRig(t, "x")
	r.api.SetTokenizer(func(id, text string) ([][]extension.Token, error) {
		return [][]extension.Token{{{Start: 0, End: len(text), Scopes: []string{"source." + id, "keyword"}}}}, nil
	})
	if lines, ok := r.ext.tokenize("go", "func"); !ok || len(lines) != 1 || lines[0][0].Scopes[1] != "keyword" {
		t.Errorf("tokenize = %v, %v", lines, ok)
	}
	r.api.SetTokenizer(func(string, string) ([][]extension.Token, error) { return nil, os.ErrInvalid })
	if _, ok := r.ext.tokenize("go", "func"); ok {
		t.Error("a failing tokenizer made a highlighted block")
	}
	if _, ok := r.ext.tokenize("", "x"); ok {
		t.Error("an empty language was highlighted")
	}
}

func TestGoldenThePreviewOfAFile(t *testing.T) {
	text := "# Notes\n\nSome *emphasis* and a [link](https://example.com).\n\n- one\n- two\n\n```go\nx := 1\n```\n"
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, text)
			content := r.open()
			ts := uitest.Pump(t, th, gui.Size{Width: 420, Height: 300}, widgets.NewBuilder(content.Build))
			for range 3 {
				ts.Pump()
			}
			uitest.MatchesGolden(ts, "markdown_preview_"+th.Name)
		})
	}
}
