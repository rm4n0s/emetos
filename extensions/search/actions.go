package search

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// ---- dismiss and copy ----

// dismiss removes a node of the results (the match, the file or the folder) without touching a file.
func (v *view) dismiss(n ui.TreeNode) {
	switch d := n.Data.(type) {
	case matchNode:
		v.res.removeMatch(d.file, d.match)
	case fileNode:
		v.res.removeFile(d.file)
	case folderNode:
		v.res.removeFolder(d.path)
	default:
		return
	}
	v.status.replaced = ""
	v.rebuildTree()
	v.updateKeys()
	v.refreshView()
}

// dismissFocused is search.action.remove.
func (v *view) dismissFocused() {
	if n, ok := v.focusedNode(); ok {
		v.dismiss(n)
	}
}

// copyText puts text on the clipboard.
func (v *view) copyText(text string) {
	if text == "" {
		return
	}
	if err := v.api.Clipboard().WriteText(text); err != nil {
		v.api.Log().Warn("the clipboard cannot be written", "err", err)
	}
}

// matchLine is the line of a match as the copy commands write it: "  12: the text".
func matchLine(m *matchItem) string {
	return "  " + strconv.Itoa(m.Range.Start.Line+1) + ": " + m.Preview
}

// copyFocused is search.action.copyMatch: the match, the file with its matches, or the folder with its files.
func (v *view) copyFocused() {
	n, ok := v.focusedNode()
	if !ok {
		return
	}
	switch d := n.Data.(type) {
	case matchNode:
		v.copyText(strings.TrimSpace(d.match.Preview))
	case fileNode:
		v.copyText(v.fileText(d.file))
	case folderNode:
		var b strings.Builder
		for _, f := range v.res.files {
			if f.path == d.path || strings.HasPrefix(f.path, d.path+"/") {
				b.WriteString(v.fileText(f))
			}
		}
		v.copyText(b.String())
	}
}

// fileText is a file with its matches, one per line.
func (v *view) fileText(f *fileItem) string {
	var b strings.Builder
	b.WriteString(v.absPath(f))
	b.WriteString("\n")
	for _, m := range f.matches {
		b.WriteString(matchLine(m))
		b.WriteString("\n")
	}
	return b.String()
}

// copyPathFocused is search.action.copyPath: the path of the file or folder.
func (v *view) copyPathFocused() {
	n, ok := v.focusedNode()
	if !ok {
		return
	}
	switch d := n.Data.(type) {
	case fileNode:
		v.copyText(v.absPath(d.file))
	case folderNode:
		v.copyText(v.folder + "/" + d.path)
	case matchNode:
		v.copyText(v.absPath(d.file))
	}
}

// copyAll is search.action.copyAll: every file with its matches.
func (v *view) copyAll() {
	var b strings.Builder
	for _, f := range v.res.files {
		b.WriteString(v.fileText(f))
		b.WriteString("\n")
	}
	v.copyText(strings.TrimRight(b.String(), "\n"))
}

// ---- replace ----

// edit is one replacement of a match.
type edit struct {
	file  *fileItem
	match *matchItem
	text  string
}

// editsOf makes the replacements of some matches.
func (v *view) editsOf(f *fileItem, matches []*matchItem) []edit {
	out := make([]edit, len(matches))
	for i, m := range matches {
		out[i] = edit{file: f, match: m, text: v.replacement(m.Text)}
	}
	return out
}

// replaceFocused is search.action.replace and replaceAllInFile: the match, or the matches of the file or of the folder, that has
// the focus.
func (v *view) replaceFocused() {
	n, ok := v.focusedNode()
	if !ok || !v.replaceOpen {
		return
	}
	v.replaceNode(n)
}

// replaceNode replaces the matches below a node.
func (v *view) replaceNode(n ui.TreeNode) {
	switch d := n.Data.(type) {
	case matchNode:
		v.replace([]edit{{file: d.file, match: d.match, text: v.replacement(d.match.Text)}}, false)
	case fileNode:
		v.replace(v.editsOf(d.file, d.file.matches), false)
	case folderNode:
		var edits []edit
		for _, f := range v.res.files {
			if f.path == d.path || strings.HasPrefix(f.path, d.path+"/") {
				edits = append(edits, v.editsOf(f, f.matches)...)
			}
		}
		v.replace(edits, false)
	}
}

// replaceAll is Replace All: it asks, and then replaces every match of the results (VS Code's confirmation, replaceAll).
func (v *view) replaceAll() {
	if !v.replaceOpen || v.res.empty() {
		return
	}
	var edits []edit
	for _, f := range v.res.files {
		edits = append(edits, v.editsOf(f, f.matches)...)
	}
	v.replace(edits, true)
}

// replaceAllNoConfirm replaces every match at once, without the question; Replace All asks it first.
func (v *view) replaceAllNoConfirm() {
	var edits []edit
	for _, f := range v.res.files {
		edits = append(edits, v.editsOf(f, f.matches)...)
	}
	v.replace(edits, false)
}

// confirmMessage is the question before Replace All (VS Code's replaceAll.*.confirmation.message).
func confirmMessage(occurrences, files int, replaceValue string) string {
	what := "Replace " + strconv.Itoa(occurrences) + " occurrence"
	if occurrences != 1 {
		what += "s"
	}
	what += " across " + strconv.Itoa(files) + " file"
	if files != 1 {
		what += "s"
	}
	if replaceValue != "" {
		return what + " with '" + displayValue(replaceValue) + "'?"
	}
	return what + "?"
}

// nothingReplaced is the message when every replacement was stale: the files changed after the search.
const nothingReplaced = "Nothing was replaced: the files changed since the search. Search again."

// replacedMessage is the message after a replace (VS Code's replaceAll.*.message).
func replacedMessage(occurrences, files int, replaceValue string) string {
	what := "Replaced " + strconv.Itoa(occurrences) + " occurrence"
	if occurrences != 1 {
		what += "s"
	}
	what += " across " + strconv.Itoa(files) + " file"
	if files != 1 {
		what += "s"
	}
	if replaceValue != "" {
		return what + " with '" + displayValue(replaceValue) + "'."
	}
	return what + "."
}

// displayValue shortens a replace value for a message.
func displayValue(s string) string {
	if utf8.RuneCountInString(s) > 50 {
		r := []rune(s)
		return string(r[:50]) + "..."
	}
	return s
}

// replace makes the replacements. With confirm it asks first, with a quick pick that has Replace and Cancel.
func (v *view) replace(edits []edit, confirm bool) {
	if len(edits) == 0 {
		return
	}
	files := map[*fileItem]bool{}
	for _, e := range edits {
		files[e.file] = true
	}
	value := v.replaceBox.Value()
	if !confirm {
		v.doReplace(edits, value)
		return
	}
	err := v.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: confirmMessage(len(edits), len(files), value),
		Items:       []extension.QuickPickItem{{Label: "Replace", Description: "(Enter)"}, {Label: "Cancel"}},
	}, func(index int) {
		if index == 0 {
			v.doReplace(edits, value)
		}
	})
	if err != nil {
		v.api.Notifications().Error("Cannot ask for the replacement: " + err.Error())
	}
}

// doReplace edits the open documents with Documents().Apply (one undo step each), saves them, and writes the files that no
// editor has open through Workspace().ReplaceInFiles on a goroutine; the matches that were replaced leave the results.
func (v *view) doReplace(edits []edit, value string) {
	open := map[string]bool{}
	for _, u := range v.api.Documents().Open() {
		open[u] = true
	}
	byFile := map[*fileItem][]edit{}
	var order []*fileItem
	for _, e := range edits {
		if _, ok := byFile[e.file]; !ok {
			order = append(order, e.file)
		}
		byFile[e.file] = append(byFile[e.file], e)
	}
	var docChanges []extension.ResourceChange
	var docFiles []*fileItem
	var closed []extension.FileReplacement
	var closedFiles []*fileItem
	stale := map[*fileItem]int{}
	for _, f := range order {
		fe := byFile[f]
		if open[f.uri] {
			text, err := v.api.Documents().Read(v.ctx, f.uri)
			if err != nil {
				v.api.Log().Warn("an open document cannot be read", "uri", f.uri, "err", err)
				stale[f] = len(fe)
				continue
			}
			var re []extension.RangeEdit
			for _, e := range fe {
				if textAt(text, e.match.Range) != e.match.Text {
					stale[f]++
					continue
				}
				re = append(re, extension.RangeEdit{Range: e.match.Range, Text: e.text})
			}
			if len(re) > 0 {
				docChanges = append(docChanges, extension.ResourceChange{Kind: "edit", URI: f.uri, Edits: re})
				docFiles = append(docFiles, f)
			}
			continue
		}
		fr := extension.FileReplacement{Path: f.path}
		for _, e := range fe {
			fr.Replacements = append(fr.Replacements, extension.TextReplacement{Range: e.match.Range, Expected: e.match.Text, Text: e.text})
		}
		closed = append(closed, fr)
		closedFiles = append(closedFiles, f)
	}
	var failed []string
	if len(docChanges) > 0 {
		if err := v.api.Documents().Apply(v.ctx, extension.WorkspaceEdit{Changes: docChanges}); err != nil {
			v.api.Log().Warn("the open documents cannot be changed", "err", err)
			failed = append(failed, "open files: "+describe(err))
			docFiles = nil
		}
		for _, f := range docFiles {
			v.saveDocument(f.uri)
		}
	}
	finish := func(rep extension.ReplaceReport, err error) {
		done := map[string]bool{}
		for _, p := range rep.Files {
			done[p] = true
		}
		for _, f := range docFiles {
			done[f.path] = true
		}
		for _, fl := range rep.Failed {
			failed = append(failed, fl.Path+": "+fl.Message)
		}
		if err != nil {
			failed = append(failed, describe(err))
		}
		replaced, filesDone := 0, 0
		for _, f := range order {
			if !done[f.path] {
				continue
			}
			filesDone++
			for _, e := range byFile[f] {
				replaced++
				v.res.removeMatch(f, e.match)
			}
		}
		replaced -= rep.Skipped + sumStale(stale)
		v.status.replaced = replacedMessage(replaced, filesDone, value)
		if replaced <= 0 {
			v.status.replaced = nothingReplaced
		}
		if len(failed) > 0 {
			v.api.Notifications().Warn("Some files could not be changed: " + strings.Join(failed, "; "))
		}
		v.rebuildTree()
		v.updateKeys()
		v.refreshView()
	}
	if len(closed) == 0 {
		finish(extension.ReplaceReport{}, nil)
		return
	}
	_ = closedFiles
	go func() {
		rep, err := v.api.Workspace().ReplaceInFiles(v.ctx, closed)
		v.api.Post(func() { finish(rep, err) })
	}()
}

func sumStale(m map[*fileItem]int) int {
	n := 0
	for _, c := range m {
		n += c
	}
	return n
}

// describe is the text of an error for a message.
func describe(err error) string {
	if e, ok := errors.FromError(err); ok && e.Message != "" {
		return e.Message
	}
	return err.Error()
}

// saveDocument saves an open document after its replacement, as VS Code's replace does.
func (v *view) saveDocument(uri string) {
	err := v.api.Events().Request(v.ctx, extension.TopicDocumentSave, extension.DocumentURIRequest{URI: uri}, func(_ extension.Event, err error) {
		if err != nil {
			v.api.Log().Warn("a document cannot be saved after the replacement", "uri", uri, "err", err)
		}
	})
	if err != nil {
		v.api.Log().Warn("a save cannot be requested", "uri", uri, "err", err)
	}
}

// textAt is the text of a range (lines from 0, columns in bytes) of a text with "\n" line ends; "" when the range is outside.
func textAt(text string, r extension.Range) string {
	start, ok1 := offsetOf(text, r.Start)
	end, ok2 := offsetOf(text, r.End)
	if !ok1 || !ok2 || end < start {
		return ""
	}
	return text[start:end]
}

func offsetOf(text string, p extension.Position) (int, bool) {
	off := 0
	for line := 0; line < p.Line; line++ {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return 0, false
		}
		off += i + 1
	}
	end := strings.IndexByte(text[off:], '\n')
	if end < 0 {
		end = len(text) - off
	}
	if p.Col < 0 || p.Col > end {
		return 0, false
	}
	return off + p.Col, true
}

// ---- the replace preview ----

// previewReplace opens the diff editor with the file as it is and as the replacement would make it (VS Code's openReplacePreview).
// Accept replaces the matches of the file.
func (v *view) previewReplace(f *fileItem) {
	text, err := v.api.Documents().Read(v.ctx, f.uri)
	if err != nil {
		v.api.Notifications().Error("Cannot read " + f.path + ": " + err.Error())
		return
	}
	var parts []string
	last := 0
	for _, m := range f.matches {
		s, ok1 := offsetOf(text, m.Range.Start)
		e, ok2 := offsetOf(text, m.Range.End)
		if !ok1 || !ok2 || s < last || text[s:e] != m.Text {
			continue
		}
		parts = append(parts, text[last:s], v.replacement(m.Text))
		last = e
	}
	parts = append(parts, text[last:])
	proposed := strings.Join(parts, "")
	edits := v.editsOf(f, f.matches)
	value := v.replaceBox.Value()
	go func() {
		res, err := v.api.Editors().OpenProposedChange(v.ctx, extension.ProposedChange{
			URI: f.uri, Original: text, Proposed: proposed, Title: f.Name() + " ↔ " + f.Name() + " (Replace Preview)",
		})
		if err != nil || !res.Accepted {
			return
		}
		v.api.Post(func() { v.doReplace(edits, value) })
	}()
}
