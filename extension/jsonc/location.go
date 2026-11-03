package jsonc

// Location says where in the document an offset is.
type Location struct {
	// Path is the path of the offset: property names and array indices. In an object
	// where no name has been read yet, the last segment is "".
	Path Path
	// PreviousNode is the property name or literal value before the offset, or nil.
	PreviousNode *Node
	// IsAtPropertyKey is true when the offset is where a property name goes.
	IsAtPropertyKey bool
}

// Matches tells whether a pattern of property names and array indices matches the start
// of the path of the location. In the pattern "*" matches any one segment and "**" any
// number of them, none included.
//
// Ported from VS Code: src/vs/base/common/json.ts (getLocation, matches).
func (l Location) Matches(pattern Path) bool { return matchPath(pattern, l.Path) }

func matchPath(pattern, path Path) bool {
	if len(pattern) == 0 {
		return true
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(path); skip++ {
			if matchPath(pattern[1:], path[skip:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	if pattern[0] == path[0] || pattern[0] == "*" {
		return matchPath(pattern[1:], path[1:])
	}
	return false
}

// GetLocation finds the location of an offset in text: the path to it, whether it is
// at a property name, and the name or literal before it.
//
// Ported from VS Code: src/vs/base/common/json.ts (getLocation).
func GetLocation(text string, offset int) Location {
	segments := Path{}
	var previous *Node
	atKey := false
	setPrevious := func(value any, offset, length int, t NodeType) {
		previous = &Node{Type: t, Value: value, Offset: offset, Length: length, ColonOffset: -1}
	}
	Visit(text, &Visitor{
		ObjectBegin: func(o, _ int) {
			if offset <= o {
				StopVisit()
			}
			previous = nil
			atKey = offset > o
			segments = append(segments, "") // a placeholder, replaced by the name
		},
		ObjectProperty: func(name string, o, length int) {
			if offset < o {
				StopVisit()
			}
			setPrevious(name, o, length, PropertyNode)
			segments[len(segments)-1] = name
			if offset <= o+length {
				StopVisit()
			}
		},
		ObjectEnd: func(o, _ int) {
			if offset <= o {
				StopVisit()
			}
			previous = nil
			segments = segments[:len(segments)-1]
		},
		ArrayBegin: func(o, _ int) {
			if offset <= o {
				StopVisit()
			}
			previous = nil
			segments = append(segments, 0)
		},
		ArrayEnd: func(o, _ int) {
			if offset <= o {
				StopVisit()
			}
			previous = nil
			segments = segments[:len(segments)-1]
		},
		LiteralValue: func(value any, o, length int) {
			if offset < o {
				StopVisit()
			}
			setPrevious(value, o, length, nodeTypeOf(value))
			if offset <= o+length {
				StopVisit()
			}
		},
		Separator: func(sep string, o, _ int) {
			if offset <= o {
				StopVisit()
			}
			if sep == ":" && previous != nil && previous.Type == PropertyNode {
				previous.ColonOffset = o
				atKey = false
				previous = nil
			} else if sep == "," {
				if n, ok := segments[len(segments)-1].(int); ok {
					segments[len(segments)-1] = n + 1
				} else {
					atKey = true
					segments[len(segments)-1] = ""
				}
				previous = nil
			}
		},
	}, ParseOptions{})
	return Location{Path: segments, PreviousNode: previous, IsAtPropertyKey: atKey}
}
