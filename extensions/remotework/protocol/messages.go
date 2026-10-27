package protocol

import (
	"github.com/rm4n0s/errors"
)

// The limits of the fields.
const (
	// TileSize is the side of a tile in pixels: the picture is cut into squares of 64x64, the ones at the right and bottom
	// edges smaller.
	TileSize = 64
	// BytesPerPixel is the size of a pixel of a tile before it is deflated: R, G, B, A, one byte each, not premultiplied.
	BytesPerPixel = 4
	// MaxTileData is the limit of the deflated data of one tile: a whole tile does not deflate to more than the pixels
	// themselves plus the few bytes of the format, so this holds with room to spare.
	MaxTileData = TileSize*TileSize*BytesPerPixel + 1024
	// MaxDimension is the largest width or height of a picture.
	MaxDimension = 7680
	// MaxPixels is the largest picture, in pixels (a 4K display): it bounds the memory of the pictures both sides keep.
	MaxPixels = 3840 * 2160
	// MaxToken is the longest session token.
	MaxToken = 64
	// MaxKeyText is the longest text of a key event, in bytes.
	MaxKeyText = 32
	// MaxClipboard is the longest clipboard text, in bytes.
	MaxClipboard = 1 << 20
	// MaxSource, MaxMessageText, MaxActions and MaxActionLabel limit a notification: the name of its source, its text, the
	// number of its actions and the label of an action, in bytes where they are texts.
	MaxSource      = 128
	MaxMessageText = 4096
	MaxActions     = 8
	MaxActionLabel = 128
	// MaxReasonText is the longest explanation of a disconnect.
	MaxReasonText = 256
)

// ValidSize tells whether a picture of this size is allowed: both sides positive, none above MaxDimension, and the
// area no more than MaxPixels.
func ValidSize(width, height int) bool {
	return width > 0 && height > 0 && width <= MaxDimension && height <= MaxDimension && width*height <= MaxPixels
}

// TilesAcross is the number of tiles in a row (or column) of a picture that is n pixels wide (or high).
func TilesAcross(n int) int { return (n + TileSize - 1) / TileSize }

// ---- Hello ----

// Hello opens a session: the client sends it first, with the token the host gave it over the SSH channel, and the host
// answers with its own (an empty token). Fields: the Magic (4 bytes), Version (2), the length of Token (1) and Token.
type Hello struct {
	// Version is the version of the protocol the sender speaks.
	Version uint16
	// Token is the session token (SPEC §10.18), at most MaxToken bytes; the host's answer carries none.
	Token []byte
}

// Type implements Message.
func (*Hello) Type() Type { return TypeHello }

func (m *Hello) encode(w *writer) error {
	if len(m.Token) > MaxToken {
		return errors.New("RemoteWorkEncodeTooLarge", "the token is longer than its limit", "length", len(m.Token), "limit", MaxToken)
	}
	w.b = append(w.b, Magic...)
	w.u16(m.Version)
	w.u8(uint8(len(m.Token)))
	w.b = append(w.b, m.Token...)
	return nil
}

func (m *Hello) decode(r *reader) error {
	magic, err := r.take(len(Magic))
	if err != nil {
		return err
	}
	if string(magic) != Magic {
		return errors.New("RemoteWorkBadMagic", "the stream does not start like Remote Work", "magic", string(magic))
	}
	if m.Version, err = r.u16(); err != nil {
		return err
	}
	n, err := r.u8()
	if err != nil {
		return err
	}
	if int(n) > MaxToken {
		return errors.New("RemoteWorkTooLarge", "the token is longer than its limit", "length", n, "limit", MaxToken)
	}
	token, err := r.take(int(n))
	if err != nil {
		return err
	}
	m.Token = append([]byte(nil), token...)
	return nil
}

// Check tells whether the peer speaks this version of the protocol. The error has the tag RemoteWorkVersion.
func (m *Hello) Check() error {
	if m.Version != Version {
		return errors.New("RemoteWorkVersion", "the peer speaks another version of the Remote Work protocol", "peer", m.Version, "ours", Version)
	}
	return nil
}

// ---- Tile ----

// tileHeaderSize is the size of the fields of Tile before its data.
const tileHeaderSize = 8 + 8 + 2 + 2 + 2 + 2 + 4

// Tile is one changed tile of a picture. It stands alone, so that it can travel as a datagram: it says which picture it
// is of, how large that is, where the tile is and how many tiles the picture has to change. Fields: Frame (8 bytes), Base
// (8), Width (2), Height (2), X (2), Y (2), Total (4) and Data, which fills the rest of the payload.
type Tile struct {
	// Frame is the number of the picture, counted from 1.
	Frame uint64
	// Base is the picture the tiles of Frame are the changes to: the last one the client confirmed, 0 for none (then every
	// tile of the picture is sent). It is below Frame.
	Base uint64
	// Width and Height are the size of the picture in pixels (ValidSize).
	Width, Height uint16
	// X and Y are the column and the row of the tile, in tiles.
	X, Y uint16
	// Total is the number of tiles that Frame sends, at least 1 and at most the number of tiles of the picture; the
	// client has the picture when it has that many.
	Total uint32
	// Data is the deflated pixels of the tile (package tiles), 1 to MaxTileData bytes.
	Data []byte
}

// Type implements Message.
func (*Tile) Type() Type { return TypeTile }

func (m *Tile) check() (ok bool, what string) {
	switch {
	case m.Frame == 0:
		return false, "the frame is 0"
	case m.Base >= m.Frame:
		return false, "the base is not below the frame"
	case !ValidSize(int(m.Width), int(m.Height)):
		return false, "the size of the picture is not allowed"
	case int(m.X) >= TilesAcross(int(m.Width)) || int(m.Y) >= TilesAcross(int(m.Height)):
		return false, "the tile is outside the picture"
	case m.Total == 0 || uint64(m.Total) > uint64(TilesAcross(int(m.Width)))*uint64(TilesAcross(int(m.Height))):
		return false, "the number of tiles of the frame is not possible"
	case len(m.Data) == 0:
		return false, "the tile has no data"
	}
	return true, ""
}

func (m *Tile) encode(w *writer) error {
	if len(m.Data) > MaxTileData {
		return errors.New("RemoteWorkEncodeTooLarge", "the data of the tile is larger than its limit", "length", len(m.Data), "limit", MaxTileData)
	}
	if ok, what := m.check(); !ok {
		return errors.New("RemoteWorkEncodeInvalid", what, "type", "Tile")
	}
	w.u64(m.Frame)
	w.u64(m.Base)
	w.u16(m.Width)
	w.u16(m.Height)
	w.u16(m.X)
	w.u16(m.Y)
	w.u32(m.Total)
	w.b = append(w.b, m.Data...)
	return nil
}

func (m *Tile) decode(r *reader) error {
	var err error
	if m.Frame, err = r.u64(); err != nil {
		return err
	}
	if m.Base, err = r.u64(); err != nil {
		return err
	}
	if m.Width, err = r.u16(); err != nil {
		return err
	}
	if m.Height, err = r.u16(); err != nil {
		return err
	}
	if m.X, err = r.u16(); err != nil {
		return err
	}
	if m.Y, err = r.u16(); err != nil {
		return err
	}
	if m.Total, err = r.u32(); err != nil {
		return err
	}
	data, err := r.take(len(r.b))
	if err != nil {
		return err
	}
	if len(data) > MaxTileData {
		return errors.New("RemoteWorkTooLarge", "the data of the tile is larger than its limit", "length", len(data), "limit", MaxTileData)
	}
	m.Data = append([]byte(nil), data...)
	if ok, what := m.check(); !ok {
		return errors.New("RemoteWorkBadValue", what, "type", "Tile")
	}
	return nil
}

// ---- FrameConfirm and Refresh ----

// FrameConfirm tells the host that the client has the whole picture Frame: the next pictures are sent as the changes to
// it. Field: Frame (8 bytes).
type FrameConfirm struct {
	// Frame is the number of the picture the client has, at least 1.
	Frame uint64
}

// Type implements Message.
func (*FrameConfirm) Type() Type { return TypeFrameConfirm }

func (m *FrameConfirm) encode(w *writer) error {
	if m.Frame == 0 {
		return errors.New("RemoteWorkEncodeInvalid", "the frame is 0", "type", "FrameConfirm")
	}
	w.u64(m.Frame)
	return nil
}

func (m *FrameConfirm) decode(r *reader) error {
	var err error
	if m.Frame, err = r.u64(); err != nil {
		return err
	}
	if m.Frame == 0 {
		return errors.New("RemoteWorkBadValue", "the frame is 0", "type", "FrameConfirm")
	}
	return nil
}

// Refresh asks the host to send the whole picture again (a client that lost its picture, or came back). It has no
// fields.
type Refresh struct{}

// Type implements Message.
func (*Refresh) Type() Type { return TypeRefresh }

func (*Refresh) encode(*writer) error { return nil }

func (*Refresh) decode(*reader) error { return nil }

// ---- Key ----

// KeyKind is what a key did.
type KeyKind uint8

// The kinds of a key event, the ones of Glutter's key events.
const (
	// KeyDown is a key that went down.
	KeyDown KeyKind = iota
	// KeyUp is a key that was released.
	KeyUp
	// KeyRepeat is a key that is held down and repeats.
	KeyRepeat

	keyKindEnd
)

// Key is a key event with the text it produced, so that the layout of the client's keyboard and its input method decide
// what is typed. Fields: Kind (1 byte), Physical (8), Logical (8), the length of Text (1) and Text.
type Key struct {
	// Kind is whether the key went down, up or repeats.
	Kind KeyKind
	// Physical is the USB HID usage of the key (Glutter's PhysicalKeyboardKey), which does not depend on the layout.
	Physical uint64
	// Logical is the ID of the key (Glutter's LogicalKeyboardKey), which does.
	Logical uint64
	// Text is what the key typed, at most MaxKeyText bytes; empty for a release or a key that types nothing.
	Text string
}

// Type implements Message.
func (*Key) Type() Type { return TypeKey }

func (m *Key) encode(w *writer) error {
	if err := w.enum("kinds of a key event", uint8(m.Kind), uint8(keyKindEnd)); err != nil {
		return err
	}
	w.u64(m.Physical)
	w.u64(m.Logical)
	return w.str8(m.Text, MaxKeyText)
}

func (m *Key) decode(r *reader) error {
	k, err := r.enum("kinds of a key event", uint8(keyKindEnd))
	if err != nil {
		return err
	}
	m.Kind = KeyKind(k)
	if m.Physical, err = r.u64(); err != nil {
		return err
	}
	if m.Logical, err = r.u64(); err != nil {
		return err
	}
	m.Text, err = r.str8(MaxKeyText)
	return err
}

// ---- Mouse and Scroll ----

// MouseKind is what the pointer did.
type MouseKind uint8

// The kinds of a pointer event.
const (
	// MouseMove is a pointer that moved, with or without buttons down.
	MouseMove MouseKind = iota
	// MouseDown is a button that was pressed.
	MouseDown
	// MouseUp is a button that was released.
	MouseUp

	mouseKindEnd
)

// The bits of Mouse.Buttons, the ones of Glutter's pointer buttons.
const (
	// ButtonPrimary is the left button.
	ButtonPrimary uint8 = 1 << iota
	// ButtonSecondary is the right button.
	ButtonSecondary
	// ButtonMiddle is the wheel button.
	ButtonMiddle

	allButtons = ButtonPrimary | ButtonSecondary | ButtonMiddle
)

// Mouse is a pointer event in the coordinates of the view, in pixels. Fields: Kind (1 byte), Buttons (1), X (4, a
// 32-bit float) and Y (4).
type Mouse struct {
	// Kind is whether the pointer moved or a button went down or up.
	Kind MouseKind
	// Buttons are the buttons that are down after the event, as bits of the Button constants.
	Buttons uint8
	// X and Y are the position, finite numbers.
	X, Y float32
}

// Type implements Message.
func (*Mouse) Type() Type { return TypeMouse }

func (m *Mouse) encode(w *writer) error {
	if err := w.enum("kinds of a pointer event", uint8(m.Kind), uint8(mouseKindEnd)); err != nil {
		return err
	}
	if m.Buttons&^allButtons != 0 {
		return errors.New("RemoteWorkEncodeInvalid", "the buttons have a bit the protocol does not have", "buttons", m.Buttons)
	}
	w.u8(m.Buttons)
	if err := w.f32(m.X); err != nil {
		return err
	}
	return w.f32(m.Y)
}

func (m *Mouse) decode(r *reader) error {
	k, err := r.enum("kinds of a pointer event", uint8(mouseKindEnd))
	if err != nil {
		return err
	}
	m.Kind = MouseKind(k)
	if m.Buttons, err = r.u8(); err != nil {
		return err
	}
	if m.Buttons&^allButtons != 0 {
		return errors.New("RemoteWorkBadValue", "the buttons have a bit the protocol does not have", "buttons", m.Buttons)
	}
	if m.X, err = r.f32(); err != nil {
		return err
	}
	m.Y, err = r.f32()
	return err
}

// Scroll is a scroll of the wheel or the trackpad. Fields: X, Y, DX and DY (4 bytes each, 32-bit floats).
type Scroll struct {
	// X and Y are the position of the pointer, in pixels.
	X, Y float32
	// DX and DY are the distance scrolled, in pixels, positive towards the right and the bottom.
	DX, DY float32
}

// Type implements Message.
func (*Scroll) Type() Type { return TypeScroll }

func (m *Scroll) encode(w *writer) error {
	for _, v := range [...]float32{m.X, m.Y, m.DX, m.DY} {
		if err := w.f32(v); err != nil {
			return err
		}
	}
	return nil
}

func (m *Scroll) decode(r *reader) error {
	for _, p := range [...]*float32{&m.X, &m.Y, &m.DX, &m.DY} {
		v, err := r.f32()
		if err != nil {
			return err
		}
		*p = v
	}
	return nil
}

// ---- Clipboard ----

// Clipboard carries the text of a copy or a paste, only when the user copies or pastes. Field: Text, the whole payload.
type Clipboard struct {
	// Text is the text, at most MaxClipboard bytes of UTF-8.
	Text string
}

// Type implements Message.
func (*Clipboard) Type() Type { return TypeClipboard }

func (m *Clipboard) encode(w *writer) error {
	if len(m.Text) > MaxClipboard {
		return errors.New("RemoteWorkEncodeTooLarge", "the clipboard text is larger than its limit", "length", len(m.Text), "limit", MaxClipboard)
	}
	return w.text(m.Text)
}

func (m *Clipboard) decode(r *reader) error {
	b, err := r.take(len(r.b))
	if err != nil {
		return err
	}
	m.Text, err = r.text(b)
	return err
}

// ---- Notification ----

// Severity is how serious a notification is.
type Severity uint8

// The severities of a notification.
const (
	// SeverityInfo is an information.
	SeverityInfo Severity = iota
	// SeverityWarning is a warning.
	SeverityWarning
	// SeverityError is an error.
	SeverityError

	severityEnd
)

// Notification is a notification of the workspace, shown on the client with its actions; an action that is chosen comes
// back as a NotificationAction and runs on the host. Fields: ID (8 bytes), Severity (1), Source (2-byte length, text),
// Message (2-byte length, text), the number of actions (1) and each label (2-byte length, text).
type Notification struct {
	// ID identifies the notification in NotificationAction.
	ID uint64
	// Severity is how serious it is.
	Severity Severity
	// Source is who raised it, at most MaxSource bytes.
	Source string
	// Message is the text, at most MaxMessageText bytes.
	Message string
	// Actions are the labels of the buttons, at most MaxActions of at most MaxActionLabel bytes.
	Actions []string
}

// Type implements Message.
func (*Notification) Type() Type { return TypeNotification }

func (m *Notification) encode(w *writer) error {
	if len(m.Actions) > MaxActions {
		return errors.New("RemoteWorkEncodeTooLarge", "the notification has more actions than the limit", "count", len(m.Actions), "limit", MaxActions)
	}
	w.u64(m.ID)
	if err := w.enum("severities", uint8(m.Severity), uint8(severityEnd)); err != nil {
		return err
	}
	if err := w.str16(m.Source, MaxSource); err != nil {
		return err
	}
	if err := w.str16(m.Message, MaxMessageText); err != nil {
		return err
	}
	w.u8(uint8(len(m.Actions)))
	for _, a := range m.Actions {
		if err := w.str16(a, MaxActionLabel); err != nil {
			return err
		}
	}
	return nil
}

func (m *Notification) decode(r *reader) error {
	var err error
	if m.ID, err = r.u64(); err != nil {
		return err
	}
	s, err := r.enum("severities", uint8(severityEnd))
	if err != nil {
		return err
	}
	m.Severity = Severity(s)
	if m.Source, err = r.str16(MaxSource); err != nil {
		return err
	}
	if m.Message, err = r.str16(MaxMessageText); err != nil {
		return err
	}
	n, err := r.u8()
	if err != nil {
		return err
	}
	if int(n) > MaxActions {
		return errors.New("RemoteWorkTooLarge", "the notification has more actions than the limit", "count", n, "limit", MaxActions)
	}
	for range n {
		a, err := r.str16(MaxActionLabel)
		if err != nil {
			return err
		}
		m.Actions = append(m.Actions, a)
	}
	return nil
}

// NotificationAction is the action of a notification that the user chose on the client. Fields: ID (8 bytes) and Action
// (1).
type NotificationAction struct {
	// ID is the ID of the Notification.
	ID uint64
	// Action is the index in the Actions of the notification, below MaxActions.
	Action uint8
}

// Type implements Message.
func (*NotificationAction) Type() Type { return TypeNotificationAction }

func (m *NotificationAction) encode(w *writer) error {
	w.u64(m.ID)
	return w.enum("actions", m.Action, MaxActions)
}

func (m *NotificationAction) decode(r *reader) error {
	var err error
	if m.ID, err = r.u64(); err != nil {
		return err
	}
	m.Action, err = r.enum("actions", MaxActions)
	return err
}

// ---- Resize ----

// Resize is the size of the client's view, in pixels; the host makes its view that large. Fields: Width (2 bytes) and
// Height (2).
type Resize struct {
	// Width and Height are the size, allowed by ValidSize.
	Width, Height uint16
}

// Type implements Message.
func (*Resize) Type() Type { return TypeResize }

func (m *Resize) encode(w *writer) error {
	if !ValidSize(int(m.Width), int(m.Height)) {
		return errors.New("RemoteWorkEncodeInvalid", "the size of the view is not allowed", "width", m.Width, "height", m.Height)
	}
	w.u16(m.Width)
	w.u16(m.Height)
	return nil
}

func (m *Resize) decode(r *reader) error {
	var err error
	if m.Width, err = r.u16(); err != nil {
		return err
	}
	if m.Height, err = r.u16(); err != nil {
		return err
	}
	if !ValidSize(int(m.Width), int(m.Height)) {
		return errors.New("RemoteWorkBadValue", "the size of the view is not allowed", "width", m.Width, "height", m.Height)
	}
	return nil
}

// ---- Ping ----

// Ping measures the round trip and keeps a quiet session from idling out: the peer answers with the same Seq and Reply
// set. Fields: Seq (8 bytes) and Reply (1, 0 or 1).
type Ping struct {
	// Seq numbers the ping; the answer repeats it.
	Seq uint64
	// Reply is true in the answer.
	Reply bool
}

// Type implements Message.
func (*Ping) Type() Type { return TypePing }

func (m *Ping) encode(w *writer) error {
	w.u64(m.Seq)
	w.flag(m.Reply)
	return nil
}

func (m *Ping) decode(r *reader) error {
	var err error
	if m.Seq, err = r.u64(); err != nil {
		return err
	}
	m.Reply, err = r.flag()
	return err
}

// ---- Disconnect ----

// Reason is why a session ends.
type Reason uint8

// The reasons of a disconnect.
const (
	// ReasonClosed: the sender ended the session on purpose (the user closed the tab).
	ReasonClosed Reason = iota
	// ReasonHost: the user of the host pressed Disconnect.
	ReasonHost
	// ReasonIdle: the idle timeout of the host passed.
	ReasonIdle
	// ReasonOff: Remote Work was turned off for the workspace.
	ReasonOff
	// ReasonProtocol: the peer sent something the protocol does not allow.
	ReasonProtocol
	// ReasonVersion: the peer speaks another version of the protocol.
	ReasonVersion
	// ReasonReplaced: another client took the session.
	ReasonReplaced
	// ReasonWorkspaceClosed: the workspace was closed on the host.
	ReasonWorkspaceClosed

	reasonEnd
)

var reasonNames = [...]string{
	ReasonClosed: "closed", ReasonHost: "disconnected by the host", ReasonIdle: "idle timeout",
	ReasonOff: "Remote Work was turned off", ReasonProtocol: "protocol error", ReasonVersion: "version mismatch",
	ReasonReplaced: "replaced by another connection", ReasonWorkspaceClosed: "the workspace was closed",
}

// String is the reason in words, for the tab to show.
func (r Reason) String() string {
	if r < reasonEnd {
		return reasonNames[r]
	}
	return "unknown reason"
}

// Disconnect ends the session. Fields: Reason (1 byte) and Text (2-byte length, text).
type Disconnect struct {
	// Reason is why the session ends.
	Reason Reason
	// Text explains it, at most MaxReasonText bytes; may be empty.
	Text string
}

// Type implements Message.
func (*Disconnect) Type() Type { return TypeDisconnect }

func (m *Disconnect) encode(w *writer) error {
	if err := w.enum("reasons", uint8(m.Reason), uint8(reasonEnd)); err != nil {
		return err
	}
	return w.str16(m.Text, MaxReasonText)
}

func (m *Disconnect) decode(r *reader) error {
	v, err := r.enum("reasons", uint8(reasonEnd))
	if err != nil {
		return err
	}
	m.Reason = Reason(v)
	m.Text, err = r.str16(MaxReasonText)
	return err
}
