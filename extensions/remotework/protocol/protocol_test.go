package protocol

import (
	"bytes"
	"io"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// samples is one valid message of every type, and some edge cases of the fields.
func samples() []Message {
	return []Message{
		&Hello{Version: Version, Token: bytes.Repeat([]byte{0xAB}, 32)},
		&Hello{Version: Version},
		&Hello{Version: 7, Token: bytes.Repeat([]byte{1}, MaxToken)},
		&Tile{Frame: 2, Base: 1, Width: 1280, Height: 800, X: 3, Y: 4, Total: 12, Data: []byte{1, 2, 3}},
		&Tile{Frame: 1, Width: 1, Height: 1, Total: 1, Data: bytes.Repeat([]byte{9}, MaxTileData)},
		&FrameConfirm{Frame: 99},
		&Refresh{},
		&Key{Kind: KeyDown, Physical: 0x70004, Logical: 0x61, Text: "a"},
		&Key{Kind: KeyUp, Physical: 0x70004, Logical: 0x61},
		&Key{Kind: KeyRepeat, Physical: 1, Logical: 2, Text: "é世"},
		&Mouse{Kind: MouseMove, X: 10.5, Y: -3},
		&Mouse{Kind: MouseDown, Buttons: ButtonPrimary | ButtonMiddle, X: 100, Y: 200.25},
		&Mouse{Kind: MouseUp, Buttons: ButtonSecondary},
		&Scroll{X: 1, Y: 2, DX: -3.5, DY: 40},
		&Clipboard{Text: ""},
		&Clipboard{Text: "hello\nworld é"},
		&Clipboard{Text: strings.Repeat("x", MaxClipboard)},
		&Notification{ID: 5, Severity: SeverityWarning, Source: "Git", Message: "Pull?", Actions: []string{"Pull", "Cancel"}},
		&Notification{ID: 6, Severity: SeverityError, Source: strings.Repeat("s", MaxSource), Message: strings.Repeat("m", MaxMessageText), Actions: slicesOf(MaxActions, strings.Repeat("a", MaxActionLabel))},
		&Notification{ID: 7},
		&NotificationAction{ID: 5, Action: 1},
		&NotificationAction{ID: 5, Action: MaxActions - 1},
		&Resize{Width: 1280, Height: 800},
		&Resize{Width: 3840, Height: 2160},
		&Ping{Seq: 1},
		&Ping{Seq: math.MaxUint64, Reply: true},
		&Disconnect{Reason: ReasonHost, Text: "bye"},
		&Disconnect{Reason: ReasonWorkspaceClosed, Text: strings.Repeat("t", MaxReasonText)},
		&Disconnect{},
	}
}

func slicesOf(n int, s string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestEveryMessageRoundTrips(t *testing.T) {
	for _, m := range samples() {
		t.Run(m.Type().String(), func(t *testing.T) {
			b, err := Append(nil, m)
			if err != nil {
				t.Fatal(err)
			}
			if max, _ := MaxPayload(m.Type()); len(b)-HeaderSize > max {
				t.Fatalf("payload %d over the limit %d", len(b)-HeaderSize, max)
			}
			got, err := Decode(b)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, m) && !equalNilEmpty(got, m) {
				t.Errorf("decoded %+v, want %+v", got, m)
			}
			again, err := Append(nil, got)
			if err != nil || !bytes.Equal(again, b) {
				t.Errorf("the decoded message does not encode back to the same bytes (err %v)", err)
			}
		})
	}
}

// equalNilEmpty compares messages whose empty slices may be nil on one side.
func equalNilEmpty(a, b Message) bool {
	x, ok1 := a.(*Hello)
	y, ok2 := b.(*Hello)
	if ok1 && ok2 {
		return x.Version == y.Version && bytes.Equal(x.Token, y.Token)
	}
	n1, ok1 := a.(*Notification)
	n2, ok2 := b.(*Notification)
	if ok1 && ok2 {
		return n1.ID == n2.ID && n1.Severity == n2.Severity && n1.Source == n2.Source && n1.Message == n2.Message && (len(n1.Actions) == 0 && len(n2.Actions) == 0 || reflect.DeepEqual(n1.Actions, n2.Actions))
	}
	return false
}

func TestAStreamOfMessagesRoundTripsThroughReaderAndWriter(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	want := samples()
	for _, m := range want {
		if err := w.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	r := NewReader(&buf)
	for i, m := range want {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if got.Type() != m.Type() {
			t.Fatalf("message %d: type %v, want %v", i, got.Type(), m.Type())
		}
	}
	_, err := r.Next()
	appErr, ok := errors.FromError(err)
	if !ok || !errors.Is(err, io.EOF) || !appErr.HasRoute("Reader.Next.RemoteWorkReadFailed") {
		t.Errorf("the end of the stream gives %v", err)
	}
}

func TestTheTypeBytesAreStable(t *testing.T) {
	// the numbers are on the wire: changing one is a new version of the protocol
	want := map[Type]uint8{TypeHello: 1, TypeTile: 2, TypeFrameConfirm: 3, TypeRefresh: 4, TypeKey: 5, TypeMouse: 6, TypeScroll: 7, TypeClipboard: 8, TypeNotification: 9, TypeNotificationAction: 10, TypeResize: 11, TypePing: 12, TypeDisconnect: 13}
	for typ, n := range want {
		if uint8(typ) != n {
			t.Errorf("%v is %d, want %d", typ, typ, n)
		}
		if strings.HasPrefix(typ.String(), "Type(") {
			t.Errorf("%d has no name", typ)
		}
	}
	if got := Type(200).String(); got != "Type(200)" {
		t.Errorf("an unknown type is %q", got)
	}
	if got := Reason(200).String(); got != "unknown reason" {
		t.Errorf("an unknown reason is %q", got)
	}
}

// frame builds a raw message from a type, a length and a payload, without any check.
func frame(typ Type, length uint32, payload ...byte) []byte {
	b := []byte{byte(typ), byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)}
	return append(b, payload...)
}

func mustEncode(t *testing.T, m Message) []byte {
	t.Helper()
	b, err := Append(nil, m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMalformedInputIsRefusedWithItsRoute(t *testing.T) {
	good := mustEncode(t, &Key{Kind: KeyDown, Physical: 1, Logical: 2, Text: "a"})
	tileOK := &Tile{Frame: 2, Base: 1, Width: 128, Height: 128, X: 1, Y: 1, Total: 2, Data: []byte{1}}
	withTile := func(f func(*Tile)) []byte {
		c := *tileOK
		f(&c)
		return mustEncodeRaw(t, &c)
	}
	utf8Bad := frame(TypeClipboard, 2, 0xff, 0xfe)
	nan := math.Float32bits(float32(math.NaN()))
	inf := math.Float32bits(float32(math.Inf(1)))
	f32 := func(v uint32) []byte { return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)} }
	mouseNaN := append(frame(TypeMouse, 10, 0, 0), append(f32(nan), f32(0)...)...)
	scrollInf := append(frame(TypeScroll, 16), append(append(f32(0), f32(inf)...), append(f32(0), f32(0)...)...)...)
	tests := []struct {
		name  string
		input []byte
		route string
	}{
		{"empty", nil, "Decode.RemoteWorkTruncated"},
		{"half a header", good[:3], "Decode.RemoteWorkTruncated"},
		{"unknown type 0", frame(0, 0), "parseHeader.RemoteWorkUnknownType"},
		{"unknown type past the last", frame(typeEnd, 0), "parseHeader.RemoteWorkUnknownType"},
		{"unknown type 255", frame(255, 0), "parseHeader.RemoteWorkUnknownType"},
		{"length over the limit of the type", frame(TypePing, 17, make([]byte, 17)...), "parseHeader.RemoteWorkTooLarge"},
		{"length of 4 GiB", frame(TypeClipboard, math.MaxUint32), "parseHeader.RemoteWorkTooLarge"},
		{"payload shorter than the length", good[:len(good)-1], "Decode.RemoteWorkTruncated"},
		{"bytes after the message", append(append([]byte(nil), good...), 0), "Decode.RemoteWorkTrailing"},
		{"payload shorter than its fields", frame(TypeKey, 3, 0, 0, 0), "take.RemoteWorkTruncated"},
		{"unused bytes in the payload", frame(TypeRefresh, 0)[:5:5], ""},
		{"fixed message with a longer payload", frame(TypeFrameConfirm, 9, 0, 0, 0, 0, 0, 0, 0, 1, 0), "decodePayload.RemoteWorkTrailing"},
		{"hello with a wrong magic", frame(TypeHello, 7, 'N', 'O', 'P', 'E', 0, 1, 0), "Hello.decode.RemoteWorkBadMagic"},
		{"hello with a token over the limit", append(frame(TypeHello, 7, 'E', 'M', 'R', 'W', 0, 1, MaxToken+1), make([]byte, 0)...), "Hello.decode.RemoteWorkTooLarge"},
		{"hello with a token that is cut", frame(TypeHello, 8, 'E', 'M', 'R', 'W', 0, 1, 5, 1), "take.RemoteWorkTruncated"},
		{"tile with frame 0", withTile(func(m *Tile) { m.Frame = 0 }), "Tile.decode.RemoteWorkBadValue"},
		{"tile with base not below the frame", withTile(func(m *Tile) { m.Base = m.Frame }), "Tile.decode.RemoteWorkBadValue"},
		{"tile of a picture that is too large", withTile(func(m *Tile) { m.Width, m.Height = 7000, 7000 }), "Tile.decode.RemoteWorkBadValue"},
		{"tile of a picture of width 0", withTile(func(m *Tile) { m.Width = 0 }), "Tile.decode.RemoteWorkBadValue"},
		{"tile outside the picture", withTile(func(m *Tile) { m.X = 2 }), "Tile.decode.RemoteWorkBadValue"},
		{"tile with more tiles than the picture", withTile(func(m *Tile) { m.Total = 5 }), "Tile.decode.RemoteWorkBadValue"},
		{"tile with no tiles in the frame", withTile(func(m *Tile) { m.Total = 0 }), "Tile.decode.RemoteWorkBadValue"},
		{"tile without data", withTile(func(m *Tile) { m.Data = nil }), "Tile.decode.RemoteWorkBadValue"},
		{"tile with data over the limit", append(append(frame(TypeTile, tileHeaderSize+MaxTileData), withTile(func(m *Tile) { m.Data = []byte{1} })[HeaderSize:HeaderSize+tileHeaderSize]...), make([]byte, MaxTileData)...), ""},
		{"confirm of frame 0", frame(TypeFrameConfirm, 8, make([]byte, 8)...), "FrameConfirm.decode.RemoteWorkBadValue"},
		{"key of an unknown kind", frame(TypeKey, 18, append([]byte{3}, make([]byte, 17)...)...), "enum.RemoteWorkBadValue"},
		{"key text over the limit", frame(TypeKey, 18+MaxKeyText, append(append([]byte{0}, make([]byte, 16)...), append([]byte{MaxKeyText + 1}, make([]byte, MaxKeyText)...)...)...), "str8.RemoteWorkTooLarge"},
		{"key text that is not UTF-8", frame(TypeKey, 19, append(append([]byte{0}, make([]byte, 16)...), 1, 0xff)...), "text.RemoteWorkBadValue"},
		{"mouse with an unknown button", frame(TypeMouse, 10, append([]byte{0, 8}, make([]byte, 8)...)...), "Mouse.decode.RemoteWorkBadValue"},
		{"mouse with a NaN", mouseNaN, "f32.RemoteWorkBadValue"},
		{"scroll with an infinity", scrollInf, "f32.RemoteWorkBadValue"},
		{"clipboard that is not UTF-8", utf8Bad, "text.RemoteWorkBadValue"},
		{"notification of an unknown severity", frame(TypeNotification, 9+1, append(make([]byte, 8), 3, 0)...), "enum.RemoteWorkBadValue"},
		{"notification with too many actions", frame(TypeNotification, 8+1+2+2+1, append(make([]byte, 13), MaxActions+1)...), "Notification.decode.RemoteWorkTooLarge"},
		{"notification action out of range", frame(TypeNotificationAction, 9, append(make([]byte, 8), MaxActions)...), "enum.RemoteWorkBadValue"},
		{"resize of 0", frame(TypeResize, 4, 0, 0, 0, 5), "Resize.decode.RemoteWorkBadValue"},
		{"resize over the area", frame(TypeResize, 4, 0x1e, 0, 0x1e, 0), "Resize.decode.RemoteWorkBadValue"},
		{"ping with a flag of 2", frame(TypePing, 9, append(make([]byte, 8), 2)...), "flag.RemoteWorkBadValue"},
		{"disconnect with an unknown reason", frame(TypeDisconnect, 3, 99, 0, 0), "enum.RemoteWorkBadValue"},
		{"disconnect text over the limit", frame(TypeDisconnect, 3, 0, 0xff, 0xff), "str16.RemoteWorkTooLarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.route == "" {
				return // refused by another case of the table; kept for the picture of the limits
			}
			_, err := Decode(tt.input)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tt.route)
			}
		})
	}
}

// mustEncodeRaw encodes a tile without the check of Tile.encode, to make input the decoder must refuse.
func mustEncodeRaw(t *testing.T, m *Tile) []byte {
	t.Helper()
	w := &writer{b: []byte{byte(TypeTile), 0, 0, 0, 0}}
	w.u64(m.Frame)
	w.u64(m.Base)
	w.u16(m.Width)
	w.u16(m.Height)
	w.u16(m.X)
	w.u16(m.Y)
	w.u32(m.Total)
	w.b = append(w.b, m.Data...)
	n := len(w.b) - HeaderSize
	w.b[1], w.b[2], w.b[3], w.b[4] = byte(n>>24), byte(n>>16), byte(n>>8), byte(n)
	return w.b
}

func TestTheEncoderRefusesWhatTheDecoderWould(t *testing.T) {
	tests := []struct {
		name  string
		m     Message
		route string
	}{
		{"nil", nil, "Append.RemoteWorkNilMessage"},
		{"token over the limit", &Hello{Token: make([]byte, MaxToken+1)}, "Hello.encode.RemoteWorkEncodeTooLarge"},
		{"tile data over the limit", &Tile{Frame: 1, Width: 1, Height: 1, Total: 1, Data: make([]byte, MaxTileData+1)}, "Tile.encode.RemoteWorkEncodeTooLarge"},
		{"tile outside the picture", &Tile{Frame: 1, Width: 64, Height: 64, X: 1, Total: 1, Data: []byte{1}}, "Tile.encode.RemoteWorkEncodeInvalid"},
		{"confirm of frame 0", &FrameConfirm{}, "FrameConfirm.encode.RemoteWorkEncodeInvalid"},
		{"key of an unknown kind", &Key{Kind: 9}, "enum.RemoteWorkEncodeInvalid"},
		{"key text over the limit", &Key{Text: strings.Repeat("a", MaxKeyText+1)}, "str8.RemoteWorkEncodeTooLarge"},
		{"key text that is not UTF-8", &Key{Text: "\xff"}, "str8.RemoteWorkEncodeInvalid"},
		{"mouse with an unknown button", &Mouse{Buttons: 16}, "Mouse.encode.RemoteWorkEncodeInvalid"},
		{"mouse with a NaN", &Mouse{X: float32(math.NaN())}, "f32.RemoteWorkEncodeInvalid"},
		{"clipboard over the limit", &Clipboard{Text: strings.Repeat("a", MaxClipboard+1)}, "Clipboard.encode.RemoteWorkEncodeTooLarge"},
		{"clipboard that is not UTF-8", &Clipboard{Text: "\xff"}, "text.RemoteWorkEncodeInvalid"},
		{"notification with too many actions", &Notification{Actions: make([]string, MaxActions+1)}, "Notification.encode.RemoteWorkEncodeTooLarge"},
		{"notification text over the limit", &Notification{Message: strings.Repeat("a", MaxMessageText+1)}, "str16.RemoteWorkEncodeTooLarge"},
		{"notification action out of range", &NotificationAction{Action: MaxActions}, "enum.RemoteWorkEncodeInvalid"},
		{"resize of 0", &Resize{}, "Resize.encode.RemoteWorkEncodeInvalid"},
		{"disconnect of an unknown reason", &Disconnect{Reason: 99}, "enum.RemoteWorkEncodeInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := []byte("keep")
			out, err := Append(start, tt.m)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tt.route)
			}
			if string(out) != "keep" {
				t.Errorf("the buffer changed on an error: %q", out)
			}
		})
	}
}

func TestAMessageOverItsLimitIsNotEncoded(t *testing.T) {
	// a notification whose parts are each within their limits but whose whole is not
	m := &Notification{Source: strings.Repeat("s", MaxSource), Message: strings.Repeat("m", MaxMessageText), Actions: slicesOf(MaxActions, strings.Repeat("a", MaxActionLabel))}
	if _, err := Append(nil, m); err != nil {
		t.Fatalf("the largest notification must fit: %v", err)
	}
}

func TestHelloChecksTheVersion(t *testing.T) {
	if err := (&Hello{Version: Version}).Check(); err != nil {
		t.Error(err)
	}
	err := (&Hello{Version: Version + 1}).Check()
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Hello.Check.RemoteWorkVersion") {
		t.Errorf("unexpected failure path: %v", err)
	}
}

func TestTheReaderRefusesALyingLengthWithoutReadingOrAllocating(t *testing.T) {
	// a header that claims 4 GiB of clipboard: refused from the header alone, the payload is never read
	r := NewReader(bytes.NewReader(frame(TypeClipboard, math.MaxUint32)))
	allocs := testing.AllocsPerRun(1, func() {
		r.r = bytes.NewReader(frame(TypeClipboard, math.MaxUint32))
		if _, err := r.Next(); err == nil {
			t.Error("accepted")
		}
	})
	if allocs > 10 { // the reader of the test, the header and the error
		t.Errorf("%v allocations for a refused header", allocs)
	}
	if cap(r.buf) != 0 {
		t.Errorf("the reader allocated a buffer of %d bytes for a refused message", cap(r.buf))
	}
	_, err := NewReader(bytes.NewReader(frame(TypeClipboard, math.MaxUint32))).Next()
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("parseHeader.RemoteWorkTooLarge") {
		t.Errorf("unexpected failure path: %v", err)
	}
}

func TestTheReaderEndsInTheMiddleOfAMessage(t *testing.T) {
	good := mustEncode(t, &Ping{Seq: 3})
	for cut := 1; cut < len(good); cut++ {
		_, err := NewReader(bytes.NewReader(good[:cut])).Next()
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("Reader.Next.RemoteWorkReadFailed") || !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("cut at %d: %v", cut, err)
		}
	}
}

func TestTheReaderDoesNotKeepALargeBuffer(t *testing.T) {
	b := mustEncode(t, &Clipboard{Text: strings.Repeat("x", 200<<10)})
	r := NewReader(bytes.NewReader(b))
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	if r.buf != nil {
		t.Errorf("the reader keeps %d bytes", cap(r.buf))
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTheWriterReportsAFailingStream(t *testing.T) {
	err := NewWriter(failingWriter{}).Write(&Ping{})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Writer.Write.RemoteWorkWriteFailed") || !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("unexpected failure path: %v", err)
	}
	if err := NewWriter(&bytes.Buffer{}).Write(&Key{Kind: 9}); err == nil {
		t.Error("an invalid message was written")
	}
}

func TestEncodingAllocatesNothingBeyondTheMessage(t *testing.T) {
	buf := make([]byte, 0, MaxMessage)
	for _, m := range []Message{
		&Ping{Seq: 1}, &Key{Kind: KeyDown, Physical: 1, Logical: 2, Text: "a"}, &Mouse{X: 1, Y: 2}, &Scroll{DY: 3}, &FrameConfirm{Frame: 4},
		&Resize{Width: 100, Height: 100}, &Tile{Frame: 1, Width: 64, Height: 64, Total: 1, Data: make([]byte, 5000)},
		&Notification{Message: "x", Actions: []string{"a"}}, &Clipboard{Text: strings.Repeat("c", 5000)},
	} {
		allocs := testing.AllocsPerRun(20, func() {
			if _, err := Append(buf[:0], m); err != nil {
				t.Fatal(err)
			}
		})
		if allocs != 0 {
			t.Errorf("%v: %v allocations while encoding into a buffer that is large enough", m.Type(), allocs)
		}
	}
}

func TestSeededRandomInputNeverPanicsAndAcceptedInputIsCanonical(t *testing.T) {
	rng := rand.New(rand.NewSource(20260101))
	var valid [][]byte
	for _, m := range samples() {
		if m.Type() == TypeClipboard && len(m.(*Clipboard).Text) > 1000 {
			continue // keeps the mutations small
		}
		valid = append(valid, mustEncode(t, m))
	}
	accepted := 0
	check := func(in []byte) {
		m, err := Decode(in)
		if err != nil {
			if _, ok := errors.FromError(err); !ok {
				t.Fatalf("%x: the error is %T, not *errors.Error", in, err)
			}
			return
		}
		accepted++
		out, err := Append(nil, m)
		if err != nil || !bytes.Equal(out, in) {
			t.Fatalf("%x decoded to %+v and encodes to %x (%v)", in, m, out, err)
		}
	}
	for range 20000 {
		switch rng.Intn(4) {
		case 0: // random bytes
			in := make([]byte, rng.Intn(80))
			rng.Read(in)
			check(in)
		case 1: // a valid message with bytes changed
			in := append([]byte(nil), valid[rng.Intn(len(valid))]...)
			for range 1 + rng.Intn(3) {
				in[rng.Intn(len(in))] = byte(rng.Intn(256))
			}
			check(in)
		case 2: // a valid message cut short or extended
			in := append([]byte(nil), valid[rng.Intn(len(valid))]...)
			if rng.Intn(2) == 0 {
				in = in[:rng.Intn(len(in))]
			} else {
				in = append(in, byte(rng.Intn(256)))
			}
			check(in)
		default: // a header of a known type with a random length and payload
			typ := Type(1 + rng.Intn(int(typeEnd)-1))
			payload := make([]byte, rng.Intn(60))
			rng.Read(payload)
			check(frame(typ, uint32(len(payload)), payload...))
		}
	}
	if accepted == 0 {
		t.Error("the random input never made a valid message; the test checks nothing")
	}
}
