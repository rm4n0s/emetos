// Package mediapreview is the extension Media Preview (SPEC §10.37 "Image Preview" and "Video", VS Code's media-preview): the file
// editor of images, with the zoom of VS Code's image preview and its three status bar entries, and the video player. Images are decoded
// off the UI thread, by Glutter's codec (PNG, JPEG, GIF, BMP, WebP), golang.org/x/image/tiff, a reader of ICO files and Glutter's svg
// package, and AVIF and HEIC by FFmpeg; videos are decoded by the user's FFmpeg and their sound played through Glutter's audio.
package mediapreview

import (
	"context"
	"os"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
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

// ImageGlobs are the files the image preview opens; AVIF, HEIC and HEIF are read through FFmpeg.
var ImageGlobs = []string{"*.png", "*.jpg", "*.jpeg", "*.jpe", "*.gif", "*.bmp", "*.ico", "*.webp", "*.tif", "*.tiff", "*.svg", "*.avif", "*.heic", "*.heif"}

// whenImagePreview is the when-clause of the commands: the image preview is the active editor (VS Code's).
const whenImagePreview = "activeCustomEditorId == '" + ImageEditorID + "'"

// Extension is Media Preview in one scope. Its fields belong to the UI thread.
type Extension struct {
	api      extension.API
	ctx      context.Context
	previews []*preview
	videos   []*videoTab
	// findFFmpeg, probe, newMedia, newSink and clockOf are FFmpeg, the sound device and the clock; the tests give fakes.
	findFFmpeg func(ctx context.Context, setting string) (ffmpeg.Programs, error)
	probe      func(ctx context.Context, progs ffmpeg.Programs, path string) (ffmpeg.Info, error)
	newMedia   func(progs ffmpeg.Programs, path string) media
	newSink    func(ctx context.Context, binding *widgets.WidgetsBinding) (sink, error)
	clockOf    func(binding *widgets.WidgetsBinding) clock
}

// New is the Factory of the extension.
func New() extension.Extension {
	x := &Extension{
		findFFmpeg: func(ctx context.Context, setting string) (ffmpeg.Programs, error) {
			return ffmpeg.Find(ctx, ffmpeg.FindOptions{Setting: setting, Path: os.Getenv("PATH")})
		},
		probe:    ffmpeg.Probe,
		newMedia: func(progs ffmpeg.Programs, path string) media { return ffmpegMedia{progs: progs, path: path} },
		clockOf:  func(b *widgets.WidgetsBinding) clock { return bindingClock{b} },
	}
	x.newSink = func(ctx context.Context, b *widgets.WidgetsBinding) (sink, error) {
		return openAudioSink(ctx, b, x.api.Log())
	}
	return x
}

// bindingClock is the real time, with the timers of the binding (on the UI thread).
type bindingClock struct{ b *widgets.WidgetsBinding }

// Now implements clock.
func (bindingClock) Now() time.Time { return time.Now() }

// AfterFunc implements clock.
func (c bindingClock) AfterFunc(d time.Duration, fn func()) func() {
	return c.b.Painting().AfterFunc(d, fn)
}

// Manifest implements extension.Extension.
func (x *Extension) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Media Preview", Version: "1.0.0",
		Description:  "Previews of images and videos, as VS Code's built-in Media Preview.",
		Requirements: requirements(),
		Contributes: extension.Contributes{
			FileEditors: []extension.FileEditorSpec{
				{ID: ImageEditorID, Name: "Image Preview", Icon: "file-media", Globs: ImageGlobs, Priority: extension.FileEditorDefault},
				{ID: VideoEditorID, Name: "Video Preview", Icon: "device-camera-video", Globs: VideoGlobs, Priority: extension.FileEditorDefault},
			},
			Settings: []extension.SettingSpec{{Key: ffmpeg.PathSetting, Type: "string", Default: "", Scope: "machine",
				Description: "The path of the `ffmpeg` program that plays videos and opens AVIF and HEIC images; `ffprobe` is looked for beside it. When empty, `ffmpeg` is looked for in `PATH`, then in `/opt/homebrew/bin` and `/usr/local/bin`."},
				{Key: SettingAutoPlay, Type: "boolean", Default: false, Description: "Start playing videos on mute automatically."},
				{Key: SettingLoop, Type: "boolean", Default: false, Description: "Loop videos over again automatically."},
			},
			Commands: []extension.CommandSpec{
				{ID: CommandZoomIn, Title: "Zoom in", Category: "Image Preview", When: whenImagePreview},
				{ID: CommandZoomOut, Title: "Zoom out", Category: "Image Preview", When: whenImagePreview},
				{ID: CommandCopyImage, Title: "Copy", Category: "Image Preview", When: whenImagePreview},
				{ID: CommandVideoPlay, Title: "Play", Category: "Video Preview", When: whenVideo},
				{ID: CommandVideoPause, Title: "Pause", Category: "Video Preview", When: whenVideo},
				{ID: CommandVideoToggle, Title: "Play or Pause", Category: "Video Preview", When: whenVideo},
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
	if err := api.FileEditors().Provide(VideoEditorID, extension.FileEditorProvider{Open: x.openVideo}); err != nil {
		return err
	}
	for id, act := range map[string]func(*videoTab){
		CommandVideoPlay:   (*videoTab).play,
		CommandVideoPause:  (*videoTab).hidden,
		CommandVideoToggle: (*videoTab).togglePlay,
	} {
		if err := api.Commands().Handle(id, func(context.Context, any) error {
			if v := x.activeVideo(); v != nil {
				act(v)
			}
			return nil
		}); err != nil {
			return err
		}
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

// activeVideo is the video tab that is the active editor of the window, or nil.
func (x *Extension) activeVideo() *videoTab {
	for _, v := range x.videos {
		if v.tab.Active() {
			return v
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
