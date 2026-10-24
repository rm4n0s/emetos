package app

import (
	"os"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The golden of files changed on disk (step 7.4): the tab of a file that was deleted (struck through, in the error color),
// the tab of a file with unsaved changes whose file changed on disk, and the notification that asks what to do.

func TestGoldenExternalChanges(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a := &extApp{goldenSave(t, th.theme)} // main.go has unsaved changes
			putExternal(t, a.files["main.go"], "package main // changed by another program\n", 100)
			if err := os.Remove(a.files["util.go"]); err != nil {
				t.Fatal(err)
			}
			a.deliver(changed(a.files["main.go"]), deleted(a.files["util.go"]))
			if len(a.warnings()) != 1 || !a.item("util.go").Deleted {
				t.Fatalf("notifications %v, tabs %+v", a.notes(), a.tabs())
			}
			a.ts.Pump(400 * time.Millisecond)
			testutil.MatchesGolden(a.ts, "external_changes_"+th.name)
			a.noErrors()
		})
	}
}
