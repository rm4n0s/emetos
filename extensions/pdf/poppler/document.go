package poppler

import (
	"bufio"
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/rm4n0s/errors"
)

// tailSize is how much of what a program writes on stderr is kept for its errors.
const tailSize = 4096

// tailBuffer keeps the last tailSize bytes written to it.
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

// maxOutput is the most a program may write on stdout: the information of a document, its outline or the text of a page are far
// less.
const maxOutput = 64 << 20

// errOutputTooLarge ends a program that writes more than maxOutput; it is never returned.
var errOutputTooLarge = &errors.Error{Tag: "PopplerOutputTooLarge"}

// limitedBuffer is a buffer that takes at most maxOutput bytes: a write past it fails, which ends the program writing (its pipe is
// closed). The buffer is a field, not embedded: io.Copy would use its ReadFrom and pass by Write.
type limitedBuffer struct {
	buf  bytes.Buffer
	over bool
}

// Write implements io.Writer.
func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > maxOutput {
		b.over = true
		return 0, errOutputTooLarge
	}
	return b.buf.Write(p)
}

// Bytes are what was written.
func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

// String is what was written.
func (b *limitedBuffer) String() string { return b.buf.String() }

// result is how a program ended: startErr when it could not be started, stopped when ctx ended it, exitErr when it failed.
type result struct {
	stderr   string
	startErr error
	exitErr  error
	stopped  bool
}

// run runs a program in a process group of its own, with stdout written to out, and kills the group when ctx ends.
func run(ctx context.Context, program string, args []string, out *limitedBuffer) result {
	cmd := exec.Command(program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tail := &tailBuffer{}
	cmd.Stdout, cmd.Stderr = out, tail
	if out == nil {
		cmd.Stdout = tail
	}
	if err := cmd.Start(); err != nil {
		return result{startErr: err}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return result{stderr: tail.String(), exitErr: err}
	case <-ctx.Done():
		killGroup(cmd)
		err := <-done // the end of the killed process
		return result{stderr: tail.String(), exitErr: err, stopped: true}
	}
}

// passwordArgs are the arguments that give the user password to a program.
func passwordArgs(password string) []string {
	if password == "" {
		return nil
	}
	return []string{"-upw", password}
}

// lockedMessage is what Poppler says of a document whose password is missing or wrong.
const lockedMessage = "Incorrect password"

// lastLine is the last line a program wrote that is not empty, without Poppler's "Syntax Error: " and the like before it.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if i := strings.Index(line, "Error: "); i >= 0 && i < 20 {
		line = line[i+len("Error: "):]
	}
	return line
}

// Page is the size of a page in points (1/72 inch), as it is shown: a page rotated by 90 or 270 degrees has its sides swapped.
type Page struct {
	Width, Height float64
}

// Document is what pdfinfo says of a PDF.
type Document struct {
	Pages     []Page
	Encrypted bool
}

// maxPages is the last page asked of pdfinfo: it stops at the document's own last page.
const maxPages = 1000000

// The lines of pdfinfo that are read: "Pages:          2", "Page    1 size: 612 x 792 pts (letter)", "Page    1 rot:   90",
// "Encrypted:      yes (print:yes ...)".
var (
	pagesLine = regexp.MustCompile(`(?m)^Pages:\s+(\d+)\s*$`)
	sizeLine  = regexp.MustCompile(`^Page\s+(\d+) size:\s+([\d.]+) x ([\d.]+) pts`)
	rotLine   = regexp.MustCompile(`^Page\s+(\d+) rot:\s+(-?\d+)`)
)

// Info runs pdfinfo on a file and reads its pages and their sizes. It fails with PopplerStartFailed, PopplerStopped when ctx ends,
// PDFPasswordNeeded when the document has a password and none was given, PDFPasswordWrong, PDFDamaged with what pdfinfo said, and
// PDFInfoInvalid when its output names no pages.
func Info(ctx context.Context, p Programs, file, password string) (Document, error) {
	var out limitedBuffer
	args := append(append([]string{"-f", "1", "-l", strconv.Itoa(maxPages)}, passwordArgs(password)...), file)
	res := run(ctx, p.Path(ProgramInfo), args, &out)
	switch {
	case res.startErr != nil:
		return Document{}, errors.NewErr("PopplerStartFailed", res.startErr).SetMetadata("program", p.Path(ProgramInfo))
	case res.stopped:
		return Document{}, errors.NewErr("PopplerStopped", ctx.Err()).SetMetadata("program", ProgramInfo, "file", file)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage) && password == "":
		return Document{}, errors.New("PDFPasswordNeeded", "This document is protected by a password.", "file", file)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage):
		return Document{}, errors.New("PDFPasswordWrong", "The password is not correct.", "file", file)
	case res.exitErr != nil:
		return Document{}, errors.NewErr("PDFDamaged", res.exitErr).SetMetadata("file", file, "message", lastLine(res.stderr), "stderr", res.stderr)
	}
	doc, ok := parseInfo(out.String())
	if !ok {
		return Document{}, errors.New("PDFInfoInvalid", "pdfinfo named no pages", "file", file, "output", out.String())
	}
	return doc, nil
}

// parseInfo reads what pdfinfo printed; false when it names no pages or not the size of each.
func parseInfo(s string) (Document, bool) {
	m := pagesLine.FindStringSubmatch(s)
	if m == nil {
		return Document{}, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 || n > maxPages {
		return Document{}, false
	}
	doc := Document{Pages: make([]Page, n)}
	sized := make([]bool, n)
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Encrypted:") {
			doc.Encrypted = strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(line, "Encrypted:")), "yes")
			continue
		}
		if m := sizeLine.FindStringSubmatch(line); m != nil {
			i, _ := strconv.Atoi(m[1])
			w, errW := strconv.ParseFloat(m[2], 64)
			h, errH := strconv.ParseFloat(m[3], 64)
			if i < 1 || i > n || errW != nil || errH != nil || w <= 0 || h <= 0 {
				return Document{}, false
			}
			doc.Pages[i-1] = Page{Width: w, Height: h}
			sized[i-1] = true
			continue
		}
		if m := rotLine.FindStringSubmatch(line); m != nil {
			i, _ := strconv.Atoi(m[1])
			r, _ := strconv.Atoi(m[2])
			if i >= 1 && i <= n && ((r%360)+360)%180 == 90 {
				pg := &doc.Pages[i-1]
				pg.Width, pg.Height = pg.Height, pg.Width
			}
		}
	}
	for _, ok := range sized {
		if !ok {
			return Document{}, false
		}
	}
	return doc, true
}

// Render draws one page (from 1) at a resolution in dots per inch with pdftoppm, which writes a PNG in a folder of its own (Poppler
// 26 no longer writes on stdout). It fails as Info does, with PDFRenderFailed in place of PDFDamaged, PDFRenderTempFailed when the
// folder cannot be made, PDFRenderInvalid when the picture cannot be read, and PDFRenderCleanupFailed when the folder cannot be
// removed.
func Render(ctx context.Context, p Programs, file, password string, page int, dpi float64) (img image.Image, err error) {
	dir, mkErr := os.MkdirTemp("", "emetos-pdf-")
	if mkErr != nil {
		return nil, errors.NewErr("PDFRenderTempFailed", mkErr)
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil && err == nil {
			img, err = nil, errors.NewErr("PDFRenderCleanupFailed", rmErr).SetMetadata("dir", dir)
		}
	}()
	n := strconv.Itoa(page)
	args := append([]string{"-f", n, "-l", n, "-r", strconv.FormatFloat(dpi, 'f', 2, 64), "-png", "-singlefile"}, passwordArgs(password)...)
	args = append(args, file, filepath.Join(dir, "page"))
	res := run(ctx, p.Path(ProgramDraw), args, nil)
	switch {
	case res.startErr != nil:
		return nil, errors.NewErr("PopplerStartFailed", res.startErr).SetMetadata("program", p.Path(ProgramDraw))
	case res.stopped:
		return nil, errors.NewErr("PopplerStopped", ctx.Err()).SetMetadata("program", ProgramDraw, "file", file, "page", page)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage) && password == "":
		return nil, errors.New("PDFPasswordNeeded", "This document is protected by a password.", "file", file)
	case res.exitErr != nil && strings.Contains(res.stderr, lockedMessage):
		return nil, errors.New("PDFPasswordWrong", "The password is not correct.", "file", file)
	case res.exitErr != nil:
		return nil, errors.NewErr("PDFRenderFailed", res.exitErr).SetMetadata("file", file, "page", page, "message", lastLine(res.stderr))
	}
	f, openErr := os.Open(filepath.Join(dir, "page.png"))
	if openErr != nil {
		return nil, errors.NewErr("PDFRenderInvalid", openErr).SetMetadata("file", file, "page", page, "stderr", res.stderr)
	}
	img, decErr := png.Decode(bufio.NewReader(f))
	if closeErr := f.Close(); decErr == nil {
		decErr = closeErr
	}
	if decErr != nil {
		return nil, errors.NewErr("PDFRenderInvalid", decErr).SetMetadata("file", file, "page", page)
	}
	return img, nil
}
