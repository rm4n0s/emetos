package format

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension/jsonc"
)

// Document is a type of file the client reads and writes: Collection (and Folder), Request and Environment.
type Document interface {
	Collection | Request | Environment
}

// Position is a place in a text: the line and column (both from 1, the column counted in characters) and the byte offset.
type Position struct {
	Line, Column, Offset int
}

// PositionOf returns the place of the byte offset in the text.
func PositionOf(text string, offset int) Position {
	offset = max(0, min(offset, len(text)))
	line, col := 1, 1
	for _, r := range text[:offset] {
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return Position{Line: line, Column: col, Offset: offset}
}

// fieldPointers maps the keys of the top level of a document to the fields they fill.
func fieldPointers(doc any) map[string]any {
	switch d := doc.(type) {
	case *Collection:
		return map[string]any{"name": &d.Name, "description": &d.Description, "order": &d.Order, "variables": &d.Variables, "auth": &d.Auth, "headers": &d.Headers}
	case *Request:
		return map[string]any{
			"name": &d.Name, "description": &d.Description, "method": &d.Method, "url": &d.URL, "params": &d.Params, "headers": &d.Headers,
			"auth": &d.Auth, "body": &d.Body, "settings": &d.Settings, "extract": &d.Extract, "checks": &d.Checks,
		}
	case *Environment:
		return map[string]any{"name": &d.Name, "variables": &d.Variables}
	}
	return nil
}

// Parse reads a file of the client from its text, with comments and trailing commas allowed. Keys it does not know are ignored.
//
// A file of a newer version (emetosHttp above Version) is read as far as it can be and returned together with an error that
// has the tag HttpFileNewerVersion (IsNewer tells it): the caller shows the file read-only. The other errors, which come with the
// line and column in their metadata ("line", "column", "offset"), have the tags HttpFileSyntax (the text is not JSON with
// comments), HttpFileNotObject, HttpFileVersionMissing, HttpFileVersionInvalid and HttpFileValueInvalid (a key holds a value of the
// wrong shape, or one the format does not allow; "key" names it).
func Parse[T Document](text string) (T, error) {
	var doc T
	root, perrs := jsonc.ParseTree(text, jsonc.ParseOptions{})
	if len(perrs) > 0 {
		p := PositionOf(text, perrs[0].Offset)
		return doc, errors.New("HttpFileSyntax", perrs[0].Message(), "line", p.Line, "column", p.Column, "offset", p.Offset, "count", len(perrs))
	}
	if root == nil || root.Type != jsonc.ObjectNode {
		off := 0
		if root != nil {
			off = root.Offset
		}
		p := PositionOf(text, off)
		return doc, errors.New("HttpFileNotObject", "the file must hold one JSON object", "line", p.Line, "column", p.Column, "offset", p.Offset)
	}
	vnode := jsonc.FindNodeAtLocation(root, jsonc.Path{"emetosHttp"})
	if vnode == nil {
		p := PositionOf(text, root.Offset)
		return doc, errors.New("HttpFileVersionMissing", `the file has no "emetosHttp" version`, "line", p.Line, "column", p.Column, "offset", p.Offset)
	}
	version, ok := vnode.Value.(float64)
	if vnode.Type != jsonc.NumberNode || !ok || version < 1 || version != float64(int(version)) {
		p := PositionOf(text, vnode.Offset)
		return doc, errors.New("HttpFileVersionInvalid", `"emetosHttp" must be a whole number from 1`, "line", p.Line, "column", p.Column, "offset", p.Offset)
	}
	newer := int(version) > Version

	fields := fieldPointers(&doc)
	for _, prop := range root.Children {
		if len(prop.Children) != 2 {
			continue
		}
		key, _ := prop.Children[0].Value.(string)
		ptr, known := fields[key]
		if !known {
			continue
		}
		if err := jsonc.Decode(prop.Children[1], ptr); err != nil {
			if newer {
				continue // a newer file may shape its keys otherwise
			}
			p := PositionOf(text, prop.Children[1].Offset)
			return *new(T), errors.New("HttpFileValueInvalid", err.Error(), "key", key, "line", p.Line, "column", p.Column, "offset", p.Offset)
		}
	}
	if newer {
		p := PositionOf(text, vnode.Offset)
		return doc, errors.New("HttpFileNewerVersion", "the file is of a newer version of the format; it is shown read-only", "version", int(version), "supported", Version, "line", p.Line, "column", p.Column, "offset", p.Offset)
	}
	if bad := validate(valueOf(&doc)); bad != nil {
		p := PositionOf(text, root.Offset)
		if n := jsonc.FindNodeAtLocation(root, jsonc.Path{bad.key}); n != nil {
			p = PositionOf(text, n.Offset)
		}
		return *new(T), errors.New("HttpFileValueInvalid", bad.msg, "key", bad.key, "line", p.Line, "column", p.Column, "offset", p.Offset)
	}
	return doc, nil
}

// valueOf returns the document a pointer to one points to, as a plain value.
func valueOf(p any) any {
	switch d := p.(type) {
	case *Collection:
		return *d
	case *Request:
		return *d
	case *Environment:
		return *d
	}
	return nil
}

// IsNewer reports whether err is the HttpFileNewerVersion of Parse or Update.
func IsNewer(err error) bool {
	e, ok := errors.FromError(err)
	return ok && e.Tag == "HttpFileNewerVersion"
}

// wire shapes: the document with the version in front, as it is written.
type (
	collectionWire struct {
		Version int `json:"emetosHttp"`
		Collection
	}
	requestWire struct {
		Version int `json:"emetosHttp"`
		Request
	}
	environmentWire struct {
		Version int `json:"emetosHttp"`
		Environment
	}
)

// wireOf wraps a document for writing.
func wireOf(doc any) any {
	switch d := doc.(type) {
	case Collection:
		return collectionWire{Version, d}
	case Request:
		return requestWire{Version, d}
	case Environment:
		return environmentWire{Version, d}
	}
	return nil
}
