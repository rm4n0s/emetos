package realinput

import (
	"context"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func connectFake(t *testing.T, globals []string) (*Client, *fakeCompositor) {
	t.Helper()
	f := newFakeCompositor(t, globals)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c, err := Connect(ctx, Options{Socket: f.socket, KeyDelay: time.Millisecond, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, f
}

// keyEvents turns the recorded requests of the keyboard into "+code", "-code" and "mods=N".
func keyEvents(reqs []fakeRequest) []string {
	var out []string
	for _, r := range reqs {
		if r.device != "keyboard" {
			continue
		}
		switch r.name {
		case "key":
			sign := "-"
			if r.args[2] == 1 {
				sign = "+"
			}
			out = append(out, sign+itoa(r.args[1]))
		case "modifiers":
			out = append(out, "mods="+itoa(r.args[0]))
		}
	}
	return out
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestConnectGivesTheKeyboardItsKeymap(t *testing.T) {
	_, f := connectFake(t, allGlobals)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keymap != keymapText+"\x00" {
		t.Errorf("keymap = %q, want the US keymap", f.keymap)
	}
}

func TestTypeAndPress(t *testing.T) {
	tests := []struct {
		name string
		do   func(ctx context.Context, c *Client) error
		want []string
	}{
		{"lower case", func(ctx context.Context, c *Client) error { return c.Type(ctx, "ab") }, []string{"+30", "-30", "+48", "-48"}},
		{"capital with shift", func(ctx context.Context, c *Client) error { return c.Type(ctx, "A") },
			[]string{"+42", "mods=1", "+30", "-30", "-42", "mods=0"}},
		{"symbol with shift", func(ctx context.Context, c *Client) error { return c.Type(ctx, "(") },
			[]string{"+42", "mods=1", "+10", "-10", "-42", "mods=0"}},
		{"space and new line", func(ctx context.Context, c *Client) error { return c.Type(ctx, " \n") }, []string{"+57", "-57", "+28", "-28"}},
		{"ctrl+p", func(ctx context.Context, c *Client) error { return c.Press(ctx, "ctrl+p") },
			[]string{"+29", "mods=4", "+25", "-25", "-29", "mods=0"}},
		{"ctrl+shift+v is case insensitive", func(ctx context.Context, c *Client) error { return c.Press(ctx, "Ctrl+Shift+V") },
			[]string{"+29", "+42", "mods=5", "+47", "-47", "-42", "-29", "mods=0"}},
		{"escape then f2", func(ctx context.Context, c *Client) error { return c.Press(ctx, "Escape", "f2") },
			[]string{"+1", "-1", "+60", "-60"}},
		{"ctrl+plus adds shift", func(ctx context.Context, c *Client) error { return c.Press(ctx, "ctrl++") },
			[]string{"+29", "+42", "mods=5", "+13", "-13", "-42", "-29", "mods=0"}},
		{"alt+left", func(ctx context.Context, c *Client) error { return c.Press(ctx, "alt+Left") },
			[]string{"+56", "mods=8", "+105", "-105", "-56", "mods=0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, f := connectFake(t, allGlobals)
			if err := tt.do(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			if got := keyEvents(f.taken()); !slices.Equal(got, tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEveryPrintableCharacterHasAKey(t *testing.T) {
	for r := rune(0x20); r < 0x7f; r++ {
		if _, ok := usKeys[r]; !ok {
			t.Errorf("no key for %q", r)
		}
	}
}

func TestPointer(t *testing.T) {
	c, f := connectFake(t, allGlobals)
	c.SetWindow(Point{100, 50}, Rect{0, 0, 1280, 800})
	ctx := context.Background()
	if err := c.Click(ctx, Point{10, 20}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range f.taken() {
		if r.device != "pointer" {
			continue
		}
		switch r.name {
		case "motion_absolute":
			got = append(got, "at "+itoa(r.args[1])+","+itoa(r.args[2])+" of "+itoa(r.args[3])+"x"+itoa(r.args[4]))
		case "button":
			got = append(got, "button "+itoa(r.args[1])+" state "+itoa(r.args[2]))
		}
	}
	want := []string{"at 110,70 of 1280x800", "button 272 state 1", "button 272 state 0"}
	if !slices.Equal(got, want) {
		t.Errorf("pointer requests = %v, want %v", got, want)
	}
	if err := c.DoubleClick(ctx, Point{1, 1}); err != nil {
		t.Fatal(err)
	}
	buttons := 0
	for _, r := range f.taken() {
		if r.name == "button" {
			buttons++
		}
	}
	if buttons != 4 {
		t.Errorf("a double click sent %d button events, want 4", buttons)
	}
	if err := c.Scroll(ctx, Point{5, 5}, 0, -2); err != nil {
		t.Fatal(err)
	}
	discrete := 0
	for _, r := range f.taken() {
		if r.name == "axis_discrete" {
			discrete++
			if int32(r.args[3]) != -1 {
				t.Errorf("a notch up has discrete %d, want -1", int32(r.args[3]))
			}
		}
	}
	if discrete != 2 {
		t.Errorf("two notches sent %d discrete axis events", discrete)
	}
}

func TestDragMovesInSteps(t *testing.T) {
	c, f := connectFake(t, allGlobals)
	c.SetWindow(Point{}, Rect{0, 0, 100, 100})
	if err := c.Drag(context.Background(), Point{0, 0}, Point{40, 0}, 4); err != nil {
		t.Fatal(err)
	}
	var xs []uint32
	for _, r := range f.taken() {
		if r.name == "motion_absolute" {
			xs = append(xs, r.args[1])
		}
	}
	if want := []uint32{0, 10, 20, 30, 40}; !slices.Equal(xs, want) {
		t.Errorf("x positions = %v, want %v", xs, want)
	}
}

func TestParseComboTable(t *testing.T) {
	tests := []struct {
		in   string
		code int
		mask int
	}{
		{"enter", 28, 0}, {"Escape", 1, 0}, {"F11", 87, 0}, {"ctrl+,", 51, modCtrl}, {"shift+tab", 15, modShift},
		{"super+a", 30, modSuper}, {"ctrl+alt+delete", 111, modCtrl | modAlt}, {"A", 30, 0}, {"?", 53, modShift},
	}
	for _, tt := range tests {
		got, err := parseCombo(tt.in)
		if err != nil {
			t.Errorf("parseCombo(%q): %v", tt.in, err)
			continue
		}
		if got.code != tt.code || got.mask != tt.mask {
			t.Errorf("parseCombo(%q) = code %d mask %d, want %d, %d", tt.in, got.code, got.mask, tt.code, tt.mask)
		}
	}
}

// routeOf checks the route of an error.
func requireRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
	}
}

func TestErrorRoutes(t *testing.T) {
	ctx := context.Background()
	t.Run("no socket", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		_, err := Connect(ctx, Options{})
		requireRoute(t, err, "Connect.RealInputConnectFailed")
	})
	t.Run("nothing listens", func(t *testing.T) {
		_, err := Connect(ctx, Options{Socket: "/nonexistent/wayland-socket"})
		requireRoute(t, err, "Connect.RealInputConnectFailed")
	})
	t.Run("protocols missing", func(t *testing.T) {
		f := newFakeCompositor(t, []string{"wl_seat"})
		_, err := Connect(ctx, Options{Socket: f.socket})
		requireRoute(t, err, "Connect->Client.setup.RealInputProtocolMissing")
		if appErr, _ := errors.FromError(err); appErr != nil && !strings.Contains(strings.Join(toStrings(appErr.Metadata["missing"]), ","), "zwp_virtual_keyboard_manager_v1") {
			t.Errorf("metadata = %v, want the missing keyboard manager", appErr.Metadata)
		}
	})
	t.Run("a protocol error", func(t *testing.T) {
		f := newFakeCompositor(t, allGlobals)
		f.failOnSync.Store(true)
		_, err := Connect(ctx, Options{Socket: f.socket})
		requireRoute(t, err, "Client.roundTrip.RealInputCompositorError")
	})
	t.Run("a character with no key", func(t *testing.T) {
		c, _ := connectFake(t, allGlobals)
		requireRoute(t, c.Type(ctx, "é"), "Client.Type.RealInputKeyUnknown")
	})
	t.Run("a combination that is wrong", func(t *testing.T) {
		c, f := connectFake(t, allGlobals)
		for _, combo := range []string{"hyper+p", "ctrl+nonsense", "ctrl+é"} {
			requireRoute(t, c.Press(ctx, "a", combo), "Client.Press->parseCombo.RealInputComboInvalid")
		}
		if got := keyEvents(f.taken()); len(got) != 0 {
			t.Errorf("a wrong combination still pressed %v", got)
		}
	})
	t.Run("the pointer without a window", func(t *testing.T) {
		c, _ := connectFake(t, allGlobals)
		requireRoute(t, c.Click(ctx, Point{1, 1}), "Client.clickAt->Client.moveTo.RealInputNoWindow")
	})
	t.Run("a closed client", func(t *testing.T) {
		c, _ := connectFake(t, allGlobals)
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		requireRoute(t, c.Type(ctx, "a"), "Client.Type->Client.alive.RealInputClosed")
	})
	t.Run("a canceled wait", func(t *testing.T) {
		c, _ := connectFake(t, allGlobals)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		requireRoute(t, c.Type(canceled, "a"), "Client.Type->Client.tap")
	})
	t.Run("a pause that is canceled", func(t *testing.T) {
		c, _ := connectFake(t, allGlobals)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		requireRoute(t, c.pause(canceled, time.Second), "Client.pause.RealInputCanceled")
	})
	t.Run("the compositor hangs up", func(t *testing.T) {
		c, f := connectFake(t, allGlobals)
		f.hangUp.Store(true)
		requireRoute(t, c.Sync(ctx), "Client.Sync->Client.roundTrip->wireConn.read.RealInputReadFailed")
	})
	t.Run("the compositor sends a short event", func(t *testing.T) {
		c, f := connectFake(t, allGlobals)
		f.shortEvent.Store(true)
		requireRoute(t, c.Sync(ctx), "wireConn.read.RealInputReadFailed")
	})
	t.Run("the keymap cannot be made", func(t *testing.T) {
		f := newFakeCompositor(t, allGlobals)
		_, err := Connect(ctx, Options{Socket: f.socket, newKeymapFile: func(string) (*os.File, error) { return nil, syscall.ENOMEM }})
		requireRoute(t, err, "Connect->Client.setup.RealInputKeymapFailed")
	})
	t.Run("the compositor goes away", func(t *testing.T) {
		c, f := connectFake(t, allGlobals)
		_ = f.listener.Close()
		_ = c.w.conn.Close()
		requireRoute(t, c.Type(ctx, "a"), "RealInputWriteFailed")
	})
}

func toStrings(v any) []string {
	s, _ := v.([]string)
	return s
}
