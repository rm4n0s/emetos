package sql

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// panelWidget builds the content of the panel tab as its box does.
func panelWidget(e *editorRig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return e.api.View(ViewID).Content.Build(bc) })
}

// steady fixes the times of a run, which the picture shows.
func steady(rec *runRecord) {
	rec.started = time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)
	rec.elapsed = 12 * time.Millisecond
	for _, st := range rec.steps {
		st.res.Elapsed = 3 * time.Millisecond
	}
}

func TestGoldenThePanelWithAResultGrid(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			e := newEditorRig(t, "SELECT b.id, b.title, a.name AS author, b.isbn, b.price\nFROM books b JOIN authors a ON a.id = b.author_id\nORDER BY b.id", true)
			e.conn.Name = "Library"
			rec := e.run(RunAllQueries)
			steady(rec)
			e.ext.q.view.Refresh()
			ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 190}, panelWidget(e))
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "panel_grid_"+th.Name)
		})
	}
}

func TestGoldenThePanelWithRowsAffectedAndSeveralResults(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			e := newEditorRig(t, "UPDATE authors SET born = born + 1 WHERE id = 1;\nINSERT INTO log (line) VALUES ('a'), ('b');\nSELECT count(*) AS authors FROM authors;\nSELECT n, line FROM log;\nPRAGMA foreign_keys = ON", true)
			rec := e.run(RunAllQueries)
			steady(rec)
			rec.tab = len(rec.sets) // the Messages
			ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 190}, panelWidget(e))
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "panel_affected_"+th.Name)
		})
	}
}

func TestGoldenThePanelWithAnError(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			e := newEditorRig(t, "SELECT 1;\nSELECT id, titel\nFROM books;\nSELECT 3", true)
			rec := e.run(RunAllQueries)
			steady(rec)
			ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 190}, panelWidget(e))
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "panel_error_"+th.Name)
		})
	}
}

func TestGoldenThePanelWhileARunGoesOnAndBeforeAnyRun(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			e := newEditorRig(t, "SELECT 1", true)
			ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 90}, panelWidget(e))
			uitest.MatchesGolden(ts, "panel_empty_"+th.Name)

			// a run in progress: the Cancel button, and the statement it is at
			q := e.ext.q
			ed := q.editor(e.uri)
			rec := &runRecord{n: 1, started: time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC), connection: "Library", total: 5, current: 3, running: true}
			ed.runs, ed.shown, ed.running = []*runRecord{rec}, rec, rec
			q.shown = ed
			ts = uitest.Pump(t, th, gui.Size{Width: 760, Height: 90}, panelWidget(e))
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "panel_running_"+th.Name)
			_ = gluttertest.ByTextContaining("Cancel")
		})
	}
}
