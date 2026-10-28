package emulator

import (
	"strings"
	"sync"
	"testing"
)

// rig is an emulator with its callbacks recorded.
type rig struct {
	*Emulator
	mu      sync.Mutex
	replies []byte
	titles  []string
	bells   int
	cwds    []string
	oscs    []OSCEvent
	palette int
}

func newRig(rows, cols int) *rig {
	return newRigSB(rows, cols, 100)
}

func newRigSB(rows, cols, scrollback int) *rig {
	r := &rig{}
	r.Emulator = New(rows, cols, Options{Scrollback: scrollback, Callbacks: Callbacks{
		Reply:          func(p []byte) { r.mu.Lock(); r.replies = append(r.replies, p...); r.mu.Unlock() },
		Title:          func(t string) { r.titles = append(r.titles, t) },
		Bell:           func() { r.bells++ },
		Cwd:            func(h, p string) { r.cwds = append(r.cwds, h+p) },
		OSC:            func(ev OSCEvent) { r.oscs = append(r.oscs, ev) },
		PaletteChanged: func() { r.palette++ },
	}})
	return r
}

func (r *rig) feed(s string) {
	if _, err := r.Write([]byte(s)); err != nil {
		panic(err)
	}
}

// take returns the replies so far and clears them.
func (r *rig) take() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := string(r.replies)
	r.replies = nil
	return s
}

// screen returns the rows of the screen as text, trailing spaces removed.
func (r *rig) screen() []string {
	var f Frame
	r.ReadLines(r.Scrollback(), r.rows, &f)
	out := make([]string, f.Rows)
	for y := range out {
		out[y] = f.RowText(y)
	}
	return out
}

// screenString joins the screen rows with newlines, with the trailing empty rows dropped.
func (r *rig) screenString() string {
	rows := r.screen()
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return strings.Join(rows, "\n")
}

func (r *rig) cursorPos() (x, y int) {
	var f Frame
	r.Snapshot(&f)
	return f.Cursor.X, f.Cursor.Y
}

func (r *rig) cell(y, x int) (Cell, Style) {
	var f Frame
	r.ReadLines(r.Scrollback()+y, 1, &f)
	c := f.Cell(0, x)
	return c, f.StyleOf(c)
}

func assertEq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}
