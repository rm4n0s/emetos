package terminal

import (
	gui "github.com/rm4n0s/glutter/ui"
)

// fontCache keeps the atlases of the fonts the terminals of one window tab use, so that two panes with the same font share the
// glyphs they rasterized. It keeps the last few and drops the others when a setting changes the font. UI thread only.
type fontCache struct {
	atlases []*fontEntry
}

type fontEntry struct {
	spec  fontSpec
	atlas *atlas
	decor *decor
}

// maxFonts is how many fonts the cache keeps.
const maxFonts = 4

// get returns the atlas and the decoration atlas of a font, making them the first time.
func (c *fontCache) get(pd *gui.PlatformDispatcher, fs fontSpec) (*atlas, *decor) {
	for i, e := range c.atlases {
		if e.spec == fs {
			if i != 0 {
				c.atlases[0], c.atlases[i] = c.atlases[i], c.atlases[0]
			}
			return e.atlas, e.decor
		}
	}
	a := newAtlas(pd, fs)
	e := &fontEntry{spec: fs, atlas: a, decor: newDecor(a.m)}
	c.atlases = append([]*fontEntry{e}, c.atlases...)
	if len(c.atlases) > maxFonts {
		c.atlases = c.atlases[:maxFonts]
	}
	return a, e.decor
}
