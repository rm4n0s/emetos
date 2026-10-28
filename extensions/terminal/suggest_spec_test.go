package terminal

import (
	"io/fs"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

func itemLabels(items []rawItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.label)
	}
	sort.Strings(out)
	return out
}

func TestEveryBundledSpecAndBuiltinListLoads(t *testing.T) {
	data := bundledData()
	entries, err := fs.ReadDir(data, "specs")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 40 {
		t.Fatalf("only %d specs are bundled", len(entries))
	}
	store := newSpecStore(data)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		spec, err := store.get(name)
		if err != nil || spec == nil {
			t.Errorf("spec %s: %v, %v", name, spec, err)
			continue
		}
		if len(spec.Name) == 0 {
			t.Errorf("spec %s has no name", name)
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		list, err := builtinsOf(data, shell)
		if err != nil || len(list) < 20 {
			t.Errorf("builtins of %s: %d, %v", shell, len(list), err)
		}
		if !slices.ContainsFunc(list, func(b builtinCmd) bool { return b.Name == "cd" }) {
			t.Errorf("the builtins of %s have no cd", shell)
		}
	}
	if list, err := builtinsOf(data, "pwsh"); err != nil || list != nil {
		t.Errorf("builtins of pwsh = %v, %v; want none", list, err)
	}
}

func TestWalkSpecOfGit(t *testing.T) {
	store := newSpecStore(bundledData())
	git, err := store.get("git")
	if err != nil || git == nil {
		t.Fatalf("git: %v, %v", git, err)
	}
	tests := []struct {
		name     string
		words    []string
		cur      string
		want     []string // labels that must be offered
		wantNot  []string // labels that must not be
		wantFile bool
	}{
		{"the subcommands", nil, "", []string{"checkout", "commit", "push", "status"}, []string{"--version"}, false},
		{"an option", nil, "--ver", []string{"--version"}, []string{"checkout"}, false},
		{"after a subcommand its options", []string{"commit"}, "-", []string{"--amend", "-m"}, []string{"checkout"}, false},
		{"the value of an option with a suggestion", []string{"archive", "--format"}, "", []string{"tar", "zip"}, []string{"--format"}, false},
		{"an argument with only a generator offers nothing of its own", []string{"add"}, "", nil, []string{"checkout"}, false},
		{"an option that was given is not offered again", []string{"commit", "--amend"}, "--", nil, []string{"--amend"}, false},
		{"several short options together", []string{"commit", "-am"}, "", nil, nil, false},
		{"after two dashes only arguments", []string{"add", "--"}, "", nil, []string{"checkout", "--force"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := walkSpec(git, tt.words, tt.cur)
			got := itemLabels(res.items)
			for _, w := range tt.want {
				if !slices.Contains(got, w) {
					t.Errorf("%q is not offered; got %d items", w, len(got))
				}
			}
			for _, w := range tt.wantNot {
				if slices.Contains(got, w) {
					t.Errorf("%q is offered", w)
				}
			}
			if res.showFiles != tt.wantFile {
				t.Errorf("showFiles = %v, want %v", res.showFiles, tt.wantFile)
			}
		})
	}
}

func TestWalkSpecOfSmallCommands(t *testing.T) {
	store := newSpecStore(bundledData())
	for _, tt := range []struct {
		command     string
		words       []string
		cur         string
		wantFolders bool
		wantFiles   bool
		wantItem    string
	}{
		{"cd", nil, "", true, false, ""},
		{"cat", nil, "", true, true, ""},
		{"ls", nil, "", true, true, ""},
		{"ls", nil, "-", false, false, "-a"},
		{"mkdir", nil, "-", false, false, "-p"},
		{"grep", nil, "-", false, false, "-i"},
	} {
		t.Run(tt.command+" "+strings.Join(tt.words, " ")+tt.cur, func(t *testing.T) {
			spec, err := store.get(tt.command)
			if err != nil || spec == nil {
				t.Fatalf("%s: %v", tt.command, err)
			}
			res := walkSpec(spec, tt.words, tt.cur)
			if res.showFolders != tt.wantFolders || res.showFiles != tt.wantFiles {
				t.Errorf("showFolders, showFiles = %v, %v; want %v, %v", res.showFolders, res.showFiles, tt.wantFolders, tt.wantFiles)
			}
			if tt.wantItem != "" && !slices.Contains(itemLabels(res.items), tt.wantItem) {
				t.Errorf("%s is not offered", tt.wantItem)
			}
		})
	}
}

func TestAnOptionWithRequiredArgumentsTakesTheNextWord(t *testing.T) {
	spec := &specNode{
		Name: specNames{"tool"},
		Options: []*specOpt{
			{Name: specNames{"-o", "--output"}, Args: []*specArg{{Name: "file", Templates: []string{"filepaths"}}}},
			{Name: specNames{"-v"}, Repeatable: true},
		},
		Subcommands: []*specNode{{Name: specNames{"run", "r"}, Args: []*specArg{{Name: "target", Suggestions: []specSuggested{{Name: specNames{"all"}}}}}}},
	}
	tests := []struct {
		name      string
		words     []string
		cur       string
		wantItems []string
		wantFiles bool
	}{
		{"the value of the option", []string{"-o"}, "", nil, true},
		{"the long name", []string{"--output"}, "x", nil, true},
		{"with an equals sign the value is in the word", []string{"--output=a.txt"}, "", []string{"r", "run"}, false},
		{"after the value", []string{"-o", "a.txt"}, "", []string{"r", "run"}, false},
		{"an alias of a subcommand", []string{"r"}, "", []string{"all"}, false},
		{"a repeatable option stays offered", []string{"-v"}, "-", []string{"--output", "-o", "-v"}, false},
		{"an option that is not repeatable goes", []string{"-o", "x"}, "-", []string{"-v"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := walkSpec(spec, tt.words, tt.cur)
			if got := itemLabels(res.items); !slices.Equal(got, tt.wantItems) && (len(got) != 0 || len(tt.wantItems) != 0) {
				t.Errorf("items = %v, want %v", got, tt.wantItems)
			}
			if res.showFiles != tt.wantFiles {
				t.Errorf("showFiles = %v, want %v", res.showFiles, tt.wantFiles)
			}
		})
	}
}

func TestTheSpecStoreErrorsHaveRoutes(t *testing.T) {
	data := fstest.MapFS{
		"specs/broken.json":    {Data: []byte("{not json")},
		"builtins/bash.json":   {Data: []byte("[bad")},
		"specs/fine.json":      {Data: []byte(`{"name":["fine","f"],"description":"d"}`)},
		"builtins/fish.json":   {Data: []byte(`[{"name":"echo"}]`)},
		"builtins/unused.json": {Data: []byte(`[]`)},
	}
	store := newSpecStore(data)
	_, err := store.get("broken")
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("specStore.get.TerminalSpecInvalid") {
		t.Errorf("a broken spec: %v", err)
	}
	if spec, err := store.get("fine"); err != nil || spec == nil || len(spec.Name) != 2 {
		t.Errorf("a good spec: %v, %v", spec, err)
	}
	if spec, err := store.get("/usr/bin/fine"); err != nil || spec == nil {
		t.Errorf("a command given with its path has the spec of its name: %v, %v", spec, err)
	}
	if spec, err := store.get("..\\fine"); err != nil || spec != nil {
		t.Errorf("a name with a backslash is not a spec: %v, %v", spec, err)
	}
	if spec, err := store.get("missing"); err != nil || spec != nil {
		t.Errorf("a missing spec: %v, %v", spec, err)
	}
	if _, err := builtinsOf(data, "bash"); err == nil {
		t.Errorf("broken builtins gave no error")
	} else if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("builtinsOf.TerminalBuiltinsInvalid") {
		t.Errorf("broken builtins: %v", err)
	}
	if _, err := builtinsOf(data, "zsh"); err == nil {
		t.Errorf("missing builtins gave no error")
	} else if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("builtinsOf.TerminalBuiltinsMissing") {
		t.Errorf("missing builtins: %v", err)
	}
	if list, err := builtinsOf(data, "fish"); err != nil || len(list) != 1 {
		t.Errorf("fish builtins: %v, %v", list, err)
	}
}
