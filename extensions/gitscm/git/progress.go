package git

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// Progress is one line of the progress git prints on its error output when it is given --progress ("Receiving objects:  45% (9/20)").
type Progress struct {
	// Phase is what git is doing: "Counting objects", "Compressing objects", "Receiving objects", "Writing objects", "Resolving deltas",
	// "Unpacking objects", "Updating files", "Enumerating objects", "Checking out files".
	Phase string
	// Percent is the percentage of the phase that is done, 0 to 100; -1 for a phase that git counts without a percentage ("Enumerating
	// objects: 5").
	Percent int
	// Remote says that the line is the remote's ("remote: Counting objects: ...").
	Remote bool
}

// reProgress reads a progress line: an optional "remote: ", the phase, and its percentage or its count.
var reProgress = regexp.MustCompile(`^(remote: )?([A-Z][A-Za-z ]*?):\s+(?:(\d+)%|\d+)(?:\s|,|$)`)

// ParseProgress reads a line of --progress output. It reports false for a line that is not progress (a message, an error).
//
// Ported from VS Code: extensions/git/src/git.ts (Git.clone: the lines of onSpawn)
func ParseProgress(line string) (Progress, bool) {
	line = strings.TrimSpace(line)
	m := reProgress.FindStringSubmatch(line)
	if m == nil {
		return Progress{}, false
	}
	p := Progress{Phase: m[2], Percent: -1, Remote: m[1] != ""}
	switch p.Phase {
	case "Enumerating objects", "Counting objects", "Compressing objects", "Receiving objects", "Writing objects", "Resolving deltas",
		"Unpacking objects", "Updating files", "Checking out files", "Filtering content", "Checking connectivity":
	default:
		return Progress{}, false
	}
	if m[3] != "" {
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return Progress{}, false
		}
		p.Percent = min(n, 100)
	}
	return p, true
}

// Overall is the progress of the whole command as a percentage, 0 to 100, from the phase it is in: the counting and the compressing of
// the sender are the first fifth, the transfer the next 40 percent and the resolving of the deltas the rest; a push, which has no
// resolving, spends the transfer's share and the rest on writing. A line with no percentage says nothing about the whole (-1).
//
// Ported from VS Code: extensions/git/src/git.ts (Git.clone: totalProgress)
func (p Progress) Overall() int {
	if p.Percent < 0 {
		return -1
	}
	base, span := 0, 0
	switch p.Phase {
	case "Counting objects", "Enumerating objects":
		base, span = 0, 10
	case "Compressing objects":
		base, span = 10, 10
	case "Receiving objects", "Unpacking objects":
		base, span = 20, 40
	case "Writing objects":
		base, span = 20, 80
	case "Resolving deltas":
		base, span = 60, 40
	case "Updating files", "Checking out files":
		base, span = 90, 10
	default:
		return -1
	}
	return base + p.Percent*span/100
}

// progressWriter is the error output of a command that was given a Progress function: it splits what git writes at the carriage returns
// and the line feeds, hands the progress lines to the function and passes everything else on to next, so that the tail of the error
// output, which classifies a failure and fills the Git channel, holds git's messages and not thousands of updates of a percentage.
type progressWriter struct {
	next     *tailBuffer
	progress func(Progress)
	partial  []byte
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.partial = append(w.partial, p...)
	for {
		i := bytes.IndexAny(w.partial, "\r\n")
		if i < 0 {
			break
		}
		line := string(w.partial[:i])
		terminator := w.partial[i]
		w.partial = w.partial[i+1:]
		if terminator == '\r' && len(w.partial) > 0 && w.partial[0] == '\n' {
			w.partial = w.partial[1:]
		}
		w.line(line)
	}
	if len(w.partial) > maxProgressLine {
		w.line(string(w.partial))
		w.partial = nil
	}
	return len(p), nil
}

// maxProgressLine bounds a line that never ends.
const maxProgressLine = 64 << 10

// line passes one line on: progress to the function, the rest to the tail.
func (w *progressWriter) line(s string) {
	if pr, ok := ParseProgress(s); ok {
		w.progress(pr)
		return
	}
	if strings.TrimSpace(s) == "" {
		return
	}
	_, _ = w.next.Write([]byte(s + "\n"))
}

// flush passes the last line, which had no line feed, on.
func (w *progressWriter) flush() {
	if len(w.partial) > 0 {
		w.line(string(w.partial))
		w.partial = nil
	}
}
