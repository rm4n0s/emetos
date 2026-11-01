package langserver

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// tree makes the files of a table (a name ending in / is a folder) under a new folder and returns it.
func tree(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		if f[len(f)-1] == '/' {
			if err := os.MkdirAll(filepath.Join(root, f), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, root, f, "", 0o644)
	}
	return root
}

func TestResolveRoot(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		folder  string // relative to the tree
		markers []string
		home    string // relative to the tree
		want    string // relative to the tree
	}{
		{"the folder holds a marker", []string{"p/Cargo.toml"}, "p", []string{"Cargo.toml"}, "h", "p"},
		{"a subfolder of a project", []string{"p/Cargo.toml", "p/src/main.rs"}, "p/src", []string{"Cargo.toml"}, "h", "p"},
		{"two levels down", []string{"p/pom.xml", "p/a/b/C.java"}, "p/a/b", []string{"pom.xml", "build.gradle"}, "h", "p"},
		{"the nearest marker wins", []string{"p/Cargo.toml", "p/crates/x/Cargo.toml", "p/crates/x/src/lib.rs"}, "p/crates/x/src", []string{"Cargo.toml"}, "h", "p/crates/x"},
		{"a glob marker", []string{"p/App.sln", "p/src/x.cs"}, "p/src", []string{"*.sln", "*.csproj"}, "h", "p"},
		{"no marker anywhere", []string{"p/src/x.rs"}, "p/src", []string{"Cargo.toml"}, "h", "p/src"},
		{"no markers given", []string{"p/Cargo.toml"}, "p/src", nil, "h", "p/src"},
		{"the top of a repository without a marker stops the climb", []string{"Cargo.toml", "r/.git/HEAD", "r/src/x.rs"}, "r/src", []string{"Cargo.toml"}, "h", "r/src"},
		{"a marker inside the repository is found", []string{"r/.git/HEAD", "r/Cargo.toml", "r/src/x.rs"}, "r/src", []string{"Cargo.toml"}, "h", "r"},
		{"the home folder is not a project", []string{"h/package.json", "h/code/app/x.ts"}, "h/code/app", []string{"package.json"}, "h", "h/code/app"},
		{"a folder above the home folder is not looked at", []string{"package.json", "h/x.ts"}, "h", []string{"package.json"}, "h", "h"},
		{"a marker that is a folder", []string{"p/.vscode/settings.json", "p/x/y.txt"}, "p/x", []string{".vscode"}, "h", "p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tree(t, append(tt.files, "h/")...)
			got := ResolveRoot(filepath.Join(root, tt.folder), tt.markers, filepath.Join(root, tt.home))
			if want := filepath.Join(root, tt.want); got != want {
				rel, _ := filepath.Rel(root, got)
				t.Errorf("ResolveRoot = %s, want %s", rel, tt.want)
			}
		})
	}
	if got := ResolveRoot("", []string{"x"}, "/h"); got != "" {
		t.Errorf("ResolveRoot of no folder = %q", got)
	}
}

func TestResolveRootClimbsAtMostTwelveFolders(t *testing.T) {
	root := tree(t, "Cargo.toml")
	deep := root
	for i := 0; i < 14; i++ {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveRoot(deep, []string{"Cargo.toml"}, "/nonexistent-home"); got != deep {
		t.Errorf("ResolveRoot reached %s from 14 folders down", got)
	}
}

func TestFindProjects(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		markers []string
		opts    ProjectOptions
		want    []string
	}{
		{"the folder itself", []string{"A.sln"}, []string{"*.sln"}, ProjectOptions{}, []string{"."}},
		{"solutions by depth and name", []string{"b/B.sln", "a/A.sln", "a/sub/C.sln", "Root.sln"}, []string{"*.sln"}, ProjectOptions{}, []string{".", "a", "b", "a/sub"}},
		{"dependencies and build output are skipped", []string{"node_modules/x/package.json", "target/debug/Cargo.toml", "app/package.json", "bin/Debug/App.csproj"}, []string{"package.json", "Cargo.toml", "*.csproj"}, ProjectOptions{}, []string{"app"}},
		{"hidden folders are skipped", []string{".cache/a/Cargo.toml", "x/Cargo.toml"}, []string{"Cargo.toml"}, ProjectOptions{}, []string{"x"}},
		{"the depth", []string{"a/b/c/d/Cargo.toml", "a/b/Cargo.toml"}, []string{"Cargo.toml"}, ProjectOptions{}, []string{"a/b"}},
		{"a deeper search", []string{"a/b/c/d/Cargo.toml", "a/b/Cargo.toml"}, []string{"Cargo.toml"}, ProjectOptions{Depth: 4}, []string{"a/b", "a/b/c/d"}},
		{"the folder only", []string{"Cargo.toml", "a/Cargo.toml"}, []string{"Cargo.toml"}, ProjectOptions{Depth: -1}, []string{"."}},
		{"the limit", []string{"a/Cargo.toml", "b/Cargo.toml", "c/Cargo.toml"}, []string{"Cargo.toml"}, ProjectOptions{Limit: 2}, []string{"a", "b"}},
		{"folders the caller skips", []string{"vendor/x/Cargo.toml", "y/Cargo.toml"}, []string{"Cargo.toml"}, ProjectOptions{Skip: []string{"vendor"}}, []string{"y"}},
		{"nothing", []string{"a/readme.md"}, []string{"Cargo.toml"}, ProjectOptions{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := tree(t, tt.files...)
			var got []string
			for _, p := range FindProjects(root, tt.markers, tt.opts) {
				rel, _ := filepath.Rel(root, p)
				got = append(got, rel)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("FindProjects = %v, want %v", got, tt.want)
			}
		})
	}
	if FindProjects("", []string{"x"}, ProjectOptions{}) != nil || FindProjects(t.TempDir(), nil, ProjectOptions{}) != nil {
		t.Error("projects found without a folder or markers")
	}
	if FindProjects(filepath.Join(t.TempDir(), "gone"), []string{"x"}, ProjectOptions{}) != nil {
		t.Error("projects found in a folder that is not there")
	}
}
