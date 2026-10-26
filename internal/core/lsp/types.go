package lsp

import (
	"bytes"
	"encoding/json"

	"github.com/rm4n0s/errors"
)

// The types below are a hand-written subset of LSP 3.17: what the features of area 3 send and receive. A
// member the protocol allows to be one of several shapes has a type that reads them all (and writes back
// the one form that is always valid), so that the features see one shape. Members of the protocol that
// Emetos does not use are left out; a value the server must get back unchanged (Data) is kept as raw JSON.

// trimmed is raw without the white space around it.
func trimmed(raw []byte) []byte { return bytes.TrimSpace(raw) }

func isNull(raw []byte) bool   { return len(raw) == 0 || string(trimmed(raw)) == "null" }
func isString(raw []byte) bool { r := trimmed(raw); return len(r) > 0 && r[0] == '"' }
func isArray(raw []byte) bool  { r := trimmed(raw); return len(r) > 0 && r[0] == '[' }
func isBool(raw []byte) bool {
	r := string(trimmed(raw))
	return r == "true" || r == "false"
}

// Location is a place in a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink is a place with the span of the symbol that led to it.
type LocationLink struct {
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`
	TargetURI            string `json:"targetUri"`
	TargetRange          Range  `json:"targetRange"`
	TargetSelectionRange Range  `json:"targetSelectionRange"`
}

// LocationLinks is the result of the requests for definitions, declarations, type definitions and
// implementations: a Location, Locations or LocationLinks, all read as links (a Location is a link whose
// target and selection are its range).
type LocationLinks []LocationLink

// UnmarshalJSON reads null, one Location, an array of Locations and an array of LocationLinks.
func (l *LocationLinks) UnmarshalJSON(data []byte) error {
	*l = nil
	if isNull(data) {
		return nil
	}
	if !isArray(data) {
		var loc Location
		if err := json.Unmarshal(data, &loc); err != nil {
			return errors.NewErr("LocationInvalid", err)
		}
		*l = LocationLinks{{TargetURI: loc.URI, TargetRange: loc.Range, TargetSelectionRange: loc.Range}}
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return errors.NewErr("LocationInvalid", err)
	}
	out := make(LocationLinks, 0, len(items))
	for _, item := range items {
		var probe struct {
			TargetURI *string `json:"targetUri"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return errors.NewErr("LocationInvalid", err)
		}
		if probe.TargetURI != nil {
			var link LocationLink
			if err := json.Unmarshal(item, &link); err != nil {
				return errors.NewErr("LocationInvalid", err)
			}
			out = append(out, link)
			continue
		}
		var loc Location
		if err := json.Unmarshal(item, &loc); err != nil {
			return errors.NewErr("LocationInvalid", err)
		}
		out = append(out, LocationLink{TargetURI: loc.URI, TargetRange: loc.Range, TargetSelectionRange: loc.Range})
	}
	*l = out
	return nil
}

// TextEdit replaces a range with text. AnnotationID is set on the edits of a change annotation
// (AnnotatedTextEdit).
type TextEdit struct {
	Range        Range  `json:"range"`
	NewText      string `json:"newText"`
	AnnotationID string `json:"annotationId,omitempty"`
}

// TextDocumentIdentifier names a document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier names a document at a version the client sent.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// OptionalVersionedTextDocumentIdentifier names a document; the version is null (nil) when the edit
// does not depend on one.
type OptionalVersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int   `json:"version"`
}

// TextDocumentItem is a document as didOpen sends it.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentPositionParams is a position in a document, the params of most requests.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// TextDocumentContentChangeEvent is one change of didChange: a replacement of Range, or, with no Range,
// the whole new text.
type TextDocumentContentChangeEvent struct {
	Range       *Range `json:"range,omitempty"`
	RangeLength *int   `json:"rangeLength,omitempty"`
	Text        string `json:"text"`
}

// The kinds of MarkupContent.
const (
	MarkupPlainText = "plaintext"
	MarkupMarkdown  = "markdown"
)

// MarkupContent is text with the format it is in. A plain string, which the protocol allows where
// documentation or a tooltip is, is read as plain text.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// UnmarshalJSON reads a MarkupContent object or a string.
func (m *MarkupContent) UnmarshalJSON(data []byte) error {
	if isString(data) {
		m.Kind = MarkupPlainText
		if err := json.Unmarshal(data, &m.Value); err != nil {
			return errors.NewErr("MarkupInvalid", err)
		}
		return nil
	}
	type plain MarkupContent
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return errors.NewErr("MarkupInvalid", err)
	}
	*m = MarkupContent(p)
	return nil
}

// Command is a command the client runs through the server (workspace/executeCommand).
type Command struct {
	Title     string            `json:"title"`
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

// Diagnostic is a problem the server found.
type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           int                            `json:"severity,omitempty"`
	Code               json.RawMessage                `json:"code,omitempty"`
	CodeDescription    *CodeDescription               `json:"codeDescription,omitempty"`
	Source             string                         `json:"source,omitempty"`
	Message            string                         `json:"message"`
	Tags               []int                          `json:"tags,omitempty"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
	Data               json.RawMessage                `json:"data,omitempty"`
}

// CodeText is the code of the diagnostic as text, whether the server sent a string or a number.
func (d Diagnostic) CodeText() string {
	if isNull(d.Code) {
		return ""
	}
	var s string
	if err := json.Unmarshal(d.Code, &s); err == nil {
		return s
	}
	return string(trimmed(d.Code))
}

// CodeDescription links to the description of a diagnostic's code.
type CodeDescription struct {
	Href string `json:"href"`
}

// DiagnosticRelatedInformation points at another place that matters to a diagnostic.
type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// PublishDiagnosticsParams are the params of textDocument/publishDiagnostics. Version is the version of
// the document the diagnostics are for, when the server says.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// DocumentDiagnosticParams are the params of the pull request textDocument/diagnostic.
type DocumentDiagnosticParams struct {
	TextDocument     TextDocumentIdentifier `json:"textDocument"`
	Identifier       string                 `json:"identifier,omitempty"`
	PreviousResultID string                 `json:"previousResultId,omitempty"`
}

// DocumentDiagnosticReport is the answer to a pull request: Kind is "full" (Items are all the
// diagnostics) or "unchanged" (those of ResultID still hold).
type DocumentDiagnosticReport struct {
	Kind             string                              `json:"kind"`
	ResultID         string                              `json:"resultId,omitempty"`
	Items            []Diagnostic                        `json:"items,omitempty"`
	RelatedDocuments map[string]DocumentDiagnosticReport `json:"relatedDocuments,omitempty"`
}

// WorkspaceFolder is a folder of the workspace.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// ExecuteCommandParams are the params of workspace/executeCommand.
type ExecuteCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

// FileEvent is a change of a file the client watches for the server.
type FileEvent struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

// DidChangeWatchedFilesParams are the params of workspace/didChangeWatchedFiles.
type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

// DidChangeConfigurationParams are the params of workspace/didChangeConfiguration.
type DidChangeConfigurationParams struct {
	Settings json.RawMessage `json:"settings"`
}

// FileSystemWatcher is a glob a server wants to hear about; GlobPattern is a string or a
// RelativePattern object, as JSON. Kind is a mask of FileCreated=1, FileChanged=2 and FileDeleted=4 (7,
// all, when nil).
type FileSystemWatcher struct {
	GlobPattern json.RawMessage `json:"globPattern"`
	Kind        *int            `json:"kind,omitempty"`
}

// DidChangeWatchedFilesRegistrationOptions are the registration options of
// workspace/didChangeWatchedFiles.
type DidChangeWatchedFilesRegistrationOptions struct {
	Watchers []FileSystemWatcher `json:"watchers"`
}

// ShowMessageParams are the params of window/showMessage.
type ShowMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

// LogMessageParams are the params of window/logMessage.
type LogMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

// MessageActionItem is a button of a message request.
type MessageActionItem struct {
	Title string `json:"title"`
}

// ShowMessageRequestParams are the params of window/showMessageRequest.
type ShowMessageRequestParams struct {
	Type    int                 `json:"type"`
	Message string              `json:"message"`
	Actions []MessageActionItem `json:"actions,omitempty"`
}

// ConfigurationItem asks for one section of the settings.
type ConfigurationItem struct {
	ScopeURI string `json:"scopeUri,omitempty"`
	Section  string `json:"section,omitempty"`
}

// ConfigurationParams are the params of workspace/configuration.
type ConfigurationParams struct {
	Items []ConfigurationItem `json:"items"`
}

// Registration is a capability the server registers while it runs.
type Registration struct {
	ID              string          `json:"id"`
	Method          string          `json:"method"`
	RegisterOptions json.RawMessage `json:"registerOptions,omitempty"`
}

// RegistrationParams are the params of client/registerCapability.
type RegistrationParams struct {
	Registrations []Registration `json:"registrations"`
}

// Unregistration removes a registration.
type Unregistration struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

// UnregistrationParams are the params of client/unregisterCapability; the protocol spells the member
// "unregisterations".
type UnregistrationParams struct {
	Unregisterations []Unregistration `json:"unregisterations"`
}

// ProgressToken identifies a progress: a string or a number, kept as the server sent it.
type ProgressToken struct{ raw json.RawMessage }

// Key is the token as text, usable as a map key.
func (t ProgressToken) Key() string { return string(t.raw) }

// MarshalJSON writes the token as it was read.
func (t ProgressToken) MarshalJSON() ([]byte, error) {
	if len(t.raw) == 0 {
		return []byte("null"), nil
	}
	return t.raw, nil
}

// UnmarshalJSON reads a string or a number.
func (t *ProgressToken) UnmarshalJSON(data []byte) error {
	t.raw = append(t.raw[:0], trimmed(data)...)
	return nil
}

// NewProgressToken makes the token with a string value.
func NewProgressToken(s string) ProgressToken {
	raw, err := json.Marshal(s)
	if err != nil {
		return ProgressToken{}
	}
	return ProgressToken{raw: raw}
}

// ProgressParams are the params of $/progress; Value depends on what the progress is for.
type ProgressParams struct {
	Token ProgressToken   `json:"token"`
	Value json.RawMessage `json:"value"`
}

// The kinds of work done progress.
const (
	ProgressBegin  = "begin"
	ProgressReport = "report"
	ProgressEnd    = "end"
)

// WorkDoneProgress is the value of a $/progress of a work done progress: Kind is "begin", "report" or
// "end".
type WorkDoneProgress struct {
	Kind        string `json:"kind"`
	Title       string `json:"title,omitempty"`
	Message     string `json:"message,omitempty"`
	Percentage  *int   `json:"percentage,omitempty"`
	Cancellable bool   `json:"cancellable,omitempty"`
}

// WorkDone reads the value as a work done progress; ok is false when it is not one.
func (p ProgressParams) WorkDone() (w WorkDoneProgress, ok bool) {
	if err := json.Unmarshal(p.Value, &w); err != nil {
		return WorkDoneProgress{}, false
	}
	switch w.Kind {
	case ProgressBegin, ProgressReport, ProgressEnd:
		return w, true
	}
	return WorkDoneProgress{}, false
}

// WorkDoneProgressCreateParams are the params of window/workDoneProgress/create.
type WorkDoneProgressCreateParams struct {
	Token ProgressToken `json:"token"`
}

// WorkDoneProgressCancelParams are the params of window/workDoneProgress/cancel.
type WorkDoneProgressCancelParams struct {
	Token ProgressToken `json:"token"`
}
