package protocol

import (
	"encoding/binary"
	"math"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// reader reads the fields of a payload. Every method checks what it reads: it cannot read past the end, and a string or
// a number the format does not allow is refused. The reader is the only place the decoders fail in, so the routes of the
// errors are short.
type reader struct{ b []byte }

// take returns the next n bytes of the payload, which are not copied.
func (r *reader) take(n int) ([]byte, error) {
	if n > len(r.b) {
		return nil, errors.New("RemoteWorkTruncated", "the payload ends inside a field", "need", n, "have", len(r.b))
	}
	out := r.b[:n:n]
	r.b = r.b[n:]
	return out, nil
}

func (r *reader) u8() (uint8, error) {
	b, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *reader) u16() (uint16, error) {
	b, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (r *reader) u32() (uint32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (r *reader) u64() (uint64, error) {
	b, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b), nil
}

// f32 reads a number that is finite.
func (r *reader) f32() (float32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}
	v := math.Float32frombits(binary.BigEndian.Uint32(b))
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return 0, errors.New("RemoteWorkBadValue", "a number is not finite", "bits", binary.BigEndian.Uint32(b))
	}
	return v, nil
}

// flag reads a byte that is 0 or 1.
func (r *reader) flag() (bool, error) {
	v, err := r.u8()
	if err != nil {
		return false, err
	}
	if v > 1 {
		return false, errors.New("RemoteWorkBadValue", "a flag is neither 0 nor 1", "value", v)
	}
	return v == 1, nil
}

// enum reads a byte that is less than end.
func (r *reader) enum(name string, end uint8) (uint8, error) {
	v, err := r.u8()
	if err != nil {
		return 0, err
	}
	if v >= end {
		return 0, errors.New("RemoteWorkBadValue", "a value is not one of the "+name, "value", v)
	}
	return v, nil
}

// text turns bytes into a string that is UTF-8; the bytes are copied.
func (r *reader) text(b []byte) (string, error) {
	if !utf8.Valid(b) {
		return "", errors.New("RemoteWorkBadValue", "a text is not valid UTF-8", "size", len(b))
	}
	return string(b), nil
}

// str8 reads a text with a length of one byte, of at most max bytes.
func (r *reader) str8(max int) (string, error) {
	n, err := r.u8()
	if err != nil {
		return "", err
	}
	if int(n) > max {
		return "", errors.New("RemoteWorkTooLarge", "a text is longer than its limit", "length", n, "limit", max)
	}
	b, err := r.take(int(n))
	if err != nil {
		return "", err
	}
	return r.text(b)
}

// str16 reads a text with a length of two bytes, of at most max bytes.
func (r *reader) str16(max int) (string, error) {
	n, err := r.u16()
	if err != nil {
		return "", err
	}
	if int(n) > max {
		return "", errors.New("RemoteWorkTooLarge", "a text is longer than its limit", "length", n, "limit", max)
	}
	b, err := r.take(int(n))
	if err != nil {
		return "", err
	}
	return r.text(b)
}

// writer appends the fields of a payload. It checks what it writes as the reader does, so a message that cannot be
// decoded cannot be encoded.
type writer struct{ b []byte }

func (w *writer) u8(v uint8)   { w.b = append(w.b, v) }
func (w *writer) u16(v uint16) { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *writer) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *writer) u64(v uint64) { w.b = binary.BigEndian.AppendUint64(w.b, v) }

func (w *writer) f32(v float32) error {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return errors.New("RemoteWorkEncodeInvalid", "a number is not finite")
	}
	w.b = binary.BigEndian.AppendUint32(w.b, math.Float32bits(v))
	return nil
}

func (w *writer) flag(v bool) {
	if v {
		w.b = append(w.b, 1)
		return
	}
	w.b = append(w.b, 0)
}

func (w *writer) enum(name string, v, end uint8) error {
	if v >= end {
		return errors.New("RemoteWorkEncodeInvalid", "a value is not one of the "+name, "value", v)
	}
	w.b = append(w.b, v)
	return nil
}

// str8 appends a text with a length of one byte.
func (w *writer) str8(s string, max int) error {
	if len(s) > max {
		return errors.New("RemoteWorkEncodeTooLarge", "a text is longer than its limit", "length", len(s), "limit", max)
	}
	if !utf8.ValidString(s) {
		return errors.New("RemoteWorkEncodeInvalid", "a text is not valid UTF-8", "size", len(s))
	}
	w.b = append(w.b, byte(len(s)))
	w.b = append(w.b, s...)
	return nil
}

// str16 appends a text with a length of two bytes.
func (w *writer) str16(s string, max int) error {
	if len(s) > max {
		return errors.New("RemoteWorkEncodeTooLarge", "a text is longer than its limit", "length", len(s), "limit", max)
	}
	if !utf8.ValidString(s) {
		return errors.New("RemoteWorkEncodeInvalid", "a text is not valid UTF-8", "size", len(s))
	}
	w.b = binary.BigEndian.AppendUint16(w.b, uint16(len(s)))
	w.b = append(w.b, s...)
	return nil
}

// text appends a text that fills the rest of the payload.
func (w *writer) text(s string) error {
	if !utf8.ValidString(s) {
		return errors.New("RemoteWorkEncodeInvalid", "a text is not valid UTF-8", "size", len(s))
	}
	w.b = append(w.b, s...)
	return nil
}
