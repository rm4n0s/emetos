# Glutter issues

Things Glutter is missing or gets wrong, found while building Emetos. Each entry says what is wrong, where Emetos works around it (if it does), and what the fix in Glutter would be. An entry is removed when the fix is in the Glutter version Emetos uses.

## There is no way to rasterize one glyph into an image, so the glyph atlas of the terminal draws paragraphs into a picture and reads it back (Glutter 1.9.2 to 1.9.4)

- **What:** the terminal (`extensions/terminal/atlas.go`, SPEC §10.25 "Speed") draws its grid with `Canvas.DrawAtlas`: each glyph is rasterized once into a sprite sheet and the sheet is drawn with the `BlendModeModulate` mode, so that one draw tints thousands of glyphs. Glutter has no call that rasterizes a glyph (or a run of text) of a font into an image of a given size with the font's hinting and antialiasing; the only way to get the pixels is to lay out a paragraph per glyph, draw them in white on a recording, and read the picture back with `Picture.ToImage` and the software rasterizer of `offscreen`. It works and is fast enough (a glyph is rasterized once per font and style; a page of 512 x 512 px holds several hundred), but it makes a paragraph and a picture for every new glyph, and the metrics of the cell (width, height and baseline) are measured from a paragraph of its own.
- **Where the workaround is:** `extensions/terminal/atlas.go` (`atlas.rasterize`), with the `// Glutter:` comments.
- **What Glutter could provide:** `ParagraphFactory.RasterizeGlyphs(font, size, runes) (image.Image, []GlyphMetrics)` or a `gpu.GlyphAtlas` that exposes the sheets it keeps for text, so that a widget draws its own grid of glyphs from them; and `FontMetrics` of a font at a size (advance of the zero, ascent, descent, line gap) without a paragraph.

## A pane that has the focus and leaves the tree is not told that it lost it (Glutter 1.9.2 to 1.9.4)

- **What:** when a widget that has the keyboard focus is unmounted (the terminal's pane of a tab that another tab replaces), the `Focus` widget's `OnFocusChange` is not called with false, so a widget that keeps its own "focused" flag (the terminal draws a filled cursor while it has the focus and an outline otherwise) stays in the state it had. Flutter's `Focus` calls `onFocusChange` from the node's listener, and the node leaves the scope without notifying when it is detached with the tree.
- **Where the workaround is:** `paneView.unmounted` in `extensions/terminal/pane.go` clears the flag itself.
- **What Glutter could provide:** the focus node notifies its listeners with `hasFocus` false when it is detached from the focus tree while it has the focus.

## The clipboard carries plain text only (Glutter 1.9.4)

- **What:** `services.Clipboard` reads and writes one string (`text/plain`); there is no way to put a second representation such as `text/html` on the clipboard. The terminal's **Copy Output as HTML** (step 17d.5, `workbench.action.terminal.copyLastCommandOutput` and the decoration's menu) is meant to give a rich editor formatted text with the colors of the terminal; with plain text only, the HTML markup itself is what is pasted.
- **Where the workaround is:** `paneView.outputHTML` and `paneView.copyText` in `extensions/terminal/shell_pane.go` (the markup is copied as text, with a `// Glutter:` comment).
- **What Glutter could provide:** `Clipboard.SetData(map[mimeType][]byte)` (and a matching read), with the Wayland data offer and the X11 selection offering every type.
