package realinput

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/rm4n0s/errors"
)

// Point is a position in pixels.
type Point struct{ X, Y float64 }

// Rect is a rectangle in pixels.
type Rect struct{ X, Y, Width, Height float64 }

// Button is a button of the pointer, as Linux names it (linux/input-event-codes.h).
type Button uint32

// The buttons of the pointer.
const (
	// ButtonLeft is BTN_LEFT.
	ButtonLeft Button = 0x110
	// ButtonRight is BTN_RIGHT.
	ButtonRight Button = 0x111
	// ButtonMiddle is BTN_MIDDLE.
	ButtonMiddle Button = 0x112
)

// NoKeyDelay as Options.KeyDelay means no wait between keys.
const NoKeyDelay time.Duration = -1

// Options configure Connect.
type Options struct {
	// Socket is the Wayland socket of the compositor: a path, or a name in XDG_RUNTIME_DIR. WAYLAND_DISPLAY (and
	// XDG_RUNTIME_DIR) when empty.
	Socket string
	// KeyDelay is how long the client waits after each key (and after its round trip), so that the application sees the
	// keys at the pace of a fast typist rather than in one burst; 10 ms when zero, and a negative value (NoKeyDelay) is no
	// wait at all: the next key follows as soon as the compositor has passed the last one on. Zero is therefore not "all keys
	// in one batch"; TypeBatch and PressBatch send keys in one batch.
	KeyDelay time.Duration
	// Timeout bounds every wait for the compositor (a round trip, a write); 5 s when zero.
	Timeout time.Duration

	// newKeymapFile makes the file of the keymap; a test replaces it. sealedFile when nil.
	newKeymapFile func(text string) (*os.File, error)
}

// A Client is one virtual keyboard and one virtual pointer on a compositor. It is safe for concurrent use; the calls
// are made one at a time.
type Client struct {
	mu   sync.Mutex
	w    wireConn
	opts Options
	// start is the time of the connection, from which the time stamps of the events count.
	start time.Time
	// keyboard and pointer are the ids of the virtual devices.
	keyboard, pointer uint32
	// keymap is the file of the keymap; it stays open for the life of the client.
	keymap *os.File
	// layout is the bounding box of the outputs, in layout pixels, and window the origin of the window in the layout.
	layout Rect
	window Point
}

// Connect connects to the compositor, makes the one virtual keyboard (with its keymap) and the virtual pointer, and
// returns when the compositor has them. The errors have the tags RealInputConnectFailed, RealInputProtocolMissing (the
// compositor lacks the seat or one of the two virtual device protocols), RealInputKeymapFailed, RealInputCompositorError,
// RealInputWriteFailed and RealInputReadFailed.
func Connect(ctx context.Context, opts Options) (*Client, error) {
	if opts.KeyDelay == 0 {
		opts.KeyDelay = 10 * time.Millisecond
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	socket := opts.Socket
	if socket == "" {
		socket = os.Getenv("WAYLAND_DISPLAY")
	}
	if socket == "" {
		return nil, errors.New("RealInputConnectFailed", "no compositor: WAYLAND_DISPLAY is not set and Options.Socket is empty")
	}
	if !filepath.IsAbs(socket) {
		socket = filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), socket)
	}
	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, errors.NewErr("RealInputConnectFailed", err).SetMetadata("socket", socket)
	}
	conn, ok := raw.(*net.UnixConn)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("RealInputConnectFailed", "the socket is not a Unix socket", "socket", socket)
	}
	c := &Client{w: wireConn{conn: conn, next: 2, timeout: opts.Timeout}, opts: opts, start: time.Now()}
	if err := c.setup(ctx); err != nil {
		_ = conn.Close()
		if c.keymap != nil {
			_ = c.keymap.Close()
		}
		return nil, err
	}
	return c, nil
}

// global is an interface the compositor announced.
type global struct {
	name, version uint32
}

// setup binds the globals and makes the devices.
func (c *Client) setup(ctx context.Context) error {
	const (
		displayID  = 1
		opGetReg   = 1 // wl_display.get_registry
		opBind     = 0 // wl_registry.bind
		opKeyboard = 0 // zwp_virtual_keyboard_manager_v1.create_virtual_keyboard
		opKeymap   = 0 // zwp_virtual_keyboard_v1.keymap
		opPointer  = 0 // zwlr_virtual_pointer_manager_v1.create_virtual_pointer
	)
	registry := c.w.newID()
	if err := c.w.send(displayID, opGetReg, u32(registry), -1); err != nil {
		return err
	}
	globals := map[string]global{}
	err := c.roundTrip(ctx, func(ev event) {
		if ev.obj != registry || ev.opcode != 0 || len(ev.args) < 8 {
			return
		}
		name := binary.LittleEndian.Uint32(ev.args)
		iface, off, ok := argString(ev.args, 4)
		if !ok || off+4 > len(ev.args) {
			return
		}
		if _, seen := globals[iface]; !seen {
			globals[iface] = global{name: name, version: binary.LittleEndian.Uint32(ev.args[off:])}
		}
	})
	if err != nil {
		return err
	}
	var missing []string
	for _, iface := range []string{"wl_seat", "zwp_virtual_keyboard_manager_v1", "zwlr_virtual_pointer_manager_v1"} {
		if _, ok := globals[iface]; !ok {
			missing = append(missing, iface)
		}
	}
	if len(missing) > 0 {
		return errors.New("RealInputProtocolMissing", "the compositor lacks what a virtual keyboard and pointer need (sway has all of it)", "missing", missing)
	}
	bind := func(iface string, version uint32) (uint32, error) {
		g := globals[iface]
		id := c.w.newID()
		return id, c.w.send(registry, opBind, cat(u32(g.name), str(iface), u32(min(g.version, version)), u32(id)), -1)
	}
	seat, err := bind("wl_seat", 1)
	if err != nil {
		return err
	}
	keyboards, err := bind("zwp_virtual_keyboard_manager_v1", 1)
	if err != nil {
		return err
	}
	pointers, err := bind("zwlr_virtual_pointer_manager_v1", 1)
	if err != nil {
		return err
	}
	c.keyboard = c.w.newID()
	if err := c.w.send(keyboards, opKeyboard, cat(u32(seat), u32(c.keyboard)), -1); err != nil {
		return err
	}
	c.pointer = c.w.newID()
	if err := c.w.send(pointers, opPointer, cat(u32(seat), u32(c.pointer)), -1); err != nil {
		return err
	}
	makeFile := sealedFile
	if c.opts.newKeymapFile != nil {
		makeFile = c.opts.newKeymapFile
	}
	file, err := makeFile(keymapText + "\x00")
	if err != nil {
		return errors.NewErr("RealInputKeymapFailed", err)
	}
	c.keymap = file
	// format 1 is xkb_v1
	if err := c.w.send(c.keyboard, opKeymap, cat(u32(1), u32(uint32(len(keymapText)+1))), int(file.Fd())); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	// the compositor hands the keymap to the focused client when it has handled the request; the application's keyboard
	// object takes a moment more to load it
	time.Sleep(20 * time.Millisecond)
	return nil
}

// sealedFile makes an anonymous file with the text and seals it against changes, as a keymap is shared: with
// memfd_create, or, where that is not possible, a temporary file that is removed at once.
func sealedFile(text string) (*os.File, error) {
	name, err := syscall.BytePtrFromString("emetos-keymap")
	if err != nil {
		return nil, err
	}
	const (
		mfdCloexec      = 0x1
		mfdAllowSealing = 0x2
		fAddSeals       = 1033
		sealAll         = 0x1 | 0x2 | 0x4 | 0x8 // shrink, grow, write, seal
	)
	if sysMemfdCreate != 0 {
		fd, _, errno := syscall.Syscall(sysMemfdCreate, uintptr(unsafe.Pointer(name)), mfdCloexec|mfdAllowSealing, 0)
		if errno == 0 {
			f := os.NewFile(fd, "emetos-keymap")
			if _, err := f.WriteString(text); err != nil {
				_ = f.Close()
				return nil, err
			}
			if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), fAddSeals, sealAll); errno != 0 {
				_ = f.Close()
				return nil, errno
			}
			return f, nil
		}
	}
	f, err := os.CreateTemp("", "emetos-keymap")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(f.Name()); err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// roundTrip asks the compositor for a wl_display.sync and reads until it answers; handle, when set, sees every event
// on the way. The compositor answers in order, so every earlier request has been handled when it returns. An error event
// of the display is returned with the tag RealInputCompositorError.
func (c *Client) roundTrip(ctx context.Context, handle func(event)) error {
	callback := c.w.newID()
	if err := c.w.send(1, 0, u32(callback), -1); err != nil {
		return err
	}
	for {
		ev, err := c.w.read(ctx)
		if err != nil {
			return err
		}
		if ev.obj == 1 && ev.opcode == 0 {
			// wl_display.error(object_id, code, message)
			message := ""
			if len(ev.args) >= 8 {
				message, _, _ = argString(ev.args, 8)
			}
			return errors.New("RealInputCompositorError", "the compositor ended the connection with a protocol error", "message", message)
		}
		if ev.obj == callback {
			return nil
		}
		if handle != nil {
			handle(ev)
		}
	}
}

// Close removes the devices by closing the connection, and releases the keymap. Close it only when the application
// under test has ended: a compositor that loses its last keyboard tells the clients there is no keymap, and they drop
// keys.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.w.conn == nil {
		return nil
	}
	err := c.w.conn.Close()
	c.w.conn = nil
	if c.keymap != nil {
		_ = c.keymap.Close()
		c.keymap = nil
	}
	return err
}

// now is the time stamp of an event, in milliseconds since the connection.
func (c *Client) now() uint32 { return uint32(time.Since(c.start).Milliseconds()) }

// ---- the keyboard ----

// Opcodes of zwp_virtual_keyboard_v1.
const (
	opKey       = 1
	opModifiers = 2
)

// keyRequest makes the request of one key event of the virtual keyboard.
func (c *Client) keyRequest(code int, down bool) []byte {
	state := uint32(0)
	if down {
		state = 1
	}
	return encode(c.keyboard, opKey, cat(u32(c.now()), u32(uint32(code)), u32(state)))
}

// modifiersRequest makes the request that tells the compositor the modifiers held.
func (c *Client) modifiersRequest(mask int) []byte {
	return encode(c.keyboard, opModifiers, cat(u32(uint32(mask)), u32(0), u32(0), u32(0)))
}

// downRequests are the requests that press a key with its modifiers held: the modifier keys, their mask, the key.
func (c *Client) downRequests(k combo) [][]byte {
	var reqs [][]byte
	for _, m := range k.mods {
		reqs = append(reqs, c.keyRequest(m, true))
	}
	if k.mask != 0 {
		reqs = append(reqs, c.modifiersRequest(k.mask))
	}
	return append(reqs, c.keyRequest(k.code, true))
}

// upRequests are the requests that release a key and its modifiers.
func (c *Client) upRequests(k combo) [][]byte {
	reqs := [][]byte{c.keyRequest(k.code, false)}
	for i := len(k.mods) - 1; i >= 0; i-- {
		reqs = append(reqs, c.keyRequest(k.mods[i], false))
	}
	if k.mask != 0 {
		reqs = append(reqs, c.modifiersRequest(0))
	}
	return reqs
}

// tapRequests are the requests that press and release one key with the modifiers held.
func (c *Client) tapRequests(k combo) [][]byte { return append(c.downRequests(k), c.upRequests(k)...) }

// sendEach writes the requests one at a time.
func (c *Client) sendEach(reqs [][]byte) error {
	for _, r := range reqs {
		if err := c.w.write(r, -1, c.keyboard, 0); err != nil {
			return err
		}
	}
	return nil
}

// tap presses and releases one key with the modifiers held, then waits for the compositor and the key delay.
func (c *Client) tap(ctx context.Context, k combo) error {
	if err := c.sendEach(c.tapRequests(k)); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	return c.pause(ctx, c.opts.KeyDelay)
}

// tapBatch presses and releases keys one after the other with one write for all of them, as an input method that hands the
// application several keys at once, then waits for the compositor.
func (c *Client) tapBatch(ctx context.Context, keys []combo) error {
	var reqs [][]byte
	for _, k := range keys {
		reqs = append(reqs, c.tapRequests(k)...)
	}
	if err := c.w.sendBatch(reqs); err != nil {
		return err
	}
	return c.roundTrip(ctx, nil)
}

// pause waits, and ends early with the error of ctx.
func (c *Client) pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return errors.NewErr("RealInputCanceled", ctx.Err())
	}
}

// strokes turns text into the keys that type it on the US layout.
func strokes(text string) ([]combo, error) {
	var out []combo
	for _, r := range text {
		k, ok := usKeys[r]
		if !ok {
			return nil, errors.New("RealInputKeyUnknown", "the character has no key on the US layout", "character", string(r))
		}
		stroke := combo{code: k.code}
		if k.shift {
			stroke.mods, stroke.mask = []int{keyLeftShift}, modShift
		}
		out = append(out, stroke)
	}
	return out, nil
}

// Type types the text on the US layout, a character at a time, with Shift for capitals and symbols; a new line is Enter
// and a tab is Tab. A character that has no key on the layout fails with the tag RealInputKeyUnknown, before anything is
// typed.
func (c *Client) Type(ctx context.Context, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys, err := strokes(text)
	if err != nil {
		return err
	}
	if err := c.alive(); err != nil {
		return err
	}
	for _, k := range keys {
		if err := c.tap(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// TypeBatch types the text as Type does, but all its keys are written to the compositor at once, with no round trip or wait
// between them: the application gets them in one read, as when an input method or a fast burst delivers several keys together.
func (c *Client) TypeBatch(ctx context.Context, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys, err := strokes(text)
	if err != nil {
		return err
	}
	if err := c.alive(); err != nil {
		return err
	}
	return c.tapBatch(ctx, keys)
}

// parseCombos reads key combinations.
func parseCombos(combos []string) ([]combo, error) {
	parsed := make([]combo, 0, len(combos))
	for _, text := range combos {
		k, err := parseCombo(text)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, k)
	}
	return parsed, nil
}

// Press presses key combinations one after the other: "ctrl+p", "Escape", "alt+Left", "ctrl+shift+v", "f2", "enter". A
// combination that cannot be read fails with the tag RealInputComboInvalid, before any key is pressed.
func (c *Client) Press(ctx context.Context, combos ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	parsed, err := parseCombos(combos)
	if err != nil {
		return err
	}
	if err := c.alive(); err != nil {
		return err
	}
	for _, k := range parsed {
		if err := c.tap(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// PressBatch presses key combinations as Press does, with all the keys written to the compositor at once (see TypeBatch).
func (c *Client) PressBatch(ctx context.Context, combos ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	parsed, err := parseCombos(combos)
	if err != nil {
		return err
	}
	if err := c.alive(); err != nil {
		return err
	}
	return c.tapBatch(ctx, parsed)
}

// Hold holds a key down for a time and releases it. The compositor sends no repeats (in Wayland the client makes them, from
// the repeat rate and delay of wl_keyboard.repeat_info, 25 a second after 600 ms unless the compositor says otherwise), so a
// key held long enough repeats in the application as it does for a user.
func (c *Client) Hold(ctx context.Context, combo string, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, err := parseCombo(combo)
	if err != nil {
		return err
	}
	if err := c.alive(); err != nil {
		return err
	}
	if err := c.sendEach(c.downRequests(k)); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	held := c.pause(ctx, d)
	// the key is released also when the wait was canceled, so that it does not stay down
	if err := c.sendEach(c.upRequests(k)); err != nil {
		return err
	}
	// the compositor has the release when this returns, also after a canceled wait
	if err := c.roundTrip(context.WithoutCancel(ctx), nil); err != nil {
		return err
	}
	return held
}

// alive tells whether the client is connected.
func (c *Client) alive() error {
	if c.w.conn == nil {
		return errors.New("RealInputClosed", "the client is closed")
	}
	return nil
}

// Sync waits for the compositor to handle everything sent so far.
func (c *Client) Sync(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.alive(); err != nil {
		return err
	}
	return c.roundTrip(ctx, nil)
}

// ---- the pointer ----

// Opcodes of zwlr_virtual_pointer_v1.
const (
	opMotionAbsolute = 1
	opButton         = 2
	opAxis           = 3
	opFrame          = 4
	opAxisSource     = 5
	opAxisDiscrete   = 7
)

// SetWindow tells the client where windows coordinates are: the position of the top left of the window's content in the
// layout, and the bounding box of the outputs (SwayWindowOf and SwayLayout give both). Pointer methods take window
// coordinates.
func (c *Client) SetWindow(origin Point, layout Rect) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.window, c.layout = origin, layout
}

// moveTo moves the pointer to a position of the window, and ends the event with a frame.
func (c *Client) moveTo(p Point) error {
	if c.layout.Width <= 0 || c.layout.Height <= 0 {
		return errors.New("RealInputNoWindow", "the pointer needs SetWindow: the layout of the outputs is not known")
	}
	x := min(max(c.window.X+p.X-c.layout.X, 0), c.layout.Width-1)
	y := min(max(c.window.Y+p.Y-c.layout.Y, 0), c.layout.Height-1)
	args := cat(u32(c.now()), u32(uint32(x+0.5)), u32(uint32(y+0.5)), u32(uint32(c.layout.Width)), u32(uint32(c.layout.Height)))
	if err := c.w.send(c.pointer, opMotionAbsolute, args, -1); err != nil {
		return err
	}
	return c.w.send(c.pointer, opFrame, nil, -1)
}

func (c *Client) button(b Button, down bool) error {
	state := uint32(0)
	if down {
		state = 1
	}
	if err := c.w.send(c.pointer, opButton, cat(u32(c.now()), u32(uint32(b)), u32(state)), -1); err != nil {
		return err
	}
	return c.w.send(c.pointer, opFrame, nil, -1)
}

// MoveTo moves the pointer to a position of the window.
func (c *Client) MoveTo(ctx context.Context, p Point) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.alive(); err != nil {
		return err
	}
	if err := c.moveTo(p); err != nil {
		return err
	}
	return c.roundTrip(ctx, nil)
}

// click presses and releases a button where the pointer is, count times in quick succession.
func (c *Client) click(ctx context.Context, b Button, count int) error {
	for i := range count {
		if i > 0 {
			if err := c.pause(ctx, 40*time.Millisecond); err != nil {
				return err
			}
		}
		if err := c.button(b, true); err != nil {
			return err
		}
		if err := c.roundTrip(ctx, nil); err != nil {
			return err
		}
		if err := c.pause(ctx, 20*time.Millisecond); err != nil {
			return err
		}
		if err := c.button(b, false); err != nil {
			return err
		}
		if err := c.roundTrip(ctx, nil); err != nil {
			return err
		}
	}
	return nil
}

// Click moves the pointer to a position of the window and clicks the left button.
func (c *Client) Click(ctx context.Context, p Point) error { return c.clickAt(ctx, p, ButtonLeft, 1) }

// DoubleClick moves the pointer to a position of the window and clicks the left button twice, quickly.
func (c *Client) DoubleClick(ctx context.Context, p Point) error {
	return c.clickAt(ctx, p, ButtonLeft, 2)
}

// RightClick moves the pointer to a position of the window and clicks the right button.
func (c *Client) RightClick(ctx context.Context, p Point) error {
	return c.clickAt(ctx, p, ButtonRight, 1)
}

func (c *Client) clickAt(ctx context.Context, p Point, b Button, count int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.alive(); err != nil {
		return err
	}
	if err := c.moveTo(p); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	// the application takes the enter of the pointer before the press
	if err := c.pause(ctx, 20*time.Millisecond); err != nil {
		return err
	}
	return c.click(ctx, b, count)
}

// ClickHolding moves the pointer to a position of the window and clicks the left button while a modifier key ("ctrl", "alt",
// "shift") is held, which is released after the click. It fails with RealInputComboInvalid for a name that is not a modifier.
func (c *Client) ClickHolding(ctx context.Context, p Point, modifier string) error {
	m, ok := modifierKeys[strings.ToLower(modifier)]
	if !ok {
		return errors.New("RealInputComboInvalid", "a click holds a modifier that does not exist", "modifier", modifier)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.alive(); err != nil {
		return err
	}
	held := combo{mask: m.mask, code: m.code}
	if err := c.moveTo(p); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	// the application takes the enter of the pointer before the press
	if err := c.pause(ctx, 20*time.Millisecond); err != nil {
		return err
	}
	if err := c.sendEach(c.downRequests(held)); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	clicked := c.click(ctx, ButtonLeft, 1)
	// the key is released also when the click failed, so that it does not stay down
	if err := c.sendEach(c.upRequests(held)); err != nil {
		return err
	}
	if err := c.roundTrip(context.WithoutCancel(ctx), nil); err != nil {
		return err
	}
	return clicked
}

// Drag presses the left button at one position, moves to another in steps, and releases it there.
func (c *Client) Drag(ctx context.Context, from, to Point, steps int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.alive(); err != nil {
		return err
	}
	if err := c.moveTo(from); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	if err := c.pause(ctx, 20*time.Millisecond); err != nil {
		return err
	}
	if err := c.button(ButtonLeft, true); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	steps = max(steps, 1)
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		if err := c.moveTo(Point{from.X + (to.X-from.X)*f, from.Y + (to.Y-from.Y)*f}); err != nil {
			return err
		}
		if err := c.roundTrip(ctx, nil); err != nil {
			return err
		}
		if err := c.pause(ctx, 15*time.Millisecond); err != nil {
			return err
		}
	}
	if err := c.button(ButtonLeft, false); err != nil {
		return err
	}
	return c.roundTrip(ctx, nil)
}

// Scroll moves the pointer to a position of the window and turns the wheel: clicksY notches down when positive (up when
// negative), clicksX notches to the right when positive.
func (c *Client) Scroll(ctx context.Context, p Point, clicksX, clicksY int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.alive(); err != nil {
		return err
	}
	if err := c.moveTo(p); err != nil {
		return err
	}
	if err := c.roundTrip(ctx, nil); err != nil {
		return err
	}
	if err := c.pause(ctx, 20*time.Millisecond); err != nil {
		return err
	}
	for _, axis := range []struct {
		id     uint32
		clicks int
	}{{0, clicksY}, {1, clicksX}} {
		step := 1
		if axis.clicks < 0 {
			step = -1
		}
		for range abs(axis.clicks) {
			// a notch of a wheel is 15 units of the axis (libinput's), and one discrete step
			if err := c.w.send(c.pointer, opAxisSource, u32(0), -1); err != nil {
				return err
			}
			args := cat(u32(c.now()), u32(axis.id), fixed(float64(15*step)), u32(uint32(int32(step))))
			if err := c.w.send(c.pointer, opAxisDiscrete, args, -1); err != nil {
				return err
			}
			if err := c.w.send(c.pointer, opFrame, nil, -1); err != nil {
				return err
			}
			if err := c.roundTrip(ctx, nil); err != nil {
				return err
			}
			if err := c.pause(ctx, 15*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
