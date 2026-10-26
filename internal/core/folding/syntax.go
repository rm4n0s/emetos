package folding

import "sort"

// Ported from VS Code: src/vs/editor/contrib/folding/browser/syntaxRangeProvider.ts (sanitizeRanges, RangesCollector)

// SyntaxRange is a folding range a language server or an extension found: the first and the last line (zero-based) of the
// lines it folds, the first line staying visible, its kind ("comment", "imports", "region" or empty) and the rank of the
// provider that gave it (the lower the rank the earlier the provider, which wins when two ranges start on one line).
type SyntaxRange struct {
	Start, End int
	Kind       string
	Rank       int
}

// Sanitize makes the regions of ranges that may overlap, repeat or cross: they are sorted by their first line (then by rank)
// and a range that crosses the one before it is dropped (VS Code keeps it), so that the regions are nested, as the model requires. When there
// are more than the limit allows, the outermost ones are kept (all the regions of a nesting level or none). It fails only when
// the regions cannot be made, which the ranges kept prevent.
func Sanitize(ranges []SyntaxRange, limit Limit) (*Regions, error) {
	sorted := append([]SyntaxRange(nil), ranges...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].Rank < sorted[j].Rank
	})
	c := &syntaxCollector{limit: limit}
	var top *SyntaxRange
	var previous []*SyntaxRange
	for i := range sorted {
		entry := &sorted[i]
		if top == nil {
			top = entry
			c.add(entry, len(previous))
			continue
		}
		if entry.Start <= top.Start {
			continue
		}
		if entry.End <= top.End {
			previous = append(previous, top)
			top = entry
			c.add(entry, len(previous))
			continue
		}
		if entry.Start <= top.End {
			continue // it crosses the range above it: VS Code adds it all the same, the model needs nested regions
		}
		// it starts after the range above it: that range, and the ones that end before the entry, are closed
		for {
			top = nil
			if n := len(previous); n > 0 {
				top, previous = previous[n-1], previous[:n-1]
			}
			if top == nil || entry.Start <= top.End {
				break
			}
		}
		if top != nil {
			previous = append(previous, top)
		}
		top = entry
		c.add(entry, len(previous))
	}
	return c.regions()
}

// syntaxCollector gathers the regions in the order of their first lines.
type syntaxCollector struct {
	limit  Limit
	starts []int
	ends   []int
	levels []int
	kinds  []string
	// perLevel counts the regions of each nesting level below 30.
	perLevel []int
}

func (c *syntaxCollector) add(r *SyntaxRange, level int) {
	if r.Start > MaxLine || r.End > MaxLine {
		return
	}
	c.starts, c.ends, c.levels, c.kinds = append(c.starts, r.Start), append(c.ends, r.End), append(c.levels, level), append(c.kinds, r.Kind)
	if level < 30 {
		for len(c.perLevel) <= level {
			c.perLevel = append(c.perLevel, 0)
		}
		c.perLevel[level]++
	}
}

func (c *syntaxCollector) regions() (*Regions, error) {
	limit := c.limit.max()
	n := len(c.starts)
	if n <= limit {
		if c.limit.Update != nil {
			c.limit.Update(n, 0)
		}
		return c.build(c.starts, c.ends, c.kinds)
	}
	if c.limit.Update != nil {
		c.limit.Update(n, limit)
	}
	entries, maxLevel := 0, len(c.perLevel)
	for level, count := range c.perLevel {
		if count > 0 {
			if count+entries > limit {
				maxLevel = level
				break
			}
			entries += count
		}
	}
	var starts, ends []int
	var kinds []string
	for i := range c.starts {
		level := c.levels[i]
		if level < maxLevel || (level == maxLevel && entries < limit) {
			if level == maxLevel {
				entries++
			}
			starts, ends, kinds = append(starts, c.starts[i]), append(ends, c.ends[i]), append(kinds, c.kinds[i])
		}
	}
	return c.build(starts, ends, kinds)
}

// build makes the regions; the kinds are kept only when a region has one.
func (c *syntaxCollector) build(starts, ends []int, kinds []string) (*Regions, error) {
	typed := false
	for _, k := range kinds {
		typed = typed || k != ""
	}
	if !typed {
		kinds = nil
	}
	return NewRegions(starts, ends, kinds)
}
