package editor

import (
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

const lensText = "package main\n\nfunc TestA(t *testing.T) {\n}\n\nfunc TestB(t *testing.T) {\n}\n"

func TestCodeLensesAreZonesAboveTheirLines(t *testing.T) {
	r := newLang129Rig(t, lensText, nil)
	p := &lensProvider{lenses: []langfeatures.CodeLens{lens(2, "run test"), lens(2, "debug test"), lens(5, "run test"), lens(5, "")}}
	r.reg.RegisterCodeLens("test", r.selector(), p)
	r.vm.viewport = 400 // the editor is laid out: the lines are on the screen
	r.host()
	r.settle()
	if r.vm.CodeLensCount() != 2 || len(r.vm.Zones()) != 2 {
		t.Fatalf("%d lens rows, %d zones", r.vm.CodeLensCount(), len(r.vm.Zones()))
	}
	// a line with lenses gets one row above it, and the line numbers do not move
	zones := r.vm.Zones()
	if zones[0].AfterLine != 2 || zones[1].AfterLine != 5 || zones[0].Rows != 1 {
		t.Errorf("zones = %+v", zones)
	}
	proj := r.vm.Projection()
	first, _ := proj.LineRows(2)
	above, _ := proj.LineRows(1)
	if first-above != 2 {
		t.Errorf("the row of line 2 is %d and the one above %d: one row is the lens's", first, above)
	}
	if got := r.vm.Document().Text(); got != lensText {
		t.Errorf("the text changed: %q", got)
	}
	// the lens of the second line has no command yet and the provider resolves it when the line is on the screen
	titles := r.vm.CodeLensTitles()
	if !reflect.DeepEqual(titles[0], []string{"run test", "debug test"}) {
		t.Errorf("titles of the first row = %v", titles[0])
	}
	r.clock.advance(time.Second)
	r.settle()
	if p.resolved != 1 {
		t.Errorf("%d lenses were resolved: the one that lacked a command", p.resolved)
	}
	if got := r.vm.CodeLensTitles()[1]; !reflect.DeepEqual(got, []string{"run test", "resolved"}) {
		t.Errorf("titles of the second row = %v", got)
	}
	// a click on a title runs its command, through the server that gave it
	zs := r.vm.CodeLensZones()
	if !r.vm.CodeLensRunAt(zs[0], 1) {
		t.Fatal("no title to click")
	}
	cmds := r.executedCommands()
	if len(cmds) != 1 || cmds[0].ID != "cmd.debug test" || cmds[0].Owner != "srv" {
		t.Errorf("executed = %+v", cmds)
	}
	if r.vm.CodeLensRunAt(zs[0], 2) || r.vm.CodeLensRunAt(9999, 0) {
		t.Error("a title that is not there ran")
	}
	// the zones follow the lines through the edits
	r.typeAt(0, 0, "// c\n")
	if got := r.vm.Zones(); got[0].AfterLine != 3 || got[1].AfterLine != 6 {
		t.Errorf("after an edit above, zones = %+v", got)
	}
}

func TestCodeLensesAreAskedAgainAfterAnEditAndGoWithTheSetting(t *testing.T) {
	r := newLang129Rig(t, lensText, nil)
	p := &lensProvider{lenses: []langfeatures.CodeLens{lens(2, "run test")}}
	r.reg.RegisterCodeLens("test", r.selector(), p)
	r.host()
	r.settle()
	if p.calls != 1 || r.vm.CodeLensCount() != 1 {
		t.Fatalf("%d requests, %d rows", p.calls, r.vm.CodeLensCount())
	}
	p.mu.Lock()
	p.lenses = []langfeatures.CodeLens{lens(2, "run test"), lens(5, "run test")}
	p.mu.Unlock()
	r.typeAt(1, 0, " ")
	r.settle()
	if p.calls != 1 {
		t.Errorf("a request before the delay: %d", p.calls)
	}
	r.clock.advance(time.Second)
	r.settle()
	if p.calls != 2 || r.vm.CodeLensCount() != 2 {
		t.Errorf("%d requests, %d rows", p.calls, r.vm.CodeLensCount())
	}
	// editor.codeLens off takes the zones away, and on brings them back
	o := r.vm.Options()
	o.Lens.CodeLens = false
	r.vm.SetOptions(o)
	if r.vm.CodeLensCount() != 0 || len(r.vm.Zones()) != 0 {
		t.Errorf("lenses stay with editor.codeLens off: %d rows, %d zones", r.vm.CodeLensCount(), len(r.vm.Zones()))
	}
	o.Lens.CodeLens = true
	r.vm.SetOptions(o)
	r.settle()
	if r.vm.CodeLensCount() != 2 {
		t.Errorf("%d rows after editor.codeLens came on", r.vm.CodeLensCount())
	}
	// a stale answer is dropped: the lenses of an older version
	r.vm.lensSet(nil, r.vm.Document().Version())
	if r.vm.CodeLensCount() != 0 {
		t.Error("an empty answer keeps rows")
	}
}

func TestCodeLensesDoNotShowForEmbeddedEditorsOrWithoutProviders(t *testing.T) {
	r := newLang129Rig(t, lensText, nil)
	r.host()
	r.settle()
	if r.vm.CodeLensCount() != 0 {
		t.Error("lenses without a provider")
	}
	r2 := newLang129Rig(t, lensText, nil)
	r2.vm.embedded = true
	r2.reg.RegisterCodeLens("test", r2.selector(), &lensProvider{lenses: []langfeatures.CodeLens{lens(2, "x")}})
	r2.host()
	r2.settle()
	if r2.vm.CodeLensCount() != 0 {
		t.Error("the editor of a peek view shows lenses")
	}
}

func TestCodeLensWidgetLayoutAndClick(t *testing.T) {
	r := newLang129Rig(t, lensText, func(o *Options) { o.LineNumbers = LineNumbersOn })
	p := &lensProvider{lenses: []langfeatures.CodeLens{lens(2, "run test"), lens(2, "debug test")}}
	r.reg.RegisterCodeLens("test", r.selector(), p)
	r.host()
	ts := pumpEditor(t, r.vm, uiSize(640, 300))
	r.settle()
	for range 3 {
		ts.Pump(16 * time.Millisecond)
	}
	ts.FlushPosted()
	ts.Pump(16 * time.Millisecond)
	if !ts.Exists(gluttertest.ByText("run test")) || !ts.Exists(gluttertest.ByText("debug test")) {
		t.Fatal("the titles of the lenses are not on the screen")
	}
	// the titles are in the row above the line, indented as the line, and the row has the height of a line of text
	zone := r.vm.Zones()[0]
	top := ts.TopLeft(gluttertest.ByText("run test"))
	wantTop := r.vm.Projection().RowTop(zone.FirstRow)
	if top.Dy < wantTop || top.Dy > wantTop+19 {
		t.Errorf("the title is at y=%v, the zone's row at %v", top.Dy, wantTop)
	}
	ts.Tap(gluttertest.ByText("debug test"))
	cmds := r.executedCommands()
	if len(cmds) != 1 || cmds[0].ID != "cmd.debug test" {
		t.Fatalf("executed after a tap on the title: %+v", cmds)
	}
	// a click on a lens does not move the cursor of the editor
	if pos := r.vm.Cursors().Primary().Position(); pos.Line != 0 || pos.Col != 0 {
		t.Errorf("the cursor moved to %v", pos)
	}
	noErrors(t, ts)
}

func TestADisabledCodeLensShowsItsTitleAndRunsNothing(t *testing.T) {
	r := newLang129Rig(t, lensText, nil)
	disabled := lens(2, "debug test")
	disabled.Command.Disabled = true
	p := &lensProvider{lenses: []langfeatures.CodeLens{lens(2, "run test"), disabled}}
	r.reg.RegisterCodeLens("test", r.selector(), p)
	r.host()
	r.settle()
	if got := r.vm.CodeLensTitles()[0]; !reflect.DeepEqual(got, []string{"run test", "debug test"}) {
		t.Fatalf("titles = %v", got)
	}
	zs := r.vm.CodeLensZones()
	r.vm.CodeLensRunAt(zs[0], 1)
	if cmds := r.executedCommands(); len(cmds) != 0 {
		t.Errorf("a disabled lens ran %+v", cmds)
	}
	r.vm.CodeLensRunAt(zs[0], 0)
	if cmds := r.executedCommands(); len(cmds) != 1 || cmds[0].ID != "cmd.run test" {
		t.Errorf("executed = %+v", cmds)
	}
}
