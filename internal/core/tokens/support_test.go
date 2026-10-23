package tokens

import "github.com/rm4n0s/emetos/internal/core/fileio"

// readResult is what reading a file gives, for a document in tests.
func readResult(text string, large bool) fileio.ReadResult {
	return fileio.ReadResult{Text: text, Encoding: fileio.UTF8, LineEnding: fileio.LF, LargeFile: large}
}
