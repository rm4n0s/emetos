package dap

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// DefaultMaxMessageSize is the largest message body ReadFrame accepts when it is given no limit: 64 MiB. The largest
// bodies of an adapter (the variables of a huge array, a long stack) are a few MiB; the limit is there so that an adapter
// that sends garbage cannot make the editor allocate gigabytes.
const DefaultMaxMessageSize = 64 << 20

// maxHeaderLines is how many header lines a frame may have; the protocol defines one.
const maxHeaderLines = 32

// ReadFrame reads one message of the base protocol from r: header lines (each ending in "\r\n", or in a bare "\n" as VS Code
// accepts), an empty line, and a body of Content-Length bytes. It returns the body, which may be empty.
//
// The length of a header line is limited by the buffer of r (4096 bytes for bufio.NewReader), and the body by maxSize
// (DefaultMaxMessageSize when it is not positive); a message over the limit is refused before its body is read, and the
// stream is not usable afterwards.
//
// At the end of the input between two messages the error is io.EOF itself. Every other failure has a tag:
// DapProtocolInvalid (a header line that is too long, a line without a colon, too many lines, a Content-Length that is
// missing or not a number: the metadata "reason" says which), DapMessageTooLarge, DapFrameTruncated and DapFrameReadFailed.
func ReadFrame(r *bufio.Reader, maxSize int) ([]byte, error) {
	if maxSize <= 0 {
		maxSize = DefaultMaxMessageSize
	}
	length := -1
	lines := 0
	for {
		line, err := r.ReadSlice('\n')
		if err != nil {
			switch {
			case errors.Is(err, bufio.ErrBufferFull):
				return nil, errors.New("DapProtocolInvalid", "a header line is longer than the read buffer", "reason", "header too long", "buffer", r.Size())
			case errors.Is(err, io.EOF) && lines == 0 && len(line) == 0:
				return nil, io.EOF
			case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
				return nil, errors.New("DapFrameTruncated", "the input ended inside the header of a message")
			default:
				return nil, errors.NewErr("DapFrameReadFailed", err)
			}
		}
		lines++
		if lines > maxHeaderLines {
			return nil, errors.New("DapProtocolInvalid", "a message has too many header lines", "reason", "too many headers", "max", maxHeaderLines)
		}
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if text == "" {
			break
		}
		name, value, ok := strings.Cut(text, ":")
		if !ok {
			return nil, errors.New("DapProtocolInvalid", "a header line has no colon", "reason", "header without colon", "line", clip(text))
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, perr := strconv.ParseUint(strings.TrimSpace(value), 10, 31)
			if perr != nil {
				return nil, errors.New("DapProtocolInvalid", "Content-Length is not a number", "reason", "length invalid", "value", clip(value))
			}
			length = int(n)
		}
	}
	if length < 0 {
		return nil, errors.New("DapProtocolInvalid", "a message has no Content-Length header", "reason", "length missing")
	}
	if length > maxSize {
		return nil, errors.New("DapMessageTooLarge", "a message is larger than the limit", "length", length, "max", maxSize)
	}
	var body bytes.Buffer
	body.Grow(min(length, 64<<10))
	n, err := io.CopyN(&body, r, int64(length))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("DapFrameTruncated", "the input ended inside the body of a message", "length", length, "read", n)
		}
		return nil, errors.NewErr("DapFrameReadFailed", err).SetMetadata("length", length, "read", n)
	}
	return body.Bytes(), nil
}

// clip shortens a piece of text from the adapter for the metadata of an error.
func clip(s string) string {
	const limit = 80
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

// WriteFrame writes body to w as one message of the base protocol, in a single Write. It fails with the tag
// DapFrameWriteFailed.
func WriteFrame(w io.Writer, body []byte) error {
	frame := make([]byte, 0, len(body)+32)
	frame = append(frame, "Content-Length: "...)
	frame = strconv.AppendInt(frame, int64(len(body)), 10)
	frame = append(frame, "\r\n\r\n"...)
	frame = append(frame, body...)
	if _, err := w.Write(frame); err != nil {
		return errors.NewErr("DapFrameWriteFailed", err).SetMetadata("length", len(body))
	}
	return nil
}
