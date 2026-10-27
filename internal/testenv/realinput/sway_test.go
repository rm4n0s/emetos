package realinput

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeSway answers the IPC of sway: it replies to each message type with the payload in replies, or with garbage when
// the entry is "garbage:".
func fakeSway(t *testing.T, replies map[uint32]string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "emetos-fakesway")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "sway.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	t.Setenv("SWAYSOCK", socket)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				header := make([]byte, 14)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				payload := make([]byte, binary.NativeEndian.Uint32(header[6:]))
				if _, err := io.ReadFull(conn, payload); err != nil {
					return
				}
				reply := replies[binary.NativeEndian.Uint32(header[10:])]
				magic := "i3-ipc"
				if reply == "garbage:" {
					magic, reply = "nothing", ""
				}
				out := append([]byte(magic), 0, 0, 0, 0, 0, 0, 0, 0)
				binary.NativeEndian.PutUint32(out[6:], uint32(len(reply)))
				_, _ = conn.Write(append(out, reply...))
			}()
		}
	}()
}

const swayTree = `{"nodes":[{"nodes":[{"nodes":[],"floating_nodes":[
  {"pid":4242,"app_id":"dev.emetos.Emetos","name":"Emetos","focused":true,
   "rect":{"x":100,"y":50,"width":800,"height":600},"window_rect":{"x":0,"y":2,"width":800,"height":598},"nodes":[],"floating_nodes":[]},
  {"pid":77,"app_id":"other","name":"other","rect":{"x":0,"y":0,"width":10,"height":10},"window_rect":{"x":0,"y":0,"width":10,"height":10},"nodes":[],"floating_nodes":[]}]}]}]}`

const swayOutputs = `[{"active":true,"rect":{"x":0,"y":0,"width":1280,"height":800}},
 {"active":true,"rect":{"x":1280,"y":-100,"width":800,"height":600}},
 {"active":false,"rect":{"x":9999,"y":9999,"width":1,"height":1}}]`

func TestSwayLayoutAndWindows(t *testing.T) {
	fakeSway(t, map[uint32]string{ipcGetOutputs: swayOutputs, ipcGetTree: swayTree, ipcRunCommand: `[{"success":true}]`})
	ctx := context.Background()
	layout, err := SwayLayout(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Rect{0, -100, 2080, 900}); layout != want {
		t.Errorf("layout = %+v, want %+v", layout, want)
	}
	w, err := SwayWindowOf(ctx, 4242)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Rect{100, 52, 800, 598}); w.Content != want || !w.Focused || w.AppID != "dev.emetos.Emetos" {
		t.Errorf("window = %+v, want content %+v", w, want)
	}
	if err := SwayCommand(ctx, "focus"); err != nil {
		t.Errorf("SwayCommand: %v", err)
	}
}

func TestSwayErrorRoutes(t *testing.T) {
	ctx := context.Background()
	t.Run("no socket", func(t *testing.T) {
		t.Setenv("SWAYSOCK", "")
		_, err := SwayLayout(ctx)
		requireRoute(t, err, "SwayLayout->swayCall.RealInputSwayFailed")
	})
	t.Run("nothing listens", func(t *testing.T) {
		t.Setenv("SWAYSOCK", "/nonexistent/sway.sock")
		_, err := SwayWindows(ctx)
		requireRoute(t, err, "SwayWindows->swayCall.RealInputSwayFailed")
	})
	t.Run("a reply that is not sway's", func(t *testing.T) {
		fakeSway(t, map[uint32]string{ipcGetOutputs: "garbage:"})
		_, err := SwayLayout(ctx)
		requireRoute(t, err, "SwayLayout->swayCall.RealInputSwayBadReply")
	})
	t.Run("a reply that is not JSON", func(t *testing.T) {
		fakeSway(t, map[uint32]string{ipcGetOutputs: "nope", ipcGetTree: "nope", ipcRunCommand: "nope"})
		_, err := SwayLayout(ctx)
		requireRoute(t, err, "SwayLayout.RealInputSwayBadReply")
		_, err = SwayWindows(ctx)
		requireRoute(t, err, "SwayWindows.RealInputSwayBadReply")
		requireRoute(t, SwayCommand(ctx, "focus"), "SwayCommand.RealInputSwayBadReply")
	})
	t.Run("no output", func(t *testing.T) {
		fakeSway(t, map[uint32]string{ipcGetOutputs: `[{"active":false,"rect":{}}]`})
		_, err := SwayLayout(ctx)
		requireRoute(t, err, "SwayLayout.RealInputSwayBadReply")
	})
	t.Run("a command that fails", func(t *testing.T) {
		fakeSway(t, map[uint32]string{ipcRunCommand: `[{"success":false,"error":"Unknown command"}]`})
		requireRoute(t, SwayCommand(ctx, "bogus"), "SwayCommand.RealInputSwayFailed")
	})
	t.Run("a window that never maps", func(t *testing.T) {
		fakeSway(t, map[uint32]string{ipcGetTree: swayTree})
		short, cancel := context.WithTimeout(ctx, 200_000_000)
		defer cancel()
		_, err := SwayWindowOf(short, 1)
		requireRoute(t, err, "SwayWindowOf.RealInputWindowNotFound")
	})
}
