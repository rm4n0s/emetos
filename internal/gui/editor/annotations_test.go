package editor

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

const annotationSample = "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"

func annotationLines(vm *ViewModel) []int {
	var out []int
	for _, a := range vm.LineAnnotations() {
		out = append(out, a.Line)
	}
	return out
}

func TestLineAnnotationsFollowTheEditsOfTheDocument(t *testing.T) {
	end := func(line int) buffer.Position {
		return buffer.Position{Line: line, Col: len(strings.Split(annotationSample, "\n")[line])}
	}
	tests := []struct {
		name string
		edit document.Edit
		want []int // the lines of the annotations on lines 1 and 3 after the edit
	}{
		{"nothing", document.Edit{Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 0)}, Text: ""}, []int{1, 3}},
		{"a line before them", document.Edit{Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 0)}, Text: "// c\n"}, []int{2, 4}},
		{"typing at the start of the first line", document.Edit{Range: buffer.Range{Start: positionOf(1, 0), End: positionOf(1, 0)}, Text: "x"}, []int{1, 3}},
		{"typing at the end of an annotated line", document.Edit{Range: buffer.Range{Start: end(3), End: end(3)}, Text: " // c"}, []int{1, 3}},
		{"a new line at the end of an annotated line", document.Edit{Range: buffer.Range{Start: end(3), End: end(3)}, Text: "\n\t"}, []int{1, 3}},
		{"a line after them", document.Edit{Range: buffer.Range{Start: end(4), End: end(4)}, Text: "\n"}, []int{1, 3}},
		{"an edit inside an annotated line", document.Edit{Range: buffer.Range{Start: positionOf(3, 3), End: positionOf(3, 5)}, Text: "XYZ"}, []int{1, 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := NewViewModel(newTestDoc(annotationSample), goOptions(), nil)
			vm.SetLineAnnotations([]LineAnnotation{{Line: 1, Text: "empty"}, {Line: 3, Text: "call"}})
			if got := annotationLines(vm); !slices.Equal(got, []int{1, 3}) {
				t.Fatalf("before the edit: lines %v", got)
			}
			if _, err := vm.Document().Edit([]document.Edit{tc.edit}, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			if got := annotationLines(vm); !slices.Equal(got, tc.want) {
				t.Errorf("lines %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLineAnnotationsKeepTheirTextAndMoveWithTheirLinesWhenLinesAreDeleted(t *testing.T) {
	vm := NewViewModel(newTestDoc(annotationSample), goOptions(), nil)
	vm.SetLineAnnotations([]LineAnnotation{{Line: 3, Text: "call", Color: "c", Hover: "h"}})
	// the first three lines go: the call is on line 0
	if _, err := vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(3, 0)}}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	got := vm.LineAnnotations()
	if len(got) != 1 || got[0].Line != 0 || got[0].Text != "call" || got[0].Color != "c" || got[0].Hover != "h" {
		t.Errorf("annotations %+v", got)
	}
}

func TestLineAnnotationsDropWhatTheDocumentLacks(t *testing.T) {
	vm := NewViewModel(newTestDoc(annotationSample), goOptions(), nil)
	vm.SetLineAnnotations([]LineAnnotation{{Line: -1, Text: "a"}, {Line: 2, Text: ""}, {Line: 99, Text: "c"}, {Line: 0, Text: "kept"}})
	if got := vm.LineAnnotations(); len(got) != 1 || got[0].Text != "kept" || vm.LineAnnotationCount() != 1 {
		t.Errorf("annotations %+v", got)
	}
	vm.SetLineAnnotations(nil)
	if vm.LineAnnotationCount() != 0 || len(vm.LineAnnotations()) != 0 {
		t.Errorf("a new empty list left %d", vm.LineAnnotationCount())
	}
	vm.SetLineAnnotations([]LineAnnotation{{Line: 0, Text: "x"}})
	vm.DisposeLineAnnotations()
	if vm.LineAnnotationCount() != 0 || len(vm.Document().DecorationsOf(vm.owner(annotationsOwner))) != 0 {
		t.Error("Dispose left decorations")
	}
}

func TestTwoEditorsOfADocumentHaveTheirOwnAnnotations(t *testing.T) {
	doc := newTestDoc(annotationSample)
	a, b := NewViewModel(doc, goOptions(), nil), NewViewModel(doc, goOptions(), nil)
	a.SetLineAnnotations([]LineAnnotation{{Line: 0, Text: "a"}})
	b.SetLineAnnotations([]LineAnnotation{{Line: 2, Text: "b"}, {Line: 3, Text: "b"}})
	a.SetLineAnnotations(nil)
	if got := annotationLines(b); !slices.Equal(got, []int{2, 3}) {
		t.Errorf("b lost its annotations: %v", got)
	}
	if got := annotationLines(a); len(got) != 0 {
		t.Errorf("a kept %v", got)
	}
}

func TestAnnotationsDoNotMoveTheText(t *testing.T) {
	vm := NewViewModel(newTestDoc(annotationSample), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
	layer := ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	before := []float64{g.x(3, 5), g.rowEnd(3), vm.Projection().Height()}
	rowsBefore := vm.Projection().RowCount()
	widthBefore := vm.ContentWidth()
	vm.SetLineAnnotations([]LineAnnotation{{Line: 3, Text: strings.Repeat("long annotation ", 40)}})
	ts.Pump(16 * time.Millisecond)
	after := []float64{g.x(3, 5), g.rowEnd(3), vm.Projection().Height()}
	if !slices.Equal(before, after) || vm.Projection().RowCount() != rowsBefore || vm.ContentWidth() != widthBefore {
		t.Errorf("the annotation moved the text: %v -> %v, rows %d -> %d, width %v -> %v", before, after, rowsBefore, vm.Projection().RowCount(), widthBefore, vm.ContentWidth())
	}
	// the position under the pointer past the end of the line is still the end of the line
	if got := layer.PositionAt(ui.Offset{Dx: g.rowEnd(3) + annotationMargin + 30, Dy: 3*19 + 5}); got != positionOf(3, len("\tprintln(\"hello\")")) {
		t.Errorf("the position past the end of the line is %v", got)
	}
}

func TestAnnotationAtFindsTheTextOfAnAnnotationOnly(t *testing.T) {
	vm := NewViewModel(newTestDoc(annotationSample), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	layer := ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	vm.SetLineAnnotations([]LineAnnotation{{Line: 3, Text: "first"}, {Line: 3, Text: "second"}, {Line: 4, Text: ""}})
	ts.Pump(16 * time.Millisecond)
	y := 3*19 + 5.0
	start := g.rowEnd(3) + annotationMargin
	first := g.textWidth("first")
	tests := []struct {
		name string
		at   ui.Offset
		want annotationHit
		ok   bool
	}{
		{"the text of the line", ui.Offset{Dx: g.x(3, 4), Dy: y}, annotationHit{}, false},
		{"the margin", ui.Offset{Dx: g.rowEnd(3) + annotationMargin/2, Dy: y}, annotationHit{}, false},
		{"the start of the first", ui.Offset{Dx: start + 1, Dy: y}, annotationHit{line: 3, index: 0}, true},
		{"the end of the first", ui.Offset{Dx: start + first - 1, Dy: y}, annotationHit{line: 3, index: 0}, true},
		{"the gap between them", ui.Offset{Dx: start + first + annotationGap/2, Dy: y}, annotationHit{}, false},
		{"the second", ui.Offset{Dx: start + first + annotationGap + 3, Dy: y}, annotationHit{line: 3, index: 1}, true},
		{"past the second", ui.Offset{Dx: start + first + annotationGap + g.textWidth("second") + 5, Dy: y}, annotationHit{}, false},
		{"another row", ui.Offset{Dx: start + 1, Dy: 2*19 + 5}, annotationHit{}, false},
	}
	for _, tc := range tests {
		got, ok := layer.AnnotationAt(tc.at)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestAnnotationsOfAWrappedLineAreAfterItsLastRow(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 400, Height: 400}, strings.Repeat("word ", 60)+"\nshort\n", nil)
	vm, ts := r.vm, r.ts
	layer := ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
	vm.SetLineAnnotations([]LineAnnotation{{Line: 0, Text: "note"}})
	ts.Pump(16 * time.Millisecond)
	first, count := vm.Projection().LineRows(0)
	if count < 2 {
		t.Fatalf("the long line is on %d rows", count)
	}
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	last := first + count - 1
	y := vm.Projection().RowTop(last) + 5
	if _, ok := layer.AnnotationAt(ui.Offset{Dx: g.rowEnd(last) + annotationMargin + 2, Dy: y}); !ok {
		t.Error("the annotation is not after the last row of the line")
	}
	if _, ok := layer.AnnotationAt(ui.Offset{Dx: g.rowEnd(first) + annotationMargin + 2, Dy: 5}); ok {
		t.Error("the annotation is after the first row of the line")
	}
}
