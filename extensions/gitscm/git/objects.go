package git

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// LsTreeEntry is one entry of "git ls-tree -l".
type LsTreeEntry struct {
	Mode   string
	Type   string
	Object string
	// Size is the size in bytes as git prints it: a number, or "-" for a tree.
	Size string
	Path string
}

// ParseLsTree parses the output of "git ls-tree -l -z": records "<mode> <type> <object> <size>\t<path>" ended by a NUL. The
// fields are separated by spaces, padded; the path is everything after the tab. VS Code reads the output without -z, one
// line a record; with -z a path with a newline is right.
//
// Ported from VS Code: extensions/git/src/git.ts (parseLsTree).
func ParseLsTree(data []byte) []LsTreeEntry {
	var out []LsTreeEntry
	for _, rec := range terminated(string(data)) {
		meta, path, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 {
			continue
		}
		out = append(out, LsTreeEntry{Mode: f[0], Type: f[1], Object: f[2], Size: f[3], Path: path})
	}
	return out
}

// LsFilesEntry is one entry of "git ls-files --stage".
type LsFilesEntry struct {
	Mode   string
	Object string
	// Stage is "0" for a normal entry and "1", "2", "3" for the base, ours and theirs of a conflict.
	Stage string
	Path  string
}

// ParseLsFiles parses the output of "git ls-files --stage -z": records "<mode> <object> <stage>\t<path>" ended by a NUL.
//
// Ported from VS Code: extensions/git/src/git.ts (parseLsFiles).
func ParseLsFiles(data []byte) []LsFilesEntry {
	var out []LsFilesEntry
	_ = StreamLsFiles(bytes.NewReader(data), func(e LsFilesEntry) error { out = append(out, e); return nil })
	return out
}

// StreamLsFiles reads the output of "git ls-files --stage -z" from r and calls fn for each entry; when fn returns an error
// the reading stops and the error is returned. A record that is cut short is dropped.
func StreamLsFiles(r io.Reader, fn func(LsFilesEntry) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		rec, err := br.ReadString(0)
		if err != nil && err != io.EOF {
			return err
		}
		if strings.HasSuffix(rec, "\x00") {
			meta, path, ok := strings.Cut(strings.TrimSuffix(rec, "\x00"), "\t")
			f := strings.Fields(meta)
			if ok && len(f) == 3 {
				if ferr := fn(LsFilesEntry{Mode: f[0], Object: f[1], Stage: f[2], Path: path}); ferr != nil {
					return ferr
				}
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

// ParseIgnored reads the output of "git check-ignore -v -z --stdin": for each path that matches, four NUL-terminated fields
// (the file with the pattern, its line, the pattern, the path). It returns the paths that are ignored: those whose pattern is
// not negated (a pattern that starts with "!" un-ignores the path).
//
// Ported from VS Code: extensions/git/src/repository.ts (parseIgnoreCheck).
func ParseIgnored(data []byte) []string {
	fields := strings.Split(string(data), "\x00")
	var out []string
	for i := 0; i+4 < len(fields); i += 4 { // the last field must be terminated: another element follows it
		pattern, path := fields[i+2], fields[i+3]
		if pattern != "" && !strings.HasPrefix(pattern, "!") {
			out = append(out, path)
		}
	}
	return out
}

// terminated splits s at NULs and returns the records that a NUL ended; the text after the last NUL (empty for a complete
// output, the start of a record for a stream cut short) is not a record.
func terminated(s string) []string {
	parts := strings.Split(s, "\x00")
	return parts[:len(parts)-1]
}
