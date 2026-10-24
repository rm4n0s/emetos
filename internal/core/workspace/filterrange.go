package workspace

import (
	"regexp"
	"strconv"
	"strings"
)

// FilterRange is the position or range a quick open filter asks for after the file name:
// "main.go:12:5", "main.go#12", "main.go(12,5)", "main.go:20:3-40:5". Lines and columns count
// from 1.
type FilterRange struct {
	StartLine, StartColumn int
	EndLine, EndColumn     int
}

// lineColonPattern is the suffix of a filter that says a position: <#|:|(><line><#|:|,><col?> with
// an optional range <-<endLine><#|:|,><endCol?>>.
//
// Ported from VS Code: src/vs/workbench/contrib/search/common/search.ts (LINE_COLON_PATTERN)
var lineColonPattern = regexp.MustCompile(`\s?[#:(](?:line )?(\d*)(?:[#:,](\d*))?(?:-(\d*)(?:[#:,](\d*))?)?\)?:?\s*$`)

// ExtractRangeFromFilter splits a quick open filter into the file pattern and the position after
// it. ok is false when the filter has no position. A filter that has one of the unless strings at
// its start, or before a text that is not a position, is left alone (the file mode passes "@", the
// prefix of the symbols of the file).
//
// Ported from VS Code: src/vs/workbench/contrib/search/common/search.ts (extractRangeFromFilter)
func ExtractRangeFromFilter(filter string, unless ...string) (pattern string, rng FilterRange, ok bool) {
	if filter == "" {
		return "", FilterRange{}, false
	}
	for _, value := range unless {
		pos := strings.Index(filter, value)
		if pos == 0 || (pos > 0 && !lineColonPattern.MatchString(filter[pos+len(value):])) {
			return "", FilterRange{}, false
		}
	}
	m := lineColonPattern.FindStringSubmatchIndex(filter)
	if m == nil {
		return "", FilterRange{}, false
	}
	group := func(i int) (int, bool) {
		if m[2*i] < 0 || m[2*i] == m[2*i+1] {
			return 0, false
		}
		n, err := strconv.Atoi(filter[m[2*i]:m[2*i+1]])
		return n, err == nil
	}
	line, hasLine := group(1)
	switch {
	case hasLine:
		rng = FilterRange{StartLine: line, StartColumn: 1, EndLine: line, EndColumn: 1}
		if col, ok := group(2); ok {
			rng.StartColumn, rng.EndColumn = col, col
		}
		if endLine, ok := group(3); ok {
			rng.EndLine, rng.EndColumn = endLine, 1
			if endCol, ok := group(4); ok {
				rng.EndColumn = endCol
			}
		}
	case m[3] == m[2]:
		// "something:" or "something#" without a line number: the start of the file
		rng = FilterRange{StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 1}
	default:
		return "", FilterRange{}, false
	}
	return filter[:m[0]], rng, true
}
