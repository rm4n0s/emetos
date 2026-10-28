// Package search finds a string in the files of a folder: the engine of the Search view (SPEC §10.27, D29). It has no UI and
// uses no program of the machine (VS Code asks ripgrep; Emetos does the work itself).
//
// A [Query] says what to look for (VS Code's IPatternInfo: the text or pattern, case, whole word, regular expression, smart
// case) and [Options] where (the folder, the globs of the "files to include" and "files to exclude" boxes, files.exclude and
// search.exclude, the ignore files, symbolic links, the encoding, the limit, and the text of the editors that have unsaved
// changes). [Run] walks the folder on a goroutine, searches the files it finds on a worker per CPU (at most 8), and calls
// back, one file at a time, with the matches of each file, so the first results come while the walk goes on.
//
// What is matched:
//   - A literal (no regular expression, no line break) is found with a byte search: bytes.Index, or, for a case-insensitive
//     ASCII text, a search that looks for the first byte in both cases with bytes.IndexByte and compares the rest folding
//     ASCII. A text with non-ASCII letters that ignores case uses the regular expression.
//   - A regular expression is run by internal/core/regex with JavaScript's syntax (the syntax of VS Code's find). It runs line
//     by line, or over the whole file when the pattern can match a line break (find.IsMultilineRegexSource), as VS Code does.
//   - A file is text when it has no NUL in its first 8 KiB; a byte order mark decides UTF-8 or UTF-16, and files.encoding says
//     the rest. Invalid UTF-8 becomes U+FFFD, as the editor reads the file, so a position of a match is a position in the
//     document that opens (line from 0, column in bytes of the UTF-8 line).
//
// Ported from VS Code (in structure and behavior): src/vs/workbench/services/search/common/queryBuilder.ts (the include and
// exclude patterns, smart case), .../node/ripgrepTextSearchEngine.ts (what is matched and how) and
// src/vs/workbench/services/search/common/replace.ts (the replace string).
package search
