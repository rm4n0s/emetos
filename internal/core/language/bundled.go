package language

import (
	"embed"
	"io/fs"

	"github.com/rm4n0s/errors"
)

// assets holds the VS Code extensions that Emetos bundles, one folder each, as
// scripts/update-assets.sh copies them (SPEC §8.1, §17).
//
//go:embed all:assets
var assets embed.FS

// bundledExtensions are the folders of assets that Bundled adds, in the order of registration.
var bundledExtensions = []string{"go", "json", "yaml", "markdown-basics", "python", "javascript", "typescript-basics",
	"html", "css", "scss", "less", "xml", "sql", "shellscript", "make", "docker", "ini"}

// Bundled makes a registry of the languages that Emetos carries: Go, JSON, JSON with Comments,
// JSON Lines, Code Snippets, YAML, Markdown, Python, JavaScript, JavaScript React, TypeScript and
// TypeScript React. Every call makes a registry of its own. The
// error has the tag BundledExtensionUnreadable when an extension of the assets cannot be read.
func Bundled() (*Registry, error) {
	r := NewRegistry()
	if err := addExtensions(r, assets, bundledExtensions); err != nil {
		return nil, err
	}
	return r, nil
}

// addExtensions adds the extensions in the folders of fsys, named "assets/<name>", to r.
func addExtensions(r *Registry, fsys fs.FS, names []string) error {
	for _, name := range names {
		sub, err := fs.Sub(fsys, "assets/"+name)
		if err != nil {
			return errors.NewErr("BundledExtensionUnreadable", err).SetMetadata("extension", name)
		}
		if err := r.AddExtension(sub); err != nil {
			return err
		}
	}
	return nil
}
