package app

import (
	"math"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// logArguments are the arguments of dev.log.
type logArguments struct {
	// Channel is the channel the line goes to.
	Channel string
	// Message is the line.
	Message string
}

// parseLog reads the arguments of dev.log: {"channel": text (the Emetos channel when it is missing),
// "message": text}, or just the message as a string.
func parseLog(args any) (logArguments, error) {
	a := logArguments{Channel: logs.MainChannel}
	var v map[string]any
	switch x := args.(type) {
	case string:
		v = map[string]any{"message": x}
	case map[string]any:
		v = x
	default:
		return a, errors.New("DevLogArgumentsInvalid", "dev.log needs a message or {\"channel\": ..., \"message\": ...}")
	}
	if raw, present := v["channel"]; present {
		channel, ok := raw.(string)
		if !ok || channel == "" {
			return a, errors.New("DevLogChannelInvalid", "the channel of dev.log is a text that is not empty", "channel", raw)
		}
		a.Channel = channel
	}
	message, _ := v["message"].(string)
	if message == "" {
		return a, errors.New("DevLogMessageInvalid", "dev.log needs a message that is a text that is not empty", "message", v["message"])
	}
	a.Message = message
	return a, nil
}

// registerDevLog registers dev.log (SPEC §15.2), which appends a line to a log channel of the Output
// panel. It exists in builds that run dev commands only.
func registerDevLog(r *command.Registry, channels *logs.Store) {
	meta := command.Meta{ID: "dev.log", Description: `Append a line to a channel of the Output panel. Arguments: {"channel", "message"}.`}
	err := RegisterDev(r, meta, func(_ command.Context, args any) error {
		a, err := parseLog(args)
		if err != nil {
			return err
		}
		channels.Append(a.Channel, a.Message)
		return nil
	})
	if err != nil {
		// the id is fixed, new and starts with dev., so a failure is a mistake here
		panic(err)
	}
}

// markersArguments are the arguments of dev.setMarkers.
type markersArguments struct {
	// Owner is who sets the markers.
	Owner string
	// Resource is the absolute path of the file.
	Resource string
	// Markers are the markers; none removes those of the owner.
	Markers []problems.Marker
}

// devMarkerOwner is the owner of the markers dev.setMarkers sets when it names none.
const devMarkerOwner = "dev"

// parseSetMarkers reads the arguments of dev.setMarkers: {"owner": text ("dev" when missing),
// "resource": a path (relative paths are relative to the folder Emetos runs in), "markers": [{"severity":
// "error" | "warning" | "info" | "hint" (error when missing), "message": text, "startLine", "startColumn",
// "endLine", "endColumn": whole numbers from 1 (1, 1, the start line and the start column when
// missing), "source": text, "code": text}]}. A missing list removes the markers of the owner for the
// file. getwd gives the folder Emetos runs in.
func parseSetMarkers(args any, getwd func() (string, error)) (markersArguments, error) {
	a := markersArguments{Owner: devMarkerOwner}
	v, ok := args.(map[string]any)
	if !ok {
		return a, errors.New("DevSetMarkersArgumentsInvalid", "dev.setMarkers needs {\"owner\": ..., \"resource\": ..., \"markers\": [...]}")
	}
	if raw, present := v["owner"]; present {
		owner, ok := raw.(string)
		if !ok || owner == "" {
			return a, errors.New("DevSetMarkersOwnerInvalid", "the owner of dev.setMarkers is a text that is not empty", "owner", raw)
		}
		a.Owner = owner
	}
	resource, _ := v["resource"].(string)
	if resource == "" {
		return a, errors.New("DevSetMarkersResourceInvalid", "dev.setMarkers needs a resource that is a path", "resource", v["resource"])
	}
	if !filepath.IsAbs(resource) {
		cwd, err := getwd()
		if err != nil {
			return a, errors.NewErr("DevSetMarkersCwdFailed", err).SetMetadata("resource", resource)
		}
		resource = filepath.Join(cwd, resource)
	}
	a.Resource = filepath.Clean(resource)
	raw, present := v["markers"]
	if !present || raw == nil {
		return a, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return a, errors.New("DevSetMarkersMarkersInvalid", "the markers of dev.setMarkers are a list", "markers", raw)
	}
	for i, item := range list {
		m, err := parseMarker(item, i)
		if err != nil {
			return a, err
		}
		a.Markers = append(a.Markers, m)
	}
	return a, nil
}

// parseMarker reads one marker of dev.setMarkers.
func parseMarker(item any, index int) (problems.Marker, error) {
	m := problems.Marker{Severity: problems.SeverityError}
	v, ok := item.(map[string]any)
	if !ok {
		return m, errors.New("DevSetMarkersMarkerInvalid", "a marker of dev.setMarkers is an object", "index", index, "marker", item)
	}
	if raw, present := v["severity"]; present {
		name, _ := raw.(string)
		sev, ok := problems.ParseSeverity(name)
		if !ok {
			return m, errors.New("DevSetMarkersSeverityInvalid", "the severity of a marker is error, warning, info or hint", "index", index, "severity", raw)
		}
		m.Severity = sev
	}
	message, _ := v["message"].(string)
	if message == "" {
		return m, errors.New("DevSetMarkersMessageInvalid", "a marker needs a message that is a text that is not empty", "index", index, "message", v["message"])
	}
	m.Message = message
	for _, f := range []struct {
		key string
		dst *int
	}{{"startLine", &m.StartLine}, {"startColumn", &m.StartColumn}, {"endLine", &m.EndLine}, {"endColumn", &m.EndColumn}} {
		raw, present := v[f.key]
		if !present {
			continue
		}
		n, ok := raw.(float64)
		if !ok || n < 1 || n != math.Trunc(n) || n > math.MaxInt32 {
			return m, errors.New("DevSetMarkersPositionInvalid", "the positions of a marker are whole numbers from 1", "index", index, "key", f.key, "value", raw)
		}
		*f.dst = int(n)
	}
	if m.StartLine == 0 {
		m.StartLine = 1
	}
	if m.StartColumn == 0 {
		m.StartColumn = 1
	}
	if m.EndLine == 0 {
		m.EndLine = m.StartLine
	}
	if m.EndColumn == 0 {
		m.EndColumn = m.StartColumn
	}
	for _, f := range []struct {
		key string
		dst *string
	}{{"source", &m.Source}, {"code", &m.Code}} {
		raw, present := v[f.key]
		if !present {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			return m, errors.New("DevSetMarkersTextInvalid", "the source and the code of a marker are texts", "index", index, "key", f.key, "value", raw)
		}
		*f.dst = text
	}
	return m, nil
}

// registerDevMarkers registers dev.setMarkers (SPEC §15.2), which the window implements while it is
// shown: it sets the markers of an owner for a file, so that the Problems panel has something to show before
// any producer exists. Without a window the command fails with the tag DevNoWindow.
func registerDevMarkers(r *command.Registry) {
	meta := command.Meta{ID: "dev.setMarkers", Description: `Set the markers of an owner for a file. Arguments: {"owner", "resource", "markers": [{"severity", "message", "startLine", "startColumn", "endLine", "endColumn", "source", "code"}]}.`}
	err := RegisterDev(r, meta, func(command.Context, any) error {
		return errors.New("DevNoWindow", "no window is shown")
	})
	if err != nil {
		// the id is fixed, new and starts with dev., so a failure is a mistake here
		panic(err)
	}
}

// bindDevMarkers gives dev.setMarkers its handler: the markers go to the service of the window.
func (s *shellState) bindDevMarkers() {
	if !DevCommandsEnabled() {
		return
	}
	win := PeekWorkspace(s.Context())
	remove, err := win.Commands.SetHandler("dev.setMarkers", func(_ command.Context, args any) error {
		a, err := parseSetMarkers(args, os.Getwd)
		if err != nil {
			return err
		}
		if len(a.Markers) == 0 {
			win.Markers.Remove(a.Owner, a.Resource)
			return nil
		}
		win.Markers.Set(a.Owner, a.Resource, a.Markers)
		return nil
	})
	if err != nil {
		win.Process.Logger.Error("cannot bind dev.setMarkers", "err", err)
		return
	}
	s.panels.removeCommands = append(s.panels.removeCommands, remove)
}
