package git

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RefsFormat is the --format of "git for-each-ref" that ParseRefs reads, ten fields separated by NUL (%00): the full name,
// the object, the commit a tag points at, the upstream, the upstream track, the upstream's remote and remote ref, the commit
// date, the date of the commit a tag points at, and "*" for the checked out branch.
const RefsFormat = "%(refname)%00%(objectname)%00%(*objectname)%00%(upstream)%00%(upstream:track)%00%(upstream:remotename)%00%(upstream:remoteref)%00%(committerdate:unix)%00%(*committerdate:unix)%00%(HEAD)"

// RefsDetailsFormat is RefsFormat with four more fields for the details a picker shows: the author's name and the subject of the commit,
// and the same two of the commit an annotated tag points at.
const RefsDetailsFormat = RefsFormat + "%00%(authorname)%00%(*authorname)%00%(subject)%00%(*subject)"

// RefType is the kind of a reference.
type RefType int

// The kinds of reference, as VS Code's RefType.
const (
	// RefHead is a local branch.
	RefHead RefType = iota
	// RefRemoteHead is a remote-tracking branch.
	RefRemoteHead
	// RefTag is a tag.
	RefTag
)

// Upstream is the branch a local branch tracks.
type Upstream struct {
	// Remote is the remote's name, "." for a branch of this repository.
	Remote string
	// Name is the branch at the remote ("main").
	Name string
	// Ref is the full name of the remote-tracking branch ("refs/remotes/origin/main").
	Ref string
}

// Ref is a branch, a remote-tracking branch or a tag.
type Ref struct {
	// Name is the short name: "main", "origin/main", "v1.0".
	Name string
	// FullName is the whole name: "refs/heads/main".
	FullName string
	// Type is the kind.
	Type RefType
	// Commit is the commit the ref names; for an annotated tag, the commit it points at.
	Commit string
	// Object is the object the ref itself names (for an annotated tag, the tag object).
	Object string
	// Remote is the remote of a remote-tracking branch.
	Remote string
	// Upstream is set for a local branch that tracks another.
	Upstream *Upstream
	// Ahead and Behind count the commits of a local branch against its upstream ("[ahead 1, behind 2]").
	Ahead, Behind int
	// Gone says that the upstream no longer exists ("[gone]").
	Gone bool
	// CommitDate is the committer date of the commit.
	CommitDate time.Time
	// Head says that the branch is checked out.
	Head bool
	// AuthorName and Subject are the author and the first line of the message of the commit; set by RefsDetailsFormat only (empty
	// otherwise). For an annotated tag they are those of the commit it points at.
	AuthorName, Subject string
}

var trackPattern = regexp.MustCompile(`\[(?:ahead (\d+))?[,\s]*(?:behind (\d+))?\]`)

// ParseRefs parses the output of "git for-each-ref --format=RefsFormat": one line a ref, the fields separated by NUL. Lines
// that are not branches, remote-tracking branches or tags (refs/stash, refs/notes) and lines that are cut short are skipped.
//
// Ported from VS Code: extensions/git/src/git.ts (parseRefs, getBranch).
func ParseRefs(data []byte) []Ref {
	var refs []Ref
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[:len(lines)-1] { // the text after the last newline is a line that was cut short
		f := strings.Split(line, "\x00")
		if len(f) < 10 {
			continue
		}
		full := f[0]
		r := Ref{FullName: full, Commit: f[1], Object: f[1], Head: f[9] == "*"}
		date := f[7]
		if f[2] != "" { // an annotated tag: the commit is the peeled object
			r.Commit = f[2]
			date = f[8]
		}
		if len(f) >= 14 {
			r.AuthorName, r.Subject = f[10], f[12]
			if f[2] != "" {
				r.AuthorName, r.Subject = f[11], f[13]
			}
		}
		if date != "" {
			if secs, err := strconv.ParseInt(date, 10, 64); err == nil {
				r.CommitDate = time.Unix(secs, 0).UTC()
			}
		}
		switch {
		case strings.HasPrefix(full, "refs/heads/"):
			r.Type = RefHead
			r.Name = strings.TrimPrefix(full, "refs/heads/")
			track := f[4]
			if track == "[gone]" {
				r.Gone = true
			} else if m := trackPattern.FindStringSubmatch(track); m != nil {
				r.Ahead, r.Behind = atoi(m[1]), atoi(m[2])
			}
			if f[3] != "" && !r.Gone {
				u := &Upstream{Remote: f[5], Ref: f[3]}
				if f[6] != "" {
					u.Name = strings.TrimPrefix(f[6], "refs/heads/")
				} else {
					u.Name = strings.TrimPrefix(strings.TrimPrefix(f[3], "refs/remotes/"), u.Remote+"/")
				}
				r.Upstream = u
			}
		case strings.HasPrefix(full, "refs/remotes/"):
			r.Type = RefRemoteHead
			r.Name = strings.TrimPrefix(full, "refs/remotes/")
			if remote, _, ok := strings.Cut(r.Name, "/"); ok {
				r.Remote = remote
			}
		case strings.HasPrefix(full, "refs/tags/"):
			r.Type = RefTag
			r.Name = strings.TrimPrefix(full, "refs/tags/")
		default:
			continue
		}
		refs = append(refs, r)
	}
	return refs
}
