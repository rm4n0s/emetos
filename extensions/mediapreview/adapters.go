package mediapreview

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/audio"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
)

// ffmpegMedia is the media of a file through the FFmpeg found.
type ffmpegMedia struct {
	progs ffmpeg.Programs
	path  string
}

// Frames implements media.
func (m ffmpegMedia) Frames(ctx context.Context, opts ffmpeg.FrameOptions) (frameSource, error) {
	r, err := ffmpeg.Frames(ctx, m.progs, m.path, opts)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Sound implements media.
func (m ffmpegMedia) Sound(ctx context.Context, from time.Duration) (io.ReadCloser, error) {
	r, err := ffmpeg.Sound(ctx, m.progs, m.path, from)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// audioSink is a stream of Glutter's audio on a playback device of its own, so that pausing the device pauses only this video.
type audioSink struct {
	dev    *audio.Device
	stream *audio.Stream
	log    *slog.Logger
}

// openAudioSink opens the default playback device for the sound of FFmpeg. It fails with VideoSoundUnavailable when the system has no
// audio (the video then plays without sound, on the monotonic clock).
func openAudioSink(ctx context.Context, binding *widgets.WidgetsBinding, log *slog.Logger) (sink, error) {
	dev, err := audio.ForBinding(binding).OpenPlayback(ctx)
	if err != nil {
		return nil, errors.NewErr("VideoSoundUnavailable", err)
	}
	return &audioSink{dev: dev, stream: dev.NewStream(audio.Format{Rate: ffmpeg.SoundRate, Channels: ffmpeg.SoundChannels}), log: log}, nil
}

// Put implements sink.
func (s *audioSink) Put(samples []float32) error { return s.stream.Put(samples) }

// Queued implements sink.
func (s *audioSink) Queued() time.Duration { return s.stream.Queued() }

// SetGain implements sink.
func (s *audioSink) SetGain(g float64) { s.stream.SetGain(g) }

// Clear implements sink.
func (s *audioSink) Clear() { s.stream.Clear() }

// Pause implements sink: a device that cannot pause keeps playing what is queued, and the clock follows what it played.
func (s *audioSink) Pause() {
	if err := s.dev.Pause(); err != nil {
		s.log.Debug("the sound device cannot pause", "err", err)
	}
}

// Resume implements sink: a device that is lost plays nothing, and the clock waits.
func (s *audioSink) Resume() {
	if err := s.dev.Resume(); err != nil {
		s.log.Debug("the sound device cannot resume", "err", err)
	}
}

// Close implements sink.
func (s *audioSink) Close() {
	s.stream.Close()
	if err := s.dev.Close(); err != nil {
		s.log.Debug("the sound device cannot be closed", "err", err)
	}
}
