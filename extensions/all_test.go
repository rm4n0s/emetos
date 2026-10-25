package extensions

import (
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
