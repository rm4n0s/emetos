// Package langpack is what the language packs of Emetos share (SPEC §8.1, §10.15): an extension whose only contribution is
// languages, declared as the package.json of a VS Code extension and the files it names, in an embed.FS of the pack.
//
// A pack is
//
//	//go:embed all:assets
//	var assets embed.FS
//
//	// New is the Factory of the pack.
//	func New() extension.Extension {
//		return langpack.New(langpack.Spec{ID: "emetos.golang", Name: "Go Language Basics", Description: "...", Assets: assets})
//	}
//
// The languages need no activation (Emetos adds them from the manifest, before any file opens). The pack still declares
// onStartup, so that the Extensions page shows it as active in every tab where it is on.
package langpack

import (
	"context"
	"io/fs"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// AssetsDir is the folder, in the embedded file system of a pack, that holds the package.json of the pack and its files
// (extensions/<pack>/assets/, written by scripts/update-assets.sh).
const AssetsDir = "assets"

// Spec describes a language pack.
type Spec struct {
	// ID is the id of the extension: emetos.<pack>.
	ID string
	// Name is the name shown to the user.
	Name string
	// Description says in one sentence which languages the pack has.
	Description string
	// Assets is the embedded file system of the pack; the package.json is in its AssetsDir folder.
	Assets fs.FS
	// Dir is the folder of Assets that holds the package.json; AssetsDir when empty. A test sets it.
	Dir string
}

// pack is the extension of a language pack.
type pack struct {
	spec Spec
	// root is Assets rooted at the folder, or nil when the folder is not a valid path (err says why).
	root fs.FS
	err  error
}

// New makes the extension of a language pack. Every call makes an extension of its own, as the contract asks. A pack
// whose folder is not a path has no languages, and its Activate fails with the tag LanguagePackAssetsInvalid, so that
// the Extensions page shows why.
func New(spec Spec) extension.Extension {
	dir := spec.Dir
	if dir == "" {
		dir = AssetsDir
	}
	root, err := fs.Sub(spec.Assets, dir)
	if err != nil {
		return &pack{spec: spec, err: errors.NewErr("LanguagePackAssetsInvalid", err).SetMetadata("extension", spec.ID, "dir", dir)}
	}
	return &pack{spec: spec, root: root}
}

// Manifest implements extension.Extension.
func (p *pack) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: p.spec.ID, Name: p.spec.Name, Version: "0.1.0", Description: p.spec.Description,
		ActivationEvents: []string{extension.OnStartup},
		Contributes:      extension.Contributes{Languages: p.root},
	}
}

// Activate implements extension.Extension: there is nothing to start, the languages are the manifest's.
func (p *pack) Activate(context.Context, extension.API) error { return p.err }
