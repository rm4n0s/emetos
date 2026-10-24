package document

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/vstest"
	"github.com/rm4n0s/errors"
)

func newDoc(text string) *Document { return New(UntitledURI(1), text, NewOptions()) }

func pos(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }

func rg(sl, sc, el, ec int) buffer.Range { return buffer.Range{Start: pos(sl, sc), End: pos(el, ec)} }

func ins(line, col int, text string) Edit {
	return Edit{Range: buffer.Range{Start: pos(line, col), End: pos(line, col)}, Text: text}
}

// vs makes an Edit from a VS Code range (1-based lines and UTF-16 columns) of
// the document's current text, as the ported tests write them.
func vs(t *testing.T, d *Document, sl, sc, el, ec int, text string) Edit {
	t.Helper()
	return Edit{Range: vstest.Range(t, d.Text(), sl, sc, el, ec), Text: text}
}

func edit(t *testing.T, d *Document, batch []Edit, opts EditOptions) EditResult {
	t.Helper()
	res, err := d.Edit(batch, opts)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	return res
}

func undo(t *testing.T, d *Document) HistoryResult {
	t.Helper()
	res, err := d.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	return res
}

func redo(t *testing.T, d *Document) HistoryResult {
	t.Helper()
	res, err := d.Redo()
	if err != nil {
		t.Fatalf("Redo: %v", err)
	}
	return res
}

func wantTag(t *testing.T, err error, route, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) || !appErr.HasRoute(tag) {
		t.Errorf("unexpected failure path: %s, want %s through %s", appErr.Route(), tag, route)
	}
}

func lines(s ...string) string { return strings.Join(s, "\n") }

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/model/editableTextModel.test.ts
// ('Bug 19872: Undo is funky', '(2)', 'insert empty text', 'last op is no-op',
// 'issue #47733: Undo mangles unicode characters', 'issue #48741: Broken undo
// stack with move lines up with multiple cursors'). Each applies a batch,
// checks the text, and undoes it with the inverse batch and with Undo.
func TestVSCodeApplyEdits(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		edits func(*testing.T, *Document) []Edit
		want  string
	}{
		{"Bug 19872: Undo is funky", lines("something", " A", "", " B", "something else"),
			func(t *testing.T, d *Document) []Edit {
				return []Edit{vs(t, d, 2, 1, 2, 2, ""), vs(t, d, 3, 1, 4, 2, "")}
			}, lines("something", "A", "B", "something else")},
		{"Bug 19872: Undo is funky (2)", lines("something", "A", "B", "something else"),
			func(t *testing.T, d *Document) []Edit {
				return []Edit{vs(t, d, 2, 1, 2, 1, " "), vs(t, d, 3, 1, 3, 1, lines("", " "))}
			}, lines("something", " A", "", " B", "something else")},
		{"last op is no-op", lines("My First Line", "\t\tMy Second Line", "    Third Line", "", "1"),
			func(t *testing.T, d *Document) []Edit {
				return []Edit{vs(t, d, 1, 1, 1, 2, ""), vs(t, d, 4, 1, 4, 1, "")}
			}, lines("y First Line", "\t\tMy Second Line", "    Third Line", "", "1")},
		{"issue #47733: Undo mangles unicode characters", "'👁'",
			func(t *testing.T, d *Document) []Edit {
				return []Edit{vs(t, d, 1, 1, 1, 1, "\""), vs(t, d, 1, 2, 1, 2, "\"")}
			}, "\"'\"👁'"},
		{"issue #48741: Broken undo stack with move lines up with multiple cursors", lines("line1", "line2", "line3", ""),
			func(t *testing.T, d *Document) []Edit {
				return []Edit{
					vs(t, d, 4, 1, 4, 1, "line3"),
					vs(t, d, 3, 1, 3, 6, ""),
					vs(t, d, 2, 1, 3, 1, ""),
					vs(t, d, 3, 6, 3, 6, "\nline2"),
				}
			}, lines("line1", "", "line2", "line3")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDoc(tt.text)
			res := edit(t, d, tt.edits(t, d), EditOptions{})
			if got := d.Text(); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			// The inverse batch undoes it, applied to the new text.
			other := newDoc(d.Text())
			edit(t, other, res.Inverse, EditOptions{})
			if got := other.Text(); got != tt.text {
				t.Errorf("after the inverse batch: %q, want %q", got, tt.text)
			}
			// So does Undo, and Redo does it again.
			undo(t, d)
			if got := d.Text(); got != tt.text {
				t.Errorf("after Undo: %q, want %q", got, tt.text)
			}
			redo(t, d)
			if got := d.Text(); got != tt.want {
				t.Errorf("after Redo: %q, want %q", got, tt.want)
			}
		})
	}
}

// The #47733 case continues: deleting the two quotes again.
func TestVSCodeIssue47733Delete(t *testing.T) {
	d := newDoc("\"'\"👁'")
	if got := d.Snapshot().ValidateRange(vstest.Range(t, d.Text(), 1, 3, 1, 4)); got != vstest.Range(t, d.Text(), 1, 3, 1, 4) {
		t.Errorf("a valid range changed: %+v", got)
	}
	edit(t, d, []Edit{vs(t, d, 1, 1, 1, 2, ""), vs(t, d, 1, 3, 1, 4, "")}, EditOptions{})
	if got, want := d.Text(), "'👁'"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestVSCodeInsertEmptyText(t *testing.T) {
	d := newDoc(lines("My First Line", "1"))
	version := d.Version()
	res := edit(t, d, []Edit{vs(t, d, 1, 1, 1, 1, "")}, EditOptions{})
	if res.Changed || d.Version() != version || d.CanUndo() || d.Dirty() {
		t.Errorf("an empty insert changed the document: %+v, version %d, can undo %v, dirty %v", res, d.Version(), d.CanUndo(), d.Dirty())
	}
}

// Ported from VS Code: editableTextModel.test.ts ('high-low surrogates 1'..'4').
// The inputs are invalid in VS Code (columns inside a surrogate pair) and so
// are the byte columns here, inside the 4-byte character: a range that covers
// text grows to take the whole character, an empty one moves before it.
func TestVSCodeSurrogates(t *testing.T) {
	tests := []struct {
		name string
		r    buffer.Range
		want string
	}{
		{"1: insert inside", rg(0, 2, 0, 2), lines("a📚some", "very nice")},
		{"2: from inside to after", rg(0, 2, 0, 4), lines("asome", "very nice")},
		{"3: from before to inside", rg(0, 0, 0, 2), lines("asome", "very nice")},
		{"4: the whole character", rg(0, 0, 0, 4), lines("asome", "very nice")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDoc(lines("📚some", "very nice"))
			edit(t, d, []Edit{{Range: tt.r, Text: "a"}}, EditOptions{})
			if got := d.Text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts
// ('issue #90973: Undo brings back model alternative version').
func TestVSCodeUndoBringsBackAlternativeVersion(t *testing.T) {
	d := newDoc("")
	beforeVersion, beforeAlt := d.Version(), d.AlternativeVersion()
	edit(t, d, []Edit{ins(0, 0, "Hello")}, EditOptions{Kind: KindTyping})
	undo(t, d)
	if d.Version() == beforeVersion {
		t.Error("the version did not change")
	}
	if d.AlternativeVersion() != beforeAlt {
		t.Errorf("alternative version = %d, want %d", d.AlternativeVersion(), beforeAlt)
	}
}

// Ported from VS Code: cursor.test.ts ("issue #23539: Setting model EOL isn't undoable").
func TestVSCodeLineEndingIsUndoable(t *testing.T) {
	d := newDoc("Hello\nworld")
	if err := d.SetLineEnding(fileio.CRLF); err != nil {
		t.Fatal(err)
	}
	if d.LineEnding() != fileio.CRLF || !d.Dirty() {
		t.Errorf("line ending %q, dirty %v", d.LineEnding(), d.Dirty())
	}
	undo(t, d)
	if d.LineEnding() != fileio.LF || d.Dirty() {
		t.Errorf("after Undo: line ending %q, dirty %v", d.LineEnding(), d.Dirty())
	}
	redo(t, d)
	if d.LineEnding() != fileio.CRLF {
		t.Errorf("after Redo: line ending %q", d.LineEnding())
	}
	// Setting the same ending is not a change.
	version := d.Version()
	if err := d.SetLineEnding(fileio.CRLF); err != nil || d.Version() != version {
		t.Errorf("same ending: %v, version %d -> %d", err, version, d.Version())
	}
}

// Ported from VS Code: cursor.test.ts ('there is an undo stop between typing and
// deleting left', 'typing and deleting right', 'deleting left and typing',
// 'deleting left and deleting right', 'deleting right and typing', 'deleting
// right and deleting left'). VS Code's cursor decides the stops; here the kinds
// of the edits do, with the same results: two undo steps.
func TestVSCodeUndoStopsBetweenKinds(t *testing.T) {
	kinds := map[string]EditKind{"typing": KindTyping, "deleting left": KindDeleteLeft, "deleting right": KindDeleteRight}
	// run applies two edits of a kind to a line, one character each, the cursor
	// at the place the VS Code test has it, and returns the cursor after.
	run := func(t *testing.T, d *Document, kind string, cursor int) int {
		k := kinds[kind]
		for i := 0; i < 2; i++ {
			before := []buffer.Selection{buffer.NewCursor(pos(0, cursor))}
			var batch []Edit
			next := cursor
			switch k {
			case KindTyping:
				batch = []Edit{ins(0, cursor, "x")}
				next++
			case KindDeleteLeft:
				batch = []Edit{{Range: rg(0, cursor-1, 0, cursor)}}
				next--
			case KindDeleteRight:
				batch = []Edit{{Range: rg(0, cursor, 0, cursor+1)}}
			}
			edit(t, d, batch, EditOptions{Kind: k, Before: before, After: func([]Edit) []buffer.Selection {
				return []buffer.Selection{buffer.NewCursor(pos(0, next))}
			}})
			cursor = next
		}
		return cursor
	}
	for first := range kinds {
		for second := range kinds {
			if first == second {
				continue
			}
			t.Run(first+" then "+second, func(t *testing.T) {
				d := newDoc("A line of text to edit")
				original := d.Text()
				cursor := run(t, d, first, 8)
				afterFirst := d.Text()
				run(t, d, second, cursor)
				undo(t, d)
				if got := d.Text(); got != afterFirst {
					t.Errorf("after one Undo: %q, want %q", got, afterFirst)
				}
				undo(t, d)
				if got := d.Text(); got != original {
					t.Errorf("after two Undos: %q, want %q", got, original)
				}
				if d.CanUndo() {
					t.Error("more than two undo steps")
				}
			})
		}
	}
}

func TestURI(t *testing.T) {
	tests := []struct {
		uri   URI
		str   string
		title string
	}{
		{FileURI("/home/u/a b/main.go"), "file:///home/u/a b/main.go", "main.go"},
		{UntitledURI(3), "untitled:3", "Untitled-3"},
		{VirtualURI("defaults/settings.json"), "emetos-virtual:defaults/settings.json", "settings.json"},
	}
	for _, tt := range tests {
		if got := tt.uri.String(); got != tt.str {
			t.Errorf("String = %q, want %q", got, tt.str)
		}
		if got := tt.uri.Title(); got != tt.title {
			t.Errorf("Title = %q, want %q", got, tt.title)
		}
		back, err := ParseURI(tt.str)
		if err != nil || back != tt.uri {
			t.Errorf("ParseURI(%q) = %+v, %v", tt.str, back, err)
		}
	}
	for _, bad := range []string{"", "nocolon", "file:relative", "file://relative", "untitled:0", "untitled:x", "untitled:-1", "http://x"} {
		_, err := ParseURI(bad)
		if err == nil {
			t.Errorf("ParseURI(%q) succeeded", bad)
			continue
		}
		wantTag(t, err, "ParseURI", "URIInvalid")
	}
}

func TestLowestFreeUntitled(t *testing.T) {
	tests := []struct {
		open []URI
		want int
	}{
		{nil, 1},
		{[]URI{UntitledURI(1)}, 2},
		{[]URI{UntitledURI(2), UntitledURI(3)}, 1},
		{[]URI{UntitledURI(1), UntitledURI(3), FileURI("/a")}, 2},
		{[]URI{UntitledURI(1), UntitledURI(2), UntitledURI(3)}, 4},
	}
	for _, tt := range tests {
		if got := LowestFreeUntitled(tt.open); got != UntitledURI(tt.want) {
			t.Errorf("LowestFreeUntitled(%v) = %v, want untitled %d", tt.open, got, tt.want)
		}
	}
}

func TestNewAndFromRead(t *testing.T) {
	d := New(UntitledURI(1), "a\nb", Options{})
	tab, spaces := d.Indentation()
	if d.LanguageID() != DefaultLanguage || d.Encoding() != fileio.UTF8 || d.LineEnding() != fileio.LF || tab != 4 || spaces ||
		d.ReadOnly() || d.LargeFile() || d.HasBOM() || d.Dirty() || d.Version() != 1 || d.AlternativeVersion() != 1 {
		t.Errorf("defaults wrong: %+v", d)
	}
	if o := NewOptions(); !o.InsertSpaces || o.TabSize != 4 {
		t.Errorf("NewOptions = %+v", o)
	}
	res := fileio.ReadResult{Text: "x\ny\n", Encoding: fileio.UTF8BOM, HasBOM: true, LineEnding: fileio.CRLF, LargeFile: true,
		Stat: fileio.FileStat{Size: 7}}
	f := FromRead(FileURI("/a/b.txt"), res, NewOptions())
	if f.Text() != "x\ny\n" || f.Encoding() != fileio.UTF8BOM || !f.HasBOM() || f.LineEnding() != fileio.CRLF ||
		!f.LargeFile() || f.Stat().Size != 7 || f.Dirty() || f.URI() != FileURI("/a/b.txt") {
		t.Errorf("FromRead wrong: %+v", f)
	}
	f.SetLanguageID("go")
	f.SetEncoding("windows1252")
	f.SetBOM(false)
	f.SetIndentation(2, false)
	f.SetIndentation(0, true) // below 1 is the default
	f.SetReadOnly(true)
	if tab, spaces := f.Indentation(); f.LanguageID() != "go" || f.Encoding() != "windows1252" || f.HasBOM() || tab != 4 || !spaces || !f.ReadOnly() {
		t.Errorf("setters wrong: %+v", f)
	}
}

// Dirty is a matter of the alternative version id, so undoing to the saved
// text clears it, and saving moves the mark.
func TestDirtyRoundTrips(t *testing.T) {
	type op string
	tests := []struct {
		name  string
		ops   []op
		dirty []bool // after each op
	}{
		{"edit", []op{"edit"}, []bool{true}},
		{"edit then undo", []op{"edit", "undo"}, []bool{true, false}},
		{"edit undo redo", []op{"edit", "undo", "redo"}, []bool{true, false, true}},
		{"save after edit", []op{"edit", "save"}, []bool{true, false}},
		{"save then undo", []op{"edit", "save", "undo"}, []bool{true, false, true}},
		{"save undo redo", []op{"edit", "save", "undo", "redo"}, []bool{true, false, true, false}},
		{"save then edit", []op{"edit", "save", "edit"}, []bool{true, false, true}},
		{"save, edit, undo", []op{"edit", "save", "edit", "undo"}, []bool{true, false, true, false}},
		{"two edits, save, undo twice, redo twice", []op{"edit", "edit", "save", "undo", "undo", "redo", "redo"}, []bool{true, true, false, true, true, true, false}},
		{"undo past the saved text and edit", []op{"edit", "save", "undo", "edit"}, []bool{true, false, true, true}},
		{"edit after undo is never the saved text", []op{"edit", "undo", "edit", "undo"}, []bool{true, false, true, false}},
		{"save of the clean text", []op{"save", "edit", "undo"}, []bool{false, true, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDoc("text")
			n := 0
			for i, o := range tt.ops {
				switch o {
				case "edit":
					n++
					// Kind Other so that every edit is a group of its own.
					edit(t, d, []Edit{ins(0, 0, string(rune('a'+n)))}, EditOptions{})
				case "undo":
					undo(t, d)
				case "redo":
					redo(t, d)
				case "save":
					d.MarkSaved(fileio.FileStat{Size: int64(d.Snapshot().Len())})
				}
				if d.Dirty() != tt.dirty[i] {
					t.Fatalf("after %q (op %d): dirty = %v, want %v", o, i, d.Dirty(), tt.dirty[i])
				}
			}
		})
	}
}

func TestMarkSavedKeepsTheStat(t *testing.T) {
	d := newDoc("a")
	stat := fileio.FileStat{Size: 99}
	edit(t, d, []Edit{ins(0, 1, "b")}, EditOptions{})
	d.MarkSaved(stat)
	if d.Stat() != stat || d.SavedAlternativeVersion() != d.AlternativeVersion() {
		t.Errorf("stat %+v, saved %d, alt %d", d.Stat(), d.SavedAlternativeVersion(), d.AlternativeVersion())
	}
}

func TestEditValidation(t *testing.T) {
	t.Run("overlapping edits are refused", func(t *testing.T) {
		d := newDoc("abcdef")
		_, err := d.Edit([]Edit{{Range: rg(0, 1, 0, 4), Text: "x"}, {Range: rg(0, 3, 0, 5), Text: "y"}}, EditOptions{})
		wantTag(t, err, "prepare", "EditsOverlap")
		_, err = d.Edit([]Edit{{Range: rg(0, 1, 0, 4), Text: "x"}, ins(0, 2, "y")}, EditOptions{})
		wantTag(t, err, "Edit", "EditsOverlap")
		if d.Text() != "abcdef" || d.Version() != 1 {
			t.Errorf("a refused batch changed the document: %q, version %d", d.Text(), d.Version())
		}
	})
	t.Run("touching edits are fine", func(t *testing.T) {
		d := newDoc("abcdef")
		edit(t, d, []Edit{{Range: rg(0, 1, 0, 3), Text: "X"}, {Range: rg(0, 3, 0, 5), Text: "Y"}, ins(0, 3, "-")}, EditOptions{})
		if got := d.Text(); got != "aX-Yf" {
			t.Errorf("text = %q", got)
		}
	})
	t.Run("inserts at one place keep their order", func(t *testing.T) {
		d := newDoc("ab")
		edit(t, d, []Edit{ins(0, 1, "1"), ins(0, 1, "2"), ins(0, 1, "3")}, EditOptions{})
		if got := d.Text(); got != "a123b" {
			t.Errorf("text = %q", got)
		}
	})
	t.Run("an insert before a replacement at the same place", func(t *testing.T) {
		d := newDoc("abc")
		edit(t, d, []Edit{{Range: rg(0, 1, 0, 2), Text: "X"}, ins(0, 1, "<")}, EditOptions{})
		if got := d.Text(); got != "a<Xc" {
			t.Errorf("text = %q", got)
		}
	})
	t.Run("unsorted batches work", func(t *testing.T) {
		d := newDoc("abcdef")
		edit(t, d, []Edit{ins(0, 5, "5"), ins(0, 1, "1"), ins(0, 3, "3")}, EditOptions{})
		if got := d.Text(); got != "a1bc3de5f" {
			t.Errorf("text = %q", got)
		}
	})
	t.Run("ranges are moved into the text", func(t *testing.T) {
		d := newDoc("ab\ncd")
		edit(t, d, []Edit{{Range: rg(-3, 0, 0, 1), Text: "X"}, {Range: rg(1, 1, 9, 9), Text: "Y"}}, EditOptions{})
		if got := d.Text(); got != "Xb\ncY" {
			t.Errorf("text = %q", got)
		}
	})
	t.Run("carriage returns in text become line feeds", func(t *testing.T) {
		d := newDoc("")
		edit(t, d, []Edit{ins(0, 0, "a\r\nb\rc\nd")}, EditOptions{})
		if got := d.Text(); got != "a\nb\nc\nd" {
			t.Errorf("text = %q", got)
		}
	})
	t.Run("an empty batch changes nothing", func(t *testing.T) {
		d := newDoc("a")
		res := edit(t, d, nil, EditOptions{})
		if res.Changed || d.Version() != 1 {
			t.Errorf("= %+v, version %d", res, d.Version())
		}
	})
}

func TestReadOnly(t *testing.T) {
	d := New(VirtualURI("x"), "text", Options{ReadOnly: true})
	_, err := d.Edit([]Edit{ins(0, 0, "a")}, EditOptions{})
	wantTag(t, err, "Edit", "DocumentReadOnly")
	_, err = d.Undo()
	wantTag(t, err, "Undo", "DocumentReadOnly")
	_, err = d.Redo()
	wantTag(t, err, "Redo", "DocumentReadOnly")
	wantTag(t, d.SetLineEnding(fileio.CRLF), "SetLineEnding", "DocumentReadOnly")
	if d.Text() != "text" || d.Version() != 1 {
		t.Errorf("a read-only document changed: %q", d.Text())
	}
	// SetText still works: it is how a reloaded file arrives.
	d.SetText("new")
	if d.Text() != "new" {
		t.Errorf("SetText on a read-only document: %q", d.Text())
	}
}

func TestInverseBatch(t *testing.T) {
	d := newDoc("one\ntwo\nthree")
	res := edit(t, d, []Edit{
		{Range: rg(0, 0, 0, 3), Text: "ONE!"},
		{Range: rg(1, 0, 2, 0), Text: ""},
		ins(2, 5, "\n4"),
	}, EditOptions{})
	if got := d.Text(); got != "ONE!\nthree\n4" {
		t.Fatalf("text = %q", got)
	}
	want := []Edit{
		{Range: rg(0, 0, 0, 4), Text: "one"},
		{Range: rg(1, 0, 1, 0), Text: "two\n"},
		{Range: rg(1, 5, 2, 1), Text: ""},
	}
	if len(res.Inverse) != len(want) {
		t.Fatalf("inverse = %+v", res.Inverse)
	}
	for i := range want {
		if res.Inverse[i] != want[i] {
			t.Errorf("inverse[%d] = %+v, want %+v", i, res.Inverse[i], want[i])
		}
	}
}

// A random batch is applied, undone and redone against a plain string, over text
// with characters of every width.
func TestRandomBatchesAgainstAString(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	words := []string{"a", "bc", "é", "日本", "😀", "\n", "x\ny", ""}
	for round := 0; round < 150; round++ {
		var sb strings.Builder
		for i := 0; i < rng.Intn(30); i++ {
			sb.WriteString(words[rng.Intn(len(words)-1)])
		}
		d := newDoc(sb.String())
		original := d.Text()
		// A batch of non-overlapping edits at character boundaries, in random order.
		runes := []rune(original)
		var points []int
		for i := 0; i <= len(runes); i++ {
			if rng.Intn(3) == 0 {
				points = append(points, i)
			}
		}
		var batch []Edit
		model := original
		type seg struct {
			start, end int
			text       string
		}
		var segs []seg
		for i := 0; i+1 < len(points); i += 2 {
			text := words[rng.Intn(len(words))]
			s, e := len(string(runes[:points[i]])), len(string(runes[:points[i+1]]))
			segs = append(segs, seg{s, e, text})
			batch = append(batch, Edit{Range: buffer.Range{Start: d.Snapshot().PositionAt(s), End: d.Snapshot().PositionAt(e)}, Text: text})
		}
		for i := len(segs) - 1; i >= 0; i-- {
			model = model[:segs[i].start] + segs[i].text + model[segs[i].end:]
		}
		rng.Shuffle(len(batch), func(i, j int) { batch[i], batch[j] = batch[j], batch[i] })
		res := edit(t, d, batch, EditOptions{})
		if d.Text() != model {
			t.Fatalf("round %d: text = %q, want %q", round, d.Text(), model)
		}
		if res.Changed {
			undo(t, d)
			if d.Text() != original {
				t.Fatalf("round %d: after Undo %q, want %q", round, d.Text(), original)
			}
			redo(t, d)
			if d.Text() != model {
				t.Fatalf("round %d: after Redo %q, want %q", round, d.Text(), model)
			}
			other := newDoc(model)
			edit(t, other, res.Inverse, EditOptions{})
			if other.Text() != original {
				t.Fatalf("round %d: inverse gave %q, want %q", round, other.Text(), original)
			}
		}
	}
}

func TestSetURI(t *testing.T) {
	doc := New(FileURI("/w/a.txt"), "hello\n", NewOptions())
	if _, err := doc.Edit([]Edit{{Range: buffer.Range{}, Text: "x"}}, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	version, dirty := doc.Version(), doc.Dirty()
	type change struct{ old, new URI }
	var got []change
	stop := doc.OnURIChange(func(o, n URI) { got = append(got, change{o, n}) })

	doc.SetURI(FileURI("/w/b.txt"))
	doc.SetURI(FileURI("/w/b.txt"))
	if doc.URI() != FileURI("/w/b.txt") || doc.Version() != version || doc.Dirty() != dirty || doc.Text() != "xhello\n" {
		t.Errorf("uri %v version %d dirty %v text %q", doc.URI(), doc.Version(), doc.Dirty(), doc.Text())
	}
	if !doc.CanUndo() {
		t.Error("the history was lost")
	}
	want := []change{{FileURI("/w/a.txt"), FileURI("/w/b.txt")}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("changes = %v, want %v", got, want)
	}
	stop()
	doc.SetURI(FileURI("/w/c.txt"))
	if len(got) != 1 {
		t.Errorf("a stopped listener was told: %v", got)
	}
}

func TestReload(t *testing.T) {
	d := newDoc("old\ntext")
	edit(t, d, []Edit{ins(0, 0, "x")}, EditOptions{})
	var causes []Cause
	d.Subscribe(func(e ChangeEvent) { causes = append(causes, e.Cause) })
	d.Reload(fileio.ReadResult{
		Text: "café\nbar", Encoding: "windows1252", HasBOM: true, LineEnding: fileio.CRLF, LargeFile: true,
		Stat: fileio.FileStat{Size: 9},
	})
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"the text", d.Text(), "café\nbar"},
		{"the encoding", d.Encoding(), "windows1252"},
		{"the byte order mark", d.HasBOM(), true},
		{"the line ending", d.LineEnding(), fileio.CRLF},
		{"the stat", d.Stat().Size, int64(9)},
		{"the large file mode is not changed", d.LargeFile(), false},
		{"saved", d.Dirty(), false},
		{"one event", len(causes), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
	if len(causes) == 1 && causes[0] != CauseSetText {
		t.Errorf("cause %v", causes[0])
	}
	if d.CanUndo() {
		t.Error("the undo history survived the reload")
	}
}
