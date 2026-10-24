package app

import (
	"log/slog"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/notify"
)

// notifyArguments are the arguments of dev.notify.
type notifyArguments struct {
	// Severity is the severity of the notification.
	Severity notify.Severity
	// Message is the text.
	Message string
	// Source says who sent it; may be empty.
	Source string
	// Actions are the labels of the buttons.
	Actions []string
	// Progress is the progress the notification shows, or nil for none.
	Progress *notify.Progress
}

// parseNotify reads the arguments of dev.notify: {"severity": "info" | "warning" | "error" (info
// when it is missing), "message": text, "source": text, "actions": [labels], "progress": a number
// from 0 to 100 or "infinite"}, or just the message as a string.
func parseNotify(args any) (notifyArguments, error) {
	var a notifyArguments
	var v map[string]any
	switch x := args.(type) {
	case string:
		v = map[string]any{"message": x}
	case map[string]any:
		v = x
	default:
		return a, errors.New("DevNotifyArgumentsInvalid", "dev.notify needs a message or {\"severity\": ..., \"message\": ...}")
	}
	if raw, present := v["severity"]; present {
		name, _ := raw.(string)
		switch name {
		case "info":
			a.Severity = notify.Info
		case "warning":
			a.Severity = notify.Warning
		case "error":
			a.Severity = notify.Error
		default:
			return a, errors.New("DevNotifySeverityInvalid", "the severity of dev.notify is info, warning or error", "severity", raw)
		}
	}
	message, _ := v["message"].(string)
	if message == "" {
		return a, errors.New("DevNotifyMessageInvalid", "dev.notify needs a message that is a text that is not empty", "message", v["message"])
	}
	a.Message = message
	if raw, present := v["source"]; present {
		source, ok := raw.(string)
		if !ok {
			return a, errors.New("DevNotifySourceInvalid", "the source of dev.notify is a text", "source", raw)
		}
		a.Source = source
	}
	if raw, present := v["actions"]; present {
		list, ok := raw.([]any)
		if !ok {
			return a, errors.New("DevNotifyActionsInvalid", "the actions of dev.notify are a list of labels", "actions", raw)
		}
		for _, item := range list {
			label, ok := item.(string)
			if !ok || label == "" {
				return a, errors.New("DevNotifyActionsInvalid", "an action of dev.notify is a label that is not empty", "action", item)
			}
			a.Actions = append(a.Actions, label)
		}
	}
	if raw, present := v["progress"]; present {
		switch x := raw.(type) {
		case string:
			if x != "infinite" {
				return a, errors.New("DevNotifyProgressInvalid", "the progress of dev.notify is a number from 0 to 100 or \"infinite\"", "progress", raw)
			}
			a.Progress = &notify.Progress{Infinite: true}
		case float64:
			if x < 0 || x > 100 {
				return a, errors.New("DevNotifyProgressInvalid", "the progress of dev.notify is a number from 0 to 100 or \"infinite\"", "progress", raw)
			}
			a.Progress = &notify.Progress{Total: 100, Worked: x}
		default:
			return a, errors.New("DevNotifyProgressInvalid", "the progress of dev.notify is a number from 0 to 100 or \"infinite\"", "progress", raw)
		}
	}
	return a, nil
}

// registerDevNotify registers dev.notify (SPEC §15.2), which sends a notification through the service: its
// buttons close it and write which one was pressed to the log. It exists in builds that run dev commands only.
func registerDevNotify(r *command.Registry, svc *notify.Service, log *slog.Logger) {
	meta := command.Meta{ID: "dev.notify", Description: `Send a notification. Arguments: {"severity", "message", "source", "actions", "progress"}.`}
	err := RegisterDev(r, meta, func(_ command.Context, args any) error {
		a, err := parseNotify(args)
		if err != nil {
			return err
		}
		n := notify.Notification{Severity: a.Severity, Message: a.Message, Source: a.Source, Progress: a.Progress}
		for _, label := range a.Actions {
			n.Actions = append(n.Actions, notify.Action{Label: label, Run: func() { log.Info("dev.notify action", "action", label) }})
		}
		svc.Notify(n)
		return nil
	})
	if err != nil {
		// the id is fixed, new and starts with dev., so a failure is a mistake here
		panic(err)
	}
}
