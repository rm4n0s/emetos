package dap

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

func frameOf(body string) string {
	return "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = wantRouteError(t, err, route)
}

func wantRouteError(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), route)
	}
	return appErr
}

func TestReadFrameReads(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		reader func(io.Reader) io.Reader
		want   []string
	}{
		{"one message", frameOf(`{"a":1}`), nil, []string{`{"a":1}`}},
		{"several messages in one read", frameOf("one") + frameOf("two") + frameOf("three"), nil, []string{"one", "two", "three"}},
		{"headers split byte by byte", frameOf(`{"seq":1}`) + frameOf(`{"seq":2}`), iotest.OneByteReader, []string{`{"seq":1}`, `{"seq":2}`}},
		{"a body across reads", frameOf(strings.Repeat("x", 100000)), func(r io.Reader) io.Reader { return iotest.HalfReader(r) }, []string{strings.Repeat("x", 100000)}},
		{"bare line feeds, as VS Code allows", "Content-Length: 2\n\nhi", nil, []string{"hi"}},
		{"the name in any case and spaces around the value", "content-length:   3  \r\n\r\nabc", nil, []string{"abc"}},
		{"another header is ignored", "Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: 2\r\n\r\nhi", nil, []string{"hi"}},
		{"an empty body", frameOf("") + frameOf("x"), nil, []string{"", "x"}},
		{"a body with a multi-byte character", frameOf("héé"), nil, []string{"héé"}},
		{"a body that looks like a header", frameOf("Content-Length: 9\r\n\r\n"), nil, []string{"Content-Length: 9\r\n\r\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r io.Reader = strings.NewReader(tt.input)
			if tt.reader != nil {
				r = tt.reader(r)
			}
			br := bufio.NewReaderSize(r, 4096)
			for i, want := range tt.want {
				got, err := ReadFrame(br, 0)
				if err != nil {
					t.Fatalf("message %d: %v", i, err)
				}
				if string(got) != want {
					t.Errorf("message %d = %q, want %q", i, got, want)
				}
			}
			if _, err := ReadFrame(br, 0); err != io.EOF {
				t.Errorf("after the last message: %v, want io.EOF", err)
			}
		})
	}
}

// failAfter reads from r and then fails with err instead of the end of the input.
type failAfter struct {
	r   io.Reader
	err error
}

func (f failAfter) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		err = f.err
	}
	return n, err
}

func TestReadFrameFailures(t *testing.T) {
	boom := errors.New("Boom", "the reader broke")
	tests := []struct {
		name    string
		input   io.Reader
		max     int
		route   string
		reason  string
		buffer  int
		wantErr error
	}{
		{"garbage instead of a header", strings.NewReader("hello there\r\n\r\n"), 0, "ReadFrame.DapProtocolInvalid", "header without colon", 0, nil},
		{"a header line that is longer than the buffer", strings.NewReader(strings.Repeat("a", 6000)), 0, "ReadFrame.DapProtocolInvalid", "header too long", 4096, nil},
		{"too many header lines", strings.NewReader(strings.Repeat("X-Y: z\r\n", 40)), 0, "ReadFrame.DapProtocolInvalid", "too many headers", 0, nil},
		{"no Content-Length", strings.NewReader("X-Y: z\r\n\r\nbody"), 0, "ReadFrame.DapProtocolInvalid", "length missing", 0, nil},
		{"a Content-Length that is not a number", strings.NewReader("Content-Length: abc\r\n\r\n"), 0, "ReadFrame.DapProtocolInvalid", "length invalid", 0, nil},
		{"a negative Content-Length", strings.NewReader("Content-Length: -1\r\n\r\n"), 0, "ReadFrame.DapProtocolInvalid", "length invalid", 0, nil},
		{"a message over the limit", strings.NewReader(frameOf(strings.Repeat("x", 100))), 50, "ReadFrame.DapMessageTooLarge", "", 0, nil},
		{"a message over the default limit, without reading its body", strings.NewReader("Content-Length: 67108865\r\n\r\n"), 0, "ReadFrame.DapMessageTooLarge", "", 0, nil},
		{"the input ends inside a header", strings.NewReader("Content-Len"), 0, "ReadFrame.DapFrameTruncated", "", 0, nil},
		{"the input ends after the header lines", strings.NewReader("Content-Length: 5\r\n"), 0, "ReadFrame.DapFrameTruncated", "", 0, nil},
		{"the input ends inside a body", strings.NewReader("Content-Length: 10\r\n\r\nabc"), 0, "ReadFrame.DapFrameTruncated", "", 0, nil},
		{"a read that fails inside a header", failAfter{strings.NewReader("Content-Len"), boom}, 0, "ReadFrame.DapFrameReadFailed", "", 0, boom},
		{"a read that fails inside a body", failAfter{strings.NewReader("Content-Length: 10\r\n\r\nabc"), boom}, 0, "ReadFrame.DapFrameReadFailed", "", 0, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := tt.buffer
			if size == 0 {
				size = 4096
			}
			_, err := ReadFrame(bufio.NewReaderSize(tt.input, size), tt.max)
			if err == nil {
				t.Fatal("no error")
			}
			appErr := wantRouteError(t, err, tt.route)
			if tt.reason != "" && appErr.Metadata["reason"] != tt.reason {
				t.Errorf("reason = %v, want %q", appErr.Metadata["reason"], tt.reason)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("the cause is lost: %v", err)
			}
		})
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestWriteFrame(t *testing.T) {
	var b bytes.Buffer
	if err := WriteFrame(&b, []byte(`{"seq":1}`)); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "Content-Length: 9\r\n\r\n{\"seq\":1}"; got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}
	back, err := ReadFrame(bufio.NewReader(&b), 0)
	if err != nil || string(back) != `{"seq":1}` {
		t.Errorf("round trip = %q, %v", back, err)
	}
	err = WriteFrame(failingWriter{io.ErrClosedPipe}, []byte("x"))
	wantRoute(t, err, "WriteFrame.DapFrameWriteFailed")
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("the cause is lost: %v", err)
	}
}
