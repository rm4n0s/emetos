package realinput

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

// fakeCompositor is a Wayland server that speaks just enough for the client: a registry with the seat and the two
// virtual device managers, wl_display.sync, the creation of the devices, and a record of every request the devices get.
type fakeCompositor struct {
	t        *testing.T
	listener *net.UnixListener
	socket   string
	// globals are the interfaces the registry announces; a test removes some to see the client fail.
	globals []string
	// failOnSync, when set, makes the server answer the first sync with a wl_display.error.
	failOnSync atomic.Bool

	mu sync.Mutex
	// shortEvent, when set, makes the next sync answer with an event shorter than a header.
	shortEvent atomic.Bool
	// hangUp, when set, makes the next sync close the connection with no answer.
	hangUp atomic.Bool
	// requests are what the devices received, in order.
	requests []fakeRequest
	// keymap is the text of the keymap the keyboard was given, and keymapSeals the seals of its file.
	keymap string
}

// fakeRequest is a request to a virtual device.
type fakeRequest struct {
	device string // "keyboard" or "pointer"
	name   string // "key", "modifiers", "motion_absolute", ...
	args   []uint32
}

var allGlobals = []string{"wl_seat", "zwp_virtual_keyboard_manager_v1", "zwlr_virtual_pointer_manager_v1"}

func newFakeCompositor(t *testing.T, globals []string) *fakeCompositor {
	t.Helper()
	dir, err := os.MkdirTemp("", "emetos-fakecomp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "wl")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	f := &fakeCompositor{t: t, listener: l, socket: socket, globals: globals}
	go f.serve()
	return f
}

func (f *fakeCompositor) serve() {
	for {
		conn, err := f.listener.AcceptUnix()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeCompositor) send(conn *net.UnixConn, obj uint32, opcode uint16, args []byte) {
	msg := make([]byte, 8+len(args))
	binary.LittleEndian.PutUint32(msg, obj)
	binary.LittleEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(opcode))
	copy(msg[8:], args)
	_, _ = conn.Write(msg)
}

// handle serves one client.
func (f *fakeCompositor) handle(conn *net.UnixConn) {
	defer func() { _ = conn.Close() }()
	kinds := map[uint32]string{1: "wl_display"}
	var registry uint32
	for {
		header := make([]byte, 8)
		oob := make([]byte, 64)
		n, oobn, _, _, err := conn.ReadMsgUnix(header, oob)
		if err != nil || n < 8 {
			return
		}
		fd := -1
		if oobn > 0 {
			if msgs, err := syscall.ParseSocketControlMessage(oob[:oobn]); err == nil && len(msgs) > 0 {
				if fds, err := syscall.ParseUnixRights(&msgs[0]); err == nil && len(fds) > 0 {
					fd = fds[0]
				}
			}
		}
		obj := binary.LittleEndian.Uint32(header)
		so := binary.LittleEndian.Uint32(header[4:])
		opcode, size := uint16(so), int(so>>16)
		args := make([]byte, size-8)
		if _, err := io.ReadFull(conn, args); err != nil {
			return
		}
		word := func(i int) uint32 { return binary.LittleEndian.Uint32(args[4*i:]) }
		switch kinds[obj] {
		case "wl_display":
			switch opcode {
			case 1: // get_registry
				registry = word(0)
				kinds[registry] = "wl_registry"
				for i, iface := range f.globals {
					f.send(conn, registry, 0, append(append(u32(uint32(i+1)), str(iface)...), u32(2)...))
				}
			case 0: // sync
				if f.hangUp.Swap(false) {
					return
				}
				if f.failOnSync.Swap(false) {
					f.send(conn, 1, 0, append(append(u32(1), u32(7)...), str("fake protocol error")...))
					continue
				}
				if f.shortEvent.Swap(false) {
					// an event that says it is 4 bytes long
					msg := make([]byte, 8)
					binary.LittleEndian.PutUint32(msg, 1)
					binary.LittleEndian.PutUint32(msg[4:], 4<<16)
					_, _ = conn.Write(msg)
					continue
				}
				f.send(conn, word(0), 0, u32(0))
			}
		case "wl_registry":
			// bind(name, interface, version, id)
			name := word(0)
			iface, off, _ := argString(args, 4)
			id := binary.LittleEndian.Uint32(args[off+4:])
			if int(name) <= len(f.globals) && f.globals[name-1] == iface {
				kinds[id] = iface
			}
		case "zwp_virtual_keyboard_manager_v1":
			kinds[word(1)] = "keyboard"
		case "zwlr_virtual_pointer_manager_v1":
			kinds[word(1)] = "pointer"
		case "keyboard":
			names := map[uint16]string{0: "keymap", 1: "key", 2: "modifiers"}
			f.record("keyboard", names[opcode], args)
			if opcode == 0 && fd >= 0 {
				data := make([]byte, word(1))
				if _, err := syscall.Pread(fd, data, 0); err == nil {
					f.mu.Lock()
					f.keymap = string(data)
					f.mu.Unlock()
				}
				_ = syscall.Close(fd)
			}
		case "pointer":
			names := map[uint16]string{0: "motion", 1: "motion_absolute", 2: "button", 3: "axis", 4: "frame", 5: "axis_source", 7: "axis_discrete"}
			f.record("pointer", names[opcode], args)
		}
	}
}

func (f *fakeCompositor) record(device, name string, args []byte) {
	r := fakeRequest{device: device, name: name}
	for i := 0; i+4 <= len(args); i += 4 {
		r.args = append(r.args, binary.LittleEndian.Uint32(args[i:]))
	}
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()
}

// taken returns the requests received so far, without the keymap, and forgets them.
func (f *fakeCompositor) taken() []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeRequest
	for _, r := range f.requests {
		if r.name != "keymap" {
			out = append(out, r)
		}
	}
	f.requests = nil
	return out
}
