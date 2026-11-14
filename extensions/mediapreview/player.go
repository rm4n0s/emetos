package mediapreview

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
)

// The engine of the video player (SPEC §10.37 "Video", D53): two ffmpeg processes per run, the picture and the sound, read on
// goroutines of their own into bounded queues; the sound is the clock, and the picture follows it. The engine knows FFmpeg and the
// audio device only through media and sink, so the tests give fakes and a clock of their own.

// The bounds of the queues: decoded frames waiting to be shown, and sound put in the device and not played yet.
const (
	maxQueuedFrames = 8
	maxQueuedSound  = time.Second
	// tickTime is how often the player looks at its clock while it plays.
	tickTime = 10 * time.Millisecond
	// soundChunk is how many frames of sound are read and put at a time (0.05 s).
	soundChunk = ffmpeg.SoundRate / 20
	// waitForRoom is how long the sound goroutine waits for room in the device.
	waitForRoom = 10 * time.Millisecond
	// stallTime is how long the sound may stay where it is while it is queued and the video plays: a device that plays nothing (a
	// sound server that is suspended, a sink that never runs) hands the clock to the monotonic one, so the picture goes on.
	stallTime = 500 * time.Millisecond
)

// media is FFmpeg as the player uses it, for one file.
type media interface {
	// Frames decodes the frames from a time, at a size and a rate.
	Frames(ctx context.Context, opts ffmpeg.FrameOptions) (frameSource, error)
	// Sound decodes the sound from a time: 32-bit little-endian floats, two channels, 48 kHz.
	Sound(ctx context.Context, from time.Duration) (io.ReadCloser, error)
}

// frameSource is a reader of frames (ffmpeg.FrameReader).
type frameSource interface {
	FrameSize() int
	Next(buf []byte) (bool, error)
	Close() error
}

// sink is where the sound plays: a stream of Glutter's audio on a device of its own, or a fake of the tests. Its methods may be called
// from any goroutine.
type sink interface {
	Put(samples []float32) error
	// Queued is the sound put and not played yet.
	Queued() time.Duration
	SetGain(g float64)
	// Clear drops what is queued; Pause and Resume stop and start the device.
	Clear()
	Pause()
	Resume()
	Close()
}

// clock is time for the player: the real one, or the tests'.
type clock interface {
	Now() time.Time
	// AfterFunc calls fn on the UI thread after d, and returns the function that cancels it.
	AfterFunc(d time.Duration, fn func()) (stop func())
}

// playerOptions are what a player is made with.
type playerOptions struct {
	info  ffmpeg.Info
	media media
	// newSink opens the sound device; nil, or an error, plays without sound, on the monotonic clock.
	newSink func() (sink, error)
	clock   clock
	// post runs a function on the UI thread; changed is called on it when what is shown changed.
	post    func(func())
	changed func()
	// size is the size the frames are decoded at.
	width, height int
}

// shownFrame is a decoded frame and its time.
type shownFrame struct {
	at  time.Duration
	img *ui.Image
}

// run is the picture and the sound from a time, until a seek, the end or the close.
type run struct {
	ctx    context.Context
	cancel context.CancelFunc
	from   time.Duration
	frames chan shownFrame
	// framesErr is the error that ended the decoding of the frames; it is set before frames is closed, and framesDone with it.
	framesErr  error
	framesDone atomic.Bool
	// soundFrames counts the frames of sound put in the sink; soundDone is set when the sound ended, with soundErr its error.
	soundFrames atomic.Int64
	soundDone   atomic.Bool
	soundErr    atomic.Pointer[error]
	wg          sync.WaitGroup
	// started and elapsed are the monotonic clock of a run without sound: the time it last started playing, and how long it played
	// before that.
	started time.Time
	elapsed time.Duration
	// pending is the next frame, taken from the queue and not due yet.
	pending      *shownFrame
	framesClosed bool
	// lastPlayed is the sound played when it last moved, at lastMove; stalled tells that the device stopped playing and the run went
	// on the monotonic clock.
	lastPlayed time.Duration
	lastMove   time.Time
	stalled    bool
}

// player is one video. Its fields belong to the UI thread.
type player struct {
	o       playerOptions
	sink    sink
	sinkErr error
	run     *run
	playing bool
	// pos is the position while paused, and the time the run started from while it plays.
	pos     time.Duration
	frame   *ui.Image
	frameAt time.Duration
	// ended tells that the video played to its end; err is the error that stopped it (a codec FFmpeg cannot decode).
	ended bool
	err   error
	// volume, muted and loop are the controls; forceMute mutes it whatever the user says (a Remote Work client's view).
	volume           float64
	muted, forceMute bool
	loop             bool
	stopTick         func()
	// gen counts the decodings of a still frame, so an older one is dropped.
	gen int
	// dropped counts the frames that came too late to be shown.
	dropped int
	closed  bool
}

func newPlayer(o playerOptions) *player { return &player{o: o, volume: 1} }

// frameTime is the time between two frames.
func (p *player) frameTime() time.Duration {
	if p.o.info.Rate <= 0 {
		return time.Second / 30
	}
	return time.Duration(float64(time.Second) / p.o.info.Rate)
}

// rate is the rate the frames are decoded at.
func (p *player) rate() float64 {
	if p.o.info.Rate <= 0 || p.o.info.Rate > 120 {
		return 30
	}
	return p.o.info.Rate
}

// Position is where the video is now.
func (p *player) Position() time.Duration {
	if !p.playing || p.run == nil {
		return p.pos
	}
	return p.clockOf(p.run)
}

// soundPlayed is the sound of a run the device played.
func (p *player) soundPlayed(r *run) time.Duration {
	return max(time.Duration(float64(r.soundFrames.Load())/ffmpeg.SoundRate*float64(time.Second))-p.sink.Queued(), 0)
}

// clockOf is the position of a run: the sound played, else (no sound, or a device that stalled) the monotonic clock.
func (p *player) clockOf(r *run) time.Duration {
	if p.sink != nil && p.o.info.HasAudio && !r.stalled {
		return r.from + p.soundPlayed(r)
	}
	e := r.elapsed
	if !r.started.IsZero() {
		e += p.o.clock.Now().Sub(r.started)
	}
	return r.from + e
}

// gain is the gain of the sound: the volume, or nothing while muted.
func (p *player) gain() float64 {
	if p.muted || p.forceMute {
		return 0
	}
	return p.volume
}

// Play plays from the position (from the start after the end).
func (p *player) Play() {
	if p.playing || p.closed {
		return
	}
	if p.ended {
		p.pos, p.ended = 0, false
	}
	p.err = nil
	if p.sink == nil && p.sinkErr == nil && p.o.newSink != nil && p.o.info.HasAudio {
		p.sink, p.sinkErr = p.o.newSink()
		if p.sinkErr == nil {
			p.sink.SetGain(p.gain())
		}
	}
	p.playing = true
	if p.run == nil {
		p.startRun(p.pos)
	} else if p.sink != nil {
		p.sink.Resume()
	}
	p.run.started = p.o.clock.Now()
	p.run.lastMove = p.run.started
	p.tick()
}

// Pause stops at the position: the processes wait on their pipes and the device stops.
func (p *player) Pause() {
	if !p.playing {
		return
	}
	p.playing = false
	if p.run != nil {
		p.run.elapsed += p.o.clock.Now().Sub(p.run.started)
		p.run.started = time.Time{}
		// the run goes on from where it stopped: its from and its counts stay, so the clock says the same when it plays again
		p.pos = p.clockOf(p.run)
	}
	if p.sink != nil {
		p.sink.Pause()
	}
	if p.stopTick != nil {
		p.stopTick()
		p.stopTick = nil
	}
	p.o.changed()
}

// Seek goes to a time: the run ends and a new one starts there while it plays; paused, the frame at the time is shown. The last
// frame stays on screen meanwhile.
func (p *player) Seek(t time.Duration) {
	if p.closed {
		return
	}
	t = min(max(t, 0), max(p.o.info.Duration, 0))
	playing := p.playing
	p.endRun()
	p.pos, p.ended, p.err = t, false, nil
	if playing {
		p.startRun(t)
		p.run.started = p.o.clock.Now()
		p.tick()
		return
	}
	p.showStill(t)
	p.o.changed()
}

// showStill decodes the frame at a time and shows it, off the UI thread.
func (p *player) showStill(t time.Duration) {
	if !p.o.info.HasVideo {
		return
	}
	p.gen++
	gen := p.gen
	ctx, cancel := context.WithCancel(context.Background())
	opts := ffmpeg.FrameOptions{From: t, Width: p.o.width, Height: p.o.height, Rate: p.rate()}
	go func() {
		defer cancel()
		img, err := firstFrame(ctx, p.o.media, opts)
		p.o.post(func() {
			if p.closed || gen != p.gen || p.playing {
				return
			}
			if err != nil {
				p.err = err
			} else if img != nil {
				p.frame, p.frameAt = img, t
			}
			p.o.changed()
		})
	}()
}

// firstFrame decodes one frame.
func firstFrame(ctx context.Context, m media, opts ffmpeg.FrameOptions) (*ui.Image, error) {
	src, err := m.Frames(ctx, opts)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, src.FrameSize())
	ok, err := src.Next(buf)
	if cerr := src.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil || !ok {
		return nil, err
	}
	return ui.NewImageFromRGBA(buf, opts.Width, opts.Height), nil
}

// startRun starts the picture and the sound from a time.
func (p *player) startRun(from time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{ctx: ctx, cancel: cancel, from: from, frames: make(chan shownFrame, maxQueuedFrames)}
	p.run = r
	if p.o.info.HasVideo {
		r.wg.Add(1)
		go p.decodeFrames(r)
	} else {
		close(r.frames)
	}
	if p.sink != nil && p.o.info.HasAudio {
		p.sink.Clear()
		p.sink.Resume()
		r.wg.Add(1)
		go p.decodeSound(r)
	} else {
		r.soundDone.Store(true)
	}
}

// endRun cancels the run (its processes end) and drops its sound.
func (p *player) endRun() {
	r := p.run
	if r == nil {
		return
	}
	p.run = nil
	r.cancel()
	if p.sink != nil {
		p.sink.Clear()
	}
	if p.stopTick != nil {
		p.stopTick()
		p.stopTick = nil
	}
}

// decodeFrames reads the frames of a run into its queue, on a goroutine of its own.
func (p *player) decodeFrames(r *run) {
	defer r.wg.Done()
	defer close(r.frames)
	defer r.framesDone.Store(true)
	w, h := p.o.width, p.o.height
	src, err := p.o.media.Frames(r.ctx, ffmpeg.FrameOptions{From: r.from, Width: w, Height: h, Rate: p.rate()})
	if err != nil {
		r.framesErr = err
		return
	}
	defer func() {
		if err := src.Close(); err != nil && r.framesErr == nil && r.ctx.Err() == nil {
			r.framesErr = err
		}
	}()
	step := time.Duration(float64(time.Second) / p.rate())
	for i := 0; ; i++ {
		buf := make([]byte, src.FrameSize())
		ok, err := src.Next(buf)
		if err != nil {
			if r.ctx.Err() == nil {
				r.framesErr = err
			}
			return
		}
		if !ok {
			return
		}
		f := shownFrame{at: r.from + time.Duration(i)*step, img: ui.NewImageFromRGBA(buf, w, h)}
		select {
		case r.frames <- f:
		case <-r.ctx.Done():
			return
		}
	}
}

// decodeSound reads the sound of a run into the sink, on a goroutine of its own, keeping at most maxQueuedSound in it.
func (p *player) decodeSound(r *run) {
	defer r.wg.Done()
	defer r.soundDone.Store(true)
	src, err := p.o.media.Sound(r.ctx, r.from)
	if err != nil {
		r.soundErr.Store(&err)
		return
	}
	defer func() {
		if err := src.Close(); err != nil && r.ctx.Err() == nil && r.soundErr.Load() == nil {
			r.soundErr.Store(&err)
		}
	}()
	raw := make([]byte, soundChunk*ffmpeg.SoundChannels*4)
	samples := make([]float32, soundChunk*ffmpeg.SoundChannels)
	for {
		n, err := io.ReadFull(src, raw)
		n -= n % (ffmpeg.SoundChannels * 4)
		if n > 0 {
			for i := 0; i < n/4; i++ {
				samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
			}
			for p.sink.Queued() > maxQueuedSound {
				select {
				case <-r.ctx.Done():
					return
				case <-time.After(waitForRoom):
				}
			}
			if r.ctx.Err() != nil {
				return
			}
			if perr := p.sink.Put(samples[:n/4]); perr != nil {
				r.soundErr.Store(&perr)
				return
			}
			r.soundFrames.Add(int64(n / (4 * ffmpeg.SoundChannels)))
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return
		}
		if err != nil {
			if r.ctx.Err() == nil {
				r.soundErr.Store(&err)
			}
			return
		}
	}
}

// tick shows the frame that is due and schedules the next tick. A frame that is due late by more than one frame is dropped.
func (p *player) tick() {
	p.stopTick = nil
	r := p.run
	if !p.playing || r == nil || p.closed {
		return
	}
	p.watchStall(r)
	pos := p.clockOf(r)
	late := p.frameTime()
	for {
		if r.pending == nil && !r.framesClosed {
			select {
			case f, ok := <-r.frames:
				if ok {
					r.pending = &f
				} else {
					r.framesClosed = true
				}
			default:
			}
		}
		if r.pending == nil || r.pending.at > pos {
			break
		}
		if pos-r.pending.at > late && p.frame != nil {
			p.dropped++
		} else {
			p.frame, p.frameAt = r.pending.img, r.pending.at
		}
		r.pending = nil
	}
	if err := p.runError(r); err != nil {
		p.stop(err)
		return
	}
	if p.runEnded(r, pos) {
		if p.loop {
			p.endRun()
			p.pos = 0
			p.startRun(0)
			p.run.started = p.o.clock.Now()
		} else {
			p.endRun()
			p.playing, p.ended, p.pos = false, true, max(p.o.info.Duration, pos)
			p.o.changed()
			return
		}
	}
	p.o.changed()
	p.stopTick = p.o.clock.AfterFunc(tickTime, p.tick)
}

// watchStall hands the clock of a run to the monotonic one when the device stopped playing: the sound stayed where it is for stallTime
// while some was queued.
func (p *player) watchStall(r *run) {
	if p.sink == nil || !p.o.info.HasAudio || r.stalled {
		return
	}
	now, played := p.o.clock.Now(), p.soundPlayed(r)
	if played != r.lastPlayed || r.lastMove.IsZero() {
		r.lastPlayed, r.lastMove = played, now
		return
	}
	if now.Sub(r.lastMove) >= stallTime && p.sink.Queued() > 0 {
		r.stalled = true
		r.elapsed, r.started = played, now // the monotonic clock goes on from where the sound stopped
	}
}

// runError is the error that ended a part of the run: the frames once their queue is empty, the sound at once.
func (p *player) runError(r *run) error {
	if r.framesClosed && r.pending == nil && r.framesErr != nil {
		return r.framesErr
	}
	if e := r.soundErr.Load(); e != nil {
		return *e
	}
	return nil
}

// runEnded tells that the run played everything: the frames shown and the sound played out (or the duration passed, for a file whose
// sound ended early or has none).
func (p *player) runEnded(r *run, pos time.Duration) bool {
	framesOver := r.framesClosed && r.pending == nil
	soundOver := r.soundDone.Load() && (p.sink == nil || p.sink.Queued() == 0)
	if !p.o.info.HasAudio || p.sink == nil {
		return framesOver && (pos >= p.o.info.Duration || p.o.info.Duration == 0)
	}
	return framesOver && soundOver
}

// stop stops at an error.
func (p *player) stop(err error) {
	p.pos = p.Position()
	p.endRun()
	p.playing, p.err = false, err
	p.o.changed()
}

// SetVolume sets the volume, from 0 to 1.
func (p *player) SetVolume(v float64) {
	p.volume = min(max(v, 0), 1)
	if p.sink != nil {
		p.sink.SetGain(p.gain())
	}
	p.o.changed()
}

// SetMuted mutes and unmutes.
func (p *player) SetMuted(m bool) {
	p.muted = m
	if p.sink != nil {
		p.sink.SetGain(p.gain())
	}
	p.o.changed()
}

// SetForceMute mutes the video whatever the user says (a Remote Work client's view).
func (p *player) SetForceMute(m bool) {
	if p.forceMute == m {
		return
	}
	p.forceMute = m
	if p.sink != nil {
		p.sink.SetGain(p.gain())
	}
}

// SetLoop plays the video again from its start at its end.
func (p *player) SetLoop(l bool) {
	p.loop = l
	p.o.changed()
}

// Close ends the run and the device; the processes end at once.
func (p *player) Close() {
	if p.closed {
		return
	}
	p.closed = true
	p.endRun()
	p.playing = false
	if p.sink != nil {
		p.sink.Close()
	}
}
