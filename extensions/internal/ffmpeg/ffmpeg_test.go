package ffmpeg

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// route fails the test unless err is an *errors.Error whose route has want.
func route(t *testing.T, err error, want string) {
	t.Helper()
	e, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !e.HasRoute(want) {
		t.Errorf("route %s, want %s", e.Route(), want)
	}
}

// programs finds the fake programs of a folder.
func programs(t *testing.T, bin string) Programs {
	t.Helper()
	p, err := Find(t.Context(), FindOptions{Path: bin, Extra: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFind(t *testing.T) {
	bin := fakeBin(t, true)
	homebrew := fakeBin(t, true)
	alone := fakeBin(t, false)
	empty := t.TempDir()
	// a program that fails -version, with an ffprobe beside it
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "ffmpeg"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(bin, "ffprobe"), filepath.Join(broken, "ffprobe")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		opts    FindOptions
		version string
		ffmpeg  string
		setting bool
		route   string
	}{
		{"in PATH", FindOptions{Path: empty + ":" + bin, Extra: []string{}}, "", filepath.Join(bin, "ffmpeg"), false, ""},
		{"through the setting", FindOptions{Setting: filepath.Join(homebrew, "ffmpeg"), Path: bin}, "", filepath.Join(homebrew, "ffmpeg"), true, ""},
		{"a name in the setting", FindOptions{Setting: "ffmpeg", Path: bin, Extra: []string{}}, "", filepath.Join(bin, "ffmpeg"), true, ""},
		{"in a Homebrew folder", FindOptions{Path: empty, Extra: []string{homebrew}}, "", filepath.Join(homebrew, "ffmpeg"), false, ""},
		{"a build from git: no version, accepted", FindOptions{Path: bin, Extra: []string{}}, "git", filepath.Join(bin, "ffmpeg"), false, ""},
		{"FFmpeg 4.4 is the oldest accepted", FindOptions{Path: bin, Extra: []string{}}, "4.4", filepath.Join(bin, "ffmpeg"), false, ""},
		{"too old", FindOptions{Path: bin, Extra: []string{}}, "4.3.2", "", false, "Find.FFmpegTooOld"},
		{"missing", FindOptions{Path: empty, Extra: []string{}}, "", "", false, "Find.FFmpegNotFound"},
		{"a setting that names nothing", FindOptions{Setting: filepath.Join(empty, "ffmpeg"), Path: bin}, "", "", false, "Find.FFmpegPathInvalid"},
		{"a name in the setting that is nowhere", FindOptions{Setting: "ffmpeg7", Path: bin, Extra: []string{}}, "", "", false, "Find.FFmpegPathInvalid"},
		{"no ffprobe beside it", FindOptions{Path: alone, Extra: []string{}}, "", "", false, "Find.FFprobeNotFound"},
		{"-version fails", FindOptions{Setting: filepath.Join(broken, "ffmpeg")}, "", "", false, "Find->queryVersion.FFmpegVersionFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FAKE_FFMPEG_VERSION", tt.version)
			p, err := Find(t.Context(), tt.opts)
			if tt.route != "" {
				route(t, err, tt.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.FFmpeg != tt.ffmpeg || p.FFprobe != filepath.Join(filepath.Dir(tt.ffmpeg), "ffprobe") || p.FromSetting != tt.setting {
				t.Errorf("found %+v", p)
			}
			want := map[string]string{"": "6.1.1", "git": "", "4.4": "4.4"}[tt.version]
			if p.Version != want {
				t.Errorf("version %q, want %q", p.Version, want)
			}
		})
	}
}

func TestTheVersionOfACancelledFindIsNotAsked(t *testing.T) {
	bin := fakeBin(t, true)
	t.Setenv("FAKE_FFMPEG_VERSION", "hang")
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := Find(ctx, FindOptions{Path: bin, Extra: []string{}})
	route(t, err, "Find->queryVersion.FFmpegVersionFailed")
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		output, want string
		ok           bool
	}{
		{"ffmpeg version 6.1.1 Copyright (c) 2000-2023", "6.1.1", true},
		{"ffmpeg version n7.0 Copyright", "7.0", true},
		{"ffmpeg version 7.1-full_build-www.gyan.dev Copyright", "7.1", true},
		{"ffmpeg version 4.4.2-0ubuntu0.22.04.1 Copyright", "4.4.2", true},
		{"ffmpeg version N-113672-g2a5b4c1 Copyright", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseVersion(tt.output)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseVersion(%q) = %q %v", tt.output, got, ok)
		}
	}
	for _, c := range []struct {
		a, b string
		want int
	}{{"4.4", "4.4", 0}, {"4.3.2", "4.4", -1}, {"10.0", "4.4", 1}, {"4.4.1", "4.4", 1}, {"5", "5.1", -1}} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%s, %s) = %d", c.a, c.b, got)
		}
	}
}

func TestProbe(t *testing.T) {
	p := programs(t, fakeBin(t, true))
	tests := []struct {
		kind string
		want Info
	}{
		{"video", Info{Duration: 12345 * time.Millisecond, HasVideo: true, HasAudio: true, VideoCodec: "h264", AudioCodec: "aac", Width: 640, Height: 360, Rate: 30000.0 / 1001}},
		{"silent", Info{Duration: 12345 * time.Millisecond, HasVideo: true, VideoCodec: "h264", Width: 640, Height: 360, Rate: 30000.0 / 1001}},
		{"cover", Info{Duration: 12345 * time.Millisecond, HasAudio: true, AudioCodec: "aac"}},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Setenv("FAKE_FFPROBE", tt.kind)
			got, err := Probe(t.Context(), p, "/w/a.mp4")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("info %+v, want %+v", got, tt.want)
			}
		})
	}
	t.Run("damaged", func(t *testing.T) {
		t.Setenv("FAKE_FFPROBE", "damaged")
		_, err := Probe(t.Context(), p, "/w/broken.mp4")
		route(t, err, "Probe.FFprobeFailed")
		if e, _ := errors.FromError(err); e == nil || e.Message != "The file cannot be read: broken.mp4: Invalid data found when processing input" {
			t.Errorf("message %v", err)
		}
	})
	t.Run("no ffprobe", func(t *testing.T) {
		_, err := Probe(t.Context(), Programs{FFprobe: filepath.Join(t.TempDir(), "ffprobe")}, "/w/a.mp4")
		route(t, err, "Probe.FFprobeFailed")
	})
	t.Run("not its JSON", func(t *testing.T) {
		_, err := parseProbe("/w/a.mp4", "not json")
		route(t, err, "parseProbe.FFprobeOutputInvalid")
	})
}

func TestParseRate(t *testing.T) {
	for _, c := range []struct {
		s    string
		want float64
	}{{"30000/1001", 30000.0 / 1001}, {"25/1", 25}, {"0/0", 0}, {"24", 24}, {"", 0}, {"x/2", 0}, {"2/x", 0}} {
		if got := parseRate(c.s); got != c.want {
			t.Errorf("parseRate(%q) = %v", c.s, got)
		}
	}
}

func TestFrames(t *testing.T) {
	bin := fakeBin(t, true)
	tests := []struct {
		name, version string
		from          time.Duration
		frames        int
		sync          string
	}{
		{"from the start", "6.1.1", 0, 20, "-fps_mode"},
		{"from a time", "6.1.1", 1500 * time.Millisecond, 5, "-fps_mode"},
		{"FFmpeg 4.4 has -vsync", "4.4", 0, 20, "-vsync"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FAKE_FFMPEG_VERSION", tt.version)
			p := programs(t, bin)
			argsFile := filepath.Join(t.TempDir(), "args")
			t.Setenv("FAKE_FFMPEG_ARGS", argsFile)
			r, err := Frames(t.Context(), p, "/w/a.mp4", FrameOptions{From: tt.from, Width: 4, Height: 2, Rate: 10})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			}()
			if r.FrameSize() != 32 {
				t.Fatalf("frame size %d", r.FrameSize())
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
				if buf[0] != byte(n) || buf[31] != byte(n) {
					t.Fatalf("frame %d holds %d", n, buf[0])
				}
				n++
			}
			if n != tt.frames {
				t.Errorf("%d frames, want %d", n, tt.frames)
			}
			args, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{tt.sync, "scale=4:2", "rgba", "rawvideo", "-ss\n" + seconds(tt.from)} {
				if !strings.Contains(string(args), want) {
					t.Errorf("no %q in the arguments:\n%s", want, args)
				}
			}
		})
	}
}

func TestSound(t *testing.T) {
	t.Setenv("FAKE_FFMPEG_DURATION", "1")
	p := programs(t, fakeBin(t, true))
	r, err := Sound(t.Context(), p, "/w/a.mp4", 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	// half a second of two channels of 32-bit samples at 48 kHz
	if len(data) != 24000*2*4 {
		t.Errorf("%d bytes", len(data))
	}
	if err := r.Close(); err != nil {
		t.Error(err)
	}
}

func TestACancelledContextEndsTheProcessAndItsChildren(t *testing.T) {
	p := programs(t, fakeBin(t, true))
	pids := filepath.Join(t.TempDir(), "pids")
	t.Setenv("FAKE_FFMPEG_HANG", pids)
	for _, kind := range []string{"frames", "sound"} {
		t.Run(kind, func(t *testing.T) {
			if err := os.Remove(pids); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			var next func() error
			if kind == "frames" {
				r, err := Frames(ctx, p, "/w/a.mp4", FrameOptions{Width: 2, Height: 2, Rate: 5})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = r.Close() }() // ended already
				next = func() error { _, err := r.Next(make([]byte, r.FrameSize())); return err }
			} else {
				r, err := Sound(ctx, p, "/w/a.mp4", 0)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = r.Close() }()
				next = func() error { _, err := r.Read(make([]byte, 64)); return err }
			}
			var ids []int
			deadline := time.Now().Add(10 * time.Second)
			for len(ids) != 2 && time.Now().Before(deadline) {
				if data, err := os.ReadFile(pids); err == nil {
					ids = nil
					for f := range strings.FieldsSeq(string(data)) {
						id, _ := strconv.Atoi(f)
						ids = append(ids, id)
					}
				}
				time.Sleep(5 * time.Millisecond)
			}
			if len(ids) != 2 || !alive(ids[0]) || !alive(ids[1]) {
				t.Fatalf("the fake did not start its child: %v", ids)
			}
			cancel()
			if err := next(); err == nil {
				t.Error("reading went on after the context ended")
			} else {
				route(t, err, "FFmpegStopped")
			}
			deadline = time.Now().Add(10 * time.Second)
			for (alive(ids[0]) || alive(ids[1])) && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if alive(ids[0]) || alive(ids[1]) {
				t.Errorf("left running: ffmpeg %v, its child %v", alive(ids[0]), alive(ids[1]))
			}
		})
	}
}

func TestTheErrorsOfFFmpeg(t *testing.T) {
	p := programs(t, fakeBin(t, true))
	read := func(t *testing.T) error {
		r, err := Frames(t.Context(), p, "/w/a.mp4", FrameOptions{Width: 2, Height: 2, Rate: 5})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		}()
		buf := make([]byte, r.FrameSize())
		for {
			ok, err := r.Next(buf)
			if err != nil || !ok {
				return err
			}
		}
	}
	t.Run("a codec it cannot decode", func(t *testing.T) {
		t.Setenv("FAKE_FFMPEG_CODEC", "h264")
		err := read(t)
		route(t, err, "FrameReader.Next->process.ended.FFmpegCodecUnsupported")
		if e, _ := errors.FromError(err); e == nil || e.Message != "This FFmpeg cannot decode h264." || e.Metadata["codec"] != "h264" {
			t.Errorf("error %v", err)
		}
	})
	t.Run("another failure", func(t *testing.T) {
		t.Setenv("FAKE_FFMPEG_FAIL", "a.mp4: moov atom not found")
		err := read(t)
		route(t, err, "FrameReader.Next->process.ended.FFmpegDecodeFailed")
		if e, _ := errors.FromError(err); e == nil || e.Message != "FFmpeg could not decode the file: a.mp4: moov atom not found" {
			t.Errorf("error %v", err)
		}
	})
	t.Run("a last frame cut short", func(t *testing.T) {
		t.Setenv("FAKE_FFMPEG_CUT", "1")
		route(t, read(t), "FrameReader.Next.FFmpegFrameCut")
	})
	t.Run("the sound of a codec it cannot decode", func(t *testing.T) {
		t.Setenv("FAKE_FFMPEG_CODEC", "opus")
		r, err := Sound(t.Context(), p, "/w/a.mp4", 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(r)
		route(t, err, "SoundReader.Read->process.ended.FFmpegCodecUnsupported")
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Run("no size", func(t *testing.T) {
		_, err := Frames(t.Context(), p, "/w/a.mp4", FrameOptions{Rate: 5})
		route(t, err, "Frames.FFmpegFramesInvalid")
	})
	t.Run("no ffmpeg", func(t *testing.T) {
		_, err := Frames(t.Context(), Programs{FFmpeg: filepath.Join(t.TempDir(), "ffmpeg")}, "/w/a.mp4", FrameOptions{Width: 2, Height: 2, Rate: 5})
		route(t, err, "Frames->start.FFmpegStartFailed")
	})
	t.Run("reading after Close", func(t *testing.T) {
		r, err := Frames(t.Context(), p, "/w/a.mp4", FrameOptions{Width: 2, Height: 2, Rate: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = r.Next(make([]byte, r.FrameSize()))
		route(t, err, "FrameReader.Next.FFmpegReadFailed")
		s, err := Sound(t.Context(), p, "/w/a.mp4", 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = s.Read(make([]byte, 8))
		route(t, err, "SoundReader.Read.FFmpegReadFailed")
	})
	t.Run("a pipe that was closed already", func(t *testing.T) {
		r, err := Frames(t.Context(), p, "/w/a.mp4", FrameOptions{Width: 2, Height: 2, Rate: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.p.out.Close(); err != nil {
			t.Fatal(err)
		}
		route(t, r.Close(), "process.close.func1.FFmpegCloseFailed")
	})
}

func TestUnsupportedCodec(t *testing.T) {
	for _, c := range []struct {
		stderr, codec string
		ok            bool
	}{
		{"[vist#0:0/hevc @ 0x1] Decoder (codec hevc) not found for input stream #0:0", "hevc", true},
		{"Unknown decoder 'h264'", "h264", true},
		{"No decoder for codec av1", "av1", true},
		{"moov atom not found", "", false},
	} {
		codec, ok := UnsupportedCodec(c.stderr)
		if codec != c.codec || ok != c.ok {
			t.Errorf("UnsupportedCodec(%q) = %q %v", c.stderr, codec, ok)
		}
	}
	var b tailBuffer
	if _, err := b.Write(bytes.Repeat([]byte("a"), 5000)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("end")); err != nil {
		t.Fatal(err)
	}
	if s := b.String(); len(s) != tailSize || !strings.HasSuffix(s, "end") {
		t.Errorf("the tail keeps %d bytes", len(s))
	}
}
