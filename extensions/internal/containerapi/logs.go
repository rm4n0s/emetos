package containerapi

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// Stream says which output a frame or a line belongs to.
type Stream uint8

// The streams of the multiplexed log and exec output.
const (
	// StreamStdin is the header value 0, which engines do not send for logs.
	StreamStdin Stream = 0
	// StreamStdout and StreamStderr are the container's two outputs.
	StreamStdout Stream = 1
	StreamStderr Stream = 2
)

// String returns "stdin", "stdout" or "stderr".
func (s Stream) String() string {
	switch s {
	case StreamStdin:
		return "stdin"
	case StreamStdout:
		return "stdout"
	case StreamStderr:
		return "stderr"
	}
	return "stream" + strconv.Itoa(int(s))
}

// MaxFrame is the largest frame of the multiplexed stream that Demux accepts. Engines write frames of at most 32 KiB; a
// header that claims more is a damaged stream, not a reason to allocate that much.
const MaxFrame = 1 << 20

// MaxLogLine is the longest line the Logs call hands over; a longer line is cut into pieces of this size.
const MaxLogLine = 64 << 10

// Frame is one piece of the multiplexed stream.
type Frame struct {
	// Stream is the output the data came from.
	Stream Stream
	// Data is the payload; it is valid only until fn returns.
	Data []byte
}

// Demux splits the multiplexed stream that the engines send for the logs and the exec output of a container that has no
// terminal: frames of an 8-byte header (the stream in the first byte, three zero bytes, the length as a big-endian 32-bit
// number) and the payload. fn is called for each frame; its error ends Demux and is returned. A stream that ends between
// two frames is a normal end; one that ends in the middle of a frame, an unknown stream, a header with non-zero reserved
// bytes and a frame longer than MaxFrame are errors with the tag ContainerAPIFrameInvalid. A failing reader is
// ContainerAPIStreamFailed.
func Demux(r io.Reader, fn func(Frame) error) error {
	var header [8]byte
	var buf []byte
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			if err == io.ErrUnexpectedEOF {
				return errors.New("ContainerAPIFrameInvalid", "the stream ends in the middle of a frame header", "reason", "truncated header")
			}
			return errors.NewErr("ContainerAPIStreamFailed", err)
		}
		if header[0] > byte(StreamStderr) {
			return errors.New("ContainerAPIFrameInvalid", "the frame header names the unknown stream "+strconv.Itoa(int(header[0])), "reason", "unknown stream", "stream", int(header[0]))
		}
		if header[1] != 0 || header[2] != 0 || header[3] != 0 {
			return errors.New("ContainerAPIFrameInvalid", "the frame header has non-zero reserved bytes", "reason", "reserved bytes")
		}
		size := binary.BigEndian.Uint32(header[4:])
		if size > MaxFrame {
			return errors.New("ContainerAPIFrameInvalid", "the frame is longer than the limit of "+strconv.Itoa(MaxFrame)+" bytes", "reason", "too long", "size", size, "limit", MaxFrame)
		}
		if int(size) > cap(buf) {
			buf = make([]byte, size)
		}
		buf = buf[:size]
		if _, err := io.ReadFull(r, buf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return errors.New("ContainerAPIFrameInvalid", "the stream ends in the middle of a frame", "reason", "truncated frame", "size", size)
			}
			return errors.NewErr("ContainerAPIStreamFailed", err)
		}
		if err := fn(Frame{Stream: Stream(header[0]), Data: buf}); err != nil {
			return err
		}
	}
}

// LogLine is one line of a container's output.
type LogLine struct {
	// Stream is stdout or stderr.
	Stream Stream
	// Time is the timestamp the engine put in front of the line, when LogsOptions.Timestamps asked for it.
	Time time.Time
	// Text is the line without its line end.
	Text string
}

// LineSplitter turns the frames of the multiplexed stream into lines: a frame may hold several lines or a part of one, and
// the two streams are kept apart. A line longer than Max is cut.
type LineSplitter struct {
	// Max is the longest line that is handed over whole; 0 is MaxLogLine.
	Max     int
	partial [3][]byte
}

// Write adds the data of a frame to its stream and calls emit for each line that is complete. The error of emit stops it
// and is returned.
func (l *LineSplitter) Write(stream Stream, data []byte, emit func(Stream, string) error) error {
	if int(stream) >= len(l.partial) {
		stream = StreamStdout
	}
	max := l.Max
	if max <= 0 {
		max = MaxLogLine
	}
	buf := append(l.partial[stream], data...)
	for {
		i := indexByte(buf, '\n')
		if i < 0 {
			break
		}
		line := buf[:i]
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		if err := emitCut(stream, line, max, emit); err != nil {
			l.partial[stream] = nil
			return err
		}
		buf = buf[i+1:]
	}
	for len(buf) > max {
		if err := emit(stream, string(buf[:max])); err != nil {
			l.partial[stream] = nil
			return err
		}
		buf = buf[max:]
	}
	// keep what is left, in a buffer of its own so that the caller's data can be reused
	l.partial[stream] = append(l.partial[stream][:0], buf...)
	return nil
}

// Flush emits the part of a line that is left on each stream (the output of a program that ends without a line end).
func (l *LineSplitter) Flush(emit func(Stream, string) error) error {
	for s := range l.partial {
		if len(l.partial[s]) == 0 {
			continue
		}
		text := string(l.partial[s])
		l.partial[s] = nil
		if err := emit(Stream(s), text); err != nil {
			return err
		}
	}
	return nil
}

// emitCut emits a line, in pieces of at most max bytes.
func emitCut(stream Stream, line []byte, max int, emit func(Stream, string) error) error {
	for len(line) > max {
		if err := emit(stream, string(line[:max])); err != nil {
			return err
		}
		line = line[max:]
	}
	return emit(stream, string(line))
}

// indexByte returns the index of the first c in b, or -1.
func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// LogsOptions are the options of Logs.
type LogsOptions struct {
	// Stdout and Stderr choose the outputs; neither set means both.
	Stdout, Stderr bool
	// Follow keeps the call open for new output until ctx is cancelled or the container stops.
	Follow bool
	// Timestamps asks the engine to put a time in front of each line, which is parsed into LogLine.Time.
	Timestamps bool
	// Tail is the number of lines from the end to start with; 0 is all.
	Tail int
	// Since starts at this time; the zero time is the beginning.
	Since time.Time
}

// Logs reads the logs of a container and calls fn for each line. The multiplexed stream is split into stdout and stderr
// lines; a container with a terminal has one stream, which is reported as stdout. Without Follow it returns when the logs
// are done; with it, when ctx is cancelled (then it returns nil), the container stops, or fn returns an error (which Logs
// returns). The part of a line still unfinished when the stream ends is emitted too, unless ctx was cancelled.
func (c *Client) Logs(ctx context.Context, id string, opts LogsOptions, fn func(LogLine) error) error {
	info, err := c.InspectContainer(ctx, id)
	if err != nil {
		return err
	}
	q := url.Values{}
	both := !opts.Stdout && !opts.Stderr
	q.Set("stdout", boolText(opts.Stdout || both))
	q.Set("stderr", boolText(opts.Stderr || both))
	if opts.Follow {
		q.Set("follow", "true")
	}
	if opts.Timestamps {
		q.Set("timestamps", "true")
	}
	if opts.Tail > 0 {
		q.Set("tail", strconv.Itoa(opts.Tail))
	}
	if !opts.Since.IsZero() {
		q.Set("since", strconv.FormatInt(opts.Since.Unix(), 10))
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/containers/" + url.PathEscape(id) + "/logs", query: q})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // a body that was read has nothing to report on closing

	emit := func(s Stream, text string) error {
		line := LogLine{Stream: s, Text: text}
		if opts.Timestamps {
			if ts, rest, found := strings.Cut(text, " "); found {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
					line.Time, line.Text = t, rest
				}
			}
		}
		return fn(line)
	}
	var split LineSplitter
	if info.Tty {
		buf := make([]byte, 32<<10)
		for err == nil {
			var n int
			n, err = resp.Body.Read(buf)
			if n > 0 {
				if werr := split.Write(StreamStdout, buf[:n], emit); werr != nil {
					return werr
				}
			}
		}
		if err == io.EOF {
			err = nil
		} else if ctx.Err() == nil {
			err = errors.NewErr("ContainerAPIStreamFailed", err)
		}
	} else {
		err = Demux(resp.Body, func(f Frame) error { return split.Write(f.Stream, f.Data, emit) })
	}
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return err
	}
	return split.Flush(emit)
}
