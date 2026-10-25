package app

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// The dev commands of the merge editor (SPEC §15.2, D15): dev.openMergeEditor opens a file with conflict markers (or four
// files) in the merge editor and dev.acceptMerge resolves a conflict of the merge editor of the active group, so that a
// proof can show a merge that is half resolved.

// openMergeArguments are the arguments of dev.openMergeEditor.
type openMergeArguments struct {
	// Path is the file with conflict markers; empty for the form with four files.
	Path string
	// Input1, Input2, Base and Result are the four files of `--merge`: the incoming side, the current side, the base and the
	// result.
	Input1, Input2, Base, Result string
}

// parseOpenMerge reads the arguments of dev.openMergeEditor: {"path": "..."} or just the path as a string (a file with
// conflict markers), or {"input1": ..., "input2": ..., "base": ..., "result": ...} (four files, as `emetos --merge`).
func parseOpenMerge(args any) (openMergeArguments, error) {
	var a openMergeArguments
	switch v := args.(type) {
	case string:
		a.Path = v
	case map[string]any:
		a.Path, _ = v["path"].(string)
		a.Input1, _ = v["input1"].(string)
		a.Input2, _ = v["input2"].(string)
		a.Base, _ = v["base"].(string)
		a.Result, _ = v["result"].(string)
	}
	files := 0
	for _, f := range []string{a.Input1, a.Input2, a.Base, a.Result} {
		if f != "" {
			files++
		}
	}
	if (a.Path == "") == (files == 0) || (files != 0 && files != 4) {
		return a, errors.New("DevOpenMergeInvalid",
			`dev.openMergeEditor needs {"path": ...} or {"input1": ..., "input2": ..., "base": ..., "result": ...}`, "arguments", args)
	}
	return a, nil
}

// mergeSideChoices are the sides dev.acceptMerge knows and what each one resolves a conflict with.
var mergeSideChoices = map[string]diff.Choice{
	"incoming": diff.AcceptInput1,
	"current":  diff.AcceptInput2,
	"both":     diff.AcceptBoth,
	"base":     diff.AcceptBase,
}

// acceptMergeArguments are the arguments of dev.acceptMerge.
type acceptMergeArguments struct {
	// Conflict is the index of the conflict, counted from 0 among the conflicts of the merge.
	Conflict int
	// Choice is what the conflict is resolved with.
	Choice diff.Choice
}

// parseAcceptMerge reads the arguments of dev.acceptMerge: {"conflict": n, "side": "incoming"|"current"|"both"|"base"}.
func parseAcceptMerge(args any) (acceptMergeArguments, error) {
	var a acceptMergeArguments
	m, ok := args.(map[string]any)
	if !ok {
		return a, errors.New("DevAcceptMergeInvalid", `dev.acceptMerge needs {"conflict": ..., "side": ...}`, "arguments", args)
	}
	n, ok := m["conflict"].(float64)
	if !ok || n != float64(int(n)) || n < 0 {
		return a, errors.New("DevAcceptMergeConflictInvalid", `the conflict of dev.acceptMerge is a whole number counted from 0`, "conflict", m["conflict"])
	}
	a.Conflict = int(n)
	side, _ := m["side"].(string)
	choice, ok := mergeSideChoices[side]
	if !ok {
		return a, errors.New("DevAcceptMergeSideInvalid", `the side of dev.acceptMerge is "incoming", "current", "both" or "base"`, "side", m["side"])
	}
	a.Choice = choice
	return a, nil
}

// bindDevMerge gives dev.openMergeEditor and dev.acceptMerge (SPEC §15.2) their handlers.
func (s *shellState) bindDevMerge() {
	if !DevCommandsEnabled() {
		return
	}
	win := PeekWorkspace(s.Context())
	for id, handler := range map[string]command.Handler{
		"dev.openMergeEditor": func(c command.Context, args any) error { return s.devOpenMergeEditor(c.Ctx, args) },
		"dev.acceptMerge":     func(_ command.Context, args any) error { return s.devAcceptMerge(args) },
	} {
		remove, err := win.Commands.SetHandler(id, handler)
		if err != nil {
			win.Process.Logger.Error("cannot bind "+id, "err", err)
			continue
		}
		s.removeDevMerge = append(s.removeDevMerge, remove)
	}
}

// devOpenMergeEditor opens a file with conflict markers in the merge editor of the active group, at once (the file is read
// and the merge computed on the UI thread, so that the next step of a script finds the merge editor), or the four files of
// `--merge` (read in the background). A relative path is relative to the folder Emetos runs in, as dev.openFile's is.
func (s *shellState) devOpenMergeEditor(ctx context.Context, args any) error {
	a, err := parseOpenMerge(args)
	if err != nil {
		return err
	}
	win := PeekWorkspace(s.Context())
	if a.Path == "" {
		var paths [4]string
		for i, p := range []string{a.Input1, a.Input2, a.Base, a.Result} {
			if paths[i], err = s.devMergePath(p); err != nil {
				return err
			}
		}
		s.openMergeFiles(paths[0], paths[1], paths[2], paths[3], "", "", func(_ *editorgroup.MergeInput, err error) {
			if err != nil {
				win.Process.Logger.Warn("dev.openMergeEditor failed", "err", err)
			}
		})
		return nil
	}
	path, err := s.devMergePath(a.Path)
	if err != nil {
		return err
	}
	doc, err := s.openFile(ctx, path)
	if err != nil {
		return err
	}
	// a merge editor that shows the file already is the answer (the file holds its result now, with no markers)
	if group, input := s.mergeEditorOf(doc); input != nil {
		win.Documents.Release(doc)
		group.Model.SetActive(input)
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
		s.refreshKeys()
		return nil
	}
	prep, err := markersMerge(ctx, doc.Text())
	if err != nil {
		win.Documents.Release(doc)
		return err
	}
	s.openMerge(prep, editorgroup.MergeSource{Kind: editorgroup.MergeMarkers, Result: doc.URI().Path}, doc, nil)
	return nil
}

// devAcceptMerge resolves a conflict of the merge editor of the active group with a side, as the action above the conflict
// does: one undoable edit of the result.
func (s *shellState) devAcceptMerge(args any) error {
	a, err := parseAcceptMerge(args)
	if err != nil {
		return err
	}
	p := s.activeMergePane()
	if p == nil {
		return errors.New("DevAcceptMergeNoEditor", "the active editor is not a merge editor", "conflict", a.Conflict)
	}
	return p.model.Accept(a.Conflict, a.Choice)
}

// devMergePath is the absolute path of a file of dev.openMergeEditor: a relative path is relative to the folder of the
// window, or to the folder Emetos runs in when none is open (as dev.openFile's is).
func (s *shellState) devMergePath(path string) (string, error) {
	if folder := PeekWorkspace(s.Context()).Folder; folder != "" && !filepath.IsAbs(path) {
		return filepath.Join(folder, path), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.NewErr("DevOpenMergePathFailed", err).SetMetadata("path", path)
	}
	return abs, nil
}
