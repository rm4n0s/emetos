package extension

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestParseCommandLink(t *testing.T) {
	tests := []struct {
		name, dest string
		id         string
		args       any
		route      string // the route of the error, empty for none
	}{
		{"no arguments", "command:git.viewCommit", "git.viewCommit", nil, ""},
		{"an empty query", "command:git.viewCommit?", "git.viewCommit", nil, ""},
		{"an empty list", "command:x?%5B%5D", "x", nil, ""},
		{"one argument is passed as it is", "command:x?%5B%22abc%22%5D", "x", "abc", ""},
		{"an object", "command:x?%5B%7B%22id%22%3A%22abc%22%7D%5D", "x", map[string]any{"id": "abc"}, ""},
		{"two arguments are a list", "command:x?%5B1%2C%22b%22%5D", "x", []any{float64(1), "b"}, ""},
		{"a value that is not a list", "command:x?%22plain%22", "x", "plain", ""},
		{"a plus is a plus", "command:x?%5B%22a+b%22%5D", "x", "a+b", ""},
		{"spaces and non-ASCII", "command:x?%5B%22d%C3%A9j%C3%A0%20vu%22%5D", "x", "déjà vu", ""},
		{"not a command link", "https://example.com", "", nil, "ParseCommandLink.ExtensionCommandLinkInvalid"},
		{"no command", "command:", "", nil, "ParseCommandLink.ExtensionCommandLinkInvalid"},
		{"no command but arguments", "command:?%5B%5D", "", nil, "ParseCommandLink.ExtensionCommandLinkInvalid"},
		{"a space in the id", "command:a%20b", "a%20b", nil, ""},
		{"arguments that are not JSON", "command:x?%7Bnope", "", nil, "ParseCommandLink.ExtensionCommandLinkInvalid"},
		{"a bad escape", "command:x?%zz", "", nil, "ParseCommandLink.ExtensionCommandLinkInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, args, err := ParseCommandLink(tc.dest)
			if tc.route != "" {
				appErr, ok := errors.FromError(err)
				if !ok || !appErr.HasRoute(tc.route) {
					t.Fatalf("error %v, want the route %s", err, tc.route)
				}
				return
			}
			if err != nil || id != tc.id || !reflect.DeepEqual(args, tc.args) {
				t.Errorf("got %q %#v %v; want %q %#v", id, args, err, tc.id, tc.args)
			}
		})
	}
}
