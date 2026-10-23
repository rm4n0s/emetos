package editor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/settings"
)

// Ported from VS Code: src/vs/workbench/browser/parts/editor/breadcrumbsModel.ts (the file path part)

// BreadcrumbsOptions are the settings of the breadcrumbs bar.
type BreadcrumbsOptions struct {
	// Enabled is breadcrumbs.enabled: whether the bar is shown.
	Enabled bool
	// FilePath is breadcrumbs.filePath: "on" shows the whole path, "last" only the file's name and
	// "off" no path.
	FilePath string
	// Icons is breadcrumbs.icons: whether the items have icons.
	Icons bool
}

// BreadcrumbsOptionsFromSettings reads the breadcrumbs settings.
func BreadcrumbsOptionsFromSettings(store *settings.Store) BreadcrumbsOptions {
	return BreadcrumbsOptions{
		Enabled:  settings.Get[bool](store, "breadcrumbs.enabled", ""),
		FilePath: settings.Get[string](store, "breadcrumbs.filePath", ""),
		Icons:    settings.Get[bool](store, "breadcrumbs.icons", ""),
	}
}

// BreadcrumbItem is one segment of the path of a file: a folder or the file.
type BreadcrumbItem struct {
	// Name is what the bar shows.
	Name string
	// Path is the absolute path of the folder or file the segment stands for.
	Path string
	// Dir tells a folder from the file.
	Dir bool
}

// BreadcrumbItems are the segments of the path of a file, from the outermost folder to the file.
// With a root (the folder that is open) that contains the file, the first segment is the root,
// named as its folder is, and the others are relative to it; otherwise the segments are the
// parts of the absolute path. mode is breadcrumbs.filePath: "on" gives all of them, "last" only
// the file, anything else none. A relative or empty path has none.
func BreadcrumbItems(file, root, mode string) []BreadcrumbItem {
	if mode != "on" && mode != "last" || file == "" || !filepath.IsAbs(file) {
		return nil
	}
	file = filepath.Clean(file)
	sep := string(filepath.Separator)
	var items []BreadcrumbItem
	base, rel := sep, strings.TrimPrefix(file, sep)
	if root != "" && filepath.IsAbs(root) {
		root = filepath.Clean(root)
		if r, err := filepath.Rel(root, file); err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+sep) {
			items = append(items, BreadcrumbItem{Name: filepath.Base(root), Path: root, Dir: true})
			base, rel = root, r
		}
	}
	parts := strings.Split(rel, sep)
	path := base
	for i, part := range parts {
		if part == "" {
			continue
		}
		path = filepath.Join(path, part)
		items = append(items, BreadcrumbItem{Name: part, Path: path, Dir: i < len(parts)-1})
	}
	if mode == "last" && len(items) > 1 {
		items = items[len(items)-1:]
	}
	return items
}

// BreadcrumbEntry is a file or folder that a breadcrumbs picker lists.
type BreadcrumbEntry struct {
	// Name is the name of the entry in its folder.
	Name string
	// Path is its absolute path.
	Path string
	// Dir tells a folder (or a link to one) from a file.
	Dir bool
}

// ReadSiblings lists a folder for a breadcrumbs picker: folders first, then files, each in
// natural order ignoring case (the explorer's default order). A link is a folder when what it
// points at is one. The read stops when ctx is cancelled.
func ReadSiblings(ctx context.Context, dir string) ([]BreadcrumbEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("BreadcrumbsReadCancelled", err).SetMetadata("dir", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.NewErr("BreadcrumbsReadDirFailed", err).SetMetadata("dir", dir)
	}
	out := make([]BreadcrumbEntry, 0, len(entries))
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, errors.NewErr("BreadcrumbsReadCancelled", err).SetMetadata("dir", dir)
		}
		path := filepath.Join(dir, e.Name())
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(path); err == nil {
				isDir = st.IsDir()
			}
		}
		out = append(out, BreadcrumbEntry{Name: e.Name(), Path: path, Dir: isDir})
	}
	slices.SortStableFunc(out, func(a, b BreadcrumbEntry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return compareNames(a.Name, b.Name)
	})
	return out, nil
}

// compareNames orders file names ignoring case, with runs of digits compared as numbers
// ("file2" before "file10"); names that only differ in case are ordered by their bytes.
func compareNames(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na, nb := strings.TrimLeft(string(ra[si:i]), "0"), strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		la, lb := unicode.ToLower(ra[i]), unicode.ToLower(rb[j])
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	if c := (len(ra) - i) - (len(rb) - j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}
