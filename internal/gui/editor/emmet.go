package editor

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/emmet"
	"github.com/rm4n0s/emetos/internal/core/snippet"
)

// Ported from VS Code: extensions/emmet/src/abbreviationActions.ts (expandEmmetAbbreviation)

// expandEmmet is the command editor.emmet.action.expandAbbreviation and the Tab key with
// emmet.triggerExpansionOnTab: every cursor expands the abbreviation before it (or its selection)
// and the expansions are inserted as one snippet. When nothing expands, or the expansion fails,
// and emmet.triggerExpansionOnTab is on, the key does what Tab does; a failure is returned as
// the error of the command, which the shell shows as a notification.
//
// Ported from VS Code: extensions/emmet/src/abbreviationActions.ts
func (vm *ViewModel) expandEmmet() error {
	opts := vm.opts.Emmet
	fallback := func() error {
		if opts.TriggerExpansionOnTab {
			return vm.apply(vm.commands().Tab(vm.cursors.Selections()))
		}
		return nil
	}
	syntax, ok := emmet.SyntaxFor(vm.doc.LanguageID(),
		emmet.IncludeLanguagesFromSettings(opts.IncludeLanguages), emmet.ExcludedLanguagesFromSettings(opts.ExcludeLanguages))
	if !ok {
		return fallback()
	}
	variables, err := emmet.VariablesFromSettings(opts.Variables)
	if err != nil {
		return err
	}
	profiles, _ := opts.SyntaxProfiles.(map[string]any)
	snap := vm.doc.Snapshot()
	sels := vm.cursors.Selections()
	edits := make([]snippet.Edit, 0, len(sels))
	// each cursor on its own, last first, as VS Code does (a failure then leaves the first ones out)
	for i := len(sels) - 1; i >= 0; i-- {
		sel := sels[i]
		req := emmet.ExpandRequest{Syntax: syntax, Variables: variables, Profiles: profiles}
		pos := sel.Active
		line, err := snap.LineContent(pos.Line)
		if err != nil {
			return err
		}
		base := 0
		if sel.IsEmpty() {
			req.TextBefore = line[:min(pos.Col, len(line))]
		} else {
			r := sel.Range()
			req.Selection = snap.Text(r)
			pos = r.Start
			base = r.Start.Col
			if r.Start.Line != r.End.Line {
				// a selection of several lines is wrapped by Emmet's other command, not expanded
				continue
			}
		}
		res, expanded, err := emmet.ExpandAt(req)
		if err != nil {
			if fb := fallback(); fb != nil {
				return fb
			}
			return err
		}
		if !expanded {
			continue
		}
		edits = append(edits, snippet.Edit{
			Range:    buffer.Range{Start: buffer.Position{Line: pos.Line, Col: base + res.Start}, End: buffer.Position{Line: pos.Line, Col: base + res.End}},
			Template: res.Text,
		})
	}
	if len(edits) == 0 {
		return fallback()
	}
	vm.dismissSnippetPicker(false)
	err = vm.snippets().Apply(edits, snippet.DefaultInsertOptions())
	vm.history.Clear()
	vm.maxLineWidth = 0
	vm.Sync()
	vm.NotifyListeners()
	return err
}

func emmetActions(a map[string]action) {
	a["editor.emmet.action.expandAbbreviation"] = func(vm *ViewModel, _ any) error { return vm.expandEmmet() }
}
