package csharp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The decompiled and generated sources (SPEC §10.34 "C#"). With the client capability experimental.csharp.metadataUris, csharp-ls answers
// a definition in a .NET library with csharp:/<project.csproj>/decompiled/<Symbol>.cs and one in a file a source generator made with
// csharp:/<project.csproj>/generated/<HintName>, and csharp/metadata gives their text. The editor turns the URI into the read-only document
// "emetos-virtual:/csharp/<rest>" (internal/core/lsp ProvidedPath); this provider makes its text.

// ContentScheme is the scheme of the documents the pack provides: the scheme of the URIs csharp-ls uses.
const ContentScheme = "csharp"

// metadataMethod is the request of the server that gives the text of a csharp:/ document.
const metadataMethod = "csharp/metadata"

// provideContent registers the provider. A scope that cannot show documents has none.
func (c *csharp) provideContent() {
	c.reg = langserver.ProvideContent(c.api, "the decompiled sources of csharp-ls", extension.ContentProvider{
		Scheme:   ContentScheme,
		Label:    metadataLabel,
		Language: func(string) string { return "csharp" },
		Text:     c.metadataText,
	})
}

// serverURI is the URI csharp-ls knows a document of the provider by: csharp:/ and the rest of the path as it is.
func serverURI(uri string) (string, bool) {
	scheme, path, ok := extension.SplitContentURI(uri)
	if !ok || scheme != ContentScheme || path == "" || strings.HasPrefix(path, "/") {
		return "", false
	}
	return ContentScheme + ":/" + path, true
}

// metadataLabel is the name of the tab of a decompiled source: the last element of its path, System.Console.cs.
func metadataLabel(uri string) string {
	_, path, ok := extension.SplitContentURI(uri)
	if !ok {
		return uri
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	return path
}

// metadataText is ContentProvider.Text: it runs off the UI thread. A server that is not running, or does not know the document (it was
// restarted since the definition was asked), is an error the window shows with the name of the document.
func (c *csharp) metadataText(ctx context.Context, uri string) (string, error) {
	server, ok := serverURI(uri)
	if !ok {
		return "", errors.New("CSharpContentInvalid", "this is not the address of a decompiled source of csharp-ls", "uri", uri)
	}
	result, err := c.api.LanguageServers().Request(ctx, ServerID, metadataMethod, map[string]any{"textDocument": map[string]any{"uri": server}})
	if err != nil {
		return "", err
	}
	var reply struct {
		Source string `json:"source"`
	}
	if json.Unmarshal(result, &reply) != nil || reply.Source == "" {
		return "", errors.New("CSharpMetadataEmpty", "csharp-ls has no decompiled or generated source for this document", "uri", server)
	}
	return reply.Source, nil
}
