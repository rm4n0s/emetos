// Package languagetest makes the registry of the bundled languages for tests. The language packs are extensions
// (extensions/, SPEC §10.15) that no package under internal/ may import, so a test reads their files from the source tree
// (package packfs) and adds them to a registry in the order the editor does.
package languagetest

import (
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/language/packfs"
)

// Bundled makes a registry of its own that has every bundled language: Go, JSON, JSON with Comments, JSON Lines, Code
// Snippets, YAML, Markdown, Python, JavaScript, JavaScript React, TypeScript, TypeScript React, and the rest of SPEC §8.1.
// The error has the tags of packfs.Open and of language.Registry.AddExtension.
func Bundled() (*language.Registry, error) {
	r := language.NewRegistry()
	list, err := packfs.All()
	if err != nil {
		return nil, err
	}
	for _, fsys := range list {
		if err := r.AddExtension(fsys); err != nil {
			return nil, err
		}
	}
	return r, nil
}
