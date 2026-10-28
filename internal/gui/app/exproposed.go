package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The proposed changes of extensions (SPEC §10.16 "As built (step 13.3)"): Editors().OpenProposedChange shows the diff editor
// of a change with the current text of a file on the left, read-only, and the proposed text on the right, which the user
// may edit, and Accept and Reject at the right of the tab strip. The two texts are documents of their own that live in the
// diff editor only (they are not in the window's set of documents, so no language server, backup, auto save or file watcher
// sees them), and the editor is a DiffInput that has a DiffProposal: it is never dirty, asks nothing when it closes, is not
// saved with the window and cannot be split. Nothing is ever written to the file: the extension that asked decides what
// happens to it after the answer.

const (
	// ProposedPrefix is the start of the path of the virtual URIs of the texts of a proposed change:
	// emetos-virtual:/proposed/<n>/current/<path> and emetos-virtual:/proposed/<n>/proposed/<path>.
	ProposedPrefix = "/proposed/"
)

// OpenProposed implements exthost.EditorHost: the diff editor of the change opens in the group on the side of the active
// group (proposalGroup), kept, as its active editor. The answer is given when the user accepts or rejects, or closes the tab; the function that is returned closes the
// tab without an answer.
func (h extEditorHost) OpenProposed(change extension.ProposedChange, answer func(extension.ProposedResult)) (func(), error) {
	s, err := h.shellOrErr("show a proposed change")
	if err != nil {
		return nil, err
	}
	win := h.w
	uri, err := document.ParseURI(change.URI)
	if err != nil {
		return nil, err
	}
	if uri.Scheme != document.SchemeFile {
		return nil, errors.New("ExtEditorsProposalFileOnly", "only a file can have a proposed change", "uri", change.URI)
	}
	name := filepath.Base(uri.Path)
	title := change.Title
	if title == "" {
		title = name + " (proposed)"
	}
	lang := win.Process.Languages.Detect(uri.Path, "")
	if change.Language != "" {
		if id, ok := win.Process.Languages.ByName(strings.TrimSpace(change.Language)); ok {
			lang = id
		}
	}
	// the documents have "\n" lines; the answer gets the line endings of the file back
	crlf := strings.Contains(change.Original, "\r\n") || (change.Original == "" && strings.Contains(change.Proposed, "\r\n"))
	original, proposed := normalizeLines(change.Original), normalizeLines(change.Proposed)
	n := win.proposals.Add(1)
	rel := strings.TrimPrefix(filepath.ToSlash(uri.Path), "/")
	base := document.NewOptions()
	base.LanguageID = lang
	left, right := base, base
	left.ReadOnly, left.Label = true, name
	right.Label = title
	current := document.New(document.VirtualURI(fmt.Sprintf("%s%d/current/%s", ProposedPrefix, n, rel)), original, left)
	modified := document.New(document.VirtualURI(fmt.Sprintf("%s%d/proposed/%s", ProposedPrefix, n, rel)), proposed, right)

	input := editorgroup.NewDiffInput(current, modified, s.inputOptions())
	proposal := editorgroup.NewDiffProposal(title, func(accepted bool) {
		if !accepted {
			answer(extension.ProposedResult{})
			return
		}
		text := modified.Text()
		res := extension.ProposedResult{Accepted: true, Text: text, Edited: text != proposed}
		if crlf {
			res.Text = strings.ReplaceAll(text, "\n", "\r\n")
		}
		answer(res)
	})
	input.SetProposal(proposal)
	g := s.proposalGroup()
	s.SetState(func() {
		s.nav.hold++
		g.Model.OpenEditor(input, editorgroup.OpenOptions{Pinned: true, Active: true})
		s.nav.hold--
		s.syncNav()
		if err := s.group.grid.Activate(g); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
	})
	s.refreshKeys()
	return func() {
		if !proposal.Cancel() {
			return
		}
		if holder := s.groupHolding(input); holder != nil {
			s.closeEditors(holder, []editorgroup.EditorInput{input})
		}
	}, nil
}

// proposalGroup is the group a proposed change opens in: the one that shows another open proposal (so that several do not
// each make a group), else the group on the side of the active group, made when there is none, so that the editor that
// asked (a tab of the extension that asked) stays on screen beside the diff; the active group when no side group can be made.
func (s *shellState) proposalGroup() *editorgroup.Group {
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if in, ok := e.(*editorgroup.DiffInput); ok && in.Proposal() != nil && !in.Proposal().Settled() {
				return g
			}
		}
	}
	if side := s.sideGroup(); side != nil {
		return side
	}
	return s.activeGroup()
}

// normalizeLines makes the line endings of a text "\n".
func normalizeLines(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// settleProposal answers the proposed change a diff editor reviews and closes the editor. Accept gives the answer the text
// of the right side as it is now.
func (s *shellState) settleProposal(in *editorgroup.DiffInput, accept bool) {
	p := in.Proposal()
	if p == nil || p.Settled() {
		return
	}
	p.Settle(accept)
	if g := s.groupHolding(in); g != nil {
		s.closeEditors(g, []editorgroup.EditorInput{in})
	}
}

// openProposal is the diff editor of an open proposed change that the group shows as its active editor, or nil.
func openProposal(group *editorgroup.Group) *editorgroup.DiffInput {
	if in, ok := group.Model.ActiveEditor().(*editorgroup.DiffInput); ok && in.Proposal() != nil && !in.Proposal().Settled() {
		return in
	}
	return nil
}

// proposalButtons are Accept and Reject for the title of a group that shows a proposed change; nil for any other group.
func (s *shellState) proposalButtons(group *editorgroup.Group) []widgets.Widget {
	in := openProposal(group)
	if in == nil {
		return nil
	}
	return []widgets.Widget{
		kit.NewButton("Accept", kit.ButtonOpts{Small: true, Icon: fonts.CodiconCheck, Tooltip: "Accept the proposed change", OnPressed: func() { s.settleProposal(in, true) }}),
		kit.NewButton("Reject", kit.ButtonOpts{Small: true, Secondary: true, Icon: fonts.CodiconClose, Tooltip: "Reject the proposed change", OnPressed: func() { s.settleProposal(in, false) }}),
	}
}
