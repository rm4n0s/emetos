package quickinput

import (
	"testing"

	"github.com/rm4n0s/errors"
)

// stub is a provider that does nothing.
type stub struct{ info ProviderInfo }

func (s stub) Info() ProviderInfo       { return s.info }
func (s stub) Provide(*Session) Handler { return BaseHandler{} }

func TestRegistryResolve(t *testing.T) {
	tests := []struct {
		name     string
		prefixes []string
		value    string
		want     string
		found    bool
	}{
		{"commands", []string{">", ":", "?", "view "}, ">line", ">", true},
		{"go to line", []string{">", ":", "?", "view "}, ":12", ":", true},
		{"the longest prefix wins", []string{":", "::"}, "::5", "::", true},
		{"a shorter one when the longer does not match", []string{":", "::"}, ":5", ":", true},
		{"view needs its space", []string{"view ", "v"}, "view x", "view ", true},
		{"v is another provider", []string{"view ", "v"}, "vie", "v", true},
		{"view without the space is no match", []string{"view "}, "view", "", false},
		{"no provider", []string{">"}, "line", "", false},
		{"the default provider takes the rest", []string{">", ""}, "file", "", true},
		{"a prefix beats the default", []string{">", ""}, ">x", ">", true},
		{"the empty value goes to the default", []string{">", ""}, "", "", true},
		{"the empty value with no default", []string{">"}, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			for _, p := range tc.prefixes {
				r.MustRegister(stub{ProviderInfo{Prefix: p}})
			}
			got := r.Resolve(tc.value)
			if (got != nil) != tc.found {
				t.Fatalf("found %v, want %v", got != nil, tc.found)
			}
			if got != nil && got.Info().Prefix != tc.want {
				t.Errorf("prefix %q, want %q", got.Info().Prefix, tc.want)
			}
		})
	}
}

func TestRegistryRegisterFailures(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(stub{ProviderInfo{Prefix: ">"}})
	tests := []struct {
		name  string
		p     Provider
		route string
	}{
		{"duplicate prefix", stub{ProviderInfo{Prefix: ">"}}, "Registry.Register.QuickAccessPrefixDuplicate"},
		{"nil provider", nil, "Registry.Register.QuickAccessProviderInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := r.Register(tc.p)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	t.Run("must register panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Errorf("no panic")
			}
		}()
		r.MustRegister(stub{ProviderInfo{Prefix: ">"}})
	})
}

func TestRegistryProvidersAreOrderedByPrefix(t *testing.T) {
	r := NewRegistry()
	for _, p := range []string{"view ", ">", "", "::", ":", "?"} {
		r.MustRegister(stub{ProviderInfo{Prefix: p}})
	}
	var got []string
	for _, p := range r.Providers() {
		got = append(got, p.Info().Prefix)
	}
	// a string sorts by its bytes: '>' (0x3E) and '?' (0x3F) come after ':' (0x3A)
	want := []string{"", ":", "::", ">", "?", "view "}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	r.Unregister(">")
	if p := r.Resolve(">x"); p == nil || p.Info().Prefix != "" {
		t.Errorf("the provider is still there: %v", p)
	}
}
