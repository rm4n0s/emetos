package poppler

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"slices"
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

// found finds the fake programs of a folder.
func found(t *testing.T, dir string) Programs {
	t.Helper()
	p, err := Find(t.Context(), FindOptions{Path: dir, Extra: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFind(t *testing.T) {
	bin := fakeDir(t)
	homebrew := fakeDir(t)
	partial := fakeDir(t, ProgramInfo, ProgramDraw)
	empty := t.TempDir()
	broken := t.TempDir() // a pdfinfo that cannot be started
	for _, n := range programs {
		if err := os.WriteFile(filepath.Join(broken, n), []byte("not a program"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name    string
		opts    FindOptions
		version string
		dir     string
		setting bool
		route   string
	}{
		{"in PATH", FindOptions{Path: empty + ":" + bin, Extra: []string{}}, "", bin, false, ""},
		{"through the setting", FindOptions{Setting: homebrew, Path: bin}, "", homebrew, true, ""},
		{"in a Homebrew folder", FindOptions{Path: empty, Extra: []string{homebrew}}, "", homebrew, false, ""},
		{"22.02 is the oldest accepted", FindOptions{Path: bin, Extra: []string{}}, "22.02.0", bin, false, ""},
		{"too old", FindOptions{Path: bin, Extra: []string{}}, "21.12.0", "", false, "Find.PopplerTooOld"},
		{"missing", FindOptions{Path: empty, Extra: []string{}}, "", "", false, "Find.PopplerNotFound"},
		{"a setting that is not a folder with pdfinfo", FindOptions{Setting: empty, Path: bin}, "", "", false, "Find.PopplerPathInvalid"},
		{"pdftotext and pdftohtml not beside pdfinfo", FindOptions{Path: partial, Extra: []string{}}, "", "", false, "Find.PopplerProgramMissing"},
		{"-v names no version", FindOptions{Path: bin, Extra: []string{}}, "none", "", false, "Find->queryVersion.PopplerVersionFailed"},
		{"-v cannot be run", FindOptions{Setting: broken}, "", "", false, "Find->queryVersion.PopplerVersionFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FAKE_POPPLER_VERSION", tt.version)
			p, err := Find(t.Context(), tt.opts)
			if tt.route != "" {
				route(t, err, tt.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Dir != tt.dir || p.FromSetting != tt.setting || p.Version == "" {
				t.Errorf("found %+v, want the folder %s, setting %v", p, tt.dir, tt.setting)
			}
		})
	}
}

func TestAVersionThatNeverAnswersEndsWithTheContext(t *testing.T) {
	bin := fakeDir(t)
	t.Setenv("FAKE_POPPLER_VERSION", "hang")
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Find(ctx, FindOptions{Path: bin, Extra: []string{}})
	route(t, err, "Find->queryVersion.PopplerVersionFailed")
	if time.Since(start) > 10*time.Second {
		t.Errorf("Find took %v", time.Since(start))
	}
}

func TestVersions(t *testing.T) {
	tests := []struct {
		output, want string
		ok           bool
	}{
		{"pdfinfo version 22.02.0\nCopyright 2005-2022 The Poppler Developers", "22.02.0", true},
		{"pdfinfo version 26.08.0\n", "26.08.0", true},
		{"pdfinfo version 0.86.1\nCopyright 2005-2020", "0.86.1", true},
		{"pdfinfo: not Poppler", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseVersion(tt.output)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseVersion(%q) = %q, %v, want %q, %v", tt.output, got, ok, tt.want, tt.ok)
		}
	}
	compares := []struct {
		a, b string
		want int
	}{
		{"22.02.0", "22.02", 0}, {"22.1", "22.02", -1}, {"0.86.1", "22.02", -1}, {"24.02.0", "22.02", 1}, {"22.02.1", "22.02", 1},
	}
	for _, tt := range compares {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersions(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestInfo(t *testing.T) {
	p := found(t, fakeDir(t))
	tests := []struct {
		name, pages, password, given string
		want                         []Page
		encrypted                    bool
	}{
		{"two letter pages", "", "", "", []Page{{612, 792}, {612, 792}}, false},
		{"a rotated page has its sides swapped", "612x792,842x595.5r90,842x595r180,100x50r-90", "", "",
			[]Page{{612, 792}, {595.5, 842}, {842, 595}, {50, 100}}, false},
		{"the password is given", "", "secret", "secret", []Page{{612, 792}, {612, 792}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FAKE_PDF_PAGES", tt.pages)
			t.Setenv("FAKE_PDF_PASSWORD", tt.password)
			doc, err := Info(t.Context(), p, "/w/a.pdf", tt.given)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(doc.Pages, tt.want) || doc.Encrypted != tt.encrypted {
				t.Errorf("got %+v, want %v (encrypted %v)", doc, tt.want, tt.encrypted)
			}
		})
	}
}

func TestInfoFails(t *testing.T) {
	bin := fakeDir(t)
	p := found(t, bin)
	tests := []struct {
		name, env, value, password, route, message string
	}{
		{"a password needed", "FAKE_PDF_PASSWORD", "secret", "", "Info.PDFPasswordNeeded", "This document is protected by a password."},
		{"a wrong password", "FAKE_PDF_PASSWORD", "secret", "guess", "Info.PDFPasswordWrong", "The password is not correct."},
		{"a damaged file", "FAKE_PDF_DAMAGED", "Couldn't read xref table", "", "Info.PDFDamaged", ""},
		{"no pages", "FAKE_PDF_PAGES", "0x0", "", "Info.PDFInfoInvalid", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, tt.value)
			_, err := Info(t.Context(), p, "/w/a.pdf", tt.password)
			route(t, err, tt.route)
			e, _ := errors.FromError(err)
			if tt.message != "" && e != nil && e.Message != tt.message {
				t.Errorf("message %q, want %q", e.Message, tt.message)
			}
			if tt.env == "FAKE_PDF_DAMAGED" && e != nil && e.Metadata["message"] != tt.value {
				t.Errorf("the message of pdfinfo is %v, want %q", e.Metadata["message"], tt.value)
			}
		})
	}
	t.Run("no pdfinfo", func(t *testing.T) {
		gone := p
		gone.Dir = t.TempDir()
		_, err := Info(t.Context(), gone, "/w/a.pdf", "")
		route(t, err, "Info.PopplerStartFailed")
	})
}

func TestParseInfo(t *testing.T) {
	tests := []struct {
		name, output string
		ok           bool
	}{
		{"pages without sizes", "Pages:  2\nPage    1 size:  612 x 792 pts\n", false},
		{"no Pages line", "Page    1 size:  612 x 792 pts\n", false},
		{"a size of a page that is not there", "Pages:  1\nPage    1 size:  612 x 792 pts\nPage    3 size:  612 x 792 pts\n", false},
		{"a size of zero", "Pages:  1\nPage    1 size:  0 x 792 pts\n", false},
		{"a title that looks like a line", "Title:  Pages:  9\nPages:  1\nPage    1 size:  612 x 792 pts (letter)\n", true},
	}
	for _, tt := range tests {
		if _, ok := parseInfo(tt.output); ok != tt.ok {
			t.Errorf("%s: ok %v, want %v", tt.name, ok, tt.ok)
		}
	}
}

func TestRender(t *testing.T) {
	bin := fakeDir(t)
	p := found(t, bin)
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_POPPLER_ARGS", args)
	t.Setenv("FAKE_PDF_PAGES", "612x792,842x595r90")
	t.Setenv("FAKE_PDF_PASSWORD", "secret")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	img, err := Render(t.Context(), p, "/w/a.pdf", "secret", 2, 36)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 298, 421) {
		t.Errorf("the page is %v, want 298 x 421 (595 x 842 points at 36 dpi)", got)
	}
	if r, _, _, _ := img.At(5, 5).RGBA(); r>>8 != 80 {
		t.Errorf("the gray of page 2 is %d, want 80", r>>8)
	}
	data, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(string(data), "\n")
	want := []string{"-f", "2", "-l", "2", "-r", "36.00", "-png", "-singlefile", "-upw", "secret", "/w/a.pdf"}
	if !slices.Equal(got[:len(want)], want) || !strings.HasPrefix(got[len(want)], tmp) {
		t.Errorf("arguments %q, want %q and a file in the temporary folder", got, want)
	}
	if left, err := os.ReadDir(tmp); err != nil || len(left) != 0 {
		t.Errorf("the temporary folder holds %v (%v)", left, err)
	}
}

func TestRenderFails(t *testing.T) {
	bin := fakeDir(t)
	p := found(t, bin)
	tests := []struct {
		name, env, value, password string
		page                       int
		route                      string
	}{
		{"a password needed", "FAKE_PDF_PASSWORD", "secret", "", 1, "Render.PDFPasswordNeeded"},
		{"a wrong password", "FAKE_PDF_PASSWORD", "secret", "guess", 1, "Render.PDFPasswordWrong"},
		{"a damaged file", "FAKE_PDF_DAMAGED", "Couldn't read xref table", "", 1, "Render.PDFRenderFailed"},
		{"a page that is not there", "", "", "", 9, "Render.PDFRenderFailed"},
		{"no picture written", "FAKE_PPM", "none", "", 1, "Render.PDFRenderInvalid"},
		{"a picture that is not a PNG", "FAKE_PPM", "garbage", "", 1, "Render.PDFRenderInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv(tt.env, tt.value)
			}
			_, err := Render(t.Context(), p, "/w/a.pdf", tt.password, tt.page, 72)
			route(t, err, tt.route)
		})
	}
	t.Run("no pdftoppm", func(t *testing.T) {
		gone := p
		gone.Dir = t.TempDir()
		_, err := Render(t.Context(), gone, "/w/a.pdf", "", 1, 72)
		route(t, err, "Render.PopplerStartFailed")
	})
	t.Run("no temporary folder", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "gone"))
		_, err := Render(t.Context(), p, "/w/a.pdf", "", 1, 72)
		route(t, err, "Render.PDFRenderTempFailed")
	})
	t.Run("a folder that cannot be removed", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root removes files of a read-only folder")
		}
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		t.Setenv("FAKE_PPM", "stuck")
		t.Cleanup(func() { // let the test remove its folder
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Error(err)
			}
			for _, e := range entries {
				if err := os.Chmod(filepath.Join(tmp, e.Name()), 0o755); err != nil {
					t.Error(err)
				}
			}
		})
		img, err := Render(t.Context(), p, "/w/a.pdf", "", 1, 72)
		route(t, err, "Render->Render.func1.PDFRenderCleanupFailed")
		if img != nil {
			t.Error("a picture came with the error")
		}
	})
}

// waitForPids waits for the two process ids the fake writes when it hangs.
func waitForPids(t *testing.T, file string) []int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(file); err == nil {
			var ids []int
			for f := range strings.FieldsSeq(string(data)) {
				id, _ := strconv.Atoi(f)
				ids = append(ids, id)
			}
			if len(ids) == 2 {
				return ids
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the fake did not start")
	return nil
}

func TestACancelledContextEndsTheProcessAndItsChildren(t *testing.T) {
	p := found(t, fakeDir(t))
	pids := filepath.Join(t.TempDir(), "pids")
	t.Setenv("FAKE_POPPLER_HANG", pids)
	for _, kind := range []string{"info", "render"} {
		t.Run(kind, func(t *testing.T) {
			if err := os.Remove(pids); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				var err error
				if kind == "info" {
					_, err = Info(ctx, p, "/w/a.pdf", "")
				} else {
					_, err = Render(ctx, p, "/w/a.pdf", "", 1, 72)
				}
				done <- err
			}()
			ids := waitForPids(t, pids)
			cancel()
			select {
			case err := <-done:
				if kind == "info" {
					route(t, err, "Info.PopplerStopped")
				} else {
					route(t, err, "Render.PopplerStopped")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the call did not end with its context")
			}
			deadline := time.Now().Add(10 * time.Second)
			for _, id := range ids {
				for alive(id) && time.Now().Before(deadline) {
					time.Sleep(5 * time.Millisecond)
				}
				if alive(id) {
					t.Errorf("process %d is still running", id)
				}
			}
		})
	}
}

func TestLastLine(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Syntax Error: Couldn't find trailer dictionary\nSyntax Error: Couldn't read xref table\n", "Couldn't read xref table"},
		{"I/O Error: Couldn't open file 'a.pdf': No such file or directory.\n\n", "Couldn't open file 'a.pdf': No such file or directory."},
		{"something else", "something else"},
	}
	for _, tt := range tests {
		if got := lastLine(tt.in); got != tt.want {
			t.Errorf("lastLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
