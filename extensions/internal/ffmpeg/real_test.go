//go:build emetos_ffmpeg

package ffmpeg

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The client against the real FFmpeg (go test -tags emetos_ffmpeg ./extensions/internal/ffmpeg): a 2 s video that the test makes with
// FFmpeg's testsrc and a sine, in codecs every build has (FFV1 and FLAC, so Fedora's ffmpeg-free makes and reads it too).

func realPrograms(t *testing.T) Programs {
	t.Helper()
	p, err := Find(t.Context(), FindOptions{Path: os.Getenv("PATH")})
	if err != nil {
		t.Skipf("no usable FFmpeg: %v", err)
	}
	return p
}

func makeVideo(t *testing.T, p Programs) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "test.mkv")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.FFmpeg, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=10", "-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "ffv1", "-c:a", "flac", "-shortest", out)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v\n%s", err, data)
	}
	return out
}

func TestTheRealFFmpeg(t *testing.T) {
	p := realPrograms(t)
	video := makeVideo(t, p)
	info, err := Probe(t.Context(), p, video)
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasVideo || !info.HasAudio || info.Width != 160 || info.Height != 120 || info.Rate != 10 || info.VideoCodec != "ffv1" || info.AudioCodec != "flac" {
		t.Errorf("info %+v", info)
	}
	if info.Duration < 1900*time.Millisecond || info.Duration > 2100*time.Millisecond {
		t.Errorf("duration %v", info.Duration)
	}
	r, err := Frames(t.Context(), p, video, FrameOptions{From: time.Second, Width: 80, Height: 60, Rate: 10})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, r.FrameSize())
	n := 0
	for {
		ok, err := r.Next(buf)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		n++
	}
	if err := r.Close(); err != nil {
		t.Error(err)
	}
	if n < 9 || n > 11 {
		t.Errorf("%d frames in the last second", n)
	}
	s, err := Sound(t.Context(), p, video, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
	// about a second of two channels of 32-bit samples at 48 kHz
	if want := SoundRate * SoundChannels * 4; len(data) < want*9/10 || len(data) > want*11/10 {
		t.Errorf("%d bytes of sound, want about %d", len(data), want)
	}
	// a file that is not a video
	text := filepath.Join(t.TempDir(), "notes.mp4")
	if err := os.WriteFile(text, []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Probe(t.Context(), p, text)
	route(t, err, "Probe.FFprobeFailed")
}
