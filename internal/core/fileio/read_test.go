package fileio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func write(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRead(t *testing.T, data []byte, opts ReadOptions) ReadResult {
	t.Helper()
	res, err := Read(context.Background(), write(t, data), opts)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return res
}

func wantTag(t *testing.T, err error, route, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) || !appErr.HasRoute(tag) {
		t.Errorf("unexpected failure path: %s, want %s through %s", appErr.Route(), tag, route)
	}
}

func TestEncodingTable(t *testing.T) {
	all := Encodings()
	if len(all) != 42 {
		t.Errorf("%d encodings, want the 49 of VS Code minus the 7 x/text lacks", len(all))
	}
	seen := map[string]bool{}
	for i, e := range all {
		if e.ID == "" || e.LabelLong == "" || e.LabelShort == "" {
			t.Errorf("encoding %d has an empty field: %+v", i, e)
		}
		if seen[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		seen[e.ID] = true
		if i > 0 && e.Order <= all[i-1].Order {
			t.Errorf("%s is out of order", e.ID)
		}
		if e.EncodeOnly != (e.ID == UTF8BOM) {
			t.Errorf("%s: EncodeOnly = %v", e.ID, e.EncodeOnly)
		}
		if (e.enc == nil) != (e.ID == UTF8 || e.ID == UTF8BOM) {
			t.Errorf("%s: codec presence is wrong", e.ID)
		}
		got, ok := LookupEncoding(e.ID)
		if !ok || got.ID != e.ID {
			t.Errorf("LookupEncoding(%q) = %+v, %v", e.ID, got, ok)
		}
	}
	for _, dropped := range []string{"cp1125", "cp857", "big5hkscs", "iso885911", "koi8ru", "koi8t", "gb2312"} {
		if _, ok := LookupEncoding(dropped); ok {
			t.Errorf("%s should have been left out", dropped)
		}
	}
	// The slice is a copy.
	all[0].ID = "changed"
	if Encodings()[0].ID != UTF8 {
		t.Error("Encodings returned the table itself")
	}
}

// samples are words in the scripts the encodings cover; each encoding is tested
// with the first sample it can represent.
var samples = []string{
	"café €", "Ünïcödé çğış", "Привет", "Γειά", "שלום", "مرحبا", "日本語", "한국어", "中文", "สวัสดี", "ąčęł", "ĦĜ", "Ġħ",
	"café", "Ünïcödé", "ąčę", "ŵŷ", "æøå",
}

func sampleFor(t *testing.T, e Encoding) string {
	t.Helper()
	for _, s := range samples {
		if _, err := e.enc.NewEncoder().String(s); err == nil {
			return "line one " + s + "\nline two\n"
		}
	}
	t.Fatalf("%s: no sample can be encoded", e.ID)
	return ""
}

func TestReadEveryEncoding(t *testing.T) {
	for _, e := range Encodings() {
		if e.enc == nil {
			continue
		}
		t.Run(e.ID, func(t *testing.T) {
			text := sampleFor(t, e)
			raw, err := e.enc.NewEncoder().Bytes([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			// Reopened with the encoding, as UTF-16 without a mark can only be (see
			// TestUTF16WithoutMarkNeedsLatin1); the others are read as configured.
			res := mustRead(t, raw, ReadOptions{Encoding: e.ID, ForceEncoding: strings.HasPrefix(e.ID, "utf16")})
			if res.Text != text || res.Encoding != e.ID || res.HasBOM {
				t.Errorf("Read = %q as %s (BOM %v), want %q as %s", res.Text, res.Encoding, res.HasBOM, text, e.ID)
			}
		})
	}
}

func TestReadUTFWithAndWithoutBOM(t *testing.T) {
	const text = "héllo\n日本語 😀\n"
	// Without a mark UTF-16 is only recognized in the Latin-1 range.
	const latin = "héllo\nwörld\n"
	utf16 := func(order string, bom bool) []byte {
		e, _ := LookupEncoding("utf16" + order)
		src := text
		if !bom {
			src = latin
		}
		b, err := e.enc.NewEncoder().Bytes([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if !bom {
			return b
		}
		if order == "le" {
			return append([]byte{0xFF, 0xFE}, b...)
		}
		return append([]byte{0xFE, 0xFF}, b...)
	}
	tests := []struct {
		name    string
		data    []byte
		wantEnc string
		wantBOM bool
	}{
		{"utf8", []byte(text), UTF8, false},
		{"utf8 with a mark", append([]byte{0xEF, 0xBB, 0xBF}, text...), UTF8BOM, true},
		{"utf16le with a mark", utf16("le", true), UTF16LE, true},
		{"utf16be with a mark", utf16("be", true), UTF16BE, true},
		{"utf16le without a mark", utf16("le", false), UTF16LE, false},
		{"utf16be without a mark", utf16("be", false), UTF16BE, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := text
			if !tt.wantBOM && strings.HasPrefix(tt.wantEnc, "utf16") {
				want = latin
			}
			res := mustRead(t, tt.data, ReadOptions{})
			if res.Text != want || res.Encoding != tt.wantEnc || res.HasBOM != tt.wantBOM {
				t.Errorf("Read = %q as %s (BOM %v), want %q as %s (BOM %v)", res.Text, res.Encoding, res.HasBOM, want, tt.wantEnc, tt.wantBOM)
			}
		})
	}
}

// VS Code takes a file with NUL bytes and no mark for UTF-16 only when every
// other byte is a NUL, which is true of Latin-1 text and not of other scripts;
// such a file looks binary and is read by reopening it with the encoding.
func TestUTF16WithoutMarkNeedsLatin1(t *testing.T) {
	e, _ := LookupEncoding(UTF16LE)
	raw, err := e.enc.NewEncoder().Bytes([]byte("日本語 text\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Read(context.Background(), write(t, raw), ReadOptions{})
	wantTag(t, err, "Read", "FileBinary")
	res := mustRead(t, raw, ReadOptions{Encoding: UTF16LE, ForceEncoding: true})
	if res.Text != "日本語 text\n" {
		t.Errorf("reopened = %q", res.Text)
	}
}

func TestDetectBOM(t *testing.T) {
	tests := []struct {
		data []byte
		enc  string
		n    int
	}{
		{nil, "", 0}, {[]byte{0xFE}, "", 0}, {[]byte{0xFE, 0xFF}, UTF16BE, 2}, {[]byte{0xFF, 0xFE}, UTF16LE, 2},
		{[]byte{0xEF, 0xBB}, "", 0}, {[]byte{0xEF, 0xBB, 0xBF}, UTF8BOM, 3}, {[]byte{0xEF, 0xBB, 0xBE}, "", 0},
		{[]byte("abc"), "", 0},
	}
	for _, tt := range tests {
		if enc, n := detectBOM(tt.data); enc != tt.enc || n != tt.n {
			t.Errorf("detectBOM(% x) = %q, %d, want %q, %d", tt.data, enc, n, tt.enc, tt.n)
		}
	}
}

func TestDetectByZeroBytes(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		enc    string
		binary bool
	}{
		{"text", []byte("hello"), "", false},
		{"empty", nil, "", false},
		{"utf16 le", []byte{'a', 0, 'b', 0}, UTF16LE, false},
		{"utf16 be", []byte{0, 'a', 0, 'b'}, UTF16BE, false},
		{"nul among text", []byte("ab\x00cd"), "", true},
		{"nul first then text", []byte{0, 'a', 'b', 'c'}, "", true},
		{"binary header", []byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0}, "", true},
		{"nul after the window", append([]byte(strings.Repeat("a", 600)), 0), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if enc, binary := detectByZeroBytes(tt.data); enc != tt.enc || binary != tt.binary {
				t.Errorf("= %q, %v, want %q, %v", enc, binary, tt.enc, tt.binary)
			}
		})
	}
}

func TestBinary(t *testing.T) {
	data := []byte("ab\x00cd\n")
	_, err := Read(context.Background(), write(t, data), ReadOptions{})
	wantTag(t, err, "Read", "FileBinary")

	res := mustRead(t, data, ReadOptions{AllowBinary: true})
	if !res.Binary || res.Text != "ab\x00cd\n" {
		t.Errorf("Open anyway = %+v", res)
	}
	if res := mustRead(t, []byte("text only\n"), ReadOptions{AllowBinary: true}); res.Binary {
		t.Error("a text file was reported binary")
	}
	// A NUL after the first 512 bytes is not looked at, as in VS Code.
	late := append([]byte(strings.Repeat("a", 600)), 0, 'b')
	if res := mustRead(t, late, ReadOptions{}); res.Binary {
		t.Error("a NUL after the window made the file binary")
	}
}

func TestLineEndings(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		def   LineEnding
		text  string
		want  LineEnding
		mixed bool
	}{
		{"empty", "", "", "", LF, false},
		{"empty with a default", "", CRLF, "", CRLF, false},
		{"no break with a default", "abc", CRLF, "abc", CRLF, false},
		{"lf", "a\nb\n", "", "a\nb\n", LF, false},
		{"crlf", "a\r\nb\r\n", "", "a\nb\n", CRLF, false},
		{"lone cr", "a\rb\r", "", "a\nb\n", CRLF, false},
		{"mostly crlf", "a\r\nb\nc\r\n", "", "a\nb\nc\n", CRLF, true},
		{"mostly lf", "a\nb\r\nc\n", "", "a\nb\nc\n", LF, true},
		{"half and half is lf", "a\nb\r\n", "", "a\nb\n", LF, true},
		{"lone cr counts for crlf", "a\rb\rc\n", "", "a\nb\nc\n", CRLF, true},
		{"cr then lf apart", "a\r\n\nb", "", "a\n\nb", LF, true},
		{"trailing cr", "abc\r", "", "abc\n", CRLF, false},
		{"crlf crlf crlf", "\r\n\r\n\r\n", "", "\n\n\n", CRLF, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := mustRead(t, []byte(tt.in), ReadOptions{DefaultLineEnding: tt.def})
			if res.Text != tt.text || res.LineEnding != tt.want || res.Mixed != tt.mixed {
				t.Errorf("Read(%q) = %q, %q, mixed %v; want %q, %q, mixed %v", tt.in, res.Text, res.LineEnding, res.Mixed, tt.text, tt.want, tt.mixed)
			}
		})
	}
}

func TestInvalidUTF8(t *testing.T) {
	res := mustRead(t, []byte("a\xffb\xe6\x97"), ReadOptions{})
	if want := "a�b��"; res.Text != want {
		t.Errorf("Read = %q, want %q", res.Text, want)
	}
}

func TestAutoGuess(t *testing.T) {
	latin := []byte("caf\xe9\n")
	utf8Text := []byte("café\n")
	tests := []struct {
		name string
		data []byte
		opts ReadOptions
		want string
		enc  string
	}{
		{"off: configured encoding on utf8 bytes", utf8Text, ReadOptions{Encoding: "windows1252"}, "cafÃ©\n", "windows1252"},
		{"on: valid utf8 wins", utf8Text, ReadOptions{Encoding: "windows1252", AutoGuess: true}, "café\n", UTF8},
		{"on: invalid utf8 falls back to the setting", latin, ReadOptions{Encoding: "windows1252", AutoGuess: true}, "café\n", "windows1252"},
		{"on: invalid utf8 and utf8 setting", latin, ReadOptions{AutoGuess: true}, "caf�\n", UTF8},
		{"on: a mark still wins", append([]byte{0xEF, 0xBB, 0xBF}, utf8Text...), ReadOptions{Encoding: "windows1252", AutoGuess: true}, "café\n", UTF8BOM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := mustRead(t, tt.data, tt.opts)
			if res.Text != tt.want || res.Encoding != tt.enc {
				t.Errorf("Read = %q as %s, want %q as %s", res.Text, res.Encoding, tt.want, tt.enc)
			}
		})
	}
}

func TestForceEncoding(t *testing.T) {
	// A UTF-8 file with a mark, reopened as UTF-8: the mark stays a mark.
	res := mustRead(t, []byte("\xEF\xBB\xBFabc"), ReadOptions{Encoding: UTF8, ForceEncoding: true})
	if res.Text != "abc" || res.Encoding != UTF8BOM || !res.HasBOM {
		t.Errorf("= %+v", res)
	}
	// A UTF-16 LE file reopened as Windows 1252: nothing is a mark any more.
	res = mustRead(t, []byte{0xFF, 0xFE, 'a', 0}, ReadOptions{Encoding: "windows1252", ForceEncoding: true})
	if res.Text != "ÿþa\x00" || res.Encoding != "windows1252" || res.HasBOM {
		t.Errorf("= %q %s %v", res.Text, res.Encoding, res.HasBOM)
	}
	// A file that looks binary is read as the encoding asked for.
	res = mustRead(t, []byte{'a', 0, 'b', 0}, ReadOptions{Encoding: "windows1252", ForceEncoding: true})
	if res.Text != "a\x00b\x00" || res.Binary {
		t.Errorf("= %+v", res)
	}
	// A BOM-less UTF-16 file asked for as UTF-16 LE.
	res = mustRead(t, []byte{'a', 0, 'b', 0}, ReadOptions{Encoding: UTF16LE, ForceEncoding: true})
	if res.Text != "ab" || res.Encoding != UTF16LE {
		t.Errorf("= %+v", res)
	}
}

func TestLimits(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		lim   Limits
		large bool
		tag   string
	}{
		{"small", "a\nb\n", Limits{LargeFileSize: 100, LargeFileLines: 100}, false, ""},
		{"size at the large threshold", strings.Repeat("a", 100), Limits{LargeFileSize: 100, LargeFileLines: 1000}, true, ""},
		{"size just below", strings.Repeat("a", 99), Limits{LargeFileSize: 100, LargeFileLines: 1000}, false, ""},
		{"lines at the threshold", strings.Repeat("\n", 9), Limits{LargeFileSize: 1000, LargeFileLines: 10}, true, ""},
		{"lines just below", strings.Repeat("\n", 8), Limits{LargeFileSize: 1000, LargeFileLines: 10}, false, ""},
		{"crlf lines are counted after conversion", strings.Repeat("\r\n", 9), Limits{LargeFileSize: 1000, LargeFileLines: 10}, true, ""},
		{"at the maximum", strings.Repeat("a", 50), Limits{MaxFileSize: 50, LargeFileSize: 1000}, false, ""},
		{"over the maximum", strings.Repeat("a", 51), Limits{MaxFileSize: 50, LargeFileSize: 1000}, false, "FileTooLarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Read(context.Background(), write(t, []byte(tt.data)), ReadOptions{Limits: tt.lim})
			if tt.tag != "" {
				wantTag(t, err, "Read", tt.tag)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.LargeFile != tt.large {
				t.Errorf("LargeFile = %v, want %v", res.LargeFile, tt.large)
			}
		})
	}
	// With no limits given, the defaults of SPEC §6.4 apply.
	l := Limits{}.withDefaults()
	if l.MaxFileSize != 256<<20 || l.LargeFileSize != 20<<20 || l.LargeFileLines != 300000 {
		t.Errorf("defaults = %+v", l)
	}
}

func TestStat(t *testing.T) {
	path := write(t, []byte("hello\n"))
	when := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	res, err := Read(context.Background(), path, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stat.Size != 6 || !res.Stat.ModTime.Equal(when) || res.Stat.Mode.Perm() != 0o640 {
		t.Errorf("Stat = %+v", res.Stat)
	}
}

func TestReadFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("linked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	res, err := Read(context.Background(), link, ReadOptions{})
	if err != nil || res.Text != "linked\n" {
		t.Errorf("Read(link) = %q, %v", res.Text, err)
	}
}

func TestReadErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	denied := filepath.Join(dir, "denied")
	if err := os.WriteFile(denied, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		path string
		opts ReadOptions
		tag  string
		skip bool
	}{
		{"missing", context.Background(), missing, ReadOptions{}, "FileNotFound", false},
		{"folder", context.Background(), dir, ReadOptions{}, "FileIsDirectory", false},
		{"pipe", context.Background(), fifo, ReadOptions{}, "FileNotRegular", false},
		{"permission", context.Background(), denied, ReadOptions{}, "FilePermissionDenied", os.Geteuid() == 0},
		{"bad path", context.Background(), "a\x00b", ReadOptions{}, "FileReadFailed", false},
		{"unknown encoding", context.Background(), write(t, []byte("x")), ReadOptions{Encoding: "klingon"}, "EncodingUnknown", false},
		{"cancelled before", cancelled, write(t, []byte("x")), ReadOptions{}, "ReadCancelled", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip("running as root: permissions don't apply")
			}
			_, err := Read(tt.ctx, tt.path, tt.opts)
			wantTag(t, err, "Read", tt.tag)
		})
	}
}

// afterCalls is a context that reports cancellation once Err has been asked n times.
type afterCalls struct {
	context.Context
	n int
}

func (c *afterCalls) Err() error {
	if c.n--; c.n < 0 {
		return context.Canceled
	}
	return nil
}

func TestReadCancelledWhileReading(t *testing.T) {
	// 600 KiB takes three reads of 256 KiB; the context is cancelled on the third check.
	path := write(t, []byte(strings.Repeat("x", 600<<10)))
	_, err := Read(&afterCalls{Context: context.Background(), n: 3}, path, ReadOptions{})
	wantTag(t, err, "readFile", "ReadCancelled")
}

func TestReadLargeFileInChunks(t *testing.T) {
	text := strings.Repeat("a line of text\n", 100000) // 1.5 MB, several chunks
	res := mustRead(t, []byte(text), ReadOptions{})
	if res.Text != text {
		t.Error("a file read in several chunks differs")
	}
}
