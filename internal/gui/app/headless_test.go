package app

import (
	"context"
	"image"
	"image/color"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// viewRig is a keyApp with a headless view of its workspace.
type viewRig struct {
	*keyApp
	view   *headlessView
	frames chan *image.RGBA
}

// newViewRig makes a window with a document and a headless view of it of the size, whose pictures are kept in frames (the
// latest only, as the view itself keeps the latest). The least time between two frames of the view is 0: a test pumps the
// window and the frame is there.
func newViewRig(t *testing.T, text string, size extension.Size) *viewRig {
	t.Helper()
	a := newKeyApp(t, text)
	// the font of the interface is the test font, as in the other pictures of the tests; the view takes it from the window
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	v, err := a.win.makeHeadlessView(size, headlessPacing{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	r := &viewRig{keyApp: a, view: v, frames: make(chan *image.RGBA, 1)}
	v.OnFrame(func(img *image.RGBA) {
		select { // the latest picture replaces the one nobody took
		case <-r.frames:
		default:
		}
		r.frames <- img
	})
	return r
}

// pump runs the window and the view for a few frames.
func (r *viewRig) pump() {
	for range 4 {
		r.ts.Pump(20 * time.Millisecond)
	}
}

// frame pumps until the view hands over a picture.
func (r *viewRig) frame(t testing.TB) *image.RGBA {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r.pump()
		select {
		case img := <-r.frames:
			return img
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatal("the view drew nothing")
	return nil
}

// key sends a key to the view the way a client's keyboard does.
func (r *viewRig) key(logical services.LogicalKeyboardKey, physical services.PhysicalKeyboardKey, text string) {
	r.view.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Physical: physical.USBHidUsage(), Logical: logical.KeyID(), Text: text})
	r.view.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Physical: physical.USBHidUsage(), Logical: logical.KeyID()})
	r.pump()
}

// chord presses a chord in the view.
func (r *viewRig) chord(c keybinding.Chord) {
	type mod struct {
		on       bool
		logical  services.LogicalKeyboardKey
		physical services.PhysicalKeyboardKey
	}
	mods := []mod{
		{c.Ctrl, services.LogicalKeyboardKeyControlLeft, services.PhysicalKeyboardKeyControlLeft},
		{c.Shift, services.LogicalKeyboardKeyShiftLeft, services.PhysicalKeyboardKeyShiftLeft},
		{c.Alt, services.LogicalKeyboardKeyAltLeft, services.PhysicalKeyboardKeyAltLeft},
	}
	send := func(kind extension.KeyKind, logical services.LogicalKeyboardKey, physical services.PhysicalKeyboardKey) {
		r.view.SendKey(extension.KeyEvent{Kind: kind, Physical: physical.USBHidUsage(), Logical: logical.KeyID()})
	}
	for _, m := range mods {
		if m.on {
			send(extension.KeyDown, m.logical, m.physical)
		}
	}
	send(extension.KeyDown, logicalOf(c.Key), canonicalPhysical[c.Key])
	send(extension.KeyUp, logicalOf(c.Key), canonicalPhysical[c.Key])
	for i := len(mods) - 1; i >= 0; i-- {
		if mods[i].on {
			send(extension.KeyUp, mods[i].logical, mods[i].physical)
		}
	}
	r.pump()
}

// viewText is the text of the document of the editor in front in the view.
func (r *viewRig) viewText() string {
	if e := r.view.ws.shell.activeEditor(); e != nil {
		return e.doc.Text()
	}
	return ""
}

func differs(a, b *image.RGBA) bool {
	if a.Rect != b.Rect {
		return true
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return true
		}
	}
	return false
}

// colors counts the different colors of a picture.
func colors(img *image.RGBA) int {
	seen := map[color.RGBA]bool{}
	for i := 0; i < len(img.Pix); i += 4 {
		seen[color.RGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}] = true
	}
	return len(seen)
}

func TestTheViewDrawsTheWorkspaceAtItsSize(t *testing.T) {
	r := newViewRig(t, "alpha\nbeta\n", extension.Size{Width: 800, Height: 500})
	img := r.frame(t)
	if img.Rect.Dx() != 800 || img.Rect.Dy() != 500 {
		t.Fatalf("the picture is %v", img.Rect)
	}
	if n := colors(img); n < 8 {
		t.Errorf("the picture has %d colors: the view drew a blank", n)
	}
	if r.viewText() != "alpha\nbeta\n" {
		t.Errorf("the editor of the view shows %q", r.viewText())
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors in the window: %v", errs)
	}
}

func TestAnEditInTheWindowShowsInTheView(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 800, Height: 500})
	before := r.frame(t)
	// typed in the window: the document is shared, so the view draws it
	r.ts.SendKeyEvent('x', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyX, Character: "x"})
	if r.text() != "xalpha\n" {
		t.Fatalf("the window did not take the key: %q", r.text())
	}
	after := r.frame(t)
	if r.viewText() != "xalpha\n" {
		t.Errorf("the view shows %q", r.viewText())
	}
	if !differs(before, after) {
		t.Error("the picture of the view did not change with the edit")
	}
	// and the other way round
	r.key('y', services.PhysicalKeyboardKeyKeyY, "y")
	if r.text() != "yxalpha\n" && r.text() != "xyalpha\n" {
		t.Errorf("an edit in the view is not in the window: %q", r.text())
	}
}

func TestKeysTypedInTheViewReachTheDocumentThroughItsFocus(t *testing.T) {
	r := newViewRig(t, "", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	for _, c := range "hey" {
		r.key(services.LogicalKeyboardKey(c), canonicalPhysical[keybinding.KeyA], string(c))
	}
	if got := r.viewText(); got != "hey" {
		t.Fatalf("the view took %q", got)
	}
	if got := r.text(); got != "hey" {
		t.Errorf("the document the window shows has %q", got)
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTheViewHasItsOwnCursorAndFocus(t *testing.T) {
	r := newViewRig(t, "one\ntwo\nthree\n", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	windowCursor := r.selections()
	r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.End}) // ctrl+end in the view: the end of the document
	viewSel := r.view.ws.shell.activeEditor().view.Cursors().Selections()
	if len(viewSel) != 1 || viewSel[0].Active.Line != 3 {
		t.Fatalf("the cursor of the view is %v", viewSel)
	}
	if got := r.selections(); len(got) != 1 || got[0] != windowCursor[0] {
		t.Errorf("the cursor of the window moved with the view's: %v, was %v", got, windowCursor)
	}
	// the focus is its own: a chord of the window's workbench does not reach the view's and the other way round
	if err := r.win.Commands.Run(commandContext(t, r.win), "workbench.action.toggleSidebarVisibility", nil); err != nil {
		t.Fatal(err)
	}
	r.pump()
	if r.win.Layout.State().SideBarVisible == r.view.ws.Layout.State().SideBarVisible {
		t.Error("the layout of the view follows the window's")
	}
}

func TestTheViewResizes(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 800, Height: 500})
	if img := r.frame(t); img.Rect.Dx() != 800 || img.Rect.Dy() != 500 {
		t.Fatalf("first picture %v", img.Rect)
	}
	if err := r.view.Resize(extension.Size{Width: 1000, Height: 640}); err != nil {
		t.Fatal(err)
	}
	if got := r.view.Size(); got != (extension.Size{Width: 1000, Height: 640}) {
		t.Errorf("Size is %v", got)
	}
	var img *image.RGBA
	for i := 0; i < 5; i++ {
		img = r.frame(t)
		if img.Rect.Dx() == 1000 {
			break
		}
	}
	if img.Rect.Dx() != 1000 || img.Rect.Dy() != 640 {
		t.Fatalf("the picture after the resize is %v", img.Rect)
	}
	err := r.view.Resize(extension.Size{Width: 0, Height: 10})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("headlessView.Resize.ExtensionHeadlessSize") {
		t.Errorf("unexpected failure path: %v", err)
	}
}

func TestClosingTheViewFreesWhatItHolds(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	doc := r.view.ws.shell.activeEditor().doc
	refs := r.win.Documents.refs[doc]
	if refs < 2 {
		t.Fatalf("the document has %d users with two views open", refs)
	}
	r.view.Close()
	r.view.Close() // twice is nothing
	r.pump()
	select {
	case <-r.view.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the view did not end")
	}
	if got := r.win.Documents.refs[doc]; got != refs-1 {
		t.Errorf("the document has %d users after the view closed, want %d", got, refs-1)
	}
	if len(r.win.views) != 0 {
		t.Errorf("the window still lists %d views", len(r.win.views))
	}
	if r.view.ws.shell != nil {
		t.Error("the shell of the view is still there")
	}
	// the window is untouched
	r.ts.SendKeyEvent('x', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyX, Character: "x"})
	if r.text() != "xalpha\n" {
		t.Errorf("the window broke: %q", r.text())
	}
	// input to a closed view is ignored
	r.view.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Logical: 'q', Text: "q"})
	r.pump()
	if r.text() != "xalpha\n" {
		t.Errorf("a closed view still types: %q", r.text())
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// commandContext is the context the tests run commands in.
func commandContext(t testing.TB, w *Workspace) command.Context {
	t.Helper()
	return command.Context{Ctx: t.Context(), Keys: w.Keys}
}

// projectRig is a viewRig over a folder that has the project of testdata/headless, with main.go open in front.
func newProjectRig(t testing.TB, size extension.Size, theme string) *viewRig {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"workbench.colorTheme": theme, "files.hotExit": "off", "window.menuBarVisibility": "classic"})
	root := filepath.Join(t.TempDir(), "project")
	copyTree(t, filepath.Join(testutil.RepoRoot(t), "internal", "gui", "app", "testdata", "headless", "project"), root)
	win, err := NewWorkspace(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(context.Background(), filepath.Join(root, "main.go"), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, ui.Size{Width: 1280, Height: 800})
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	ts.Pump(10 * time.Millisecond)
	a := &keyApp{win: win, ts: ts}
	v, err := win.makeHeadlessView(size, headlessPacing{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	r := &viewRig{keyApp: a, view: v, frames: make(chan *image.RGBA, 1)}
	v.OnFrame(func(img *image.RGBA) {
		select {
		case <-r.frames:
		default:
		}
		r.frames <- img
	})
	return r
}

func copyTree(t testing.TB, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTheCommandPaletteOpensInTheViewOnly(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 900, Height: 560})
	before := r.frame(t)
	r.chord(keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyP})
	if !r.view.ws.QuickInput.Visible() {
		t.Fatal("the shortcut did not open the quick input of the view")
	}
	if r.win.QuickInput.Visible() {
		t.Error("the quick input of the window opened with the view's shortcut")
	}
	img := r.frame(t)
	if !differs(before, img) {
		t.Error("the picture of the view does not show the palette")
	}
	// the palette is a text field: what the client types goes into it
	for _, c := range "toggle" {
		r.key(services.LogicalKeyboardKey(c), canonicalPhysical[keybinding.KeyA], string(c))
	}
	if got := r.view.ws.QuickInput.Picker().Value(); got != ">toggle" {
		t.Errorf("the quick input of the view holds %q", got)
	}
	r.key(services.LogicalKeyboardKeyEscape, services.PhysicalKeyboardKeyEscape, "")
	if r.view.ws.QuickInput.Visible() {
		t.Error("Escape did not close the palette")
	}
}

func TestADialogOfTheViewTakesTheKeysOfItsClient(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 900, Height: 560})
	r.frame(t)
	answered := make(chan int, 1)
	r.view.ws.Dialogs.Show(kit.DialogSpec{Type: kit.DialogInfo, Message: "Pick", Buttons: []string{"&&OK"}, OnAnswer: func(res kit.DialogResult) { answered <- res.Button }})
	r.pump()
	img := r.frame(t)
	if colors(img) < 8 {
		t.Error("the dialog is not drawn")
	}
	r.key(services.LogicalKeyboardKeyEnter, services.PhysicalKeyboardKeyEnter, "")
	select {
	case <-answered:
	default:
		r.pump()
		select {
		case <-answered:
		default:
			t.Fatal("Enter did not answer the dialog")
		}
	}
}

func TestAClickAndAScrollOfTheViewMoveItsEditorOnly(t *testing.T) {
	r := newProjectRig(t, extension.Size{Width: 1000, Height: 500}, "Dark Modern")
	r.frame(t)
	e := r.view.ws.shell.activeEditor()
	if e == nil {
		t.Fatal("the view has no editor")
	}
	windowEditor := r.win.shell.activeEditor()
	scrolled := windowEditor.view.ScrollTop()
	// a wheel over the editor scrolls the view's editor
	for range 5 {
		r.view.SendScroll(extension.ScrollEvent{X: 600, Y: 250, DY: 120})
		r.pump()
	}
	if got := e.view.ScrollTop(); got <= 0 {
		t.Errorf("the view did not scroll: %v", got)
	}
	if got := windowEditor.view.ScrollTop(); got != scrolled {
		t.Errorf("the scroll of the window's editor moved: %v, was %v", got, scrolled)
	}
	// a click puts the cursor of the view where it is
	was := windowEditor.view.Cursors().Selections()
	r.view.SendPointer(extension.PointerEvent{Kind: extension.PointerMove, X: 600, Y: 150})
	r.view.SendPointer(extension.PointerEvent{Kind: extension.PointerDown, Buttons: extension.ButtonPrimary, X: 600, Y: 150})
	r.view.SendPointer(extension.PointerEvent{Kind: extension.PointerUp, X: 600, Y: 150})
	r.pump()
	sel := e.view.Cursors().Selections()
	if len(sel) != 1 || sel[0].Active.Line == 0 {
		t.Errorf("the click did not move the cursor of the view: %v", sel)
	}
	if got := windowEditor.view.Cursors().Selections(); len(got) != 1 || got[0] != was[0] {
		t.Errorf("the cursor of the window moved: %v, was %v", got, was)
	}
}

func TestCopyAndPasteGoThroughTheClipboardOfTheView(t *testing.T) {
	r := newViewRig(t, "alpha beta\n", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	copied := make(chan string, 4)
	r.view.OnCopy(func(text string) { copied <- text })
	r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyA}) // select all
	r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyC})
	select {
	case got := <-copied:
		if got != "alpha beta\n" {
			t.Errorf("the copy sent %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a copy in the view did not reach OnCopy")
	}
	// the text of the client's clipboard is what a paste takes
	r.view.SetClipboard("from the client")
	r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyV})
	if got := r.viewText(); got != "from the client" {
		t.Errorf("the paste gave %q", got)
	}
}

func TestTheWindowClosingClosesItsViews(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	r.win.closeHeadlessViews()
	r.pump()
	select {
	case <-r.view.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the view outlived its workspace")
	}
	if n := len(r.win.views); n != 0 {
		t.Errorf("%d views are left", n)
	}
}

func TestAnotherFolderClosesTheViewsOfTheOldOne(t *testing.T) {
	r := newProjectRig(t, extension.Size{Width: 800, Height: 500}, "Dark Modern")
	r.frame(t)
	if err := r.win.SetFolder(t.Context(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	r.pump()
	select {
	case <-r.view.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the view of the old folder stayed")
	}
}

func TestAViewOfAViewOrOfAWorkspaceThatIsNotOnScreenIsRefused(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	_, err := r.view.ws.newHeadlessView(extension.Size{Width: 100, Height: 100})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("Workspace.makeHeadlessView.ExtensionHeadlessUnavailable") {
		t.Errorf("a view of a view: %v", err)
	}
	win := newTestWorkspace(t, "")
	_, err = win.newHeadlessView(extension.Size{Width: 100, Height: 100})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("Workspace.makeHeadlessView.ExtensionHeadlessUnavailable") {
		t.Errorf("a workspace with no shell: %v", err)
	}
}

func TestACommandThatActsOnEmetosItselfIsNotBoundInTheView(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	tabs := r.win.Process.Workspaces.Count()
	for _, id := range []string{"workbench.action.quit", "workbench.action.newWindow", "workbench.action.closeWindow", "emetos.workspaceTab.next"} {
		if err := r.view.ws.Commands.Run(commandContext(t, r.view.ws), id, nil); err == nil {
			t.Errorf("%s ran in the view", id)
		}
	}
	r.pump()
	if got := r.win.Process.Workspaces.Count(); got != tabs {
		t.Errorf("the view changed the tabs of the window: %d, was %d", got, tabs)
	}
	if r.ts.ExitRequests() != 0 || r.view.eng.host.exits != 0 {
		t.Errorf("the view asked the application to exit")
	}
}

func TestTheZoomOfAViewIsItsOwn(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	before := r.win.ZoomLevel()
	if err := r.view.ws.Commands.Run(commandContext(t, r.view.ws), "workbench.action.zoomIn", nil); err != nil {
		t.Fatal(err)
	}
	if r.view.ws.ZoomLevel() != before+1 {
		t.Errorf("the zoom of the view is %v", r.view.ws.ZoomLevel())
	}
	if r.win.ZoomLevel() != before {
		t.Errorf("the zoom of the window changed to %v", r.win.ZoomLevel())
	}
}

func TestSaveAsInAViewAsksForThePathInTheQuickInput(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	_, err := r.view.ws.SaveDialog(t.Context(), nil, SaveDialogOptions{})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("noFileChooser.FileChooserUnavailable") || !dialogUnavailable(err) {
		t.Errorf("unexpected failure path: %v", err)
	}
	err = r.view.ws.OpenURI(t.Context(), nil, "https://example.com")
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("noOpenURI.HeadlessOpenURIUnavailable") {
		t.Errorf("unexpected failure path: %v", err)
	}
}

func TestTheRasterizerIsMadeAgainBeforeItKeepsManyScenes(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 320, Height: 200})
	scene := func() *ui.Scene {
		r.frame(t)
		return r.view.eng.lastScene
	}
	s := scene()
	rast := &rasterizer{log: slog.Default()}
	t.Cleanup(rast.reset)
	var first *gluttertest.Tester
	for i := range 3 * rasterRecycle {
		img, err := rast.render(s, 320, 200)
		if err != nil || img.Rect.Dx() != 320 {
			t.Fatalf("render %d: %v", i, err)
		}
		if i == 0 {
			first = rast.tester
		}
		if i == rasterRecycle && rast.tester == first {
			t.Error("the tester was not replaced after rasterRecycle frames")
		}
		if rast.n > rasterRecycle {
			t.Fatalf("the rasterizer holds %d scenes", rast.n)
		}
	}
}

func TestAViewNobodyTypesInMakesNoFrames(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	r.pump()
	frames := r.view.eng.frames
	end := time.Now().Add(1200 * time.Millisecond) // a blink would come twice
	for time.Now().Before(end) {
		r.ts.Pump(20 * time.Millisecond)
		time.Sleep(20 * time.Millisecond)
	}
	if got := r.view.eng.frames; got != frames {
		t.Errorf("an idle view made %d frames in 1.2 s", got-frames)
	}
}

func TestTheRasterizerRefusesAScenelessFrame(t *testing.T) {
	rast := &rasterizer{log: slog.Default()}
	t.Cleanup(rast.reset)
	_, err := rast.render(nil, 16, 16)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("rasterizer.render.HeadlessRasterFailed") {
		t.Errorf("unexpected failure path: %v", err)
	}
}
