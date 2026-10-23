package tokens

import (
	"fmt"
	"strings"
	"testing"
)

// goLines makes a text of n lines that look like Go code.
func goLines(n int) string {
	var b strings.Builder
	for i := range n {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "func handler%d(ctx context.Context, request *Request) error {\n", i)
		case 1:
			fmt.Fprintf(&b, "\tresult, err := service.Process(ctx, request.Items[%d])\n", i%64)
		case 2:
			b.WriteString("\tif err != nil { return fmt.Errorf(\"processing: %w\", err) }\n")
		default:
			b.WriteString("}\n")
		}
	}
	return b.String()
}

// BenchmarkTokenizeGoLines is the speed of the tokenizer on Go code: the time to tokenize 1,000
// lines, converting the tokens to the compact form.
func BenchmarkTokenizeGoLines(b *testing.B) {
	g := grammarFor(b, "go")
	text := goLines(1000)
	// The first line made with a grammar compiles its rules; that is not what is measured.
	warm := New(newDoc(text), g, Options{})
	warm.Settle()
	warm.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tok := New(newDoc(text), g, Options{})
		tok.Settle()
		tok.Close()
	}
}
