package workbench

import (
	"path"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/settings"
)

// TitleDirty is the indicator `${dirty}` stands for when the active editor has unsaved changes.
const TitleDirty = "● "

// TitleInput is what the title of the window is made of (SPEC §10.1): the template and the facts
// the variables of the template stand for. The variables that need a remote, a workspace file, a
// repository or a screen reader (remoteName, activeRepositoryName, activeEditorState...) are empty,
// as are the ones whose facts are not given.
type TitleInput struct {
	// Template is the value of window.title and Separator that of window.titleSeparator.
	Template, Separator string
	// AppName is `${appName}`: "Emetos".
	AppName string
	// ProfileName is `${profileName}`: the name of the profile, empty for the default one.
	ProfileName string
	// Home is the home folder of the user, which paths in it are written relative to as "~/...".
	Home string
	// Folder is the folder open in the window, or "".
	Folder string
	// ActiveFile is the path of the file of the active editor, or "" when there is none or it is not a
	// file.
	ActiveFile string
	// ActiveName is the name of the active editor when it is not a file ("Untitled-1"); the name of the
	// file is used when ActiveFile is set.
	ActiveName string
	// LanguageID is the language of the active editor.
	LanguageID string
	// Dirty says that the active editor has unsaved changes.
	Dirty bool
	// FocusedView is the name of the view that has the focus.
	FocusedView string
}

// Title expands the template with the facts: VS Code's `getWindowTitle`. A variable the template
// names that is not known is left out, and so is `${separator}` where it does not have text on
// both sides. A title that comes out empty is the name of the application.
//
// Ported from VS Code: src/vs/workbench/browser/parts/titlebar/windowTitle.ts (getWindowTitle,
// getFullWindowTitle)
func (in TitleInput) Title() string {
	template := in.Template
	if template == "" {
		template = settings.DefaultWindowTitle
	}
	separator := in.Separator
	title := ExpandTemplate(template, in.variables(), separator)
	// non-space white space (a new line in a file name) is a space
	title = strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', ' ', ' ':
			return ' '
		}
		return r
	}, title)
	if strings.TrimSpace(title) == "" {
		return in.AppName
	}
	return title
}

// variables are the values of the variables of the title.
func (in TitleInput) variables() map[string]string {
	activeShort := in.ActiveName
	activeMedium, activeLong := activeShort, activeShort
	var folderShort, folderMedium, folderLong string
	if in.ActiveFile != "" {
		file := path.Clean(in.ActiveFile)
		activeShort = path.Base(file)
		activeMedium = in.label(file, true)
		activeLong = in.label(file, false)
		if dir := path.Dir(file); dir != "." {
			folderShort = path.Base(dir)
			folderMedium = in.label(dir, true)
			folderLong = in.label(dir, false)
		}
	}
	var rootName, rootPath, folderName, folderPath string
	if in.Folder != "" {
		root := path.Clean(in.Folder)
		rootName = path.Base(root)
		rootPath = in.label(root, false)
		folderName = rootName
		folderPath = rootPath
	}
	dirty := ""
	if in.Dirty {
		dirty = TitleDirty
	}
	return map[string]string{
		"activeEditorShort":      activeShort,
		"activeEditorMedium":     activeMedium,
		"activeEditorLong":       activeLong,
		"activeEditorLanguageId": in.LanguageID,
		"activeFolderShort":      folderShort,
		"activeFolderMedium":     folderMedium,
		"activeFolderLong":       folderLong,
		"rootName":               rootName,
		"rootNameShort":          rootName,
		"rootPath":               rootPath,
		"folderName":             folderName,
		"folderPath":             folderPath,
		"dirty":                  dirty,
		"appName":                in.AppName,
		"profileName":            in.ProfileName,
		"focusedView":            in.FocusedView,
		"remoteName":             "",
		"activeEditorState":      "",
	}
}

// label is a path as the label service writes it: relative to the folder of the window when it is
// asked for and the path is inside the folder (the folder itself is empty), else the whole path with
// the home folder as "~".
//
// Ported from VS Code: src/vs/base/common/labels.ts (getPathLabel, tildify)
func (in TitleInput) label(p string, relative bool) string {
	if relative && in.Folder != "" {
		root := path.Clean(in.Folder)
		if p == root {
			return ""
		}
		if rel, ok := strings.CutPrefix(p, strings.TrimSuffix(root, "/")+"/"); ok {
			return rel
		}
	}
	return tildify(p, in.Home)
}

// tildify writes a path in the home folder as "~/...".
func tildify(p, home string) string {
	if p == "" || home == "" {
		return p
	}
	home = strings.TrimRight(home, "/") + "/"
	if rest, ok := strings.CutPrefix(p, home); ok {
		return "~/" + rest
	}
	return p
}

// ExpandTemplate fills the `${name}` variables of a template with values, and `${separator}` with
// the separator where there is text on both sides of it (and not twice in a row). A name without a
// value is left out. It is VS Code's `template`.
//
// Ported from VS Code: src/vs/base/common/labels.ts (template)
func ExpandTemplate(template string, values map[string]string, separator string) string {
	type segment struct {
		value string
		kind  int // 0 text, 1 variable, 2 separator
	}
	const (
		text = iota
		variable
		sep
	)
	var segments []segment
	inVariable := false
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			segments = append(segments, segment{cur.String(), text})
		}
		cur.Reset()
	}
	for _, r := range template {
		switch {
		case r == '$' || (inVariable && r == '{'):
			// beginning of a variable
			flush()
			inVariable = true
		case r == '}' && inVariable:
			name := cur.String()
			if name == "separator" {
				if n := len(segments); n == 0 || segments[n-1].kind != sep {
					segments = append(segments, segment{separator, sep})
				}
			} else if v := values[name]; v != "" {
				segments = append(segments, segment{v, variable})
			}
			cur.Reset()
			inVariable = false
		default:
			cur.WriteRune(r)
		}
	}
	if !inVariable {
		flush()
	}
	var out strings.Builder
	for i, s := range segments {
		if s.kind == sep {
			// a separator stays when there is text or a variable with a value on both sides
			if i == 0 || i == len(segments)-1 {
				continue
			}
			l, r := segments[i-1], segments[i+1]
			if l.kind == sep || r.kind == sep || l.value == "" || r.value == "" {
				continue
			}
		}
		out.WriteString(s.value)
	}
	return out.String()
}
