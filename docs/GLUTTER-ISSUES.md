# Glutter issues

What Emetos needs from Glutter and Glutter doesn't have yet, and the workarounds Emetos uses meanwhile (CLAUDE.md "Glutter", SPEC D8). Each entry says what is missing, where Emetos works around it, and what the fix in Glutter would be. Remove an entry when the fix is in a Glutter release that Emetos uses.

Emetos uses Glutter **v1.4.0**.

## Open

None.

## Fixed

- Glutter v1.2.0–v1.3.0 (Glutter steps 22.1–22.8): fonts loaded at run time, fonts in `gluttertest`, fallback through the listed families in order, font queries and the desktop's font settings, text benchmarks and an LRU glyph cache, `TreeSliver`, `ReorderableListView`.
- Glutter v1.4.0 (Glutter steps 22.9–22.11): tab stops in paragraph layout (`ParagraphStyleOpts.TabSize`), the cache of shaped runs and of text analysis.

## Workarounds in the code

None yet. Each `// Glutter:` comment in Emetos gets an entry here.
