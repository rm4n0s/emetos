package extension

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/rm4n0s/errors"
)

// The Editor Decorations API (SPEC §10.30 "Blame", step 24.22; after the `after` decoration type of vscode.window.createTextEditorDecorationType
// and the hover provider that the Git extension registers for its blame text). An extension puts a muted text after the end of lines of
// a document; every editor that shows the document draws it, and a hover on it shows the markdown the extension gave.
//
// Everything here may be called from any goroutine.

// MaxLineAnnotations is how many annotations one Set may give; more makes the call fail with ExtensionEditorDecorationsInvalid.
const MaxLineAnnotations = 10000

// MaxLineAnnotationText is how many runes of the text of an annotation are kept; a longer text is cut (the editor clips it at the edge
// of the editor anyway).
const MaxLineAnnotationText = 1000

// MaxLineAnnotationHover is how many bytes the markdown of a hover may have; a longer one is cut.
const MaxLineAnnotationHover = 64 * 1024

// LineAnnotation is a text drawn after the end of a line (VS Code's DecorationOptions with an `after` render option).
type LineAnnotation struct {
	// Line is the line, counted from 0 as the positions of the events are. A line the document does not have is not drawn.
	Line int
	// Text is the text after the line, in the font of the editor, 50 pixels from the end of the text of the line (VS Code's margin). It
	// does not move the text, is not selectable and is clipped at the edge of the editor.
	Text string
	// Color is the id of the theme color the text is drawn in ("git.blame.editorDecorationForeground"); empty, or a color the theme does
	// not know, draws it in editorInlayHint.foreground.
	Color string
	// Hover is a markdown text shown in a hover box when the pointer rests on the annotation; empty for none. A link whose destination is
	// command:<id> or command:<id>?<JSON arguments> (an array, or a single value) runs the command, when the command is one of the
	// extension itself (declared by its manifest or registered by it); a link to the command of another extension is ignored, and
	// so is every other kind of command link (VS Code's isTrusted, narrowed to the extension's own commands).
	Hover string
}

// EditorDecorations shows annotations in the editors of the documents of the scope.
type EditorDecorations interface {
	// Set replaces everything the key holds with the annotations of the document at uri (file:///path, untitled:1). The key names the
	// set, among the sets of the extension: a Set with a key that exists replaces its set whatever its document was, and an empty list
	// removes it. The editors that show the document draw the annotations at once, those that open it later when they open. It fails
	// with ExtensionEditorDecorationsInvalid for an empty key or URI, a negative line or more than MaxLineAnnotations annotations,
	// ExtensionEditorDecorationsUnavailable in a scope that has no editors (a host without a workbench) and ExtensionDeactivated after
	// the activation ended.
	Set(key, uri string, annotations []LineAnnotation) error
	// Clear removes the set of the key, if there is one. It happens by itself when the activation ends.
	Clear(key string)
}

// ParseCommandLink reads a link of a hover of the form command:<id> or command:<id>?<arguments>, where the arguments are JSON, percent-encoded as
// VS Code's encodeURIComponent does: an array is the list of the arguments (one argument is returned as it is, none as nil), any other
// value is the argument. It fails with ExtensionCommandLinkInvalid for a destination that is not a command link, one with no command id and
// arguments that are not JSON.
func ParseCommandLink(dest string) (id string, args any, err error) {
	rest, ok := strings.CutPrefix(dest, "command:")
	if !ok || rest == "" {
		return "", nil, errors.New("ExtensionCommandLinkInvalid", "a command link starts with command: and names a command", "link", dest)
	}
	id, query, _ := strings.Cut(rest, "?")
	if id == "" || strings.ContainsAny(id, " \t\r\n") {
		return "", nil, errors.New("ExtensionCommandLinkInvalid", "a command link starts with command: and names a command", "link", dest)
	}
	if query == "" {
		return id, nil, nil
	}
	raw, uerr := url.QueryUnescape(strings.ReplaceAll(query, "+", "%2B"))
	if uerr != nil {
		return "", nil, errors.NewErr("ExtensionCommandLinkInvalid", uerr).SetMetadata("link", dest)
	}
	var v any
	if jerr := json.Unmarshal([]byte(raw), &v); jerr != nil {
		return "", nil, errors.NewErr("ExtensionCommandLinkInvalid", jerr).SetMetadata("link", dest)
	}
	if list, isList := v.([]any); isList {
		switch len(list) {
		case 0:
			return id, nil, nil
		case 1:
			return id, list[0], nil
		}
		return id, list, nil
	}
	return id, v, nil
}
