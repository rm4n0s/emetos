package realinput

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// The Wayland wire format: a message is the id of its object, a word with its size in bytes (header included) in the high
// half and its opcode in the low half, and its arguments, each padded to four bytes. File descriptors travel as ancillary
// data of the socket.

// wireConn is the connection to the compositor. It is not safe for concurrent use: the Client holds its lock around every
// call.
type wireConn struct {
	conn *net.UnixConn
	// next is the id of the next object this client makes (the display is 1).
	next uint32
	// timeout bounds each read and write.
	timeout time.Duration
}

// u32 is a 32-bit argument.
func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// fixed is a signed 24.8 fixed-point argument.
func fixed(v float64) []byte { return u32(uint32(int32(v * 256))) }

// str is a string argument: its length with the terminating zero, then the bytes, padded to four.
func str(s string) []byte {
	n := len(s) + 1
	b := u32(uint32(n))
	padded := make([]byte, (n+3)&^3)
	copy(padded, s)
	return append(b, padded...)
}

// cat joins arguments.
func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// newID gives a new object id.
func (w *wireConn) newID() uint32 {
	id := w.next
	w.next++
	return id
}

// encode makes the bytes of one request.
func encode(obj uint32, opcode uint16, args []byte) []byte {
	msg := make([]byte, 8+len(args))
	binary.LittleEndian.PutUint32(msg[0:], obj)
	binary.LittleEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(opcode))
	copy(msg[8:], args)
	return msg
}

// send writes one request. fd, when it is not negative, is sent along with it.
func (w *wireConn) send(obj uint32, opcode uint16, args []byte, fd int) error {
	return w.write(encode(obj, opcode, args), fd, obj, opcode)
}

// sendBatch writes several requests with one write, so that the compositor reads them together.
func (w *wireConn) sendBatch(requests [][]byte) error {
	return w.write(cat(requests...), -1, 0, 0)
}

// write writes bytes to the socket, with a file descriptor when fd is not negative; obj and opcode name the request in an error.
func (w *wireConn) write(msg []byte, fd int, obj uint32, opcode uint16) error {
	var oob []byte
	if fd >= 0 {
		oob = syscall.UnixRights(fd)
	}
	if err := w.conn.SetWriteDeadline(time.Now().Add(w.timeout)); err != nil {
		return errors.NewErr("RealInputWriteFailed", err).SetMetadata("object", obj, "opcode", opcode)
	}
	if _, _, err := w.conn.WriteMsgUnix(msg, oob, nil); err != nil {
		return errors.NewErr("RealInputWriteFailed", err).SetMetadata("object", obj, "opcode", opcode)
	}
	return nil
}

// event is one message from the compositor.
type event struct {
	obj    uint32
	opcode uint16
	args   []byte
}

// read reads one event, giving up when ctx ends or the timeout passes.
func (w *wireConn) read(ctx context.Context) (event, error) {
	deadline := time.Now().Add(w.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := w.conn.SetReadDeadline(deadline); err != nil {
		return event{}, errors.NewErr("RealInputReadFailed", err)
	}
	// a canceled context closes the wait at once
	// the function holds the connection itself: Close sets w.conn to nil, possibly while the function is about to run
	conn := w.conn
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Unix(1, 0)) })
	defer stop()
	header := make([]byte, 8)
	if _, err := io.ReadFull(w.conn, header); err != nil {
		return event{}, errors.NewErr("RealInputReadFailed", err).SetMetadata("while", "the header of an event")
	}
	sizeOpcode := binary.LittleEndian.Uint32(header[4:])
	size := int(sizeOpcode >> 16)
	if size < 8 {
		return event{}, errors.New("RealInputReadFailed", "the compositor sent an event shorter than its header", "size", size)
	}
	ev := event{obj: binary.LittleEndian.Uint32(header), opcode: uint16(sizeOpcode), args: make([]byte, size-8)}
	if _, err := io.ReadFull(w.conn, ev.args); err != nil {
		return event{}, errors.NewErr("RealInputReadFailed", err).SetMetadata("while", "the arguments of an event", "object", ev.obj)
	}
	return ev, nil
}

// argString reads a string argument at an offset, and gives the offset after it.
func argString(args []byte, off int) (string, int, bool) {
	if off+4 > len(args) {
		return "", off, false
	}
	n := int(binary.LittleEndian.Uint32(args[off:]))
	if n < 1 || off+4+n > len(args) {
		return "", off, false
	}
	s := string(args[off+4 : off+4+n-1])
	return s, off + 4 + ((n + 3) &^ 3), true
}
