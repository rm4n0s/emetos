// Package mediapreview is the extension Media Preview (SPEC §10.37 "Image Preview", VS Code's media-preview): the file editor of
// images, with the zoom of VS Code's image preview and its three status bar entries. Images are decoded off the UI thread, by
// Glutter's codec (PNG, JPEG, GIF, BMP, WebP), golang.org/x/image/tiff, a reader of ICO files and Glutter's svg package.
package mediapreview

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
)

// ID is the id of the extension, and ImageEditorID the id of its image preview.
const (
	ID            = "emetos.mediapreview"
	ImageEditorID = "emetos.mediapreview.image"
)

// The commands of the image preview, with VS Code's ids.
const (
	CommandZoomIn    = "imagePreview.zoomIn"
	CommandZoomOut   = "imagePreview.zoomOut"
	CommandCopyImage = "imagePreview.copyImage"
)

// ImageGlobs are the files the image preview opens; AVIF, HEIC and HEIF come with FFmpeg (step 36.4).
var ImageGlobs = []string{"*.png", "*.jpg", "*.jpeg", "*.jpe", "*.gif", "*.bmp", "*.ico", "*.webp", "*.tif", "*.tiff", "*.svg"}

// whenImagePreview is the when-clause of the commands: the image preview is the active editor (VS Code's).
const whenImagePreview = "activeCustomEditorId == '" + ImageEditorID + "'"

// Extension is Media Preview in one scope. Its fields belong to the UI thread.
type Extension struct {
	api      extension.API
	ctx      context.Context
	previews []*preview
}

// New is the Factory of the extension.
func New() extension.Extension { return &Extension{} }

// Manifest implements extension.Extension.
func (x *Extension) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Media Preview", Version: "1.0.0",
		Description: "Previews of images, as VS Code's built-in Media Preview.",
		Contributes: extension.Contributes{
			FileEditors: []extension.FileEditorSpec{{ID: ImageEditorID, Name: "Image Preview", Icon: "file-media", Globs: ImageGlobs,
				Priority: extension.FileEditorDefault}},
			Commands: []extension.CommandSpec{
				{ID: CommandZoomIn, Title: "Zoom in", Category: "Image Preview", When: whenImagePreview},
				{ID: CommandZoomOut, Title: "Zoom out", Category: "Image Preview", When: whenImagePreview},
				{ID: CommandCopyImage, Title: "Copy", Category: "Image Preview", When: whenImagePreview},
			},
		},
	}
}

// Activate implements extension.Extension.
func (x *Extension) Activate(ctx context.Context, api extension.API) error {
	x.api, x.ctx = api, ctx
	if err := api.FileEditors().Provide(ImageEditorID, extension.FileEditorProvider{Open: x.open}); err != nil {
		return err
	}
	for id, act := range map[string]func(*preview){
		CommandZoomIn:    (*preview).zoomIn,
		CommandZoomOut:   (*preview).zoomOut,
		CommandCopyImage: func(p *preview) { p.copyImage() },
	} {
		if err := api.Commands().Handle(id, func(context.Context, any) error {
			if p := x.active(); p != nil {
				act(p)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// active is the preview that is the active editor of the window, or nil.
func (x *Extension) active() *preview {
	for _, p := range x.previews {
		if p.tab.Active() {
			return p
		}
	}
	return nil
}
