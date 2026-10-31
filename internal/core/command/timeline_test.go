package command

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

func TestTheTimelineViewIsFoldedAndBelowTheOutline(t *testing.T) {
	r := DefaultViews()
	v, ok := r.View(ViewTimeline)
	if !ok || v.Name != "Timeline" || v.Container != ViewContainerExplorer || !v.Collapsed || v.HideByDefault {
		t.Fatalf("view = %+v, %v", v, ok)
	}
	got := viewIDs(r.Views(ViewContainerExplorer, contextkey.MapContext{"workbenchState": "folder"}))
	if n := len(got); n < 2 || got[n-2] != ViewOutline || got[n-1] != ViewTimeline {
		t.Errorf("views = %v", got)
	}
}

func TestTheTimelineCommandsAndTheirTitles(t *testing.T) {
	reg := NewRegistry()
	registerTimelineCommands(reg)
	tests := []struct {
		id, title, category, icon string
	}{
		{"timeline.focus", "Focus on Timeline View", "View", ""},
		{"timeline.refresh", "Refresh", "Timeline", "refresh"},
		// VS Code lists neither of these in the command palette
		{"timeline.toggleFollowActiveEditor", "", "", ""},
		{"files.openTimeline", "", "", ""},
	}
	for _, tt := range tests {
		m, ok := reg.Lookup(tt.id)
		if !ok || m.Title != tt.title || m.Category != tt.category || m.Icon != tt.icon {
			t.Errorf("%s = %+v, %v", tt.id, m, ok)
		}
	}
}

// TestOpenTimelineIsInTheMenusOfAFileWhileAProviderIsRegistered checks the conditions of the two rows.
func TestOpenTimelineIsInTheMenusOfAFileWhileAProviderIsRegistered(t *testing.T) {
	cmds := NewRegistry()
	registerTimelineCommands(cmds)
	menus := NewMenuRegistry(cmds)
	registerTimelineMenus(menus)
	tests := []struct {
		name string
		menu string
		ctx  contextkey.MapContext
		want bool
	}{
		{"a file in the Explorer", ExplorerContext, contextkey.MapContext{"resourceScheme": "file", "timelineHasProvider": true}, true},
		{"a folder in the Explorer", ExplorerContext, contextkey.MapContext{"resourceScheme": "file", "explorerResourceIsFolder": true, "timelineHasProvider": true}, false},
		{"no provider", ExplorerContext, contextkey.MapContext{"resourceScheme": "file"}, false},
		{"a file tab", EditorTitleContext, contextkey.MapContext{"isFileSystemResource": true, "timelineHasProvider": true}, true},
		{"a tab that is not a file", EditorTitleContext, contextkey.MapContext{"timelineHasProvider": true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for _, e := range menus.Items(tt.menu, tt.ctx) {
				found = found || e.Command == "files.openTimeline" && e.Title == "Open Timeline"
			}
			if found != tt.want {
				t.Errorf("Open Timeline in the menu: %v, want %v", found, tt.want)
			}
		})
	}
}
