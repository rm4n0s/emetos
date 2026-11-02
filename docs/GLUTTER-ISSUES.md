# Glutter issues worked around in Emetos

Each entry says what Glutter lacks or gets wrong, where Emetos works round it (marked `// Glutter:` in the code) and what the fix in Glutter would be. An entry goes when the fix is in the version of Glutter that Emetos uses; the file goes when no entry is left.

## A list of rows of unknown height cannot be moved to its end without building every row

- **What:** a `ListView` builder with no item extent has to build and lay out every row between the first one and the offset it is moved to (`ScrollController.JumpTo(MaxScrollExtent)`). In a debug build `RenderSliverMultiBoxAdaptor.Insert` also verifies the order of all its children for each insert (`debugVerifyChildOrder`), so the time grows with the square of the rows built: the Debug Console with 100,000 lines needed more than a minute to show its end (500 lines: 90 ms, 4,000 lines: 1.3 s).
- **Where:** `internal/gui/views/debug/consolebody.go` (the list is reversed once it is longer than its view, so that its end is the offset 0, which builds the rows in view only) and `internal/gui/kit/scrollmodel.go` (`ScrollControllerModel.SetReverse`, which turns the position round for the scrollbar). The cost: while the user is scrolled up the text moves with each new line (decision 1263).
- **Fix in Glutter:** a sliver list that can start at an index (or at the end) with estimated extents for the rows before it, as a `scrollToIndex` or an `initialScrollIndex` that does not build the rows it passes; and `debugVerifyChildOrder` should not run per insert (once per layout, or only under an assertion flag).
