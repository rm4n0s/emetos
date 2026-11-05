package terminal

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// The entries that open a terminal in the editor area (step 29.1, SPEC §10.25 "Opening a terminal in the editor area").

func TestTheManifestOffersTheTerminalInTheEditorArea(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		menu  string
		cmd   string
		title string
		group string
		order float64
	}{
		{"the + menu of a tab strip", extension.MenuEditorTabsNew, CmdCreateEditor, "New Terminal", "2_terminal", 0},
		{"New Terminal", extension.MenuTerminal, CmdNew, "New Terminal", "1_create", 1},
		{"New Terminal in Editor Area", extension.MenuTerminal, CmdCreateEditor, "New Terminal in Editor Area", "1_create", 2},
		{"Split Terminal", extension.MenuTerminal, CmdSplit, "Split Terminal", "1_create", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, item := range m.Contributes.Menus {
				if item.Menu == tt.menu && item.Command == tt.cmd {
					if item.Title != tt.title || item.Group != tt.group || item.Order != tt.order {
						t.Errorf("the item is %+v", item)
					}
					return
				}
			}
			t.Errorf("no item of %s in %s", tt.cmd, tt.menu)
		})
	}
	for _, item := range m.Contributes.Menus {
		if item.Menu == extension.MenuEditorTabsNew && item.Command != CmdCreateEditor {
			t.Errorf("the + menu has %s", item.Command)
		}
	}
}

func TestCreateEditorOpensInTheGroupOfItsArgument(t *testing.T) {
	tests := []struct {
		name      string
		args      any
		wantGroup int
	}{
		{"no argument is the active group", nil, 0},
		{"a group", map[string]any{"groupId": 2.0}, 2},
		{"a group and a profile", map[string]any{"groupId": 2.0, "profileName": "test"}, 2},
		{"a group, a folder and a name", map[string]any{"groupId": 3.0, "cwd": "", "name": "builder"}, 3},
		{"a profile name as a string", "test", 0},
		{"keys without a group", map[string]any{"name": "tabbed"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			r.run(CmdCreateEditor, tt.args)
			tabs := openTabs(r)
			if len(tabs) != 1 {
				t.Fatalf("%d editor tabs", len(tabs))
			}
			if tabs[0].Group() != tt.wantGroup {
				t.Errorf("the tab is in group %d, want %d", tabs[0].Group(), tt.wantGroup)
			}
			if m, ok := tt.args.(map[string]any); ok {
				if name, _ := m["name"].(string); name != "" && r.ext.all()[0].term.Name() != name {
					t.Errorf("the name is %q", r.ext.all()[0].term.Name())
				}
			}
		})
	}
}

func TestTheProfileListEndsWithTheEditorAreaRow(t *testing.T) {
	r := newExtRig(t, "")
	r.api.SetSetting(settingDefaultLocation, "view")
	r.run(CmdNewWithProfile, nil)
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no quick pick of profiles")
	}
	labels := pick.Labels()
	if len(labels) != 3 || labels[1] != " " || labels[2] != "$(terminal) New Terminal in Editor Area" {
		t.Fatalf("the rows are %q", labels)
	}
	pick.Choose(2)
	if r.ext.count() != 1 || len(openTabs(r)) != 1 || !r.ext.all()[0].inEditor() {
		t.Errorf("%d terminals, %d editor tabs after choosing the row", r.ext.count(), len(openTabs(r)))
	}
}
