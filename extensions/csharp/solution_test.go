package csharp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The solution or project the server loads, as tables of trees: the files are made under a folder of the test, an empty file each.

// tree makes the files (name to text, "" for an empty file) and the folders under base.
func tree(t *testing.T, base string, files ...string) {
	t.Helper()
	for _, f := range files {
		writeFile(t, base, f, "", 0o644)
	}
}

func TestResolveTarget(t *testing.T) {
	tests := []struct {
		name string
		// files are made under the root of the test; folder is the folder of the tab, relative to it; chosen and the setting are relative to
		// it too.
		files   []string
		folder  string
		chosen  string
		setting string
		// want is the target, with its paths relative to the root of the test.
		wantRoot, wantFile, wantSource string
		wantSeveral                    []string
		wantArgs                       []string
	}{
		{name: "one solution", files: []string{"App.sln", "App/App.csproj"}, wantRoot: ".", wantFile: "App.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "App.sln"}},
		{name: "one slnx", files: []string{"App.slnx"}, wantRoot: ".", wantFile: "App.slnx", wantSource: sourceFolder, wantArgs: []string{"--solution", "App.slnx"}},
		{name: "one project and no solution", files: []string{"App.csproj", "Program.cs"}, wantRoot: ".", wantFile: "App.csproj", wantSource: sourceFolder},
		{name: "several projects and no solution are left to the server", files: []string{"A.csproj", "B.csproj"}, wantRoot: ".", wantSource: sourceFolder},
		{name: "several solutions are left to the server and offered", files: []string{"A.sln", "B.sln", "C.slnx"}, wantRoot: ".", wantSource: sourceNone, wantSeveral: []string{"A.sln", "B.sln", "C.slnx"}},
		{name: "a solution wins over projects of the folder", files: []string{"App.sln", "App.csproj"}, wantRoot: ".", wantFile: "App.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "App.sln"}},
		{name: "the solution above a project folder", files: []string{"App.sln", "src/App/App.csproj"}, folder: "src/App", wantRoot: ".", wantFile: "App.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "App.sln"}},
		{name: "a project nearer than a solution does not hide it", files: []string{"App.sln", "src/App/App.csproj", "src/App/Util/Util.csproj"}, folder: "src/App/Util", wantRoot: ".", wantFile: "App.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "App.sln"}},
		{name: "the project above the folder when there is no solution", files: []string{"src/App/App.csproj", "src/App/Util/a.cs"}, folder: "src/App/Util", wantRoot: "src/App", wantFile: "src/App/App.csproj", wantSource: sourceFolder},
		{name: "a solution below the folder", files: []string{"repo/src/App.sln"}, wantRoot: "repo/src", wantFile: "repo/src/App.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "App.sln"}},
		{name: "a project below the folder", files: []string{"repo/src/App.csproj"}, wantRoot: "repo/src", wantFile: "repo/src/App.csproj", wantSource: sourceFolder},
		{name: "several solutions below the folder", files: []string{"a/A.sln", "b/B.sln"}, wantRoot: ".", wantSource: sourceNone, wantSeveral: []string{"a/A.sln", "b/B.sln"}},
		{name: "a solution below wins over projects below", files: []string{"a/A.sln", "b/B.csproj", "c/C.csproj"}, wantRoot: "a", wantFile: "a/A.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "A.sln"}},
		{name: "folders of dependencies and build output are not looked in", files: []string{"node_modules/x/X.sln", "obj/Y.sln", "bin/Z.csproj", "src/a.cs"}, wantRoot: ".", wantSource: sourceNone},
		{name: "nothing", files: []string{"a.txt"}, wantRoot: ".", wantSource: sourceNone},
		{name: "the choice of the user", files: []string{"A.sln", "B.sln"}, chosen: "B.sln", wantRoot: ".", wantFile: "B.sln", wantSource: sourceChosen, wantArgs: []string{"--solution", "B.sln"}},
		{name: "the choice wins over the setting", files: []string{"A.sln", "B.sln", "C.sln"}, chosen: "B.sln", setting: "C.sln", wantRoot: ".", wantFile: "B.sln", wantSource: sourceChosen, wantArgs: []string{"--solution", "B.sln"}},
		{name: "a chosen project", files: []string{"A.sln", "tools/T.csproj"}, chosen: "tools/T.csproj", wantRoot: "tools", wantFile: "tools/T.csproj", wantSource: sourceChosen},
		{name: "a choice that is gone is forgotten", files: []string{"A.sln"}, chosen: "Gone.sln", wantRoot: ".", wantFile: "A.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "A.sln"}},
		{name: "a choice that is no solution is forgotten", files: []string{"A.sln", "notes.txt"}, chosen: "notes.txt", wantRoot: ".", wantFile: "A.sln", wantSource: sourceFolder, wantArgs: []string{"--solution", "A.sln"}},
		{name: "the setting", files: []string{"A.sln", "B.sln"}, setting: "B.sln", wantRoot: ".", wantFile: "B.sln", wantSource: sourceSetting, wantArgs: []string{"--solution", "B.sln"}},
		{name: "the setting names a project in a folder", files: []string{"src/App/App.csproj", "x.sln"}, setting: "src/App/App.csproj", wantRoot: "src/App", wantFile: "src/App/App.csproj", wantSource: sourceSetting},
		{name: "the setting is relative to the folder of the tab", files: []string{"repo/B.sln", "repo/A.sln"}, folder: "repo", setting: "B.sln", wantRoot: "repo", wantFile: "repo/B.sln", wantSource: sourceSetting, wantArgs: []string{"--solution", "B.sln"}},
		{name: "an upper case extension", files: []string{"App.SLN"}, setting: "App.SLN", wantRoot: ".", wantFile: "App.SLN", wantSource: sourceSetting, wantArgs: []string{"--solution", "App.SLN"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tree(t, root, tt.files...)
			folder := filepath.Join(root, filepath.FromSlash(tt.folder))
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			// a .git folder at the top and a home folder above the root keep the climb inside the test
			if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			set := map[string]any{}
			if tt.setting != "" {
				set[SolutionSetting] = tt.setting
			}
			chosen := ""
			if tt.chosen != "" {
				chosen = filepath.Join(root, filepath.FromSlash(tt.chosen))
			}
			e := env(folder, filepath.Join(root, "..", "home"), "", set, nil)
			got, err := resolveTarget(e, chosen)
			if err != nil {
				t.Fatal(err)
			}
			abs := func(rel string) string {
				if rel == "" {
					return ""
				}
				return filepath.Join(root, filepath.FromSlash(rel))
			}
			if got.Root != abs(tt.wantRoot) || got.File != abs(tt.wantFile) || got.Source != tt.wantSource {
				t.Errorf("target = {root %s, file %s, source %q}, want {root %s, file %s, source %q}", got.Root, got.File, got.Source, abs(tt.wantRoot), abs(tt.wantFile), tt.wantSource)
			}
			var several []string
			for _, f := range tt.wantSeveral {
				several = append(several, abs(f))
			}
			if !slices.Equal(got.Several, several) {
				t.Errorf("several = %v, want %v", got.Several, several)
			}
			if args := got.args(); !slices.Equal(args, tt.wantArgs) {
				t.Errorf("args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestATabWithoutAFolderHasNoTarget(t *testing.T) {
	got, err := resolveTarget(env("", "", "", nil, nil), "")
	if err != nil || got.Root != "" || got.File != "" || got.Source != sourceNone {
		t.Errorf("target = %+v, %v", got, err)
	}
	if got.describe("") != "No solution or project was found" {
		t.Errorf("describe = %q", got.describe(""))
	}
}

func TestTheClimbDoesNotLeaveTheRepository(t *testing.T) {
	root := t.TempDir()
	tree(t, root, "Outer.sln", "repo/.git/HEAD", "repo/src/a.cs")
	folder := filepath.Join(root, "repo", "src")
	got, err := resolveTarget(env(folder, filepath.Join(root, "home"), "", nil, nil), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.File != "" || got.Root != folder {
		t.Errorf("target = %+v: the solution above the repository is not this folder's", got)
	}
}

func TestDescribeSaysWhatIsLoadedAndWhy(t *testing.T) {
	folder := "/w/repo"
	tests := []struct {
		t    target
		want string
	}{
		{target{Root: "/w/repo", File: "/w/repo/App.sln", Source: sourceFolder}, "Loads App.sln (the only one in the folder)"},
		{target{Root: "/w/repo/src", File: "/w/repo/src/App.sln", Source: sourceChosen}, "Loads src/App.sln (chosen with Select Solution)"},
		{target{Root: "/w/repo", File: "/w/other/App.sln", Source: sourceSetting}, "Loads /w/other/App.sln (csharp.solution)"},
		{target{Root: "/w/repo", Several: []string{"/w/repo/A.sln", "/w/repo/B.sln"}}, "The folder has several solutions; csharp-ls picks one of them. Select Solution chooses."},
		{target{Root: "/w/repo/src"}, "Loads the projects it finds in src"},
		{target{}, "No solution or project was found"},
	}
	for _, tt := range tests {
		if got := tt.t.describe(folder); got != tt.want {
			t.Errorf("describe(%+v) = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestIsLoadable(t *testing.T) {
	for file, want := range map[string]bool{"a.sln": true, "a.slnx": true, "a.csproj": true, "A.CSPROJ": true, "a.fsproj": false, "a.cs": false, "sln": false, "": false} {
		if got := isLoadable(file); got != want {
			t.Errorf("isLoadable(%q) = %v, want %v", file, got, want)
		}
	}
}
