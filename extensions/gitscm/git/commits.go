package git

import (
	"bytes"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CommitFormat is the --format of "git log -z" and "git show -s -z" that ParseCommits reads: hash, author name, author
// e-mail, author date, committer date (seconds since the epoch), parents, ref names and the raw message.
const CommitFormat = "%H%n%aN%n%aE%n%at%n%ct%n%P%n%D%n%B"

// ShortStat is the line "3 files changed, 2 insertions(+), 1 deletion(-)" of --shortstat.
type ShortStat struct {
	Files, Insertions, Deletions int
}

// CoAuthor is a "Co-authored-by: Name <email>" trailer of a message.
type CoAuthor struct {
	Name, Email string
}

// Commit is one commit read from "git log" or "git show".
type Commit struct {
	// Hash is the object name (40 or 64 hexadecimal characters).
	Hash string
	// Message is the whole message, with the one final newline removed.
	Message string
	// Parents are the hashes of the parents, none for a root commit.
	Parents []string
	// AuthorName, AuthorEmail and AuthorDate come from the author line, CommitDate from the committer line.
	AuthorName, AuthorEmail string
	AuthorDate, CommitDate  time.Time
	// RefNames are the names of %D: "HEAD -> main", "tag: v1", "origin/main"; none when the commit has no decoration.
	RefNames []string
	// ShortStat is set when the log was asked for --shortstat and the commit changed files.
	ShortStat *ShortStat
	// CoAuthors are the trailers of the message, without duplicates by e-mail.
	CoAuthors []CoAuthor
	// FilePath is the path of the file at this commit, set by ParseFileLog for a followed file.
	FilePath string
}

var (
	hashPattern      = regexp.MustCompile(`^[0-9a-f]{40}(?:[0-9a-f]{24})?$`)
	shortStatPattern = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)
	// finalStat is a whole shortstat line; the last one of a stream may lack its newline (VS Code's recordings do).
	finalStat       = regexp.MustCompile(`^(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?$`)
	coAuthorPattern = regexp.MustCompile(`(?im)^Co-authored-by:\s*(.+?)\s*<([^>]+)>\s*$`)
)

// CommitParser reads the output of "git log -z --format=CommitFormat [--shortstat]" as a stream. Each commit ends with a NUL;
// the shortstat of a commit follows that NUL, so a commit is handed to the callback when the next one starts (or at Close).
// A commit that is not complete (a stream cut short) is dropped; the shortstat of the last one is read only when its line is
// complete.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitCommits).
type CommitParser struct {
	emit    func(Commit) error
	buf     []byte
	pending *Commit
}

// NewCommitParser makes a CommitParser that calls emit for each commit. When emit returns an error, Write returns it and the
// parser is finished.
func NewCommitParser(emit func(Commit) error) *CommitParser {
	return &CommitParser{emit: emit}
}

// Write adds a chunk of the output.
func (p *CommitParser) Write(chunk []byte) (int, error) {
	p.buf = append(p.buf, chunk...)
	for {
		nul := bytes.IndexByte(p.buf, 0)
		if nul < 0 {
			break
		}
		token := string(p.buf[:nul])
		p.buf = p.buf[nul+1:]
		if err := p.token(token); err != nil {
			return len(chunk), err
		}
	}
	// Do not keep the consumed prefix alive.
	if len(p.buf) == 0 {
		p.buf = nil
	}
	return len(chunk), nil
}

// token handles the text between two NULs: the shortstat of the commit before, then the header and the message of a commit.
func (p *CommitParser) token(tok string) error {
	rest, stat := leadingShortStat(tok)
	if p.pending != nil {
		if stat != nil {
			p.pending.ShortStat = stat
		}
		if err := p.flush(); err != nil {
			return err
		}
	}
	c, ok := parseCommitRecord(rest)
	if ok {
		p.pending = &c
	}
	return nil
}

func (p *CommitParser) flush() error {
	c := p.pending
	p.pending = nil
	if c == nil {
		return nil
	}
	return p.emit(*c)
}

// Close ends the stream: the text after the last NUL can still hold the shortstat of the last commit, which is then handed
// over.
func (p *CommitParser) Close() error {
	if p.pending == nil {
		return nil
	}
	rest := string(p.buf)
	p.buf = nil
	if m := finalStat.FindStringSubmatch(strings.TrimSpace(rest)); m != nil {
		// The last line of the stream is a complete shortstat without its newline.
		p.pending.ShortStat = &ShortStat{Files: atoi(m[1]), Insertions: atoi(m[2]), Deletions: atoi(m[3])}
	} else if _, stat := leadingShortStat(rest); stat != nil {
		p.pending.ShortStat = stat
	}
	return p.flush()
}

// leadingShortStat strips the blank lines and the complete shortstat line that precede a record, and returns the rest of the
// text and the shortstat.
func leadingShortStat(s string) (string, *ShortStat) {
	var stat *ShortStat
	for {
		line, after, hasNewline := strings.Cut(s, "\n")
		if !hasNewline {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			s = after
			continue
		}
		if m := shortStatPattern.FindStringSubmatch(trimmed); m != nil && !hashPattern.MatchString(trimmed) {
			stat = &ShortStat{Files: atoi(m[1]), Insertions: atoi(m[2]), Deletions: atoi(m[3])}
			s = after
			continue
		}
		break
	}
	return s, stat
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// parseCommitRecord reads "hash\nname\nemail\nat\nct\nparents\nrefs\nmessage".
func parseCommitRecord(s string) (Commit, bool) {
	fields := strings.SplitN(s, "\n", 8)
	if len(fields) < 7 || !hashPattern.MatchString(fields[0]) {
		return Commit{}, false
	}
	message := ""
	if len(fields) == 8 {
		message = strings.TrimSuffix(fields[7], "\n")
	}
	c := Commit{
		Hash:        fields[0],
		AuthorName:  fields[1],
		AuthorEmail: fields[2],
		AuthorDate:  time.Unix(int64(atoi(fields[3])), 0).UTC(),
		CommitDate:  time.Unix(int64(atoi(fields[4])), 0).UTC(),
		Message:     message,
		CoAuthors:   ParseCoAuthors(message),
	}
	if fields[5] != "" {
		c.Parents = strings.Split(fields[5], " ")
	}
	if fields[6] != "" {
		for _, n := range strings.Split(fields[6], ",") {
			c.RefNames = append(c.RefNames, strings.TrimSpace(n))
		}
	}
	return c, true
}

// ParseCommits parses a whole output of "git log -z --format=CommitFormat". VS Code gives an empty name list as [""] for a
// commit with no decoration; RefNames is empty here.
func ParseCommits(data []byte) []Commit {
	var out []Commit
	p := NewCommitParser(func(c Commit) error { out = append(out, c); return nil })
	_, _ = p.Write(data)
	_ = p.Close()
	return out
}

// StreamCommits reads the output of git log from r and calls fn for each commit; when fn returns an error the reading stops
// and the error is returned.
func StreamCommits(r io.Reader, fn func(Commit) error) error {
	p := NewCommitParser(fn)
	if _, err := io.Copy(p, r); err != nil {
		return err
	}
	return p.Close()
}

// fileLogNumStat matches the --numstat record that follows a commit in "git log --numstat -z": "<added>\t<deleted>\t<path>\0"
// or, for a rename, "<added>\t<deleted>\t\0<old path>\0<new path>\0".
var fileLogNumStat = regexp.MustCompile(`(([0-9a-f]{40}(?:[0-9a-f]{24})?)\n[^\x00]*\x00\n)(?:-|\d+)\t(?:-|\d+)\t(?:\x00[^\x00]*\x00([^\x00]*)|([^\x00]*))\x00`)

// ParseFileLog parses the output of "git log --numstat --shortstat -z --follow -- <file>": the commits, each with the path
// of the file at that commit (FilePath, which differs from the followed path before a rename). A commit without a numstat
// record has no FilePath.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitFileLog).
func ParseFileLog(data []byte) []Commit {
	paths := map[string]string{}
	log := fileLogNumStat.ReplaceAllStringFunc(string(data), func(m string) string {
		sub := fileLogNumStat.FindStringSubmatch(m)
		path := sub[4]
		if sub[3] != "" {
			path = sub[3]
		}
		paths[sub[2]] = path
		return sub[1]
	})
	commits := ParseCommits([]byte(log))
	for i := range commits {
		if p := paths[commits[i].Hash]; p != "" {
			commits[i].FilePath = p
		}
	}
	return commits
}

// ParseCoAuthors reads the "Co-authored-by: Name <email>" trailers of a message, without duplicates by e-mail (a squash merge
// repeats the trailers of what it squashed).
//
// Ported from VS Code: extensions/git/src/git.ts (parseCoAuthors).
func ParseCoAuthors(message string) []CoAuthor {
	var out []CoAuthor
	seen := map[string]bool{}
	for _, m := range coAuthorPattern.FindAllStringSubmatch(message, -1) {
		name, email := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		key := strings.ToLower(email)
		if name != "" && email != "" && !seen[key] {
			seen[key] = true
			out = append(out, CoAuthor{Name: name, Email: email})
		}
	}
	return out
}
