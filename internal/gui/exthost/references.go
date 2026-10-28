package exthost

import (
	"context"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Find All References and Find All Implementations for extensions (SPEC §10.28, step 17e.3): Languages().References and
// Implementations ask the registry of the scope, the same one the editor's Go to References asks, so an extension gets what
// the language servers and the providers in Go answer; and Documents().Active and Clipboard().WriteText give the extension the
// cursor to ask about and a way to copy what it shows.

// ActiveDocumentSource is the optional part of a DocumentSource that describes the text editor in front.
type ActiveDocumentSource interface {
	// Active describes the active text editor with its selections now; false when there is none. It is called on the UI
	// thread.
	Active() (extension.ActiveDocument, bool)
}

// Active implements extension.Documents.
func (d documentsAPI) Active() (extension.ActiveDocument, bool) {
	src, ok := d.inst.scope.opts.Documents.(ActiveDocumentSource)
	if !ok || d.inst.isDead() {
		return extension.ActiveDocument{}, false
	}
	return src.Active()
}

// References implements extension.Languages.
func (l languagesAPI) References(ctx context.Context, uri string, pos extension.Position, done func([]extension.Location, error)) error {
	return l.locations(ctx, uri, pos, done, langfeatures.FeatureReferences)
}

// Implementations implements extension.Languages.
func (l languagesAPI) Implementations(ctx context.Context, uri string, pos extension.Position, done func([]extension.Location, error)) error {
	return l.locations(ctx, uri, pos, done, langfeatures.FeatureImplementation)
}

// locations asks the registry for the places of a feature (references or implementations) of the symbol at the position, off
// the UI thread, and hands the answer to done on it.
func (l languagesAPI) locations(ctx context.Context, uri string, pos extension.Position, done func([]extension.Location, error), feature langfeatures.Feature) error {
	in := l.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if done == nil {
		return errors.New("ExtensionRequestInvalid", "a request for locations needs a function to call with the answer", "extension", in.id, "uri", uri)
	}
	u, err := document.ParseURI(uri)
	if err != nil {
		return err
	}
	lsw := in.scope.opts.LSP
	if lsw == nil {
		return errors.New("ExtensionDocumentUnavailable", "the scope has no language features", "extension", in.id, "uri", uri)
	}
	var doc *document.Document
	for _, d := range lsw.Documents() {
		if d.URI() == u {
			doc = d
		}
	}
	if doc == nil {
		return errors.New("ExtensionDocumentUnavailable", "the document is not open in the scope", "extension", in.id, "uri", uri)
	}
	ld := langfeatures.Document{URI: u, LanguageID: doc.LanguageID(), Version: doc.Version(), Text: doc.Snapshot()}
	s := in.scope
	finish := func(out []extension.Location, err error) {
		s.post(func() { s.runPosted(in, func() { done(out, err) }) })
	}
	if !s.features.Has(ld, feature) {
		finish(nil, errors.New("ExtensionNoProvider", "no language server or provider offers this for the document", "extension", in.id, "uri", uri))
		return nil
	}
	rctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(in.ctx, cancel)
	go func() {
		defer stop()
		defer cancel()
		var found []langfeatures.Location
		var errs []langfeatures.ProviderError
		if feature == langfeatures.FeatureReferences {
			found, errs = s.features.References(rctx, ld, fromPos(pos), true)
		} else {
			var links []langfeatures.LocationLink
			links, errs = s.features.Locations(rctx, ld, fromPos(pos), langfeatures.NavImplementation)
			for _, link := range links {
				target := link.Target
				if link.TargetSelection != (langfeatures.Range{}) {
					target.Range = link.TargetSelection
				}
				found = append(found, target)
			}
		}
		if err := rctx.Err(); err != nil {
			finish(nil, errors.NewErr("ExtensionRequestCancelled", err).SetMetadata("extension", in.id, "uri", uri))
			return
		}
		if len(found) == 0 && len(errs) > 0 {
			finish(nil, errs[0].Err)
			return
		}
		out := make([]extension.Location, len(found))
		for i, f := range found {
			out[i] = extension.Location{URI: f.URI.String(), Range: toRange(f.Range)}
		}
		finish(out, nil)
	}()
	return nil
}

// Clipboard implements extension.API.
func (a *api) Clipboard() extension.Clipboard { return clipboardAPI{a.inst} }

type clipboardAPI struct{ inst *instance }

// WriteText implements extension.Clipboard.
func (c clipboardAPI) WriteText(text string) error {
	in := c.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if in.scope.opts.Clipboard == nil {
		return errors.New("ExtensionClipboardUnavailable", "the scope has no clipboard", "extension", in.id)
	}
	return in.scope.opts.Clipboard(text)
}
