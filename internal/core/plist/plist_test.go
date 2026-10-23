package plist

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

const plistHead = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">`

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		body string
		want map[string]any
	}{
		{"empty dict", `<dict/>`, map[string]any{}},
		{"strings", `<dict><key>name</key><string>A &amp; B</string><key>m</key><string>\\[wW]|\.</string></dict>`,
			map[string]any{"name": "A & B", "m": `\\[wW]|\.`}},
		{"booleans and numbers", `<dict><key>a</key><true/><key>b</key><false/><key>c</key><integer>3</integer><key>d</key><real>1.5</real></dict>`,
			map[string]any{"a": true, "b": false, "c": int64(3), "d": 1.5}},
		{"arrays", `<dict><key>fileTypes</key><array/><key>patterns</key><array><dict><key>include</key><string>#x</string></dict><string>y</string></array></dict>`,
			map[string]any{"fileTypes": []any{}, "patterns": []any{map[string]any{"include": "#x"}, "y"}}},
		{"nested dicts", `<dict><key>repository</key><dict><key>x</key><dict><key>match</key><string>a</string></dict></dict></dict>`,
			map[string]any{"repository": map[string]any{"x": map[string]any{"match": "a"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(plistHead + tc.body + `</plist>`))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name, text, route string
	}{
		{"not a plist", `hello`, "Parse.PlistInvalid"},
		{"the root is an array", plistHead + `<array/></plist>`, "Parse.PlistInvalid"},
		{"a value is missing", plistHead + `</plist>`, "Parse->nextValue.PlistInvalid"},
		{"an element that is not a value", plistHead + `<dict><key>a</key><date>x</date></dict></plist>`, "readValue.PlistInvalid"},
		{"a dict with no key", plistHead + `<dict><string>a</string></dict></plist>`, "readValue.PlistInvalid"},
		{"an integer that is not a number", plistHead + `<dict><key>a</key><integer>x</integer></dict></plist>`, "readValue.PlistInvalid"},
		{"a real that is not a number", plistHead + `<dict><key>a</key><real>x</real></dict></plist>`, "readValue.PlistInvalid"},
		{"a dict that is not closed", plistHead + `<dict>`, "readValue.PlistInvalid"},
		{"a dict whose value is missing", plistHead + `<dict><key>a</key>`, "readValue->nextValue.PlistInvalid"},
		{"an array that is not closed", plistHead + `<dict><key>a</key><array>`, "readValue.PlistInvalid"},
		{"a string that is not closed", plistHead + `<dict><key>a</key><string>x`, "text.PlistInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.text))
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tc.route)
			}
		})
	}
}
