package gitscm

import "testing"

func TestMatchesAnyGlob(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		branch   string
		want     bool
	}{
		{"no pattern", nil, "main", false},
		{"the name itself", []string{"main"}, "main", true},
		{"another name", []string{"main"}, "master", false},
		{"a prefix is not the name", []string{"main"}, "main2", false},
		{"one of several", []string{"develop", "main"}, "main", true},
		{"a star inside a folder", []string{"release/*"}, "release/1.0", true},
		{"a star does not cross a slash", []string{"release/*"}, "release/1.0/hotfix", false},
		{"two stars cross a slash", []string{"release/**"}, "release/1.0/hotfix", true},
		{"two stars between folders match no folder", []string{"a/**/b"}, "a/b", true},
		{"two stars between folders match several", []string{"a/**/b"}, "a/x/y/b", true},
		{"a star alone", []string{"*"}, "main", true},
		{"a star alone does not match a folder", []string{"*"}, "feature/x", false},
		{"a star at the end of a word", []string{"v*"}, "v1.2", true},
		{"a star after a letter may match a dot", []string{"v*"}, "v.1", true},
		{"a star at the start does not match a dot name", []string{"*"}, ".hidden", false},
		{"a question mark", []string{"v?"}, "v1", true},
		{"a question mark is one character", []string{"v?"}, "v12", false},
		{"a question mark is not a slash", []string{"v?x"}, "v/x", false},
		{"a set", []string{"v[0-9]"}, "v7", true},
		{"a set that is not", []string{"v[!0-9]"}, "v7", false},
		{"a set that is not matches the rest", []string{"v[!0-9]"}, "va", true},
		{"alternatives", []string{"{main,develop}"}, "develop", true},
		{"alternatives with a prefix", []string{"rel/{a,b}-*"}, "rel/b-1", true},
		{"alternatives that do not match", []string{"rel/{a,b}-*"}, "rel/c-1", false},
		{"a negation matches everything else", []string{"!develop"}, "main", true},
		{"a negation does not match its name", []string{"!develop"}, "develop", false},
		{"an escaped star", []string{`a\*`}, "a*", true},
		{"an escaped star is not a wildcard", []string{`a\*`}, "ab", false},
		{"dots and plus are plain", []string{"v1.0+x"}, "v1.0+x", true},
		{"dots are not wildcards", []string{"v1.0"}, "v1x0", false},
		{"an unclosed set is plain", []string{"a[b"}, "a[b", true},
		{"an unclosed brace matches nothing", []string{"{a,b"}, "a", false},
		{"non-ASCII names", []string{"fonctionnalité/*"}, "fonctionnalité/é", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesAnyGlob(tc.patterns, tc.branch); got != tc.want {
				t.Errorf("matchesAnyGlob(%q, %q) = %v, want %v", tc.patterns, tc.branch, got, tc.want)
			}
		})
	}
}
