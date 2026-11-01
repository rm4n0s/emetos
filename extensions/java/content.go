package java

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The class files (SPEC §10.34 "Java"). A definition in the JDK or in a library is a location with a jdt://contents/... URI, which the
// editor turns into the read-only document "emetos-virtual:/jdt/contents/..." (internal/core/lsp ProvidedPath). This provider makes its
// text: the source of the class when the server has it, else what it decompiles, asked of the server with java/classFileContents.

// ContentScheme is the scheme of the documents the pack provides, the scheme of the URIs jdtls uses for class files.
const ContentScheme = "jdt"

// provideContent registers the provider. A scope that cannot show documents has none.
func (j *java) provideContent() {
	langserver.ProvideContent(j.api, "the class files of jdtls", extension.ContentProvider{
		Scheme:   ContentScheme,
		Label:    classLabel,
		Language: func(string) string { return "java" },
		Text:     j.classText,
	})
}

// serverURI is the URI jdtls knows a document of the provider by: jdt:// and the rest of the path as it is.
func serverURI(uri string) (string, bool) {
	scheme, path, ok := extension.SplitContentURI(uri)
	if !ok || scheme != ContentScheme || path == "" {
		return "", false
	}
	return ContentScheme + "://" + path, true
}

// classLabel is the name of the tab of a class file: the last element of its path, String.class.
func classLabel(uri string) string {
	_, path, ok := extension.SplitContentURI(uri)
	if !ok {
		return uri
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	return path
}

// classText is ContentProvider.Text: it runs off the UI thread. A server that is not running, or does not know the class, is an error
// the window shows with the name of the document.
func (j *java) classText(ctx context.Context, uri string) (string, error) {
	server, ok := serverURI(uri)
	if !ok {
		return "", errors.New("JavaContentInvalid", "this is not the address of a class file of jdtls", "uri", uri)
	}
	result, err := j.api.LanguageServers().Request(ctx, ServerID, classFileContentsMethod, map[string]any{"uri": server})
	if err != nil {
		return "", err
	}
	var text string
	if json.Unmarshal(result, &text) != nil || text == "" {
		return "", errors.New("JavaClassFileEmpty", "jdtls has no source or decompiled text for this class", "uri", server)
	}
	return text, nil
}
