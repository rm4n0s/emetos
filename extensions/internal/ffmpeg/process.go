package ffmpeg

import (
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// tailSize is how much of what a process writes on stderr is kept for its errors.
const tailSize = 4096

// tailBuffer keeps the last tailSize bytes written to it. It is safe from several goroutines.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

// Write implements io.Writer.
func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-tailSize:]...)
	}
	return len(p), nil
}

// String is what was kept.
func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// The messages of FFmpeg that name a codec it cannot decode, of FFmpeg 4.4 to 7.
var codecPatterns = []*regexp.Regexp{
	regexp.MustCompile(`Decoder \(codec ([\w-]+)\) not found`),
	regexp.MustCompile(`Unknown decoder '([\w-]+)'`),
	regexp.MustCompile(`No decoder for codec ([\w-]+)`),
}

// UnsupportedCodec finds in what FFmpeg wrote on stderr the codec it cannot decode.
func UnsupportedCodec(stderr string) (string, bool) {
	for _, re := range codecPatterns {
		if m := re.FindStringSubmatch(stderr); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// lastLine is the last line of the text that is not empty.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// process is an ffmpeg that writes on a pipe: in a process group of its own, killed with its context or Close.
type process struct {
	cmd    *exec.Cmd
	out    *os.File
	stderr *tailBuffer
	done   chan struct{}
	// waitErr is what Wait gave; it is set before done is closed.
	waitErr error
	ctx     context.Context
	stop    func() bool
	once    sync.Once
}

// start starts the program with the arguments. The pipe is made here, not with StdoutPipe, so that Wait does not close it under a
// reader.
func start(ctx context.Context, program string, args []string) (*process, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, errors.NewErr("FFmpegStartFailed", err).SetMetadata("path", program)
	}
	cmd := exec.Command(program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tail := &tailBuffer{}
	cmd.Stdout, cmd.Stderr = pw, tail
	if err := cmd.Start(); err != nil {
		_ = pr.Close() // nothing was read from it
		_ = pw.Close()
		return nil, errors.NewErr("FFmpegStartFailed", err).SetMetadata("path", program)
	}
	if err := pw.Close(); err != nil { // the child has its own end
		killGroup(cmd)
		_ = cmd.Wait() // the process was killed; its end is not an error of the caller
		_ = pr.Close()
		return nil, errors.NewErr("FFmpegStartFailed", err).SetMetadata("path", program)
	}
	p := &process{cmd: cmd, out: pr, stderr: tail, done: make(chan struct{}), ctx: ctx}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	p.stop = context.AfterFunc(ctx, func() { killGroup(cmd) })
	return p, nil
}

// Pid is the id of the process (its process group's too).
func (p *process) Pid() int { return p.cmd.Process.Pid }

// ended waits for the process after its output ended and tells why it ended: nil for a clean end, the context's error when it was
// ended, else FFmpegCodecUnsupported or FFmpegDecodeFailed with what it wrote.
func (p *process) ended(path string) error {
	<-p.done
	if p.waitErr == nil {
		return nil
	}
	if err := p.ctx.Err(); err != nil {
		return errors.NewErr("FFmpegStopped", err).SetMetadata("path", path)
	}
	stderr := p.stderr.String()
	if codec, ok := UnsupportedCodec(stderr); ok {
		return errors.New("FFmpegCodecUnsupported", "This FFmpeg cannot decode "+codec+".", "path", path, "codec", codec, "stderr", stderr)
	}
	msg := lastLine(stderr)
	if msg == "" {
		msg = p.waitErr.Error()
	}
	return errors.New("FFmpegDecodeFailed", "FFmpeg could not decode the file: "+msg, "path", path, "stderr", stderr)
}

// close kills the process group, waits for the process and closes the pipe.
func (p *process) close() error {
	var err error
	p.once.Do(func() {
		p.stop()
		killGroup(p.cmd)
		<-p.done
		if cerr := p.out.Close(); cerr != nil {
			err = errors.NewErr("FFmpegCloseFailed", cerr)
		}
	})
	return err
}

// seconds writes a time for -ss.
func seconds(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', 3, 64) }

// FrameOptions say which frames Frames decodes.
type FrameOptions struct {
	// From is the time of the first frame.
	From time.Duration
	// Width and Height are the size of the frames in pixels; Rate the frames per second.
	Width, Height int
	Rate          float64
}

// FrameReader reads the frames of a video, each Width x Height x 4 bytes of RGBA.
type FrameReader struct {
	p    *process
	path string
	opts FrameOptions
}

// Frames starts decoding the frames of the video at path. It fails with FFmpegStartFailed when ffmpeg cannot be started, and with
// FFmpegFramesInvalid for a size or a rate that is not positive.
func Frames(ctx context.Context, progs Programs, path string, opts FrameOptions) (*FrameReader, error) {
	if opts.Width <= 0 || opts.Height <= 0 || opts.Rate <= 0 {
		return nil, errors.New("FFmpegFramesInvalid", "the frames need a size and a rate", "width", opts.Width, "height", opts.Height, "rate", opts.Rate)
	}
	vsync := []string{"-fps_mode", "cfr"} // FFmpeg 5.1 renamed -vsync
	if progs.Version != "" && CompareVersions(progs.Version, "5.1") < 0 {
		vsync = []string{"-vsync", "cfr"}
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-ss", seconds(opts.From), "-i", path, "-an", "-sn",
		"-vf", "scale=" + strconv.Itoa(opts.Width) + ":" + strconv.Itoa(opts.Height)}
	args = append(args, vsync...)
	args = append(args, "-r", strconv.FormatFloat(opts.Rate, 'f', -1, 64), "-pix_fmt", "rgba", "-f", "rawvideo", "pipe:1")
	p, err := start(ctx, progs.FFmpeg, args)
	if err != nil {
		return nil, err
	}
	return &FrameReader{p: p, path: path, opts: opts}, nil
}

// FrameSize is the size of a frame in bytes.
func (r *FrameReader) FrameSize() int { return r.opts.Width * r.opts.Height * 4 }

// Pid is the id of the ffmpeg process.
func (r *FrameReader) Pid() int { return r.p.Pid() }

// Next reads the next frame into buf, which has FrameSize bytes. It reports false at the end of the video, and fails with the error of
// FFmpeg (FFmpegCodecUnsupported, FFmpegDecodeFailed), FFmpegStopped when the context ended it, and FFmpegFrameCut for a last frame
// that is not whole.
func (r *FrameReader) Next(buf []byte) (bool, error) {
	n, err := io.ReadFull(r.p.out, buf[:r.FrameSize()])
	switch err {
	case nil:
		return true, nil
	case io.EOF:
		if endErr := r.p.ended(r.path); endErr != nil {
			return false, endErr
		}
		return false, nil
	case io.ErrUnexpectedEOF:
		if endErr := r.p.ended(r.path); endErr != nil {
			return false, endErr
		}
		return false, errors.New("FFmpegFrameCut", "the last frame of the video is cut short", "path", r.path, "bytes", n)
	}
	return false, errors.NewErr("FFmpegReadFailed", err).SetMetadata("path", r.path)
}

// Close ends ffmpeg.
func (r *FrameReader) Close() error { return r.p.close() }

// SoundRate is the sample rate of Sound, and SoundChannels its channels.
const (
	SoundRate     = 48000
	SoundChannels = 2
)

// SoundReader reads the sound of a video: 32-bit little-endian floats, SoundChannels interleaved, SoundRate a second.
type SoundReader struct {
	p    *process
	path string
}

// Sound starts decoding the sound of the video at path from a time. It fails with FFmpegStartFailed when ffmpeg cannot be started.
func Sound(ctx context.Context, progs Programs, path string, from time.Duration) (*SoundReader, error) {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-ss", seconds(from), "-i", path, "-vn", "-sn",
		"-ac", strconv.Itoa(SoundChannels), "-ar", strconv.Itoa(SoundRate), "-f", "f32le", "pipe:1"}
	p, err := start(ctx, progs.FFmpeg, args)
	if err != nil {
		return nil, err
	}
	return &SoundReader{p: p, path: path}, nil
}

// Pid is the id of the ffmpeg process.
func (r *SoundReader) Pid() int { return r.p.Pid() }

// Read implements io.Reader: io.EOF at the end of the sound, else the error of FFmpeg as Next gives it.
func (r *SoundReader) Read(b []byte) (int, error) {
	n, err := r.p.out.Read(b)
	if err == io.EOF {
		if endErr := r.p.ended(r.path); endErr != nil {
			return n, endErr
		}
		return n, io.EOF
	}
	if err != nil {
		return n, errors.NewErr("FFmpegReadFailed", err).SetMetadata("path", r.path)
	}
	return n, nil
}

// Close ends ffmpeg.
func (r *SoundReader) Close() error { return r.p.close() }
