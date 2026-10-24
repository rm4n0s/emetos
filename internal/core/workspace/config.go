package workspace

import (
	"reflect"
	"slices"

	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// SortOrder is the value of explorer.sortOrder.
type SortOrder string

// The values of explorer.sortOrder.
const (
	// SortDefault sorts by name, folders before files.
	SortDefault SortOrder = "default"
	// SortMixed sorts by name, files interwoven with folders.
	SortMixed SortOrder = "mixed"
	// SortFilesFirst sorts by name, files before folders.
	SortFilesFirst SortOrder = "filesFirst"
	// SortType groups by extension, then sorts by name, folders before files.
	SortType SortOrder = "type"
	// SortModified sorts by the time of the last change, newest first, folders before files.
	SortModified SortOrder = "modified"
	// SortFoldersNestsFiles sorts by name, folders before files, and files with nested children
	// before the other files. File nesting is not built, so it is SortDefault.
	SortFoldersNestsFiles SortOrder = "foldersNestsFiles"
)

// Lexicographic is the value of explorer.sortOrderLexicographicOptions.
type Lexicographic string

// The values of explorer.sortOrderLexicographicOptions.
const (
	// LexDefault mixes upper and lower case names.
	LexDefault Lexicographic = "default"
	// LexUpper groups the names that start in upper case before the others.
	LexUpper Lexicographic = "upper"
	// LexLower groups the names that start in lower case before the others.
	LexLower Lexicographic = "lower"
	// LexUnicode sorts names by their code points.
	LexUnicode Lexicographic = "unicode"
)

// Config is what the settings say about the tree of a folder.
type Config struct {
	// SortOrder is explorer.sortOrder ("" is SortDefault).
	SortOrder SortOrder
	// Lexicographic is explorer.sortOrderLexicographicOptions ("" is LexDefault).
	Lexicographic Lexicographic
	// Reverse is explorer.sortOrderReverse.
	Reverse bool
	// CompactFolders is explorer.compactFolders: chains of folders with one folder in each are
	// shown as one element (see Workspace.CompactChain).
	CompactFolders bool
	// Exclude is files.exclude: the files and folders the tree leaves out.
	Exclude glob.Expression
}

// DefaultConfig is the configuration of the default settings.
func DefaultConfig() Config {
	return Config{
		SortOrder: SortDefault, Lexicographic: LexDefault, CompactFolders: true,
		Exclude: glob.ExpressionFromSetting(map[string]any{
			"**/.git": true, "**/.svn": true, "**/.hg": true, "**/.jj": true, "**/.DS_Store": true, "**/Thumbs.db": true,
		}),
	}
}

// ConfigFromSettings reads the settings of the tree.
func ConfigFromSettings(store *settings.Store) Config {
	return Config{
		SortOrder:      SortOrder(settings.Get[string](store, "explorer.sortOrder", "")),
		Lexicographic:  Lexicographic(settings.Get[string](store, "explorer.sortOrderLexicographicOptions", "")),
		Reverse:        settings.Get[bool](store, "explorer.sortOrderReverse", ""),
		CompactFolders: settings.Get[bool](store, "explorer.compactFolders", ""),
		Exclude:        glob.ExpressionFromSetting(settings.Get[map[string]any](store, "files.exclude", "")),
	}
}

// equal tells whether two configurations are the same.
func (c Config) equal(o Config) bool {
	return c.SortOrder == o.SortOrder && c.Lexicographic == o.Lexicographic && c.Reverse == o.Reverse &&
		c.CompactFolders == o.CompactFolders && reflect.DeepEqual(c.Exclude, o.Exclude)
}

// WatchSettings are the settings of the watcher of a folder.
type WatchSettings struct {
	// Exclude are the keys of files.watcherExclude whose value is true: paths or globs, relative
	// to the watched folder or absolute.
	Exclude []string
	// Include are the entries of files.watcherInclude: extra paths to watch in the folder.
	Include []string
}

// WatchSettingsFromSettings reads files.watcherExclude and files.watcherInclude.
func WatchSettingsFromSettings(store *settings.Store) WatchSettings {
	var out WatchSettings
	exclude := settings.Get[map[string]any](store, "files.watcherExclude", "")
	keys := make([]string, 0, len(exclude))
	for k, v := range exclude {
		if on, _ := v.(bool); on && k != "" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	out.Exclude = keys
	for _, p := range settings.Get[[]string](store, "files.watcherInclude", "") {
		if p != "" {
			out.Include = append(out.Include, p)
		}
	}
	return out
}
