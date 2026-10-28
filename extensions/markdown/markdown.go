// Package markdown is the Markdown extension (SPEC §8.1, §10.15): the language pack of Markdown, with the languages of fenced
// code blocks, and the preview file view of a Markdown file, with the editor title action "Open Preview to the Side". The
// grammars and language configurations are in assets/, copied by scripts/update-assets.sh; the licenses are in
// third_party/ and NOTICE. The preview is drawn by extension/ui/markdown.
package markdown

import (
	"context"
	"embed"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
)

// The ids of the extension.
const (
	// ID is the id of the extension.
	ID = "emetos.markdown"
	// PreviewView is the id of the preview file view, and OpenPreviewToSideCommand the command of the editor title action.
	PreviewView              = "emetos.markdown.preview"
	OpenPreviewToSideCommand = "emetos.markdown.openPreviewToSide"
)

//go:embed all:assets
var assets embed.FS

// New is the Factory of the extension.
func New() extension.Extension {
	return &md{images: markdown.NewImageCache(markdown.DefaultImageBudget), pack: langpack.New(langpack.Spec{
		ID: ID, Name: "Markdown Language Basics", Description: "Syntax highlighting and language configuration for Markdown, with the languages of fenced code blocks, and a preview of Markdown files.",
		Assets: assets,
	})}
}

// md is the extension of one scope: the language pack, and the preview.
type md struct {
	pack extension.Extension
	api  extension.API
	// images decodes and keeps the images of every preview of the window (SPEC §10.15 "As built (step 17b.2)").
	images *markdown.ImageCache
}

// Manifest implements extension.Extension: the pack's, with the preview.
func (m *md) Manifest() extension.Manifest {
	man := m.pack.Manifest()
	man.Contributes.Commands = []extension.CommandSpec{{
		ID: OpenPreviewToSideCommand, Title: "Open Preview to the Side", Category: "Markdown",
		Description: "Shows the preview of the Markdown file of the editor in the group on the side.",
	}}
	man.Contributes.EditorActions = []extension.EditorActionSpec{{
		Command: OpenPreviewToSideCommand, Icon: "open-preview", Tooltip: "Open Preview to the Side", When: "resourceLangId == markdown", Order: 1,
	}}
	man.Contributes.FileViews = []extension.FileViewSpec{{
		ID: PreviewView, Name: "Preview", Icon: "preview", Languages: []string{"markdown"}, Order: 1,
	}}
	return man
}

// Activate implements extension.Extension.
func (m *md) Activate(ctx context.Context, api extension.API) error {
	if err := m.pack.Activate(ctx, api); err != nil {
		return err
	}
	m.api = api
	if err := api.FileViews().Provide(PreviewView, extension.FileViewProvider{Open: m.open}); err != nil {
		return err
	}
	return api.Commands().Handle(OpenPreviewToSideCommand, func(_ context.Context, args any) error {
		return api.FileViews().Open("", PreviewView, extension.FileViewOpenOptions{Side: true, Group: extension.GroupOfArgs(args)})
	})
}

// open makes the preview of a document: it draws the text of the document, again after each change, and follows the scroll
// of the Text view.
func (m *md) open(doc extension.DocumentHandle, tab extension.FileViewTab) (extension.FileViewContent, error) {
	ctl := markdown.NewController()
	ctl.ScrollToLine(doc.TopLine())
	stopChange := doc.OnChange(func(extension.ViewDocumentChange) { tab.Refresh() })
	stopScroll := doc.OnScroll(func(line int) { ctl.ScrollToLine(line) })
	opts := markdown.Options{
		Controller:   ctl,
		Tokenize:     m.tokenize,
		OnLink:       func(dest string) { m.openLink(doc, dest) },
		ResolveImage: func(dest string) string { return localImage(doc, dest) },
		Images:       m.images,
	}
	return extension.FileViewContent{
		Build: func(widgets.BuildContext) widgets.Widget { return markdown.NewView(doc.Text(), opts) },
		Close: func() { stopChange(); stopScroll() },
	}, nil
}

// tokenize highlights the code of a fenced block.
func (m *md) tokenize(language, text string) ([][]extension.Token, bool) {
	id, ok := m.api.Languages().Resolve(language)
	if !ok {
		return nil, false
	}
	lines, err := m.api.Languages().Tokenize(id, text)
	return lines, err == nil
}

// openLink follows a link of the preview: a web or mail address goes to the desktop (the OpenURI portal), a link to a file of
// the workspace opens the file, and a link to a place in the document or to anything else does nothing.
func (m *md) openLink(doc extension.DocumentHandle, dest string) {
	log := m.api.Log()
	u, err := url.Parse(dest)
	if err != nil {
		log.Debug("a link of the preview cannot be read", "dest", dest, "err", err)
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		go func() {
			if err := m.api.Workspace().OpenURI(context.Background(), dest); err != nil {
				m.api.Post(func() { m.api.Notifications().Warn("Cannot open " + dest + ": " + err.Error()) })
			}
		}()
	case "":
		if u.Path == "" {
			return // a place in the document
		}
		file := linkedFile(doc, u.Path)
		if file == "" {
			m.api.Notifications().Info("The linked file " + u.Path + " is not in the workspace.")
			return
		}
		if err := doc.OpenFile(file); err != nil {
			m.api.Notifications().Warn("Cannot open " + file + ": " + err.Error())
		}
	}
}

// linkedFile is the absolute path of the file a relative link names, when it is a file inside the folder of the workspace
// (or the folder of the document, when the workspace has none).
func linkedFile(doc extension.DocumentHandle, rel string) string {
	root, base := root(doc)
	if root == "" {
		return ""
	}
	if p, err := url.PathUnescape(rel); err == nil {
		rel = p
	}
	var abs string
	if path.IsAbs(rel) {
		abs = filepath.Join(root, filepath.FromSlash(rel))
	} else {
		abs = filepath.Join(base, filepath.FromSlash(rel))
	}
	if !within(root, abs) {
		return ""
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		return ""
	}
	return abs
}

// root is the folder the links of a document stay in, and the folder links are relative to.
func root(doc extension.DocumentHandle) (root, base string) {
	u, err := url.Parse(doc.URI())
	if err != nil || u.Scheme != "file" {
		return "", ""
	}
	base = filepath.Dir(u.Path)
	root = doc.Folder()
	if root == "" || !within(root, u.Path) {
		root = base
	}
	return root, base
}

// within tells whether a path is the folder or inside it.
func within(folder, p string) bool {
	rel, err := filepath.Rel(folder, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// localImage is the file an image of the preview shows: a path relative to the document, or to the folder when it starts
// with a slash. Web images are not loaded (nothing in Emetos fetches from the network on its own).
func localImage(doc extension.DocumentHandle, dest string) string {
	if u, err := url.Parse(dest); err != nil || u.Scheme != "" {
		return ""
	}
	dest, _, _ = strings.Cut(dest, "#")
	dest, _, _ = strings.Cut(dest, "?")
	return linkedFile(doc, dest)
}
