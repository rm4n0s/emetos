package theme

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"strings"

	"github.com/rm4n0s/errors"
)

// assets holds the themes that Emetos bundles with the manifest of the extension they come from,
// as scripts/update-assets.sh copies them (SPEC §8.4, §17).
//
//go:embed all:assets
var assets embed.FS

// DefaultDark and DefaultLight are the names of the themes that are used when no theme is chosen.
const (
	// DefaultDark is the default theme.
	DefaultDark = "Dark Modern"
	// DefaultLight is the default theme of a light desktop (window.autoDetectColorScheme).
	DefaultLight = "Light Modern"
)

// offered are the ids of the bundled themes that are offered. The files of the two Visual Studio
// themes are bundled too, as Dark+ and Light+ include them, but they are not offered.
var offered = map[string]bool{
	"Dark Modern": true, "Light Modern": true, "Dark+": true, "Light+": true,
	"Default High Contrast": true, "Default High Contrast Light": true,
}

// Info describes a theme that can be loaded.
type Info struct {
	// ID is the id the theme has in its manifest (for a user theme, its file name without the
	// extension).
	ID string
	// Label is the name to show: "Dark Modern", "Dark+", "Light High Contrast" ...
	Label string
	// Type is the type of the theme.
	Type Type
	// Path is the file of the theme in its file system.
	Path string
	// Bundled tells whether the theme comes with Emetos.
	Bundled bool
}

// Bundled lists the themes that come with Emetos: Dark Modern, Light Modern, Dark+, Light+, Dark
// High Contrast and Light High Contrast, in the order of the manifest of VS Code's default
// themes. The error has the tag BundledThemesInvalid when the manifest cannot be read.
func Bundled() ([]Info, error) {
	data, err := fs.ReadFile(assets, "assets/package.json")
	if err != nil {
		return nil, errors.NewErr("BundledThemesInvalid", err).SetMetadata("file", "package.json")
	}
	var manifest struct {
		Contributes struct {
			Themes []struct {
				ID, Label, UITheme, Path string
			} `json:"themes"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, errors.NewErr("BundledThemesInvalid", err).SetMetadata("file", "package.json")
	}
	names := map[string]string{}
	if nls, err := fs.ReadFile(assets, "assets/package.nls.json"); err == nil {
		if err := json.Unmarshal(nls, &names); err != nil {
			return nil, errors.NewErr("BundledThemesInvalid", err).SetMetadata("file", "package.nls.json")
		}
	}
	var out []Info
	for _, t := range manifest.Contributes.Themes {
		file := path.Join("assets", t.Path)
		if _, err := fs.Stat(assets, file); err != nil || !offered[t.ID] {
			continue // a theme that is not bundled or not offered
		}
		typ, ok := ParseType(t.UITheme)
		if !ok {
			return nil, errors.New("BundledThemesInvalid", "a theme has an unknown uiTheme", "theme", t.ID, "uiTheme", t.UITheme)
		}
		label := t.Label
		if key := strings.Trim(label, "%"); key != label {
			if name, ok := names[key]; ok {
				label = name
			}
		}
		out = append(out, Info{ID: t.ID, Label: label, Type: typ, Path: file, Bundled: true})
	}
	return out, nil
}

// LoadBundled loads a bundled theme by its label or its id.
func LoadBundled(name string) (*Theme, error) {
	list, err := Bundled()
	if err != nil {
		return nil, err
	}
	for _, info := range list {
		if info.Label == name || info.ID == name {
			return Load(assets, info.Path, info.Type)
		}
	}
	return nil, errors.New("BundledThemeNotFound", "no bundled theme has this name", "name", name)
}
