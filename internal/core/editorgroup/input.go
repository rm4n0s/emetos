package editorgroup

import (
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Capabilities are the things an editor input can or cannot do (VS Code's EditorInputCapabilities).
type Capabilities uint

const (
	// CapReadonly marks an input whose content cannot be changed.
	CapReadonly Capabilities = 1 << iota
	// CapUntitled marks an input that has no file yet.
	CapUntitled
	// CapCanSplit marks an input that can be shown in several groups at once (steps 6.9, 6.10); a
	// text editor can.
	CapCanSplit
	// CapCannotClose marks an input that has no close button and that the user cannot close.
	CapCannotClose
)

// Has tells whether every capability of c2 is in c.
func (c Capabilities) Has(c2 Capabilities) bool { return c&c2 == c2 }

// InputEventKind says what changed in an input.
type InputEventKind int

const (
	// InputDirty is a change of the dirty mark.
	InputDirty InputEventKind = iota
	// InputLabel is a change of the name, the description or the title.
	InputLabel
	// InputCapabilities is a change of the capabilities.
	InputCapabilities
	// InputWillDispose is sent once, just before the input is disposed.
	InputWillDispose
)

// InputEvent tells that something about an input changed.
type InputEvent struct {
	Kind InputEventKind
}

// EditorInput is what an editor tab stands for: a thing that can be opened in an editor group
// (SPEC §10.3). The model compares inputs with Matches, so two inputs for the same file are the
// same editor. An input is used on the UI thread only, and must be a pointer (the model also
// compares inputs with ==).
//
// Saving and reverting are not part of the interface: the window saves the documents of its text inputs
// (SPEC §6.3) and the inputs of other kinds, which have nothing to save yet, get it when they exist.
type EditorInput interface {
	// TypeID names the kind of the input, such as "emetos.textEditor"; it chooses the deserializer
	// of a restored input.
	TypeID() string
	// Resource is what the input shows, such as a file; false when it shows no resource.
	Resource() (document.URI, bool)
	// Name is the label of the tab: the name of the file.
	Name() string
	// Description is the dimmer text after the name: the folder of the file. A tab shows it only
	// while another tab has the same name (see TabLabels).
	Description() string
	// Title is the text of the tooltip of the tab: the full path.
	Title() string
	// Dirty tells whether the input has changes that are not saved.
	Dirty() bool
	// Matches tells whether other is the same editor as the input.
	Matches(other EditorInput) bool
	// Capabilities says what the input can do.
	Capabilities() Capabilities
	// Serialize gives the text that Deserialize-ing code (the factory of RestoreModel) makes the
	// input again from; false for an input that cannot be restored (step 7.5).
	Serialize() (string, bool)
	// Subscribe calls fn on the changes of the input; it gives the function that stops the calls.
	Subscribe(fn func(InputEvent)) (unsubscribe func())
	// Dispose releases what the input holds. It sends InputWillDispose first.
	Dispose()
}

// Copier is implemented by an input that can be shown in several groups at once: CopyInput gives
// another input for the same thing, which the other group owns (VS Code's EditorInput.copy). An
// input without it is shown in one group only, as a singleton editor is, and a split or a copy
// leaves it out.
type Copier interface {
	CopyInput() EditorInput
}

// BaseInput is the part of an EditorInput that sends the events. An input embeds it and calls
// Emit when its state changes; Dispose sends InputWillDispose and forgets the listeners.
type BaseInput struct {
	listeners []*inputListener
	disposed  bool
}

type inputListener struct{ fn func(InputEvent) }

// Subscribe implements EditorInput.
func (b *BaseInput) Subscribe(fn func(InputEvent)) (unsubscribe func()) {
	l := &inputListener{fn: fn}
	b.listeners = append(b.listeners, l)
	return func() {
		for i, x := range b.listeners {
			if x == l {
				b.listeners = append(b.listeners[:i:i], b.listeners[i+1:]...)
				return
			}
		}
	}
}

// Emit sends an event to the listeners, unless the input is disposed.
func (b *BaseInput) Emit(kind InputEventKind) {
	if b.disposed {
		return
	}
	for _, l := range append([]*inputListener(nil), b.listeners...) {
		l.fn(InputEvent{Kind: kind})
	}
}

// Dispose sends InputWillDispose, once, and removes the listeners. An input that embeds
// BaseInput calls it from its own Dispose.
func (b *BaseInput) Dispose() {
	if b.disposed {
		return
	}
	b.Emit(InputWillDispose)
	b.disposed = true
	b.listeners = nil
}

// Disposed tells whether Dispose was called.
func (b *BaseInput) Disposed() bool { return b.disposed }
