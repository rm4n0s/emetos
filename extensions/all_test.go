package extensions

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/testext"
)

// TestEveryFactoryMakesAValidExtension holds the factories of All, and the test extension, to the contract: a valid
// manifest, an id no other has, and a new extension at each call.
func TestEveryFactoryMakesAValidExtension(t *testing.T) {
	factories := append(All(), testext.New)
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
		if f().Manifest().ID == testext.ID {
			t.Errorf("extensions.All lists the test extension")
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
			continue // an extension of the workbench (the TODOs view, the Tasks tab), not a language pack
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
