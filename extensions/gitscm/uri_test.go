package gitscm

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// The URIs of the versions of files. VS Code's git.test.ts has no case of its URIs (toGitUri and fromGitUri are tested nowhere in
// extensions/git); the table is written for the form of the SPEC.
func TestAVersionOfAFileHasAURIThatReadsBackToTheSame(t *testing.T) {
	const id = "0123456789ab"
	tests := []struct {
		name, ref, path string
		uri             string
	}{
		{"the commit HEAD", "HEAD", "src/a.go", "emetos-virtual:/git/0123456789ab/HEAD/src/a.go"},
		{"the index", "", "src/a.go", "emetos-virtual:/git/0123456789ab//src/a.go"},
		{"the version a changed file is compared with", "~", "a.go", "emetos-virtual:/git/0123456789ab/~/a.go"},
		{"a stage", "~2", "a.go", "emetos-virtual:/git/0123456789ab/~2/a.go"},
		{"a commit", "9a3f1c2d4e5f60718293a4b5c6d7e8f901234567", "a.go", "emetos-virtual:/git/0123456789ab/9a3f1c2d4e5f60718293a4b5c6d7e8f901234567/a.go"},
		{"a path with spaces", "HEAD", "my docs/read me.md", "emetos-virtual:/git/0123456789ab/HEAD/my docs/read me.md"},
		{"a path with a percent sign and a hash", "HEAD", "50%/#1 ?.txt", "emetos-virtual:/git/0123456789ab/HEAD/50%/#1 ?.txt"},
		{"a path that is not ASCII", "HEAD", "données/日本語.go", "emetos-virtual:/git/0123456789ab/HEAD/données/日本語.go"},
		{"a path that holds the name of a ref", "HEAD", "HEAD/HEAD", "emetos-virtual:/git/0123456789ab/HEAD/HEAD/HEAD"},
		{"a submodule", "index", "libs/sub.diff", "emetos-virtual:/git/0123456789ab/index/libs/sub.diff"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := gitURI{RepoID: id, Ref: tc.ref, Path: tc.path}
			if got := g.String(); got != tc.uri {
				t.Fatalf("URI %q, want %q", got, tc.uri)
			}
			back, ok := parseGitURI(tc.uri)
			if !ok || back != g {
				t.Errorf("read back %+v %v, want %+v", back, ok, g)
			}
		})
	}
}

func TestAURIThatIsNotAVersionOfAFileDoesNotParse(t *testing.T) {
	for _, uri := range []string{
		"", "file:///a.go", "emetos-virtual:/saved/a.go", "emetos-virtual:/git/", "emetos-virtual:/git/id", "emetos-virtual:/git/id/HEAD",
		"emetos-virtual:/git/id/HEAD/", "emetos-virtual:/git//HEAD/a.go", "emetos-virtual:/gitx/id/HEAD/a.go", "git:/id/HEAD/a.go",
	} {
		if g, ok := parseGitURI(uri); ok {
			t.Errorf("%q parsed as %+v", uri, g)
		}
	}
}

func TestTheFileOfAVersionAndTheSubmoduleForm(t *testing.T) {
	g := gitURI{RepoID: "i", Ref: "HEAD", Path: "src/a b.go"}
	if got := g.filePath("/work/repo"); got != "/work/repo/src/a b.go" {
		t.Errorf("file %q", got)
	}
	sub := gitURI{RepoID: "i", Ref: refSubIndex, Path: "libs/sub.diff"}
	if !sub.isSubmodule() || sub.filePath("/work/repo") != "/work/repo/libs/sub" {
		t.Errorf("submodule %v %q", sub.isSubmodule(), sub.filePath("/work/repo"))
	}
	// the ref decides: a file called x.diff in a commit is a file
	if (gitURI{Ref: "HEAD", Path: "x.diff"}).isSubmodule() || (gitURI{Ref: refSubIndex, Path: "x"}).isSubmodule() {
		t.Error("a file is taken for a submodule")
	}
}

func TestTheRepositoryIdIsStableAndTellsRepositoriesApart(t *testing.T) {
	a, b := repoID("/work/a"), repoID("/work/b")
	if a == b || a != repoID("/work/a") || len(a) != 12 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("ids %q %q", a, b)
	}
	if got := toGitURI(a, "/work/a", "/work/a/src/x.go", "HEAD"); got != extension.ContentURI("git", a+"/HEAD/src/x.go") {
		t.Errorf("URI %q", got)
	}
}
