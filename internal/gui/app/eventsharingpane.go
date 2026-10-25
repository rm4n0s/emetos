package app

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The page "Extensions > Event Sharing" and the question of a link in the window (SPEC §10.15 "Event sharing", step 11.3):
// the kind of editor of an EventSharingInput, the command that opens it (emetos.extensions.manageEventSharing), what its
// Remove buttons and Private switches do, and the dialog that asks "Let events from <tab> reach <tab>?" in the receiving
// tab. The links themselves belong to the process (exthost.Host.Sharing).

// eventSharingKind is how the window handles EventSharingInputs.
func eventSharingKind() editorKind {
	return editorKind{
		typeID: editorgroup.EventSharingInputTypeID,
		icon:   fonts.CodiconExtensions,
		restore: func(string) (editorgroup.EditorInput, bool) {
			return editorgroup.NewEventSharingInput(), true
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return &eventSharingPane{s: s, input: input.(*editorgroup.EventSharingInput)}
		},
	}
}

// eventSharingPane is the editor of an EventSharingInput: the page of the links.
type eventSharingPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.EventSharingInput
}

// build implements pane.
func (p *eventSharingPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWorkspace(bc)
	host := win.Process.Extensions
	report := func(err error) {
		if err != nil {
			win.Process.Logger.Warn("cannot write event-sharing.json", "err", err)
			win.Notify.Warn("Emetos", "Cannot save the change: "+describe(err))
		}
	}
	return p.wrap(p.s, group, extensionsui.NewSharingPage(extensionsui.SharingOptions{
		Snapshot:  host.SharingSnapshot,
		Subscribe: host.OnChange,
		OnRemove: func(l exthost.SharingEntry) {
			report(host.RemoveLink(win.ctx, l.From, l.To, l.Session))
		},
		OnPrivate: func(id string, private bool) {
			report(host.SetPrivate(win.ctx, id, private))
		},
	}))
}

// focus implements pane: the page keeps the focus of its group.
func (p *eventSharingPane) focus() {}

// dispose implements pane.
func (p *eventSharingPane) dispose() {}

// openEventSharingPage shows the page of the links.
func (s *shellState) openEventSharingPage() {
	s.openSingletonPage(func(e editorgroup.EditorInput) bool { _, ok := e.(*editorgroup.EventSharingInput); return ok },
		func() editorgroup.EditorInput { return editorgroup.NewEventSharingInput() })
}

// The buttons of the question, in the order of exthost.SharingAnswer's meanings: the first is the default button.
const (
	sharingAllowButton   = "Allow"
	sharingSessionButton = "This Session Only"
	sharingDenyButton    = "Deny"
	sharingDenyIndex     = 2
)

// sharingDialog is the dialog that asks whether the events of one window tab may reach another (SPEC §10.15 "Event
// sharing"): the question, which extension wants which topics, and what each answer does. Escape and the close button
// deny.
func sharingDialog(q exthost.SharingAsk, answer func(exthost.SharingAnswer)) kit.DialogSpec {
	var detail strings.Builder
	detail.WriteString(q.Extension + " in " + q.To + " wants these topics from " + q.From + ":\n")
	for _, t := range q.Topics {
		detail.WriteString("    " + t + "\n")
	}
	detail.WriteString("\nFrom: " + q.FromID + "\nTo: " + q.ToID + "\n\n")
	detail.WriteString("Allow keeps the link in event-sharing.json; This Session Only forgets it when Emetos quits; Deny asks again only when the extension is activated anew. You can change links in Extensions > Event Sharing.")
	return kit.DialogSpec{
		Type:         kit.DialogQuestion,
		Message:      "Let events from " + q.From + " reach " + q.To + "?",
		Detail:       detail.String(),
		Buttons:      []string{sharingAllowButton, sharingSessionButton, sharingDenyButton},
		CancelButton: foundation.Some(sharingDenyIndex),
		OnAnswer: func(r kit.DialogResult) {
			switch {
			case r.Cancelled || r.Button == sharingDenyIndex:
				answer(exthost.SharingDeny)
			case r.Button == 0:
				answer(exthost.SharingAllow)
			default:
				answer(exthost.SharingSession)
			}
		},
	}
}

// askSharing shows the question of a link in this workspace tab, over its own workbench: it waits there, with the other
// dialogs of the tab, until the user looks at the tab.
func (w *Workspace) askSharing(q exthost.SharingAsk, answer func(exthost.SharingAnswer)) {
	w.Dialogs.Show(sharingDialog(q, answer))
}

// sharingID names the workspace in the links between window tabs: its cleaned absolute folder, and nothing for a tab with
// no folder, which no link can name (SPEC §10.15 "As built (step 11.3)").
func (w *Workspace) sharingID() string {
	if w.Folder == "" {
		return ""
	}
	return filepath.Clean(w.Folder)
}
