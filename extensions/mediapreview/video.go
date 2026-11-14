package mediapreview

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
)

// The video player (SPEC §10.37 "Video", D53): the file editor emetos.mediapreview.video. FFmpeg is found and the file probed off the
// UI thread when the tab opens; the player is made at the first layout, when the size of the view is known, and shows the first frame
// (or the frame of the restored position) before Play.

// VideoEditorID is the id of the video player.
const VideoEditorID = "emetos.mediapreview.video"

// VideoGlobs are the files the video player opens.
var VideoGlobs = []string{"*.mp4", "*.m4v", "*.webm", "*.mkv", "*.mov", "*.avi", "*.ogv", "*.mpg", "*.mpeg", "*.wmv", "*.flv", "*.3gp"}

// VS Code's settings of the video preview.
const (
	SettingAutoPlay = "mediaPreview.video.autoPlay"
	SettingLoop     = "mediaPreview.video.loop"
)

// The commands of the video player.
const (
	CommandVideoPlay   = "emetos.mediapreview.video.play"
	CommandVideoPause  = "emetos.mediapreview.video.pause"
	CommandVideoToggle = "emetos.mediapreview.video.togglePlay"
)

// whenVideo is the when-clause of the commands of the player.
const whenVideo = "activeCustomEditorId == '" + VideoEditorID + "'"

// The steps of the keys: a seek with Left and Right, the volume with Up and Down.
const (
	seekStep   = 5 * time.Second
	volumeStep = 0.1
)

// installDocs are FFmpeg's instructions, and rpmFusionDocs how Fedora gets the codecs its own FFmpeg leaves out.
const (
	installDocs   = "https://ffmpeg.org/download.html"
	rpmFusionDocs = "https://rpmfusion.org/Howto/Multimedia"
)

// fileStamp is what tells that a file changed: its size and the time it was written.
type fileStamp struct {
	size int64
	mod  time.Time
}

// stampOf is the stamp of the file at path; false when it cannot be read.
func stampOf(path string) (fileStamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, false
	}
	return fileStamp{size: info.Size(), mod: info.ModTime()}, true
}

// videoState is what a tab of the player saves: the position in seconds, the volume and the mute.
type videoState struct {
	Position float64  `json:"position,omitempty"`
	Volume   *float64 `json:"volume,omitempty"`
	Muted    bool     `json:"muted,omitempty"`
}

// videoTab is one tab of the player. Its fields belong to the UI thread.
type videoTab struct {
	x      *Extension
	file   extension.FileEditorFile
	tab    extension.FileEditorTab
	ctx    context.Context
	cancel context.CancelFunc
	// gen counts the reads of the file (open, Check Again, a change on disk).
	gen      int
	loading  bool
	progs    ffmpeg.Programs
	info     ffmpeg.Info
	problem  error
	deleted  bool
	player   *player
	saved    videoState
	autoPlay bool
	// playWhenReady is Play asked for before the player was made: it plays when it is, with the sound on.
	playWhenReady bool
	// stamp is the stamp of the file when it was last read: a change that leaves it the same (the watcher's first event) reads nothing.
	stamp  fileStamp
	view   *videoViewState
	closed bool
}

// openVideo is the Open of the player.
func (x *Extension) openVideo(file extension.FileEditorFile, tab extension.FileEditorTab) (extension.FileEditorContent, error) {
	v := &videoTab{x: x, file: file, tab: tab}
	if s := file.State(); len(s) > 0 {
		if err := json.Unmarshal(s, &v.saved); err != nil {
			x.api.Log().Debug("the state of a video tab cannot be read", "path", file.Path(), "err", err)
		}
	} else {
		v.autoPlay, _ = extension.SettingValue[bool](x.api.Settings(), SettingAutoPlay)
	}
	v.ctx, v.cancel = context.WithCancel(x.ctx)
	x.videos = append(x.videos, v)
	file.OnChange(v.changed)
	v.load()
	return extension.FileEditorContent{Build: v.build, SaveState: v.saveState, OnClose: v.close}, nil
}

// load finds FFmpeg and probes the file on a goroutine of its own.
func (v *videoTab) load() {
	v.gen++
	gen, path, ctx := v.gen, v.file.Path(), v.ctx
	v.stamp, _ = stampOf(path)
	setting, _ := extension.SettingValue[string](v.x.api.Settings(), ffmpeg.PathSetting)
	find := v.x.findFFmpeg
	v.loading = true
	v.tab.SetBusy(true)
	go func() {
		progs, err := find(ctx, setting)
		var info ffmpeg.Info
		if err == nil {
			info, err = v.x.probe(ctx, progs, path)
		}
		v.x.api.Post(func() { v.loaded(gen, progs, info, err) })
	}()
}

// loaded takes what load found. A reload keeps the position.
func (v *videoTab) loaded(gen int, progs ffmpeg.Programs, info ffmpeg.Info, err error) {
	if v.closed || gen != v.gen {
		return
	}
	v.loading = false
	v.tab.SetBusy(false)
	if v.player != nil {
		v.saved.Position = v.player.Position().Seconds()
		v.saved.Volume, v.saved.Muted = &v.player.volume, v.player.muted
		v.player.Close()
		v.player = nil
	}
	v.progs, v.info, v.problem, v.deleted = progs, info, err, false
	if err == nil && !info.HasVideo && !info.HasAudio {
		v.problem = errors.New("VideoHasNoStreams", "The file has no picture and no sound.", "path", v.file.Path())
	}
	v.update()
}

// changed is the file changed or deleted on disk.
func (v *videoTab) changed(c extension.FileEditorChange) {
	if c.Deleted {
		v.gen++
		v.loading, v.deleted = false, true
		v.tab.SetBusy(false)
		if v.player != nil {
			v.player.Close()
			v.player = nil
		}
		v.update()
		return
	}
	if s, ok := stampOf(v.file.Path()); ok && s == v.stamp && !v.deleted {
		return
	}
	v.load()
}

// update builds the view again.
func (v *videoTab) update() {
	if v.view != nil {
		v.view.changed()
	}
}

// videoOf finds FFmpeg (with the setting read now, on the UI thread) and probes the file, for Decode of the formats only FFmpeg reads.
func (x *Extension) videoOf(path string) func(context.Context) (ffmpeg.Info, media, error) {
	setting, _ := extension.SettingValue[string](x.api.Settings(), ffmpeg.PathSetting)
	find, probe, newMedia := x.findFFmpeg, x.probe, x.newMedia
	return func(ctx context.Context) (ffmpeg.Info, media, error) {
		progs, err := find(ctx, setting)
		if err != nil {
			return ffmpeg.Info{}, nil, err
		}
		info, err := probe(ctx, progs, path)
		if err != nil {
			return ffmpeg.Info{}, nil, err
		}
		return info, newMedia(progs, path), nil
	}
}

// decodeSize is the size the frames are decoded at in a view of w x h pixels: the picture fitted, never above its own size.
func decodeSize(info ffmpeg.Info, w, h float64) (int, int) {
	vw, vh := float64(info.Width), float64(info.Height)
	if vw <= 0 || vh <= 0 {
		return 2, 2
	}
	s := 1.0
	if w > 0 && h > 0 {
		s = math.Min(1, math.Min(w/vw, h/vh))
	}
	return max(2, int(math.Round(vw*s))), max(2, int(math.Round(vh*s)))
}

// ensurePlayer makes the player at the first layout after the probe, with the view's size in pixels, and shows the frame of the
// restored position (and plays it when mediaPreview.video.autoPlay asks, muted as VS Code does).
func (v *videoTab) ensurePlayer(binding *widgets.WidgetsBinding, w, h float64) {
	if v.player != nil || v.problem != nil || v.loading || v.deleted || v.info.Duration == 0 && !v.info.HasVideo && !v.info.HasAudio {
		return
	}
	dw, dh := decodeSize(v.info, w, h)
	clk := v.x.clockOf(binding)
	p := newPlayer(playerOptions{
		info: v.info, media: v.x.newMedia(v.progs, v.file.Path()), clock: clk, post: v.x.api.Post, changed: v.update,
		newSink: func() (sink, error) { return v.x.newSink(v.ctx, binding) },
		width:   dw, height: dh,
	})
	if v.saved.Volume != nil {
		p.volume = min(max(*v.saved.Volume, 0), 1)
	}
	p.muted = v.saved.Muted
	p.loop, _ = extension.SettingValue[bool](v.x.api.Settings(), SettingLoop)
	v.player = p
	p.Seek(time.Duration(v.saved.Position * float64(time.Second)))
	switch {
	case v.playWhenReady:
		v.playWhenReady, v.autoPlay = false, false
		p.Play()
	case v.autoPlay:
		v.autoPlay = false
		p.muted = true
		p.Play()
	}
}

// saveState is the state of the tab.
func (v *videoTab) saveState() (any, error) {
	if v.player == nil {
		return v.saved, nil
	}
	vol := v.player.volume
	return videoState{Position: v.player.Position().Seconds(), Volume: &vol, Muted: v.player.muted}, nil
}

// close ends the tab: the player's processes end at once.
func (v *videoTab) close() {
	v.closed = true
	v.cancel()
	if v.player != nil {
		v.player.Close()
	}
	v.x.videos = slices.DeleteFunc(v.x.videos, func(o *videoTab) bool { return o == v })
}

// hidden pauses the player while its tab is not on screen.
func (v *videoTab) hidden() {
	if v.player != nil {
		v.player.Pause()
	}
}

// play is the command Play: before the player is made (FFmpeg is still found and the file probed) it plays when it is.
func (v *videoTab) play() {
	if v.player == nil {
		v.playWhenReady = v.problem == nil && !v.deleted
		return
	}
	v.player.Play()
}

// togglePlay is Space and the button.
func (v *videoTab) togglePlay() {
	if v.player == nil {
		v.playWhenReady = !v.playWhenReady && v.problem == nil && !v.deleted
		return
	}
	if v.player.playing {
		v.player.Pause()
	} else {
		v.player.Play()
	}
}

// problemText is the message of what keeps the video from playing, and the address of its instructions.
func problemText(err error, what string) (text, docs string, needsFFmpeg bool) {
	e, ok := errors.FromError(err)
	if !ok {
		return err.Error(), "", false
	}
	switch e.Tag {
	case "ImageNeedsFFmpeg":
		return e.Message, installDocs, true
	case "FFmpegNotFound", "FFmpegPathInvalid", "FFprobeNotFound", "FFmpegTooOld", "FFmpegVersionFailed":
		return what + " needs FFmpeg. " + e.Message + ".", installDocs, true
	case "FFmpegCodecUnsupported":
		codec, _ := e.Metadata["codec"].(string)
		if codec == "h264" || codec == "hevc" {
			return e.Message + " Fedora's ffmpeg-free leaves it out; see the instructions.", rpmFusionDocs, false
		}
		return e.Message, installDocs, false
	}
	return e.Message, "", false
}

// copyInstallCommand copies the command that installs FFmpeg on this system.
func (x *Extension) copyInstallCommand() {
	cmd, ok := requirements()[0].Install.CommandFor(extension.SystemFamily())
	if !ok {
		return
	}
	if err := x.api.Clipboard().WriteText(cmd.Command); err != nil {
		x.api.Log().Warn("the install command cannot be copied", "err", err)
		return
	}
	x.api.Notifications().Info("Copied: " + cmd.Command)
}

// openDocs opens instructions in the browser.
func (x *Extension) openDocs(uri string) {
	if err := x.api.Workspace().OpenURI(x.ctx, uri); err != nil {
		x.api.Log().Warn("the instructions cannot be opened", "uri", uri, "err", err)
	}
}

// setFFmpegPath asks for the path of ffmpeg and saves it in mediaPreview.ffmpegPath; then reload runs.
func (x *Extension) setFFmpegPath(reload func()) {
	if x.api.Origins().Current() != extension.HostOrigin {
		x.api.Notifications().Info("The path of FFmpeg is a setting of the host and cannot be changed from here.")
		return
	}
	current, _ := extension.SettingValue[string](x.api.Settings(), ffmpeg.PathSetting)
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Path of the ffmpeg program", Value: current,
		Message: "Saved as " + ffmpeg.PathSetting + " in your settings; ffprobe is looked for beside it. Leave it empty to look in PATH.",
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		if err := x.api.Settings().Update(ffmpeg.PathSetting, strings.TrimSpace(value)); err != nil {
			x.api.Log().Warn("the path of FFmpeg cannot be saved", "err", err)
			return
		}
		reload()
	})
	if err != nil {
		x.api.Log().Warn("the path of FFmpeg cannot be asked", "err", err)
	}
}

// problemView is the message of what keeps a file from being shown, with the buttons of the requirement when FFmpeg is missing (Copy
// Install Command, Set Path..., Instructions) and Check Again.
func (x *Extension) problemView(bc widgets.BuildContext, err error, what string, reload func()) widgets.Widget {
	text, docs, needsFFmpeg := problemText(err, what)
	var buttons []widgets.Widget
	if needsFFmpeg {
		if _, ok := requirements()[0].Install.CommandFor(extension.SystemFamily()); ok {
			buttons = append(buttons, ui.NewButton("Copy Install Command", ui.ButtonOpts{OnPressed: x.copyInstallCommand}))
		}
		buttons = append(buttons, ui.NewButton("Set Path...", ui.ButtonOpts{Secondary: true, OnPressed: func() { x.setFFmpegPath(reload) }}))
	}
	if docs != "" {
		buttons = append(buttons, ui.NewButton("Instructions", ui.ButtonOpts{Secondary: true, OnPressed: func() { x.openDocs(docs) }}))
	}
	if needsFFmpeg {
		buttons = append(buttons, ui.NewButton("Check Again", ui.ButtonOpts{Secondary: true, OnPressed: reload}))
	}
	return messageWithButtons(bc, text, buttons)
}
