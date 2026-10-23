package editorgroup

import (
	"strings"
)

// TabLabel is what a tab shows of an editor.
type TabLabel struct {
	// Name is the name of the editor.
	Name string
	// Description is the dimmer text after the name; it is empty unless another editor has the
	// same name.
	Description string
	// Title is the text of the tooltip.
	Title string
}

// TabLabels gives the labels of editors, in the order of the editors. An editor that shares its
// name with another gets a description, the shortest part of its folder that tells it from the
// others (VS Code's `workbench.editor.labelFormat` "default").
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts
// (computeTabLabels)
func TabLabels(editors []EditorInput) []TabLabel {
	labels := make([]TabLabel, len(editors))
	byName := map[string][]int{}
	for i, e := range editors {
		labels[i] = TabLabel{Name: e.Name(), Title: e.Title()}
		byName[labels[i].Name] = append(byName[labels[i].Name], i)
	}
	for _, same := range byName {
		if len(same) < 2 {
			continue
		}
		descriptions := make([]string, len(same))
		for j, i := range same {
			descriptions[j] = editors[i].Description()
		}
		short := Shorten(descriptions)
		for j, i := range same {
			labels[i].Description = short[j]
		}
	}
	return labels
}

const ellipsis = "…"

// Shorten gives, for each path, the shortest part of it that none of the others has, with an
// ellipsis for what is left out: ["/a/b/x", "/a/b/y"] gives ["/a/…/x", "/a/…/y"]. A path with no
// part of its own is kept as it is, and an empty one becomes "./". Paths are written with "/"
// and may start with "/" or "~".
//
// Ported from VS Code: src/vs/base/common/labels.ts (shorten)
func Shorten(paths []string) []string {
	const sep = "/"
	out := make([]string, len(paths))
	for pathIndex, original := range paths {
		if original == "" {
			out[pathIndex] = "." + sep
			continue
		}
		prefix, trimmed := "", original
		switch {
		case strings.HasPrefix(trimmed, sep):
			prefix, trimmed = sep, trimmed[len(sep):]
		case strings.HasPrefix(trimmed, "~"):
			prefix, trimmed = "~", trimmed[1:]
		}
		segments := strings.Split(trimmed, sep)
		match := true
		for subpathLength := 1; match && subpathLength <= len(segments); subpathLength++ {
			for start := len(segments) - subpathLength; match && start >= 0; start-- {
				match = false
				subpath := strings.Join(segments[start:start+subpathLength], sep)
				// unique among the other paths
				for other := 0; !match && other < len(paths); other++ {
					if other == pathIndex || paths[other] == "" || !strings.Contains(paths[other], subpath) {
						continue
					}
					isSubpathEnding := start+subpathLength == len(segments)
					// the separator before the subpath makes it a folder name, not just text
					withSep := subpath
					if start > 0 && strings.Contains(paths[other], sep) {
						withSep = sep + subpath
					}
					match = !isSubpathEnding || strings.HasSuffix(paths[other], withSep)
				}
				if match {
					continue
				}
				result := ""
				if prefix != "" {
					if start == 1 {
						// the first segment is kept with the subpath
						start = 0
						subpathLength++
						subpath = segments[0] + sep + subpath
					}
					if start > 0 {
						result = segments[0] + sep
					}
					result = prefix + result
				}
				if start > 0 {
					result += ellipsis + sep
				}
				result += subpath
				if start+subpathLength < len(segments) {
					if start+subpathLength == len(segments)-1 && segments[len(segments)-1] == "" {
						result += sep
					} else {
						result += sep + ellipsis
					}
				}
				out[pathIndex] = result
			}
		}
		if match {
			out[pathIndex] = original // no part of its own
		}
	}
	return out
}
