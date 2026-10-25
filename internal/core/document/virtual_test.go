package document

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func routeIs(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
	}
}

func TestVirtualRegistry(t *testing.T) {
	ctx := context.Background()
	r := NewVirtualRegistry()
	n := 0
	provider := VirtualProvider{Label: "Default Settings", Language: "jsonc", Text: func(context.Context) (string, error) {
		n++
		return "// run " + string(rune('0'+n)) + "\r\n{}\r\n", nil
	}}
	if err := r.Register("/defaultSettings.json", provider); err != nil {
		t.Fatal(err)
	}
	if !r.Has(VirtualURI("/defaultSettings.json")) || r.Has(VirtualURI("/other")) || r.Has(FileURI("/defaultSettings.json")) {
		t.Error("Has")
	}
	if got := r.Paths(); !reflect.DeepEqual(got, []string{"/defaultSettings.json"}) {
		t.Errorf("Paths %v", got)
	}
	// the content is made again each time, with "\n" lines
	for i := 1; i <= 2; i++ {
		c, err := r.Generate(ctx, VirtualURI("/defaultSettings.json"))
		if err != nil {
			t.Fatal(err)
		}
		want := VirtualContent{Text: "// run " + string(rune('0'+i)) + "\n{}\n", Label: "Default Settings", Language: "jsonc"}
		if c != want {
			t.Errorf("content %+v, want %+v", c, want)
		}
	}
	// a provider without a language makes plain text
	if err := r.Register("/plain", VirtualProvider{Text: func(context.Context) (string, error) { return "x", nil }}); err != nil {
		t.Fatal(err)
	}
	if c, _ := r.Generate(ctx, VirtualURI("/plain")); c.Language != DefaultLanguage {
		t.Errorf("language %q", c.Language)
	}
}

func TestVirtualRegistryErrors(t *testing.T) {
	ctx := context.Background()
	r := NewVirtualRegistry()
	text := func(context.Context) (string, error) { return "x", nil }
	if err := r.Register("/a", VirtualProvider{Text: text}); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("Boom", "the provider broke")
	if err := r.Register("/broken", VirtualProvider{Text: func(context.Context) (string, error) { return "", boom }}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		err   func() error
		route string
	}{
		{"no path", func() error { return r.Register("", VirtualProvider{Text: text}) }, "VirtualRegistry.Register.VirtualProviderInvalid"},
		{"no function", func() error { return r.Register("/b", VirtualProvider{}) }, "VirtualRegistry.Register.VirtualProviderInvalid"},
		{"duplicate", func() error { return r.Register("/a", VirtualProvider{Text: text}) }, "VirtualRegistry.Register.VirtualProviderDuplicate"},
		{"unknown path", func() error { _, err := r.Generate(ctx, VirtualURI("/zzz")); return err }, "VirtualRegistry.Generate.VirtualURIUnknown"},
		{"not virtual", func() error { _, err := r.Generate(ctx, FileURI("/a")); return err }, "VirtualRegistry.Generate.VirtualURIUnknown"},
		{"the provider fails", func() error { _, err := r.Generate(ctx, VirtualURI("/broken")); return err }, "VirtualRegistry.Generate.VirtualGenerateFailed"},
	} {
		t.Run(c.name, func(t *testing.T) { routeIs(t, c.err(), c.route) })
	}
	_, err := r.Generate(ctx, VirtualURI("/broken"))
	if appErr, _ := errors.FromError(err); !errors.Is(appErr, boom) || !strings.Contains(appErr.Error(), "broke") {
		t.Errorf("the cause is lost: %v", err)
	}
}

func TestVirtualDocumentIsReadOnly(t *testing.T) {
	doc := NewVirtual(VirtualURI("/defaultSettings.json"), VirtualContent{Text: "{\n}\n", Label: "Default Settings", Language: "jsonc"})
	if !doc.ReadOnly() || doc.Dirty() || doc.Label() != "Default Settings" || doc.LanguageID() != "jsonc" || doc.Text() != "{\n}\n" {
		t.Fatalf("document: readonly %v dirty %v label %q language %q", doc.ReadOnly(), doc.Dirty(), doc.Label(), doc.LanguageID())
	}
	_, err := doc.Edit([]Edit{{Text: "x"}}, EditOptions{})
	routeIs(t, err, "Document.Edit->Document.checkWritable.DocumentReadOnly")
	if doc.Text() != "{\n}\n" || doc.Dirty() {
		t.Errorf("the document changed: %q dirty %v", doc.Text(), doc.Dirty())
	}
	_, err = doc.BeginSave(SaveOptions{})
	routeIs(t, err, "SaveReadOnlyDocument")
	// the content may be replaced by the code that generates it, and the document stays clean
	doc.SetText("{\n\t// new\n}\n")
	if doc.Dirty() || doc.Text() != "{\n\t// new\n}\n" {
		t.Errorf("after SetText: %q dirty %v", doc.Text(), doc.Dirty())
	}
}

func TestVirtualPrefixProviders(t *testing.T) {
	ctx := context.Background()
	r := NewVirtualRegistry()
	say := func(who string) func(context.Context, string) (string, error) {
		return func(_ context.Context, rest string) (string, error) { return who + ":" + rest + "\r\nend", nil }
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.RegisterPrefix("/saved/", VirtualPrefixProvider{
		Label:    func(rest string) string { return "label " + rest },
		Language: func(rest string) string { return "lang-" + rest },
		Text:     say("saved"),
	}))
	must(r.RegisterPrefix("/saved/deep/", VirtualPrefixProvider{Text: say("deep")}))
	must(r.Register("/saved/exact", VirtualProvider{Label: "Exact", Text: func(context.Context) (string, error) { return "one", nil }}))
	for _, c := range []struct {
		name                  string
		uri                   URI
		has                   bool
		text, label, language string
	}{
		{"the rest of the path", VirtualURI("/saved/home/a.go"), true, "saved:home/a.go\nend", "label home/a.go", "lang-home/a.go"},
		{"the longer prefix wins", VirtualURI("/saved/deep/x"), true, "deep:x\nend", "x", DefaultLanguage},
		{"a document of its own wins over the prefix", VirtualURI("/saved/exact"), true, "one", "Exact", DefaultLanguage},
		{"nothing after the prefix", VirtualURI("/saved/"), false, "", "", ""},
		{"another folder", VirtualURI("/other/x"), false, "", "", ""},
		{"not virtual", FileURI("/saved/x"), false, "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := r.Has(c.uri); got != c.has {
				t.Errorf("Has = %v", got)
			}
			got, err := r.Generate(ctx, c.uri)
			if !c.has {
				routeIs(t, err, "VirtualRegistry.Generate.VirtualURIUnknown")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Text != c.text || got.Label != c.label || got.Language != c.language {
				t.Errorf("got %+v, want text %q label %q language %q", got, c.text, c.label, c.language)
			}
		})
	}
	if paths := r.Paths(); len(paths) != 1 || paths[0] != "/saved/exact" {
		t.Errorf("paths %v: the prefixes are not paths", paths)
	}
}

func TestVirtualPrefixErrors(t *testing.T) {
	ctx := context.Background()
	r := NewVirtualRegistry()
	text := func(context.Context, string) (string, error) { return "x", nil }
	if err := r.RegisterPrefix("/a/", VirtualPrefixProvider{Text: text}); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("Boom", "the provider broke")
	if err := r.RegisterPrefix("/broken/", VirtualPrefixProvider{Text: func(context.Context, string) (string, error) { return "", boom }}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		err   func() error
		route string
	}{
		{"no slash at the start", func() error { return r.RegisterPrefix("a/", VirtualPrefixProvider{Text: text}) }, "VirtualRegistry.RegisterPrefix.VirtualPrefixInvalid"},
		{"no slash at the end", func() error { return r.RegisterPrefix("/a", VirtualPrefixProvider{Text: text}) }, "VirtualRegistry.RegisterPrefix.VirtualPrefixInvalid"},
		{"only a slash", func() error { return r.RegisterPrefix("/", VirtualPrefixProvider{Text: text}) }, "VirtualRegistry.RegisterPrefix.VirtualPrefixInvalid"},
		{"no function", func() error { return r.RegisterPrefix("/b/", VirtualPrefixProvider{}) }, "VirtualRegistry.RegisterPrefix.VirtualPrefixInvalid"},
		{"duplicate", func() error { return r.RegisterPrefix("/a/", VirtualPrefixProvider{Text: text}) }, "VirtualRegistry.RegisterPrefix.VirtualPrefixDuplicate"},
		{"the provider fails", func() error { _, err := r.Generate(ctx, VirtualURI("/broken/x")); return err }, "VirtualRegistry.Generate.VirtualGenerateFailed"},
	} {
		t.Run(c.name, func(t *testing.T) { routeIs(t, c.err(), c.route) })
	}
}
