package buffer

import (
	"fmt"
	"strings"
	"testing"
)

// benchBuffer is a buffer of 100,000 lines of about 40 bytes.
func benchBuffer(b *testing.B) *Buffer {
	b.Helper()
	var sb strings.Builder
	for i := 0; i < 100000; i++ {
		fmt.Fprintf(&sb, "line %06d of the benchmark text, here\n", i)
	}
	buf, err := NewFromReader(strings.NewReader(sb.String()))
	if err != nil {
		b.Fatal(err)
	}
	return buf
}

// BenchmarkInsertTyping types one byte at a time in the middle of the buffer.
func BenchmarkInsertTyping(b *testing.B) {
	buf := benchBuffer(b)
	at := buf.Len() / 2
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := buf.Insert(at+i, "x"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInsertTypingScattered types in a different place every time, so
// every byte starts a new piece.
func BenchmarkInsertTypingScattered(b *testing.B) {
	buf := benchBuffer(b)
	n := buf.Len()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := buf.Insert((i*7919)%n, "x"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLineContent(b *testing.B) {
	buf := benchBuffer(b)
	lines := buf.LineCount()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buf.LineContent((i * 7919) % lines); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLineContentEdited reads lines of a buffer made of many pieces.
func BenchmarkLineContentEdited(b *testing.B) {
	buf := benchBuffer(b)
	n := buf.Len()
	for i := 0; i < 5000; i++ {
		must(buf.Insert((i*104729)%n, "x\n"))
	}
	lines := buf.LineCount()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buf.LineContent((i * 7919) % lines); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPositionAt(b *testing.B) {
	buf := benchBuffer(b)
	n := buf.Len()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.PositionAt((i * 7919) % n)
	}
}

func BenchmarkOffsetAt(b *testing.B) {
	buf := benchBuffer(b)
	lines := buf.LineCount()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.OffsetAt(Position{Line: (i * 7919) % lines, Col: 10})
	}
}
