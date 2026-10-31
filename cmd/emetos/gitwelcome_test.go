package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions"
	"github.com/rm4n0s/emetos/extensions/gitscm"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// gitManifest is the manifest of the Git extension of the compiled-in list.
func gitManifest(t *testing.T) extension.Manifest {
	t.Helper()
	for _, e := range extensions.All() {
		if m := e().Manifest(); m.ID == gitscm.ID {
			return m
		}
	}
	t.Fatal("the Git extension is not in extensions.All()")
	return extension.Manifest{}
}

// TestTheWelcomeContentOfTheSourceControlViewFollowsTheStateOfTheGitExtension checks, with the conditions of the manifest and the
// context keys the extension sets, which welcome content holds in each state (SPEC §10.30 "Welcome content").
func TestTheWelcomeContentOfTheSourceControlViewFollowsTheStateOfTheGitExtension(t *testing.T) {
	m := gitManifest(t)
	base := map[string]any{
		"config.git.enabled": true, "git.missing": false, "git.state": "initialized", "workbenchState": "folder", "scm.providerCount": 0,
		"git.parentRepositoryCount": 0, "git.unsafeRepositoryCount": 0, "git.closedRepositoryCount": 0, "git.untrustedRepositoryCount": 0,
	}
	tests := []struct {
		name string
		with map[string]any
		want []string // the beginnings of the contents that hold, in order
	}{
		{"Git is off", map[string]any{"config.git.enabled": false}, []string{"If you would like to use Git features"}},
		{"git is missing", map[string]any{"git.missing": true}, []string{"Git is not installed, or is older than 2.30"}},
		{"scanning", map[string]any{"git.state": "uninitialized"}, []string{"Scanning folder for Git repositories"}},
		{"a folder without a repository", nil, []string{"The folder currently open doesn't have a Git repository"}},
		{"no folder", map[string]any{"workbenchState": "empty"}, []string{"In order to use Git features, you can open a folder"}},
		{"a repository is open", map[string]any{"scm.providerCount": 1}, nil},
		{"one repository waits for the trust question", map[string]any{"scm.providerCount": 1, "git.untrustedRepositoryCount": 1}, []string{"The Git repository in the folder is not trusted"}},
		{"two repositories wait", map[string]any{"scm.providerCount": 2, "git.untrustedRepositoryCount": 2}, []string{"The Git repositories in the folder are not trusted"}},
		{"an open repository and one that waits", map[string]any{"scm.providerCount": 2, "git.untrustedRepositoryCount": 1}, []string{"The Git repository in the folder is not trusted"}},
		{"one repository in a parent folder", map[string]any{"git.parentRepositoryCount": 1}, []string{"A Git repository was found in the parent folders"}},
		{"two in parent folders", map[string]any{"git.parentRepositoryCount": 2}, []string{"Git repositories were found in the parent folders"}},
		{"an unsafe repository", map[string]any{"git.unsafeRepositoryCount": 1}, []string{"The detected Git repository is potentially unsafe"}},
		{"unsafe repositories", map[string]any{"git.unsafeRepositoryCount": 2}, []string{"The detected Git repositories are potentially unsafe"}},
		{"a closed repository", map[string]any{"git.closedRepositoryCount": 1}, []string{"A Git repository was found that was previously closed"}},
		{"closed repositories", map[string]any{"git.closedRepositoryCount": 2}, []string{"Git repositories were found that were previously closed"}},
		{"the repository is not trusted, so nothing says the folder has none", map[string]any{"git.untrustedRepositoryCount": 1}, []string{"The Git repository in the folder is not trusted"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope := contextkey.NewRoot()
			for k, v := range base {
				scope.Set(k, v)
			}
			for k, v := range tc.with {
				scope.Set(k, v)
			}
			type piece struct {
				group    string
				contents string
			}
			var got []piece
			for _, w := range m.Contributes.ViewWelcome {
				if w.View != "scm" {
					continue
				}
				if w.When != "" {
					expr, err := contextkey.Parse(w.When)
					if err != nil {
						t.Fatalf("the condition %q: %v", w.When, err)
					}
					if expr == nil || !scope.Matches(expr) {
						continue
					}
				}
				got = append(got, piece{w.Group, w.Contents})
			}
			var starts []string
			for _, p := range got {
				starts = append(starts, p.contents)
			}
			if len(starts) != len(tc.want) {
				t.Fatalf("%d pieces hold, want %d:\n%s", len(starts), len(tc.want), strings.Join(starts, "\n--\n"))
			}
			for i, w := range tc.want {
				if !strings.HasPrefix(starts[i], w) {
					t.Errorf("piece %d = %q, want a text that starts with %q", i, starts[i], w)
				}
			}
			if slices.ContainsFunc(tc.want, func(s string) bool { return strings.Contains(s, "not trusted") }) {
				if !strings.Contains(starts[0], "[Trust Repository](command:git.trustRepository)") {
					t.Error("the trust text has no Trust Repository button")
				}
			}
		})
	}
}

// TestTheGitSettingsAreInTheExtensionsGroupOfTheSettingsEditor checks that every setting of the Git extension lands in
// Extensions > Git (no group of the editor claims a "git.*" key), the way VS Code lists them.
func TestTheGitSettingsAreInTheExtensionsGroupOfTheSettingsEditor(t *testing.T) {
	m := gitManifest(t)
	var list []settings.Setting
	for _, s := range m.Contributes.Settings {
		list = append(list, settings.Setting{Key: s.Key, Types: []settings.Type{settings.String}})
	}
	if len(list) < 50 {
		t.Fatalf("%d settings", len(list))
	}
	cats := settings.BuildCategories(settings.DefaultTOC(), list)
	var git *settings.Category
	for _, c := range cats {
		if c.ID == "extensions" {
			for _, ch := range c.Children {
				if ch.Label == "Git" {
					git = ch
				}
			}
		}
	}
	if git == nil {
		t.Fatal("no Extensions > Git group")
	}
	if len(git.Keys) != len(list) {
		t.Errorf("%d of %d settings are in Extensions > Git", len(git.Keys), len(list))
	}
}
