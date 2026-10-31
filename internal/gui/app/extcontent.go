package app

import (
	"context"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// What the window gives an extension to open documents it makes up (SPEC §10.30 "Diffs of changes", step 24.7): the registry of the
// read-only documents of a scheme (the virtual documents of §7.1, made by the extension and by no one else), the opening of a
// diff editor or of a single document with a title, and the list and the closing of the tabs. The text of a provided document is
// made off the UI thread; the editor opens when it is there.
//
// Ported from VS Code: src/vs/workbench/api/browser/mainThreadDocumentContentProviders.ts

// ProvideContent implements exthost.ContentHost: the provider of a scheme is added to the registry of the window tab as the provider
// of the paths below /<scheme>/. It fails with the tag ExtContentSchemeTaken when the window tab has the scheme.
func (d extensionDocuments) ProvideContent(owner string, p extension.ContentProvider) (extension.ContentRegistration, error) {
	w := d.w
	prefix := "/" + p.Scheme + "/"
	if w.virtual.HasPrefix(prefix) {
		return nil, errors.New("ExtContentSchemeTaken", "the window tab has a content provider for this scheme already", "scheme", p.Scheme, "extension", owner)
	}
	uriOf := func(rest string) string { return extension.ContentURI(p.Scheme, rest) }
	label := func(rest string) string {
		if p.Label != nil {
			if l := p.Label(uriOf(rest)); l != "" {
				return l
			}
		}
		return path.Base(rest)
	}
	languages := w.Process.Languages
	err := w.virtual.RegisterPrefix(prefix, document.VirtualPrefixProvider{
		Label: label,
		Language: func(rest string) string {
			if p.Language != nil {
				if l := p.Language(uriOf(rest)); l != "" {
					return l
				}
			}
			return languages.Detect("/"+label(rest), "")
		},
		Text: func(ctx context.Context, rest string) (string, error) { return p.Text(ctx, uriOf(rest)) },
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(w.ctx)
	return &contentRegistration{w: w, prefix: prefix, ctx: ctx, cancel: cancel}, nil
}

// contentRegistration is the extension.ContentRegistration of a provider of the window tab.
type contentRegistration struct {
	w      *Workspace
	prefix string
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

// Changed implements extension.ContentRegistration: the documents of these URIs that are open are made again in the background and
// given the new text when it differs.
func (r *contentRegistration) Changed(uris ...string) {
	w := r.w
	w.onUI(func() {
		for _, raw := range uris {
			u, err := document.ParseURI(raw)
			if err != nil || u.Scheme != document.SchemeVirtual || !strings.HasPrefix(u.Path, r.prefix) {
				continue
			}
			if _, open := w.Documents.Get(u); !open {
				continue
			}
			go func() {
				content, err := w.Documents.GenerateVirtual(r.ctx, u)
				if err != nil {
					w.Process.Logger.Debug("a provided document cannot be made again", "uri", raw, "err", err)
					return
				}
				w.onUI(func() {
					if r.ctx.Err() != nil {
						return
					}
					if doc, open := w.Documents.Get(u); open && doc.Text() != content.Text {
						doc.SetText(content.Text)
					}
				})
			}()
		}
	})
}

// Dispose implements extension.ContentRegistration.
func (r *contentRegistration) Dispose() {
	r.once.Do(func() {
		r.cancel()
		r.w.virtual.UnregisterPrefix(r.prefix)
	})
}

// contentShell is the shell that shows the window tab, or an error with the tag ExtDocumentShowUnavailable when it is not on screen.
func (d extensionDocuments) contentShell() (*shellState, error) {
	s := d.w.shell
	if s == nil || !s.Mounted() {
		return nil, errors.New("ExtDocumentShowUnavailable", "the window tab is not on screen")
	}
	return s, nil
}

// side is one document of a diff or one document to open: a file that is read, an untitled document that is open, or a provided one that
// is made. It is prepared off the UI thread and finished on it.
type side struct {
	uri document.URI
	// doc is the document: acquired for a file or an untitled one, adopted by finish for a provided one.
	doc *document.Document
	// content is what the provider made, for a provided document.
	content document.VirtualContent
	err     error
}

// parseSide reads the URI of a side. A provided document needs a provider (ExtContentUnknown), an untitled one must be open. The
// untitled document is acquired here, on the UI thread.
func (d extensionDocuments) parseSide(raw string) (*side, error) {
	u, err := document.ParseURI(raw)
	if err != nil {
		return nil, err
	}
	sd := &side{uri: u}
	switch u.Scheme {
	case document.SchemeVirtual:
		if !d.w.virtual.Has(u) {
			return nil, errors.New("ExtContentUnknown", "no content provider makes this document", "uri", raw)
		}
	case document.SchemeUntitled:
		doc, ok := d.w.Documents.Get(u)
		if !ok {
			return nil, errors.New("ExtContentUnknown", "an untitled document that is not open cannot be shown", "uri", raw)
		}
		d.w.Documents.Retain(doc)
		sd.doc = doc
	}
	return sd, nil
}

// prepare makes the side ready off the UI thread: the text of a provided document, the document of a file.
func (w *Workspace) prepare(ctx context.Context, sd *side) {
	switch sd.uri.Scheme {
	case document.SchemeVirtual:
		sd.content, sd.err = w.Documents.GenerateVirtual(ctx, sd.uri)
	case document.SchemeFile:
		sd.doc, sd.err = w.Documents.Acquire(ctx, sd.uri.Path, document.NewOptions())
	}
}

// finish turns a prepared provided document into a document, on the UI thread, and counts a use of it.
func (w *Workspace) finish(sd *side) {
	if sd.uri.Scheme == document.SchemeVirtual && sd.err == nil && sd.doc == nil {
		sd.doc = w.Documents.AdoptVirtual(sd.uri, sd.content)
		w.Documents.Retain(sd.doc)
	}
}

// releaseSides lets go of the use of a side's document.
func (w *Workspace) releaseSides(sides ...*side) {
	for _, sd := range sides {
		if sd.doc != nil {
			w.Documents.Release(sd.doc)
			sd.doc = nil
		}
	}
}

// reportOpenFailure tells the user that a document could not be opened: a binary file with the notice of the editor, anything else with
// its message.
func (w *Workspace) reportOpenFailure(name string, err error) {
	if isBinaryError(err) || errors.Is(err, &errors.Error{Tag: extension.TagContentBinary}) {
		w.Notify.Warn("Emetos", name+": "+BinaryFileNotice)
		return
	}
	w.Notify.Error("Emetos", "Cannot open "+name+": "+describe(err))
}

// sideName is a name for a side in a message.
func sideName(sd *side, title string) string {
	if title != "" {
		return title
	}
	return sd.uri.Title()
}

// OpenDiff implements exthost.ContentHost.
func (d extensionDocuments) OpenDiff(spec extension.DiffSpec) error {
	s, err := d.contentShell()
	if err != nil {
		return err
	}
	left, err := d.parseSide(spec.Left)
	if err != nil {
		return err
	}
	right, err := d.parseSide(spec.Right)
	if err != nil {
		d.w.releaseSides(left)
		return err
	}
	w := d.w
	binding := widgets.WidgetsBindingOf(s.Context())
	s.Go(func(life context.Context) {
		w.prepare(life, left)
		if left.err == nil {
			w.prepare(life, right)
		}
		binding.Post(func() {
			failed := left.err
			if failed == nil {
				failed = right.err
			}
			if failed != nil || !s.Mounted() {
				w.releaseSides(left, right)
				if failed != nil && s.Mounted() {
					w.reportOpenFailure(sideName(right, spec.Title), failed)
				}
				return
			}
			w.finish(left)
			w.finish(right)
			s.openDiffWith(left.doc, right.doc, nil, diffOpen{
				Title: spec.Title, Preview: spec.Preview, PreserveFocus: spec.PreserveFocus,
				Place: func(p *diffPane) { placeSelection(p.sides[1], right.doc, spec.Selection) },
			})
		})
	})
	return nil
}

// OpenDocument implements exthost.ContentHost.
func (d extensionDocuments) OpenDocument(spec extension.OpenSpec) error {
	s, err := d.contentShell()
	if err != nil {
		return err
	}
	sd, err := d.parseSide(spec.URI)
	if err != nil {
		return err
	}
	w := d.w
	binding := widgets.WidgetsBindingOf(s.Context())
	s.Go(func(life context.Context) {
		w.prepare(life, sd)
		binding.Post(func() {
			if sd.err != nil || !s.Mounted() {
				w.releaseSides(sd)
				if sd.err != nil && s.Mounted() {
					w.reportOpenFailure(sideName(sd, spec.Title), sd.err)
				}
				return
			}
			w.finish(sd)
			doc := sd.doc
			sd.doc = nil // showAcquired lets go of the use
			s.show(binding, doc, openOptions{
				Preview: spec.Preview, PreserveFocus: spec.PreserveFocus, Title: spec.Title,
				Place: placeFunc(doc, spec.Selection),
			})
		})
	})
	return nil
}

// placeFunc selects a range of the contract in the editor of a document once it exists; nil for none.
func placeFunc(doc *document.Document, sel *extension.Selection) func(*textEditor) {
	if sel == nil {
		return nil
	}
	return func(e *textEditor) { placeSelection(e, doc, sel) }
}

// placeSelection puts the selection of the contract (zero-based lines, byte columns) in an editor, kept inside the text.
func placeSelection(e *textEditor, doc *document.Document, sel *extension.Selection) {
	if e == nil || sel == nil {
		return
	}
	snap := doc.Snapshot()
	clamp := func(p extension.Position) buffer.Position {
		line := min(max(p.Line, 0), max(snap.LineCount()-1, 0))
		text, _ := snap.LineContent(line)
		return buffer.Position{Line: line, Col: min(max(p.Col, 0), len(text))}
	}
	e.view.Cursors().SetSelections([]buffer.Selection{{Anchor: clamp(sel.Anchor), Active: clamp(sel.Active)}})
	e.view.RevealPrimary()
}

// tabID is the identity of a tab of a group for Tabs and CloseTabs.
func tabID(group *editorgroup.Group, in editorgroup.EditorInput) (id, kind string, ok bool) {
	switch v := in.(type) {
	case *editorgroup.TextInput:
		return strconv.Itoa(group.ID()) + ":" + v.Document().URI().String(), "text", true
	case *editorgroup.DiffInput:
		if v.Proposal() != nil {
			return "", "", false
		}
		return strconv.Itoa(group.ID()) + ":" + v.TabKey(), "diff", true
	}
	return "", "", false
}

// Tabs implements exthost.ContentHost.
func (d extensionDocuments) Tabs() []extension.DocumentTab {
	s := d.w.shell
	if s == nil || !s.Mounted() || s.group.grid == nil {
		return nil
	}
	var out []extension.DocumentTab
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			id, kind, ok := tabID(g, in)
			if !ok {
				continue
			}
			tab := extension.DocumentTab{ID: id, Group: g.ID(), Kind: kind, Dirty: in.Dirty(), Preview: !g.Model.IsPinned(in)}
			switch v := in.(type) {
			case *editorgroup.TextInput:
				tab.URI = v.Document().URI().String()
			case *editorgroup.DiffInput:
				tab.Original, tab.Modified = v.Original().URI().String(), v.Modified().URI().String()
			}
			out = append(out, tab)
		}
	}
	return out
}

// CloseTabs implements exthost.ContentHost: the tabs close as the close button closes them, with the question about unsaved changes.
func (d extensionDocuments) CloseTabs(tabs []extension.DocumentTab) int {
	s := d.w.shell
	if s == nil || !s.Mounted() || s.group.grid == nil {
		return 0
	}
	want := map[string]bool{}
	for _, t := range tabs {
		want[t.ID] = true
	}
	found := 0
	var targets []editorgroup.CloseTarget
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		var closing []editorgroup.EditorInput
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			if id, _, ok := tabID(g, in); ok && want[id] {
				closing = append(closing, in)
				found++
			}
		}
		if len(closing) > 0 {
			targets = append(targets, editorgroup.CloseTarget{Group: g, Editors: closing})
		}
	}
	if len(targets) > 0 {
		s.closeConfirmed(targets, nil)
	}
	return found
}

// RevealFile implements exthost.FileRevealer: the file is shown in the Explorer, or its folder in the file manager.
func (d extensionDocuments) RevealFile(path string, inOS bool) error {
	s, err := d.contentShell()
	if err != nil {
		return err
	}
	binding := widgets.WidgetsBindingOf(s.Context())
	if inOS {
		s.revealInOS(binding, path)
		return nil
	}
	s.revealInExplorer(binding, path)
	return nil
}
