package lsp

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/errors"
)

func TestDocumentURI(t *testing.T) {
	tests := []struct {
		name string
		uri  document.URI
		want string
	}{
		{"a file", document.FileURI("/home/me/main.go"), "file:///home/me/main.go"},
		{"a space is encoded", document.FileURI("/home/me/my file.go"), "file:///home/me/my%20file.go"},
		{"hash and question mark are encoded", document.FileURI("/tmp/a#b?c.go"), "file:///tmp/a%23b%3Fc.go"},
		{"a percent sign is encoded", document.FileURI("/tmp/100%.txt"), "file:///tmp/100%25.txt"},
		{"non-ASCII is encoded", document.FileURI("/tmp/é€.go"), "file:///tmp/%C3%A9%E2%82%AC.go"},
		{"an untitled document", document.UntitledURI(3), "untitled:Untitled-3"},
		{"generated content", document.VirtualURI("/defaultSettings.json"), "emetos-virtual:/defaultSettings.json"},
		// the class files of Eclipse JDT LS: the whole URI after jdt:// is kept, query and escapes included
		{"a class file of the server", document.VirtualURI("/jdt/contents/java.base/java.lang/String.class?=app/%5C/usr%5C/lib%5C/jvm%3Cjava.lang(String.class"),
			"jdt://contents/java.base/java.lang/String.class?=app/%5C/usr%5C/lib%5C/jvm%3Cjava.lang(String.class"},
		// the decompiled sources of csharp-ls: csharp: and one slash, then the absolute path of the project
		{"decompiled source of the server", document.VirtualURI("/csharp/home/me/app/App.csproj/decompiled/System.Console.cs"),
			"csharp:/home/me/app/App.csproj/decompiled/System.Console.cs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DocumentURI(tt.uri)
			if got != tt.want {
				t.Errorf("DocumentURI = %q, want %q", got, tt.want)
			}
			back, err := ParseDocumentURI(got)
			if err != nil {
				t.Fatal(err)
			}
			if back != tt.uri {
				t.Errorf("ParseDocumentURI(%q) = %+v, want %+v", got, back, tt.uri)
			}
		})
	}
}

func TestParseDocumentURI(t *testing.T) {
	tests := []struct {
		in   string
		want document.URI
	}{
		{"file:///a/b.go", document.FileURI("/a/b.go")},
		{"file://localhost/a/b.go", document.FileURI("/a/b.go")},
		{"file:///a/../b.go", document.FileURI("/b.go")},
		{"untitled:1", document.UntitledURI(1)},
		{"untitled:Untitled-12", document.UntitledURI(12)},
	}
	for _, tt := range tests {
		got, err := ParseDocumentURI(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseDocumentURI(%q) = %+v, %v, want %+v", tt.in, got, err, tt.want)
		}
	}
}

func TestProvidedPath(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"jdt://contents/a/B.class?=x", "/jdt/contents/a/B.class?=x", true},
		{"jdt://", "", false},
		{"jdt:contents/a", "", false},
		{"csharp:/home/me/App.csproj/decompiled/System.Console.cs", "/csharp/home/me/App.csproj/decompiled/System.Console.cs", true},
		{"csharp:/p/App.csproj/generated/Hint.g.cs", "/csharp/p/App.csproj/generated/Hint.g.cs", true},
		{"csharp:/", "", false},
		{"csharp://home/me/App.csproj", "", false},
		{"csharp:home/me", "", false},
		{"jar:file:///a.jar!/B.class", "", false},
		{"file:///a/b.go", "", false},
	}
	for _, tt := range tests {
		got, ok := ProvidedPath(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ProvidedPath(%q) = %q, %v, want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseDocumentURIErrors(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		route string
	}{
		{"not a URL", "%zz", "ParseDocumentURI.DocumentURIInvalid"},
		{"a relative path", "file://a/b.go", "ParseDocumentURI.DocumentURIInvalid"},
		{"a host that is not local", "file://server/a/b.go", "ParseDocumentURI.DocumentURIInvalid"},
		{"an untitled name that is not a number", "untitled:foo", "ParseDocumentURI.DocumentURIInvalid"},
		{"untitled zero", "untitled:Untitled-0", "ParseDocumentURI.DocumentURIInvalid"},
		{"a scheme the editor has no documents for", "jar:file:///a.jar!/B.class", "ParseDocumentURI.DocumentURIUnsupported"},
		{"no scheme", "/a/b.go", "ParseDocumentURI.DocumentURIUnsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDocumentURI(tt.in)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want %s", appErr.Route(), tt.route)
			}
		})
	}
}
