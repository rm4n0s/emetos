package keyqr

import (
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// fakeDriver is a camera in memory: the kernel's side of V4L2 as the driver interface has it. Frames are given with give; they
// are put in a buffer the capture has queued each time Wait is called, as a camera does.
type fakeDriver struct {
	mu sync.Mutex

	caps     capability
	formats  []uint32
	settle   func(pixelFormat uint32, w, h int) format // what SetFormat answers; the format asked for at 640x480 when nil
	buffers  int                                       // how many buffers RequestBuffers grants (4 when 0)
	bufLen   int                                       // the size of a buffer (1 MB when 0)
	errs     map[string]error                          // an error for a method, by its name
	perWait  int                                       // how many frames one Wait produces (1 when 0)
	waitTime time.Duration                             // how long a Wait with no frame sleeps (the timeout when 0)

	calls    []string
	asked    []format // what SetFormat was asked: pixel format, width, height
	bufs     [][]byte
	queued   []int // the buffers the camera may fill, in the order they were queued
	filled   []dequeued
	script   [][]byte // frames to come
	damage   int      // the next frames the camera makes carry the error flag
	closed   int
	streamOn bool
	maxQueue int
	dequeues int
}

func newFakeDriver(formats ...uint32) *fakeDriver {
	return &fakeDriver{caps: capability{Card: "Fake Camera", Capture: true, Streaming: true}, formats: formats, errs: map[string]error{}}
}

// give adds a frame to the script.
func (f *fakeDriver) give(frames ...[]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = append(f.script, frames...)
}

func (f *fakeDriver) record(name string) error {
	f.calls = append(f.calls, name)
	return f.errs[name]
}

func (f *fakeDriver) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeDriver) count(name string) int {
	n := 0
	for _, c := range f.callLog() {
		if c == name {
			n++
		}
	}
	return n
}

func (f *fakeDriver) QueryCap() (capability, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.caps, f.record("QueryCap")
}

func (f *fakeDriver) Formats() ([]uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Formats"); err != nil {
		return nil, err
	}
	return f.formats, nil
}

func (f *fakeDriver) SetFormat(pf uint32, w, h int) (format, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, format{PixelFormat: pf, Width: w, Height: h})
	if err := f.record("SetFormat"); err != nil {
		return format{}, err
	}
	if f.settle != nil {
		return f.settle(pf, w, h), nil
	}
	return format{PixelFormat: pf, Width: 640, Height: 480, BytesPerLine: 640 * 2, SizeImage: 640 * 480 * 2}, nil
}

func (f *fakeDriver) RequestBuffers(n int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("RequestBuffers"); err != nil {
		return 0, err
	}
	if f.buffers == 0 {
		f.buffers = n
	}
	return f.buffers, nil
}

func (f *fakeDriver) MapBuffer(i int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("MapBuffer"); err != nil {
		return nil, err
	}
	n := f.bufLen
	if n == 0 {
		n = 1 << 20
	}
	for len(f.bufs) <= i {
		f.bufs = append(f.bufs, make([]byte, n))
	}
	return f.bufs[i], nil
}

func (f *fakeDriver) Queue(i int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Queue"); err != nil {
		return err
	}
	f.queued = append(f.queued, i)
	f.maxQueue = max(f.maxQueue, len(f.queued))
	return nil
}

func (f *fakeDriver) Dequeue() (dequeued, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("Dequeue"); err != nil {
		return dequeued{}, err
	}
	if len(f.filled) == 0 {
		return dequeued{}, unix.EAGAIN
	}
	d := f.filled[0]
	f.filled = f.filled[1:]
	f.dequeues++
	return d, nil
}

func (f *fakeDriver) StreamOn() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.record("StreamOn")
	f.streamOn = err == nil
	return err
}

func (f *fakeDriver) StreamOff() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streamOn = false
	return f.record("StreamOff")
}

// Wait fills the buffers the capture queued with the next frames of the script, and says whether there is a frame.
func (f *fakeDriver) Wait(timeout time.Duration) (bool, error) {
	f.mu.Lock()
	if err := f.record("Wait"); err != nil {
		f.mu.Unlock()
		return false, err
	}
	n := f.perWait
	if n == 0 {
		n = 1
	}
	for ; n > 0 && len(f.script) > 0 && len(f.queued) > 0; n-- {
		i := f.queued[0]
		f.queued = f.queued[1:]
		frame := f.script[0]
		f.script = f.script[1:]
		copy(f.bufs[i], frame)
		f.filled = append(f.filled, dequeued{Index: i, BytesUsed: len(frame), Damaged: f.damage > 0})
		f.damage = max(f.damage-1, 0)
	}
	ready := len(f.filled) > 0
	f.mu.Unlock()
	if !ready {
		d := f.waitTime
		if d == 0 {
			d = timeout
		}
		time.Sleep(d)
	}
	return ready, nil
}

func (f *fakeDriver) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return f.record("Close")
}
