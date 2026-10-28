package search

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/glob"
)

// ParsePatterns turns the text of the "files to include" or "files to exclude" box into glob patterns relative to the
// folder (VS Code's QueryBuilder.parseSearchPaths): the text is split at the commas that are not inside braces or brackets,
// and
//   - a segment that starts with "./" is relative to the folder: "./src" is "src" and "src/**";
//   - a segment that is an absolute path inside the folder is the same, relative to it; one that is outside the folder
//     is left out;
//   - any other segment is searched for in every folder: "foo" is "**/foo" and "**/foo/**", and ".js" is "*.js".
//
// A leading "~" is not expanded and "../" reaches out of the folder, so those segments are left out.
func ParsePatterns(text, folder string) []string {
	var out []string
	for _, seg := range glob.SplitGlobAware(text, ',') {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		seg = strings.ReplaceAll(seg, `\`, "/")
		switch {
		case seg == "." || seg == "./":
			out = append(out, "**")
		case strings.HasPrefix(seg, "./"):
			out = appendRelative(out, strings.TrimPrefix(seg, "./"))
		case strings.HasPrefix(seg, "../") || seg == ".." || seg[0] == '~':
			// outside the folder
		case filepath.IsAbs(seg):
			rel, ok := strings.CutPrefix(filepath.ToSlash(filepath.Clean(seg)), strings.TrimSuffix(filepath.ToSlash(folder), "/")+"/")
			if ok {
				out = appendRelative(out, rel)
			}
		default:
			seg = strings.TrimRight(seg, "/")
			if seg == "" {
				continue
			}
			if seg[0] == '.' {
				seg = "*" + seg
			}
			out = append(out, strings.ReplaceAll("**/"+seg+"/**", "**/**", "**"), strings.ReplaceAll("**/"+seg, "**/**", "**"))
		}
	}
	return out
}

// appendRelative adds the patterns of a path relative to the folder: the path itself and what is below it.
func appendRelative(out []string, rel string) []string {
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return append(out, "**")
	}
	return append(out, rel, rel+"/**")
}

// expression makes a glob expression of patterns that are all on.
func expression(patterns []string) glob.Expression {
	expr := make(glob.Expression, len(patterns))
	for i, p := range patterns {
		expr[i] = glob.Clause{Pattern: p}
	}
	return expr
}
