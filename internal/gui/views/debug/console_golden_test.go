package debug

import (
	"strconv"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var consoleThemes = []struct {
	name   string
	colors func() kit.Colors
}{{"dark", kit.DarkModern}, {"light", kit.LightModern}}

// mixedOutput is a session that printed lines of every category, ANSI colors, a group, an object, repeated lines and a line with a
// source, and answered an evaluation.
func mixedOutput() daptest.Script {
	return scriptOf(fullCaps, append(kickWith(
		out(map[string]any{"category": "console", "output": "Launching: /work/bin/pricelist\n"}),
		out(map[string]any{"category": "stdout", "output": "starting the loader\n"}),
		out(map[string]any{"category": "stdout", "output": "\x1b[1;32mOK\x1b[0m loaded \x1b[36m3 rows\x1b[0m from \x1b[4mprices.txt\x1b[0m\n"}),
		out(map[string]any{"category": "stdout", "output": "retrying\n"}),
		out(map[string]any{"category": "stdout", "output": "retrying\n"}),
		out(map[string]any{"category": "stdout", "output": "retrying\n"}),
		out(map[string]any{"category": "stdout", "output": "Prices", "group": "start"}),
		out(map[string]any{"category": "stdout", "output": "bread  2.50\n"}),
		out(map[string]any{"category": "stdout", "output": "tea    4.00\n"}),
		out(map[string]any{"category": "stdout", "group": "end"}),
		out(map[string]any{"category": "stdout", "output": "order", "variablesReference": 700}),
		out(map[string]any{"category": "stderr", "output": "warning: row 4 is empty\n", "source": map[string]any{"path": "/work/src/loader.go", "name": "loader.go"}, "line": 18}),
		out(map[string]any{"category": "stdout", "output": "see /work/src/loader.go:18:3 or https://example.com/docs\n"}),
	),
		daptest.ReplyArgs("variables", map[string]any{"variablesReference": 700}, map[string]any{"variables": []map[string]any{
			{"name": "ID", "value": "42", "type": "int"}, {"name": "Total", "value": "6.5", "type": "float64"},
			{"name": "Items", "value": "[]string len:2", "type": "[]string", "variablesReference": 701}}}),
		daptest.ReplyArgs("evaluate", map[string]any{"expression": "order.Total * 2", "context": "repl"}, map[string]any{"result": "13", "type": "float64"}),
	)...)
}

func TestGoldenConsoleMixed(t *testing.T) {
	for _, th := range consoleThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newConsoleRig(t, consoleRigOpts{colors: th.colors(), size: ui.Size{Width: 720, Height: 380}, script: mixedOutput()})
			r.kick()
			r.waitForRow("https://example.com/docs") // the last line of the output
			r.con.Evaluate("order.Total * 2")
			r.waitForRow("13")
			// a path is a link once it was found to be a file, on a goroutine of its own
			r.waitFor("the path to be looked up", func() bool { return r.con.ResolveLink(kit.LinkPart{Kind: kit.LinkPath, Path: "/work/src/loader.go"}) })
			testutil.MatchesGolden(r.ts, "console_mixed_"+th.name)
		})
	}
}

func TestGoldenConsoleGroups(t *testing.T) {
	for _, th := range consoleThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newConsoleRig(t, consoleRigOpts{colors: th.colors(), size: ui.Size{Width: 560, Height: 300}, script: scriptOf(fullCaps, kickWith(
				out(map[string]any{"category": "stdout", "output": "Build", "group": "start"}),
				out(map[string]any{"category": "stdout", "output": "compiling 12 files\n"}),
				out(map[string]any{"category": "stdout", "output": "Linking", "group": "start"}),
				out(map[string]any{"category": "stdout", "output": "ld: ok\n"}),
				out(map[string]any{"category": "stdout", "group": "end"}),
				out(map[string]any{"category": "stdout", "group": "end"}),
				out(map[string]any{"category": "stderr", "output": "Details", "group": "startCollapsed"}),
				out(map[string]any{"category": "stderr", "output": "hidden until opened\n"}),
				out(map[string]any{"category": "stderr", "group": "end"}),
				out(map[string]any{"category": "stdout", "output": "done\n"}))...)})
			r.kick()
			r.waitForRow("done")
			testutil.MatchesGolden(r.ts, "console_groups_"+th.name)
		})
	}
}

func TestGoldenConsoleObjectTree(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{size: ui.Size{Width: 560, Height: 300}, script: scriptOf(fullCaps, append(kickWith(
		out(map[string]any{"category": "stdout", "output": "order", "variablesReference": 700})),
		daptest.ReplyArgs("variables", map[string]any{"variablesReference": 700}, map[string]any{"variables": []map[string]any{
			{"name": "ID", "value": "42", "type": "int"},
			{"name": "Items", "value": "[]string len:2", "type": "[]string", "variablesReference": 701},
			{"name": "Customer", "value": "*main.Customer", "type": "*main.Customer", "variablesReference": 702}}}),
		daptest.ReplyArgs("variables", map[string]any{"variablesReference": 701}, map[string]any{"variables": []map[string]any{
			{"name": "0", "value": "\"bread\"", "type": "string"}, {"name": "1", "value": "\"tea\"", "type": "string"}}}),
		daptest.ReplyArgs("variables", map[string]any{"variablesReference": 702}, map[string]any{"variables": []map[string]any{
			{"name": "Name", "value": "\"Ada\"", "type": "string"}, {"name": "Active", "value": "true", "type": "bool"}}}))...)})
	r.kick()
	r.waitRows(3)
	for _, row := range r.con.Rows() {
		if row.Expandable {
			r.con.Toggle(row.Key)
			r.waitFor("the children", func() bool { return len(r.con.Rows()) > 3 })
			break
		}
	}
	r.waitRows(5)
	testutil.MatchesGolden(r.ts, "console_object_tree")
}

func TestGoldenConsoleFilter(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{size: ui.Size{Width: 560, Height: 240}, script: mixedOutput()})
	r.kick()
	r.waitForRow("https://example.com/docs")
	r.con.FilterController().SetValue("warning, retrying, !prices")
	r.con.SetFilter("warning, retrying, !prices")
	r.pump()
	testutil.MatchesGolden(r.ts, "console_filter")
}

func TestGoldenConsoleTallerThanTheView(t *testing.T) {
	// the rows are one line taller than the view: the end shows, and the first row is cut by the top of the view
	r := newConsoleRig(t, consoleRigOpts{size: ui.Size{Width: 720, Height: 290}, script: mixedOutput()})
	r.kick()
	r.waitForRow("https://example.com/docs")
	r.con.Evaluate("order.Total * 2")
	r.waitForRow("13")
	testutil.MatchesGolden(r.ts, "console_taller")
}

// TestConsoleClipsNothingWhenTheRowsFit is the layout of the rows in a view that changes its height: a console that fits starts at the
// top of the view and shows every row whole; one that does not shows its end, with the last row whole against the input, and only the
// first row cut by the top of the view (VS Code scrolls its list to the end the same way).
func TestConsoleClipsNothingWhenTheRowsFit(t *testing.T) {
	r := newConsoleRig(t, consoleRigOpts{size: ui.Size{Width: 720, Height: 380}, script: mixedOutput()})
	r.kick()
	r.waitForRow("https://example.com/docs")
	r.con.Evaluate("order.Total * 2")
	r.waitForRow("13")
	rows := r.con.Rows()
	first, last := gluttertest.ByKey(ConsoleRowKey(rows[0].Key)), gluttertest.ByKey(ConsoleRowKey(rows[len(rows)-1].Key))
	list := gluttertest.ByKey(ConsoleListKey)
	near := func(what string, got, want float64) {
		t.Helper()
		if got < want-1 || got > want+1 {
			t.Errorf("%s is at %.1f, want %.1f", what, got, want)
		}
	}
	for _, h := range []float64{380, 250, 200, 380, 330, 270, 380} {
		r.ts.SetViewSize(ui.Size{Width: 720, Height: h})
		r.pump()
		listRect := r.ts.TopLeft(list)
		top, bottom := listRect.Dy, listRect.Dy+r.ts.Size(list).Height
		name := " (the view is " + strconv.Itoa(int(h)) + " high)"
		// a list builds the rows in view only: a first row that is not built is out of view, so the rows do not fit
		if r.ts.Exists(first) && r.ts.TopLeft(first).Dy >= top-0.5 {
			near("the top of the first row"+name, r.ts.TopLeft(first).Dy, top)
			continue
		}
		if h == 380 {
			t.Fatalf("the rows are not in view from the top in a view of %v", h)
		}
		near("the bottom of the last row"+name, r.ts.TopLeft(last).Dy+r.ts.Size(last).Height, bottom)
	}
}
