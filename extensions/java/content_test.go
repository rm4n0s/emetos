package java

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The class files of jdtls as read-only documents: the provider of the scheme jdt over the fake API.

// classURI is the document of a class file as the editor names it: jdt://contents/... after emetos-virtual:/jdt/.
const classPath = "contents/java.base/java.lang/String.class?=app/%5C/usr%5C/lib%5C/jvm%5C/java-25%5C/lib%5C/jrt-fs.jar=/module=/java.base%3Cjava.lang(String.class"

func TestTheProviderMakesTheTextOfAClassFileWithTheRequestOfTheServer(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.answer(map[string]string{classFileContentsMethod: `"public final class String {}\n"`})
	uri := extension.ContentURI(ContentScheme, classPath)
	text, err := r.api.ContentText(context.Background(), uri)
	if err != nil || text != "public final class String {}\n" {
		t.Fatalf("text = %q, %v", text, err)
	}
	want := `{"uri":"jdt://` + strings.ReplaceAll(classPath, "&", `&`) + `"}`
	if got := r.requests(classFileContentsMethod); len(got) != 1 || got[0] != want {
		t.Errorf("requests = %v, want %s: the address goes back to the server as it came", got, want)
	}
	if got := r.api.ContentLabel(uri); got != "String.class" {
		t.Errorf("label = %q", got)
	}
	if got := r.api.ContentLanguage(uri); got != "java" {
		t.Errorf("language = %q", got)
	}
}

func TestTheLabelOfAClassFileIsItsName(t *testing.T) {
	tests := []struct{ path, want string }{
		{"contents/java.base/java.util/List.class?=x/y", "List.class"},
		{"contents/a/b/Map$Entry.class", "Map$Entry.class"},
		{"List.class", "List.class"},
		{"contents/a/B.class?=p/q/r", "B.class"},
	}
	for _, tt := range tests {
		if got := classLabel(extension.ContentURI(ContentScheme, tt.path)); got != tt.want {
			t.Errorf("classLabel(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
	if got := classLabel("not a content address"); got != "not a content address" {
		t.Errorf("classLabel of an address that is no document = %q", got)
	}
}

func TestAClassFileTheServerCannotGiveIsAnError(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	for _, answer := range []string{"null", `""`, "42"} {
		r.answer(map[string]string{classFileContentsMethod: answer})
		_, err := r.api.ContentText(context.Background(), extension.ContentURI(ContentScheme, classPath))
		wantRoute(t, err, "classText.JavaClassFileEmpty")
	}
	// the server's own error goes up untouched
	r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) {
		return nil, errors.New("TestBoom", "boom")
	})
	_, err := r.api.ContentText(context.Background(), extension.ContentURI(ContentScheme, classPath))
	wantRoute(t, err, "TestBoom")
	// a document that is not the address of a class file
	for _, uri := range []string{extension.ContentURI(ContentScheme, ""), "emetos-virtual:/other/x", "file:///a.java"} {
		_, err = r.api.ContentText(context.Background(), uri)
		if err == nil {
			t.Errorf("%s: no error", uri)
		}
	}
	_, err = (&java{}).classText(context.Background(), extension.ContentURI("other", "x"))
	wantRoute(t, err, "classText.JavaContentInvalid")
}

func TestAScopeThatCannotShowDocumentsLogsNoWarning(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
		warn bool
	}{
		{"a scope with no documents", errors.New("ExtensionDocumentUnavailable", "no documents here"), "are not available in this scope", false},
		{"another failure", errors.New("ExtensionContentProviderInvalid", "invalid"), "are not available", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf logged
			r := newRigWith(t, rigOptions{javaVersion: "21"}, func(a *fakeapi.API) {
				a.SetLogger(newLogger(&buf))
				a.FailProvideContent(tt.err)
			})
			if !strings.Contains(buf.text(), tt.want) || strings.Contains(buf.text(), "level=WARN") != tt.warn {
				t.Errorf("log:\n%s", buf.text())
			}
			if r.api.ContentProviderFor(ContentScheme) != nil {
				t.Error("a provider is registered")
			}
		})
	}
}
