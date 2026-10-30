package git

import (
	"regexp"
	"strconv"

	"github.com/rm4n0s/errors"
)

// Version is the version of a git program: the numbers of "git version 2.43.0.windows.1" (2.43.0) or of "git version
// 2.56.0.rc1" (2.56.0). A pre-release compares like its final version; a missing patch number is 0.
type Version struct {
	Major, Minor, Patch int
}

// MinVersion is the oldest git that Emetos runs (SPEC §10.30, D35): 2.30.
var MinVersion = Version{Major: 2, Minor: 30}

// Compare returns -1, 0 or 1 when v is older than, equal to or newer than other.
func (v Version) Compare(other Version) int {
	for _, p := range [][2]int{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}} {
		switch {
		case p[0] < p[1]:
			return -1
		case p[0] > p[1]:
			return 1
		}
	}
	return 0
}

// AtLeast says whether v is at least major.minor.
func (v Version) AtLeast(major, minor int) bool {
	return v.Compare(Version{Major: major, Minor: minor}) >= 0
}

// String formats the version as "2.43.0".
func (v Version) String() string {
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}

var versionPattern = regexp.MustCompile(`^(?:git version )?(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion reads the output of "git --version" ("git version 2.30.1 (Apple Git-130)") or a bare version ("2.30").
// The error has the tag GitVersionUnreadable.
func ParseVersion(raw string) (Version, error) {
	m := versionPattern.FindStringSubmatch(trimSpace(raw))
	if m == nil {
		return Version{}, errors.New("GitVersionUnreadable", "cannot read the version of git", "output", raw)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		v.Patch, _ = strconv.Atoi(m[3])
	}
	return v, nil
}

// trimSpace trims the ASCII white space around s.
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
