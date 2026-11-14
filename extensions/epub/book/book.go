// Package book reads EPUB books for the EPUB Reader (SPEC §10.37 "EPUB", D55): the container, the package document (the title, the
// authors, the language, the manifest and the spine), the table of contents (EPUB 3's navigation document, else EPUB 2's NCX) and the
// chapters, parsed leniently into blocks and inline runs for a reflowed reader. The book's CSS is not applied. Reading is bounded:
// entries over 64 MiB, a book over 1 GiB uncompressed, more than 10,000 entries and entries that inflate more than 200 times are
// refused; paths are cleaned and never leave the zip; a book whose encryption is more than fonts' obfuscation is protected by DRM and
// refused. It has no UI.
package book

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/rm4n0s/errors"
)

// The limits of a book.
const (
	MaxEntrySize  = 64 << 20
	MaxTotalSize  = 1 << 30
	MaxEntries    = 10000
	MaxRatio      = 200
	ratioFloor    = 1 << 20 // an entry this small is not looked at for its ratio
	containerPath = "META-INF/container.xml"
	encryptPath   = "META-INF/encryption.xml"
)

// TooLargeMessage is what a book over a limit says.
const TooLargeMessage = "This book is too large to open."

// DRMMessage is what a book protected by DRM says.
const DRMMessage = "This book is protected by DRM and cannot be opened."

// SpineItem is a chapter in the reading order: its path in the zip and whether it is in the linear order.
type SpineItem struct {
	Path   string
	ID     string
	Linear bool
}

// TOCEntry is an entry of the table of contents: its title, the chapter's path and the anchor in it ("" for its top), and the
// entries under it.
type TOCEntry struct {
	Title    string
	Path     string
	Anchor   string
	Children []TOCEntry
}

// Book is an open book. Close it when it is no longer read.
type Book struct {
	Title    string
	Authors  []string
	Language string
	Spine    []SpineItem
	TOC      []TOCEntry
	file     *os.File
	entries  map[string]*zip.File
}

// Open opens a book and reads its container, package document and table of contents. It fails with EPUBOpenFailed when the file
// cannot be read, EPUBNotZip, EPUBTooManyEntries, EPUBEntryTooLarge, EPUBTooLarge, EPUBZipBomb, EPUBPathInvalid for an entry whose
// path leaves the zip, EPUBProtected, EPUBNoContainer, EPUBContainerInvalid, EPUBPackageMissing, EPUBPackageInvalid and
// EPUBStopped when ctx ends.
func Open(ctx context.Context, file string) (*Book, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, errors.NewErr("EPUBOpenFailed", err).SetMetadata("path", file)
	}
	b, err := open(ctx, f, file)
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			if e, ok := errors.FromError(err); ok { // the failure that came first is the one told; the close is noted on it
				_ = e.SetMetadata("close", closeErr.Error()) // it gives back the same error, for chaining
			}
		}
		return nil, err
	}
	return b, nil
}

// open reads an opened file as a book.
func open(ctx context.Context, f *os.File, file string) (*Book, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, errors.NewErr("EPUBOpenFailed", err).SetMetadata("path", file)
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return nil, errors.NewErr("EPUBNotZip", err).SetMetadata("path", file)
	}
	if len(zr.File) > MaxEntries {
		return nil, errors.New("EPUBTooManyEntries", TooLargeMessage, "path", file, "entries", len(zr.File))
	}
	b := &Book{file: f, entries: map[string]*zip.File{}}
	var total uint64
	for _, e := range zr.File {
		name, ok := cleanZipPath(e.Name)
		if !ok {
			return nil, errors.New("EPUBPathInvalid", "This book has a file whose path leaves the book.", "path", file, "entry", e.Name)
		}
		if e.UncompressedSize64 > MaxEntrySize {
			return nil, errors.New("EPUBEntryTooLarge", TooLargeMessage, "path", file, "entry", e.Name, "size", e.UncompressedSize64)
		}
		if e.UncompressedSize64 > ratioFloor && e.UncompressedSize64 > MaxRatio*max(e.CompressedSize64, 1) {
			return nil, errors.New("EPUBZipBomb", TooLargeMessage, "path", file, "entry", e.Name, "size", e.UncompressedSize64,
				"compressed", e.CompressedSize64)
		}
		total += e.UncompressedSize64
		b.entries[name] = e
	}
	if total > MaxTotalSize {
		return nil, errors.New("EPUBTooLarge", TooLargeMessage, "path", file, "size", total)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("EPUBStopped", err).SetMetadata("path", file)
	}
	if enc, ok := b.entries[encryptPath]; ok {
		data, err := readEntry(enc)
		if err != nil {
			return nil, errors.NewErr("EPUBProtected", err).SetMetadata("path", file)
		}
		if protected(data) {
			return nil, errors.New("EPUBProtected", DRMMessage, "path", file)
		}
	}
	cont, ok := b.entries[containerPath]
	if !ok {
		return nil, errors.New("EPUBNoContainer", "This file is not an EPUB book: it has no META-INF/container.xml.", "path", file)
	}
	data, err := readEntry(cont)
	if err != nil {
		return nil, errors.NewErr("EPUBContainerInvalid", err).SetMetadata("path", file)
	}
	opfPath, ok := rootFile(data)
	if !ok {
		return nil, errors.New("EPUBContainerInvalid", "The container of this book names no package document.", "path", file)
	}
	opf, ok := b.entries[opfPath]
	if !ok {
		return nil, errors.New("EPUBPackageMissing", "The package document of this book is missing.", "path", file, "package", opfPath)
	}
	data, err = readEntry(opf)
	if err != nil {
		return nil, errors.NewErr("EPUBPackageInvalid", err).SetMetadata("path", file, "package", opfPath)
	}
	pkg, err := parsePackage(data)
	if err != nil {
		return nil, errors.NewErr("EPUBPackageInvalid", err).SetMetadata("path", file, "package", opfPath)
	}
	if len(pkg.spine) == 0 {
		return nil, errors.New("EPUBPackageInvalid", "The package document of this book has no chapters.", "path", file, "package", opfPath)
	}
	b.read(pkg, path.Dir(opfPath))
	return b, nil
}

// Close closes the file of the book.
func (b *Book) Close() error {
	if err := b.file.Close(); err != nil {
		return errors.NewErr("EPUBCloseFailed", err)
	}
	return nil
}

// cleanZipPath is an entry's path, cleaned; false when it leaves the zip (an absolute path, "..", a drive).
func cleanZipPath(name string) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", false
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// resolve is a reference of a document at dir (a relative path with an optional fragment) as a path in the zip and the fragment;
// false when it leaves the zip or is not a path in the book (a URL).
func resolve(dir, ref string) (string, string, bool) {
	if isURL(ref) {
		return "", "", false
	}
	p, frag, _ := strings.Cut(ref, "#")
	if unescaped, err := url.PathUnescape(p); err == nil {
		p = unescaped
	}
	if p == "" {
		return "", frag, true
	}
	if strings.HasPrefix(p, "/") {
		return "", "", false
	}
	full, ok := cleanZipPath(path.Join(dir, p))
	return full, frag, ok
}

// isURL tells whether a reference has a scheme ("https:", "mailto:").
func isURL(ref string) bool {
	i := strings.IndexByte(ref, ':')
	if i <= 0 {
		return false
	}
	for _, r := range ref[:i] {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !letter && (r < '0' || r > '9') && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return !strings.ContainsAny(ref[:i], "/#?")
}

// readEntry reads an entry, never more than MaxEntrySize (a zip may lie about its sizes).
func readEntry(e *zip.File) ([]byte, error) {
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(rc, MaxEntrySize+1))
	closeErr := rc.Close()
	switch {
	case readErr != nil:
		return nil, readErr
	case closeErr != nil:
		return nil, closeErr
	case len(data) > MaxEntrySize:
		return nil, errTooLarge
	}
	return data, nil
}

// inflatedError is an entry that inflates past MaxEntrySize, though its header said less; the callers wrap it.
type inflatedError struct{}

func (inflatedError) Error() string { return "an entry of the book inflates past 64 MiB" }

// errTooLarge is the inflatedError.
var errTooLarge = inflatedError{}

// fontObfuscation are the algorithms of encryption.xml that only hide fonts (the IDPF's and Adobe's): a book that uses no other is
// not protected.
var fontObfuscation = map[string]bool{
	"http://www.idpf.org/2008/embedding": true,
	"http://ns.adobe.com/pdf/enc#RC":     true,
}

// protected tells whether encryption.xml encrypts anything with another algorithm than the fonts' obfuscation.
func protected(data []byte) bool {
	d := lenientDecoder(data)
	for {
		tok, err := d.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "EncryptionMethod" {
			if !fontObfuscation[attr(se, "Algorithm")] {
				return true
			}
		}
	}
}

// rootFile is the path of the package document that container.xml names.
func rootFile(data []byte) (string, bool) {
	d := lenientDecoder(data)
	for {
		tok, err := d.Token()
		if err != nil {
			return "", false
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "rootfile" {
			if mt := attr(se, "media-type"); mt != "" && mt != "application/oebps-package+xml" {
				continue
			}
			p, ok := cleanZipPath(attr(se, "full-path"))
			return p, ok && p != "."
		}
	}
}

// lenientDecoder is a decoder of a book's XML and XHTML that forgives what books put in them: HTML's entities, the void elements of
// HTML left open, mismatched end tags and control characters.
func lenientDecoder(data []byte) *xml.Decoder {
	clean := bytes.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return ' '
		}
		return r
	}, data)
	d := xml.NewDecoder(bytes.NewReader(clean))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	return d
}

// attr is the value of an attribute by its local name.
func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// Resource reads a file of the book (an image) by its path in the zip. It fails with EPUBResourceMissing and EPUBResourceInvalid.
func (b *Book) Resource(p string) ([]byte, error) {
	e, ok := b.entries[p]
	if !ok {
		return nil, errors.New("EPUBResourceMissing", "This book has no file "+p+".", "entry", p)
	}
	data, err := readEntry(e)
	if err != nil {
		return nil, errors.NewErr("EPUBResourceInvalid", err).SetMetadata("entry", p)
	}
	return data, nil
}
