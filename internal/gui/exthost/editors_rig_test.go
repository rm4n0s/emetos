package exthost

import (
	"strings"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// fakeEditors is the editor area of a test tab: it records what the extensions asked and keeps the tabs.
type fakeEditors struct {
	opened   []*editorgroup.ExtensionTabInput
	groups   map[*editorgroup.ExtensionTabInput]int
	closed   []*editorgroup.ExtensionTabInput
	revealed []*editorgroup.ExtensionTabInput
	fileOpen []fileOpen
	failOpen error
	// indexes are the places OpenTabAt was asked for, drags the drags begun and failDrag makes BeginDrag fail.
	indexes  []int
	drags    []*fakeDrag
	failDrag error
	// proposals are the proposed changes the extensions asked to show.
	proposals   []*fakeProposal
	failPropose error
}

// fakeProposal is a diff editor of a proposed change: the test answers it as the user does.
type fakeProposal struct {
	change  extension.ProposedChange
	answer  func(extension.ProposedResult)
	closed  int
	settled bool
}

// reply answers once, like the title buttons of the diff editor; a settled proposal ignores it.
func (p *fakeProposal) reply(r extension.ProposedResult) {
	if p.settled {
		return
	}
	p.settled = true
	p.answer(r)
}

func (f *fakeEditors) OpenProposed(change extension.ProposedChange, answer func(extension.ProposedResult)) (func(), error) {
	if f.failPropose != nil {
		return nil, f.failPropose
	}
	p := &fakeProposal{change: change, answer: answer}
	f.proposals = append(f.proposals, p)
	return func() {
		if !p.settled {
			p.closed++
			p.settled = true
		}
	}, nil
}

// fileOpen is a call of OpenFileView.
type fileOpen struct {
	uri, view string
	side      bool
	group     int
}

func (f *fakeEditors) OpenTab(in *editorgroup.ExtensionTabInput, group int, _ bool) error {
	if f.failOpen != nil {
		return f.failOpen
	}
	if f.groups == nil {
		f.groups = map[*editorgroup.ExtensionTabInput]int{}
	}
	if group == 0 {
		group = 1
	}
	f.opened = append(f.opened, in)
	f.groups[in] = group
	return nil
}

func (f *fakeEditors) OpenTabAt(in *editorgroup.ExtensionTabInput, group, index int) error {
	f.indexes = append(f.indexes, index)
	return f.OpenTab(in, group, false)
}

// fakeDrag is a drag the window shows: it records the pointer, and a drop opens a tab in the group and at the index the test
// chose (nowhere when dropNone is set).
type fakeDrag struct {
	label                string
	moves                []ui.Offset
	dropGroup, dropIndex int
	dropNone             bool
	ended                bool
}

func (d *fakeDrag) Move(at ui.Offset) { d.moves = append(d.moves, at) }

func (d *fakeDrag) End(_ ui.Offset, drop bool, open func(group, index int) error) (bool, error) {
	d.ended = true
	if !drop || d.dropNone {
		return false, nil
	}
	if err := open(d.dropGroup, d.dropIndex); err != nil {
		return false, err
	}
	return true, nil
}

func (f *fakeEditors) BeginDrag(label string) (DragSession, error) {
	if f.failDrag != nil {
		return nil, f.failDrag
	}
	d := &fakeDrag{label: label, dropGroup: 2, dropIndex: 1}
	f.drags = append(f.drags, d)
	return d, nil
}

func (f *fakeEditors) CloseTab(in *editorgroup.ExtensionTabInput) { f.closed = append(f.closed, in) }

func (f *fakeEditors) RevealTab(in *editorgroup.ExtensionTabInput, _ bool) {
	f.revealed = append(f.revealed, in)
}

func (f *fakeEditors) GroupOf(in *editorgroup.ExtensionTabInput) int { return f.groups[in] }

func (f *fakeEditors) OpenFileView(uri, view string, side bool, group int) error {
	f.fileOpen = append(f.fileOpen, fileOpen{uri, view, side, group})
	return nil
}

// fakeLanguages is the grammars of a test tab: a language is known when it is "go", and every line is one token.
type fakeLanguages struct{}

func (fakeLanguages) Resolve(name string) (string, bool) { return name, name == "go" }

func (fakeLanguages) Tokenize(id, text string) ([][]extension.Token, error) {
	var out [][]extension.Token
	for _, line := range strings.Split(text, "\n") {
		out = append(out, []extension.Token{{Start: 0, End: len(line), Scopes: []string{"source." + id}}})
	}
	return out, nil
}
