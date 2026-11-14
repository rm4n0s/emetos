package mediapreview

import (
	"context"
	"os"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
)

// What Media Preview needs from the system (SPEC §10.36, §10.37 "Video", D53): FFmpeg 4.4 or newer, for videos and AVIF and HEIC
// images; images of the other formats need nothing. Only "ffmpeg -version" is run.

// RequirementFFmpeg is the id of the requirement of FFmpeg.
const RequirementFFmpeg = "ffmpeg"

// requirements are the requirements of the manifest.
func requirements() []extension.Requirement {
	return []extension.Requirement{{
		ID: RequirementFFmpeg, Name: "FFmpeg", Kind: extension.RequirementProgram, Optional: true,
		Purpose: "plays videos and opens AVIF and HEIC images (FFmpeg " + ffmpeg.MinVersion + " or newer, with ffprobe beside it)",
		Install: extension.Install{Docs: "https://ffmpeg.org/download.html", Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install ffmpeg-free"},
			{Family: extension.FamilyDebian, Command: "sudo apt install ffmpeg"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S ffmpeg"},
			{Family: extension.FamilySuse, Command: "sudo zypper install ffmpeg"},
			{Family: extension.FamilyMacOS, Command: "brew install ffmpeg"},
		}},
		PathSetting: ffmpeg.PathSetting, Check: checkFFmpegIn(nil),
	}}
}

// checkFFmpegIn is the check of FFmpeg, which looks in PATH and then in the extra folders (ffmpeg.HomebrewDirs when nil).
func checkFFmpegIn(extra []string) func(context.Context, extension.RequirementEnv) extension.RequirementResult {
	return func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
		p, err := ffmpeg.Find(ctx, ffmpeg.FindOptions{Setting: env.StringSetting(ffmpeg.PathSetting), Path: os.Getenv("PATH"), Extra: extra})
		if err == nil {
			detail := ""
			if p.FromSetting {
				detail = "found through " + ffmpeg.PathSetting
			}
			return extension.RequirementResult{State: extension.RequirementFound, Path: p.FFmpeg, Version: p.Version, Detail: detail}
		}
		if ctx.Err() != nil {
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
		}
		appErr, _ := errors.FromError(err)
		tag, msg := "", err.Error()
		if appErr != nil {
			tag, msg = appErr.Tag, appErr.Message
		}
		switch tag {
		case "FFmpegNotFound":
			return extension.RequirementResult{State: extension.RequirementMissing, Detail: msg}
		case "FFmpegTooOld":
			return extension.RequirementResult{State: extension.RequirementUnusable, Path: p.FFmpeg, Version: p.Version, Detail: msg}
		}
		// a setting that names nothing, no ffprobe beside ffmpeg, a version that cannot be asked
		return extension.RequirementResult{State: extension.RequirementUnusable, Path: p.FFmpeg, Detail: msg}
	}
}
