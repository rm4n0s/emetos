package pdf

import (
	"context"
	"os"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// What the PDF Viewer needs from the system (SPEC §10.36, §10.37 "PDF", D54): Poppler's programs, 22.02 or newer. Only "pdfinfo -v"
// is run.

// RequirementPoppler is the id of the requirement of Poppler.
const RequirementPoppler = "poppler"

// installDocs are Poppler's instructions.
const installDocs = "https://poppler.freedesktop.org"

// requirements are the requirements of the manifest.
func requirements() []extension.Requirement {
	return []extension.Requirement{{
		ID: RequirementPoppler, Name: "Poppler", Kind: extension.RequirementProgram,
		Purpose: "draws the pages, finds text and reads the outline (Poppler " + poppler.MinVersion + " or newer: pdfinfo, pdftoppm, pdftotext and pdftohtml)",
		Install: extension.Install{Docs: installDocs, Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install poppler-utils"},
			{Family: extension.FamilyDebian, Command: "sudo apt install poppler-utils"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S poppler"},
			{Family: extension.FamilySuse, Command: "sudo zypper install poppler-tools"},
			{Family: extension.FamilyMacOS, Command: "brew install poppler"},
		}},
		PathSetting: poppler.PathSetting, Check: checkPopplerIn(nil),
	}}
}

// checkPopplerIn is the check of Poppler, which looks in PATH and then in the extra folders (poppler.HomebrewDirs when nil).
func checkPopplerIn(extra []string) func(context.Context, extension.RequirementEnv) extension.RequirementResult {
	return func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
		p, err := poppler.Find(ctx, poppler.FindOptions{Setting: env.StringSetting(poppler.PathSetting), Path: os.Getenv("PATH"), Extra: extra})
		if err == nil {
			detail := ""
			if p.FromSetting {
				detail = "found through " + poppler.PathSetting
			}
			return extension.RequirementResult{State: extension.RequirementFound, Path: p.Path(poppler.ProgramInfo), Version: p.Version, Detail: detail}
		}
		if ctx.Err() != nil {
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
		}
		appErr, _ := errors.FromError(err)
		tag, msg := "", err.Error()
		if appErr != nil {
			tag, msg = appErr.Tag, appErr.Message
		}
		path := ""
		if p.Dir != "" {
			path = p.Path(poppler.ProgramInfo)
		}
		switch tag {
		case "PopplerNotFound":
			return extension.RequirementResult{State: extension.RequirementMissing, Detail: msg}
		case "PopplerTooOld":
			return extension.RequirementResult{State: extension.RequirementUnusable, Path: path, Version: p.Version, Detail: msg}
		}
		// a setting that names no folder with pdfinfo, a program missing beside it, a version that cannot be asked
		return extension.RequirementResult{State: extension.RequirementUnusable, Path: path, Detail: msg}
	}
}
