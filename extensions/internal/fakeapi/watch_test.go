package fakeapi

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheFakeWatchDoesWhatTheHostDoes(t *testing.T) {
	folder := t.TempDir()
	a := New(t, manifest(), extension.ScopeInfo{Folder: folder})
	var got [][]extension.FileChange
	stop, err := a.Workspace().Watch(context.Background(), extension.WatchSpec{Path: folder, Recursive: true, Excludes: []string{".git/objects", "**/node_modules"}},
		func(c []extension.FileChange) { got = append(got, c) })
	if err != nil {
		t.Fatal(err)
	}
	ws := a.Watches()
	if len(ws) != 1 || ws[0].Path() != folder || !ws[0].Recursive() || len(ws[0].Excludes()) != 2 {
		t.Fatalf("watches = %+v", ws)
	}
	ws[0].Fire(
		extension.FileChange{Kind: extension.FileChanged, Path: filepath.Join(folder, "a.go")},
		extension.FileChange{Kind: extension.FileCreated, Path: filepath.Join(folder, ".git", "objects", "ab")},
		extension.FileChange{Kind: extension.FileCreated, Path: filepath.Join(folder, "x", "node_modules", "m")},
	)
	if len(got) != 0 {
		t.Fatal("the changes came before the test pumped")
	}
	a.Pump()
	if len(got) != 1 || len(got[0]) != 1 || got[0][0].Path != filepath.Join(folder, "a.go") {
		t.Fatalf("got = %+v", got)
	}
	// all excluded: nothing is delivered
	ws[0].Fire(extension.FileChange{Kind: extension.FileChanged, Path: filepath.Join(folder, ".git", "objects", "cd")})
	a.Pump()
	if len(got) != 1 {
		t.Error("an excluded change was delivered")
	}
	stop()
	if !ws[0].Stopped() || len(a.Watches()) != 0 {
		t.Error("the watch is still on")
	}
	ws[0].Fire(extension.FileChange{Kind: extension.FileChanged, Path: filepath.Join(folder, "b.go")})
	a.Pump()
	if len(got) != 1 {
		t.Error("a stopped watch delivered")
	}
}

func TestTheFakeWatchRefusesWhatTheHostRefuses(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	tests := []struct {
		name string
		spec extension.WatchSpec
		fn   func([]extension.FileChange)
		tag  string
	}{
		{"a relative path", extension.WatchSpec{Path: "rel"}, func([]extension.FileChange) {}, "ExtensionWatchPathInvalid"},
		{"no function", extension.WatchSpec{Path: "/abs"}, nil, "ExtensionWatchPathInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.Workspace().Watch(context.Background(), tc.spec, tc.fn)
			route(t, err, tc.tag)
		})
	}
	a.FailWatches(errors.New("WatchBroken", "no watcher"))
	_, err := a.Workspace().Watch(context.Background(), extension.WatchSpec{Path: "/abs"}, func([]extension.FileChange) {})
	route(t, err, "WatchBroken")
	a.FailWatches(nil)
	if _, err := a.Workspace().Watch(context.Background(), extension.WatchSpec{Path: "/abs"}, func([]extension.FileChange) {}); err != nil {
		t.Errorf("after FailWatches(nil): %v", err)
	}
}

func TestTheFakeDialogsAnswerAsTheUserDoes(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	var results []extension.DialogResult
	ask := func(spec extension.DialogSpec) error {
		return a.Dialogs().Ask(spec, func(r extension.DialogResult) { results = append(results, r) })
	}
	spec := extension.DialogSpec{Message: "Trust?", Buttons: []extension.DialogButton{{Label: "Yes"}, {Label: "No", Cancel: true}}}
	if err := ask(spec); err != nil {
		t.Fatal(err)
	}
	if err := ask(spec); err != nil {
		t.Fatal(err)
	}
	open := a.OpenDialogs()
	if len(open) != 2 || open[0].Spec().Message != "Trust?" {
		t.Fatalf("open = %+v", open)
	}
	open[0].Press("Yes")
	open[1].Cancel()
	open[1].Cancel() // a second answer changes nothing
	a.Pump()
	if len(results) != 2 || results[0].Button != 0 || results[0].Cancelled || results[1].Button != 1 || !results[1].Cancelled {
		t.Errorf("results = %+v", results)
	}
	if len(a.OpenDialogs()) != 0 {
		t.Error("answered dialogs are still open")
	}
	// without a cancel button, Cancel answers -1
	if err := ask(extension.DialogSpec{Message: "OK?", Buttons: []extension.DialogButton{{Label: "OK"}}}); err != nil {
		t.Fatal(err)
	}
	a.OpenDialogs()[0].Cancel()
	a.Pump()
	if r := results[len(results)-1]; r.Button != -1 || !r.Cancelled {
		t.Errorf("result = %+v", r)
	}
	for name, bad := range map[string]extension.DialogSpec{
		"no buttons":  {Message: "x"},
		"two cancels": {Message: "x", Buttons: []extension.DialogButton{{Label: "a", Cancel: true}, {Label: "b", Cancel: true}}},
	} {
		t.Run(name, func(t *testing.T) { route(t, ask(bad), "ExtensionDialogInvalid") })
	}
}
