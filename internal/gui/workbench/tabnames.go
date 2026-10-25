package workbench

import (
	"path/filepath"
	"strings"
)

// The names of the workspace tabs of the title bar (SPEC §10.14). This file has no widgets: the labels are plain
// string work, tested on their own.

// NoFolderTabName is the name of the tab of a workspace that has no folder.
const NoFolderTabName = "No Folder"

// TabLabel is what a workspace tab shows: the name, and, when another tab has the same name, the part of the path that
// tells them apart.
type TabLabel struct {
	// Name is the base name of the folder (NoFolderTabName for a workspace without one).
	Name string
	// Description is the shortest part of the parent path that tells the tab from the others of the same name,
	// "work" for /home/me/work/api next to /home/me/home/api; empty when the name is unique.
	Description string
}

// Text is the label as one line: "api — work", with an em dash between the name and the description.
func (l TabLabel) Text() string {
	if l.Description == "" {
		return l.Name
	}
	return l.Name + " — " + l.Description
}

// LabelWorkspaceTabs gives the label of each tab for the folders of the tabs, in the order of the strip ("" for a
// workspace without a folder). Tabs of the same name get the shortest suffix of their parent folders that no other
// tab of that name has (VS Code's label disambiguation of the editor tabs: "work" and "home", or "a/x/src" and
// "b/x/src" when the nearer folders are the same). Two tabs that cannot be told apart (two tabs without a folder) keep
// the same label.
func LabelWorkspaceTabs(folders []string) []TabLabel {
	labels := make([]TabLabel, len(folders))
	parents := make([][]string, len(folders))
	byName := map[string][]int{}
	for i, f := range folders {
		if f == "" {
			labels[i].Name = NoFolderTabName
			byName[NoFolderTabName] = append(byName[NoFolderTabName], i)
			continue
		}
		f = filepath.Clean(f)
		name := filepath.Base(f)
		if name == string(filepath.Separator) || name == "." {
			name = f
		}
		labels[i].Name = name
		parents[i] = pathParts(filepath.Dir(f))
		byName[name] = append(byName[name], i)
	}
	for _, all := range byName {
		var group []int
		for _, i := range all {
			if folders[i] != "" {
				group = append(group, i)
			}
		}
		if len(group) < 2 {
			continue
		}
		for _, i := range group {
			labels[i].Description = distinguishing(i, group, parents)
		}
	}
	return labels
}

// pathParts splits a folder into its names, from the root; the root alone has none.
func pathParts(dir string) []string {
	var parts []string
	for _, p := range strings.Split(dir, string(filepath.Separator)) {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	return parts
}

// distinguishing is the shortest suffix of the parent folders of tab i that no other tab of the group has.
func distinguishing(i int, group []int, parents [][]string) string {
	mine := parents[i]
	for depth := 1; depth <= len(mine); depth++ {
		suffix := strings.Join(mine[len(mine)-depth:], "/")
		unique := true
		for _, j := range group {
			if j == i {
				continue
			}
			other := parents[j]
			if len(other) >= depth && strings.Join(other[len(other)-depth:], "/") == suffix {
				unique = false
				break
			}
		}
		if unique {
			return suffix
		}
	}
	if len(mine) == 0 {
		return string(filepath.Separator)
	}
	return string(filepath.Separator) + strings.Join(mine, "/")
}
