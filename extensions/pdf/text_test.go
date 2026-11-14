package pdf

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// The text of the fake document: page n has the line "Chapter n" at the top, and the even pages "A drawing here" lower down; the
// outline has a chapter for each of the first three pages, the second with a section.
func (r *rig) withText() {
	r.x.outline = func(_ context.Context, _ poppler.Programs, _, _ string) ([]poppler.OutlineItem, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.outlineErr != nil {
			return nil, r.outlineErr
		}
		return r.outlineItems, nil
	}
	r.outlineItems = []poppler.OutlineItem{
		{Title: "Chapter 1", Page: 1},
		{Title: "Chapter 2", Page: 2, Children: []poppler.OutlineItem{{Title: "A section", Page: 3}}},
		{Title: "Chapter 3", Page: 3},
	}
	r.x.words = func(ctx context.Context, _ poppler.Programs, _, _ string, page int) (poppler.PageWords, error) {
		r.mu.Lock()
		r.wordCalls++
		fail := r.wordsFail == page
		r.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return poppler.PageWords{}, errors.NewErr("PopplerStopped", err)
		}
		if fail {
			return poppler.PageWords{}, errors.New("PDFTextFailed", "pdftotext failed", "message", "Syntax Error")
		}
		pw := poppler.PageWords{Width: 612, Height: 792, Lines: []poppler.Line{line(60, "Chapter", strconv.Itoa(page))}}
		if page%2 == 0 {
			l := line(300, "A", "drawing", "here")
			for i := range l.Words {
				l.Words[i].XMin += 72
				l.Words[i].XMax += 72
			}
			pw.Lines = append(pw.Lines, l)
		}
		return pw, nil
	}
	r.x.pageText = func(_ context.Context, _ poppler.Programs, _, _ string, page int) (string, error) {
		return "Chapter " + strconv.Itoa(page) + "\n", nil
	}
}

func TestTheOutlineGoesToItsPages(t *testing.T) {
	r := newRig(t, 6)
	r.withText()
	_, v, ts := r.open(nil, viewSize)
	mustRun(t, r, CommandToggleOutline)
	r.settle(ts, "the outline", func() bool { return v.text.outline == outlineRead })
	ts.Pump(time.Millisecond)
	for _, title := range []string{"OUTLINE", "Chapter 1", "Chapter 2", "A section", "Chapter 3"} {
		if !ts.Exists(gluttertest.ByText(title)) {
			t.Errorf("the outline does not show %q", title)
		}
	}
	ts.Tap(gluttertest.ByText("A section"))
	ts.Pump(time.Millisecond)
	if got := v.currentPage(); got != 2 {
		t.Errorf("A section went to page %d, want 3", got+1)
	}
	// hidden and shown again, it is not read again
	mustRun(t, r, CommandToggleOutline)
	mustRun(t, r, CommandToggleOutline)
	if v.text.outline != outlineRead {
		t.Error("the outline is read again")
	}
}

func TestTheOutlineMessages(t *testing.T) {
	tests := []struct {
		name  string
		items []poppler.OutlineItem
		err   error
		text  string
	}{
		{"none", nil, nil, "This document has no outline."},
		{"failed", nil, errors.New("PDFOutlineFailed", "exit status 1", "message", "Syntax Error"), "The outline cannot be read: Syntax Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, 2)
			r.withText()
			r.outlineItems, r.outlineErr = tt.items, tt.err
			_, v, ts := r.open(nil, viewSize)
			v.toggleOutline()
			r.settle(ts, "the outline", func() bool { return v.text.outline >= outlineRead })
			ts.Pump(time.Millisecond)
			if !ts.Exists(gluttertest.ByText(tt.text)) {
				t.Errorf("no %q", tt.text)
			}
		})
	}
}

func TestFindMovesBetweenMatchesAcrossPages(t *testing.T) {
	r := newRig(t, 8)
	r.withText()
	_, v, ts := r.open(nil, viewSize)
	if err := r.api.Run(CommandFind, "drawing"); err != nil {
		t.Fatal(err)
	}
	r.settle(ts, "the pages read", func() bool { return !v.text.reading && v.text.wordsRead == 8 })
	ts.Pump(time.Millisecond)
	if len(v.text.matches) != 4 || v.text.current != 0 || v.matchesLabel() != "1 of 4" {
		t.Fatalf("%d matches, current %d, %q", len(v.text.matches), v.text.current, v.matchesLabel())
	}
	if !ts.Exists(gluttertest.ByText("1 of 4")) {
		t.Error("the count is not shown")
	}
	// the first match is on page 2: the view went there
	if got := v.currentPage(); got != 1 {
		t.Errorf("at page %d, want 2", got+1)
	}
	steps := []struct {
		do   func()
		want int // the page of the current match, from 1
	}{
		{func() { ts.SendKeyEvent(services.LogicalKeyboardKeyEnter) }, 4},
		{func() { mustRun(t, r, CommandFindNext) }, 6},
		{func() { mustRun(t, r, CommandFindNext) }, 8},
		{func() { mustRun(t, r, CommandFindNext) }, 2}, // around
		{func() { mustRun(t, r, CommandFindPrevious) }, 8},
	}
	for i, st := range steps {
		st.do()
		ts.Pump(time.Millisecond)
		m := v.text.matches[v.text.current]
		if m.page+1 != st.want {
			t.Errorf("step %d: the match is on page %d, want %d", i, m.page+1, st.want)
		}
		r0 := v.lay.rects[m.page]
		k := r0.h / 792
		if y := r0.y + m.boxes[0].y0*k; y < v.top || y > v.top+v.viewH {
			t.Errorf("step %d: the match at %v is out of the view %v..%v", i, y, v.top, v.top+v.viewH)
		}
	}
	// match case, and a text that is nowhere
	v.setMatchCase(true)
	v.setFindText("Drawing")
	if v.matchesLabel() != "No results" {
		t.Errorf("match case: %q", v.matchesLabel())
	}
	ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	ts.Pump(time.Millisecond)
	if v.text.findShown || len(v.text.matches) != 0 {
		t.Error("Escape did not close the find")
	}
	if r.wordCalls != 8 {
		t.Errorf("the words were read %d times, want once a page", r.wordCalls)
	}
}

func TestTheHighlightsAreOnTheirBoxesAtEveryZoom(t *testing.T) {
	r := newRig(t, 4)
	r.withText()
	_, v, ts := r.open(nil, viewSize)
	v.openFind("drawing")
	r.settle(ts, "the pages read", func() bool { return v.text.wordsRead == 4 })
	r.drawnAll(ts, v)
	for _, z := range []zoom{{}, {mode: zoomLevel, level: 2}} {
		v.setZoom(z)
		v.text.current = 0
		v.revealMatch()
		ts.Pump(300 * time.Millisecond)
		r.drawnAll(ts, v)
		ts.Pump(time.Millisecond)
		// the box of "drawing": x 87..157, y 300..312 points on page 2
		k := v.lay.rects[1].w / 612
		found := false
		for _, w := range ts.Widgets(gluttertest.ByType[*widgets.Positioned]()) {
			p := w.(*widgets.Positioned)
			if near(box{x0: p.Left().OrElse(-1), y0: p.Top().OrElse(-1), x1: p.Left().OrElse(-1) + p.Width().OrElse(0), y1: p.Top().OrElse(-1) + p.Height().OrElse(0)},
				box{x0: 87 * k, y0: 300 * k, x1: 157 * k, y1: 312 * k}) {
				found = true
			}
		}
		if !found {
			t.Errorf("at %v no highlight on the box of the match", z)
		}
	}
}

func TestAPageWhoseTextCannotBeRead(t *testing.T) {
	r := newRig(t, 6)
	r.withText()
	r.wordsFail = 5
	_, v, ts := r.open(nil, viewSize)
	v.openFind("drawing")
	r.settle(ts, "the reading to stop", func() bool { return !v.text.reading && v.text.wordsErr != "" })
	if len(v.text.matches) != 2 || v.matchesLabel() != "1 of 2" { // pages 2 and 4
		t.Errorf("%d matches, %q", len(v.text.matches), v.matchesLabel())
	}
}

func TestCopyPageText(t *testing.T) {
	r := newRig(t, 4)
	r.withText()
	_, v, ts := r.open(nil, viewSize)
	v.goToPage(2)
	mustRun(t, r, CommandCopyPageText)
	r.settle(ts, "the copy", func() bool { return len(r.api.ClipboardTexts()) == 1 })
	if got := r.api.ClipboardTexts()[0]; got != "Chapter 3\n" {
		t.Errorf("copied %q", got)
	}
	r.api.Pump()
	if notes := r.api.Notes(); len(notes) == 0 || notes[len(notes)-1].Message != "Copied the text of page 3." {
		t.Errorf("notifications %+v", notes)
	}
	r.x.pageText = func(context.Context, poppler.Programs, string, string, int) (string, error) {
		return "", errors.New("PDFTextFailed", "exit status 1", "message", "Syntax Error")
	}
	mustRun(t, r, CommandCopyPageText)
	r.settle(ts, "the error", func() bool { notes := r.api.Notes(); return notes[len(notes)-1].Severity == "error" })
	if notes := r.api.Notes(); notes[len(notes)-1].Message != "The text of page 3 cannot be copied: Syntax Error" {
		t.Errorf("notifications %+v", notes)
	}
}

func TestTextGoldens(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run("outline and find "+th.Name, func(t *testing.T) {
			r := newRig(t, 4)
			r.withText()
			fe, err := r.api.OpenFileEditor(ViewerID, r.write("doc.pdf", []byte("%PDF")), nil)
			if err != nil {
				t.Fatal(err)
			}
			fe.SetActive(true)
			v := r.x.viewers[0]
			ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 420}, widgets.NewBuilder(fe.Content.Build))
			r.settle(ts, "the pages", func() bool { return len(v.lay.rects) == 4 })
			v.toggleOutline()
			if err := r.api.Run(CommandFind, "drawing"); err != nil {
				t.Fatal(err)
			}
			r.settle(ts, "the outline and the words", func() bool { return v.text.outline == outlineRead && v.text.wordsRead == 4 })
			// the reveal of the match ran before or after the panel narrowed the view: the place is set here
			v.goToPage(1)
			ts.Pump(300 * time.Millisecond)
			r.drawnAll(ts, v)
			for range 10 { // the scrollbar that the scroll showed fades
				ts.Pump(300 * time.Millisecond)
			}
			uitest.MatchesGolden(ts, "pdf_outline_find_"+th.Name)
		})
	}
}
