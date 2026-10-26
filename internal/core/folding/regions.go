// Package folding is the model of the folding of a document (SPEC §8.7): the folding regions that
// a provider finds, the indentation provider, the regions that are folded as decorations of the
// document, and the commands that fold and unfold them.
package folding

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/editor/contrib/folding/browser/foldingRanges.ts and foldingModel.ts (MIT)

// Limits of the regions, VS Code's.
const (
	// MaxRegions is the most regions a document has.
	MaxRegions = 0xFFFF
	// MaxLine is the largest line number (zero-based) a region may have.
	MaxLine = 0xFFFFFF
	// DefaultLimit is the default of editor.foldingMaximumRegions.
	DefaultLimit = 5000
)

// Source says where a region came from.
type Source int

// The sources of a region.
const (
	// FromProvider is a region the provider found.
	FromProvider Source = iota
	// UserDefined is a region the user made from a selection.
	UserDefined
	// Recovered is a folded region that the provider no longer finds, kept until it does.
	Recovered
)

// abbreviation is the letter of a source in the text form of the regions.
func (s Source) abbreviation() string {
	switch s {
	case UserDefined:
		return "u"
	case Recovered:
		return "r"
	}
	return " "
}

// Range is a region described by its lines (zero-based, both included), as the providers and the
// merge give them.
type Range struct {
	// Start and End are the first and the last line of the region.
	Start, End int
	// Type is the kind of the region ("comment", "region", "imports"), or "".
	Type string
	// Collapsed says that the region is folded.
	Collapsed bool
	// Source says where the region came from.
	Source Source
}

// Regions is the list of the folding regions of a text, sorted by their first line and nested
// (VS Code's FoldingRegions). A region that starts inside another is inside it.
type Regions struct {
	starts, ends []int32
	parents      []int32 // computed when asked, -1 for a region at the top
	collapsed    []bool
	user         []bool
	recovered    []bool
	types        []string // nil when no region has a type
	// collapsedTexts are the texts that stand for the lines of a folded region, by the region's first line (a language server
	// may give one); nil for none.
	collapsedTexts map[int]string
}

// SetCollapsedTexts gives the regions the texts that a folded region shows in place of its lines, by the first line of the
// region.
func (r *Regions) SetCollapsedTexts(texts map[int]string) {
	if len(texts) == 0 {
		r.collapsedTexts = nil
		return
	}
	r.collapsedTexts = texts
}

// CollapsedTexts are the texts of SetCollapsedTexts; nil for none. The map is not to be changed.
func (r *Regions) CollapsedTexts() map[int]string { return r.collapsedTexts }

// NewRegions makes regions from their first and last lines (zero-based). The starts must be in
// ascending order and the regions nested; types may be nil.
func NewRegions(starts, ends []int, types []string) (*Regions, error) {
	if len(starts) != len(ends) || len(starts) > MaxRegions || (types != nil && len(types) != len(starts)) {
		return nil, errors.New("FoldingInvalidRegions", "the starts, ends and types of the regions do not agree, or there are too many",
			"starts", len(starts), "ends", len(ends), "types", len(types), "max", MaxRegions)
	}
	r := &Regions{
		starts: make([]int32, len(starts)), ends: make([]int32, len(ends)),
		collapsed: make([]bool, len(starts)), user: make([]bool, len(starts)), recovered: make([]bool, len(starts)),
		types: types,
	}
	for i := range starts {
		if starts[i] < 0 || ends[i] < 0 || starts[i] > MaxLine || ends[i] > MaxLine {
			return nil, errors.New("FoldingLineOutOfRange", "a line of a region is out of range", "start", starts[i], "end", ends[i], "max", MaxLine)
		}
		r.starts[i], r.ends[i] = int32(starts[i]), int32(ends[i])
	}
	return r, nil
}

func (r *Regions) ensureParents() {
	if r.parents != nil {
		return
	}
	r.parents = make([]int32, len(r.starts))
	var stack []int32
	for i := range r.starts {
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if r.starts[top] <= r.starts[i] && r.ends[top] >= r.ends[i] {
				break
			}
			stack = stack[:len(stack)-1]
		}
		r.parents[i] = -1
		if len(stack) > 0 {
			r.parents[i] = stack[len(stack)-1]
		}
		stack = append(stack, int32(i))
	}
}

// Len is the number of regions.
func (r *Regions) Len() int { return len(r.starts) }

// Start is the first line of a region.
func (r *Regions) Start(i int) int { return int(r.starts[i]) }

// End is the last line of a region.
func (r *Regions) End(i int) int { return int(r.ends[i]) }

// Type is the kind of a region, or "".
func (r *Regions) Type(i int) string {
	if r.types == nil {
		return ""
	}
	return r.types[i]
}

// HasTypes reports whether the regions have kinds.
func (r *Regions) HasTypes() bool { return r.types != nil }

// Collapsed reports whether a region is folded.
func (r *Regions) Collapsed(i int) bool { return r.collapsed[i] }

// SetCollapsed folds or unfolds a region.
func (r *Regions) SetCollapsed(i int, collapsed bool) { r.collapsed[i] = collapsed }

// Source says where a region came from.
func (r *Regions) Source(i int) Source {
	switch {
	case r.user[i]:
		return UserDefined
	case r.recovered[i]:
		return Recovered
	}
	return FromProvider
}

// SetSource sets where a region came from.
func (r *Regions) SetSource(i int, s Source) {
	r.user[i], r.recovered[i] = s == UserDefined, s == Recovered
}

// SetCollapsedAllOfType folds or unfolds the regions of a kind; it reports whether there were any.
func (r *Regions) SetCollapsedAllOfType(typ string, collapsed bool) bool {
	changed := false
	for i, t := range r.types {
		if t == typ {
			r.collapsed[i] = collapsed
			changed = true
		}
	}
	return changed
}

// Parent is the index of the region a region is inside of, or -1.
func (r *Regions) Parent(i int) int {
	r.ensureParents()
	return int(r.parents[i])
}

// Contains reports whether a region has a line.
func (r *Regions) Contains(i, line int) bool {
	return int(r.starts[i]) <= line && int(r.ends[i]) >= line
}

// findIndex is the index of the last region that starts at or before a line, or -1.
func (r *Regions) findIndex(line int) int {
	low, high := 0, len(r.starts)
	for low < high {
		mid := (low + high) / 2
		if line < int(r.starts[mid]) {
			high = mid
		} else {
			low = mid + 1
		}
	}
	return low - 1
}

// FindRange is the index of the innermost region that has a line, or -1.
func (r *Regions) FindRange(line int) int {
	index := r.findIndex(line)
	if index < 0 {
		return -1
	}
	if int(r.ends[index]) >= line {
		return index
	}
	for index = r.Parent(index); index != -1; index = r.Parent(index) {
		if r.Contains(index, line) {
			return index
		}
	}
	return -1
}

// ToRange describes a region.
func (r *Regions) ToRange(i int) Range {
	return Range{Start: int(r.starts[i]), End: int(r.ends[i]), Type: r.Type(i), Collapsed: r.collapsed[i], Source: r.Source(i)}
}

// Ranges describes all the regions.
func (r *Regions) Ranges() []Range {
	out := make([]Range, r.Len())
	for i := range out {
		out[i] = r.ToRange(i)
	}
	return out
}

// Region returns a region as a value.
func (r *Regions) Region(i int) Region { return Region{r, i} }

// String is the regions as text, for tests: [ -] 1/3 for a region that is not folded, [u+] for a
// folded region the user made.
func (r *Regions) String() string {
	parts := make([]string, r.Len())
	for i := range parts {
		state := "-"
		if r.collapsed[i] {
			state = "+"
		}
		parts[i] = "[" + r.Source(i).abbreviation() + state + "] " + strconv.Itoa(r.Start(i)) + "/" + strconv.Itoa(r.End(i))
	}
	return strings.Join(parts, ", ")
}

// FromRanges makes regions from ranges that are sorted and nested.
func FromRanges(ranges []Range) (*Regions, error) {
	starts, ends := make([]int, len(ranges)), make([]int, len(ranges))
	var types []string
	for i, rg := range ranges {
		starts[i], ends[i] = rg.Start, rg.End
		if rg.Type != "" && types == nil {
			types = make([]string, len(ranges))
		}
	}
	if types != nil {
		for i, rg := range ranges {
			types[i] = rg.Type
		}
	}
	r, err := NewRegions(starts, ends, types)
	if err != nil {
		return nil, err
	}
	for i, rg := range ranges {
		r.collapsed[i] = rg.Collapsed
		r.SetSource(i, rg.Source)
	}
	return r, nil
}

// SelectedLines tells whether a selection starts on a line in a span; the merge uses it to keep
// the region a cursor is in from staying folded.
type SelectedLines interface {
	// StartsInside reports whether a selection starts on a line from start to end, both included.
	StartsInside(start, end int) bool
}

// LinesOf makes SelectedLines of the first lines of the selections.
func LinesOf(starts []int) SelectedLines { return selectedLines(starts) }

type selectedLines []int

func (s selectedLines) StartsInside(start, end int) bool {
	for _, line := range s {
		if line >= start && line <= end {
			return true
		}
	}
	return false
}

// SanitizeAndMerge merges two lists of ranges, each sorted by first line. The first has all the
// regions the providers define now, the second only the ranges that were folded and the manual
// ones. When both have a range with the same first line the one of the provider is taken, and it
// stays folded if the other was folded and has the same last line or no selection is in it. A
// range of the second list that overlaps one of the first wins, when it is user defined and
// larger. Ranges that are not valid are dropped: lines out of the text (past maxLine; a negative
// maxLine means no limit), no more than one line, out of order or starting on the same line as the
// one before, or overlapping the one before without being inside it.
//
// Ported from VS Code: foldingRanges.ts (sanitizeAndMerge)
func SanitizeAndMerge(a, b []Range, maxLine int, sel SelectedLines) []Range {
	if maxLine < 0 {
		maxLine = MaxLine
	}
	get := func(list []Range, i int) *Range {
		if i < len(list) {
			r := list[i]
			return &r
		}
		return nil
	}
	ia, ib := 0, 0
	nextA, nextB := get(a, 0), get(b, 0)
	var stacked []*Range
	var top *Range
	prevLine := -1
	var result []Range
	for nextA != nil || nextB != nil {
		var use *Range
		if nextB != nil && (nextA == nil || nextA.Start >= nextB.Start) {
			if nextA != nil && nextA.Start == nextB.Start {
				if nextB.Source == UserDefined {
					// A user defined range, folded or not.
					use = nextB
				} else {
					// A folded range from before, or a recovered range that may be unfolded.
					use = nextA
					use.Collapsed = nextB.Collapsed && (nextA.End == nextB.End || sel == nil || !sel.StartsInside(nextA.Start+1, nextA.End+1))
					use.Source = FromProvider
				}
				ia++
				nextA = get(a, ia)
			} else {
				use = nextB
				if nextB.Collapsed && nextB.Source == FromProvider {
					// A range that was folded.
					use.Source = Recovered
				}
			}
			ib++
			nextB = get(b, ib)
		} else {
			// nextA is next. A user defined range of B takes precedence, so look ahead in B for a
			// conflict.
			scan := ib
			prescan := nextB
			for {
				if prescan == nil || prescan.Start > nextA.End {
					use = nextA
					break
				}
				if prescan.Source == UserDefined && prescan.End > nextA.End {
					// A user defined range wins: this range of A is skipped.
					break
				}
				scan++
				prescan = get(b, scan)
			}
			ia++
			nextA = get(a, ia)
		}
		if use == nil {
			continue
		}
		for top != nil && top.End < use.Start {
			top = nil
			if n := len(stacked); n > 0 {
				top, stacked = stacked[n-1], stacked[:n-1]
			}
		}
		if use.End > use.Start && use.Start > prevLine && use.End <= maxLine && (top == nil || top.End >= use.End) {
			result = append(result, *use)
			prevLine = use.Start
			if top != nil {
				stacked = append(stacked, top)
			}
			kept := result[len(result)-1]
			top = &kept
		}
	}
	return result
}

// Region is one folding region of a list of regions (VS Code's FoldingRegion).
type Region struct {
	regions *Regions
	index   int
}

// Index is the index of the region.
func (r Region) Index() int { return r.index }

// Start is the first line of the region.
func (r Region) Start() int { return r.regions.Start(r.index) }

// End is the last line of the region.
func (r Region) End() int { return r.regions.End(r.index) }

// Parent is the index of the region this one is inside of, or -1.
func (r Region) Parent() int { return r.regions.Parent(r.index) }

// Collapsed reports whether the region is folded.
func (r Region) Collapsed() bool { return r.regions.Collapsed(r.index) }

// ContainedBy reports whether the lines of the region are inside the lines first to last.
func (r Region) ContainedBy(first, last int) bool { return first <= r.Start() && last >= r.End() }

// ContainsLine reports whether the region has the line.
func (r Region) ContainsLine(line int) bool { return r.Start() <= line && line <= r.End() }

// HidesLine reports whether the region, when it is folded, hides the line: every line of it but
// the first.
func (r Region) HidesLine(line int) bool { return r.Start() < line && line <= r.End() }
