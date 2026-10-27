package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// runQuery runs a read-only dev command the way the dev server does, and returns its answer.
func runQuery(t *testing.T, p *Process, id string) (json.RawMessage, error) {
	t.Helper()
	answer := &devAnswer{}
	ctx := context.WithValue(context.Background(), devAnswerKey{}, answer)
	err := p.Commands.Run(command.Context{Ctx: ctx, Source: command.SourceAPI}, id, nil)
	if err != nil || !answer.set {
		return nil, err
	}
	raw, mErr := json.Marshal(answer.value)
	if mErr != nil {
		t.Fatal(mErr)
	}
	return raw, nil
}

func TestDevQueryErrorRoutes(t *testing.T) {
	win := newTestWorkspace(t, "")
	p := win.Process
	for _, tt := range []struct{ id, route string }{
		{"dev.inputs", "devInputs.DevInputsNoWindow"},
		{"dev.editor", "devEditor.DevEditorNoWorkspace"},
		{"dev.markdown", "devMarkdown.DevMarkdownNoWindow"},
		{"dev.texts", "devTexts.DevTextsNoWindow"},
		{"dev.workbench", "devWorkbench.DevWorkbenchNoWorkspace"},
	} {
		_, err := runQuery(t, p, tt.id)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute(tt.route) {
			t.Errorf("%s: err = %v, want the route %s", tt.id, err, tt.route)
		}
	}
	// a workspace with no document in its editors
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	p.Workspaces.binding = ts.Binding
	_, err := runQuery(t, p, "dev.editor")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("devEditor.DevEditorNone") {
		t.Errorf("dev.editor with no editor: err = %v", err)
	}
}

func TestDevQueriesAnswer(t *testing.T) {
	win := newTestWorkspace(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	win.Process.Workspaces.binding = ts.Binding

	raw, err := runQuery(t, win.Process, "dev.editor")
	if err != nil {
		t.Fatal(err)
	}
	var ed DevEditor
	if err := json.Unmarshal(raw, &ed); err != nil {
		t.Fatal(err)
	}
	if ed.Path != sampleFile(t) || ed.Text != doc.Text() || ed.Line != 1 || ed.Column != 1 || ed.Dirty {
		t.Errorf("dev.editor = %+v", ed)
	}
	if ed.InSnippet || ed.TabStops != 0 || ed.SuggestVisible || ed.SignatureVisible {
		t.Errorf("dev.editor reports a mode on an editor with none: %+v", ed)
	}

	// the workbench lists the groups with their tabs, and the texts on screen include the label of the tab
	raw, err = runQuery(t, win.Process, "dev.workbench")
	if err != nil {
		t.Fatal(err)
	}
	var bench DevWorkbench
	if err := json.Unmarshal(raw, &bench); err != nil {
		t.Fatal(err)
	}
	if len(bench.Groups) != 1 || !bench.Groups[0].Active || len(bench.Groups[0].Tabs) != 1 || bench.Groups[0].Tabs[0].Name != filepath.Base(sampleFile(t)) {
		t.Errorf("dev.workbench = %+v", bench)
	}
	raw, err = runQuery(t, win.Process, "dev.texts")
	if err != nil {
		t.Fatal(err)
	}
	var texts []DevText
	if err := json.Unmarshal(raw, &texts); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tx := range texts {
		found = found || (tx.Text == filepath.Base(sampleFile(t)) && tx.Height > 0)
	}
	if !found {
		t.Errorf("dev.texts has no label of the tab %q: %+v", filepath.Base(sampleFile(t)), texts)
	}

	// an input box on screen is listed with its text and focus
	ctrl := kit.NewInputBoxController("hello")
	t.Cleanup(ctrl.Dispose)
	ts = pumpPage(t, win, func(widgets.BuildContext) widgets.Widget {
		return kit.NewInputBox(nil, nil, kit.InputBoxOpts{Controller: ctrl})
	})
	win.Process.Workspaces.binding = ts.Binding
	ctrl.Focus()
	ts.Pump(1)
	raw, err = runQuery(t, win.Process, "dev.inputs")
	if err != nil {
		t.Fatal(err)
	}
	var inputs []DevInput
	if err := json.Unmarshal(raw, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].Text != "hello" || !inputs[0].Focused || inputs[0].Width <= 0 {
		t.Errorf("dev.inputs = %+v", inputs)
	}
}

func TestDevMarkdownListsThePreviewsOnScreen(t *testing.T) {
	win := newTestWorkspace(t, "")
	ctl := markdown.NewController()
	ts := pumpPage(t, win, func(widgets.BuildContext) widgets.Widget {
		return markdown.NewView("# Title\n\nA paragraph.\n\n- one\n- two\n", markdown.Options{Controller: ctl})
	})
	win.Process.Workspaces.binding = ts.Binding
	ts.Pump(1)
	raw, err := runQuery(t, win.Process, "dev.markdown")
	if err != nil {
		t.Fatal(err)
	}
	var previews []DevMarkdown
	if err := json.Unmarshal(raw, &previews); err != nil {
		t.Fatal(err)
	}
	if len(previews) != 1 || previews[0].Blocks != 3 || previews[0].Built != 3 || previews[0].Width <= 0 || previews[0].Height <= 0 {
		t.Errorf("dev.markdown = %+v", previews)
	}
	if st, ok := ctl.Stats(); !ok || st.Blocks != previews[0].Blocks {
		t.Errorf("the controller says %+v (%v)", st, ok)
	}
}

// devRoundTrip sends a request to the dev server and pumps the window until the answer comes.
func devRoundTrip(t *testing.T, ts interface {
	FlushPosted()
	Pump(...time.Duration)
}, conn net.Conn, line string) DevResponse {
	t.Helper()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	got := make(chan DevResponse, 1)
	go func() {
		var r DevResponse
		if err := json.NewDecoder(conn).Decode(&r); err != nil {
			r = DevResponse{Tag: "TestDecode", Error: err.Error()}
		}
		got <- r
	}()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case r := <-got:
			return r
		default:
			ts.FlushPosted()
			ts.Pump(time.Millisecond)
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("the dev server did not answer")
	return DevResponse{}
}

func TestDevServer(t *testing.T) {
	win := newTestWorkspace(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	dir, err := os.MkdirTemp("", "emetos-dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "dev.sock")
	t.Setenv(DevSocketEnv, socket)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := win.Process.Workspaces
	m.ctx, m.binding = ctx, ts.Binding
	m.startDevServer()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if r := devRoundTrip(t, ts, conn, `{"command":"dev.editor"}`); !r.OK || !strings.Contains(string(r.Result), `"line":1`) {
		t.Errorf("dev.editor: %+v", r)
	}
	if r := devRoundTrip(t, ts, conn, `{"command":"dev.log","args":"hello"}`); !r.OK || r.Result != nil {
		t.Errorf("dev.log: %+v", r)
	}
	if r := devRoundTrip(t, ts, conn, `{"command":"dev.log","args":7}`); r.OK || r.Tag != "DevLogArgumentsInvalid" {
		t.Errorf("a command that fails: %+v", r)
	}
	if r := devRoundTrip(t, ts, conn, `{"command":"dev.nothing"}`); r.OK || r.Error == "" {
		t.Errorf("an unknown command: %+v", r)
	}
	if r := devRoundTrip(t, ts, conn, `not json`); r.OK || r.Tag != "DevRequestInvalid" {
		t.Errorf("a bad request: %+v", r)
	}
	if r := devRoundTrip(t, ts, conn, `{"command":"dev.log","args":{`); r.OK || r.Tag != "DevRequestInvalid" {
		t.Errorf("bad arguments: %+v", r)
	}
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the socket is private: %v %v", info, err)
	}
}

// TestEditChordsInATextFieldAreTheFields: Select All, Undo and Redo are bound to the commands of the editor with no condition,
// and the dispatcher used them even when a box had the focus (found by real input: Ctrl+A in a box selected nothing).
func TestEditChordsInATextFieldAreTheFields(t *testing.T) {
	win := newTestWorkspace(t, "")
	ctrl := kit.NewInputBoxController("")
	t.Cleanup(ctrl.Dispose)
	ts := pumpPage(t, win, func(widgets.BuildContext) widgets.Widget {
		return kit.NewInputBox(nil, nil, kit.InputBoxOpts{Controller: ctrl})
	})
	ctrl.Focus()
	ts.Pump(1)
	chords := []keybinding.Chord{
		{Ctrl: true, Key: keybinding.KeyA}, {Ctrl: true, Key: keybinding.KeyZ},
		{Ctrl: true, Key: keybinding.KeyY}, {Ctrl: true, Shift: true, Key: keybinding.KeyZ},
	}
	for _, chord := range chords {
		if win.DispatchKey(chord, ctrl.FocusNode()) {
			t.Errorf("%s in a box was used as a command", chord.Label())
		}
		if !win.DispatchKey(chord, nil) {
			t.Errorf("%s with no text field focused is no longer a command", chord.Label())
		}
	}
}
