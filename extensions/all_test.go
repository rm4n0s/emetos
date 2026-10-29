package extensions

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/containers"
	"github.com/rm4n0s/emetos/extensions/internal/testext"
	"github.com/rm4n0s/emetos/extensions/podman"
)

// TestEveryFactoryMakesAValidExtension holds the factories of All, and the test extension, to the contract: a valid
// manifest, an id no other has, and a new extension at each call.
func TestEveryFactoryMakesAValidExtension(t *testing.T) {
	factories := append(All(), testext.New, testext.NewTabs)
	seen := map[string]bool{}
	for i, f := range factories {
		a, b := f(), f()
		if a == nil || b == nil {
			t.Fatalf("factory %d made no extension", i)
		}
		if a == b {
			t.Errorf("factory %d returned the same extension twice: two scopes would share it", i)
		}
		m := a.Manifest()
		if err := extension.CheckManifest(m); err != nil {
			t.Errorf("factory %d (%s): %v", i, m.ID, err)
		}
		if seen[m.ID] {
			t.Errorf("the id %s is used twice", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestAllMakesANewSliceEachTime(t *testing.T) {
	first := All()
	first = append(first, testext.New)
	if len(All()) != len(first)-1 {
		t.Errorf("a change of the slice All returned changed the list")
	}
}

func TestTheTestExtensionIsNotCompiledIn(t *testing.T) {
	for _, f := range All() {
		if id := f().Manifest().ID; id == testext.ID || id == testext.TabsID {
			t.Errorf("extensions.All lists the test extension %s", id)
		}
	}
}

// TestEveryPackHasItsFiles holds the language packs of All to their files: the package.json is JSON, and the language
// configurations and the grammars it names are in the pack (the registry reads them the first time a language is used,
// so a missing file would show only then).
func TestEveryPackHasItsFiles(t *testing.T) {
	packs := 0
	for _, f := range All() {
		m := f().Manifest()
		fsys := m.Contributes.Languages
		if fsys == nil {
			continue // an extension of the workbench (the Tasks tab), not a language pack
		}
		packs++
		data, err := fs.ReadFile(fsys, "package.json")
		if err != nil {
			t.Errorf("%s: %v", m.ID, err)
			continue
		}
		var pkg struct {
			Contributes struct {
				Languages []struct {
					ID            string `json:"id"`
					Configuration string `json:"configuration"`
				} `json:"languages"`
				Grammars []struct {
					ScopeName string `json:"scopeName"`
					Path      string `json:"path"`
				} `json:"grammars"`
			} `json:"contributes"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			t.Errorf("%s: package.json: %v", m.ID, err)
			continue
		}
		if !strings.HasPrefix(m.ID, "emetos.") || len(pkg.Contributes.Languages) == 0 {
			t.Errorf("%s: id %q or no languages", m.ID, m.ID)
		}
		var files []string
		for _, l := range pkg.Contributes.Languages {
			if l.Configuration != "" {
				files = append(files, l.Configuration)
			}
		}
		for _, g := range pkg.Contributes.Grammars {
			files = append(files, g.Path)
		}
		for _, file := range files {
			if _, err := fs.Stat(fsys, path.Clean(strings.TrimPrefix(file, "./"))); err != nil {
				t.Errorf("%s: %s: %v", m.ID, file, err)
			}
		}
	}
	if packs < 20 {
		t.Errorf("only %d language packs", packs)
	}
}

// TestPodmanAndDockerShareNothingTheyMustNotShare holds that the two extensions can be on at once: none of their ids (commands, views,
// view container, editor kinds, panel views, settings) is the same.
func TestPodmanAndDockerShareNothingTheyMustNotShare(t *testing.T) {
	ids := func(m extension.Manifest) map[string]bool {
		out := map[string]bool{}
		for _, c := range m.Contributes.Commands {
			out[c.ID] = true
		}
		for _, v := range m.Contributes.Views {
			out[v.ID] = true
		}
		for _, v := range m.Contributes.ViewContainers {
			out[v.ID] = true
		}
		for _, v := range m.Contributes.PanelViews {
			out[v.ID] = true
		}
		for _, v := range m.Contributes.EditorKinds {
			out[v.ID] = true
		}
		for _, v := range m.Contributes.Settings {
			out[v.Key] = true
		}
		return out
	}
	docker, pod := ids(containers.New().Manifest()), ids(podman.New().Manifest())
	for id := range pod {
		if docker[id] {
			t.Errorf("%s is in both", id)
		}
	}
	// the two add different icons to the activity bar, and their menu items on a Dockerfile are told apart by the title
	var dockerIcon, podmanIcon string
	for _, v := range containers.New().Manifest().Contributes.ViewContainers {
		dockerIcon = v.Icon
	}
	for _, v := range podman.New().Manifest().Contributes.ViewContainers {
		podmanIcon = v.Icon
	}
	if dockerIcon == "" || podmanIcon == "" || dockerIcon == podmanIcon {
		t.Errorf("icons %q and %q", dockerIcon, podmanIcon)
	}
	for _, it := range podman.New().Manifest().Contributes.Menus {
		if (it.Menu == extension.MenuExplorerContext || it.Menu == extension.MenuEditorContext) && !strings.HasPrefix(it.Title, "Podman: ") {
			t.Errorf("the item %s of the menu %s is titled %q", it.Command, it.Menu, it.Title)
		}
	}
}
