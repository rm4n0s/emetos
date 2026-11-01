package app

import (
	"encoding/json"
	"testing"

	"github.com/rm4n0s/emetos/extensions/references"
)

// The values extensions keep (extension.State) are kept in the state of the profile, by extension, folder and key (step 24b.4).

func TestTheValuesAnExtensionKeepsBelongToItsFolder(t *testing.T) {
	p := extensionProcess(t, references.New)
	one := newExtensionWorkspace(t, p, "", "go.mod")
	two := newExtensionWorkspace(t, p, "", "go.mod")
	if err := one.extensionStateSet("emetos.keeper", one.Folder, "k", json.RawMessage(`"one"`)); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		w      *Workspace
		ext    string
		key    string
		want   string
		wantOK bool
	}{
		{"the folder that kept it", one, "emetos.keeper", "k", `"one"`, true},
		{"another folder", two, "emetos.keeper", "k", "", false},
		{"another extension", one, "emetos.other", "k", "", false},
		{"another key", one, "emetos.keeper", "j", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.w.extensionStateGet(tc.ext, tc.w.Folder, tc.key)
			if ok != tc.wantOK || string(got) != tc.want {
				t.Errorf("got %s, %v; want %s, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
	// taking it away
	if err := one.extensionStateSet("emetos.keeper", one.Folder, "k", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := one.extensionStateGet("emetos.keeper", one.Folder, "k"); ok {
		t.Error("the value is still there")
	}
}
