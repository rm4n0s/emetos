// Command plist2json converts a TextMate grammar in the XML property list format to the JSON
// format that the grammar loader reads (SPEC §8.1). scripts/update-assets.sh runs it on the
// grammars of the reference extensions that are plists.
//
// Usage: plist2json <input.tmLanguage> <output.json>
package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

func main() {
	if len(os.Args) != 3 {
		_, _ = os.Stderr.WriteString("usage: plist2json <input.tmLanguage> <output.json>\n")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("conversion failed", "err", err)
		os.Exit(1)
	}
}

// run converts the plist in the file in to JSON in the file out. The error tags are
// PlistReadFailed and PlistWriteFailed, and those of convert.
func run(in, out string) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return errors.NewErr("PlistReadFailed", err).SetMetadata("path", in)
	}
	converted, err := convert(data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, converted, 0o644); err != nil {
		return errors.NewErr("PlistWriteFailed", err).SetMetadata("path", out)
	}
	return nil
}

// convert turns the text of a plist, whose root is a dict, into indented JSON with sorted keys.
// The error has the tag PlistInvalid when the text is not a plist, or its root is not a dict, and
// PlistEncodeFailed when the value cannot be written as JSON.
func convert(data []byte) ([]byte, error) {
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
		root, err := nextValue(dec)
		if err != nil {
			return nil, err
		}
		if _, ok := root.(map[string]any); !ok {
			return nil, errors.New("PlistInvalid", "the root of the plist is not a dict")
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "\t")
		if err := enc.Encode(root); err != nil {
			return nil, errors.NewErr("PlistEncodeFailed", err)
		}
		return buf.Bytes(), nil
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
