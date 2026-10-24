package quickinput

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// fakeFiles stands for the window: the files and recent files it knows, a search that can be held back,
// and what the provider asked it to do.
type fakeFiles struct {
	mu       sync.Mutex
	recent   []string
	files    []string
	complete bool
	// gates hold a search until the test opens its gate, by filter.
	gates    map[string]chan struct{}
	searched []string
	// cancelled are the filters whose search saw its context cancelled.
	cancelled []string
	err       error

	opened    []FileOpen
	removed   []string
	errs      []error
	pinned    bool
	direction string
	notify    func()
	hasIcons  bool
}

func newFakeFiles(files ...string) *fakeFiles {
	return &fakeFiles{files: files, complete: true, pinned: true, direction: "right", gates: map[string]chan struct{}{}}
}

func nameOf(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }

func dirOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return ""
}

func (f *fakeFiles) hit(path string, query string) (workspace.QuickOpenHit, bool) {
	h := workspace.QuickOpenHit{Path: "/work/" + path, Name: nameOf(path), Description: dirOf(path)}
	if query == "" {
		return h, true
	}
	if !strings.Contains(strings.ToLower(path), strings.ToLower(query)) {
		return h, false
	}
	if i := strings.Index(strings.ToLower(nameOf(path)), strings.ToLower(query)); i >= 0 {
		h.LabelMatch = []fuzzy.Match{{Start: i, End: i + len(query)}}
	}
	return h, true
}

func (f *fakeFiles) search(ctx context.Context, filter string) (workspace.QuickOpenResult, error) {
	f.mu.Lock()
	f.searched = append(f.searched, filter)
	gate := f.gates[filter]
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			f.mu.Lock()
			f.cancelled = append(f.cancelled, filter)
			f.mu.Unlock()
			return workspace.QuickOpenResult{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return workspace.QuickOpenResult{}, f.err
	}
	res := workspace.QuickOpenResult{Complete: f.complete}
	for _, p := range f.recent {
		if h, ok := f.hit(p, filter); ok {
			res.Recent = append(res.Recent, h)
		}
	}
	if filter != "" {
		for _, p := range f.files {
			if slices.Contains(f.recent, p) {
				continue
			}
			if h, ok := f.hit(p, filter); ok {
				res.Files = append(res.Files, h)
			}
		}
	}
	return res, nil
}

func (f *fakeFiles) options() FilesOptions {
	o := FilesOptions{
		Search: f.search,
		OnChange: func(fn func()) func() {
			f.mu.Lock()
			f.notify = fn
			f.mu.Unlock()
			return func() {
				f.mu.Lock()
				f.notify = nil
				f.mu.Unlock()
			}
		},
		Open:          func(req FileOpen) { f.opened = append(f.opened, req) },
		OpenPinned:    func() bool { return f.pinned },
		SideDirection: func() string { return f.direction },
		RemoveRecent:  func(p string) { f.removed = append(f.removed, p) },
		OnError:       func(err error) { f.errs = append(f.errs, err) },
	}
	if f.hasIcons {
		th, err := theme.BundledFileIconTheme("vs-seti")
		if err != nil {
			panic(err)
		}
		o.Icon = func(path, name string, typ theme.Type) (theme.FileIcon, bool) {
			// the window gives the language of the file; the Seti theme names its icons by language
			lang := map[string]string{".go": "go", ".md": "markdown"}[filepath.Ext(name)]
			return th.IconFor(theme.FileIconQuery{Kind: theme.FileIconFile, Name: name, LanguageID: lang, Type: typ})
		}
	}
	return o
}

// filesRig is a rig with the file mode registered as the default mode.
func filesRig(t *testing.T, f *fakeFiles, size ui.Size, colors kit.Colors) *rig {
	t.Helper()
	r := newRig(t, size, colors)
	r.registry.MustRegister(NewFilesProvider(f.options()))
	return r
}

// waitRows pumps frames until the rows satisfy a condition (the search runs on a goroutine).
func (r *rig) waitRows(what string, cond func(rows []Row) bool) {
	r.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		r.ts.Pump(5 * time.Millisecond)
		r.drain()
		if cond(r.ctl.Picker().Rows()) {
			r.settle()
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s; rows %v", what, r.labels())
		}
		time.Sleep(time.Millisecond)
	}
}

func rowLabels(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Item.Label
	}
	return out
}

func labelsAre(want ...string) func([]Row) bool {
	return func(rows []Row) bool { return slices.Equal(rowLabels(rows), want) }
}

var projectFiles = []string{"main.go", "go.mod", "internal/server/server.go", "internal/server/util.go", "docs/guide.md", "docs/server.md"}

func TestFilesProviderIsTheDefaultMode(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	if p := r.ctl.Provider(); p == nil || p.Info().Prefix != "" {
		t.Fatalf("provider %v", p)
	}
	if got := r.ctl.Picker().Placeholder(); got != FilesPlaceholder {
		t.Errorf("placeholder %q", got)
	}
	if r.scopeKey(FilesContextKey) != true {
		t.Errorf("%s = %v", FilesContextKey, r.scopeKey(FilesContextKey))
	}
	// the other modes still win on their prefix
	r.show(">", ShowOptions{})
	if r.ctl.Provider().Info().Prefix != ">" {
		t.Errorf("provider %q for >", r.ctl.Provider().Info().Prefix)
	}
	if r.scopeKey(FilesContextKey) == true {
		t.Error("inFilesPicker stays on in the commands mode")
	}
}

func TestFilesProviderListsGroupsAsTheUserTypes(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	f.recent = []string{"docs/guide.md", "internal/server/util.go"}
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	// an empty filter lists the recently opened files, under their heading
	r.waitRows("the recent files", labelsAre("guide.md", "util.go"))
	rows := r.ctl.Picker().Rows()
	if rows[0].Separator != FilesRecentSeparator || rows[1].Separator != "" {
		t.Errorf("separators %q %q", rows[0].Separator, rows[1].Separator)
	}
	if rows[0].Item.Description != "docs" || rows[0].Item.Label != "guide.md" {
		t.Errorf("first row %+v", rows[0].Item)
	}
	if r.ts.Exists(gluttertest.ByText(FilesRecentSeparator)) == false {
		t.Error("the heading is not drawn")
	}

	r.typeText("server")
	r.waitRows("the results of server", labelsAre("util.go", "server.go", "server.md"))
	rows = r.ctl.Picker().Rows()
	if rows[0].Separator != FilesRecentSeparator || rows[1].Separator != FilesResultsSeparator || !rows[1].Border {
		t.Errorf("separators %q %q border %v", rows[0].Separator, rows[1].Separator, rows[1].Border)
	}
	if len(rows[1].Label) == 0 {
		t.Error("the file results carry their highlights")
	}
	if active := r.ctl.Picker().ActiveIndex(); active != 0 {
		t.Errorf("the first row is not active: %d", active)
	}
	r.typeText("zzz")
	r.waitRows("no results", labelsAre(FilesNoResults))
	if _, ok := r.ctl.Picker().Active(); !ok {
		t.Error("the no results row is not active") // VS Code makes it the active (inert) item
	}
}

func TestFilesProviderShowsNothingForAnEmptyListAndWhileIndexing(t *testing.T) {
	f := newFakeFiles()
	f.complete = false
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.settle()
	if len(r.ctl.Picker().Rows()) != 0 {
		t.Errorf("rows %v with nothing recent", r.labels())
	}
	r.typeText("abc")
	r.waitRows("the no results row", labelsAre(FilesNoResults))
	if d := r.ctl.Picker().Rows()[0].Item.Description; !strings.Contains(d, "Still looking") {
		t.Errorf("description %q while the folder is listed", d)
	}
}

func TestFilesProviderNewestKeystrokeWins(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	slow, fast := make(chan struct{}), make(chan struct{})
	f.gates["g"], f.gates["gu"] = slow, fast
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.waitRows("the start", func(rows []Row) bool { return true })
	r.typeText("g") // search for "g" is held back
	r.typeText("u") // search for "gu" is held back too
	close(fast)     // the newest finishes first
	r.waitRows("the results of gu", labelsAre("guide.md"))
	close(slow) // the older one finishes last and must not replace the rows
	for range 20 {
		r.ts.Pump(5 * time.Millisecond)
		r.drain()
		time.Sleep(time.Millisecond)
	}
	if got := r.labels(); !reflect.DeepEqual(got, []string{"guide.md"}) {
		t.Errorf("rows %v: an older search replaced the newest", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.cancelled, "g") {
		t.Errorf("the search of the older keystroke was not cancelled: %v", f.cancelled)
	}
}

func TestFilesProviderKeepsRowsWhileTheNextSearchRuns(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	gate := make(chan struct{})
	f.gates["serv"] = gate
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.typeText("ser")
	r.waitRows("ser", func(rows []Row) bool { return len(rows) == 3 })
	before := r.labels()
	r.typeText("v")
	for range 10 {
		r.ts.Pump(5 * time.Millisecond)
		r.drain()
	}
	if got := r.labels(); !reflect.DeepEqual(got, before) {
		t.Errorf("rows %v changed before the search was done (were %v)", got, before)
	}
	close(gate)
	r.waitRows("the finished search", func(rows []Row) bool { return len(rows) == 3 })
}

func TestFilesProviderSearchesAgainWhenTheFilesChange(t *testing.T) {
	f := newFakeFiles("a/server.go")
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.typeText("server")
	r.waitRows("the first result", labelsAre("server.go"))
	f.mu.Lock()
	f.files = append(f.files, "b/server_test.go")
	notify := f.notify
	f.mu.Unlock()
	if notify == nil {
		t.Fatal("the provider did not subscribe to the changes of the files")
	}
	go notify() // the index tells from the goroutine of its walk
	r.waitRows("the grown list", labelsAre("server.go", "server_test.go"))
	r.ctl.Hide()
	r.settle()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notify != nil {
		t.Error("the provider still listens after the picker closed")
	}
}

func TestFilesProviderOpensFiles(t *testing.T) {
	tests := []struct {
		name   string
		pinned bool // the setting: kept by default
		mods   []services.LogicalKeyboardKey
		want   FileOpen
	}{
		{"Enter keeps the editor by default", true, nil, FileOpen{Path: "/work/main.go", Pinned: true}},
		{"Enter opens a preview when the setting is on", false, nil, FileOpen{Path: "/work/main.go"}},
		{"Ctrl+Enter opens to the side by default", true, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyControl},
			FileOpen{Path: "/work/main.go", Side: true, Pinned: true}},
		{"Ctrl+Enter keeps the preview editor when the setting is on", false, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyControl},
			FileOpen{Path: "/work/main.go", Pinned: true}},
		{"Alt+Enter opens to the side", false, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyAlt},
			FileOpen{Path: "/work/main.go", Side: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeFiles(projectFiles...)
			f.pinned = tc.pinned
			r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
			r.show("", ShowOptions{})
			r.typeText("main")
			r.waitRows("main", labelsAre("main.go"))
			for _, k := range tc.mods {
				r.ts.SendKeyDownEvent(k)
			}
			r.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
			for _, k := range tc.mods {
				r.ts.SendKeyUpEvent(k)
			}
			r.settle()
			if r.ctl.Visible() {
				t.Error("the picker stays open")
			}
			if !slices.Equal(f.opened, []FileOpen{tc.want}) {
				t.Errorf("opened %+v, want %+v", f.opened, tc.want)
			}
			if r.primary() != r.editor {
				t.Errorf("focus %v, want the editor", r.primary().DebugLabel())
			}
		})
	}
}

func TestFilesProviderPositionSuffix(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.typeText("main.go")
	r.waitRows("main.go", labelsAre("main.go"))
	searches := func() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.searched) }
	before := searches()
	// the position after the name does not search again
	r.typeText(":12:5")
	r.settle()
	if got := searches(); got != before {
		t.Errorf("the position made %d more searches", got-before)
	}
	r.press(services.LogicalKeyboardKeyEnter)
	if want := []FileOpen{{Path: "/work/main.go", Pinned: true, Line: 12, Column: 5}}; !slices.Equal(f.opened, want) {
		t.Errorf("opened %+v, want %+v", f.opened, want)
	}
	// each notation of the position
	for _, tc := range []struct {
		typed     string
		line, col int
	}{{"main.go:7", 7, 1}, {"main.go#9", 9, 1}, {"main.go(3,4)", 3, 4}, {"main.go:", 1, 1}, {"main.go", 0, 0}} {
		f.opened = nil
		r.show(tc.typed, ShowOptions{PreserveValue: true})
		r.waitRows(tc.typed, labelsAre("main.go"))
		r.press(services.LogicalKeyboardKeyEnter)
		if len(f.opened) != 1 || f.opened[0].Line != tc.line || f.opened[0].Column != tc.col {
			t.Errorf("%q: opened %+v, want %d:%d", tc.typed, f.opened, tc.line, tc.col)
		}
	}
}

func TestFilesProviderButtons(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	f.recent = []string{"docs/guide.md"}
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.waitRows("recent", labelsAre("guide.md"))
	row := r.ctl.Picker().Rows()[0]
	if len(row.Item.Buttons) != 2 || row.Item.Buttons[0].Tooltip != "Open to the Side" || row.Item.Buttons[0].Icon != fonts.CodiconSplitHorizontal ||
		row.Item.Buttons[1].Tooltip != "Remove from Recently Opened" {
		t.Fatalf("buttons %+v", row.Item.Buttons)
	}
	// remove from the recent files
	r.ctl.Trigger(row, 1, KeyMods{})
	if !slices.Equal(f.removed, []string{"/work/docs/guide.md"}) || len(r.ctl.Picker().Items()) != 1 {
		t.Errorf("removed %v, items %v", f.removed, len(r.ctl.Picker().Items()))
	}
	// open to the side from a file result: the picker closes, then the file opens
	r.typeText("main")
	r.waitRows("main", labelsAre("main.go"))
	row = r.ctl.Picker().Rows()[0]
	if len(row.Item.Buttons) != 1 {
		t.Fatalf("a file result has %d buttons, want only the one to the side", len(row.Item.Buttons))
	}
	r.ctl.Trigger(row, 0, KeyMods{})
	r.settle()
	if r.ctl.Visible() {
		t.Error("the picker stays open")
	}
	if want := []FileOpen{{Path: "/work/main.go", Side: true, Pinned: true}}; !slices.Equal(f.opened, want) {
		t.Errorf("opened %+v, want %+v", f.opened, want)
	}
	// the direction of the split changes the button
	f.direction = "down"
	r.show("", ShowOptions{})
	r.typeText("util")
	r.waitRows("util", labelsAre("util.go"))
	if b := r.ctl.Picker().Rows()[0].Item.Buttons[0]; b.Icon != fonts.CodiconSplitVertical || b.Tooltip != "Open to the Bottom" {
		t.Errorf("button %+v", b)
	}
}

func TestFilesProviderReportsFailures(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	f.err = errors.New("SearchBoom", "boom")
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.typeText("main")
	deadline := time.Now().Add(10 * time.Second)
	for len(f.errs) == 0 && time.Now().Before(deadline) {
		r.ts.Pump(5 * time.Millisecond)
		r.drain()
		time.Sleep(time.Millisecond)
	}
	if len(f.errs) == 0 {
		t.Fatal("the failure was not reported")
	}
}

func TestFilesProviderStopsItsSearchWhenTheSessionEnds(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	f.gates["main"] = make(chan struct{})
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("main", ShowOptions{PreserveValue: true})
	r.ctl.Hide()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.mu.Lock()
		n := len(f.cancelled)
		f.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the search was not cancelled when the picker closed")
		}
		r.ts.Pump(5 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

func TestFilesProviderIsInTheHelpAndTakesAnInitialText(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("?", ShowOptions{})
	found := false
	for _, row := range r.ctl.Picker().Rows() {
		if row.Item.Description == "Go to File" {
			found = true
			if row.Item.Keybinding != "Ctrl+P" {
				t.Errorf("key %q", row.Item.Keybinding)
			}
		}
	}
	if !found {
		t.Errorf("the help does not list the file mode: %v", r.labels())
	}
	// an initial text is searched, and kept as it is
	r.show("server", ShowOptions{PreserveValue: true})
	r.waitRows("server", func(rows []Row) bool { return len(rows) == 3 })
	if r.ctl.Picker().Value() != "server" {
		t.Errorf("value %q", r.ctl.Picker().Value())
	}
}

func TestFilesProviderFileIcons(t *testing.T) {
	f := newFakeFiles(projectFiles...)
	f.hasIcons = true
	f.recent = []string{"main.go"}
	r := filesRig(t, f, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("", ShowOptions{})
	r.waitRows("recent", labelsAre("main.go"))
	row := r.ctl.Picker().Rows()[0]
	if row.Item.FileIcon == nil {
		t.Fatal("no icon")
	}
	icon, ok := row.Item.FileIcon(theme.TypeDark)
	if !ok || icon.Glyph == 0 || icon.Family == "" {
		t.Errorf("icon %+v %v", icon, ok)
	}
}

func TestGoldenQuickOpen(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(f *fakeFiles) (value string)
	}{
		{"files", func(f *fakeFiles) string {
			f.recent = []string{"internal/server/util.go", "docs/guide.md"}
			return "ser"
		}},
		{"recent", func(f *fakeFiles) string {
			f.recent = []string{"internal/server/server.go", "docs/guide.md", "main.go"}
			return ""
		}},
		{"none", func(f *fakeFiles) string { return "zzzq" }},
		{"no_folder", func(f *fakeFiles) string {
			f.files = nil
			f.recent = []string{"/home/me/notes/todo.md"}
			return ""
		}},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				f := newFakeFiles("main.go", "go.mod", "internal/server/server.go", "internal/server/util.go", "docs/guide.md", "docs/server.md", "docs/more/service.md")
				f.hasIcons = true
				value := tc.prepare(f)
				r := filesRig(t, f, ui.Size{Width: 720, Height: 360}, th.colors())
				r.show(value, ShowOptions{PreserveValue: value != ""})
				want := 1
				switch tc.name {
				case "files":
					want = 4
				case "recent":
					want = 3
				}
				r.waitRows(tc.name, func(rows []Row) bool { return len(rows) == want })
				testutil.MatchesGolden(r.ts, "quickopen_"+tc.name+"_"+th.name)
			})
		}
	}
}
