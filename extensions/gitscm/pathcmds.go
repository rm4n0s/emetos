package gitscm

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The commands on one path: Add to .gitignore, Rename and Delete (SPEC §10.30 "Discard").
//
// Ported from VS Code: extensions/git/src/commands.ts (ignore, rename, delete) and extensions/git/src/repository.ts (ignore, move, rm)

// ignoreLine is the line of a .gitignore that names exactly one file, given its path relative to the folder of the .gitignore with
// forward slashes: the characters git reads as a pattern are escaped.
func ignoreLine(rel string) string {
	var b strings.Builder
	for i, c := range rel {
		switch {
		case c == '\\' || c == '[' || c == '*' || c == '?':
			b.WriteByte('\\')
		case i == 0 && (c == '#' || c == '!'):
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if trimmed := strings.TrimRight(out, " "); trimmed != out {
		out = trimmed + strings.Repeat("\\ ", len(out)-len(trimmed))
	}
	return out
}

// appendToIgnoreFile adds the patterns that the .gitignore does not hold yet, on lines of their own at its end. The file is made when it
// is missing; one that does not end with a line break gets one first.
func appendToIgnoreFile(file string, patterns []string) error {
	existing := map[string]bool{}
	needsBreak := false
	if f, err := os.Open(file); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			existing[sc.Text()] = true
		}
		scanErr := sc.Err()
		_ = f.Close()
		if scanErr != nil {
			return scanErr
		}
		if data, err := os.ReadFile(file); err == nil && len(data) > 0 && data[len(data)-1] != '\n' {
			needsBreak = true
		}
	}
	var add strings.Builder
	if needsBreak {
		add.WriteByte('\n')
	}
	n := 0
	for _, p := range patterns {
		if existing[p] {
			continue
		}
		existing[p] = true
		add.WriteString(p + "\n")
		n++
	}
	if n == 0 {
		return nil
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(add.String()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// cmdIgnore is git.ignore: the paths of the selection, or the file of the editor, are added to the .gitignore at the root of their
// repository (made when missing). Nothing is staged.
func (x *gitscm) cmdIgnore(_ context.Context, args any) error {
	repos, groups := byRepository(x.selectionOrFile(args))
	for _, r := range repos {
		seen := map[string]bool{}
		var patterns []string
		for _, t := range groups[r] {
			rel, err := filepath.Rel(r.root, t.res.path)
			if err != nil || seen[t.res.path] {
				continue
			}
			seen[t.res.path] = true
			patterns = append(patterns, ignoreLine(filepath.ToSlash(rel)))
		}
		if len(patterns) == 0 {
			continue
		}
		file := filepath.Join(r.root, ".gitignore")
		r.mutate(mutation{
			name: "ignore",
			run:  func(context.Context, *git.Repo) error { return appendToIgnoreFile(file, patterns) },
		})
	}
	return nil
}

// fileArg is the file a command works on: the URI in the arguments or the active editor's. ok is false for a document that is not a
// file of an open repository.
func (x *gitscm) fileArg(args any) (path string, r *repository, ok bool) {
	uri := x.uriArg(args)
	if uri == "" {
		if doc, has := x.api.Documents().Active(); has {
			uri = doc.URI
		}
	}
	path, ok = x.fileOfURI(uri)
	if !ok {
		return "", nil, false
	}
	r = x.model.forPath(path)
	return path, r, r != nil && r.repo != nil
}

// closeTabsOf closes the editors of a file that have nothing unsaved.
func (x *gitscm) closeTabsOf(path string) {
	uri := fileURI(path)
	var tabs []extension.DocumentTab
	for _, tab := range x.api.Documents().Tabs() {
		if tab.Kind == "text" && !tab.Dirty && tab.URI == uri {
			tabs = append(tabs, tab)
		}
	}
	if len(tabs) > 0 {
		x.api.Documents().CloseTabs(tabs...)
	}
}

// cmdRename is git.rename: an input box with the path of the file relative to the repository, then git mv, then the editor shows the new
// name.
func (x *gitscm) cmdRename(_ context.Context, args any) error {
	path, r, ok := x.fileArg(args)
	if !ok {
		return nil
	}
	from, err := filepath.Rel(r.root, path)
	if err != nil {
		return nil
	}
	from = filepath.ToSlash(from)
	err = x.api.InputBox().Show(extension.InputBoxOptions{Prompt: "New path of the file, relative to the repository", Value: from}, func(value string, ok bool) {
		to := strings.TrimSpace(value)
		if !ok || to == "" || to == from {
			return
		}
		r.mutate(mutation{
			name: "mv",
			run:  func(ctx context.Context, repo *git.Repo) error { return repo.Mv(ctx, from, to) },
			after: func() {
				x.closeTabsOf(path)
				if err := x.api.Documents().OpenDocument(extension.OpenSpec{URI: fileURI(filepath.Join(r.root, filepath.FromSlash(to)))}); err != nil {
					x.noteOpenFailure("the renamed file", err)
				}
			},
		})
	})
	if err != nil {
		x.noteDialogFailure("ask for the new name", err)
	}
	return nil
}

// noteOpenFailure logs that a document could not be shown, unless the scope shows none.
func (x *gitscm) noteOpenFailure(what string, err error) {
	if errTag(err) != "ExtensionDocumentUnavailable" && errTag(err) != "ExtensionDeactivated" {
		x.log.Warn("cannot open "+what, "err", err)
	}
}

// noteDialogFailure logs that a question or an input box could not be shown, unless the scope shows none.
func (x *gitscm) noteDialogFailure(what string, err error) {
	switch errTag(err) {
	case "ExtensionInputBoxUnavailable", "ExtensionDialogsUnavailable", "ExtensionDeactivated":
		return
	}
	x.log.Warn("cannot "+what, "err", err)
}

// cmdDelete is git.delete: git rm of a file that has no uncommitted changes, after a question (git.confirmCommittedDelete), and the editor
// of the file closes unless it has unsaved changes.
func (x *gitscm) cmdDelete(_ context.Context, args any) error {
	path, r, ok := x.fileArg(args)
	if !ok {
		return nil
	}
	for _, g := range []string{groupWorking, groupUntracked, groupIndex, groupMerge} {
		if _, changed := r.findResource(g, path); changed {
			x.api.Notifications().Info("Git: Delete can only be performed on committed files without uncommitted changes.")
			return nil
		}
	}
	run := func() {
		r.mutate(mutation{
			name:  "rm",
			run:   func(ctx context.Context, repo *git.Repo) error { return repo.Rm(ctx, []string{path}, git.RmOptions{}) },
			after: func() { x.closeTabsOf(path) },
		})
	}
	if !x.cfg().confirmCommittedDelete {
		run()
		return nil
	}
	x.confirm(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Are you sure you want to delete '" + filepath.Base(path) + "'?",
		Detail:  "The file is removed from the work tree and from Git (git rm). It stays in the history.",
		Buttons: []extension.DialogButton{{Label: "Delete File"}, cancelButton},
	}, run)
	return nil
}
