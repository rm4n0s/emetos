package buffer

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/rm4n0s/errors"
)

func TestBuilderKeepsCharactersWhole(t *testing.T) {
	// 'é' is two bytes; every chunk size and every cut of the input must keep
	// characters in one chunk.
	text := strings.Repeat("é", 100000) + "\n" + strings.Repeat("日", 40000) + "😀"
	readers := map[string]func() io.Reader{
		"whole":    func() io.Reader { return strings.NewReader(text) },
		"one byte": func() io.Reader { return iotest.OneByteReader(strings.NewReader(text)) },
		"half":     func() io.Reader { return iotest.HalfReader(strings.NewReader(text)) },
		"data err": func() io.Reader { return iotest.DataErrReader(strings.NewReader(text)) },
	}
	for name, mk := range readers {
		t.Run(name, func(t *testing.T) {
			b, err := NewFromReader(mk())
			if err != nil {
				t.Fatalf("NewFromReader: %v", err)
			}
			checkTree(t, b, text)
			for c := range b.Chunks() {
				if len(c) > maxChunk {
					t.Errorf("chunk of %d bytes", len(c))
				}
				if strings.ContainsRune(c, '�') {
					t.Fatal("a chunk ends in the middle of a character")
				}
			}
			checkIndex(t, b.Snapshot(), text)
		})
	}
}

func TestBuilderChunkSizes(t *testing.T) {
	var bld Builder
	bld.WriteString(strings.Repeat("a", 100000))
	bld.WriteString("b")
	b := bld.Buffer()
	var sizes []int
	for c := range b.Chunks() {
		sizes = append(sizes, len(c))
	}
	// Full chunks of 64 KiB, then the rest.
	if len(sizes) != 2 || sizes[0] != maxChunk || sizes[1] != 100001-maxChunk {
		t.Errorf("chunk sizes = %v", sizes)
	}
	if bld.Buffer().Len() != 0 {
		t.Error("the builder still holds text after Buffer")
	}
}

func TestBuilderManySmallWrites(t *testing.T) {
	var bld Builder
	var want strings.Builder
	for i := 0; i < 5000; i++ {
		s := "line " + string(rune('a'+i%26)) + "\n"
		bld.WriteString(s)
		want.WriteString(s)
	}
	b := bld.Buffer()
	checkAll(t, b, want.String())
	chunks := 0
	for range b.Chunks() {
		chunks++
	}
	if chunks != 1 {
		t.Errorf("small writes made %d chunks, want 1", chunks)
	}
}

func TestBuilderEmpty(t *testing.T) {
	b, err := NewFromReader(strings.NewReader(""))
	if err != nil {
		t.Fatalf("NewFromReader: %v", err)
	}
	checkAll(t, b, "")
	if b.LineCount() != 1 {
		t.Errorf("LineCount = %d, want 1", b.LineCount())
	}
}

func TestNewFromReaderFailure(t *testing.T) {
	r := io.MultiReader(strings.NewReader("some text"), iotest.ErrReader(io.ErrClosedPipe))
	b, err := NewFromReader(r)
	if b != nil {
		t.Error("a Buffer came with the error")
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("NewFromReader") || !appErr.HasRoute("BufferReadFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

// A truncated sequence at the end of the input is kept as it is.
func TestBuilderKeepsInvalidBytes(t *testing.T) {
	text := "ab\xe6\x97"
	b, err := NewFromReader(strings.NewReader(text))
	if err != nil {
		t.Fatalf("NewFromReader: %v", err)
	}
	checkTree(t, b, text)
}
