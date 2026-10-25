package diffeditor

import (
	"reflect"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The inline view (SPEC §9.11, step 9.6): one editor shows the modified text, and the lines that were deleted or
// replaced appear above the lines that took their place, in view zones.
//
// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/diffEditorOptions.ts (renderSideBySide, the width
// based layout and the context keys) and components/diffEditorViewZones/diffEditorViewZones.ts (the zones of
// the deleted code and their margin)

// Mode is how the diff editor lays out the two texts.
type Mode int

const (
	// SideBySide shows the original and the modified editor next to each other.
	SideBySide Mode = iota
	// Inline shows the modified editor alone, with the deleted lines in view zones.
	Inline
)

// String is "sideBySide" or "inline".
func (m Mode) String() string {
	if m == Inline {
		return "inline"
	}
	return "sideBySide"
}

// DefaultInlineBreakpoint is the width, in pixels, at or under which the inline view is used when the space is
// limited (VS Code's `diffEditor.renderSideBySideInlineBreakpoint`).
const DefaultInlineBreakpoint = 900.0

// inlineZone is a zone of deleted lines and the change it shows.
type inlineZone struct {
	id     editor.ZoneID
	change int
}

// inlineSig is what the zones of the inline view were made for: when it is the same, they are kept.
type inlineSig struct {
	changes     []diff.LineRangeMapping
	origVersion int
	colors      Colors
}

// settingBool reads a boolean setting of the diff editor, with the value VS Code has by default when the model has
// no settings.
func (m *Model) settingBool(key string, def bool) bool {
	if m.opts.Settings == nil {
		return def
	}
	return settings.Get[bool](m.opts.Settings, key, m.vms[Modified].Document().LanguageID())
}

// RenderSideBySide is the setting diffEditor.renderSideBySide (default true).
func (m *Model) RenderSideBySide() bool { return m.settingBool("diffEditor.renderSideBySide", true) }

// UseInlineViewWhenSpaceIsLimited is the setting diffEditor.useInlineViewWhenSpaceIsLimited (default true).
func (m *Model) UseInlineViewWhenSpaceIsLimited() bool {
	return m.settingBool("diffEditor.useInlineViewWhenSpaceIsLimited", true)
}

// InlineBreakpoint is the setting diffEditor.renderSideBySideInlineBreakpoint in pixels (default 900).
func (m *Model) InlineBreakpoint() float64 {
	if m.opts.Settings == nil {
		return DefaultInlineBreakpoint
	}
	return settings.Get[float64](m.opts.Settings, "diffEditor.renderSideBySideInlineBreakpoint", m.vms[Modified].Document().LanguageID())
}

// BreakpointReached tells whether the side by side view is asked for but the widget is no wider than the
// breakpoint: the context key diffEditorRenderSideBySideInlineBreakpointReached (VS Code's
// `couldShowInlineViewBecauseOfSize`). It is false until the widget has been laid out.
func (m *Model) BreakpointReached() bool {
	return m.width > 0 && m.RenderSideBySide() && m.width <= m.InlineBreakpoint()
}

// WantedMode is the mode the settings and the width ask for: inline when the side by side view is turned off, or
// when the space is limited and the setting that uses the inline view then is on (VS Code's `renderSideBySide`).
func (m *Model) WantedMode() Mode {
	if !m.RenderSideBySide() || (m.UseInlineViewWhenSpaceIsLimited() && m.BreakpointReached()) {
		return Inline
	}
	return SideBySide
}

// Mode is the mode the editors are laid out for now. It follows WantedMode a frame after a setting or the width
// changed: the change is made off the build.
func (m *Model) Mode() Mode { return m.mode }

// Width is the width of the widget as it was last laid out (0 before it was).
func (m *Model) Width() float64 { return m.width }

// SetWidth tells the model how wide the widget is, which may change the mode. It is called while the widget is
// built; the mode changes a little later, on the UI thread, and the listeners are told.
func (m *Model) SetWidth(w float64) {
	if w == m.width {
		return
	}
	m.width = w
	m.queueMode()
}

// queueMode brings the mode to WantedMode after the build that is going on.
func (m *Model) queueMode() {
	if m.modeQueued {
		return
	}
	m.modeQueued = true
	m.opts.Post(func() {
		m.modeQueued = false
		if m.disposed {
			return
		}
		m.SetMode(m.WantedMode())
	})
}

// SetMode lays the editors out for a mode: the zones and the marks of the other mode are taken off, and the
// ones of this mode put on. The modified editor is the same in both modes, so its cursors and its scroll
// position stay. Listeners are told even when the mode is the same, since the context keys may have changed.
func (m *Model) SetMode(mode Mode) {
	if m.disposed {
		return
	}
	changed := mode != m.mode
	m.mode = mode
	if changed && m.computed {
		m.applyMarks()
		if err := m.align(); err != nil && m.opts.OnError != nil {
			m.opts.OnError(err)
		}
		if mode == SideBySide {
			m.follow(Modified) // the original editor was not scrolled in the inline view
		}
	}
	m.NotifyListeners()
}

// changeMarks are the background marks of one change for a side.
func changeMarks(ch diff.LineRangeMapping, s Side, c Colors, snap buffer.Snapshot) []editor.BackgroundMark {
	return Marks([]diff.LineRangeMapping{ch}, s, c, snap)
}

// setInlineZones puts the zones of the deleted lines in the modified editor: one for each change that has
// original lines, above the line that took their place. Zones that are as they should be are left alone.
func (m *Model) setInlineZones() error {
	vm := m.vms[Modified]
	sig := inlineSig{changes: m.changes, origVersion: m.vms[Original].Document().Version(), colors: m.colors}
	type want struct {
		change      int
		after, rows int
	}
	var wants []want
	lines := vm.Document().Snapshot().LineCount()
	for i, c := range m.changes {
		if !c.Original.IsEmpty() && c.Modified.StartLine >= 1 {
			// a diff that is a little behind the text (an edit is waiting to be diffed) may name a line that is gone
			wants = append(wants, want{change: i, after: min(c.Modified.StartLine-1, lines), rows: c.Original.Len()})
		}
	}
	same := len(m.inline) == len(wants) && reflect.DeepEqual(sig, m.inlineSig)
	for i := 0; same && i < len(wants); i++ {
		spec, ok := vm.Zone(m.inline[i].id)
		same = ok && spec.AfterLine == wants[i].after && spec.Rows == wants[i].rows && m.inline[i].change == wants[i].change
	}
	if same {
		return nil
	}
	orig := m.vms[Original]
	snap := orig.Document().Snapshot()
	var made []inlineZone
	err := vm.ChangeZones(func(c editor.ZoneChanges) error {
		for _, z := range m.inline {
			if err := c.Remove(z.id); err != nil {
				return err
			}
		}
		m.inline = nil
		for _, w := range wants {
			ch := m.changes[w.change]
			id, err := c.Add(editor.ZoneSpec{
				AfterLine: w.after, Rows: w.rows, ShowInHiddenAreas: true,
				Gutter: m.colors.RemovedGutter, GutterGlyph: fonts.CodiconRemove,
				Widget: editor.NewLinesView(editor.LinesViewOpts{
					Source: orig, FirstLine: ch.Original.StartLine - 1, Count: w.rows, ScrollWith: vm,
					Marks: changeMarks(ch, Original, m.colors, snap),
				}),
			})
			if err != nil {
				return err
			}
			made = append(made, inlineZone{id: id, change: w.change})
		}
		return nil
	})
	m.inline, m.inlineSig = made, sig
	return err
}

// clearInlineZones takes the zones of the inline view off the modified editor.
func (m *Model) clearInlineZones() error {
	if len(m.inline) == 0 {
		return nil
	}
	zones := m.inline
	m.inline, m.inlineSig = nil, inlineSig{}
	return m.vms[Modified].ChangeZones(func(c editor.ZoneChanges) error {
		for _, z := range zones {
			if err := c.Remove(z.id); err != nil {
				return err
			}
		}
		return nil
	})
}

// InlineZones are the zones of the deleted lines the modified editor has now, in the order of the changes: the
// change each is of (an index in Changes), where it is and how many rows it has.
func (m *Model) InlineZones() []InlineZone {
	var out []InlineZone
	for _, z := range m.inline {
		if spec, ok := m.vms[Modified].Zone(z.id); ok {
			out = append(out, InlineZone{Change: z.change, AfterLine: spec.AfterLine, Rows: spec.Rows})
		}
	}
	return out
}

// InlineZone is a zone of deleted lines of the inline view.
type InlineZone struct {
	// Change is the index of the change in Changes.
	Change int
	// AfterLine is the zone's place in the modified text: the number of lines above it.
	AfterLine int
	// Rows is how many rows it takes: the deleted lines.
	Rows int
}
