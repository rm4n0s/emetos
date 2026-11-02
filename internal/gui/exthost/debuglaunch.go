package exthost

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sort"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// launch.json and the debuggers' attributes (SPEC §10.31 "Launch configurations"; after debugger.ts and debugSchemas.ts): the file is
// checked against the attribute schemas the debuggers declare, the problems go to the Problems panel with the owner launch.json, and
// the completion of the file suggests the types, the requests and the attributes.

// LaunchProblemsOwner is the owner of the markers of launch.json in the Problems panel.
const LaunchProblemsOwner = "launch.json"

// launchOwner is the owner of the completion provider the host registers.
const launchOwner = "emetos.debug"

// launchPattern selects the launch files the completion is for.
const launchPattern = "**/" + debug.LaunchFileName

// schemaOf turns a declaration into what the checks of launch.json and its completion read. An attribute schema that is not an object
// was refused by CheckManifest; one that is not valid JSON is left out and logged.
func schemaOf(spec extension.DebuggerSpec, log *slog.Logger) debug.DebuggerSchema {
	s := debug.DebuggerSchema{Type: spec.Type, Label: labelOf(spec), Attributes: map[string]map[string]any{}}
	for req, raw := range spec.ConfigurationAttributes {
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			log.Warn("the attributes of a debugger are not a JSON object", "type", spec.Type, "request", req, "err", err)
			continue
		}
		s.Attributes[req] = schema
	}
	for _, sn := range spec.ConfigurationSnippets {
		s.Snippets = append(s.Snippets, debug.Snippet{Label: sn.Label, Description: sn.Description, Body: debug.Config(sn.Body)})
	}
	return s
}

// LaunchSchemas lists the attribute schemas of the debug types of the enabled extensions, by type.
func (h *DebugHost) LaunchSchemas() []debug.DebuggerSchema {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]debug.DebuggerSchema, 0, len(h.types))
	for _, t := range h.types {
		out = append(out, t.schema)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// CheckLaunch lists what is wrong in the text of a launch.json: the syntax and the values of the file (debug.ParseLaunch) and the
// configurations against the attributes of their debuggers (debug.CheckAttributes).
func (h *DebugHost) CheckLaunch(text string) []debug.Problem {
	_, problems := debug.ParseLaunch(text)
	return append(problems, debug.CheckAttributes(text, h.LaunchSchemas())...)
}

// SetLaunchText tells the host the text of a launch.json that is open, and puts its problems into the Problems panel under the owner
// launch.json. The window calls it when the file's document opens, changes or is saved; the host checks the file again when the
// debuggers change (an extension is turned on or off). Without a Problems service in the scope, nothing is shown.
func (h *DebugHost) SetLaunchText(path, text string) {
	path = filepath.Clean(path)
	h.mu.Lock()
	h.launchTexts[path] = text
	h.mu.Unlock()
	h.publishLaunchProblems(path, text)
}

// ForgetLaunch removes the problems of a launch.json whose document closed or whose file was deleted.
func (h *DebugHost) ForgetLaunch(path string) {
	path = filepath.Clean(path)
	h.mu.Lock()
	delete(h.launchTexts, path)
	h.mu.Unlock()
	if sc := h.scopeOf(); sc != nil && sc.opts.Markers != nil {
		sc.opts.Markers.Remove(LaunchProblemsOwner, path)
	}
}

// launchChecksChanged checks the open launch files again.
func (h *DebugHost) launchChecksChanged() {
	h.mu.Lock()
	texts := make(map[string]string, len(h.launchTexts))
	for p, t := range h.launchTexts {
		texts[p] = t
	}
	h.mu.Unlock()
	for p, t := range texts {
		h.publishLaunchProblems(p, t)
	}
}

func (h *DebugHost) publishLaunchProblems(path, text string) {
	sc := h.scopeOf()
	if sc == nil || sc.opts.Markers == nil {
		return
	}
	probs := h.CheckLaunch(text)
	markers := make([]problems.Marker, 0, len(probs))
	for _, p := range probs {
		if p.Offset < 0 {
			continue
		}
		sl, sc2 := p.Line+1, p.Column+1
		el, ec := endOf(text, p.Offset+max(p.Length, 1))
		sev := problems.SeverityWarning
		if p.Severity == debug.SeverityError {
			sev = problems.SeverityError
		}
		markers = append(markers, problems.Marker{
			Severity: sev, Source: "launch.json", Message: p.Message,
			StartLine: sl, StartColumn: sc2, EndLine: el, EndColumn: ec,
		})
	}
	sc.opts.Markers.Set(LaunchProblemsOwner, path, markers)
}

// endOf is the one-based line and column (in bytes) of an offset of a text.
func endOf(text string, off int) (line, col int) {
	if off > len(text) {
		off = len(text)
	}
	line, start := 1, 0
	for i := 0; i < off; i++ {
		if text[i] == '\n' {
			line++
			start = i + 1
		}
	}
	return line, off - start + 1
}

// registerLaunchCompletion adds the completion of launch.json to the scope's language features.
func (h *DebugHost) registerLaunchCompletion(s *Scope) {
	remove := s.features.RegisterCompletion(launchOwner, langfeatures.Selector{Pattern: launchPattern}, launchCompletion{h}, ",", `"`, ":")
	h.mu.Lock()
	prev := h.unsub
	h.unsub = func() {
		remove()
		prev()
	}
	h.mu.Unlock()
}

// launchCompletion is the completion provider of launch.json.
type launchCompletion struct{ h *DebugHost }

// ProvideCompletion implements langfeatures.CompletionProvider.
func (c launchCompletion) ProvideCompletion(_ context.Context, doc langfeatures.Document, pos langfeatures.Position, _ langfeatures.CompletionTrigger) (langfeatures.CompletionList, error) {
	text := doc.Text.String()
	offset := doc.Text.OffsetAt(pos)
	var items []langfeatures.CompletionItem
	for _, it := range debug.CompleteLaunch(text, offset, c.h.LaunchSchemas()) {
		r := langfeatures.Range{Start: doc.Text.PositionAt(it.Start), End: doc.Text.PositionAt(it.End)}
		item := langfeatures.CompletionItem{
			Label: it.Label, Detail: it.Detail, InsertText: it.InsertText, Snippet: it.Snippet, Owner: launchOwner,
			Insert: &r, Replace: &r, Kind: completionKindOf(it.Kind),
		}
		if it.Documentation != "" {
			item.Documentation = &langfeatures.Markup{Value: it.Documentation, Markdown: true}
		}
		items = append(items, item)
	}
	return langfeatures.CompletionList{Items: items}, nil
}

// completionKindOf is the LSP kind of a suggestion: Property (10), Value (12) or Snippet (15).
func completionKindOf(k debug.CompletionKind) langfeatures.CompletionKind {
	switch k {
	case debug.CompleteProperty:
		return 10
	case debug.CompleteSnippet:
		return 15
	}
	return 12
}
