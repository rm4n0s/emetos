package language

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// BenchmarkBundled is the time to make the registry of the bundled languages (it reads the manifests
// of the embedded extensions, not the grammars): it runs once when the application starts.
func BenchmarkBundled(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Bundled(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFirstLine is what a language costs the first time a file of it is opened: the grammar is
// read from the embedded files and made (NewGrammarWith, on the thread that opens the file), and the
// first line is tokenized, which compiles the patterns the line needs (on the tokenizer's goroutine,
// or in the first frame's tokenization budget). Every file of the language opened later shares that
// grammar. The two parts are reported apart as grammar-ms and line-ms. A new registry for every
// operation is made out of the timing.
func BenchmarkFirstLine(b *testing.B) {
	lines := map[string]string{
		"go":         `func handler(ctx context.Context, request *Request) error { return nil }`,
		"typescript": `export async function handler(ctx: Context, request: Request): Promise<void> { return; }`,
		"markdown":   "# A title with `code` and **bold** text and a [link](https://example.com)",
		"json":       `{"name": "emetos", "version": 1, "list": [1, 2, 3], "nested": {"a": null}}`,
		"python":     `def handler(ctx, request: Request) -> None: return f"{ctx}"`,
		"html":       `<div class="a" id="b"><span>text</span><script>let a = 1;</script></div>`,
	}
	for _, id := range []string{"go", "typescript", "markdown", "json", "python", "html"} {
		b.Run(id, func(b *testing.B) {
			b.ReportAllocs()
			var grammarTime, lineTime time.Duration
			for b.Loop() {
				b.StopTimer()
				r, err := Bundled()
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				t0 := time.Now()
				g, err := r.NewGrammarWith(id, GrammarSettings{Cache: regex.NewCache(1 << 14)})
				if err != nil {
					b.Fatal(err)
				}
				t1 := time.Now()
				if res := g.TokenizeLine(lines[id], nil, 0); len(res.Tokens) == 0 {
					b.Fatal("no tokens")
				}
				grammarTime += t1.Sub(t0)
				lineTime += time.Since(t1)
			}
			b.ReportMetric(float64(grammarTime.Microseconds())/1000/float64(b.N), "grammar-ms")
			b.ReportMetric(float64(lineTime.Microseconds())/1000/float64(b.N), "line-ms")
		})
	}
}
