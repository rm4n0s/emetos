package app

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// TestWithExplorerArgs holds what the context menu of the Explorer hands to the commands of extensions (extension.ExplorerArgs): the
// pressed row and the selection are added to the arguments of an item of an extension, also in a submenu; the editor's own commands,
// separators and items with arguments that are not an object are left as they are.
func TestWithExplorerArgs(t *testing.T) {
	args := map[string]any{"uri": "file:///w/Dockerfile", "uris": []any{"file:///w/Dockerfile", "file:///w/a"}}
	isExt := func(id string) bool {
		return id == "containers.images.build" || id == "containers.keeps" || id == "ext.list"
	}
	items := []kit.MenuItem{
		{ID: "explorer.newFile", Label: "New File..."},
		{ID: "containers.images.build", Label: "Build Image..."},
		{ID: "containers.keeps", Label: "Keeps", Data: map[string]any{"mode": "fast", "uri": "own"}},
		{ID: "ext.list", Label: "List", Data: []any{1}},
		kit.SeparatorItem(),
		{ID: "sub", Label: "Sub", Submenu: true, Items: []kit.MenuItem{{ID: "containers.images.build", Label: "Inner"}}},
	}
	got := withExplorerArgs(items, isExt, args)
	tests := []struct {
		name string
		at   int
		want any
	}{
		{"the editor's command", 0, nil},
		{"a command of an extension with no arguments", 1, args},
		{"the arguments of the item are kept and the explorer's win", 2, map[string]any{"mode": "fast", "uri": "file:///w/Dockerfile", "uris": args["uris"]}},
		{"arguments that are not an object", 3, []any{1}},
	}
	for _, tc := range tests {
		if !reflect.DeepEqual(got[tc.at].Data, tc.want) {
			t.Errorf("%s: %#v, want %#v", tc.name, got[tc.at].Data, tc.want)
		}
	}
	if !got[4].Separator || got[4].Data != nil {
		t.Errorf("the separator changed: %+v", got[4])
	}
	if inner := got[5].Items[0].Data; !reflect.DeepEqual(inner, args) {
		t.Errorf("the item of the submenu: %#v", inner)
	}
	if items[1].Data != nil || items[2].Data.(map[string]any)["uri"] != "own" {
		t.Error("the items that were passed in were changed")
	}
}

// TestExplorerURI is the form of the URIs the context menu hands over.
func TestExplorerURI(t *testing.T) {
	if got := explorerURI("/home/u/my project/Dockerfile"); got != "file:///home/u/my project/Dockerfile" {
		t.Errorf("explorerURI = %q", got)
	}
}
