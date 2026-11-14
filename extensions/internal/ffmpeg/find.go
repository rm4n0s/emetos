// Package ffmpeg runs the user's FFmpeg for Media Preview (SPEC §10.37 "Video", D53): it finds ffmpeg and ffprobe, checks the
// version, probes a video, and decodes its frames (RGBA) and its sound (32-bit floats, 48 kHz, stereo) through pipes. Every process
// runs in a process group of its own and is ended with its context; the last 4 KiB of what it writes on stderr make its errors, with
// the codec named when FFmpeg cannot decode it. Emetos links no decoder and stays cgo-free.
package ffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// MinVersion is the oldest FFmpeg accepted.
const MinVersion = "4.4"

// PathSetting is the setting that names the ffmpeg program.
const PathSetting = "mediaPreview.ffmpegPath"

// HomebrewDirs are the folders searched after PATH: an app started from the macOS Finder has a short PATH.
var HomebrewDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// versionTimeout ends an ffmpeg that does not answer -version.
const versionTimeout = 15 * time.Second

// Programs are the FFmpeg programs found.
type Programs struct {
	// FFmpeg and FFprobe are the paths of the programs; Version is FFmpeg's version ("6.1.1"), empty when it could not be read (a
	// build from git says "N-113672-g...").
	FFmpeg, FFprobe, Version string
	// FromSetting tells that PathSetting named the program.
	FromSetting bool
}

// FindOptions say where to look.
type FindOptions struct {
	// Setting is the value of PathSetting: a path, or a name looked for in the folders; empty looks in PATH and HomebrewDirs.
	Setting string
	// Path is the value of PATH.
	Path string
	// Extra are the folders searched after PATH; HomebrewDirs when nil.
	Extra []string
}

// Find finds ffmpeg and the ffprobe beside it, and checks that the version is MinVersion or newer. A setting that is set is the only
// place looked at. It fails with FFmpegPathInvalid for a setting that names no program, FFmpegNotFound when ffmpeg is nowhere,
// FFprobeNotFound when the folder of ffmpeg has no ffprobe, FFmpegVersionFailed when -version cannot be run, and FFmpegTooOld.
func Find(ctx context.Context, opts FindOptions) (Programs, error) {
	extra := opts.Extra
	if extra == nil {
		extra = HomebrewDirs
	}
	dirs := append(filepath.SplitList(opts.Path), extra...)
	var p Programs
	setting := strings.TrimSpace(opts.Setting)
	switch {
	case setting != "" && strings.Contains(setting, "/"):
		if !isExecutable(setting) {
			return Programs{}, errors.New("FFmpegPathInvalid", PathSetting+" is set to "+setting+", which is not a program that can be run",
				"setting", PathSetting, "value", setting)
		}
		p.FFmpeg, p.FromSetting = setting, true
	case setting != "":
		path, ok := findIn(setting, dirs)
		if !ok {
			return Programs{}, errors.New("FFmpegPathInvalid", PathSetting+" is set to "+setting+", which is not a program in PATH",
				"setting", PathSetting, "value", setting)
		}
		p.FFmpeg, p.FromSetting = path, true
	default:
		path, ok := findIn("ffmpeg", dirs)
		if !ok {
			return Programs{}, errors.New("FFmpegNotFound", "ffmpeg was not found in PATH, /opt/homebrew/bin or /usr/local/bin",
				"searched", strings.Join(dirs, string(os.PathListSeparator)))
		}
		p.FFmpeg = path
	}
	probe := filepath.Join(filepath.Dir(p.FFmpeg), "ffprobe")
	if !isExecutable(probe) {
		return Programs{}, errors.New("FFprobeNotFound", "ffprobe is not beside "+p.FFmpeg, "ffmpeg", p.FFmpeg, "ffprobe", probe)
	}
	p.FFprobe = probe
	version, err := queryVersion(ctx, p.FFmpeg)
	if err != nil {
		return Programs{}, err
	}
	p.Version = version
	if version != "" && CompareVersions(version, MinVersion) < 0 {
		return p, errors.New("FFmpegTooOld", "FFmpeg "+version+" is older than "+MinVersion, "path", p.FFmpeg, "version", version)
	}
	return p, nil
}

// isExecutable tells whether the path is a file someone may run.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// findIn looks for the file name in the folders, in order.
func findIn(name string, dirs []string) (string, bool) {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if p := filepath.Join(d, name); isExecutable(p) {
			return p, true
		}
	}
	return "", false
}

// versionPattern finds the version in the first line of ffmpeg -version: "ffmpeg version 6.1.1 Copyright", "ffmpeg version n7.0",
// "ffmpeg version 7.1-full_build-www.gyan.dev".
var versionPattern = regexp.MustCompile(`ffmpeg version n?(\d+(?:\.\d+)+)`)

// ParseVersion finds the version in what ffmpeg -version printed; false for a build that names none.
func ParseVersion(output string) (string, bool) {
	m := versionPattern.FindStringSubmatch(output)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// queryVersion runs ffmpeg -version in a process group of its own, ended with ctx or after versionTimeout.
func queryVersion(ctx context.Context, program string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.Command(program, "-version")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tail := &tailBuffer{}
	cmd.Stdout, cmd.Stderr = tail, tail
	if err := cmd.Start(); err != nil {
		return "", errors.NewErr("FFmpegVersionFailed", err).SetMetadata("path", program)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return "", errors.NewErr("FFmpegVersionFailed", err).SetMetadata("path", program, "output", tail.String())
		}
	case <-ctx.Done():
		killGroup(cmd)
		waitErr := <-done // the end of the killed process
		return "", errors.NewErr("FFmpegVersionFailed", ctx.Err()).SetMetadata("path", program, "end", waitErr)
	}
	v, _ := ParseVersion(tail.String())
	return v, nil
}

// CompareVersions compares dotted versions number by number: -1, 0 or 1.
func CompareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(as), len(bs)) {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

// killGroup kills the process group of a command started with Setpgid.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // the group may be gone already
	}
}
