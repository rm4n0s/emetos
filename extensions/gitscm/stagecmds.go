package gitscm

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The commands that stage, unstage and revert by lines (SPEC §10.30 "Stage and unstage", "The peek"): the ones the peek of a change runs
// with (uri, changes, index), the ones that work on the lines the selections touch, and the ones the toolbars of the diff editor's gutter
// run. The index is written from the original text and the modified text with git hash-object and git update-index, so the file in the
// work tree is never touched by a stage. The changes come from the host (Documents().LineChanges, the diff of the marks) and are checked
// against the file as it is when the command runs: a change that no longer matches is refused.
//
// Ported from VS Code: extensions/git/src/commands.ts (stageChange, revertChange, unstageChange, stageSelectedChanges,
// revertSelectedRanges, unstageSelectedRanges, diffStageHunk, diffStageSelection, stageHunkAtCursor, _stageChanges, _revertChanges,
// _unstageChanges)

// fileTarget is a file of an open, trusted repository.
type fileTarget struct {
	r    *repository
	path string
	// rel is the path relative to the root of the repository, with slashes.
	rel string
}

// fileTargetOf finds the repository of a file: URI. false for any other URI, a file of no open repository and an untrusted repository.
func (x *gitscm) fileTargetOf(uri string) (fileTarget, bool) {
	path, ok := uriPath(uri)
	if !ok {
		return fileTarget{}, false
	}
	r := x.model.forPath(path)
	if r == nil || r.repo == nil {
		return fileTarget{}, false
	}
	rel, err := filepath.Rel(r.root, path)
	if err != nil {
		return fileTarget{}, false
	}
	return fileTarget{r: r, path: path, rel: filepath.ToSlash(rel)}, true
}

// stageRefused is why a command by lines changed nothing, in the words the user is told.
func stageRefused(err error) (string, bool) {
	switch errTag(err) {
	case "GitStageStale":
		return "The change does not match the file any more: it was edited after it was shown.", true
	case "GitStageNoChanges":
		return "The selection range does not contain any changes.", true
	case "GitStageNotInIndex":
		return "The file is not in the index: stage it first.", true
	case "GitStageUnstageBlocked":
		return "The file has changes that are not staged: unstage the file or stage them first.", true
	case "GitStageNoHunk":
		return "No hunk found at cursor position.", true
	}
	return "", false
}

// refuse tells the user why nothing was staged, when it is one of the ways a command by lines is refused.
func (x *gitscm) refuse(err error) bool {
	msg, ok := stageRefused(err)
	if ok {
		x.api.Notifications().Info(msg)
	}
	return ok
}

// lineEnds is a text with the line breaks "\n" only, the way a document holds it.
func lineEnds(text string) string { return strings.ReplaceAll(text, "\r\n", "\n") }

// indexVersion is the text of the index version of a file, as it is stored (line breaks and all). The error has the tag
// GitStageNotInIndex for a file the index does not have.
func indexVersion(ctx context.Context, repo *git.Repo, rel string) (string, error) {
	data, found, err := repo.ReadVersion(ctx, "", rel)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("GitStageNotInIndex", "the file is not in the index", "path", rel)
	}
	return string(data), nil
}

// headVersion is the text of the version of a file in HEAD; empty for a file HEAD does not have (a new file, a repository with no commit).
func headVersion(ctx context.Context, repo *git.Repo, rel string) (string, error) {
	data, _, err := repo.ReadVersion(ctx, "HEAD", rel)
	return string(data), err
}

// indexChoice picks, among the changes of the file against its index version, those a command works on. It runs on a goroutine, with the
// changes the host finds for the texts as they are now.
type indexChoice func(fresh []extension.LineChange, modified *textDoc) ([]extension.LineChange, error)

// stageChosen stages the changes the choice picks from the changes of a text against the index version of its file.
func (x *gitscm) stageChosen(t fileTarget, modified string, choose indexChoice) {
	t.r.mutate(mutation{
		name:   "stage",
		failed: x.refuse,
		run: func(ctx context.Context, repo *git.Repo) error {
			original, err := indexVersion(ctx, repo, t.rel)
			if err != nil {
				return err
			}
			fresh, err := x.api.Documents().LineChanges(ctx, original, modified)
			if err != nil {
				return err
			}
			chosen, err := choose(fresh, newTextDoc(modified))
			if err != nil {
				return err
			}
			result := applyLineChanges(newTextDoc(original), newTextDoc(matchLineBreaks(original, modified)), chosen)
			return repo.StageContent(ctx, t.rel, []byte(result))
		},
	})
}

// pickChange picks the change a peek named, if the file still has it. The error has the tag GitStageStale.
func pickChange(fresh []extension.LineChange, c extension.LineChange) ([]extension.LineChange, error) {
	if !slices.Contains(fresh, c) {
		return nil, errors.New("GitStageStale", "the change is not one of the changes of the file", "change", c)
	}
	return []extension.LineChange{c}, nil
}

// pickSelected picks the changes the lines of the selections touch, cut to the lines. The error has the tag GitStageNoChanges.
func pickSelected(fresh []extension.LineChange, modified *textDoc, selections []extension.Selection) ([]extension.LineChange, error) {
	chosen := selectedChanges(modified, fresh, toLineRanges(selections, modified))
	if len(chosen) == 0 {
		return nil, errors.New("GitStageNoChanges", "the selection does not touch a change")
	}
	return chosen, nil
}

// pickHunkAt picks the change that holds the cursor. The error has the tag GitStageNoHunk.
func pickHunkAt(fresh []extension.LineChange, modified *textDoc, cursor textPos) ([]extension.LineChange, error) {
	for _, c := range fresh {
		if modifiedRange(modified, c).contains(cursor) {
			return []extension.LineChange{c}, nil
		}
	}
	return nil, errors.New("GitStageNoHunk", "no change holds the cursor")
}

// keepAllBut is every change but the one a peek named, if the file still has it. The error has the tag GitStageStale.
func keepAllBut(fresh []extension.LineChange, c extension.LineChange) ([]extension.LineChange, error) {
	i := slices.Index(fresh, c)
	if i < 0 {
		return nil, errors.New("GitStageStale", "the change is not one of the changes of the file", "change", c)
	}
	return slices.Delete(slices.Clone(fresh), i, i+1), nil
}

// keepUntouched is the changes no selection touches, which stay when the others are reverted. The error has the tag GitStageNoChanges
// when no change is touched.
func keepUntouched(fresh []extension.LineChange, modified *textDoc, selections []extension.Selection) ([]extension.LineChange, error) {
	var kept []extension.LineChange
	for _, c := range fresh {
		r := modifiedRange(modified, c)
		touched := false
		for _, s := range selections {
			a, b := textPos{s.Anchor.Line, s.Anchor.Col}, textPos{s.Active.Line, s.Active.Col}
			if _, hit := (textRange{a, b}).ordered().intersection(r); hit {
				touched = true
				break
			}
		}
		if !touched {
			kept = append(kept, c)
		}
	}
	if len(kept) == len(fresh) {
		return nil, errors.New("GitStageNoChanges", "the selection does not touch a change")
	}
	return kept, nil
}

// requireStaged refuses to unstage a change of a file that has changes which are not staged. The error has the tag
// GitStageUnstageBlocked.
func requireStaged(rel string, unstaged []extension.LineChange) error {
	if len(unstaged) > 0 {
		return errors.New("GitStageUnstageBlocked", "the file has changes that are not staged", "path", rel)
	}
	return nil
}

// changeArgs reads the arguments of a command of the peek and finds the file and its text.
func (x *gitscm) changeArgs(ctx context.Context, args any) (t fileTarget, modified string, changes []extension.LineChange, index int, ok bool) {
	uri, changes, index, ok := extension.ReadQuickDiffArgs(args)
	if !ok {
		return fileTarget{}, "", nil, 0, false
	}
	t, ok = x.fileTargetOf(uri)
	if !ok {
		return fileTarget{}, "", nil, 0, false
	}
	modified, err := x.api.Documents().Read(ctx, uri)
	if err != nil {
		x.noteReadFailure(uri, err)
		return fileTarget{}, "", nil, 0, false
	}
	return t, modified, changes, index, true
}

// noteReadFailure logs that the text of a document could not be read, unless the scope has no documents.
func (x *gitscm) noteReadFailure(uri string, err error) {
	switch errTag(err) {
	case "ExtensionDocumentUnavailable", "ExtensionDeactivated":
		return
	}
	x.log.Warn("cannot read a document", "uri", uri, "err", err)
}

// activeFile is the text editor in front, as a file of an open repository with its text; false when there is none.
func (x *gitscm) activeFile(ctx context.Context) (t fileTarget, modified string, sels []extension.Selection, ok bool) {
	doc, has := x.api.Documents().Active()
	if !has {
		return fileTarget{}, "", nil, false
	}
	t, ok = x.fileTargetOf(doc.URI)
	if !ok {
		return fileTarget{}, "", nil, false
	}
	modified, err := x.api.Documents().Read(ctx, doc.URI)
	if err != nil {
		x.noteReadFailure(doc.URI, err)
		return fileTarget{}, "", nil, false
	}
	return t, modified, doc.Selections, true
}

// cmdStageChange is git.stageChange: the change of the peek goes to the index.
func (x *gitscm) cmdStageChange(ctx context.Context, args any) error {
	t, modified, changes, index, ok := x.changeArgs(ctx, args)
	if !ok {
		return nil
	}
	target := changes[index]
	x.stageChosen(t, modified, func(fresh []extension.LineChange, _ *textDoc) ([]extension.LineChange, error) {
		return pickChange(fresh, target)
	})
	return nil
}

// cmdStageSelectedRanges is git.stageSelectedRanges: the changes the selections touch go to the index, cut to the selected lines.
func (x *gitscm) cmdStageSelectedRanges(ctx context.Context, _ any) error {
	t, modified, sels, ok := x.activeFile(ctx)
	if !ok {
		return nil
	}
	x.stageChosen(t, modified, func(fresh []extension.LineChange, doc *textDoc) ([]extension.LineChange, error) {
		return pickSelected(fresh, doc, sels)
	})
	return nil
}

// stageHunkAtCursor is git.diff.stageHunk without a hunk: the change the cursor of the editor in front is in goes to the index.
func (x *gitscm) stageHunkAtCursor(ctx context.Context) {
	t, modified, sels, ok := x.activeFile(ctx)
	if !ok || len(sels) == 0 {
		return
	}
	cursor := textPos{sels[0].Active.Line, sels[0].Active.Col}
	x.stageChosen(t, modified, func(fresh []extension.LineChange, doc *textDoc) ([]extension.LineChange, error) {
		return pickHunkAt(fresh, doc, cursor)
	})
}

// cmdDiffStageHunk is git.diff.stageHunk: the toolbar of a hunk in the gutter of the diff editor gives the text of the index with the
// hunk applied; run from the palette it stages the change the cursor is in.
func (x *gitscm) cmdDiffStageHunk(ctx context.Context, args any) error {
	if dc, ok := extension.ReadDiffEditorArgs(args); ok {
		x.stageDiffText(dc)
		return nil
	}
	x.stageHunkAtCursor(ctx)
	return nil
}

// cmdDiffStageSelection is git.diff.stageSelection: the toolbar of a selection in the gutter of the diff editor gives the text of the
// index with the selected lines applied.
func (x *gitscm) cmdDiffStageSelection(_ context.Context, args any) error {
	if dc, ok := extension.ReadDiffEditorArgs(args); ok {
		x.stageDiffText(dc)
	}
	return nil
}

// stageDiffText makes the text of the toolbar the index version of the modified file.
func (x *gitscm) stageDiffText(dc extension.DiffEditorContext) {
	t, ok := x.fileTargetOf(dc.ModifiedURI)
	if !ok {
		return
	}
	t.r.mutate(mutation{
		name:   "stage",
		failed: x.refuse,
		run: func(ctx context.Context, repo *git.Repo) error {
			version, err := indexVersion(ctx, repo, t.rel)
			if err != nil {
				return err
			}
			return repo.StageContent(ctx, t.rel, []byte(matchLineBreaks(version, dc.OriginalWithModifiedChanges)))
		},
	})
}

// revertKeep picks the changes of the file against its index version that stay in the document; the others are reverted. It runs on a
// goroutine, with the changes the host finds for the texts as they are now.
type revertKeep func(fresh []extension.LineChange, modified *textDoc) ([]extension.LineChange, error)

// revertChosen puts the original lines back in the document for the changes that keep does not name, as one edit that Ctrl+Z undoes, and
// saves the document (VS Code does).
func (x *gitscm) revertChosen(t fileTarget, modified string, keep revertKeep) {
	uri := fileURI(t.path)
	var result string
	t.r.mutate(mutation{
		name:   "revert",
		failed: x.refuse,
		run: func(ctx context.Context, repo *git.Repo) error {
			version, err := indexVersion(ctx, repo, t.rel)
			if err != nil {
				return err
			}
			original := lineEnds(version)
			fresh, err := x.api.Documents().LineChanges(ctx, original, modified)
			if err != nil {
				return err
			}
			kept, err := keep(fresh, newTextDoc(modified))
			if err != nil {
				return err
			}
			result = applyLineChanges(newTextDoc(original), newTextDoc(modified), kept)
			return nil
		},
		after: func() {
			now, err := x.api.Documents().Read(t.r.ctx, uri)
			if err != nil || now != modified {
				x.api.Notifications().Info("The file was edited since the change was shown: nothing was reverted.")
				return
			}
			edit, ok := replaceEdit(modified, result)
			if !ok {
				return
			}
			err = x.api.Documents().Apply(t.r.ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "edit", URI: uri, Edits: []extension.RangeEdit{edit}}}})
			if err != nil {
				x.log.Warn("cannot revert a change", "path", t.path, "err", err)
				x.api.Notifications().Error("Git: cannot revert the change: " + describe(err))
				return
			}
			x.saveAfterRevert(t, uri)
		},
	})
}

// saveAfterRevert saves the document the revert changed, as VS Code does: the file on disk is the reverted one.
func (x *gitscm) saveAfterRevert(t fileTarget, uri string) {
	ctx := t.r.ctx
	t.r.beginOwn()
	go func() {
		err := x.api.Documents().SaveAll(ctx, []string{uri})
		x.api.Post(func() {
			if !t.r.disposed {
				t.r.endOwn()
			}
			if err != nil && errTag(err) != "ExtensionDocumentUnavailable" && errTag(err) != "ExtensionDeactivated" {
				x.log.Warn("cannot save a reverted file", "path", t.path, "err", err)
			}
		})
	}()
}

// replaceEdit is the one edit that turns the old text into the new: the lines from the first that differs to the last that differs, cut to
// the characters that differ at their ends. false when the texts are the same.
func replaceEdit(oldText, newText string) (extension.RangeEdit, bool) {
	if oldText == newText {
		return extension.RangeEdit{}, false
	}
	prefix := 0
	for prefix < len(oldText) && prefix < len(newText) && oldText[prefix] == newText[prefix] {
		prefix++
	}
	for prefix > 0 && prefix < len(oldText) && !utf8.RuneStart(oldText[prefix]) {
		prefix--
	}
	suffix := 0
	for suffix < len(oldText)-prefix && suffix < len(newText)-prefix && oldText[len(oldText)-1-suffix] == newText[len(newText)-1-suffix] {
		suffix++
	}
	for suffix > 0 && !utf8.RuneStart(oldText[len(oldText)-suffix]) {
		suffix--
	}
	end := len(oldText) - suffix
	return extension.RangeEdit{
		Range: extension.Range{Start: positionAt(oldText, prefix), End: positionAt(oldText, end)},
		Text:  newText[prefix : len(newText)-suffix],
	}, true
}

// positionAt is the line and the column in bytes of an offset of a text with "\n" line breaks.
func positionAt(text string, offset int) extension.Position {
	before := text[:offset]
	line := strings.Count(before, "\n")
	return extension.Position{Line: line, Col: offset - (strings.LastIndex(before, "\n") + 1)}
}

// cmdRevertChange is git.revertChange: the lines of the change of the peek go back to the original.
func (x *gitscm) cmdRevertChange(ctx context.Context, args any) error {
	t, modified, changes, index, ok := x.changeArgs(ctx, args)
	if !ok {
		return nil
	}
	target := changes[index]
	x.revertChosen(t, modified, func(fresh []extension.LineChange, _ *textDoc) ([]extension.LineChange, error) {
		return keepAllBut(fresh, target)
	})
	return nil
}

// cmdRevertSelectedRanges is git.revertSelectedRanges: every change a selection touches goes back to the original, whole (the lines
// outside the selection too).
func (x *gitscm) cmdRevertSelectedRanges(ctx context.Context, _ any) error {
	t, modified, sels, ok := x.activeFile(ctx)
	if !ok {
		return nil
	}
	x.revertChosen(t, modified, func(fresh []extension.LineChange, doc *textDoc) ([]extension.LineChange, error) {
		return keepUntouched(fresh, doc, sels)
	})
	return nil
}

// cmdUnstageSelectedRanges is git.unstageSelectedRanges: in the staged side of a diff, the changes the selections touch go back to the
// version in HEAD, in the index.
func (x *gitscm) cmdUnstageSelectedRanges(_ context.Context, _ any) error {
	doc, has := x.api.Documents().Active()
	if !has {
		return nil
	}
	g, ok := parseGitURI(doc.URI)
	if !ok || g.Ref != refIndex {
		return nil
	}
	var r *repository
	for _, cand := range x.model.order {
		if cand.id == g.RepoID && cand.repo != nil {
			r = cand
		}
	}
	if r == nil {
		return nil
	}
	path := g.filePath(r.root)
	if _, staged := r.findResource(groupIndex, path); !staged {
		return nil
	}
	rel := g.Path
	sels := doc.Selections
	r.mutate(mutation{
		name:   "unstage",
		failed: x.refuse,
		run: func(ctx context.Context, repo *git.Repo) error {
			head, err := headVersion(ctx, repo, rel)
			if err != nil {
				return err
			}
			staged, err := indexVersion(ctx, repo, rel)
			if err != nil {
				return err
			}
			indexText, headText := lineEnds(staged), lineEnds(head)
			fresh, err := x.api.Documents().LineChanges(ctx, headText, indexText)
			if err != nil {
				return err
			}
			indexDoc := newTextDoc(indexText)
			chosen, err := pickSelected(fresh, indexDoc, sels)
			if err != nil {
				return err
			}
			inverted := make([]extension.LineChange, len(chosen))
			for i, c := range chosen {
				inverted[i] = invertLineChange(c)
			}
			result := applyLineChanges(indexDoc, newTextDoc(headText), inverted)
			return repo.StageContent(ctx, rel, []byte(matchLineBreaks(staged, result)))
		},
	})
	return nil
}

// cmdUnstageChange is git.unstageChange: a change of the file against HEAD goes back to HEAD in the index. It works when the file has no
// changes that are not staged (VS Code also undoes those, with a line mapping that is only right when there are none).
func (x *gitscm) cmdUnstageChange(ctx context.Context, args any) error {
	t, modified, changes, index, ok := x.changeArgs(ctx, args)
	if !ok {
		return nil
	}
	target := changes[index]
	t.r.mutate(mutation{
		name:   "unstage",
		failed: x.refuse,
		run: func(ctx context.Context, repo *git.Repo) error {
			staged, err := indexVersion(ctx, repo, t.rel)
			if err != nil {
				return err
			}
			head, err := headVersion(ctx, repo, t.rel)
			if err != nil {
				return err
			}
			unstaged, err := x.api.Documents().LineChanges(ctx, lineEnds(staged), modified)
			if err != nil {
				return err
			}
			if err := requireStaged(t.rel, unstaged); err != nil {
				return err
			}
			against, err := x.api.Documents().LineChanges(ctx, lineEnds(head), modified)
			if err != nil {
				return err
			}
			if _, err := pickChange(against, target); err != nil {
				return err
			}
			result := applyLineChanges(newTextDoc(modified), newTextDoc(lineEnds(head)), []extension.LineChange{invertLineChange(target)})
			return repo.StageContent(ctx, t.rel, []byte(matchLineBreaks(staged, result)))
		},
	})
	return nil
}
