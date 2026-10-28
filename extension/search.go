package extension

// The search across files (SPEC §10.27, D29): the engine is Emetos's own (internal/core/search), and an extension reaches it
// with Workspace().FindTextInFiles, after VS Code's workspace.findTextInFiles, and replaces with Workspace().NewTextReplacer
// and ReplaceInFiles.

// TextSearchQuery says what to look for (VS Code's TextSearchQuery).
type TextSearchQuery struct {
	// Pattern is the text, or the regular expression (JavaScript's syntax) when IsRegExp is set. It must not be empty.
	Pattern string
	// IsRegExp says that Pattern is a regular expression.
	IsRegExp bool
	// IsCaseSensitive matches case; IsWordMatch finds whole words only.
	IsCaseSensitive, IsWordMatch bool
	// IsSmartCase (search.smartCase) makes the search case sensitive when Pattern has an uppercase letter.
	IsSmartCase bool
	// IsMultiline lets the search match across lines; it is on anyway for a Pattern with a line break and for a regular
	// expression that can match one.
	IsMultiline bool
}

// TextSearchOptions say where to look and what to leave out. The zero value searches the whole folder of the workspace with
// the settings the user has (files.exclude, search.exclude, the ignore files, symbolic links, files.encoding).
type TextSearchOptions struct {
	// Include and Exclude are the text of the "files to include" and "files to exclude" boxes of the Search view: globs
	// separated by commas, where "./src" is relative to the folder and "src" is a folder or file of that name anywhere.
	Include, Exclude string
	// DisregardExcludeSettings leaves files.exclude and search.exclude out of the exclusions, and DisregardIgnoreFiles the
	// .gitignore and .ignore files (the Search view's "Use Exclude Settings and Ignore Files" toggle turns both).
	DisregardExcludeSettings, DisregardIgnoreFiles bool
	// OnlyOpenEditors searches the files that editors have open, with their unsaved text, and nothing else.
	OnlyOpenEditors bool
	// MaxResults is the most matches to report; 20000 (the default of search.maxResults) when not positive.
	MaxResults int
}

// TextSearchMatch is a match. Lines and columns count from 0, a column counts the bytes of the UTF-8 line (Position).
type TextSearchMatch struct {
	// Range is where the match is.
	Range Range
	// Preview is the text of the line where the match starts (cut around the match when the line is very long).
	Preview string
	// PreviewStart and PreviewEnd are the match in Preview, in bytes; a match that goes on to the next lines ends at the end
	// of Preview.
	PreviewStart, PreviewEnd int
	// Text is the matched text, with "\n" for each line break.
	Text string
}

// TextSearchFile is the matches of one file.
type TextSearchFile struct {
	// Path is the path relative to the folder, with forward slashes, and URI the file as file:///path/to/file.
	Path, URI string
	// Matches are the matches in the order of the text.
	Matches []TextSearchMatch
}

// TextSearchSummary says how a search went.
type TextSearchSummary struct {
	// Files is the number of files with matches and Matches the number of matches.
	Files, Matches int
	// LimitHit is true when the search stopped at TextSearchOptions.MaxResults.
	LimitHit bool
	// Searched is the number of files that were read; Binary, TooLarge and Unreadable count the files that were left out
	// because they look binary, are too large (over 64 MiB) or could not be read.
	Searched, Binary, TooLarge, Unreadable int
}

// TextReplacer makes the replacement of a match (see Workspace.NewTextReplacer).
type TextReplacer interface {
	// Replace is the replacement for the matched text (TextSearchMatch.Text). It is not safe for concurrent use.
	Replace(matchText string) string
}

// TextReplacement replaces a range of a file with text (ReplaceInFiles).
type TextReplacement struct {
	// Range is the replaced range, as the search reported it.
	Range Range
	// Expected is the text the range has now (TextSearchMatch.Text): when the file has other text there, the file changed
	// since the search and the replacement is skipped.
	Expected string
	// Text is the replacement.
	Text string
}

// FileReplacement is the replacements of one file that no editor has open.
type FileReplacement struct {
	// Path is the path relative to the folder, with forward slashes.
	Path string
	// Replacements are the changes of the file; they must not overlap.
	Replacements []TextReplacement
}

// ReplaceFailure is a file that ReplaceInFiles could not change.
type ReplaceFailure struct {
	// Path is the path of the file and Message why it failed.
	Path, Message string
}

// ReplaceReport says what ReplaceInFiles did.
type ReplaceReport struct {
	// Replaced is the number of replacements made and Skipped the number that were stale (the file had other text there) or
	// invalid.
	Replaced, Skipped int
	// Files are the paths of the files that were written.
	Files []string
	// Failed are the files that could not be read or written (a binary file, a file that changed while it was replaced, a
	// read only file); they are left as they were.
	Failed []ReplaceFailure
}
