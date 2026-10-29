package containerapi

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// frameBytes encodes one frame of the multiplexed stream.
func frameBytes(stream byte, data []byte) []byte {
	out := make([]byte, 8, 8+len(data))
	out[0] = stream
	binary.BigEndian.PutUint32(out[4:], uint32(len(data)))
	return append(out, data...)
}

// chunkReader returns the bytes in pieces of random sizes (at least one byte, so a header may be cut anywhere), as a socket
// does.
type chunkReader struct {
	data []byte
	rng  *rand.Rand
	max  int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := 1 + r.rng.Intn(r.max)
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.data) {
		n = len(r.data)
	}
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

// failingReader gives some bytes, then an error.
type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

func TestDemuxWithSeededRandomFrames(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		type want struct {
			stream Stream
			data   string
		}
		var wants []want
		var wire bytes.Buffer
		for i, n := 0, rng.Intn(60); i < n; i++ {
			stream := Stream(1 + rng.Intn(2))
			if rng.Intn(10) == 0 {
				stream = StreamStdin
			}
			size := rng.Intn(300)
			if rng.Intn(15) == 0 {
				size = rng.Intn(70000) // a frame larger than the read buffers
			}
			if rng.Intn(10) == 0 {
				size = 0
			}
			data := make([]byte, size)
			rng.Read(data)
			wants = append(wants, want{stream, string(data)})
			wire.Write(frameBytes(byte(stream), data))
		}
		var got []want
		err := Demux(&chunkReader{data: wire.Bytes(), rng: rng, max: 1 + rng.Intn(100)}, func(f Frame) error {
			got = append(got, want{f.Stream, string(f.Data)}) // a copy: Data is reused
			return nil
		})
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if len(got) != len(wants) {
			t.Fatalf("seed %d: %d frames, want %d", seed, len(got), len(wants))
		}
		for i := range wants {
			if got[i] != wants[i] {
				t.Fatalf("seed %d: frame %d differs (stream %v/%v, %d/%d bytes)", seed, i, got[i].stream, wants[i].stream, len(got[i].data), len(wants[i].data))
			}
		}
	}
}

func TestDemuxFailures(t *testing.T) {
	bigHeader := frameBytes(1, nil)
	binary.BigEndian.PutUint32(bigHeader[4:], MaxFrame+1)
	reserved := frameBytes(1, []byte("x"))
	reserved[2] = 7
	for _, tc := range []struct {
		name   string
		r      io.Reader
		route  string
		reason string
		frames int
	}{
		{name: "an empty stream is a normal end", r: bytes.NewReader(nil)},
		{name: "a header cut short", r: bytes.NewReader(frameBytes(1, []byte("abc"))[:5]), route: "Demux.ContainerAPIFrameInvalid", reason: "truncated header"},
		{name: "a payload cut short", r: bytes.NewReader(frameBytes(1, []byte("abcdef"))[:11]), route: "Demux.ContainerAPIFrameInvalid", reason: "truncated frame"},
		{name: "a header and no payload", r: bytes.NewReader(frameBytes(2, []byte("abc"))[:8]), route: "Demux.ContainerAPIFrameInvalid", reason: "truncated frame"},
		{name: "an unknown stream", r: bytes.NewReader(frameBytes(3, []byte("x"))), route: "Demux.ContainerAPIFrameInvalid", reason: "unknown stream"},
		{name: "reserved bytes set", r: bytes.NewReader(reserved), route: "Demux.ContainerAPIFrameInvalid", reason: "reserved bytes"},
		{name: "a frame larger than the limit", r: bytes.NewReader(bigHeader), route: "Demux.ContainerAPIFrameInvalid", reason: "too long"},
		{name: "a good frame, then a bad one", r: bytes.NewReader(append(frameBytes(1, []byte("ok")), frameBytes(9, nil)...)), route: "Demux.ContainerAPIFrameInvalid", reason: "unknown stream", frames: 1},
		{name: "the reader fails between frames", r: &failingReader{data: frameBytes(1, []byte("ok")), err: io.ErrClosedPipe}, route: "Demux.ContainerAPIStreamFailed", frames: 1},
		{name: "the reader fails inside a payload", r: &failingReader{data: frameBytes(1, []byte("abcdef"))[:10], err: io.ErrClosedPipe}, route: "Demux.ContainerAPIStreamFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := 0
			err := Demux(tc.r, func(Frame) error { frames++; return nil })
			if frames != tc.frames {
				t.Errorf("%d frames were handed over, want %d", frames, tc.frames)
			}
			if tc.route == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			appErr := routeOf(t, err, tc.route)
			if tc.reason != "" && appErr.Metadata["reason"] != tc.reason {
				t.Errorf("reason = %v, want %s", appErr.Metadata["reason"], tc.reason)
			}
			if strings.HasSuffix(tc.route, "StreamFailed") && !errors.Is(err, io.ErrClosedPipe) {
				t.Errorf("the reader's error is lost: %v", err)
			}
		})
	}
}

func TestDemuxStopsWithTheCallbacksError(t *testing.T) {
	stop := &errors.Error{Tag: "TestStop"}
	n := 0
	err := Demux(bytes.NewReader(append(frameBytes(1, []byte("a")), frameBytes(1, []byte("b"))...)), func(Frame) error {
		n++
		return stop
	})
	if err != stop || n != 1 {
		t.Errorf("err = %v after %d frames, want the callback's error after 1", err, n)
	}
}

func TestLineSplitter(t *testing.T) {
	type line struct {
		s    Stream
		text string
	}
	for _, tc := range []struct {
		name  string
		max   int
		feeds []line // each is a frame; text may hold line ends
		want  []line
	}{
		{name: "whole lines", feeds: []line{{1, "a\nb\n"}}, want: []line{{1, "a"}, {1, "b"}}},
		{name: "a line over two frames", feeds: []line{{1, "hel"}, {1, "lo\n"}}, want: []line{{1, "hello"}}},
		{name: "streams are kept apart", feeds: []line{{1, "o1 "}, {2, "e1\n"}, {1, "o2\n"}}, want: []line{{2, "e1"}, {1, "o1 o2"}}},
		{name: "CRLF", feeds: []line{{1, "a\r\nb\r\n"}}, want: []line{{1, "a"}, {1, "b"}}},
		{name: "an empty line", feeds: []line{{1, "\n\nx\n"}}, want: []line{{1, ""}, {1, ""}, {1, "x"}}},
		{name: "the end without a line end is flushed", feeds: []line{{1, "a\nb"}}, want: []line{{1, "a"}, {1, "b"}}},
		{name: "a long line is cut", max: 4, feeds: []line{{1, "abcdefghij\n"}}, want: []line{{1, "abcd"}, {1, "efgh"}, {1, "ij"}}},
		{name: "a long unfinished line is cut as it grows", max: 3, feeds: []line{{1, "abcdefg"}}, want: []line{{1, "abc"}, {1, "def"}, {1, "g"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []line
			emit := func(s Stream, text string) error { got = append(got, line{s, text}); return nil }
			ls := LineSplitter{Max: tc.max}
			for _, f := range tc.feeds {
				if err := ls.Write(f.s, []byte(f.text), emit); err != nil {
					t.Fatal(err)
				}
			}
			if err := ls.Flush(emit); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("lines = %v, want %v", got, tc.want)
			}
			// the order across streams is not part of the contract, the order within one is
			for _, s := range []Stream{1, 2} {
				var g, w []string
				for _, l := range got {
					if l.s == s {
						g = append(g, l.text)
					}
				}
				for _, l := range tc.want {
					if l.s == s {
						w = append(w, l.text)
					}
				}
				if strings.Join(g, "|") != strings.Join(w, "|") {
					t.Errorf("stream %v: %q, want %q", s, g, w)
				}
			}
		})
	}
}

func TestLineSplitterWithSeededRandomText(t *testing.T) {
	for seed := int64(1); seed <= 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var text strings.Builder
		for i, n := 0, rng.Intn(50); i < n; i++ {
			for j, m := 0, rng.Intn(20); j < m; j++ {
				text.WriteByte(byte('a' + rng.Intn(26)))
			}
			text.WriteByte('\n')
		}
		var want []string
		if text.Len() > 0 {
			want = strings.Split(strings.TrimSuffix(text.String(), "\n"), "\n")
		}
		var got []string
		ls := LineSplitter{}
		data := []byte(text.String())
		for len(data) > 0 {
			n := 1 + rng.Intn(13)
			if n > len(data) {
				n = len(data)
			}
			if err := ls.Write(StreamStdout, data[:n], func(_ Stream, s string) error { got = append(got, s); return nil }); err != nil {
				t.Fatal(err)
			}
			data = data[n:]
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") || len(got) != len(want) {
			t.Fatalf("seed %d: %d lines, want %d", seed, len(got), len(want))
		}
	}
}

func TestStreamString(t *testing.T) {
	for s, want := range map[Stream]string{StreamStdin: "stdin", StreamStdout: "stdout", StreamStderr: "stderr", 7: "stream7"} {
		if s.String() != want {
			t.Errorf("Stream(%d) = %q, want %q", s, s.String(), want)
		}
	}
}
