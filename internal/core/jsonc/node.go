package jsonc

import (
	"encoding/json"
	"slices"

	"github.com/rm4n0s/errors"
)

// NodeType is the kind of a node.
type NodeType string

// The kinds of nodes.
const (
	// ObjectNode is an object; its children are property nodes.
	ObjectNode NodeType = "object"
	// ArrayNode is an array; its children are its elements.
	ArrayNode NodeType = "array"
	// PropertyNode is a property of an object; its first child is the name (a string
	// node) and its second, if any, the value.
	PropertyNode NodeType = "property"
	// StringNode is a string.
	StringNode NodeType = "string"
	// NumberNode is a number.
	NumberNode NodeType = "number"
	// BooleanNode is true or false.
	BooleanNode NodeType = "boolean"
	// NullNode is null.
	NullNode NodeType = "null"
)

// Node is a part of a parsed text, with where it is in it (bytes).
type Node struct {
	Type NodeType
	// Value is the value of a literal: a string, a float64, a bool or nil.
	Value any
	// Offset and Length are the place of the node in the text. A property
	// goes from its name to the end of its value.
	Offset, Length int
	// ColonOffset is where the colon of a property is, or -1 when it has none.
	ColonOffset int
	// Parent is nil for the root.
	Parent *Node
	// Children are the properties of an object, the elements of an array, or the name
	// and value of a property.
	Children []*Node
}

// End is the offset after the node.
func (n *Node) End() int { return n.Offset + n.Length }

// Path is a place in a document: the names of the properties (strings) and the indices
// of the array elements (ints) to follow from the root.
type Path []any

func nodeTypeOf(value any) NodeType {
	switch value.(type) {
	case bool:
		return BooleanNode
	case float64:
		return NumberNode
	case string:
		return StringNode
	}
	return NullNode
}

// ParseTree parses text into a tree and lists the errors it found. It is tolerant: on
// invalid text it still returns what it could make of it, so always check the errors.
// The result is nil when the text has no value.
//
// Ported from VS Code: src/vs/base/common/json.ts (parseTree).
func ParseTree(text string, opts ParseOptions) (*Node, []ParseError) {
	var errs []ParseError
	current := &Node{Type: ArrayNode, Offset: -1, Length: -1, ColonOffset: -1} // artificial root
	ensurePropertyComplete := func(end int) {
		if current.Type == PropertyNode {
			current.Length = end - current.Offset
			current = current.Parent
		}
	}
	add := func(n *Node) *Node {
		n.Parent = current
		current.Children = append(current.Children, n)
		return n
	}
	ending := func(offset, length int) {
		// A property without its value is complete where the object or array ends.
		ensurePropertyComplete(offset)
		current.Length = offset + length - current.Offset
		current = current.Parent
		ensurePropertyComplete(offset + length)
	}
	Visit(text, &Visitor{
		ObjectBegin: func(offset, _ int) {
			current = add(&Node{Type: ObjectNode, Offset: offset, Length: -1, ColonOffset: -1})
		},
		ObjectProperty: func(name string, offset, length int) {
			current = add(&Node{Type: PropertyNode, Offset: offset, Length: -1, ColonOffset: -1})
			add(&Node{Type: StringNode, Value: name, Offset: offset, Length: length, ColonOffset: -1})
		},
		ObjectEnd: ending,
		ArrayBegin: func(offset, _ int) {
			current = add(&Node{Type: ArrayNode, Offset: offset, Length: -1, ColonOffset: -1})
		},
		ArrayEnd: ending,
		LiteralValue: func(value any, offset, length int) {
			add(&Node{Type: nodeTypeOf(value), Value: value, Offset: offset, Length: length, ColonOffset: -1})
			ensurePropertyComplete(offset + length)
		},
		Separator: func(char string, offset, _ int) {
			if current.Type != PropertyNode {
				return
			}
			switch char {
			case ":":
				current.ColonOffset = offset
			case ",":
				ensurePropertyComplete(offset)
			}
		},
		Error: func(code ParseErrorCode, offset, length int) {
			errs = append(errs, ParseError{Code: code, Offset: offset, Length: length})
		},
	}, opts)
	if len(current.Children) == 0 {
		return nil, errs
	}
	root := current.Children[0]
	root.Parent = nil
	return root, errs
}

// Parse parses text into Go values (map[string]any, []any, string, float64, bool and
// nil) and lists the errors it found. It is as tolerant as ParseTree.
//
// Ported from VS Code: src/vs/base/common/json.ts (parse).
func Parse(text string, opts ParseOptions) (any, []ParseError) {
	root, errs := ParseTree(text, opts)
	if root == nil {
		return nil, errs
	}
	return NodeValue(root), errs
}

// NodeValue is the value a node stands for: a map, a slice, a string, a float64, a
// bool or nil. A property without a value is left out of its object.
//
// Ported from VS Code: src/vs/base/common/json.ts (getNodeValue).
func NodeValue(n *Node) any {
	switch n.Type {
	case ArrayNode:
		out := make([]any, len(n.Children))
		for i, c := range n.Children {
			out[i] = NodeValue(c)
		}
		return out
	case ObjectNode:
		out := make(map[string]any, len(n.Children))
		for _, prop := range n.Children {
			if len(prop.Children) < 2 {
				continue
			}
			name, _ := prop.Children[0].Value.(string)
			out[name] = NodeValue(prop.Children[1])
		}
		return out
	case StringNode, NumberNode, BooleanNode, NullNode:
		return n.Value
	}
	return nil
}

// Decode stores the value of a node in the Go value v points to, as encoding/json
// would store the same JSON: struct fields follow their json tags and numbers are
// converted to the type of the destination. Numbers have the precision of a float64.
func Decode(n *Node, v any) error {
	if n == nil {
		return errors.New("JSONCNoValue", "there is no value to decode")
	}
	data, err := json.Marshal(NodeValue(n))
	if err != nil {
		return errors.NewErr("JSONCDecodeFailed", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return errors.NewErr("JSONCDecodeFailed", err)
	}
	return nil
}

// FindNodeAtLocation finds the node at a path, or returns nil.
//
// Ported from VS Code: src/vs/base/common/json.ts (findNodeAtLocation).
func FindNodeAtLocation(root *Node, path Path) *Node {
	if root == nil {
		return nil
	}
	node := root
	for _, segment := range path {
		switch seg := segment.(type) {
		case string:
			if node.Type != ObjectNode {
				return nil
			}
			var found *Node
			for _, prop := range node.Children {
				if len(prop.Children) == 2 && prop.Children[0].Value == seg {
					found = prop.Children[1]
					break
				}
			}
			if found == nil {
				return nil
			}
			node = found
		case int:
			if node.Type != ArrayNode || seg < 0 || seg >= len(node.Children) {
				return nil
			}
			node = node.Children[seg]
		default:
			return nil
		}
	}
	return node
}

// NodePath is the path from the root to a node. The name of a property has the path
// of the property, whose last segment is that name.
//
// Ported from VS Code: src/vs/base/common/json.ts (getNodePath).
func NodePath(n *Node) Path {
	if n.Parent == nil {
		return Path{}
	}
	path := NodePath(n.Parent)
	switch n.Parent.Type {
	case PropertyNode:
		path = append(path, n.Parent.Children[0].Value)
	case ArrayNode:
		if i := slices.Index(n.Parent.Children, n); i >= 0 {
			path = append(path, i)
		}
	}
	return path
}

// Contains tells whether an offset is inside a node; with includeRightBound, also
// right after it.
func Contains(n *Node, offset int, includeRightBound bool) bool {
	return (offset >= n.Offset && offset < n.End()) || (includeRightBound && offset == n.End())
}

// FindNodeAtOffset finds the innermost node at an offset, or returns nil.
//
// Ported from VS Code: src/vs/base/common/json.ts (findNodeAtOffset).
func FindNodeAtOffset(n *Node, offset int, includeRightBound bool) *Node {
	if !Contains(n, offset, includeRightBound) {
		return nil
	}
	for _, c := range n.Children {
		if c.Offset > offset {
			break
		}
		if found := FindNodeAtOffset(c, offset, includeRightBound); found != nil {
			return found
		}
	}
	return n
}
