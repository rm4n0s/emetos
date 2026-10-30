package git

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StashFormat is the --format of "git stash list -z" that ParseStashes reads: hash, parents, "stash@{n}", the reflog subject,
// author date, commit date.
const StashFormat = "%H%n%P%n%gd%n%gs%n%at%n%ct"

// Stash is an entry of the stash.
type Stash struct {
	Hash    string
	Parents []string
	// Index is the n of stash@{n}.
	Index int
	// Branch is the branch the stash was made on.
	Branch string
	// Description is the stash's message; "WIP (message)" for a stash made without one.
	Description string
	AuthorDate  time.Time
	CommitDate  time.Time
}

var stashSubject = regexp.MustCompile(`(?i)^(WIP\s)?on\s([^:]+):\s(.*)$`)

// ParseStashes parses the output of "git stash list -z --format=StashFormat". A record that is cut short is dropped.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitStashes).
func ParseStashes(data []byte) []Stash {
	var out []Stash
	for _, rec := range terminated(string(data)) {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\n")
		if len(f) != 6 || !hashPattern.MatchString(f[0]) {
			continue
		}
		ref := strings.TrimSuffix(strings.TrimPrefix(f[2], "stash@{"), "}")
		index, err := strconv.Atoi(ref)
		if err != nil || !strings.HasPrefix(f[2], "stash@{") {
			continue
		}
		s := Stash{Hash: f[0], Index: index, AuthorDate: time.Unix(int64(atoi(f[4])), 0).UTC(), CommitDate: time.Unix(int64(atoi(f[5])), 0).UTC()}
		if f[1] != "" {
			s.Parents = strings.Split(f[1], " ")
		}
		if m := stashSubject.FindStringSubmatch(f[3]); m != nil {
			s.Branch = strings.TrimSpace(m[2])
			s.Description = strings.TrimSpace(m[3])
			if m[1] != "" {
				s.Description = "WIP (" + s.Description + ")"
			}
		} else {
			s.Description = f[3]
		}
		out = append(out, s)
	}
	return out
}
