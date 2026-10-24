package keybindingsui

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The texts of the box that defines a keybinding.
const (
	// DefineMessage is what the box says while it waits for the keys.
	DefineMessage = "Press desired key combination and then press ENTER."
	// ChordTo is the text between the key caps of the two chords of a keybinding.
	ChordTo = "chord to"
	// ConflictsTitle is the message of the dialog that asks what to do with the commands that have the keybinding.
	ConflictsTitle = "Keybinding conflicts"
	// KeepBoth is the button of that dialog that adds the keybinding and leaves the others.
	KeepBoth = "Keep Both"
	// Overwrite is the button of that dialog that adds the keybinding and removes the others.
	Overwrite = "Overwrite"
	// Cancel is the button of that dialog that changes nothing.
	Cancel = "Cancel"
)

// The metrics of the box (VS Code's DefineKeybindingWidget).
const (
	defineWidth   = 400.0
	defineMaxList = 4
)

// defineState is the box that records the keys of a keybinding for a row (VS Code's DefineKeybindingWidget): it
// shows what is pressed, and the commands that have the keybinding already.
type defineState struct {
	row       keybinding.Row
	add       bool
	rec       *Recorder
	stop      func()
	conflicts []keybinding.Row
}

func (d *defineState) stopRecording() {
	if d.stop != nil {
		d.stop()
		d.stop = nil
	}
}

// ExistingText is what the box says of the commands that have the keybinding: "1 existing command has this
// keybinding", "3 existing commands have this keybinding".
func ExistingText(n int) string {
	if n == 1 {
		return "1 existing command has this keybinding"
	}
	return strconv.Itoa(n) + " existing commands have this keybinding"
}

// openDefine shows the box that records a keybinding for a row. Enter in it keeps what is recorded, Escape clears
// it and then closes the box. With add, the keybinding is one more for the command of the row.
func (s *editorState) openDefine(id string, add bool) {
	w := s.Widget()
	res, ok := s.resultFor(id)
	if !ok || s.define != nil || w.opts.Record == nil || w.opts.Actions.Define == nil {
		return
	}
	s.closeWhen()
	d := &defineState{row: res.Row, add: add}
	d.rec = &Recorder{}
	d.rec.OnChange = func() { s.definePressed() }
	d.rec.OnEnter = s.confirmDefine
	d.rec.OnEscape = s.clearOrCancelDefine
	d.stop = w.opts.Record(d.rec)
	s.list.SetSelected(id)
	s.list.SetFocus(id)
	s.SetState(func() { s.define = d })
}

// definePressed finds the commands that have the keys recorded.
func (s *editorState) definePressed() {
	d := s.define
	if d == nil {
		return
	}
	d.conflicts = nil
	if kb := d.rec.Keybinding(); len(kb) > 0 && s.model != nil {
		d.conflicts = s.model.FindConflicts(kb, d.row.When, d.row.Command)
	}
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// clearOrCancelDefine is Escape: it clears what was recorded, and closes the box when nothing was.
func (s *editorState) clearOrCancelDefine() {
	d := s.define
	if d == nil {
		return
	}
	if d.rec.Empty() {
		s.closeDefine()
		return
	}
	d.rec.Clear()
}

// closeDefine hides the box and gives the table the focus.
func (s *editorState) closeDefine() {
	d := s.define
	if d == nil {
		return
	}
	d.stopRecording()
	s.SetState(func() { s.define = nil })
	s.listCtl.Focus()
}

// confirmDefine is Enter: the keys that were recorded become the keybinding, after the user decides what to do
// with the commands that have them.
func (s *editorState) confirmDefine() {
	d := s.define
	if d == nil {
		return
	}
	kb := d.rec.Keybinding()
	row, add, conflicts := d.row, d.add, d.conflicts
	s.closeDefine()
	if len(kb) == 0 {
		return
	}
	key := kb.UserSettingsLabel()
	if !add && row.Bound() && row.Key == key {
		return // nothing changes
	}
	w := s.Widget()
	if len(conflicts) == 0 || w.opts.Dialogs == nil {
		s.applyDefine(row, add, key, nil)
		return
	}
	w.opts.Dialogs.Show(kit.DialogSpec{
		Type:         kit.DialogWarning,
		Message:      ConflictsTitle,
		Detail:       conflictsDetail(kb.Label(), conflicts),
		Buttons:      []string{KeepBoth, Overwrite, Cancel},
		CancelButton: foundation.Some(2),
		OnAnswer: func(r kit.DialogResult) {
			if !s.Mounted() {
				return
			}
			switch {
			case r.Cancelled || r.Button == 2:
				s.listCtl.Focus()
			case r.Button == 1:
				s.applyDefine(row, add, key, conflicts)
			default:
				s.applyDefine(row, add, key, nil)
			}
		},
	})
}

// applyDefine makes the keybinding and shows the row.
func (s *editorState) applyDefine(row keybinding.Row, add bool, key string, overwrite []keybinding.Row) {
	s.reselect = &rowRef{command: row.Command, key: key}
	s.fail(s.Widget().opts.Actions.Define(row, add, key, overwrite))
	s.afterEdit()
}

// conflictsDetail lists the commands that have a keybinding, for the dialog.
func conflictsDetail(label string, rows []keybinding.Row) string {
	var b strings.Builder
	b.WriteString(label + " is the keybinding of " + strconv.Itoa(len(rows)) + " other " + plural(len(rows), "command", "commands") + ":\n")
	for i, r := range rows {
		if i == defineMaxList {
			b.WriteString("\n...")
			break
		}
		b.WriteString("\n- " + rowName(r))
	}
	b.WriteString("\n\nOverwrite removes it from them, Keep Both leaves them as they are.")
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// rowName is a command with its when-clause, for a list of conflicts.
func rowName(r keybinding.Row) string {
	name := r.Label
	if name == "" {
		name = r.Command
	}
	if r.When != "" {
		name += " (" + r.When + ")"
	}
	return name
}

// defineOverlay is the box over the table: the message, what was recorded (the text and the key caps), and the
// commands that have it.
func (s *editorState) defineOverlay(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	d := s.define
	text := func(t string, size float64, color ui.Color) widgets.Widget {
		return widgets.NewText(t, widgets.TextOpts{Style: kit.TextStyleOf(bc, size, color), TextAlign: ui.TextAlignCenter})
	}
	input := widgets.NewContainer(
		widgets.NewText(d.rec.Text(), widgets.TextOpts{
			Style:     kit.TextStyleOf(bc, fontSize, colors.InputForeground),
			TextAlign: ui.TextAlignCenter, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
		}),
		widgets.ContainerOpts{
			Key: foundation.NewValueKey("keybindingsui-define-input"), Height: foundation.Some(26.0), Padding: painting.NewEdgeInsetsSymmetric(4, 0),
			Alignment: painting.AlignmentCenter,
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:  foundation.Some(colors.InputBackground),
				Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.FocusBorder), Width: foundation.Some(1.0)}),
			}),
		})
	parts := []widgets.Widget{
		text(DefineMessage, fontSize, colors.EditorWidgetForeground),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(10.0)}),
		input,
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(10.0)}),
		s.defineOutput(bc, colors),
	}
	if n := len(d.conflicts); n > 0 {
		parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(10.0)}),
			s.existing(bc, colors, d))
		for i, c := range d.conflicts {
			if i == defineMaxList {
				parts = append(parts, text("...", 12, colors.EditorWidgetForeground))
				break
			}
			parts = append(parts, widgets.NewText("- "+rowName(c), widgets.TextOpts{
				Style: kit.TextStyleOf(bc, 12, colors.EditorWidgetForeground.WithOpacity(0.8)), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
				TextAlign: ui.TextAlignCenter,
			}))
		}
	}
	box := widgets.NewContainer(
		widgets.NewColumn(parts, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin}),
		widgets.ContainerOpts{
			Key: foundation.NewValueKey("keybindingsui-define"), Width: foundation.Some(defineWidth), Padding: painting.NewEdgeInsetsAll(10),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.EditorWidgetBackground),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(6),
				BoxShadow:    []painting.BoxShadow{{Color: colors.WidgetShadow, BlurRadius: 8}},
			}),
		})
	barrier := widgets.NewGestureDetector(widgets.NewContainer(nil), widgets.GestureDetectorOpts{
		Behavior: rendering.HitTestBehaviorOpaque, OnTap: s.closeDefine,
	})
	return widgets.NewPositionedFill(widgets.NewStack([]widgets.Widget{
		widgets.NewPositionedFill(barrier),
		widgets.NewCenter(box),
	}, widgets.StackOpts{Fit: rendering.StackFitExpand}))
}

// defineOutput is what was pressed, as key caps; the second chord after "chord to".
func (s *editorState) defineOutput(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	chords := s.define.rec.Chords()
	var parts []widgets.Widget
	for i, c := range chords {
		if i > 0 {
			parts = append(parts, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 0),
				widgets.NewText(ChordTo, widgets.TextOpts{Style: kit.TextStyleOf(bc, fontSize, colors.EditorWidgetForeground)})))
		}
		parts = append(parts, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 0),
			kit.NewKeyCaps(bc, chordNames(keybinding.Keybinding{c}), kit.KeyCapsOpts{})))
	}
	if len(parts) == 0 {
		// no keys yet: the room they will take
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(24.0)})
	}
	return widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 2),
		widgets.NewRow(parts, widgets.RowOpts{MainAxisAlignment: rendering.MainAxisAlignmentCenter, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
}

// existing is the line that says how many commands have the keys: a click searches for them.
func (s *editorState) existing(bc widgets.BuildContext, colors kit.Colors, d *defineState) widgets.Widget {
	key := d.rec.Keybinding().Label()
	label := widgets.NewText(ExistingText(len(d.conflicts)), widgets.TextOpts{
		Style:     kit.TextStyleOf(bc, fontSize, colors.EditorWidgetForeground).CopyWith(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationUnderline)}),
		TextAlign: ui.TextAlignCenter,
	})
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(label, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnTap: func() {
				s.closeDefine()
				s.setQuery(`"` + key + `"`)
				s.search.Focus()
			},
		}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
}
