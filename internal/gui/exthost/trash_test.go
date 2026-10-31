package exthost

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestTrashFilesGivesTheScopePathsAndReturnsTheUndo(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	undo, err := st.api.Workspace().TrashFiles(context.Background(), []string{"file:///work/a.txt", "file:///work/dir/b c.txt"})
	if err != nil || undo == nil {
		t.Fatalf("%v, undo %v", err, undo != nil)
	}
	if len(tb.trashed) != 1 || !slices.Equal(tb.trashed[0], []string{"/work/a.txt", "/work/dir/b c.txt"}) {
		t.Errorf("trashed %v", tb.trashed)
	}
	if err := undo(context.Background()); err != nil || tb.undone != 1 {
		t.Errorf("undo: %v, %d", err, tb.undone)
	}
	// no file is no call
	if undo, err := st.api.Workspace().TrashFiles(context.Background(), nil); err != nil || undo != nil || len(tb.trashed) != 1 {
		t.Errorf("no files: %v %v %d", err, undo != nil, len(tb.trashed))
	}
}

func TestTrashFilesRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(st *serverTest, tb *tab)
		uris  []string
		route string
	}{
		{"a URI that is not a file", nil, []string{"emetos-virtual:/x"}, "workspaceAPI.TrashFiles.ExtensionFilePathInvalid"},
		{"a relative path", nil, []string{"file://work/a"}, "workspaceAPI.TrashFiles.ExtensionFilePathInvalid"},
		{"a path that is not clean", nil, []string{"file:///work/../a"}, "workspaceAPI.TrashFiles.ExtensionFilePathInvalid"},
		{"the root", nil, []string{"file:///"}, "workspaceAPI.TrashFiles.ExtensionFilePathInvalid"},
		{"no trash", func(st *serverTest, _ *tab) { st.rig.noTrash["one"] = true }, []string{"file:///work/a"}, "workspaceAPI.TrashFiles.ExtensionTrashUnavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newServerTest(t, simple(), nil)
			if tc.setup != nil {
				tc.setup(st, nil)
			}
			st.startTab("one")
			undo, err := st.api.Workspace().TrashFiles(context.Background(), tc.uris)
			mustRoute(t, err, tc.route)
			if undo != nil {
				t.Error("an undo for a refusal")
			}
		})
	}
	t.Run("the error of the trash is returned as it is", func(t *testing.T) {
		st := newServerTest(t, simple(), nil)
		tb := st.startTab("one")
		tb.trashErr = &errors.Error{Tag: "TrashCrossDevice"}
		_, err := st.api.Workspace().TrashFiles(context.Background(), []string{"file:///work/a"})
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "TrashCrossDevice" {
			t.Errorf("%v", err)
		}
	})
	t.Run("after the activation ended", func(t *testing.T) {
		st := newServerTest(t, simple(), nil)
		st.startTab("one")
		st.rig.host.Shutdown()
		_, err := st.api.Workspace().TrashFiles(context.Background(), []string{"file:///work/a"})
		mustRoute(t, err, "workspaceAPI.TrashFiles.ExtensionDeactivated")
	})
}
