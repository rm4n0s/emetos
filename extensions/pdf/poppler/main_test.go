package poppler

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The fake Poppler: the test binary run again as pdfinfo and pdftoppm (links in a folder of the test), as TestMain sees from its
// name. What they do is set by the environment the test gives them:
//
//	FAKE_POPPLER_VERSION  the version -v prints (24.02.0); "none" prints none, "hang" never answers
//	FAKE_PDF_PAGES        the pages, "612x792,842x595r90" (each "<w>x<h>", with "r<degrees>" when rotated); two letter pages
//	FAKE_PDF_PASSWORD     the user password of the document: without it (or with another) both fail as Poppler does
//	FAKE_PDF_DAMAGED      the document is damaged: both fail with this message as a syntax error
//	FAKE_POPPLER_HANG     a file: the program starts a child, writes both ids there and waits a minute
//	FAKE_POPPLER_ARGS     a file the arguments are written to, one a line
//	FAKE_PPM              how pdftoppm goes wrong: "none" writes no picture, "garbage" writes one that is not a PNG, "stuck" writes
//	                      the picture and then makes its folder read-only, so it cannot be removed
//
// pdftoppm draws the page at its size and resolution, gray 40 times the page number.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case ProgramInfo:
		os.Exit(fakeInfo(os.Args[1:]))
	case ProgramDraw:
		os.Exit(fakeDraw(os.Args[1:]))
	case "poppler-child":
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

// fakeCommon does what both programs do first; it gives an exit code when the program is done.
func fakeCommon(args []string) (int, bool) {
	if f := os.Getenv("FAKE_POPPLER_ARGS"); f != "" {
		if err := os.WriteFile(f, []byte(strings.Join(args, "\n")), 0o644); err != nil {
			return 70, true
		}
	}
	if f := os.Getenv("FAKE_POPPLER_HANG"); f != "" {
		child := exec.Command(filepath.Join(filepath.Dir(os.Args[0]), "poppler-child"))
		if err := child.Start(); err != nil {
			return 71, true
		}
		if err := os.WriteFile(f, []byte(fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)), 0o644); err != nil {
			return 72, true
		}
		time.Sleep(time.Minute)
		return 0, true
	}
	if msg := os.Getenv("FAKE_PDF_DAMAGED"); msg != "" {
		fmt.Fprintln(os.Stderr, "Syntax Error: "+msg)
		fmt.Fprintln(os.Stderr, "Syntax Error: "+msg)
		return 1, true
	}
	if pw := os.Getenv("FAKE_PDF_PASSWORD"); pw != "" && argValue(args, "-upw") != pw {
		fmt.Fprintln(os.Stderr, "Command Line Error: Incorrect password")
		return 1, true
	}
	return 0, false
}

// fakePage is a page of FAKE_PDF_PAGES.
type fakePage struct {
	w, h float64
	rot  int
}

func fakePages() []fakePage {
	spec := os.Getenv("FAKE_PDF_PAGES")
	if spec == "" {
		spec = "612x792,612x792"
	}
	var pages []fakePage
	for _, s := range strings.Split(spec, ",") {
		var p fakePage
		if i := strings.Index(s, "r"); i >= 0 {
			p.rot, _ = strconv.Atoi(s[i+1:])
			s = s[:i]
		}
		w, h, _ := strings.Cut(s, "x")
		p.w, _ = strconv.ParseFloat(w, 64)
		p.h, _ = strconv.ParseFloat(h, 64)
		pages = append(pages, p)
	}
	return pages
}

func fakeInfo(args []string) int {
	if len(args) == 1 && args[0] == "-v" {
		switch v := os.Getenv("FAKE_POPPLER_VERSION"); v {
		case "hang":
			time.Sleep(time.Minute)
		case "none":
			fmt.Fprintln(os.Stderr, "pdfinfo, a build")
		case "":
			fmt.Fprintln(os.Stderr, "pdfinfo version 24.02.0\nCopyright 2005-2024 The Poppler Developers - http://poppler.freedesktop.org")
		default:
			fmt.Fprintln(os.Stderr, "pdfinfo version "+v)
		}
		return 99 // as some versions do
	}
	if code, done := fakeCommon(args); done {
		return code
	}
	pages := fakePages()
	first, _ := strconv.Atoi(argValue(args, "-f"))
	last, _ := strconv.Atoi(argValue(args, "-l"))
	fmt.Println("Producer:        a test\nTagged:          no")
	fmt.Printf("Pages:           %d\n", len(pages))
	if os.Getenv("FAKE_PDF_PASSWORD") != "" {
		fmt.Println("Encrypted:       yes (print:yes copy:yes change:yes addNotes:yes algorithm:RC4)")
	} else {
		fmt.Println("Encrypted:       no")
	}
	for i := first; i <= min(last, len(pages)); i++ {
		p := pages[i-1]
		fmt.Printf("Page %4d size:  %s x %s pts\nPage %4d rot:   %d\n", i, strconv.FormatFloat(p.w, 'f', -1, 64), strconv.FormatFloat(p.h, 'f', -1, 64), i, p.rot)
	}
	fmt.Println("File size:       776 bytes")
	return 0
}

func fakeDraw(args []string) int {
	if code, done := fakeCommon(args); done {
		return code
	}
	pages := fakePages()
	n, _ := strconv.Atoi(argValue(args, "-f"))
	if n < 1 || n > len(pages) {
		fmt.Fprintf(os.Stderr, "Wrong page range given: the first page (%d) can not be after the last page (%d).\n", n, len(pages))
		return 99
	}
	dpi, _ := strconv.ParseFloat(argValue(args, "-r"), 64)
	p := pages[n-1]
	w, h := p.w, p.h
	if p.rot%180 != 0 {
		w, h = h, w
	}
	out := args[len(args)-1] + ".png"
	switch os.Getenv("FAKE_PPM") {
	case "none":
		return 0
	case "garbage":
		if err := os.WriteFile(out, []byte("not a picture"), 0o644); err != nil {
			return 73
		}
		return 0
	}
	img := image.NewGray(image.Rect(0, 0, int(math.Ceil(w*dpi/72)), int(math.Ceil(h*dpi/72))))
	for i := range img.Pix {
		img.Pix[i] = uint8(40 * n)
	}
	img.SetGray(0, 0, color.Gray{Y: 255})
	f, err := os.Create(out)
	if err != nil {
		return 74
	}
	if err := png.Encode(f, img); err != nil {
		return 75
	}
	if err := f.Close(); err != nil {
		return 76
	}
	if os.Getenv("FAKE_PPM") == "stuck" {
		if err := os.Chmod(filepath.Dir(out), 0o500); err != nil {
			return 77
		}
	}
	return 0
}

// fakeDir is a folder with the fake programs (all four; pdftotext and pdftohtml are never run here) and the child.
func fakeDir(t *testing.T, names ...string) string {
	t.Helper()
	if len(names) == 0 {
		names = append(programs[:len(programs):len(programs)], "poppler-child")
	}
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.Symlink(self, filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
