package exthost

import (
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/annotations"
	"github.com/rm4n0s/emetos/internal/core/command"
)

// The binding of extension.EditorDecorations to the store of line annotations of a scope (SPEC §10.30 "Blame", step 24.22). An extension
// sets the annotations of a document under a key; the editors that show the document draw them. The links of a hover that name a command
// run it only when the command is one of the extension itself.

// editorDecorationsAPI is the extension.EditorDecorations of one activation.
type editorDecorationsAPI struct{ inst *instance }

var _ extension.EditorDecorations = editorDecorationsAPI{}

// EditorDecorations implements extension.API.
func (a *api) EditorDecorations() extension.EditorDecorations { return editorDecorationsAPI{a.inst} }

// Set implements extension.EditorDecorations.
func (d editorDecorationsAPI) Set(key, uri string, list []extension.LineAnnotation) error {
	in := d.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if key == "" || uri == "" {
		return errors.New("ExtensionEditorDecorationsInvalid", "a set of annotations needs a key and the URI of a document", "extension", in.id, "key", key, "uri", uri)
	}
	if len(list) > extension.MaxLineAnnotations {
		return errors.New("ExtensionEditorDecorationsInvalid", "a set has too many annotations", "extension", in.id, "key", key, "count", len(list), "max", extension.MaxLineAnnotations)
	}
	for _, an := range list {
		if an.Line < 0 {
			return errors.New("ExtensionEditorDecorationsInvalid", "the line of an annotation is not negative", "extension", in.id, "key", key, "line", an.Line)
		}
	}
	store := in.scope.opts.EditorDecorations
	if store == nil {
		return errors.New("ExtensionEditorDecorationsUnavailable", "the scope has no editors to decorate", "extension", in.id)
	}
	out := make([]annotations.Annotation, len(list))
	for i, an := range list {
		out[i] = annotations.Annotation{Line: an.Line, Text: cutRunes(an.Text, extension.MaxLineAnnotationText), Color: an.Color, Hover: cutBytes(an.Hover, extension.MaxLineAnnotationHover)}
		if an.Hover != "" {
			out[i].Link = d.followLink
		}
	}
	in.mu.Lock()
	first := !in.annotationKeys[key]
	in.annotationKeys[key] = true
	in.mu.Unlock()
	store.Set(in.id, key, uri, out)
	if first {
		in.track(func() { store.Clear(in.id, key) })
	}
	return nil
}

// Clear implements extension.EditorDecorations.
func (d editorDecorationsAPI) Clear(key string) {
	in := d.inst
	if in.isDead() {
		return
	}
	if store := in.scope.opts.EditorDecorations; store != nil {
		store.Clear(in.id, key)
	}
}

// followLink runs the command a link of a hover names, when it is a command of the extension itself. The link has the form
// command:<id>?<JSON arguments>, the arguments percent-encoded: an array is the list of the arguments (one argument is passed as it is,
// none as nil), any other value is the argument. Everything else is logged and ignored.
func (d editorDecorationsAPI) followLink(dest string) {
	in := d.inst
	if in.isDead() {
		return
	}
	id, args, err := extension.ParseCommandLink(dest)
	if err != nil {
		in.log.Warn("a link of a hover is not a command link", "link", dest, "err", err)
		return
	}
	if !in.ownsCommand(id) {
		in.log.Warn("a link of a hover names a command of another extension and was ignored", "command", id)
		return
	}
	if err := in.scope.opts.Commands.Run(command.Context{Ctx: in.ctx, Source: command.SourceAPI}, id, args); err != nil {
		in.log.Warn("the command of a link of a hover failed", "command", id, "err", err)
	}
}

// ownsCommand tells whether the command is one of the extension: declared by its manifest or registered by the activation.
func (in *instance) ownsCommand(id string) bool {
	for _, spec := range in.slot.info.Manifest.Contributes.Commands {
		if spec.ID == id {
			return true
		}
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.ownCommands[id]
}

// cutRunes cuts a text to n runes.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// cutBytes cuts a text to n bytes, at the start of a rune.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
