package app

import (
	"context"
	"image"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// rigFrameRate is the most frames a second the views of the tests make: a test waits for the picture, so it is high.
const rigFrameRate = 1000

// viewRig is a keyApp with a headless view of its workspace.
type viewRig struct {
	*keyApp
	view   *headlessView
	frames chan *image.RGBA
}

// newViewRig makes a window with a document and a headless view of it of the size, whose pictures are kept in frames (the
// latest only, as the view itself keeps the latest). The view makes frames as fast as it can (rigFrameRate): a test pumps the
// window until the picture is there.
func newViewRig(t *testing.T, text string, size extension.Size) *viewRig {
	t.Helper()
	a := newKeyApp(t, text)
	// the font of the interface is the test font, as in the other pictures of the tests; the view takes it from the window
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	v, err := a.win.makeHeadlessView(size, rigFrameRate)
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

// settleQuiet is how long the view has to be quiet before it counts as settled: the explorer lists the folder on a goroutine,
// which the race detector on a loaded machine delays by more than the 300 ms this was before (step 17b.5 found the golden test
// failing on a loaded machine with the folder not listed yet, with and without the changes of the step).
const settleQuiet = time.Second

// settle pumps until the view has been quiet for a while (the explorer has listed the folder, the editor has the focus, the
// background work of the file has come in) and returns the latest picture.
func (r *viewRig) settle(t testing.TB) *image.RGBA {
	t.Helper()
	img := r.frame(t)
	quiet := time.Now().Add(settleQuiet)
	for time.Now().Before(quiet) {
		r.ts.Pump(20 * time.Millisecond)
		select {
		case img = <-r.frames:
			quiet = time.Now().Add(settleQuiet)
		case <-time.After(10 * time.Millisecond):
		}
	}
	return img
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
	v, err := win.makeHeadlessView(size, rigFrameRate)
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
	if r.ts.ExitRequests() != 0 {
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

func TestAViewNobodyTypesInMakesNoFrames(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.settle(t)
	frames := r.view.frames.Load()
	end := time.Now().Add(1200 * time.Millisecond) // a blink would come twice
	for time.Now().Before(end) {
		r.ts.Pump(20 * time.Millisecond)
		time.Sleep(20 * time.Millisecond)
	}
	if got := r.view.frames.Load(); got != frames {
		t.Errorf("an idle view made %d frames in 1.2 s", got-frames)
	}
}

func TestTheNotificationsOfTheWorkspaceGoToTheClientOfTheViewAndItsButtonsRunInTheWorkspace(t *testing.T) {
	r := newViewRig(t, "alpha\n", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	got := make(chan extension.ViewNotification, 8)
	r.view.OnNotification(func(n extension.ViewNotification) { got <- n })
	r.pump()
	pressed := make(chan string, 4)
	id := r.win.Notify.Warn("Git", "pull failed", notify.Action{Label: "Retry", Run: func() { pressed <- "retry" }}, notify.Action{Label: "Keep", KeepOpen: true, Run: func() { pressed <- "keep" }})
	r.win.Notify.Notify(notify.Notification{Message: "quiet", Silent: true})
	r.pump()
	var n extension.ViewNotification
	select {
	case n = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("the notification did not reach the client")
	}
	if n.ID != id || n.Severity != extension.NotificationWarning || n.Source != "Git" || n.Message != "pull failed" || len(n.Actions) != 2 || n.Actions[0] != "Retry" || n.Actions[1] != "Keep" {
		t.Errorf("the notification = %+v", n)
	}
	select {
	case n := <-got:
		t.Errorf("a silent notification reached the client: %+v", n)
	default:
	}
	// the view draws no toast of it
	if toasts := r.view.ws.Notifications.Toasts(); len(toasts) != 0 {
		t.Errorf("the view shows %d toasts while its client shows them", len(toasts))
	}
	// the buttons run in the workspace; a button that keeps the notification open leaves it, the other closes it
	r.view.RunNotificationAction(id, 1)
	r.pump()
	if got := <-pressed; got != "keep" {
		t.Errorf("pressed %q", got)
	}
	if _, ok := r.win.Notify.Get(id); !ok {
		t.Error("a button that keeps the notification open closed it")
	}
	r.view.RunNotificationAction(id, 0)
	r.pump()
	if got := <-pressed; got != "retry" {
		t.Errorf("pressed %q", got)
	}
	if _, ok := r.win.Notify.Get(id); ok {
		t.Error("the notification is still there after its button ran")
	}
	r.view.RunNotificationAction(id, 0) // gone: nothing happens
	r.view.RunNotificationAction(999, 0)
	// without a client the view draws the toasts again
	r.view.OnNotification(nil)
	r.pump()
	r.win.Notify.Info("Git", "again")
	r.pump()
	if toasts := r.view.ws.Notifications.Toasts(); len(toasts) != 1 {
		t.Errorf("the view shows %d toasts after the client let go", len(toasts))
	}
}

func TestTextAnInputMethodCommittedInTheClientIsTypedAsAKeyWithNoPhysicalKey(t *testing.T) {
	r := newViewRig(t, "", extension.Size{Width: 640, Height: 400})
	r.frame(t)
	// what the Remote Work tab sends for the text of an input method: a key of the character, with the text and no key of the keyboard
	for _, text := range []string{"é", "日本", "x"} {
		first, _ := utf8.DecodeRuneInString(text)
		r.view.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Logical: uint64(first), Text: text})
		r.view.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Logical: uint64(first)})
		r.pump()
	}
	if got := r.viewText(); got != "é日本x" {
		t.Errorf("the document holds %q", got)
	}
}

func TestTheClipboardOfTheViewIsNotTheClipboardOfTheHost(t *testing.T) {
	r := newViewRig(t, "alpha beta\n", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	r.ts.SetClipboardText("the host's secret")
	copied := make(chan string, 4)
	r.view.OnCopy(func(text string) { copied <- text })
	// a copy in the view goes to its client, and the clipboard of the host is as it was
	r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyA})
	r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyC})
	select {
	case got := <-copied:
		if got != "alpha beta\n" {
			t.Errorf("the copy sent %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a copy in the view did not reach OnCopy")
	}
	if got, _ := r.ts.ClipboardText(); got != "the host's secret" {
		t.Errorf("the copy in the view replaced the clipboard of the host: %q", got)
	}
	// a paste in the view takes the clipboard of the client, and never the host's
	r.view.SetClipboard("from the client")
	r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyV})
	if got := r.viewText(); got != "from the client" {
		t.Errorf("the paste gave %q", got)
	}
	if got, _ := r.ts.ClipboardText(); got != "the host's secret" {
		t.Errorf("the clipboard of the host changed: %q", got)
	}
}

func TestTheClipboardOfAViewKeepsTheFormatsAndTheSelectionsApart(t *testing.T) {
	ts := testutil.New(t, ui.Size{Width: 100, Height: 100})
	main := ts.Binding
	tests := []struct {
		name string
		put  func(c *viewClipboard)
		kind ui.ClipboardKind
		get  string
		want ui.ClipboardContent
		copy string // the text OnCopy got
	}{
		{"text", func(c *viewClipboard) { c.SetClipboardText(t.Context(), "abc", nil) }, ui.ClipboardKindSystem, ui.ClipboardFormatText, ui.ClipboardContent{Text: "abc", HasText: true}, "abc"},
		{"the text of the client is not a copy", func(c *viewClipboard) { c.setText("abc") }, ui.ClipboardKindSystem, ui.ClipboardFormatText, ui.ClipboardContent{Text: "abc", HasText: true}, ""},
		{"the primary selection is not the clipboard", func(c *viewClipboard) {
			c.SetClipboard(t.Context(), ui.ClipboardKindPrimary, ui.ClipboardContent{Text: "sel", HasText: true}, nil)
		}, ui.ClipboardKindSystem, ui.ClipboardFormatText, ui.ClipboardContent{}, ""},
		{"the primary selection", func(c *viewClipboard) {
			c.SetClipboard(t.Context(), ui.ClipboardKindPrimary, ui.ClipboardContent{Text: "sel", HasText: true}, nil)
		}, ui.ClipboardKindPrimary, ui.ClipboardFormatText, ui.ClipboardContent{Text: "sel", HasText: true}, ""},
		{"files", func(c *viewClipboard) {
			c.SetClipboard(t.Context(), ui.ClipboardKindSystem, ui.ClipboardContent{URIs: []string{"file:///a"}}, nil)
		}, ui.ClipboardKindSystem, ui.ClipboardFormatURIList, ui.ClipboardContent{URIs: []string{"file:///a"}}, ""},
		{"a format that is not there", func(c *viewClipboard) { c.setText("abc") }, ui.ClipboardKindSystem, ui.ClipboardFormatPNG, ui.ClipboardContent{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newViewClipboard(main)
			var copied string
			c.setCopyHandler(func(text string) { copied = text })
			tt.put(c)
			var got *ui.ClipboardContent
			c.GetClipboard(t.Context(), tt.kind, tt.get, func(content ui.ClipboardContent, err error) {
				if err != nil {
					t.Error(err)
				}
				got = &content
			})
			if got != nil {
				t.Error("the result came before the call returned")
			}
			ts.Pump(10 * time.Millisecond)
			if got == nil || got.Text != tt.want.Text || got.HasText != tt.want.HasText || len(got.URIs) != len(tt.want.URIs) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if copied != tt.copy {
				t.Errorf("OnCopy got %q, want %q", copied, tt.copy)
			}
		})
	}
}

func TestAPictureThatWaitedForOnFrameAndARefreshAreHandedOver(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	v, err := a.win.makeHeadlessView(extension.Size{Width: 640, Height: 400}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	// the function is set after the first picture was made: it gets that picture, once
	deadline := time.Now().Add(20 * time.Second)
	for v.frames.Load() == 0 && time.Now().Before(deadline) {
		a.ts.Pump(20 * time.Millisecond)
		time.Sleep(5 * time.Millisecond)
	}
	if v.frames.Load() == 0 {
		t.Fatal("the view drew nothing")
	}
	got := make(chan *image.RGBA, 8)
	// closed is set when Close has returned: a picture handed over by a call that starts after that is the failure. The view
	// draws on while the test runs (the cursor blinks), so pictures made before Close wait in got, and are not.
	var closed, late atomic.Int32
	v.OnFrame(func(img *image.RGBA) {
		if closed.Load() != 0 {
			late.Add(1)
		}
		select {
		case got <- img:
		default:
		}
	})
	var first *image.RGBA
	select {
	case first = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the picture that waited was not handed over")
	}
	if first.Rect.Dx() != 640 || first.Rect.Dy() != 400 {
		t.Errorf("the picture is %v", first.Rect)
	}
	// Refresh hands the same picture over again; the picture is not changed by the view as it draws on
	v.Refresh()
	select {
	case again := <-got:
		if differs(first, again) {
			t.Error("Refresh handed over another picture")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh handed nothing over")
	}
	// after Close nothing is handed over: no call of the function starts once Close has returned (one that is running finishes)
	v.Close()
	closed.Store(1)
	a.ts.Pump(20 * time.Millisecond)
	v.Refresh()
	time.Sleep(100 * time.Millisecond)
	if n := late.Load(); n != 0 {
		t.Errorf("%d pictures were handed over after Close", n)
	}
}

// headlessViewRig makes a headless view that has drawn its first picture, as the test above does.
func headlessViewRig(t *testing.T) (*keyApp, *headlessView) {
	t.Helper()
	a := newKeyApp(t, "alpha\n")
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	v, err := a.win.makeHeadlessView(extension.Size{Width: 640, Height: 400}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	deadline := time.Now().Add(20 * time.Second)
	for v.frames.Load() == 0 && time.Now().Before(deadline) {
		a.ts.Pump(20 * time.Millisecond)
		time.Sleep(5 * time.Millisecond)
	}
	if v.frames.Load() == 0 {
		t.Fatal("the view drew nothing")
	}
	return a, v
}

// Close does not return while the function of OnFrame is being called, and no call starts after it has returned: a picture that
// was on its way when the client let go of the view (a frame that had passed the check of closed) was handed over after Close.
func TestCloseWaitsForTheCallOfOnFrameThatIsRunning(t *testing.T) {
	a, v := headlessViewRig(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int32
	v.OnFrame(func(*image.RGBA) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the picture that waited was not handed over")
	}
	closed := make(chan struct{})
	go func() { v.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned while the function was running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the function did")
	}
	before := calls.Load()
	a.ts.Pump(20 * time.Millisecond)
	v.Refresh()
	time.Sleep(100 * time.Millisecond)
	if n := calls.Load(); n != before {
		t.Errorf("the function was called %d times after Close", n-before)
	}
}

// A function of OnFrame that closes the view must not wait for itself for ever.
func TestAFunctionOfOnFrameMayCloseTheView(t *testing.T) {
	_, v := headlessViewRig(t)
	returned := make(chan time.Duration, 1)
	v.OnFrame(func(*image.RGBA) {
		start := time.Now()
		v.Close()
		select {
		case returned <- time.Since(start):
		default:
		}
	})
	select {
	case took := <-returned:
		if took > 5*time.Second {
			t.Errorf("Close took %v", took)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close called from the function did not return")
	}
}

// The input boxes of a view (the quick input, the find widget) are Glutter's EditableText: copy, cut, paste, the primary selection and
// the check whether there is anything to paste all go through the clipboard of the parent of the view. A client of another machine must
// never read the clipboard or the primary selection of the host through them, nor replace them.
func TestTheInputBoxesOfAViewNeverTouchTheClipboardOfTheHost(t *testing.T) {
	const hostClipboard, hostPrimary = "HOST-CLIPBOARD-SECRET", "HOST-PRIMARY-SECRET"
	boxes := []struct {
		name  string
		open  keybinding.Chord
		value func(r *viewRig) string
	}{
		{"the quick input", keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyP}, func(r *viewRig) string { return r.view.ws.QuickInput.Picker().Value() }},
		{"the find widget", keybinding.Chord{Ctrl: true, Key: keybinding.KeyF}, func(r *viewRig) string { return r.view.ws.shell.activeEditor().view.FindState().SearchString() }},
	}
	pastes := []struct {
		name  string
		chord keybinding.Chord
	}{
		{"ctrl+v", keybinding.Chord{Ctrl: true, Key: keybinding.KeyV}},
		{"shift+insert", keybinding.Chord{Shift: true, Key: keybinding.Insert}},
	}
	for _, box := range boxes {
		for _, paste := range pastes {
			t.Run(box.name+" "+paste.name, func(t *testing.T) {
				r := newViewRig(t, "alpha beta\n", extension.Size{Width: 900, Height: 560})
				r.frame(t)
				r.ts.SetClipboardText(hostClipboard)
				r.ts.SetPrimaryText(hostPrimary)
				copied := make(chan string, 4)
				r.view.OnCopy(func(text string) { copied <- text })
				r.chord(box.open)
				selectAll := func() { // the caret to the end of the box, and the selection back to its start
					r.chord(keybinding.Chord{Key: keybinding.End})
					r.chord(keybinding.Chord{Shift: true, Key: keybinding.Home})
				}
				selectAll()
				r.pump()
				untouched := func(what string) {
					t.Helper()
					if got, _ := r.ts.ClipboardText(); got != hostClipboard {
						t.Errorf("%s: the clipboard of the host is %q", what, got)
					}
					if got, _ := r.ts.PrimaryText(); got != hostPrimary {
						t.Errorf("%s: the primary selection of the host is %q", what, got)
					}
				}
				// a paste with nothing on the clipboard of the view pastes nothing, and certainly not what the host has
				r.chord(paste.chord)
				if got := box.value(r); strings.Contains(got, "HOST") {
					t.Fatalf("a paste read the host: %q", got)
				}
				// a paste takes what the client sent
				r.view.SetClipboard("client text")
				selectAll()
				r.chord(paste.chord)
				if got := box.value(r); got != "client text" {
					t.Fatalf("the paste gave %q", got)
				}
				untouched("after the paste")
				// copy and cut reach OnCopy, never the host
				selectAll()
				r.pump()
				r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyC})
				select {
				case got := <-copied:
					if got != "client text" {
						t.Errorf("the copy sent %q", got)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("a copy in the box did not reach OnCopy")
				}
				untouched("after the copy")
				r.chord(keybinding.Chord{Ctrl: true, Key: keybinding.KeyX})
				select {
				case got := <-copied:
					if got != "client text" {
						t.Errorf("the cut sent %q", got)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("a cut in the box did not reach OnCopy")
				}
				if got := box.value(r); got != "" {
					t.Errorf("the cut left %q", got)
				}
				untouched("after the cut")
			})
		}
	}
}

func TestTheInputBoxesOfTheWindowKeepTheClipboardOfTheDesktop(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	a.ts.SetClipboardText("from the desktop")
	a.press(t, "ctrl+shift+p")
	if !a.win.QuickInput.Visible() {
		t.Fatal("the quick input did not open")
	}
	// Ctrl+A selects the whole text of the box, the ">" too (as in VS Code), and the paste replaces it
	a.press(t, "ctrl+a")
	a.press(t, "ctrl+v")
	if got := a.win.QuickInput.Picker().Value(); got != "from the desktop" {
		t.Fatalf("the paste in the window gave %q", got)
	}
	// a copy goes to the clipboard of the desktop, and the selection to its primary selection
	a.ts.SetClipboardText("old")
	a.press(t, "shift+home")
	a.press(t, "ctrl+c")
	if got, _ := a.ts.ClipboardText(); got != "from the desktop" {
		t.Errorf("the copy in the window left %q on the clipboard of the desktop", got)
	}
	if got, _ := a.ts.PrimaryText(); got != "from the desktop" {
		t.Errorf("the selection in the window left %q on the primary selection", got)
	}
}
