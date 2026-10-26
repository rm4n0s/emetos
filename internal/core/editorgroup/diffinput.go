package editorgroup

import (
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/workbench/common/editor/diffEditorInput.ts (name, title, description and dirty state
// of a compare editor) and src/vs/workbench/common/editor/sideBySideEditorInput.ts

// DiffInputTypeID is the TypeID of a DiffInput.
const DiffInputTypeID = "emetos.diff"

// DiffSeparator is what a compare editor writes between the names of its two sides, in its name, description and
// title ("a.go ↔ b.go").
const DiffSeparator = " ↔ "

// DiffInput is the input of the diff editor (SPEC §9.11): the original document on the left, read-only, and the
// modified document on the right, which can be edited when it is a file. The input is dirty when the modified
// document is, and saving it saves that document. It does not own the documents (as a TextInput does not): the
// window counts a use of each for as long as the editor is open.
type DiffInput struct {
	BaseInput
	original, modified *document.Document
	opts               TextInputOpts
	dirty              bool
	caps               Capabilities
	stops              []func()
	proposal           *DiffProposal
}

// DiffProposal makes a diff editor the review of a change that someone proposes to a file (SPEC §10.16 "As built (step
// 13.3)"): the modified side is the proposed text, which the user may edit, and the editor ends with an answer, Accept or
// Reject. Closing the editor, in any way, is Reject. The modified document is the proposal's own, never a file: nothing is
// saved, asked about when the editor closes, backed up or restored.
type DiffProposal struct {
	// Title is the name of the tab and its tooltip.
	Title string

	settle  func(accepted bool)
	settled bool
}

// NewDiffProposal makes a proposal with the title. settle is called once, with true for Accept and false for Reject, when
// the proposal is settled with Settle or when the input is disposed.
func NewDiffProposal(title string, settle func(accepted bool)) *DiffProposal {
	return &DiffProposal{Title: title, settle: settle}
}

// Settle answers the proposal, once: the first call calls the function of the proposal and reports true, later ones do
// nothing and report false.
func (p *DiffProposal) Settle(accepted bool) bool {
	if p.settled {
		return false
	}
	p.settled = true
	if p.settle != nil {
		p.settle(accepted)
	}
	return true
}

// Cancel ends the proposal without an answer: the function of the proposal is never called. It reports whether the
// proposal was still open.
func (p *DiffProposal) Cancel() bool {
	open := !p.settled
	p.settled = true
	return open
}

// Settled tells whether the proposal was answered or cancelled.
func (p *DiffProposal) Settled() bool { return p.settled }

// SetProposal makes the input the review of a proposed change. It is called once, before the input is shown. A proposal
// is not saved with the window (Serialize says no), cannot be split, is never dirty and edits no document for saving, so
// that closing it asks nothing.
func (d *DiffInput) SetProposal(p *DiffProposal) {
	d.proposal = p
	d.caps = d.Capabilities()
}

// Proposal is the proposal the input reviews, or nil for an ordinary comparison.
func (d *DiffInput) Proposal() *DiffProposal { return d.proposal }

// NewDiffInput makes the input that compares two documents. It follows the modified document until Dispose.
func NewDiffInput(original, modified *document.Document, opts TextInputOpts) *DiffInput {
	d := &DiffInput{original: original, modified: modified, opts: opts, dirty: modified.Dirty()}
	d.caps = d.Capabilities()
	d.stops = []func(){
		modified.Subscribe(func(document.ChangeEvent) { d.Refresh() }),
		modified.OnSaved(func(document.SavedEvent) { d.Refresh() }),
		// a renamed or moved file (either side) gives the tab another name; a Save As changes what the input can do
		modified.OnURIChange(func(_, _ document.URI) {
			d.Emit(InputLabel)
			if caps := d.Capabilities(); caps != d.caps {
				d.caps = caps
				d.Emit(InputCapabilities)
			}
		}),
		original.OnURIChange(func(_, _ document.URI) { d.Emit(InputLabel) }),
	}
	return d
}

// Original is the document on the left.
func (d *DiffInput) Original() *document.Document { return d.original }

// Modified is the document on the right.
func (d *DiffInput) Modified() *document.Document { return d.modified }

// Refresh sends an event when the dirty mark of the modified document is not the one the input last had.
func (d *DiffInput) Refresh() {
	if now := d.modified.Dirty(); now != d.dirty {
		d.dirty = now
		d.Emit(InputDirty)
	}
}

// EditedDocument implements DocumentEditor: the modified document.
func (d *DiffInput) EditedDocument() *document.Document {
	if d.proposal != nil {
		return nil // the proposed text is not saved anywhere
	}
	return d.modified
}

// TypeID implements EditorInput.
func (d *DiffInput) TypeID() string { return DiffInputTypeID }

// Resource implements EditorInput: a compare editor shows no one resource (VS Code's side by side inputs have
// one only when both sides are the same), so it is not found as the editor of a file.
func (d *DiffInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput: the names of the two sides, "<original> ↔ <modified>".
func (d *DiffInput) Name() string {
	if d.proposal != nil {
		return d.proposal.Title
	}
	return DocumentName(d.original) + DiffSeparator + DocumentName(d.modified)
}

// Description implements EditorInput: the folders of the two sides, or the one they share.
func (d *DiffInput) Description() string {
	if d.proposal != nil {
		return ""
	}
	a, b := docDescription(d.original, d.opts), docDescription(d.modified, d.opts)
	switch {
	case a == b:
		return a
	case a == "" || b == "":
		return a + b
	}
	return a + DiffSeparator + b
}

// Title implements EditorInput: the paths of the two sides.
func (d *DiffInput) Title() string {
	if d.proposal != nil {
		return d.proposal.Title
	}
	return docTitle(d.original, d.opts) + DiffSeparator + docTitle(d.modified, d.opts)
}

// TabKey implements TabKeyed: the two sides, so that two comparisons with the same name are two tabs.
func (d *DiffInput) TabKey() string {
	return d.original.URI().String() + "↔" + d.modified.URI().String()
}

// Dirty implements EditorInput: the modified document has unsaved changes.
func (d *DiffInput) Dirty() bool { return d.dirty && d.proposal == nil }

// Matches implements EditorInput: a comparison of the same two documents.
func (d *DiffInput) Matches(other EditorInput) bool {
	o, ok := other.(*DiffInput)
	if !ok || o == nil {
		return false
	}
	return o == d || o.original.URI() == d.original.URI() && o.modified.URI() == d.modified.URI()
}

// Capabilities implements EditorInput: it can be shown in several groups; it is read-only when the modified
// side is, and untitled while the modified side has no file yet.
func (d *DiffInput) Capabilities() Capabilities {
	c := CapCanSplit
	if d.proposal != nil {
		c = 0
	}
	if d.modified.ReadOnly() {
		c |= CapReadonly
	}
	if d.modified.URI().Scheme == document.SchemeUntitled {
		c |= CapUntitled
	}
	return c
}

// diffValue is what Serialize keeps of an input.
type diffValue struct {
	Original string `json:"original"`
	Modified string `json:"modified"`
}

// Serialize implements EditorInput: the URIs of the two documents as JSON. A side that is neither a file, an
// untitled document nor a virtual one cannot come back.
func (d *DiffInput) Serialize() (string, bool) {
	if d.proposal != nil {
		return "", false
	}
	for _, doc := range []*document.Document{d.original, d.modified} {
		switch doc.URI().Scheme {
		case document.SchemeFile, document.SchemeUntitled, document.SchemeVirtual:
		default:
			return "", false
		}
	}
	data, err := json.Marshal(diffValue{Original: d.original.URI().String(), Modified: d.modified.URI().String()})
	if err != nil {
		return "", false
	}
	return string(data), true
}

// ParseDiffInput reads what Serialize gave: the URIs of the original and the modified document (the window opens
// the documents and makes the input again with NewDiffInput). A value that is not that has the tag
// DiffInputInvalid.
func ParseDiffInput(value string) (original, modified document.URI, err error) {
	var v diffValue
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return document.URI{}, document.URI{}, errors.NewErr("DiffInputInvalid", err).SetMetadata("value", value)
	}
	if v.Original == "" || v.Modified == "" {
		return document.URI{}, document.URI{}, errors.New("DiffInputInvalid", "a compare editor needs two documents", "value", value)
	}
	original, err = document.ParseURI(v.Original)
	if err != nil {
		return document.URI{}, document.URI{}, err
	}
	modified, err = document.ParseURI(v.Modified)
	if err != nil {
		return document.URI{}, document.URI{}, err
	}
	return original, modified, nil
}

// CopyInput implements Copier: another comparison of the same documents.
func (d *DiffInput) CopyInput() EditorInput { return NewDiffInput(d.original, d.modified, d.opts) }

// Dispose implements EditorInput: it stops following the documents.
func (d *DiffInput) Dispose() {
	if d.proposal != nil {
		d.proposal.Settle(false) // closing a proposed change is rejecting it
	}
	for _, stop := range d.stops {
		stop()
	}
	d.stops = nil
	d.BaseInput.Dispose()
}
