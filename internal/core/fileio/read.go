package fileio

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// The thresholds of SPEC §6.4.
const (
	// DefaultMaxFileSize is the largest file that is opened: 256 MB.
	DefaultMaxFileSize int64 = 256 << 20
	// DefaultLargeFileSize is the size from which a file opens in large file mode: 20 MB.
	DefaultLargeFileSize int64 = 20 << 20
	// DefaultLargeFileLines is the number of lines from which a file opens in large file mode.
	DefaultLargeFileLines = 300000
)

const (
	// zeroByteWindow is how many bytes are looked at to decide that a file is
	// binary or BOM-less UTF-16 (VS Code's ZERO_BYTE_DETECTION_BUFFER_MAX_LEN).
	zeroByteWindow = 512
	// readChunk is the size of the reads, between which a cancelled context is noticed.
	readChunk = 256 << 10
)

// LineEnding is the end of line a file uses.
type LineEnding string

const (
	// LF is "\n".
	LF LineEnding = "\n"
	// CRLF is "\r\n".
	CRLF LineEnding = "\r\n"
)

// FileStat is what Emetos remembers of a file: when it changed, how large it was
// and its permissions. The file watcher compares it to ignore Emetos's own writes (SPEC §10.5).
type FileStat struct {
	ModTime time.Time
	Size    int64
	Mode    fs.FileMode
}

// Limits are the file size and line count thresholds of SPEC §6.4. A zero field
// takes the default; tests inject small ones.
type Limits struct {
	// MaxFileSize is the largest size, in bytes, that is read; a larger file fails with FileTooLarge.
	MaxFileSize int64
	// LargeFileSize is the size from which a file is a large file.
	LargeFileSize int64
	// LargeFileLines is the number of lines from which a file is a large file.
	LargeFileLines int
}

func (l Limits) withDefaults() Limits {
	if l.MaxFileSize == 0 {
		l.MaxFileSize = DefaultMaxFileSize
	}
	if l.LargeFileSize == 0 {
		l.LargeFileSize = DefaultLargeFileSize
	}
	if l.LargeFileLines == 0 {
		l.LargeFileLines = DefaultLargeFileLines
	}
	return l
}

// ReadOptions are the settings a read depends on.
type ReadOptions struct {
	// Encoding is the id of files.encoding, used when the file has no byte order
	// mark and nothing says otherwise. Empty means "utf8".
	Encoding string
	// ForceEncoding makes Encoding win over a byte order mark that says another
	// one: "Reopen with Encoding". A mark of the same kind is still removed.
	ForceEncoding bool
	// AutoGuess is files.autoGuessEncoding: for a file with no byte order mark
	// whose bytes are valid UTF-8, the encoding is UTF-8 whatever Encoding says.
	AutoGuess bool
	// AllowBinary reads a file that looks binary instead of failing with
	// FileBinary: "Open anyway".
	AllowBinary bool
	// DefaultLineEnding is the line ending of a file that has no line break at all.
	// Empty means LF.
	DefaultLineEnding LineEnding
	// Limits are the size and line thresholds; zero fields take the defaults.
	Limits Limits
}

// ReadResult is a file read as text.
type ReadResult struct {
	// Text is the content decoded, with every line ending turned into "\n".
	Text string
	// Encoding is the id of the encoding the file was read as. A file with a UTF-8
	// byte order mark is "utf8bom".
	Encoding string
	// HasBOM reports whether the file started with a byte order mark (which is not in Text).
	HasBOM bool
	// LineEnding is the file's line ending: the most common one when they are mixed.
	LineEnding LineEnding
	// Mixed reports that the file used more than one kind of line ending.
	Mixed bool
	// Binary reports that the file looked binary; it is only true when
	// AllowBinary was set, since otherwise the read fails.
	Binary bool
	// LargeFile reports that the file is in large file mode (SPEC §6.4).
	LargeFile bool
	// Stat is the file's FileStat when it was read.
	Stat FileStat
}

// Read reads a file as text (SPEC §6.3, §6.4). It fails with the tags
// FileNotFound, FilePermissionDenied, FileIsDirectory, FileNotRegular,
// FileReadFailed, FileTooLarge, FileBinary, EncodingUnknown and ReadCancelled.
func Read(ctx context.Context, path string, opts ReadOptions) (ReadResult, error) {
	limits := opts.Limits.withDefaults()
	configured := opts.Encoding
	if configured == "" {
		configured = UTF8
	}
	_, ok := LookupEncoding(configured)
	if !ok {
		return ReadResult{}, errors.New("EncodingUnknown", "there is no such encoding", "encoding", configured)
	}
	if err := ctx.Err(); err != nil {
		return ReadResult{}, errors.NewErr("ReadCancelled", err)
	}
	data, stat, err := readFile(ctx, path, limits.MaxFileSize)
	if err != nil {
		return ReadResult{}, err
	}

	res := ReadResult{Stat: stat}
	encID, bomLen := detectBOM(data)
	res.HasBOM = bomLen > 0
	binary := false
	switch {
	case opts.ForceEncoding:
		if bomLen > 0 && sameKind(encID, configured) {
			res.Encoding = encID
			data = data[bomLen:]
		} else {
			res.HasBOM = false
			res.Encoding = configured
		}
	case bomLen > 0:
		res.Encoding = encID
		data = data[bomLen:]
	default:
		var zeroEnc string
		zeroEnc, binary = detectByZeroBytes(data)
		switch {
		case zeroEnc != "":
			res.Encoding = zeroEnc
		case opts.AutoGuess && !binary && utf8.Valid(data):
			res.Encoding = UTF8
		default:
			res.Encoding = configured
		}
	}
	if binary && !opts.ForceEncoding {
		if !opts.AllowBinary {
			return ReadResult{}, errors.New("FileBinary", "the file looks binary", "path", path)
		}
		res.Binary = true
	}

	text, err := decode(res.Encoding, data)
	if err != nil {
		return ReadResult{}, err
	}
	res.Text, res.LineEnding, res.Mixed = normalizeLineEndings(text, opts.DefaultLineEnding)
	res.LargeFile = stat.Size >= limits.LargeFileSize || strings.Count(res.Text, "\n")+1 >= limits.LargeFileLines
	return res, nil
}

// sameKind reports whether two encoding ids differ only in whether the file has
// a byte order mark: "utf8" and "utf8bom" are the same kind.
func sameKind(a, b string) bool {
	if a == b {
		return true
	}
	return (a == UTF8 || a == UTF8BOM) && (b == UTF8 || b == UTF8BOM)
}

// readFile reads a whole file, checking the context between reads and refusing a
// file over maxSize.
func readFile(ctx context.Context, path string, maxSize int64) ([]byte, FileStat, error) {
	// Look at the path before opening it: opening a pipe would wait for a writer.
	if err := checkRegular(path, maxSize); err != nil {
		return nil, FileStat{}, err
	}
	f, err := os.Open(path)
	switch {
	case os.IsNotExist(err):
		return nil, FileStat{}, errors.NewErr("FileNotFound", err).SetMetadata("path", path)
	case os.IsPermission(err):
		return nil, FileStat{}, errors.NewErr("FilePermissionDenied", err).SetMetadata("path", path)
	case err != nil:
		return nil, FileStat{}, errors.NewErr("FileReadFailed", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, FileStat{}, errors.NewErr("FileReadFailed", err).SetMetadata("path", path)
	}
	buf := bytes.NewBuffer(make([]byte, 0, min(info.Size(), maxSize)+1))
	for {
		if err := ctx.Err(); err != nil {
			return nil, FileStat{}, errors.NewErr("ReadCancelled", err)
		}
		_, err := io.CopyN(buf, f, readChunk)
		if int64(buf.Len()) > maxSize {
			return nil, FileStat{}, errors.New("FileTooLarge", "the file grew past the limit while it was read", "path", path, "limit", maxSize)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, FileStat{}, errors.NewErr("FileReadFailed", err).SetMetadata("path", path)
		}
	}
	return buf.Bytes(), FileStat{ModTime: info.ModTime(), Size: info.Size(), Mode: info.Mode()}, nil
}

// checkRegular fails unless path is a regular file (a link to one is fine) no
// larger than maxSize.
func checkRegular(path string, maxSize int64) error {
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return errors.NewErr("FileNotFound", err).SetMetadata("path", path)
	case os.IsPermission(err):
		return errors.NewErr("FilePermissionDenied", err).SetMetadata("path", path)
	case err != nil:
		return errors.NewErr("FileReadFailed", err).SetMetadata("path", path)
	case info.IsDir():
		return errors.New("FileIsDirectory", "the path is a folder", "path", path)
	case !info.Mode().IsRegular():
		return errors.New("FileNotRegular", "the path is not a regular file", "path", path, "mode", info.Mode().String())
	case info.Size() > maxSize:
		return errors.New("FileTooLarge", "the file is larger than the limit", "path", path, "size", info.Size(), "limit", maxSize)
	}
	return nil
}

// detectBOM finds a byte order mark at the start of data: the encoding it says
// (VS Code's detectEncodingByBOMFromBuffer) and its length, or "" and 0.
func detectBOM(data []byte) (string, int) {
	switch {
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return UTF16BE, 2
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return UTF16LE, 2
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return UTF8BOM, 3
	}
	return "", 0
}

// detectByZeroBytes looks for NUL bytes in the first 512 bytes of a file with no
// byte order mark, as VS Code does: if every other byte is a NUL the file is
// taken for UTF-16 (little or big endian by which half), otherwise a NUL means
// the file is binary.
func detectByZeroBytes(data []byte) (encoding string, binary bool) {
	couldBeLE, couldBeBE, hasZero := true, true, false
	for i := 0; i < len(data) && i < zeroByteWindow; i++ {
		odd := i%2 == 1
		zero := data[i] == 0
		if zero {
			hasZero = true
		}
		if couldBeLE && (odd && !zero || !odd && zero) {
			couldBeLE = false
		}
		if couldBeBE && (odd && zero || !odd && !zero) {
			couldBeBE = false
		}
		if zero && !couldBeLE && !couldBeBE {
			break
		}
	}
	switch {
	case !hasZero:
		return "", false
	case couldBeLE:
		return UTF16LE, false
	case couldBeBE:
		return UTF16BE, false
	}
	return "", true
}

// decode turns bytes in the given encoding into UTF-8. Bytes that don't decode
// become U+FFFD.
func decode(id string, data []byte) (string, error) {
	enc, ok := LookupEncoding(id)
	if !ok {
		return "", errors.New("EncodingUnknown", "there is no such encoding", "encoding", id)
	}
	if enc.enc == nil { // UTF-8, with or without a mark that is already removed
		return toValidUTF8(data), nil
	}
	out, err := enc.enc.NewDecoder().Bytes(data)
	if err != nil {
		return "", errors.NewErr("FileReadFailed", err)
	}
	return string(out), nil
}

// toValidUTF8 returns data as a string with every invalid byte replaced by U+FFFD.
func toValidUTF8(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var sb strings.Builder
	sb.Grow(len(data))
	for len(data) > 0 {
		r, w := utf8.DecodeRune(data)
		sb.WriteRune(r)
		data = data[w:]
	}
	return sb.String()
}

// normalizeLineEndings turns "\r\n" and a lone "\r" into "\n" and tells which
// ending the text used. When the endings are mixed, the result is VS Code's
// choice: "\r\n" when more than half of the line breaks have a "\r" (a lone "\r"
// counts), else "\n". A text with no line break uses def (LF when empty).
func normalizeLineEndings(text string, def LineEnding) (string, LineEnding, bool) {
	if !strings.Contains(text, "\r") {
		lfs := strings.Count(text, "\n")
		if lfs == 0 {
			return text, orLF(def), false
		}
		return text, LF, false
	}
	var cr, lf, crlf int
	var sb strings.Builder
	sb.Grow(len(text))
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\r' && i+1 < len(text) && text[i+1] == '\n':
			crlf++
			sb.WriteByte('\n')
			i++
		case c == '\r':
			cr++
			sb.WriteByte('\n')
		case c == '\n':
			lf++
			sb.WriteByte('\n')
		default:
			sb.WriteByte(c)
		}
	}
	kinds := 0
	for _, n := range []int{cr, lf, crlf} {
		if n > 0 {
			kinds++
		}
	}
	total := cr + lf + crlf
	ending := LF
	if float64(cr+crlf) > float64(total)/2 {
		ending = CRLF
	}
	return sb.String(), ending, kinds > 1
}

func orLF(e LineEnding) LineEnding {
	if e == "" {
		return LF
	}
	return e
}
