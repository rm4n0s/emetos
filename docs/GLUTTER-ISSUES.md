# Glutter issues

What Glutter is missing or gets wrong, where Emetos works around it (`// Glutter:` comments), and what the fix in
Glutter would be. An entry is removed when the fix is in the Glutter version Emetos uses; the file is deleted when no
entry is left.

## The semantics finders of gluttertest find nothing in a release build

- **What:** Glutter v1.17.0's `gluttertest.BySemanticsLabel` and `BySemanticsLabelMatching` read
  `RenderObject.DebugSemantics` (`gluttertest/finders.go`, `bySemantics`), which only a debug build keeps. Since 1.17 a
  `go test` without tags is a release build, so in it these finders find nothing: a test that looks for a label fails,
  and one that checks that a label is gone passes without looking.
- **Workaround:** `internal/gui/testutil/testutil.go`, `RequireDebugSemantics`: the tests that use the finders (the window
  buttons of `internal/gui/workbench/titlebar_test.go` and `internal/gui/app/titlebar_test.go`) skip in a release build,
  so they run only in the debug race run (SPEC §15.1).
- **Fix in Glutter:** find by the semantics tree that the `SemanticsOwner` builds and sends to the platform, which a
  release build has too (the nodes and their labels and rects), instead of the debug field of the render object.
