# Glutter issues

What Emetos needs from Glutter and Glutter doesn't have yet, and the workarounds Emetos uses meanwhile (CLAUDE.md "Glutter", SPEC D8). Each entry says what is missing, where Emetos works around it, and what the fix in Glutter would be. Remove an entry when the fix is in a Glutter release that Emetos uses.

Emetos uses Glutter **v1.3.0**.

## Open

### 1. Tab stops in paragraph layout
- **Missing:** `\t` has no tab-stop advance in `internal/text/layout` (it is only hanging or justified whitespace).
- **Workaround:** Emetos expands tabs to spaces in the paragraph text and maps offsets (SPEC §9.2). This is how editors usually do it, so this item is low priority.
- **Fix in Glutter:** a tab-stop width in `ParagraphStyle` (Flutter has none either).

### 2. A cache of shaped runs
- **Missing:** building a paragraph shapes every span again (65–96 µs for a code line, Glutter's `docs/PERFORMANCE.md`, step 22.6, where the cache is listed as pending).
- **Workaround:** Emetos caches whole paragraphs per line (SPEC §9.2), so only changed lines are built again.
- **Fix in Glutter:** cache shaped runs keyed by text, font and size, as Glutter's PERFORMANCE.md describes. Needed only if Emetos step 3.6 misses its targets.

## Fixed

Fixed in Glutter v1.2.0–v1.3.0 (Glutter steps 22.1–22.8): fonts loaded at run time, fonts in `gluttertest`, fallback through the listed families in order, font queries and the desktop's font settings, text benchmarks and an LRU glyph cache, `TreeSliver`, `ReorderableListView`.

## Workarounds in the code

None yet. Each `// Glutter:` comment in Emetos gets an entry here.
