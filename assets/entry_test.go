package assets_test

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/assets"
)

func TestDesktopEntry(t *testing.T) {
	got := string(assets.DesktopEntry())
	if !strings.HasPrefix(got, "[Desktop Entry]\n") {
		t.Fatalf("DesktopEntry does not start with the group header: %q", got)
	}
	for _, want := range []string{"Exec=emetos %F\n", "Exec=emetos -n\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("DesktopEntry = %q, want it to contain %q", got, want)
		}
	}
}

func TestDesktopEntryReturnsACopy(t *testing.T) {
	a := assets.DesktopEntry()
	a[0] = '!'
	b := assets.DesktopEntry()
	if b[0] == '!' {
		t.Error("DesktopEntry shares its backing array between calls")
	}
}
