package theme

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"
)

// UserTheme is a theme from the folder of the user's themes.
type UserTheme struct {
	Info
	// Theme is the theme read from the file.
	Theme *Theme
}

// LoadUserThemes reads the themes in a folder: the JSON and .tmTheme files in the root of fsys
// ($XDG_CONFIG_HOME/emetos/themes/), ordered by file name. A file that cannot be read as a theme
// is left out and its error is among the problems; a theme without a type is dark. The error has
// the tag UserThemesUnreadable when the folder cannot be listed.
func LoadUserThemes(fsys fs.FS) ([]UserTheme, []error, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, nil, errors.NewErr("UserThemesUnreadable", err)
	}
	var themes []UserTheme
	var problems []error
	for _, entry := range entries {
		ext := path.Ext(entry.Name())
		if entry.IsDir() || (ext != ".json" && ext != ".tmTheme") {
			continue
		}
		t, err := Load(fsys, entry.Name(), TypeDark)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ext)
		label := t.Name
		if label == "" {
			label = id
		}
		themes = append(themes, UserTheme{Info: Info{ID: id, Label: label, Type: t.Type, Path: entry.Name()}, Theme: t})
	}
	sort.SliceStable(themes, func(i, j int) bool { return themes[i].Path < themes[j].Path })
	return themes, problems, nil
}
