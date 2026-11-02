package testing

import "strings"

// IDSeparator separates the parts of the id of an item: the id of its controller, then the id of the item among the children of the
// controller, and so on down the tree (VS Code's TestId). An id of a controller alone is the root of its tree.
const IDSeparator = "\x00"

// JoinID makes the id of the item at the path of ids below a controller.
func JoinID(controllerID string, path ...string) string {
	if len(path) == 0 {
		return controllerID
	}
	return controllerID + IDSeparator + strings.Join(path, IDSeparator)
}

// SplitID splits an id into the id of the controller and the ids of the items down to it.
func SplitID(id string) []string { return strings.Split(id, IDSeparator) }

// ControllerOf is the id of the controller the id belongs to.
func ControllerOf(id string) string {
	ctrl, _, _ := strings.Cut(id, IDSeparator)
	return ctrl
}

// ParentID is the id of the parent of an item, which for a top item is its controller's id; it is empty for the id of a controller.
func ParentID(id string) string {
	i := strings.LastIndex(id, IDSeparator)
	if i < 0 {
		return ""
	}
	return id[:i]
}

// IsAncestorID reports whether ancestor is an id above id: the controller, or the item some levels up. An id is not its own ancestor.
func IsAncestorID(ancestor, id string) bool {
	return len(id) > len(ancestor) && strings.HasPrefix(id, ancestor) && id[len(ancestor):len(ancestor)+len(IDSeparator)] == IDSeparator
}

// IDsToRoot lists the ids from the item up to its controller's id, both included.
func IDsToRoot(id string) []string {
	var out []string
	for x := id; x != ""; x = ParentID(x) {
		out = append(out, x)
	}
	return out
}
