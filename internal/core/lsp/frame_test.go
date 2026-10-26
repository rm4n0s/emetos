package lsp

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/rm4n0s/errors"
)

func frame(body string) string {
	return "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

func TestReadFrame(t *testing.T) {
	long := strings.Repeat("x", 5000)
	tests := []struct {
		name    string
		input   string
		reader  func(io.Reader) io.Reader
		max     int
		want    []string
		wantEOF bool
	}{
		{name: "one message", input: frame(`{"a":1}`), want: []string{`{"a":1}`}, wantEOF: true},
		{name: "one byte at a time", input: frame(`{"a":1}`) + frame(`{"b":2}`), reader: iotest.OneByteReader, want: []string{`{"a":1}`, `{"b":2}`}, wantEOF: true},
		{name: "half reads", input: frame(`{"a":1}`) + frame(`[]`), reader: iotest.HalfReader, want: []string{`{"a":1}`, `[]`}, wantEOF: true},
		{name: "several messages in one read", input: frame("1") + frame("22") + frame("333"), want: []string{"1", "22", "333"}, wantEOF: true},
		{name: "content type with the old spelling of the charset", input: "Content-Length: 2\r\nContent-Type: application/vscode-jsonrpc; charset=utf8\r\n\r\n{}", want: []string{"{}"}, wantEOF: true},
		{name: "header names are case insensitive", input: "content-length: 2\r\n\r\n{}", want: []string{"{}"}, wantEOF: true},
		{name: "empty body", input: "Content-Length: 0\r\n\r\n", want: []string{""}, wantEOF: true},
		{name: "a body of 5000 bytes", input: frame(long), want: []string{long}, wantEOF: true},
		{name: "non-ASCII body counts bytes", input: frame(`"é€😀"`), want: []string{`"é€😀"`}, wantEOF: true},
		{name: "limit exactly the length", input: frame("12345"), max: 5, want: []string{"12345"}, wantEOF: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r io.Reader = strings.NewReader(tt.input)
			if tt.reader != nil {
				r = tt.reader(r)
			}
			br := bufio.NewReader(r)
			for i, want := range tt.want {
				got, err := ReadFrame(br, tt.max)
				if err != nil {
					t.Fatalf("message %d: %v", i, err)
				}
				if string(got) != want {
					t.Errorf("message %d = %q, want %q", i, got, want)
				}
			}
			if _, err := ReadFrame(br, tt.max); tt.wantEOF && !errors.Is(err, io.EOF) {
				t.Errorf("after the last message: %v, want io.EOF", err)
			}
		})
	}
}

func TestReadFrameErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		route string
	}{
		{"header line without a colon", "Content-Length 5\r\n\r\n", 0, "ReadFrame.FrameHeaderInvalid"},
		{"header line ends in a bare line feed", "Content-Length: 5\n\n", 0, "ReadFrame.FrameHeaderInvalid"},
		{"no Content-Length", "Content-Type: x\r\n\r\n{}", 0, "ReadFrame.FrameLengthMissing"},
		{"empty header block", "\r\n{}", 0, "ReadFrame.FrameLengthMissing"},
		{"Content-Length is not a number", "Content-Length: abc\r\n\r\n", 0, "ReadFrame.FrameLengthInvalid"},
		{"Content-Length is negative", "Content-Length: -1\r\n\r\n", 0, "ReadFrame.FrameLengthInvalid"},
		{"Content-Length overflows", "Content-Length: 99999999999999999999\r\n\r\n", 0, "ReadFrame.FrameLengthInvalid"},
		{"charset other than utf-8", "Content-Length: 2\r\nContent-Type: text/plain; charset=latin1\r\n\r\n{}", 0, "ReadFrame.FrameCharsetUnsupported"},
		{"message over the limit", "Content-Length: 11\r\n\r\n{}", 10, "ReadFrame.FrameTooLarge"},
		{"header line over the buffer", "X-Long: " + strings.Repeat("a", 5000) + "\r\n", 0, "ReadFrame.FrameHeaderTooLong"},
		{"too many headers", strings.Repeat("X-A: b\r\n", 40), 0, "ReadFrame.FrameHeadersTooMany"},
		{"end inside the header", "Content-Length: 2\r\n", 0, "ReadFrame.FrameTruncated"},
		{"end inside the first header line", "Content-Len", 0, "ReadFrame.FrameTruncated"},
		{"end inside the body", "Content-Length: 10\r\n\r\n{}", 0, "ReadFrame.FrameTruncated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadFrame(bufio.NewReaderSize(strings.NewReader(tt.input), 4096), tt.max)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want it to contain %s", appErr.Route(), tt.route)
			}
		})
	}
}

func TestReadFrameReadFailure(t *testing.T) {
	boom := errors.New("Boom", "the reader failed")
	tests := []struct {
		name  string
		input io.Reader
	}{
		{"in the header", io.MultiReader(strings.NewReader("Content-Len"), iotest.ErrReader(boom))},
		{"in the body", io.MultiReader(strings.NewReader("Content-Length: 10\r\n\r\n{}"), iotest.ErrReader(boom))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadFrame(bufio.NewReader(tt.input), 0)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute("ReadFrame.FrameReadFailed") {
				t.Errorf("route = %s", appErr.Route())
			}
			if !errors.Is(err, boom) {
				t.Errorf("the reader's error is lost: %v", err)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWriteFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, []byte(`{"é":1}`)); err != nil {
		t.Fatal(err)
	}
	if want := "Content-Length: 8\r\n\r\n{\"é\":1}"; buf.String() != want {
		t.Errorf("frame = %q, want %q", buf.String(), want)
	}
	got, err := ReadFrame(bufio.NewReader(&buf), 0)
	if err != nil || string(got) != `{"é":1}` {
		t.Errorf("round trip = %q, %v", got, err)
	}

	err = WriteFrame(failingWriter{}, []byte("{}"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("WriteFrame.FrameWriteFailed") {
		t.Fatalf("expected the route WriteFrame.FrameWriteFailed, got %v", err)
	}
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("the writer's error is lost")
	}
}
