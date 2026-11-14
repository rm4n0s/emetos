package mediapreview

import (
	"context"
	"io"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
)

// ---- the fakes of the player ----

// fakeClock is time the test moves: its timers run on the test's goroutine, in the order they are due, when it advances.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	fn      func()
	stopped bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, fn func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), fn: fn}
	c.timers = append(c.timers, t)
	return func() {
		c.mu.Lock()
		t.stopped = true
		c.mu.Unlock()
	}
}

// Jump moves the time on at once, as a busy UI thread does, then runs the timers that are due.
func (c *fakeClock) Jump(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	c.Advance(0)
}

// Advance moves the time on, running the timers that come due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	end := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.at.After(end) && (next == nil || t.at.Before(next.at)) {
				next = t
			}
		}
		if next == nil {
			c.now = end
			c.timers = slices.DeleteFunc(c.timers, func(t *fakeTimer) bool { return t.stopped })
			c.mu.Unlock()
			return
		}
		next.stopped = true
		if next.at.After(c.now) {
			c.now = next.at
		}
		c.mu.Unlock()
		next.fn()
	}
}

// fakeMedia decodes a video of a duration it computes: the frames are eight color bars, frame i of a run with the byte i+1 as the red of
// its first pixel, and the sound is silence.
type fakeMedia struct {
	mu       sync.Mutex
	duration time.Duration
	// frameErr and soundErr are the errors the readers end with, at once.
	frameErr, soundErr   error
	frameRuns, soundRuns []time.Duration
	live                 int
}

func (m *fakeMedia) Frames(ctx context.Context, opts ffmpeg.FrameOptions) (frameSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.frameRuns = append(m.frameRuns, opts.From)
	m.live++
	n := int(math.Floor((m.duration-opts.From).Seconds()*opts.Rate + 1e-9))
	return &fakeFrames{m: m, ctx: ctx, width: opts.Width, size: opts.Width * opts.Height * 4, left: max(n, 0), err: m.frameErr}, nil
}

func (m *fakeMedia) Sound(ctx context.Context, from time.Duration) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.soundRuns = append(m.soundRuns, from)
	m.live++
	n := int(math.Round((m.duration-from).Seconds()*ffmpeg.SoundRate)) * ffmpeg.SoundChannels * 4
	return &fakeSound{m: m, ctx: ctx, left: max(n, 0), err: m.soundErr}, nil
}

func (m *fakeMedia) runs() ([]time.Duration, []time.Duration, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.frameRuns), slices.Clone(m.soundRuns), m.live
}

func (m *fakeMedia) closed() {
	m.mu.Lock()
	m.live--
	m.mu.Unlock()
}

type fakeFrames struct {
	m          *fakeMedia
	ctx        context.Context
	width      int
	size, i    int
	left       int
	err        error
	closedOnce sync.Once
}

func (f *fakeFrames) FrameSize() int { return f.size }

func (f *fakeFrames) Next(buf []byte) (bool, error) {
	if f.ctx.Err() != nil {
		return false, errors.NewErr("FFmpegStopped", f.ctx.Err())
	}
	if f.err != nil {
		return false, f.err
	}
	if f.left == 0 {
		return false, nil
	}
	f.left--
	f.i++
	bars := [][3]byte{{235, 235, 235}, {235, 235, 16}, {16, 235, 235}, {16, 235, 16}, {235, 16, 235}, {235, 16, 16}, {16, 16, 235}, {16, 16, 16}}
	for px := 0; px < f.size/4; px++ {
		c := bars[(px%f.width)*len(bars)/f.width]
		buf[4*px], buf[4*px+1], buf[4*px+2], buf[4*px+3] = c[0], c[1], c[2], 0xff
	}
	buf[0] = byte(f.i)
	return true, nil
}

func (f *fakeFrames) Close() error { f.closedOnce.Do(f.m.closed); return nil }

type fakeSound struct {
	m          *fakeMedia
	ctx        context.Context
	left       int
	err        error
	closedOnce sync.Once
}

func (s *fakeSound) Read(b []byte) (int, error) {
	if s.ctx.Err() != nil {
		return 0, errors.NewErr("FFmpegStopped", s.ctx.Err())
	}
	if s.err != nil {
		return 0, s.err
	}
	if s.left == 0 {
		return 0, io.EOF
	}
	n := min(len(b), s.left)
	clear(b[:n])
	s.left -= n
	return n, nil
}

func (s *fakeSound) Close() error { s.closedOnce.Do(s.m.closed); return nil }

// fakeSink is a sound device the test plays at its own pace (Consume).
type fakeSink struct {
	mu             sync.Mutex
	queued         int // frames
	gain           float64
	paused, closed bool
	clears         int
}

func (s *fakeSink) Put(samples []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queued += len(samples) / ffmpeg.SoundChannels
	return nil
}

func (s *fakeSink) Queued() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Duration(float64(s.queued) / ffmpeg.SoundRate * float64(time.Second))
}

func (s *fakeSink) SetGain(g float64) { s.mu.Lock(); s.gain = g; s.mu.Unlock() }
func (s *fakeSink) Clear()            { s.mu.Lock(); s.queued = 0; s.clears++; s.mu.Unlock() }
func (s *fakeSink) Pause()            { s.mu.Lock(); s.paused = true; s.mu.Unlock() }
func (s *fakeSink) Resume()           { s.mu.Lock(); s.paused = false; s.mu.Unlock() }
func (s *fakeSink) Close()            { s.mu.Lock(); s.closed = true; s.mu.Unlock() }

// Consume plays d of what is queued, unless the device is paused.
func (s *fakeSink) Consume(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.paused {
		s.queued = max(0, s.queued-int(d.Seconds()*ffmpeg.SoundRate))
	}
}

func (s *fakeSink) state() (float64, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gain, s.paused, s.closed
}

// playerRig is a player over the fakes; post queues work for the UI thread, which the test runs with pump.
type playerRig struct {
	t      *testing.T
	p      *player
	media  *fakeMedia
	sink   *fakeSink
	clock  *fakeClock
	mu     sync.Mutex
	posted []func()
}

func newPlayerRig(t *testing.T, info ffmpeg.Info, withSound bool) *playerRig {
	r := &playerRig{t: t, media: &fakeMedia{duration: info.Duration}, sink: &fakeSink{gain: 1}, clock: newFakeClock()}
	var newSink func() (sink, error)
	if withSound {
		newSink = func() (sink, error) { return r.sink, nil }
	}
	r.p = newPlayer(playerOptions{info: info, media: r.media, newSink: newSink, clock: r.clock, width: 4, height: 2,
		post: func(fn func()) {
			r.mu.Lock()
			r.posted = append(r.posted, fn)
			r.mu.Unlock()
		},
		changed: func() {},
	})
	t.Cleanup(r.p.Close)
	return r
}

// pump runs what was posted.
func (r *playerRig) pump() {
	for {
		r.mu.Lock()
		if len(r.posted) == 0 {
			r.mu.Unlock()
			return
		}
		fn := r.posted[0]
		r.posted = r.posted[1:]
		r.mu.Unlock()
		fn()
	}
}

// waitFor waits (in real time: the decoding goroutines are real) until cond holds, pumping meanwhile.
func (r *playerRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		r.pump()
		time.Sleep(time.Millisecond)
	}
}

// filled waits until the queues of the run are full: the frames, and a second of sound.
func (r *playerRig) filled() {
	r.t.Helper()
	r.waitFor("the queues to fill", func() bool {
		run := r.p.run
		if run == nil {
			return true
		}
		framesFull := !r.p.o.info.HasVideo || len(run.frames) == cap(run.frames) || run.framesDone.Load()
		soundFull := r.p.sink == nil || r.sink.Queued() > maxQueuedSound-50*time.Millisecond || run.soundDone.Load()
		return framesFull && soundFull
	})
}

// step plays d: the device plays d of sound and the clock moves d.
func (r *playerRig) step(d time.Duration) {
	r.sink.Consume(d)
	r.clock.Advance(d)
}

// frameIndex is the number in the frame shown (the n-th frame of its run, from 1).
func (r *playerRig) frameIndex() int {
	if r.p.frame == nil {
		return 0
	}
	data, err := r.p.frame.ToByteData(0)
	if err != nil {
		r.t.Fatal(err)
	}
	return int(data[0])
}

var testInfo = ffmpeg.Info{Duration: 2 * time.Second, HasVideo: true, HasAudio: true, Width: 4, Height: 2, Rate: 10, VideoCodec: "h264", AudioCodec: "aac"}

// ---- the tests ----

func TestTheFramesFollowThePositionOfTheSound(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.p.Play()
	r.filled()
	if r.p.Position() != 0 {
		t.Fatalf("position %v before any sound played", r.p.Position())
	}
	// the clock alone moves nothing: the sound was not played
	r.clock.Advance(300 * time.Millisecond)
	if r.p.Position() != 0 || r.p.frameAt != 0 {
		t.Errorf("without the sound: position %v, frame at %v", r.p.Position(), r.p.frameAt)
	}
	for range 5 {
		r.step(100 * time.Millisecond)
		r.filled()
	}
	if got := r.p.Position(); got < 490*time.Millisecond || got > 510*time.Millisecond {
		t.Errorf("position %v after half a second of sound", got)
	}
	if r.p.frameAt != 500*time.Millisecond || r.frameIndex() != 6 {
		t.Errorf("frame at %v (number %d)", r.p.frameAt, r.frameIndex())
	}
	if r.p.dropped != 0 {
		t.Errorf("%d frames dropped", r.p.dropped)
	}
}

func TestLateFramesAreDropped(t *testing.T) {
	info := testInfo
	info.HasAudio = false // the monotonic clock
	r := newPlayerRig(t, info, true)
	r.p.Play()
	r.filled()
	// the first look at the clock comes 450 ms late: frame 0 is shown (nothing was), 100 to 300 ms are late by more than a frame,
	// 400 ms is shown
	r.clock.Jump(450 * time.Millisecond)
	if r.p.frameAt != 400*time.Millisecond || r.p.dropped != 3 {
		t.Errorf("frame at %v, %d dropped", r.p.frameAt, r.p.dropped)
	}
}

func TestPauseHoldsTheFrameAndTheProcesses(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.p.Play()
	r.filled()
	for range 3 {
		r.step(100 * time.Millisecond)
	}
	r.p.Pause()
	at, pos := r.p.frameAt, r.p.Position()
	if _, paused, _ := r.sink.state(); !paused {
		t.Error("the device plays on")
	}
	r.step(time.Second) // the device is paused: nothing plays
	if r.p.Position() != pos || r.p.frameAt != at || r.p.playing {
		t.Errorf("paused: position %v (was %v), frame at %v", r.p.Position(), pos, r.p.frameAt)
	}
	r.p.Play()
	r.step(100 * time.Millisecond)
	frames, sound, live := r.media.runs()
	if len(frames) != 1 || len(sound) != 1 || live != 2 {
		t.Errorf("runs %v %v, %d processes: a pause ended them", frames, sound, live)
	}
	if got := r.p.Position(); got < pos+90*time.Millisecond || got > pos+110*time.Millisecond {
		t.Errorf("after the pause: position %v, was %v", got, pos)
	}
}

func TestASeekRestartsBothAtTheTime(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.p.Play()
	r.filled()
	r.step(200 * time.Millisecond)
	shown := r.p.frame
	r.p.Seek(1500 * time.Millisecond)
	// the last frame stays until the new run's first one, which may be there already (the fake decodes at once)
	if r.p.frame != shown && r.p.frameAt != 1500*time.Millisecond {
		t.Errorf("the frame at %v is shown after the seek", r.p.frameAt)
	}
	r.waitFor("the new run, and the old one to end", func() bool {
		frames, sound, live := r.media.runs()
		return len(frames) == 2 && len(sound) == 2 && live <= 2 // the new run's is short: it may have ended already
	})
	frames, sound, _ := r.media.runs()
	if !slices.Equal(frames, []time.Duration{0, 1500 * time.Millisecond}) || !slices.Equal(sound, []time.Duration{0, 1500 * time.Millisecond}) {
		t.Errorf("runs %v %v", frames, sound)
	}
	r.filled()
	r.step(100 * time.Millisecond)
	if r.p.frameAt != 1600*time.Millisecond {
		t.Errorf("frame at %v after the seek", r.p.frameAt)
	}
	// paused, a seek shows the frame at the time
	r.p.Pause()
	r.p.Seek(700 * time.Millisecond)
	r.waitFor("the frame at the time", func() bool { return r.p.frameAt == 700*time.Millisecond })
	if r.p.Position() != 700*time.Millisecond || r.p.playing {
		t.Errorf("position %v, playing %v", r.p.Position(), r.p.playing)
	}
	// out of the video: held to it
	r.p.Seek(-time.Second)
	if r.p.Position() != 0 {
		t.Errorf("before the start: %v", r.p.Position())
	}
	r.p.Seek(time.Hour)
	if r.p.Position() != 2*time.Second {
		t.Errorf("past the end: %v", r.p.Position())
	}
}

func TestTheEndAndLoop(t *testing.T) {
	info := ffmpeg.Info{Duration: 500 * time.Millisecond, HasVideo: true, Width: 4, Height: 2, Rate: 10}
	r := newPlayerRig(t, info, false)
	r.p.Play()
	for range 10 {
		r.filled()
		r.clock.Advance(100 * time.Millisecond)
	}
	if r.p.playing || !r.p.ended || r.p.Position() != 500*time.Millisecond {
		t.Errorf("at the end: playing %v, ended %v, position %v", r.p.playing, r.p.ended, r.p.Position())
	}
	// Play after the end starts again
	r.p.Play()
	if r.p.Position() != 0 || !r.p.playing {
		t.Errorf("play after the end: %v", r.p.Position())
	}
	r.p.Pause()
	r2 := newPlayerRig(t, info, false)
	r2.p.SetLoop(true)
	r2.p.Play()
	for range 10 {
		r2.filled()
		r2.clock.Advance(100 * time.Millisecond)
	}
	frames, _, _ := r2.media.runs()
	if !r2.p.playing || len(frames) < 2 || frames[1] != 0 {
		t.Errorf("loop: playing %v, runs %v", r2.p.playing, frames)
	}
}

func TestTheSoundEndsAVideoWithSound(t *testing.T) {
	info := testInfo
	info.Duration = 300 * time.Millisecond
	r := newPlayerRig(t, info, true)
	r.p.Play()
	for range 6 {
		r.filled()
		r.step(100 * time.Millisecond)
	}
	if r.p.playing || !r.p.ended {
		t.Errorf("playing %v, ended %v", r.p.playing, r.p.ended)
	}
}

func TestMuteAndVolumeReachTheSound(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.p.SetVolume(0.5)
	r.p.Play()
	if g, _, _ := r.sink.state(); g != 0.5 {
		t.Errorf("gain %v", g)
	}
	r.p.SetVolume(2)
	if g, _, _ := r.sink.state(); g != 1 || r.p.volume != 1 {
		t.Errorf("volume above 1: gain %v", g)
	}
	r.p.SetMuted(true)
	if g, _, _ := r.sink.state(); g != 0 {
		t.Errorf("muted: gain %v", g)
	}
	r.p.SetMuted(false)
	r.p.SetForceMute(true)
	if g, _, _ := r.sink.state(); g != 0 {
		t.Errorf("a client's view: gain %v", g)
	}
	r.p.SetForceMute(false)
	if g, _, _ := r.sink.state(); g != 1 {
		t.Errorf("back on the host: gain %v", g)
	}
}

func TestClosingEndsTheProcessesAndTheDevice(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.p.Play()
	r.filled()
	r.p.Close()
	r.waitFor("the processes to end", func() bool { _, _, live := r.media.runs(); return live == 0 })
	if _, _, closed := r.sink.state(); !closed {
		t.Error("the device is open")
	}
}

func TestACodecFFmpegCannotDecodeStopsThePlayer(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.media.frameErr = errors.New("FFmpegCodecUnsupported", "This FFmpeg cannot decode h264.", "codec", "h264")
	r.p.Play()
	r.waitFor("the error", func() bool {
		r.clock.Advance(tickTime)
		return r.p.err != nil
	})
	if r.p.playing {
		t.Error("it plays on")
	}
	text, docs, needs := problemText(r.p.err, "Playing videos")
	if text != "This FFmpeg cannot decode h264. Fedora's ffmpeg-free leaves it out; see the instructions." || docs != rpmFusionDocs || needs {
		t.Errorf("text %q docs %q needs %v", text, docs, needs)
	}
	// the sound's error too
	r2 := newPlayerRig(t, testInfo, true)
	r2.media.soundErr = errors.New("FFmpegCodecUnsupported", "This FFmpeg cannot decode opus.", "codec", "opus")
	r2.p.Play()
	r2.waitFor("the error", func() bool {
		r2.clock.Advance(tickTime)
		return r2.p.err != nil
	})
	if text, docs, _ := problemText(r2.p.err, "Playing videos"); text != "This FFmpeg cannot decode opus." || docs != installDocs {
		t.Errorf("text %q docs %q", text, docs)
	}
}

func TestAStillFrame(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.p.Seek(time.Second)
	r.waitFor("the still frame", func() bool { return r.p.frame != nil })
	if r.p.frameAt != time.Second || r.frameIndex() != 1 {
		t.Errorf("frame at %v, number %d", r.p.frameAt, r.frameIndex())
	}
	frames, sound, _ := r.media.runs()
	if !slices.Equal(frames, []time.Duration{time.Second}) || len(sound) != 0 {
		t.Errorf("runs %v %v", frames, sound)
	}
}

func TestDecodeSize(t *testing.T) {
	info := ffmpeg.Info{Width: 1920, Height: 1080}
	for _, c := range []struct {
		w, h         float64
		wantW, wantH int
	}{{960, 1000, 960, 540}, {4000, 3000, 1920, 1080}, {0, 0, 1920, 1080}, {1000, 270, 480, 270}} {
		if w, h := decodeSize(info, c.w, c.h); w != c.wantW || h != c.wantH {
			t.Errorf("decodeSize(%v, %v) = %d x %d", c.w, c.h, w, h)
		}
	}
	if w, h := decodeSize(ffmpeg.Info{}, 100, 100); w != 2 || h != 2 {
		t.Errorf("no size: %d x %d", w, h)
	}
}

func TestFormatTime(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{{0, "0:00"}, {42 * time.Second, "0:42"}, {187 * time.Second, "3:07"}, {3723 * time.Second, "1:02:03"}, {-time.Second, "0:00"}} {
		if got := formatTime(c.d); got != c.want {
			t.Errorf("formatTime(%v) = %q", c.d, got)
		}
	}
}

func TestAnAVIFThroughFFmpeg(t *testing.T) {
	path := writeFile(t, "a.avif", []byte("not read: FFmpeg reads it"))
	m := &fakeMedia{duration: time.Second}
	video := func(context.Context) (ffmpeg.Info, media, error) {
		return ffmpeg.Info{HasVideo: true, Width: 4, Height: 2, Rate: 1}, m, nil
	}
	img, err := Decode(t.Context(), path, DecodeOptions{Video: video})
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 4 || img.Height != 2 || img.Format != "avif" || len(img.Frames) != 1 {
		t.Errorf("image %dx%d %s", img.Width, img.Height, img.Format)
	}
	if got := pixel(t, img, 0, 0); got.R != 1 || got.A != 0xff {
		t.Errorf("pixel %v", got)
	}
	// no FFmpeg: the error of finding it
	missing := errors.New("FFmpegNotFound", "ffmpeg was not found in PATH, /opt/homebrew/bin or /usr/local/bin")
	_, err = Decode(t.Context(), writeFile(t, "b.heic", []byte("x")), DecodeOptions{Video: func(context.Context) (ffmpeg.Info, media, error) {
		return ffmpeg.Info{}, nil, missing
	}})
	if !errors.Is(err, missing) && err != missing {
		t.Errorf("err %v", err)
	}
	if text, _, needs := problemText(err, "Opening HEIC images"); !needs || text != "Opening HEIC images needs FFmpeg. ffmpeg was not found in PATH, /opt/homebrew/bin or /usr/local/bin." {
		t.Errorf("text %q", text)
	}
	_, err = Decode(t.Context(), writeFile(t, "c.heif", []byte("x")))
	route(t, err, "Decode.ImageNeedsFFmpeg")
	// a file with no picture, and one too large
	for _, c := range []struct {
		info  ffmpeg.Info
		route string
	}{
		{ffmpeg.Info{HasAudio: true}, "decodeWithFFmpeg.ImageDecodeFailed"},
		{ffmpeg.Info{HasVideo: true, Width: 40000, Height: 2}, "decodeWithFFmpeg.ImageTooLarge"},
	} {
		_, err := Decode(t.Context(), path, DecodeOptions{Video: func(context.Context) (ffmpeg.Info, media, error) { return c.info, m, nil }})
		route(t, err, c.route)
	}
	// FFmpeg gives no frame
	empty := &fakeMedia{duration: 0}
	_, err = Decode(t.Context(), path, DecodeOptions{Video: func(context.Context) (ffmpeg.Info, media, error) {
		return ffmpeg.Info{HasVideo: true, Width: 4, Height: 2}, empty, nil
	}})
	route(t, err, "decodeWithFFmpeg.ImageDecodeFailed")
}

func TestADeviceThatPlaysNothingHandsTheClockOn(t *testing.T) {
	r := newPlayerRig(t, testInfo, true)
	r.p.Play()
	r.filled()
	// the device never plays: for half a second the picture waits for it, then goes on the monotonic clock
	r.clock.Advance(400 * time.Millisecond)
	if r.p.Position() != 0 {
		t.Fatalf("position %v while the device may still start", r.p.Position())
	}
	// the stall is seen at 500 ms; at 900 ms the picture is 400 ms on
	r.clock.Advance(500 * time.Millisecond)
	if got := r.p.Position(); got < 390*time.Millisecond || got > 410*time.Millisecond {
		t.Errorf("position %v, 400 ms after the stall", got)
	}
	// a pause holds it there
	r.p.Pause()
	pos := r.p.Position()
	r.clock.Advance(time.Second)
	if r.p.Position() != pos {
		t.Errorf("paused: %v, was %v", r.p.Position(), pos)
	}
}
