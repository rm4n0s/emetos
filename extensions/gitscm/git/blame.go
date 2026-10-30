package git

import (
	"bytes"
	"io"
	"strings"
	"time"
)

// LineRange is a range of lines, counted from 1, both ends included.
type LineRange struct {
	Start, End int
}

// BlameCommit is a commit that is responsible for lines of a file, with the ranges of the lines (in the final file).
type BlameCommit struct {
	Hash        string
	Author      string
	AuthorEmail string
	AuthorTime  time.Time
	Summary     string
	Ranges      []LineRange
}

// BlameParser reads the output of "git blame --incremental" as a stream: Write it the chunks as they arrive, cut anywhere,
// and read Commits. A hunk is "<hash> <original line> <final line> <number of lines>", the author, author-mail, author-time and
// summary lines (only the first time git prints a commit), and "filename <path>", which ends the hunk. A hunk that is not
// complete yet waits for the next chunk. It implements io.Writer.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitBlame).
type BlameParser struct {
	rest    []byte
	order   []string
	commits map[string]*BlameCommit
	// the hunk being read
	hash                       string
	start, count               int
	author, mail, summary      string
	authorTime                 time.Time
	hasAuthor, hasMail, hasSum bool
}

// Write adds a chunk of the output.
func (p *BlameParser) Write(chunk []byte) (int, error) {
	data := append(p.rest, chunk...)
	for {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			break
		}
		p.line(strings.TrimSuffix(string(data[:nl]), "\r"))
		data = data[nl+1:]
	}
	p.rest = append([]byte(nil), data...)
	return len(chunk), nil
}

func (p *BlameParser) line(line string) {
	if p.hash == "" {
		f := strings.Split(line, " ")
		if len(f) == 4 && hashPattern.MatchString(f[0]) {
			p.hash, p.start, p.count = f[0], atoi(f[2]), atoi(f[3])
			p.author, p.mail, p.summary, p.authorTime = "", "", "", time.Time{}
			p.hasAuthor, p.hasMail, p.hasSum = false, false, false
		}
		return
	}
	switch {
	case strings.HasPrefix(line, "author "):
		p.author, p.hasAuthor = strings.TrimPrefix(line, "author "), true
	case strings.HasPrefix(line, "author-mail "):
		mail := strings.TrimPrefix(line, "author-mail ")
		p.mail, p.hasMail = strings.TrimSuffix(strings.TrimPrefix(mail, "<"), ">"), true
	case strings.HasPrefix(line, "author-time "):
		p.authorTime = time.Unix(int64(atoi(strings.TrimPrefix(line, "author-time "))), 0).UTC()
	case strings.HasPrefix(line, "summary "):
		p.summary, p.hasSum = strings.TrimPrefix(line, "summary "), true
	case strings.HasPrefix(line, "filename "):
		p.endHunk()
	}
}

func (p *BlameParser) endHunk() {
	if p.commits == nil {
		p.commits = map[string]*BlameCommit{}
	}
	c := p.commits[p.hash]
	if c == nil {
		c = &BlameCommit{Hash: p.hash, Author: p.author, AuthorEmail: p.mail, AuthorTime: p.authorTime, Summary: p.summary}
		p.commits[p.hash] = c
		p.order = append(p.order, p.hash)
	}
	if p.count > 0 {
		c.Ranges = append(c.Ranges, LineRange{Start: p.start, End: p.start + p.count - 1})
	}
	p.hash = ""
}

// Commits returns the commits read so far, in the order they first appeared, each with its ranges.
func (p *BlameParser) Commits() []BlameCommit {
	out := make([]BlameCommit, 0, len(p.order))
	for _, h := range p.order {
		out = append(out, *p.commits[h])
	}
	return out
}

// ParseBlame parses a whole output of "git blame --incremental".
func ParseBlame(data []byte) []BlameCommit {
	var p BlameParser
	_, _ = p.Write(data)
	return p.Commits()
}

// StreamBlame reads the output of git blame from r.
func StreamBlame(r io.Reader) ([]BlameCommit, error) {
	var p BlameParser
	if _, err := io.Copy(&p, r); err != nil {
		return nil, err
	}
	return p.Commits(), nil
}

// NameStatus is one file of "git diff --name-status -z".
type NameStatus struct {
	// Status is the letter: A added, M modified, D deleted, R renamed, C copied, T type changed, U unmerged.
	Status byte
	// Score is the similarity of a rename or a copy, 0 to 100; 0 for the others.
	Score int
	// Path is the path of the file (for a rename or a copy, the new path).
	Path string
	// OrigPath is the path a renamed or copied file came from.
	OrigPath string
}

// ParseNameStatus parses the output of "git diff --name-status -z": a status ("M", or "R100") and a path, or for a rename or
// a copy the old path and the new one, each ended by a NUL. A record that is cut short is dropped. Note that the old path
// comes first here, while "git status -z" prints the new path first.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitChanges).
func ParseNameStatus(data []byte) []NameStatus {
	segments := terminated(string(data))
	var out []NameStatus
	for i := 0; i < len(segments); {
		status := strings.TrimSpace(segments[i])
		if status == "" {
			i++
			continue
		}
		e := NameStatus{Status: status[0]}
		if len(status) > 1 {
			e.Score = atoi(status[1:])
		}
		if e.Status == 'R' || e.Status == 'C' {
			if i+2 >= len(segments) {
				break
			}
			e.OrigPath, e.Path = segments[i+1], segments[i+2]
			i += 3
		} else {
			if i+1 >= len(segments) {
				break
			}
			e.Path = segments[i+1]
			i += 2
		}
		out = append(out, e)
	}
	return out
}
