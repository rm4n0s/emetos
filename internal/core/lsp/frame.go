package lsp

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// DefaultMaxMessageSize is the largest message body ReadFrame accepts when it is given no limit: 64 MiB.
// The largest results of a server (semantic tokens or the symbols of a huge file) are a few MiB; the limit
// is there so that a server that sends garbage cannot make the editor allocate gigabytes.
const DefaultMaxMessageSize = 64 << 20

// maxHeaderLines is how many header lines a frame may have; the protocol defines two.
const maxHeaderLines = 32

// ReadFrame reads one message of the base protocol from r: header lines ending in "\r\n", an empty line,
// and a body of Content-Length bytes. It returns the body.
//
// The length of a header line is limited by the buffer of r (4096 bytes for bufio.NewReader), and the
// body by maxSize (DefaultMaxMessageSize when it is not positive); a message over the limit is refused
// before its body is read, and the stream is not usable afterwards.
//
// At the end of the input between two messages the error is io.EOF itself. Every other failure has a
// tag: FrameHeaderTooLong, FrameHeadersTooMany, FrameHeaderInvalid, FrameLengthMissing, FrameLengthInvalid,
// FrameCharsetUnsupported, FrameTooLarge, FrameTruncated and FrameReadFailed.
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
				return nil, errors.New("FrameHeaderTooLong", "a header line is longer than the read buffer", "buffer", r.Size())
			case errors.Is(err, io.EOF) && lines == 0 && len(line) == 0:
				return nil, io.EOF
			case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
				return nil, errors.New("FrameTruncated", "the input ended inside the header of a message")
			default:
				return nil, errors.NewErr("FrameReadFailed", err)
			}
		}
		lines++
		if lines > maxHeaderLines {
			return nil, errors.New("FrameHeadersTooMany", "a message has too many header lines", "max", maxHeaderLines)
		}
		text, ok := strings.CutSuffix(string(line), "\r\n")
		if !ok {
			return nil, errors.New("FrameHeaderInvalid", "a header line does not end in CRLF", "line", string(line))
		}
		if text == "" {
			break
		}
		name, value, ok := strings.Cut(text, ":")
		if !ok {
			return nil, errors.New("FrameHeaderInvalid", "a header line has no colon", "line", text)
		}
		value = strings.TrimSpace(value)
		switch {
		case strings.EqualFold(name, "Content-Length"):
			n, perr := strconv.ParseUint(value, 10, 31)
			if perr != nil {
				return nil, errors.New("FrameLengthInvalid", "Content-Length is not a number", "value", value)
			}
			length = int(n)
		case strings.EqualFold(name, "Content-Type"):
			if !charsetIsUTF8(value) {
				return nil, errors.New("FrameCharsetUnsupported", "the Content-Type names a charset other than utf-8", "value", value)
			}
		}
	}
	if length < 0 {
		return nil, errors.New("FrameLengthMissing", "a message has no Content-Length header")
	}
	if length > maxSize {
		return nil, errors.New("FrameTooLarge", "a message is larger than the limit", "length", length, "max", maxSize)
	}
	var body bytes.Buffer
	body.Grow(min(length, 64<<10))
	n, err := io.CopyN(&body, r, int64(length))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("FrameTruncated", "the input ended inside the body of a message", "length", length, "read", n)
		}
		return nil, errors.NewErr("FrameReadFailed", err).SetMetadata("length", length, "read", n)
	}
	return body.Bytes(), nil
}

// charsetIsUTF8 reports whether the value of a Content-Type header allows UTF-8: it has no charset, or
// "utf-8" or "utf8" (the protocol's old spelling).
func charsetIsUTF8(contentType string) bool {
	for part := range strings.SplitSeq(contentType, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "charset") {
			continue
		}
		switch strings.ToLower(strings.Trim(strings.TrimSpace(value), `"`)) {
		case "utf-8", "utf8":
		default:
			return false
		}
	}
	return true
}

// appendFrame appends the header of a message with body to dst, and the body.
func appendFrame(dst, body []byte) []byte {
	dst = append(dst, "Content-Length: "...)
	dst = strconv.AppendInt(dst, int64(len(body)), 10)
	dst = append(dst, "\r\n\r\n"...)
	return append(dst, body...)
}

// WriteFrame writes body to w as one message of the base protocol, in a single Write. It fails with the
// tag FrameWriteFailed.
func WriteFrame(w io.Writer, body []byte) error {
	frame := appendFrame(make([]byte, 0, len(body)+32), body)
	if _, err := w.Write(frame); err != nil {
		return errors.NewErr("FrameWriteFailed", err).SetMetadata("length", len(body))
	}
	return nil
}
