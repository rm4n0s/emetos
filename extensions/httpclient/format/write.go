package format

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension/jsonc"
)

// The keys of each kind of object, in the order a new file has them (SPEC §10.33 "The format"). An object of a file that is not
// listed here (the value of a check, an unknown key's value) is left as it is.
var (
	collectionKeys   = []string{"emetosHttp", "name", "description", "order", "variables", "auth", "headers"}
	requestKeys      = []string{"emetosHttp", "name", "description", "method", "url", "params", "headers", "auth", "body", "settings", "extract", "checks"}
	environmentKeys  = []string{"emetosHttp", "name", "variables"}
	pairKeys         = []string{"name", "value", "file", "description", "enabled"}
	variableKeys     = []string{"name", "value", "enabled", "secret"}
	authKeys         = []string{"type", "username", "password", "token", "key", "value", "in", "grant", "tokenUrl", "authUrl", "clientId", "clientSecret", "scope", "audience", "pkce", "addTo"}
	bodyKeys         = []string{"type", "file", "variables", "fields", "path"}
	settingsKeys     = []string{"timeoutMs", "followRedirects", "maxRedirects", "verifyTls"}
	extractKeys      = []string{"from", "path", "header", "cookie", "regex", "to", "scope"}
	checkKeys        = []string{"subject", "path", "header", "op", "value"}
	formattingOption = jsonc.FormattingOptions{InsertSpaces: true, TabSize: 2, EOL: "\n"}
)

// keysAt returns the keys of the object at the path of a document whose top level has the keys top.
func keysAt(top []string, path jsonc.Path) []string {
	if len(path) == 0 {
		return top
	}
	// the name of the last property on the way; the indices of arrays do not count
	name := ""
	for i := len(path) - 1; i >= 0; i-- {
		if s, ok := path[i].(string); ok {
			name = s
			break
		}
	}
	switch name {
	case "params", "headers", "fields":
		return pairKeys
	case "variables":
		return variableKeys
	case "auth":
		return authKeys
	case "body":
		return bodyKeys
	case "settings":
		return settingsKeys
	case "extract":
		return extractKeys
	case "checks":
		return checkKeys
	}
	return nil
}

func topKeysOf(doc any) []string {
	switch doc.(type) {
	case Collection:
		return collectionKeys
	case Request:
		return requestKeys
	}
	return environmentKeys
}

// normalized turns a document into the values encoding/json reads from the text of its file.
func normalized(doc any) (any, error) {
	data, err := json.Marshal(wireOf(doc))
	if err != nil {
		return nil, errors.NewErr("HttpFileEncodeFailed", err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errors.NewErr("HttpFileEncodeFailed", err)
	}
	return out, nil
}

// Marshal writes a new file for the document: the keys in the order of the SPEC, two spaces, a final line break. A secret
// variable has no value in it. It fails with HttpFileValueInvalid for a value the format does not allow (the key is in the
// metadata) and HttpFileEncodeFailed.
func Marshal[T Document](doc T) (string, error) {
	if bad := validate(doc); bad != nil {
		return "", errors.New("HttpFileValueInvalid", bad.msg, "key", bad.key)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(wireOf(doc)); err != nil {
		return "", errors.NewErr("HttpFileEncodeFailed", err)
	}
	return buf.String(), nil
}

// Update returns the text of a file that holds the document, made from the old text of the file by edits: a value that did not
// change is not touched, the comments and the layout stay, and so do the keys the format does not know and the unknown keys of
// the objects of the tables. A key the document lacks that the format knows is removed; a new key goes where the order of the
// SPEC puts it among the keys the file has. A secret variable's value is removed from the text.
//
// It fails with HttpFileValueInvalid (a value the format does not allow), HttpFileSyntax, HttpFileNotObject (the old text is not
// a file of the client), HttpFileNewerVersion (the old file is of a newer version: it is never written), HttpFileEncodeFailed and
// the tags of the JSON editor.
func Update[T Document](oldText string, doc T) (string, error) {
	if bad := validate(doc); bad != nil {
		return "", errors.New("HttpFileValueInvalid", bad.msg, "key", bad.key)
	}
	root, perrs := jsonc.ParseTree(oldText, jsonc.ParseOptions{})
	if len(perrs) > 0 {
		p := PositionOf(oldText, perrs[0].Offset)
		return "", errors.New("HttpFileSyntax", perrs[0].Message(), "line", p.Line, "column", p.Column, "offset", p.Offset, "count", len(perrs))
	}
	if root == nil || root.Type != jsonc.ObjectNode {
		return "", errors.New("HttpFileNotObject", "the file must hold one JSON object")
	}
	if v := jsonc.FindNodeAtLocation(root, jsonc.Path{"emetosHttp"}); v != nil {
		if n, ok := v.Value.(float64); ok && int(n) > Version {
			return "", errors.New("HttpFileNewerVersion", "the file is of a newer version of the format and is not written", "version", int(n), "supported", Version)
		}
	}
	want, err := normalized(doc)
	if err != nil {
		return "", err
	}
	return syncValue(oldText, nil, topKeysOf(doc), want)
}

// syncValue makes the value at the path of the text equal to want by the smallest edits, and returns the new text.
func syncValue(text string, path jsonc.Path, top []string, want any) (string, error) {
	root, _ := jsonc.ParseTree(text, jsonc.ParseOptions{})
	node := jsonc.FindNodeAtLocation(root, path)
	if node == nil {
		return setValue(text, path, top, want)
	}
	switch w := want.(type) {
	case map[string]any:
		if node.Type != jsonc.ObjectNode {
			return replaceValue(text, path, top, want)
		}
		return syncObject(text, path, top, node, w)
	case []any:
		if node.Type != jsonc.ArrayNode {
			return replaceValue(text, path, top, want)
		}
		return syncArray(text, path, top, node, w)
	}
	if reflect.DeepEqual(jsonc.NodeValue(node), want) {
		return text, nil
	}
	return replaceValue(text, path, top, want)
}

func syncObject(text string, path jsonc.Path, top []string, node *jsonc.Node, want map[string]any) (string, error) {
	keys := keysAt(top, path)
	have := map[string]bool{}
	for _, prop := range node.Children {
		if len(prop.Children) > 0 {
			if name, ok := prop.Children[0].Value.(string); ok {
				have[name] = true
			}
		}
	}
	var err error
	// the keys of the format, in its order, so that new ones are inserted in order
	for _, key := range keys {
		sub := append(slices.Clone(path), key)
		if v, ok := want[key]; ok {
			if text, err = syncValue(text, sub, top, v); err != nil {
				return "", err
			}
		} else if have[key] {
			edits, err := jsonc.Remove(text, sub, formattingOption)
			if err != nil {
				return "", err
			}
			if text, err = jsonc.ApplyEdits(text, edits); err != nil {
				return "", err
			}
		}
	}
	// keys that the document has and the format does not list (the value of a check is an object of any keys)
	for key, v := range want {
		if slices.Contains(keys, key) {
			continue
		}
		if text, err = syncValue(text, append(slices.Clone(path), key), top, v); err != nil {
			return "", err
		}
	}
	return text, nil
}

func syncArray(text string, path jsonc.Path, top []string, node *jsonc.Node, want []any) (string, error) {
	have := len(node.Children)
	var err error
	for i := 0; i < min(have, len(want)); i++ {
		if text, err = syncValue(text, append(slices.Clone(path), i), top, want[i]); err != nil {
			return "", err
		}
	}
	for i := have - 1; i >= len(want); i-- {
		edits, err := jsonc.Remove(text, append(slices.Clone(path), i), formattingOption)
		if err != nil {
			return "", err
		}
		if text, err = jsonc.ApplyEdits(text, edits); err != nil {
			return "", err
		}
	}
	for i := have; i < len(want); i++ {
		if text, err = setValue(text, append(slices.Clone(path), -1), top, want[i]); err != nil {
			return "", err
		}
	}
	return text, nil
}

func setValue(text string, path jsonc.Path, top []string, want any) (string, error) {
	var index func(names []string) int
	if len(path) > 0 {
		if name, ok := path[len(path)-1].(string); ok {
			keys := keysAt(top, path[:len(path)-1])
			rank := slices.Index(keys, name)
			index = func(names []string) int {
				at := 0
				for i, n := range names {
					if r := slices.Index(keys, n); r >= 0 && r < rank {
						at = i + 1
					}
				}
				return at
			}
		}
	}
	edits, err := jsonc.SetValueAt(text, path, want, formattingOption, index)
	if err != nil {
		return "", err
	}
	return jsonc.ApplyEdits(text, edits)
}

// replaceValue sets the value at the path where there is one already. The editor inserts at the index of an array, so an element
// is removed first.
func replaceValue(text string, path jsonc.Path, top []string, want any) (string, error) {
	if len(path) > 0 {
		if _, isIndex := path[len(path)-1].(int); isIndex {
			edits, err := jsonc.Remove(text, path, formattingOption)
			if err != nil {
				return "", err
			}
			if text, err = jsonc.ApplyEdits(text, edits); err != nil {
				return "", err
			}
		}
	}
	return setValue(text, path, top, want)
}
