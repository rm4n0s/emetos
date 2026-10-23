// Package plist reads XML property lists, the format of TextMate themes (.tmTheme) and of some
// grammars. Only what those files use is read: dict, array, string, integer, real and the
// booleans.
package plist

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// Parse reads an XML property list whose root is a dict, as the TextMate grammars and themes in
// that format are, and gives its value: dicts are map[string]any, arrays []any, strings string,
// integers int64, reals float64 and booleans bool. The error has the tag PlistInvalid when the
// text is not a plist, is not well formed, or its root is not a dict.
func Parse(data []byte) (map[string]any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, errors.NewErr("PlistInvalid", err).SetMetadata("reason", "there is no plist element")
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "plist" {
			continue
		}
		value, err := nextValue(dec)
		if err != nil {
			return nil, err
		}
		root, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("PlistInvalid", "the root of the plist is not a dict")
		}
		return root, nil
	}
}

// nextValue reads the next element, which must be a value, and the value it holds.
func nextValue(dec *xml.Decoder) (any, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, errors.NewErr("PlistInvalid", err).SetMetadata("reason", "a value is missing")
		}
		if start, ok := tok.(xml.StartElement); ok {
			return readValue(dec, start)
		}
	}
}

// readValue reads the value of the element start, which is a dict, array, string, integer, real,
// true or false, up to the end of the element.
func readValue(dec *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		dict := map[string]any{}
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, errors.NewErr("PlistInvalid", err).SetMetadata("reason", "a dict is not closed")
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return dict, nil
			case xml.StartElement:
				if t.Name.Local != "key" {
					return nil, errors.New("PlistInvalid", "a dict has an element that is not a key", "element", t.Name.Local)
				}
				key, err := text(dec)
				if err != nil {
					return nil, err
				}
				if dict[key], err = nextValue(dec); err != nil {
					return nil, err
				}
			}
		}
	case "array":
		list := []any{}
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, errors.NewErr("PlistInvalid", err).SetMetadata("reason", "an array is not closed")
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return list, nil
			case xml.StartElement:
				item, err := readValue(dec, t)
				if err != nil {
					return nil, err
				}
				list = append(list, item)
			}
		}
	case "string":
		return text(dec)
	case "integer":
		s, err := text(dec)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, errors.NewErr("PlistInvalid", err).SetMetadata("reason", "an integer is not a number")
		}
		return n, nil
	case "real":
		s, err := text(dec)
		if err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, errors.NewErr("PlistInvalid", err).SetMetadata("reason", "a real is not a number")
		}
		return f, nil
	case "true", "false":
		if err := dec.Skip(); err != nil {
			return nil, errors.NewErr("PlistInvalid", err).SetMetadata("reason", "a boolean is not closed")
		}
		return start.Name.Local == "true", nil
	}
	return nil, errors.New("PlistInvalid", "an element is not a plist value", "element", start.Name.Local)
}

// text reads the character data of the element that was just opened, up to its end.
func text(dec *xml.Decoder) (string, error) {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", errors.NewErr("PlistInvalid", err).SetMetadata("reason", "an element is not closed")
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			return sb.String(), nil
		}
	}
}
