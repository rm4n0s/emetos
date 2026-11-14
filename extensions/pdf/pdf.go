// Package pdf is the extension PDF Viewer (SPEC §10.37 "PDF", D54): the file editor of PDF files, whose pages are drawn by the user's
// Poppler (extensions/pdf/poppler), scrolled continuously, with zoom, page navigation and passwords.
package pdf

import (
	"context"
	"image"
	"os"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// ID is the id of the extension, and ViewerID the id of its file editor.
const (
	ID       = "emetos.pdf"
	ViewerID = "emetos.pdf.viewer"
)

// The commands of the viewer.
const (
	CommandZoomIn       = "emetos.pdf.zoomIn"
	CommandZoomOut      = "emetos.pdf.zoomOut"
	CommandNextPage     = "emetos.pdf.nextPage"
	CommandPreviousPage = "emetos.pdf.previousPage"
	CommandGoToPage     = "emetos.pdf.goToPage"
)

// whenViewer is the when-clause of the commands: the viewer is the active editor.
const whenViewer = "activeCustomEditorId == '" + ViewerID + "'"

// Extension is the PDF Viewer in one scope. Its fields belong to the UI thread.
type Extension struct {
	api     extension.API
	ctx     context.Context
	viewers []*viewer
	// find, info and render are Poppler; the tests give fakes.
	find   func(ctx context.Context, setting string) (poppler.Programs, error)
	info   func(ctx context.Context, p poppler.Programs, file, password string) (poppler.Document, error)
	render func(ctx context.Context, p poppler.Programs, file, password string, page int, dpi float64) (image.Image, error)
}

// New is the Factory of the extension.
func New() extension.Extension {
	return &Extension{
		find: func(ctx context.Context, setting string) (poppler.Programs, error) {
			return poppler.Find(ctx, poppler.FindOptions{Setting: setting, Path: os.Getenv("PATH")})
		},
		info:   poppler.Info,
		render: poppler.Render,
	}
}

// Manifest implements extension.Extension.
func (x *Extension) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "PDF Viewer", Version: "1.0.0",
		Description:  "Shows PDF files, with their pages drawn by Poppler.",
		Requirements: requirements(),
		Contributes: extension.Contributes{
			FileEditors: []extension.FileEditorSpec{
				{ID: ViewerID, Name: "PDF Viewer", Icon: "file-pdf", Globs: []string{"*.pdf"}, Priority: extension.FileEditorDefault},
			},
			Settings: []extension.SettingSpec{{Key: poppler.PathSetting, Type: "string", Default: "", Scope: "machine",
				Description: "The folder that holds Poppler's programs (`pdfinfo`, `pdftoppm`, `pdftotext` and `pdftohtml`). When empty, they are looked for in `PATH`, then in `/opt/homebrew/bin` and `/usr/local/bin`."}},
			Commands: []extension.CommandSpec{
				{ID: CommandZoomIn, Title: "Zoom In", Category: "PDF", When: whenViewer},
				{ID: CommandZoomOut, Title: "Zoom Out", Category: "PDF", When: whenViewer},
				{ID: CommandNextPage, Title: "Next Page", Category: "PDF", When: whenViewer},
				{ID: CommandPreviousPage, Title: "Previous Page", Category: "PDF", When: whenViewer},
				{ID: CommandGoToPage, Title: "Go to Page...", Category: "PDF", When: whenViewer},
			},
			Keybindings: []extension.Keybinding{
				{Command: CommandZoomIn, Key: "ctrl+=", When: whenViewer},
				{Command: CommandZoomIn, Key: "ctrl+shift+=", When: whenViewer},
				{Command: CommandZoomOut, Key: "ctrl+-", When: whenViewer},
				{Command: CommandGoToPage, Key: "ctrl+g", When: whenViewer},
			},
		},
	}
}

// Activate implements extension.Extension.
func (x *Extension) Activate(ctx context.Context, api extension.API) error {
	x.api, x.ctx = api, ctx
	if err := api.FileEditors().Provide(ViewerID, extension.FileEditorProvider{Open: x.open}); err != nil {
		return err
	}
	for id, act := range map[string]func(*viewer){
		CommandZoomIn:       (*viewer).zoomIn,
		CommandZoomOut:      (*viewer).zoomOut,
		CommandNextPage:     func(v *viewer) { v.goToPage(v.currentPage() + 1) },
		CommandPreviousPage: func(v *viewer) { v.goToPage(v.currentPage() - 1) },
		CommandGoToPage:     (*viewer).focusPageBox,
	} {
		if err := api.Commands().Handle(id, func(context.Context, any) error {
			if v := x.active(); v != nil {
				act(v)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// active is the viewer that is the active editor of the window, or nil.
func (x *Extension) active() *viewer {
	for _, v := range x.viewers {
		if v.tab.Active() {
			return v
		}
	}
	return nil
}
