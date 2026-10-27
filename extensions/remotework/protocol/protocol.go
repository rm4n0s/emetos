package protocol

import (
	"encoding/binary"
	"io"
	"strconv"

	"github.com/rm4n0s/errors"
)

// Version is the version of the protocol. Hello carries it and the two sides must agree: a peer with another version is
// refused (Hello.Check), because the format has no way to skip what it does not know.
const Version = 1

// Magic starts every Hello, so that a stream that is not Remote Work is refused at its first message.
const Magic = "EMRW"

// HeaderSize is the size of the type and the length that start every message.
const HeaderSize = 5

// Type is the type byte of a message.
type Type uint8

// The types of the messages. A byte that is not one of them is refused (RemoteWorkUnknownType): the versions of the two
// sides are equal, so there is nothing to skip.
const (
	// TypeHello opens a session, in both directions.
	TypeHello Type = iota + 1
	// TypeTile carries one changed tile of a picture.
	TypeTile
	// TypeFrameConfirm says which picture the client has completely.
	TypeFrameConfirm
	// TypeRefresh asks for the whole picture again.
	TypeRefresh
	// TypeKey is a key event with the text it produced.
	TypeKey
	// TypeMouse is a pointer event.
	TypeMouse
	// TypeScroll is a scroll of the wheel or the trackpad.
	TypeScroll
	// TypeClipboard carries the text of a copy or a paste.
	TypeClipboard
	// TypeNotification is a notification of the workspace with its actions.
	TypeNotification
	// TypeNotificationAction is the action of a notification the user chose.
	TypeNotificationAction
	// TypeResize is the size of the client's view.
	TypeResize
	// TypePing measures the round trip and keeps the session from idling.
	TypePing
	// TypeDisconnect ends the session with a reason.
	TypeDisconnect
	// TypeMissing lists the tiles of the newest picture that a client lost on the datagrams (step 15.3).
	TypeMissing

	typeEnd // not a type
)

var typeNames = [...]string{
	TypeHello: "Hello", TypeTile: "Tile", TypeFrameConfirm: "FrameConfirm", TypeRefresh: "Refresh", TypeKey: "Key",
	TypeMouse: "Mouse", TypeScroll: "Scroll", TypeClipboard: "Clipboard", TypeNotification: "Notification",
	TypeNotificationAction: "NotificationAction", TypeResize: "Resize", TypePing: "Ping", TypeDisconnect: "Disconnect", TypeMissing: "Missing",
}

// String is the name of the type.
func (t Type) String() string {
	if t > 0 && t < typeEnd {
		return typeNames[t]
	}
	return "Type(" + strconv.Itoa(int(t)) + ")"
}

// The limits of the payload of each type, in bytes: the length of a message over its limit is refused before the
// payload is read. A limit is the largest message the format can make (MaxTileData, MaxClipboard, ...), rounded up.
const (
	maxHello              = 128
	maxFrameConfirm       = 16
	maxRefresh            = 0
	maxKey                = 64
	maxMouse              = 16
	maxScroll             = 32
	maxNotification       = 8 << 10
	maxNotificationAction = 16
	maxResize             = 8
	maxPing               = 16
	maxDisconnect         = 512
	maxMissing            = missingHeaderSize + (MaxFrameTiles+7)/8
)

// MaxPayload is the limit of the payload of a type, and false for a byte that is no type.
func MaxPayload(t Type) (int, bool) {
	switch t {
	case TypeHello:
		return maxHello, true
	case TypeTile:
		return tileHeaderSize + MaxTileData, true
	case TypeFrameConfirm:
		return maxFrameConfirm, true
	case TypeRefresh:
		return maxRefresh, true
	case TypeKey:
		return maxKey, true
	case TypeMouse:
		return maxMouse, true
	case TypeScroll:
		return maxScroll, true
	case TypeClipboard:
		return MaxClipboard, true
	case TypeNotification:
		return maxNotification, true
	case TypeNotificationAction:
		return maxNotificationAction, true
	case TypeResize:
		return maxResize, true
	case TypePing:
		return maxPing, true
	case TypeDisconnect:
		return maxDisconnect, true
	case TypeMissing:
		return maxMissing, true
	}
	return 0, false
}

// MaxMessage is the size of the largest message, header included: what a buffer for any message needs.
const MaxMessage = HeaderSize + MaxClipboard

// Message is one message of the protocol. The types are the ones of this package, so a message can be made only here.
type Message interface {
	// Type is the type byte of the message.
	Type() Type
	// encode appends the fields to the writer; it fails for a value the format does not allow.
	encode(w *writer) error
	// decode reads the fields from the payload and checks them.
	decode(r *reader) error
}

// encodeMessage appends the fields of the message. It calls the methods of the concrete type, not of the interface, so
// that the writer stays on the stack and encoding into a large enough buffer allocates nothing.
func encodeMessage(w *writer, m Message) error {
	switch m := m.(type) {
	case *Hello:
		return m.encode(w)
	case *Tile:
		return m.encode(w)
	case *FrameConfirm:
		return m.encode(w)
	case *Refresh:
		return m.encode(w)
	case *Key:
		return m.encode(w)
	case *Mouse:
		return m.encode(w)
	case *Scroll:
		return m.encode(w)
	case *Clipboard:
		return m.encode(w)
	case *Notification:
		return m.encode(w)
	case *NotificationAction:
		return m.encode(w)
	case *Resize:
		return m.encode(w)
	case *Ping:
		return m.encode(w)
	case *Disconnect:
		return m.encode(w)
	case *Missing:
		return m.encode(w)
	}
	return errors.New("RemoteWorkNilMessage", "there is no message to encode")
}

// decodeMessage makes the message of a type and reads its fields from the reader (see encodeMessage for the switch).
func decodeMessage(t Type, r *reader) (Message, error) {
	switch t {
	case TypeHello:
		m := &Hello{}
		return m, m.decode(r)
	case TypeTile:
		m := &Tile{}
		return m, m.decode(r)
	case TypeFrameConfirm:
		m := &FrameConfirm{}
		return m, m.decode(r)
	case TypeRefresh:
		m := &Refresh{}
		return m, m.decode(r)
	case TypeKey:
		m := &Key{}
		return m, m.decode(r)
	case TypeMouse:
		m := &Mouse{}
		return m, m.decode(r)
	case TypeScroll:
		m := &Scroll{}
		return m, m.decode(r)
	case TypeClipboard:
		m := &Clipboard{}
		return m, m.decode(r)
	case TypeNotification:
		m := &Notification{}
		return m, m.decode(r)
	case TypeNotificationAction:
		m := &NotificationAction{}
		return m, m.decode(r)
	case TypeResize:
		m := &Resize{}
		return m, m.decode(r)
	case TypePing:
		m := &Ping{}
		return m, m.decode(r)
	case TypeDisconnect:
		m := &Disconnect{}
		return m, m.decode(r)
	case TypeMissing:
		m := &Missing{}
		return m, m.decode(r)
	}
	return nil, errors.New("RemoteWorkUnknownType", "the message has a type the protocol does not have", "type", int(t))
}

// Append encodes the message, header included, after dst and returns the extended slice. On an error dst is returned
// as it was. Nothing is allocated beyond what the message needs (the size of its payload is at most MaxPayload).
//
// The error has the tags RemoteWorkEncodeInvalid (a field the format does not allow), RemoteWorkEncodeTooLarge (a
// field over its limit) and RemoteWorkNilMessage.
func Append(dst []byte, m Message) ([]byte, error) {
	if m == nil {
		return dst, errors.New("RemoteWorkNilMessage", "there is no message to encode")
	}
	start := len(dst)
	w := writer{b: append(dst, byte(m.Type()), 0, 0, 0, 0)}
	if err := encodeMessage(&w, m); err != nil {
		return dst, err
	}
	n := len(w.b) - start - HeaderSize
	max, _ := MaxPayload(m.Type())
	if n > max {
		return dst, errors.New("RemoteWorkEncodeTooLarge", "the message is larger than its limit", "type", m.Type().String(), "size", n, "limit", max)
	}
	binary.BigEndian.PutUint32(w.b[start+1:start+HeaderSize], uint32(n))
	return w.b, nil
}

// Decode decodes one message that fills b exactly, a datagram for instance. The length in the header is checked against
// the limit of the type before the payload is looked at, and the fields of the message are copied out of b.
//
// The error has the tags RemoteWorkTruncated, RemoteWorkUnknownType, RemoteWorkTooLarge, RemoteWorkTrailing,
// RemoteWorkBadValue and RemoteWorkBadMagic.
func Decode(b []byte) (Message, error) {
	if len(b) < HeaderSize {
		return nil, errors.New("RemoteWorkTruncated", "the message ends inside its header", "size", len(b))
	}
	t, n, err := parseHeader(b[:HeaderSize])
	if err != nil {
		return nil, err
	}
	payload := b[HeaderSize:]
	switch {
	case len(payload) < n:
		return nil, errors.New("RemoteWorkTruncated", "the message ends before its payload does", "type", t.String(), "length", n, "have", len(payload))
	case len(payload) > n:
		return nil, errors.New("RemoteWorkTrailing", "there are bytes after the message", "type", t.String(), "length", n, "have", len(payload))
	}
	return decodePayload(t, payload)
}

// parseHeader reads the type and the length of a header and checks them: the type is known and the length is within its
// limit. The length is returned as an int that is at most MaxPayload(t).
func parseHeader(h []byte) (Type, int, error) {
	t := Type(h[0])
	max, ok := MaxPayload(t)
	if !ok {
		return 0, 0, errors.New("RemoteWorkUnknownType", "the message has a type the protocol does not have", "type", int(h[0]))
	}
	n := binary.BigEndian.Uint32(h[1:HeaderSize])
	if uint64(n) > uint64(max) {
		return 0, 0, errors.New("RemoteWorkTooLarge", "the message is larger than the limit of its type", "type", t.String(), "length", n, "limit", max)
	}
	return t, int(n), nil
}

// decodePayload decodes the payload of a type; its length was checked already.
func decodePayload(t Type, payload []byte) (Message, error) {
	r := reader{b: payload}
	m, err := decodeMessage(t, &r)
	if err != nil {
		return nil, err
	}
	if len(r.b) != 0 {
		return nil, errors.New("RemoteWorkTrailing", "the payload has bytes the message does not use", "type", t.String(), "left", len(r.b))
	}
	return m, nil
}

// Reader decodes the messages of a stream, one after the other. It holds one buffer, as large as the largest payload it
// has seen, so it allocates nothing more than the limit of the type of the message it reads. Its methods are not safe
// for concurrent use.
type Reader struct {
	r   io.Reader
	hdr [HeaderSize]byte
	buf []byte
}

// NewReader makes a Reader of a stream.
func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

// retainBuffer is the largest buffer a Reader keeps between messages: a clipboard that was larger does not stay in
// memory.
const retainBuffer = 64 << 10

// Next reads the next message. A stream that ends between two messages gives an error whose cause is io.EOF; one that
// ends inside a message gives io.ErrUnexpectedEOF. After any error but a RemoteWorkReadFailed the stream is out of step
// and must be closed: the format has no way to find the next message.
//
// The error has the tags of Decode and RemoteWorkReadFailed.
func (d *Reader) Next() (Message, error) {
	if _, err := io.ReadFull(d.r, d.hdr[:]); err != nil {
		return nil, errors.NewErr("RemoteWorkReadFailed", err)
	}
	t, n, err := parseHeader(d.hdr[:])
	if err != nil {
		return nil, err
	}
	if cap(d.buf) < n {
		d.buf = make([]byte, n)
	}
	payload := d.buf[:n]
	if _, err := io.ReadFull(d.r, payload); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, errors.NewErr("RemoteWorkReadFailed", err).SetMetadata("type", t.String(), "length", n)
	}
	m, err := decodePayload(t, payload)
	if cap(d.buf) > retainBuffer {
		d.buf = nil
	}
	return m, err
}

// Writer encodes messages onto a stream, each with one call to Write of the stream (so a datagram is one message). It
// keeps one buffer. Its methods are not safe for concurrent use.
type Writer struct {
	w   io.Writer
	buf []byte
}

// NewWriter makes a Writer onto a stream.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Write encodes the message and writes it. The error has the tags of Append and RemoteWorkWriteFailed.
func (w *Writer) Write(m Message) error {
	b, err := Append(w.buf[:0], m)
	if err != nil {
		return err
	}
	if cap(b) <= retainBuffer {
		w.buf = b
	}
	if _, err := w.w.Write(b); err != nil {
		return errors.NewErr("RemoteWorkWriteFailed", err).SetMetadata("type", m.Type().String())
	}
	return nil
}
