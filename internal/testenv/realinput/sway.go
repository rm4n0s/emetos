package realinput

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"time"

	"github.com/rm4n0s/errors"
)

// sway's IPC is i3's: a message is the magic "i3-ipc", the length of the payload and the message type (both 32 bits, in the
// byte order of the machine), then the payload; the reply has the same form.
const (
	ipcMagic      = "i3-ipc"
	ipcRunCommand = 0
	ipcGetOutputs = 3
	ipcGetTree    = 4
)

// swayCall sends one message to sway's IPC socket ($SWAYSOCK) and returns the payload of the reply. The errors have the
// tags RealInputSwayFailed (no socket, or the exchange failed) and RealInputSwayBadReply.
func swayCall(ctx context.Context, messageType uint32, payload string) ([]byte, error) {
	socket := os.Getenv("SWAYSOCK")
	if socket == "" {
		return nil, errors.New("RealInputSwayFailed", "SWAYSOCK is not set: the compositor is not sway")
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, errors.NewErr("RealInputSwayFailed", err).SetMetadata("socket", socket)
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, errors.NewErr("RealInputSwayFailed", err).SetMetadata("socket", socket)
	}
	msg := make([]byte, 0, 14+len(payload))
	msg = append(msg, ipcMagic...)
	msg = binary.NativeEndian.AppendUint32(msg, uint32(len(payload)))
	msg = binary.NativeEndian.AppendUint32(msg, messageType)
	msg = append(msg, payload...)
	if _, err := conn.Write(msg); err != nil {
		return nil, errors.NewErr("RealInputSwayFailed", err).SetMetadata("socket", socket)
	}
	header := make([]byte, 14)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, errors.NewErr("RealInputSwayFailed", err).SetMetadata("socket", socket)
	}
	if string(header[:6]) != ipcMagic {
		return nil, errors.New("RealInputSwayBadReply", "the reply of sway does not start with the i3-ipc magic", "socket", socket)
	}
	reply := make([]byte, binary.NativeEndian.Uint32(header[6:]))
	if _, err := io.ReadFull(conn, reply); err != nil {
		return nil, errors.NewErr("RealInputSwayFailed", err).SetMetadata("socket", socket)
	}
	return reply, nil
}

// SwayCommand runs a command of sway (what swaymsg would be given) and returns an error with the tag RealInputSwayFailed
// when sway says it failed.
func SwayCommand(ctx context.Context, command string) error {
	reply, err := swayCall(ctx, ipcRunCommand, command)
	if err != nil {
		return err
	}
	var results []struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(reply, &results); err != nil {
		return errors.NewErr("RealInputSwayBadReply", err).SetMetadata("command", command)
	}
	for _, r := range results {
		if !r.Success {
			return errors.New("RealInputSwayFailed", "sway could not run the command", "command", command, "error", r.Error)
		}
	}
	return nil
}

// SwayLayout is the bounding box of the active outputs of sway, in layout pixels: what the absolute positions of the
// virtual pointer are counted against.
func SwayLayout(ctx context.Context) (Rect, error) {
	reply, err := swayCall(ctx, ipcGetOutputs, "")
	if err != nil {
		return Rect{}, err
	}
	var outputs []struct {
		Active bool `json:"active"`
		Rect   struct {
			X, Y, Width, Height float64
		} `json:"rect"`
	}
	if err := json.Unmarshal(reply, &outputs); err != nil {
		return Rect{}, errors.NewErr("RealInputSwayBadReply", err)
	}
	var minX, minY, maxX, maxY float64
	found := false
	for _, o := range outputs {
		if !o.Active {
			continue
		}
		r := o.Rect
		if !found {
			minX, minY, maxX, maxY, found = r.X, r.Y, r.X+r.Width, r.Y+r.Height, true
			continue
		}
		minX, minY = min(minX, r.X), min(minY, r.Y)
		maxX, maxY = max(maxX, r.X+r.Width), max(maxY, r.Y+r.Height)
	}
	if !found {
		return Rect{}, errors.New("RealInputSwayBadReply", "sway has no active output")
	}
	return Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}, nil
}

// SwayWindow is a window of sway.
type SwayWindow struct {
	// PID is the process that owns the window.
	PID int
	// AppID and Title are the application id and the title.
	AppID, Title string
	// Outer is the rectangle of the window with sway's decoration, in layout pixels.
	Outer Rect
	// Content is the rectangle of the window's own surface in layout pixels: window coordinates count from its top left.
	Content Rect
	// Focused tells whether the window has the keyboard focus.
	Focused bool
}

// swayNode is a node of the tree of sway.
type swayNode struct {
	PID        int    `json:"pid"`
	AppID      string `json:"app_id"`
	Name       string `json:"name"`
	Focused    bool   `json:"focused"`
	Rect       swayRect
	WindowRect swayRect    `json:"window_rect"`
	Nodes      []*swayNode `json:"nodes"`
	Floating   []*swayNode `json:"floating_nodes"`
}

type swayRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// SwayWindows lists the windows of sway (the leaves of its tree that belong to a process).
func SwayWindows(ctx context.Context) ([]SwayWindow, error) {
	reply, err := swayCall(ctx, ipcGetTree, "")
	if err != nil {
		return nil, err
	}
	var root swayNode
	if err := json.Unmarshal(reply, &root); err != nil {
		return nil, errors.NewErr("RealInputSwayBadReply", err)
	}
	var out []SwayWindow
	var walk func(n *swayNode)
	walk = func(n *swayNode) {
		if len(n.Nodes) == 0 && len(n.Floating) == 0 && n.PID > 0 {
			out = append(out, SwayWindow{
				PID: n.PID, AppID: n.AppID, Title: n.Name, Focused: n.Focused,
				Outer:   Rect{n.Rect.X, n.Rect.Y, n.Rect.Width, n.Rect.Height},
				Content: Rect{n.Rect.X + n.WindowRect.X, n.Rect.Y + n.WindowRect.Y, n.WindowRect.Width, n.WindowRect.Height},
			})
		}
		for _, c := range n.Nodes {
			walk(c)
		}
		for _, c := range n.Floating {
			walk(c)
		}
	}
	walk(&root)
	return out, nil
}

// SwayWindowOf waits for the window of a process to be mapped, up to the deadline of ctx, and returns it. The error has the
// tag RealInputWindowNotFound when ctx ends first.
func SwayWindowOf(ctx context.Context, pid int) (SwayWindow, error) {
	for {
		windows, err := SwayWindows(ctx)
		if err != nil {
			return SwayWindow{}, err
		}
		for _, w := range windows {
			if w.PID == pid && w.Content.Width > 0 {
				return w, nil
			}
		}
		select {
		case <-ctx.Done():
			return SwayWindow{}, errors.NewErr("RealInputWindowNotFound", ctx.Err()).SetMetadata("pid", pid)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
