# Glutter issues

Things Glutter lacks or gets wrong, with the workaround Emetos uses and the fix Glutter should make. Remove an entry when the fix is in the Glutter version Emetos uses; delete this file when no entry is left (CLAUDE.md "Glutter").

## `SliverVariedExtentList` is linear in the number of rows (found in step 24.4, Glutter 1.9.7)

- **What is wrong.** `RenderSliverFixedExtentBoxAdaptor` (the base of `RenderSliverVariedExtentList`) finds the offset of a row, the first row in view and the extent of the list by adding up the heights the item extent builder gives, one call for each row before it: `IndexToLayoutOffset` for every child that is laid out, `GetMinChildIndexForScrollOffset` and `ComputeMaxScrollOffset` once. A list of 100,000 rows of different heights (the Source Control view: the input and the button are taller than the files) takes 40 to 60 ms for a frame that jumps to its end, and the extent of the list is an estimate from the rows built until the last one is laid out, so a jump to the end stops short of it.
- **Where the workaround is.** `internal/gui/kit/sliverrows.go` (`sliverRows`, `renderSliverRows`, `exactExtentDelegate`), used by `kit.List` when the model has `HeightOf`. It uses the hooks `FixedExtentLayoutOverrides` that the base has for a carousel, answered from the tops of the rows that the model keeps, with a binary search, and a delegate that tells the exact extent.
- **What Glutter should provide.** A varied extent list that keeps a prefix sum of the extents (or takes the tops from its owner), so that the offset of a row and the row at an offset take constant time and a binary search, and an exact scroll extent when the owner knows it.
