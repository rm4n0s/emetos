package format

import (
	"strings"
	"testing"
)

func TestSafeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Get User", "get-user"},
		{"  Get   User  ", "get-user"},
		{"a/b\\c:d", "a-b-c-d"},
		{"../etc/passwd", "etc-passwd"},
		{"..", "untitled"},
		{".", "untitled"},
		{"", "untitled"},
		{"***", "untitled"},
		{"snake_case 2", "snake_case-2"},
		{"Ελληνικά Αίτημα", "ελληνικά-αίτημα"},
		{"v1.2.3", "v1-2-3"},
		{"already-safe", "already-safe"},
		{"-lead-and-trail-", "lead-and-trail"},
		{"tab\tand\nline", "tab-and-line"},
		{"nul\x00byte", "nul-byte"},
		{strings.Repeat("a", 300), strings.Repeat("a", MaxNameLength)},
		{strings.Repeat("é", 300), strings.Repeat("é", MaxNameLength/2)},
		{strings.Repeat("a", 99) + " b", strings.Repeat("a", 99)},
	}
	for _, tt := range tests {
		got := SafeName(tt.in)
		if got != tt.want {
			t.Errorf("SafeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if len(got) > MaxNameLength || strings.ContainsAny(got, "/\\.") || got == "" {
			t.Errorf("SafeName(%q) = %q is not safe", tt.in, got)
		}
	}
}

func TestUniqueName(t *testing.T) {
	tests := []struct {
		name  string
		taken []string
		want  string
	}{
		{"free", nil, "get-user"},
		{"one clash", []string{"get-user"}, "get-user-2"},
		{"two clashes", []string{"get-user", "get-user-2"}, "get-user-3"},
		{"a gap is used", []string{"get-user", "get-user-3"}, "get-user-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UniqueName("get-user", func(n string) bool {
				for _, x := range tt.taken {
					if x == n {
						return true
					}
				}
				return false
			})
			if got != tt.want {
				t.Errorf("UniqueName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileNames(t *testing.T) {
	if got := RequestFileName("get-user"); got != "get-user.request.jsonc" {
		t.Errorf("RequestFileName = %q", got)
	}
	for in, want := range map[string]bool{"a.request.jsonc": true, ".request.jsonc": false, "a.jsonc": false, "collection.jsonc": false} {
		if _, ok := RequestStem(in); ok != want {
			t.Errorf("RequestStem(%q) ok = %v", in, ok)
		}
	}
	if stem, _ := RequestStem("a.b.request.jsonc"); stem != "a.b" {
		t.Errorf("stem = %q", stem)
	}
	if got := EnvironmentFileName("dev"); got != "dev.jsonc" {
		t.Errorf("EnvironmentFileName = %q", got)
	}
	tests := []struct{ typ, ext, file string }{
		{BodyJSON, ".json", "r.body.json"}, {BodyXML, ".xml", "r.body.xml"}, {BodyText, ".txt", "r.body.txt"},
		{BodyHTML, ".html", "r.body.html"}, {BodyJavaScript, ".js", "r.body.js"}, {BodyGraphQL, ".graphql", "r.body.graphql"},
		{BodyNone, "", ""}, {BodyForm, "", ""}, {BodyMultipart, "", ""}, {BodyBinary, "", ""},
	}
	for _, tt := range tests {
		if got := BodyExtension(tt.typ); got != tt.ext {
			t.Errorf("BodyExtension(%s) = %q", tt.typ, got)
		}
		if got := BodyFileName("r", tt.typ); got != tt.file {
			t.Errorf("BodyFileName(%s) = %q", tt.typ, got)
		}
	}
	if got := VariablesFileName("r"); got != "r.variables.json" {
		t.Errorf("VariablesFileName = %q", got)
	}
	for in, want := range map[string]bool{"a.json": true, "": false, ".": false, "..": false, "a/b": false, "a\\b": false, "a\x00": false} {
		if got := ValidSiblingName(in); got != want {
			t.Errorf("ValidSiblingName(%q) = %v", in, got)
		}
	}
}
