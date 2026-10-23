package brackets

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// bigSource is a file of 100,000 lines of code with brackets, as a Go file has them.
func bigSource() *fakeSource {
	var sb strings.Builder
	for i := 0; i < 25000; i++ {
		sb.WriteString("func f(a []int, b map[string]int) (int, error) {\n\tif len(a) > 0 {\n\t\treturn a[0] + b[\"x\"], nil\n\t}\n")
	}
	return plain(1, sb.String())
}

// BenchmarkPairsAfterAnEdit measures what the first query after an edit costs in a file of
// 100,000 lines: the lines the edit touched are read again and all the brackets are paired
// again (SPEC §14: typing must keep the frame under 8 ms).
func BenchmarkPairsAfterAnEdit(b *testing.B) {
	src := bigSource()
	ix := New(src, testLanguages{1: NewLanguage(1, curly, nil, false)}, true)
	view := buffer.Range{Start: buffer.Position{Line: 50000}, End: buffer.Position{Line: 50050}}
	ix.Pairs(view) // the first resolution reads every line
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Apply([]Edited{{From: 50000, To: 50000, Count: 1}})
		ix.Pairs(view)
	}
}

// BenchmarkFirstPairs measures the first resolution of a file of 100,000 lines.
func BenchmarkFirstPairs(b *testing.B) {
	src := bigSource()
	langs := testLanguages{1: NewLanguage(1, curly, nil, false)}
	view := buffer.Range{Start: buffer.Position{Line: 50000}, End: buffer.Position{Line: 50050}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New(src, langs, true).Pairs(view)
	}
}

// BenchmarkBracketsOfAScreen measures the query of the editor on every frame once the pairs are
// made: the brackets of 50 lines in the middle of the file.
func BenchmarkBracketsOfAScreen(b *testing.B) {
	src := bigSource()
	ix := New(src, testLanguages{1: NewLanguage(1, curly, nil, false)}, true)
	view := buffer.Range{Start: buffer.Position{Line: 50000}, End: buffer.Position{Line: 50050}}
	ix.Pairs(view)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Brackets(view, true)
	}
}
