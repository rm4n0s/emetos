package csharp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"strings"
)

// The decompiled and generated sources of csharp-ls as read-only documents: the provider of the scheme csharp over the fake API.

// the path of a document as the editor names it: csharp:/ after emetos-virtual:/csharp/
const (
	decompiledPath = "home/me/app/App/App.csproj/decompiled/System.Console.cs"
	generatedPath  = "home/me/app/App/App.csproj/generated/Hint.g.cs"
)

// answer sets the answers of the server to the requests of the pack, by method (a JSON text).
func (r *rig) answer(answers map[string]string) {
	r.api.OnServerRequest(ServerID, func(req fakeapi.ServerRequest) (json.RawMessage, error) {
		if a, ok := answers[req.Method]; ok {
			return json.RawMessage(a), nil
		}
		return json.RawMessage("null"), nil
	})
}

func TestTheProviderMakesTheTextOfADecompiledSourceWithTheRequestOfTheServer(t *testing.T) {
	r := newRig(t, rigOptions{})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.answer(map[string]string{metadataMethod: `{"projectName":"App","assemblyName":"System.Console","symbolName":"System.Console","source":"namespace System;\npublic static class Console {}\n"}`})
	uri := extension.ContentURI(ContentScheme, decompiledPath)
	text, err := r.api.ContentText(context.Background(), uri)
	if err != nil || text != "namespace System;\npublic static class Console {}\n" {
		t.Fatalf("text = %q, %v", text, err)
	}
	var got []string
	for _, req := range r.api.ServerRequests(ServerID) {
		if req.Method == metadataMethod {
			got = append(got, string(req.Params))
		}
	}
	want := `{"textDocument":{"uri":"csharp:/` + decompiledPath + `"}}`
	if len(got) != 1 || got[0] != want {
		t.Errorf("requests = %v, want %s: the address goes back to the server as it came", got, want)
	}
	if label := r.api.ContentLabel(uri); label != "System.Console.cs" {
		t.Errorf("label = %q", label)
	}
	if lang := r.api.ContentLanguage(uri); lang != "csharp" {
		t.Errorf("language = %q", lang)
	}
	// a file a source generator made
	r.answer(map[string]string{metadataMethod: `{"projectName":"App","assemblyName":"App","symbolName":"Hint.g.cs","source":"// generated\n"}`})
	if text, err := r.api.ContentText(context.Background(), extension.ContentURI(ContentScheme, generatedPath)); err != nil || text != "// generated\n" {
		t.Errorf("generated text = %q, %v", text, err)
	}
}

func TestTheLabelOfADecompiledSourceIsItsName(t *testing.T) {
	tests := []struct{ path, want string }{
		{decompiledPath, "System.Console.cs"},
		{"p/App.csproj/generated/Hint.g.cs", "Hint.g.cs"},
		{"Single.cs", "Single.cs"},
	}
	for _, tt := range tests {
		if got := metadataLabel(extension.ContentURI(ContentScheme, tt.path)); got != tt.want {
			t.Errorf("metadataLabel(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
	if got := metadataLabel("not a content address"); got != "not a content address" {
		t.Errorf("metadataLabel of an address that is no document = %q", got)
	}
}

func TestADocumentTheServerCannotGiveIsAnError(t *testing.T) {
	r := newRig(t, rigOptions{})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	for _, answer := range []string{"null", `{}`, `{"source":""}`, `"text"`, `42`} {
		r.answer(map[string]string{metadataMethod: answer})
		_, err := r.api.ContentText(context.Background(), extension.ContentURI(ContentScheme, decompiledPath))
		wantRoute(t, err, "metadataText.CSharpMetadataEmpty")
	}
	// the server's own error goes up untouched
	r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) {
		return nil, errors.New("TestBoom", "boom")
	})
	_, err := r.api.ContentText(context.Background(), extension.ContentURI(ContentScheme, decompiledPath))
	wantRoute(t, err, "TestBoom")
	// a document that is not the address of a source of csharp-ls
	for _, uri := range []string{extension.ContentURI(ContentScheme, ""), "emetos-virtual:/csharp//double/slash", "emetos-virtual:/other/x", "file:///a.cs"} {
		if _, err = r.api.ContentText(context.Background(), uri); err == nil {
			t.Errorf("%s: no error", uri)
		}
	}
	_, err = (&csharp{}).metadataText(context.Background(), extension.ContentURI("other", "x"))
	wantRoute(t, err, "metadataText.CSharpContentInvalid")
	_, err = (&csharp{}).metadataText(context.Background(), "emetos-virtual:/csharp//p/x.cs")
	wantRoute(t, err, "metadataText.CSharpContentInvalid")
}

func TestAServerThatIsNotRunningHasNoSources(t *testing.T) {
	r := newRig(t, rigOptions{})
	_, err := r.api.ContentText(context.Background(), extension.ContentURI(ContentScheme, decompiledPath))
	wantRoute(t, err, "ExtensionServerNotRunning")
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
			r := newRigWith(t, rigOptions{}, func(a *fakeapi.API) {
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
