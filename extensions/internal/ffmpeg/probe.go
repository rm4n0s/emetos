package ffmpeg

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// probeTimeout ends an ffprobe that does not answer: probing reads only the headers of the file.
const probeTimeout = 30 * time.Second

// Info is what ffprobe tells of a video.
type Info struct {
	// Duration is the length of the file; 0 when it is not known (a stream).
	Duration time.Duration
	// HasVideo and HasAudio tell the streams it has; VideoCodec and AudioCodec name their codecs (h264, opus).
	HasVideo, HasAudio     bool
	VideoCodec, AudioCodec string
	// Width and Height are the size of the picture; Rate its frames per second.
	Width, Height int
	Rate          float64
}

// probeOutput is the part of ffprobe's JSON that is read.
type probeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		RFrameRate   string `json:"r_frame_rate"`
		Disposition  struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

// Probe asks ffprobe what the file holds: its duration and its first video and audio streams (a cover picture is not a video). It
// fails with FFprobeFailed when ffprobe cannot read the file (its message: "Invalid data found when processing input"), and with
// FFprobeOutputInvalid when what it printed is not its JSON.
func Probe(ctx context.Context, progs Programs, path string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.Command(progs.FFprobe, "-v", "error", "-of", "json", "-show_format", "-show_streams", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out strings.Builder
	stderr := &tailBuffer{}
	cmd.Stdout, cmd.Stderr = &out, stderr
	if err := cmd.Start(); err != nil {
		return Info{}, errors.NewErr("FFprobeFailed", err).SetMetadata("path", path, "ffprobe", progs.FFprobe)
	}
	stop := context.AfterFunc(ctx, func() { killGroup(cmd) })
	err := cmd.Wait()
	stop()
	if err != nil {
		msg := lastLine(stderr.String())
		if ctx.Err() != nil {
			msg = "ffprobe did not answer"
		} else if msg == "" {
			msg = err.Error()
		}
		return Info{}, errors.New("FFprobeFailed", "The file cannot be read: "+msg, "path", path, "stderr", stderr.String())
	}
	return parseProbe(path, out.String())
}

// parseProbe reads the JSON of ffprobe.
func parseProbe(path, text string) (Info, error) {
	var o probeOutput
	if err := json.Unmarshal([]byte(text), &o); err != nil {
		return Info{}, errors.NewErr("FFprobeOutputInvalid", err).SetMetadata("path", path)
	}
	var info Info
	if d, err := strconv.ParseFloat(o.Format.Duration, 64); err == nil && d > 0 {
		info.Duration = time.Duration(d * float64(time.Second))
	}
	for _, s := range o.Streams {
		switch {
		case s.CodecType == "video" && !info.HasVideo && s.Disposition.AttachedPic == 0:
			info.HasVideo, info.VideoCodec, info.Width, info.Height = true, s.CodecName, s.Width, s.Height
			info.Rate = parseRate(s.AvgFrameRate)
			if info.Rate <= 0 {
				info.Rate = parseRate(s.RFrameRate)
			}
		case s.CodecType == "audio" && !info.HasAudio:
			info.HasAudio, info.AudioCodec = true, s.CodecName
		}
	}
	return info, nil
}

// parseRate reads a frame rate written as a fraction ("30000/1001") or a number; 0 when there is none ("0/0").
func parseRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	if !ok {
		return n
	}
	d, err := strconv.ParseFloat(den, 64)
	if err != nil || d == 0 {
		return 0
	}
	return n / d
}
