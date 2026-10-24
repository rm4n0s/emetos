package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// MaxProfileExportSize is the most bytes an exported profile file has, and the most the import reads (SPEC
// §10.12): 16 MiB.
const MaxProfileExportSize = 16 << 20

// ProfileExport is the content of an exported profile (SPEC §10.12): the one JSON file that Export/Import
// writes and reads, `{"name", "settings", "keybindings", "snippets": {file name: text}}`. The three kinds of
// content are the texts of the files of the profile; a file the profile does not have is the empty string (an
// empty snippets object). Only the snippet files at the top of the snippets folder with the extension .json or
// .code-snippets are exported.
type ProfileExport struct {
	// Name is the name of the profile that was exported; the importing side may use another one.
	Name string `json:"name"`
	// Settings is the text of settings.json.
	Settings string `json:"settings"`
	// Keybindings is the text of keybindings.json.
	Keybindings string `json:"keybindings"`
	// Snippets are the texts of the snippet files, by file name.
	Snippets map[string]string `json:"snippets"`
}

// snippetExtensions are the extensions of the snippet files the snippet service reads (core/snippet).
var snippetExtensions = []string{".json", ".code-snippets"}

// maxSnippetFileName is the longest file name (in bytes) the file systems of Linux take.
const maxSnippetFileName = 255

// ValidSnippetFileName tells whether name can be the name of a snippet file of an imported profile: the plain
// name of a file in the snippets folder, never a path. It is not empty, has no path separator, no NUL or other
// control character, is neither "." nor "..", is valid UTF-8 of at most 255 bytes, has a name before its
// extension and ends in .json or .code-snippets (the files the snippet service reads).
func ValidSnippetFileName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > maxSnippetFileName || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	ext := filepath.Ext(name)
	if !slices.Contains(snippetExtensions, ext) || len(name) == len(ext) {
		return false
	}
	return filepath.Base(name) == name
}

// ParseProfileExport reads the text of an exported profile. The text is at most MaxProfileExportSize bytes of
// JSON: an object whose "name" is a valid profile name (ValidateName's rule) and whose "settings" and
// "keybindings" are strings and "snippets" an object of strings, each key a file name that
// ValidSnippetFileName accepts. A missing "settings", "keybindings" or "snippets" (or a null) is empty; other
// keys are ignored. The error has the tags ProfileImportTooLarge, ProfileImportInvalid and
// ProfileImportSnippetInvalid.
func ParseProfileExport(data []byte) (ProfileExport, error) {
	if len(data) > MaxProfileExportSize {
		return ProfileExport{}, errors.New("ProfileImportTooLarge", "the profile file is larger than 16 MiB", "size", len(data))
	}
	var raw struct {
		Name        *string            `json:"name"`
		Settings    *string            `json:"settings"`
		Keybindings *string            `json:"keybindings"`
		Snippets    map[string]*string `json:"snippets"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return ProfileExport{}, errors.NewErr("ProfileImportInvalid", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return ProfileExport{}, errors.New("ProfileImportInvalid", "the profile file has text after its JSON value")
	}
	if raw.Name == nil {
		return ProfileExport{}, errors.New("ProfileImportInvalid", "the profile file has no name")
	}
	if !ValidProfileName(*raw.Name) {
		return ProfileExport{}, errors.New("ProfileImportInvalid",
			"the name of the profile has to be 1 to 64 letters, digits, spaces, - or _, and no space at its start or end", "name", *raw.Name)
	}
	out := ProfileExport{Name: *raw.Name, Snippets: map[string]string{}}
	if raw.Settings != nil {
		out.Settings = *raw.Settings
	}
	if raw.Keybindings != nil {
		out.Keybindings = *raw.Keybindings
	}
	for name, text := range raw.Snippets {
		if !ValidSnippetFileName(name) {
			return ProfileExport{}, errors.New("ProfileImportSnippetInvalid",
				"a snippet file of the profile has a name that is not a plain file name ending in .json or .code-snippets", "file", name)
		}
		out.Snippets[name] = ""
		if text != nil {
			out.Snippets[name] = *text
		}
	}
	return out, nil
}

// Marshal gives the text of the file of the export: indented JSON with the keys in the order of the struct and
// the snippet files sorted by name, ending in a line break. The error has the tags ProfileExportTooLarge.
func (e ProfileExport) Marshal() ([]byte, error) {
	if e.Snippets == nil {
		e.Snippets = map[string]string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	if err := enc.Encode(e); err != nil {
		// a struct of strings always encodes
		return nil, errors.NewErr("ProfileExportFailed", err)
	}
	if buf.Len() > MaxProfileExportSize {
		return nil, errors.New("ProfileExportTooLarge", "the profile is larger than 16 MiB", "size", buf.Len())
	}
	return buf.Bytes(), nil
}

// readLimited reads a file of at most MaxProfileExportSize bytes. tooLarge is the error for a larger one.
func readLimited(path string) (data []byte, tooLarge bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	data, err = io.ReadAll(io.LimitReader(f, MaxProfileExportSize+1))
	if err != nil {
		return nil, false, err
	}
	return data, len(data) > MaxProfileExportSize, nil
}

// Export reads the settings, the keybindings and the snippet files of a profile (the default profile is ""
// or "Default"). A file the profile does not have is empty; of the snippets folder only the regular files at its
// top with a name ValidSnippetFileName accepts are exported (folders, links and other files are left out). The
// text of every file has to be valid UTF-8. The error has the tags ProfileNotFound, ProfileExportFailed,
// ProfileExportUnsupported and ProfileExportTooLarge.
func (p *Profiles) Export(ctx context.Context, profile string) (ProfileExport, error) {
	profile, err := p.resolve(ctx, profile)
	if err != nil {
		return ProfileExport{}, err
	}
	dir, err := p.paths.ConfigDir(profile)
	if err != nil {
		return ProfileExport{}, err
	}
	out := ProfileExport{Name: ProfileDisplayName(profile), Snippets: map[string]string{}}
	total := 0
	read := func(path string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", errors.NewErr("ProfileExportFailed", err).SetMetadata("path", path)
		}
		data, tooLarge, err := readLimited(path)
		switch {
		case err != nil && os.IsNotExist(err):
			return "", nil
		case err != nil:
			return "", errors.NewErr("ProfileExportFailed", err).SetMetadata("path", path)
		case tooLarge:
			return "", errors.New("ProfileExportTooLarge", "a file of the profile is larger than 16 MiB", "path", path)
		case !utf8.Valid(data):
			return "", errors.New("ProfileExportUnsupported", "a file of the profile is not valid UTF-8 text", "path", path)
		}
		if total += len(data); total > MaxProfileExportSize {
			return "", errors.New("ProfileExportTooLarge", "the profile is larger than 16 MiB", "path", path)
		}
		return string(data), nil
	}
	if out.Settings, err = read(filepath.Join(dir, "settings.json")); err != nil {
		return ProfileExport{}, err
	}
	if out.Keybindings, err = read(filepath.Join(dir, "keybindings.json")); err != nil {
		return ProfileExport{}, err
	}
	snippets := filepath.Join(dir, "snippets")
	entries, err := os.ReadDir(snippets)
	if err != nil && !os.IsNotExist(err) {
		return ProfileExport{}, errors.NewErr("ProfileExportFailed", err).SetMetadata("path", snippets)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !ValidSnippetFileName(e.Name()) {
			continue
		}
		if out.Snippets[e.Name()], err = read(filepath.Join(snippets, e.Name())); err != nil {
			return ProfileExport{}, err
		}
	}
	return out, nil
}

// Import makes the profile name from the content of an export, whole or not at all (the folder is built under a
// temporary name and renamed into place, as Create does), and gives the name. name follows the rules of Create
// (ProfileNameInvalid, ProfileNameReserved, ProfileExists); export.Name is not used. An empty settings or
// keybindings text makes no file. Every snippet file name has to pass ValidSnippetFileName, so that no file is
// written outside the snippets folder. The error has those tags and ProfileImportSnippetInvalid and
// ProfileImportFailed.
func (p *Profiles) Import(ctx context.Context, export ProfileExport, name string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkNewName(ctx, name, ""); err != nil {
		return "", err
	}
	names := make([]string, 0, len(export.Snippets))
	for file := range export.Snippets {
		if !ValidSnippetFileName(file) {
			return "", errors.New("ProfileImportSnippetInvalid",
				"a snippet file of the profile has a name that is not a plain file name ending in .json or .code-snippets", "file", file)
		}
		names = append(names, file)
	}
	slices.Sort(names)
	return p.build(name, "ProfileImportFailed", func(tmp string) error {
		write := func(rel, text string) error {
			if err := ctx.Err(); err != nil {
				return errors.NewErr("ProfileImportFailed", err).SetMetadata("file", rel)
			}
			path := filepath.Join(tmp, rel)
			if err := p.opts.WriteFile(path, []byte(text), 0o644); err != nil {
				return errors.NewErr("ProfileImportFailed", err).SetMetadata("file", rel)
			}
			return nil
		}
		for _, f := range []struct{ rel, text string }{{"settings.json", export.Settings}, {"keybindings.json", export.Keybindings}} {
			if f.text == "" {
				continue
			}
			if err := write(f.rel, f.text); err != nil {
				return err
			}
		}
		if len(names) > 0 {
			if err := os.Mkdir(filepath.Join(tmp, "snippets"), 0o755); err != nil {
				return errors.NewErr("ProfileImportFailed", err).SetMetadata("file", "snippets")
			}
		}
		for _, file := range names {
			if err := write(filepath.Join("snippets", file), export.Snippets[file]); err != nil {
				return err
			}
		}
		return nil
	})
}

// FreeName gives a name for a new profile that is valid and not taken, starting from base (the name of an
// imported profile): base itself when it is free, else base with " 2", " 3", ... added (the end of base is cut so
// that the name has at most 64 characters). A base that is not a valid name is cleaned first: its characters
// that are not allowed become "-". The error has the tag of List and ProfileNameInvalid (an empty base).
func (p *Profiles) FreeName(ctx context.Context, base string) (string, error) {
	base = strings.TrimSpace(strings.Map(func(r rune) rune {
		if profileNameRune(r) {
			return r
		}
		return '-'
	}, base))
	runes := []rune(base)
	if len(runes) > MaxProfileNameLength {
		base = strings.TrimSpace(string(runes[:MaxProfileNameLength]))
	}
	if err := ValidateName(base); err != nil {
		return "", err
	}
	taken := func(name string) (bool, error) {
		if isDefaultName(name) {
			return true, nil
		}
		_, found, err := p.Lookup(ctx, name)
		return found, err
	}
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			suffix := " " + strconv.Itoa(n)
			head := []rune(base)
			if len(head)+len(suffix) > MaxProfileNameLength {
				head = head[:MaxProfileNameLength-len(suffix)]
			}
			name = strings.TrimSpace(string(head)) + suffix
		}
		used, err := taken(name)
		if err != nil {
			return "", err
		}
		if !used {
			return name, nil
		}
	}
}

// WriteProfileExport writes an export to the file at path as ParseProfileExport reads it: the text is written to a
// temporary file in the same folder and renamed over path, so a failure leaves the old file. A file that exists
// keeps its permissions; a new one has 0644 (as the user's umask would make a file a text editor saves). The error
// has the tags ProfileExportTooLarge and ProfileExportWriteFailed.
func WriteProfileExport(ctx context.Context, path string, e ProfileExport) (err error) {
	data, err := e.Marshal()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.NewErr("ProfileExportWriteFailed", err).SetMetadata("path", path)
	}
	perm := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		perm = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".profile-*.tmp")
	if err != nil {
		return errors.NewErr("ProfileExportWriteFailed", err).SetMetadata("path", path)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	steps := []func() error{
		func() error { return tmp.Chmod(perm) },
		func() error { _, werr := tmp.Write(data); return werr },
		tmp.Sync,
		tmp.Close,
		func() error { return os.Rename(tmp.Name(), path) },
	}
	for _, step := range steps {
		if serr := step(); serr != nil {
			return errors.NewErr("ProfileExportWriteFailed", serr).SetMetadata("path", path)
		}
	}
	return nil
}

// ReadProfileExport reads and parses the file of an exported profile. The error has the tags
// ProfileImportReadFailed and those of ParseProfileExport.
func ReadProfileExport(ctx context.Context, path string) (ProfileExport, error) {
	if err := ctx.Err(); err != nil {
		return ProfileExport{}, errors.NewErr("ProfileImportReadFailed", err).SetMetadata("path", path)
	}
	data, tooLarge, err := readLimited(path)
	if err != nil {
		return ProfileExport{}, errors.NewErr("ProfileImportReadFailed", err).SetMetadata("path", path)
	}
	if tooLarge {
		return ProfileExport{}, errors.New("ProfileImportTooLarge", "the profile file is larger than 16 MiB", "path", path)
	}
	return ParseProfileExport(data)
}
