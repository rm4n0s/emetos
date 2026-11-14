# Glutter issues

What Glutter is missing or does wrong for Emetos, where Emetos works around it, and what the fix in Glutter would be (CLAUDE.md "Glutter"). An entry is removed when its fix is in the Glutter version Emetos uses; the file is deleted when no entry is left.

The entries below were found measuring the views of Remote Work (SPEC §10.18 "Speed"), which are drawn by Glutter's software renderer on the host: Glutter v1.17.2, `BenchmarkHeadlessFrame` of `internal/gui/app` with `EMETOS_BENCH_VIEW=1920x1080`, a release build, CPU and allocation profiles of it.

## 1. The software renderer makes a coverage mask for every rectangular clip

**What is wrong.** `internal/gpu/soft` turns every clip into a mask: `shapeClip` (`soft.go`), which `OpClipRect` and the clip-rect layers go through, rasterizes the rectangle into a coverage mask of the size of the current clip bounds (`newAccum`, `accum.mask`, `threshold` in `raster.go`) and intersects it with the clip so far (`clip.intersect` in `clip.go`). A widget tree clips with rectangles everywhere (every scrollable, every `ClipRect`, every editor row area, every list), nearly always with whole-pixel edges under a transform that only translates, for which the mask says nothing the bounds do not.

**What it costs.** A frame of a 1920x1080 view of the workbench allocates about 158 MB, of which about 150 MB are these masks (`newAccum` 40%, `accum.mask` 29%, `clip.intersect` 15%, `threshold` 4% of the allocated bytes; Emetos's own copy of the frame is 8 MB), and about 44% of the raster time is making them; the garbage collector then takes about 30% of the frame's CPU. A typed character and a scroll cost about 80 ms at 1920x1080.

**Workaround in Emetos.** None is possible: the clips are Glutter's own (viewports, clip layers). Emetos only lowers how much is redrawn (the next entry).

**The fix in Glutter.** A fast path in `shapeClip` (and the clip-rect layer) for a rectangle whose edges fall on whole pixels after a transform that only translates or scales along the axes, when the clip so far has no mask and the clip is not a difference: intersect the bounds, make no mask. Tried in a copy of v1.17.2 (a `pixelRectClip(st, rect)` used by `OpClipRect` and by `ClipRectLayer` with the hard-edge and anti-alias behaviors): the same frames take about 42 ms instead of 80, with the same pictures. After it, what is left is filling rectangles (`fillPolys`, `compositeRow`: about 65%, which a direct fill of an opaque pixel-aligned rectangle would cut the same way) and `drawImageRect` (the minimap, about 12%).

## 2. A change redraws every picture that was recorded again, not what changed in it

**What is wrong.** Three things add up to "every rebuild redraws the whole layer":

- The damage of an offscreen surface is by the identity of a picture: a layer whose picture was recorded again is damaged as a whole, even when the new picture draws the same operations except for one row of text.
- `RenderDecoratedBox.SetDecoration` (`rendering/proxy_box_paint.go`) compares the decoration with `==`, the identity of the pointer, although `BoxDecoration.Equal` exists: every rebuild of a `DecoratedBox` or `Container` (which make a new decoration each build) marks it for paint, so every rebuild above one repaints the layer it is in.
- A `LayoutBuilder` that lays out again marks its render object for paint, with what it paints directly, whether or not its child changed.

**What it costs.** Before Emetos's own boundaries, every frame of a view redrew 100% of the picture: a pointer moving over the Explorer cost the same 80 ms at 1920x1080 as a scroll. With them a typed character still redraws the whole editor part (75% of a 1280x800 picture) instead of a row.

**Workaround in Emetos.** Each part of the workbench, the menu row and each sash paint into a layer of their own (`widgets.NewRepaintBoundary` in `internal/gui/workbench/workbench.go` `frame`, `internal/gui/workbench/titlebar.go`, `internal/gui/kit/sash.go`), and a sash that is neither hovered nor dragged paints nothing, so a change in one part leaves the pictures of the others alone (`TestAChangeInOnePartOfTheViewRedrawsThatPartOnly` in `internal/gui/app` holds the numbers). These boundaries are right to keep after the fix; what they cannot do is make a change inside a part cheaper than the part.

**The fix in Glutter.** Compare decorations by value in `SetDecoration` (`Equal` where the decoration has one); do not mark a `LayoutBuilder` for paint when its layout gave the same size and the same child; and compute the damage of a picture that was recorded again from the operations that differ from the picture it replaces (or keep the bounds of the operations and compare them one by one, falling back to the whole picture when the lists differ in length), so that a typed character damages its row.
