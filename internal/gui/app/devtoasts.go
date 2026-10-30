package app

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// DevToast is one notification whose toast shows, as dev.toasts reports it.
type DevToast struct {
	// ID is the id of the notification, the argument of dev.dismissNotification.
	ID uint64 `json:"id"`
	// Severity is "info", "warning" or "error".
	Severity string `json:"severity"`
	// Source says who sent it ("" for none), Message is the text.
	Source  string `json:"source"`
	Message string `json:"message"`
}

// devToasts answers dev.toasts: the toasts that show in the workspace of the active tab, the newest first.
func devToasts(p *Process, ctx command.Context) error {
	w := p.Workspaces.Active()
	if w == nil {
		return errors.New("DevToastsNoWorkspace", "no workspace tab is active")
	}
	list := []DevToast{}
	for _, item := range w.Notifications.Toasts() {
		list = append(list, DevToast{ID: item.ID, Severity: item.Severity.String(), Source: item.Source, Message: item.Message})
	}
	setDevAnswer(ctx.Ctx, list)
	return nil
}

// parseDismissNotification reads the arguments of dev.dismissNotification: {"id": n}, or just the id as a number. The tag is
// DevDismissNotificationInvalid.
func parseDismissNotification(args any) (uint64, error) {
	raw := args
	if v, ok := args.(map[string]any); ok {
		raw = v["id"]
	}
	id, ok := raw.(float64)
	if !ok || id < 1 || id != float64(uint64(id)) {
		return 0, errors.New("DevDismissNotificationInvalid", `dev.dismissNotification needs the id of a notification (a whole number from 1) or {"id": ...}`, "arguments", args)
	}
	return uint64(id), nil
}

// devDismissNotification runs dev.dismissNotification: it removes the notification, and so its toast, from the service of the
// workspace of the active tab.
func devDismissNotification(p *Process, args any) error {
	id, err := parseDismissNotification(args)
	if err != nil {
		return err
	}
	w := p.Workspaces.Active()
	if w == nil {
		return errors.New("DevDismissNotificationNoWorkspace", "no workspace tab is active")
	}
	if !w.Notify.Dismiss(id) {
		return errors.New("DevDismissNotificationMissing", "no notification has this id", "id", id)
	}
	return nil
}

// registerDevToasts registers dev.toasts and dev.dismissNotification (SPEC §15.2). They act on the workspace of the active tab,
// which owns the notifications, and exist in builds that run dev commands only.
func registerDevToasts(p *Process) {
	toasts := command.Meta{ID: "dev.toasts", Description: "Report the notifications whose toasts show: id, severity, source and message. Read-only; the dev server answers with the list."}
	dismiss := command.Meta{ID: "dev.dismissNotification", Description: `Remove a notification by its id. Arguments: the id, or {"id"}.`}
	for _, c := range []struct {
		meta command.Meta
		run  command.Handler
	}{
		{toasts, func(ctx command.Context, _ any) error { return devToasts(p, ctx) }},
		{dismiss, func(_ command.Context, args any) error { return devDismissNotification(p, args) }},
	} {
		if err := RegisterDev(p.Commands, c.meta, c.run); err != nil {
			// the ids are fixed, new and start with dev., so a failure is a mistake here
			panic(err)
		}
	}
}
