package claude

import (
	"bufio"
	"io"
)

// DefaultMaxLineBytes is the longest line of the program's output that is read whole: 16 MiB. A tool result carries whole
// files, so the limit is far above the 1 MiB of the Python SDK; it bounds the memory a broken program can make the editor
// use. A longer line is dropped, with the tag LineTooLong in a ProtocolErrorEvent, and the stream goes on with the next one.
const DefaultMaxLineBytes = 16 << 20

// lineReader reads the lines of a stream with a bound on their length.
type lineReader struct {
	r   *bufio.Reader
	max int
	buf []byte
}

func newLineReader(r io.Reader, maxBytes int) *lineReader {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxLineBytes
	}
	return &lineReader{r: bufio.NewReaderSize(r, 64*1024), max: maxBytes}
}

// next returns the next line without its newline. The slice is valid until the next call. When the line is longer than
// the limit, tooLong is true, line holds only its first bytes (for the log) and the rest of the line is discarded. A last
// line with no newline is returned like the others; at the end of the stream err is io.EOF and line is empty.
func (l *lineReader) next() (line []byte, tooLong bool, err error) {
	l.buf = l.buf[:0]
	for {
		frag, rerr := l.r.ReadSlice('\n')
		content := len(l.buf) + len(frag)
		if n := len(frag); n > 0 && frag[n-1] == '\n' {
			content-- // the newline is not part of the line
		}
		if content > l.max && !tooLong {
			tooLong = true
			room := l.max - len(l.buf)
			if room > 0 {
				l.buf = append(l.buf, frag[:min(room, len(frag))]...)
			}
		} else if !tooLong {
			l.buf = append(l.buf, frag...)
		}
		switch rerr {
		case nil:
			return trimNewline(l.buf), tooLong, nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if len(frag) == 0 && len(l.buf) == 0 && !tooLong {
				return nil, false, io.EOF
			}
			return trimNewline(l.buf), tooLong, nil
		default:
			return trimNewline(l.buf), tooLong, rerr
		}
	}
}

func trimNewline(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	if n := len(b); n > 0 && b[n-1] == '\r' {
		b = b[:n-1]
	}
	return b
}
