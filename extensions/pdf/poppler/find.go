// Package poppler runs the user's Poppler programs for the PDF Viewer (SPEC §10.37 "PDF", D54): it finds pdfinfo, pdftoppm,
// pdftotext and pdftohtml, checks the version, reads a document's pages and their sizes, and draws a page at a resolution. Every
// process runs in a process group of its own and is ended with its context; a password is given to every program with -upw.
package poppler

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

// MinVersion is the oldest Poppler accepted.
const MinVersion = "22.02"

// PathSetting is the setting that names the folder of the Poppler programs.
const PathSetting = "pdf.popplerPath"

// The programs of Poppler that the PDF Viewer runs.
const (
	ProgramInfo = "pdfinfo"
	ProgramDraw = "pdftoppm"
	ProgramText = "pdftotext"
	ProgramHTML = "pdftohtml"
)

// programs are the programs that must be in the folder found.
var programs = []string{ProgramInfo, ProgramDraw, ProgramText, ProgramHTML}

// HomebrewDirs are the folders searched after PATH: an app started from the macOS Finder has a short PATH.
var HomebrewDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// versionTimeout ends a pdfinfo that does not answer -v.
const versionTimeout = 15 * time.Second

// Programs are the Poppler programs found.
type Programs struct {
	// Dir is the folder that holds the programs; Version is Poppler's version ("24.02.0").
	Dir, Version string
	// FromSetting tells that PathSetting named the folder.
	FromSetting bool
}

// Path is the path of one of the programs.
func (p Programs) Path(program string) string { return filepath.Join(p.Dir, program) }

// FindOptions say where to look.
type FindOptions struct {
	// Setting is the value of PathSetting: the folder of the programs; empty looks in PATH and HomebrewDirs.
	Setting string
	// Path is the value of PATH.
	Path string
	// Extra are the folders searched after PATH; HomebrewDirs when nil.
	Extra []string
}

// Find finds the folder of pdfinfo, checks that the other programs are beside it and that the version is MinVersion or newer. A
// setting that is set is the only place looked at. It fails with PopplerPathInvalid for a setting that is not a folder with pdfinfo,
// PopplerNotFound when pdfinfo is nowhere, PopplerProgramMissing when a program is not beside pdfinfo, PopplerVersionFailed when -v
// cannot be run or names no version, and PopplerTooOld.
func Find(ctx context.Context, opts FindOptions) (Programs, error) {
	var p Programs
	if setting := strings.TrimSpace(opts.Setting); setting != "" {
		if !isExecutable(filepath.Join(setting, ProgramInfo)) {
			return Programs{}, errors.New("PopplerPathInvalid", PathSetting+" is set to "+setting+", which is not a folder with pdfinfo",
				"setting", PathSetting, "value", setting)
		}
		p.Dir, p.FromSetting = setting, true
	} else {
		extra := opts.Extra
		if extra == nil {
			extra = HomebrewDirs
		}
		dirs := append(filepath.SplitList(opts.Path), extra...)
		for _, d := range dirs {
			if d != "" && isExecutable(filepath.Join(d, ProgramInfo)) {
				p.Dir = d
				break
			}
		}
		if p.Dir == "" {
			return Programs{}, errors.New("PopplerNotFound", "pdfinfo was not found in PATH, /opt/homebrew/bin or /usr/local/bin",
				"searched", strings.Join(dirs, string(os.PathListSeparator)))
		}
	}
	var missing []string
	for _, name := range programs {
		if !isExecutable(p.Path(name)) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return p, errors.New("PopplerProgramMissing", strings.Join(missing, ", ")+" not beside pdfinfo in "+p.Dir,
			"dir", p.Dir, "missing", missing)
	}
	version, err := queryVersion(ctx, p.Path(ProgramInfo))
	if err != nil {
		return p, err
	}
	p.Version = version
	if CompareVersions(version, MinVersion) < 0 {
		return p, errors.New("PopplerTooOld", "Poppler "+version+" is older than "+MinVersion, "dir", p.Dir, "version", version)
	}
	return p, nil
}

// isExecutable tells whether the path is a file someone may run.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// versionPattern finds the version in what pdfinfo -v prints: "pdfinfo version 24.02.0".
var versionPattern = regexp.MustCompile(`pdfinfo version (\d+(?:\.\d+)+)`)

// ParseVersion finds the version in what pdfinfo -v printed.
func ParseVersion(output string) (string, bool) {
	m := versionPattern.FindStringSubmatch(output)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// queryVersion runs pdfinfo -v in a process group of its own, ended with ctx or after versionTimeout. Its exit status is not looked
// at: some versions end -v with 99.
func queryVersion(ctx context.Context, program string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.Command(program, "-v")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tail := &tailBuffer{}
	cmd.Stdout, cmd.Stderr = tail, tail
	if err := cmd.Start(); err != nil {
		return "", errors.NewErr("PopplerVersionFailed", err).SetMetadata("path", program)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case waitErr := <-done:
		if v, ok := ParseVersion(tail.String()); ok {
			return v, nil
		}
		return "", errors.New("PopplerVersionFailed", "pdfinfo -v named no version", "path", program, "output", tail.String(), "end", waitErr)
	case <-ctx.Done():
		killGroup(cmd)
		waitErr := <-done // the end of the killed process
		return "", errors.NewErr("PopplerVersionFailed", ctx.Err()).SetMetadata("path", program, "end", waitErr)
	}
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
