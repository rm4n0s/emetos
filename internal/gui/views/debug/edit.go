package debug

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The inline input of the Variables, Watch and Breakpoints views: the box in which a watch expression, the new value of a variable
// or the name of a function breakpoint is typed. Enter commits, Escape cancels, and leaving the box commits what was typed, as VS
// Code's views do (InputBox with onFinish, success on Enter and on blur).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/baseDebugView.ts (renderInputBox) and
// src/vs/workbench/contrib/debug/browser/breakpointsView.ts (FunctionBreakpointInputRenderer)

// InputKey is the key of the inline input.
var InputKey = foundation.NewValueKey("debug:inline-input")

// editState is the input that is open.
type editState struct {
	key  string
	ctl  *kit.InputBoxController
	done func(text string, ok bool)
}

// Editing tells whether an inline input is open in the view; the tree leaves its keys to the input then.
func (b *base) Editing() bool { return b.edit != nil }

// EditingKey is the key of the row whose input is open, or "".
func (b *base) EditingKey() string {
	if b.edit == nil {
		return ""
	}
	return b.edit.key
}

// StartEdit opens the inline input on the row of a key with a text. done is called once, with the text and true on Enter or when
// the input loses the focus, and with false on Escape. Another open input is closed as if cancelled.
func (b *base) StartEdit(key, text string, done func(text string, ok bool)) {
	if b.edit != nil {
		b.finishEdit(false)
	}
	e := &editState{key: key, ctl: kit.NewInputBoxController(text), done: done}
	e.ctl.SelectAll()
	e.ctl.FocusNode().SetOnKeyEvent(func(_ *widgets.FocusNode, ev services.KeyEvent) widgets.KeyEventResult {
		if _, ok := ev.(*services.KeyDownEvent); ok && ev.LogicalKey() == services.LogicalKeyboardKeyEscape {
			b.finishEdit(false)
			return widgets.KeyEventResultHandled
		}
		return widgets.KeyEventResultIgnored
	})
	b.edit = e
	b.model.SetFocus(key)
	b.notify()
	b.opts.Post(func() {
		if b.edit == e {
			e.ctl.Focus()
		}
	})
}

// CancelEdit closes the input without committing it.
func (b *base) CancelEdit() {
	if b.edit != nil {
		b.finishEdit(false)
	}
}

// commitEdit closes the input and passes the text on.
func (b *base) commitEdit() {
	if b.edit != nil {
		b.finishEdit(true)
	}
}

func (b *base) finishEdit(ok bool) {
	e := b.edit
	if e == nil {
		return
	}
	b.edit = nil
	text := e.ctl.Value()
	b.notify()
	b.ctl.Focus()
	e.done(text, ok)
}

// editBox is the input of the open edit as a widget for a row.
func (b *base) editBox() widgets.Widget {
	e := b.edit
	return kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Key: InputKey, Controller: e.ctl,
		OnSubmit: func(string) {
			if b.edit == e {
				b.commitEdit()
			}
		},
		OnFocusChange: func(focused bool) {
			if focused {
				return
			}
			// leaving the box commits it, once the focus has settled
			b.opts.Post(func() {
				if b.edit == e && !e.ctl.HasFocus() {
					b.commitEdit()
				}
			})
		},
	})
}

// setValue opens the input on a variable or an expression with its value; the text goes to the adapter in the frame (Set Value). The
// request runs on a goroutine of its own, and a failure is the value of the node (an expression) or a log line.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/variablesView.ts (VariablesView, the setValue action) and
// watchExpressionsView.ts (setExpression)
func (b *base) setValue(r Row, frame *debug.StackFrame) {
	switch {
	case r.Expression != nil:
		b.StartEdit(r.Key, r.Expression.Value(), func(text string, ok bool) {
			if !ok {
				return
			}
			go func() {
				_ = r.Expression.SetValue(b.ctx, frame, text) // a failure is the value of the expression
				b.opts.Post(b.notify)
			}()
		})
	case r.Variable != nil:
		b.StartEdit(r.Key, r.Variable.Value(), func(text string, ok bool) {
			if !ok {
				return
			}
			go func() {
				if err := r.Variable.SetValue(b.ctx, frame, text); err != nil {
					b.opts.Logger.Warn("the value cannot be set", "variable", r.Variable.Name, "err", err)
				}
				b.opts.Post(b.notify)
			}()
		})
	}
}
