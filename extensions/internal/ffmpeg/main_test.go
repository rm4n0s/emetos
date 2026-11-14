package ffmpeg

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The fake ffmpeg and ffprobe: the test binary run again under those names (links in a folder of the test), as TestMain sees from its
// name. What they do is set by the environment the test gives them:
//
//	FAKE_FFMPEG_VERSION   the version -version prints (6.1.1); "git" prints a build from git with no version, "hang" never answers
//	FAKE_FFMPEG_DURATION  the length of the video in seconds (2)
//	FAKE_FFMPEG_CODEC     a codec it cannot decode: it says so as FFmpeg 6 does, and fails
//	FAKE_FFMPEG_FAIL      a message it fails with
//	FAKE_FFMPEG_CUT       the last frame is cut short
//	FAKE_FFMPEG_HANG      a file: it starts a child, writes both ids there and waits a minute
//	FAKE_FFMPEG_ARGS      a file the arguments are written to, one a line
//	FAKE_FFPROBE          what ffprobe finds: video (h264 640x360 at 30000/1001 with aac), silent (no sound), cover (sound and a
//	                      cover picture), damaged (it fails)
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case "ffmpeg":
		os.Exit(fakeFFmpeg(os.Args[1:]))
	case "ffprobe":
		os.Exit(fakeFFprobe())
	case "ffmpeg-child":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// argValue is the value after the flag in the arguments.
func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func fakeFFmpeg(args []string) int {
	if f := os.Getenv("FAKE_FFMPEG_ARGS"); f != "" {
		if err := os.WriteFile(f, []byte(strings.Join(args, "\n")), 0o644); err != nil {
			return 70
		}
	}
	if len(args) == 1 && args[0] == "-version" {
		v := os.Getenv("FAKE_FFMPEG_VERSION")
		switch v {
		case "":
			v = "6.1.1"
		case "git":
			fmt.Println("ffmpeg version N-113672-g2a5b4c1 Copyright (c) 2000-2024 the FFmpeg developers")
			return 0
		case "hang":
			time.Sleep(time.Minute)
			return 0
		}
		fmt.Printf("ffmpeg version %s Copyright (c) 2000-2023 the FFmpeg developers\nbuilt with gcc 13\n", v)
		return 0
	}
	if f := os.Getenv("FAKE_FFMPEG_HANG"); f != "" {
		self, err := os.Executable()
		if err != nil {
			return 71
		}
		child := exec.Command(self)
		child.Args[0] = "ffmpeg-child"
		if err := child.Start(); err != nil {
			return 72
		}
		if err := os.WriteFile(f, fmt.Appendf(nil, "%d %d", os.Getpid(), child.Process.Pid), 0o644); err != nil {
			return 73
		}
		time.Sleep(time.Minute)
		return 0
	}
	if codec := os.Getenv("FAKE_FFMPEG_CODEC"); codec != "" {
		fmt.Fprintf(os.Stderr, "[vist#0:0/%s @ 0x55d] Decoder (codec %s) not found for input stream #0:0\n", codec, codec)
		fmt.Fprintln(os.Stderr, "Error opening output files: Decoder not found")
		return 1
	}
	if msg := os.Getenv("FAKE_FFMPEG_FAIL"); msg != "" {
		fmt.Fprintln(os.Stderr, "some line before")
		fmt.Fprintln(os.Stderr, msg)
		return 1
	}
	duration := 2.0
	if d, err := strconv.ParseFloat(os.Getenv("FAKE_FFMPEG_DURATION"), 64); err == nil {
		duration = d
	}
	from, _ := strconv.ParseFloat(argValue(args, "-ss"), 64)
	left := math.Max(duration-from, 0)
	switch argValue(args, "-f") {
	case "rawvideo":
		var w, h int
		if _, err := fmt.Sscanf(argValue(args, "-vf"), "scale=%d:%d", &w, &h); err != nil {
			return 74
		}
		rate, _ := strconv.ParseFloat(argValue(args, "-r"), 64)
		n := int(math.Floor(left*rate + 1e-9))
		frame := make([]byte, w*h*4)
		for i := range n {
			for j := range frame {
				frame[j] = byte(i)
			}
			if _, err := os.Stdout.Write(frame); err != nil {
				return 75
			}
		}
		if os.Getenv("FAKE_FFMPEG_CUT") != "" {
			if _, err := os.Stdout.Write(frame[:len(frame)/2]); err != nil {
				return 76
			}
		}
	case "f32le":
		total := int(math.Round(left*48000)) * 2 * 4
		chunk := make([]byte, 4096)
		for total > 0 {
			k := min(total, len(chunk))
			if _, err := os.Stdout.Write(chunk[:k]); err != nil {
				return 77
			}
			total -= k
		}
	default:
		return 78
	}
	return 0
}

func fakeFFprobe() int {
	type stream map[string]any
	video := stream{"codec_type": "video", "codec_name": "h264", "width": 640, "height": 360, "avg_frame_rate": "30000/1001", "r_frame_rate": "30000/1001"}
	audio := stream{"codec_type": "audio", "codec_name": "aac"}
	var streams []stream
	switch os.Getenv("FAKE_FFPROBE") {
	case "damaged":
		fmt.Fprintln(os.Stderr, "broken.mp4: Invalid data found when processing input")
		return 1
	case "silent":
		streams = []stream{video}
	case "cover":
		streams = []stream{audio, {"codec_type": "video", "codec_name": "mjpeg", "width": 500, "height": 500, "disposition": map[string]any{"attached_pic": 1}}}
	default:
		streams = []stream{video, audio}
	}
	out, err := json.Marshal(map[string]any{"format": map[string]any{"duration": "12.345000"}, "streams": streams})
	if err != nil {
		return 79
	}
	fmt.Println(string(out))
	return 0
}

// fakeBin is a folder with the links ffmpeg and ffprobe to the test binary (only ffmpeg when probe is false).
func fakeBin(t *testing.T, probe bool) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	names := []string{"ffmpeg"}
	if probe {
		names = append(names, "ffprobe")
	}
	for _, n := range names {
		if err := os.Symlink(self, filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
