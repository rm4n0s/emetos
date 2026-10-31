package extension

import (
	"context"
	"strings"
)

// ContentScheme is the start of the URI of the read-only documents an extension provides (SPEC §7.1, §10.30): the document of the
// scheme "git" with the path "abc/HEAD/src/a.go" is "emetos-virtual:/git/abc/HEAD/src/a.go". The scheme is also what the context
// key resourceScheme holds for such a document, so that the when-clauses of VS Code's git extension (resourceScheme == git) work.
const ContentScheme = "emetos-virtual:"

// ContentURI is the URI of the document of a scheme and a path: ContentURI("git", "abc/HEAD/a.go") is "emetos-virtual:/git/abc/HEAD/a.go".
func ContentURI(scheme, path string) string {
	return ContentScheme + "/" + scheme + "/" + strings.TrimPrefix(path, "/")
}

// SplitContentURI is the reverse of ContentURI: the scheme and the path of a URI of provided content; false for any other URI.
func SplitContentURI(uri string) (scheme, path string, ok bool) {
	rest, ok := strings.CutPrefix(uri, ContentScheme+"/")
	if !ok {
		return "", "", false
	}
	scheme, path, ok = strings.Cut(rest, "/")
	if !ok || scheme == "" {
		return "", "", false
	}
	return scheme, path, true
}

// TagContentBinary is the tag of the error that a ContentProvider's Text returns for content that is not text. The window shows the
// notice it shows for a file that looks binary ("The file is not displayed in the editor because it is either binary or uses an
// unsupported text encoding.") and opens nothing. An extension makes the error with its own tag name and passes the tag
// "ContentBinary"; the window finds it with errors.Is on a value of this tag.
const TagContentBinary = "ContentBinary"

// ContentProvider makes the text of the read-only documents of one scheme (SPEC §10.30 "Diffs of changes"; VS Code's
// TextDocumentContentProvider). The documents are opened with Documents().OpenDiff and OpenDocument, never dirty, never saved,
// never backed up; after a restart a restored tab asks the provider again, and a tab whose provider is not there is dropped without
// a word.
type ContentProvider struct {
	// Scheme names the documents: a word of letters, digits and hyphens, not the scheme of a core document ("file", "untitled",
	// "emetos-virtual"), unique among the providers of the window tab. The URIs are ContentURI(Scheme, path).
	Scheme string
	// Label gives the name of the document in its tab (the name of the file, "main.go"); the last element of the path when nil.
	Label func(uri string) string
	// Language gives the id of the language of the document; when nil or when it answers "" the language is detected from the
	// label, as for a file of that name.
	Language func(uri string) string
	// Text makes the text of the document. It runs off the UI thread, each time the document is opened and each time Changed says it
	// changed, and must stop when ctx does. A failure is reported to the user, with its message; the error with the tag
	// TagContentBinary is the notice of a binary file. The text may have any line ending; the document is shown with "\n".
	Text func(ctx context.Context, uri string) (string, error)
}

// ContentRegistration is a registered ContentProvider.
type ContentRegistration interface {
	// Changed tells the window that the text of the documents with these URIs may have changed (the index moved): the open ones are
	// made again, off the UI thread, and the diff editors that show them are computed again. A URI that is not open costs nothing.
	Changed(uris ...string)
	// Dispose takes the provider away; it happens by itself when the activation ends. The tabs that show its documents stay
	// open with the text they have.
	Dispose()
}

// DiffSpec describes a diff editor to open (Documents().OpenDiff; VS Code's vscode.diff).
type DiffSpec struct {
	// Left and Right are the two documents: a file (file:///path), an open untitled document, or a provided one (ContentURI). A
	// provided one is made first, off the UI thread, and the diff opens when both are there.
	Left, Right string
	// Title is the name of the tab ("loader.go (Working Tree)"); the names of the two sides joined by an arrow when empty.
	Title string
	// Preview opens the diff as a preview tab, which the next preview replaces; false keeps it. A diff that is open already is
	// shown again in its tab.
	Preview bool
	// Selection is put in the right side and revealed, when set.
	Selection *Selection
	// PreserveFocus keeps the keyboard where it is.
	PreserveFocus bool
}

// OpenSpec describes a document to open with a title of the caller's (Documents().OpenDocument; VS Code's vscode.open with a
// title).
type OpenSpec struct {
	// URI is the document: a file, an open untitled document, or a provided one (ContentURI).
	URI string
	// Title is the name of the tab; the name of the document when empty.
	Title string
	// Preview opens it as a preview tab.
	Preview bool
	// Selection is put in the document and revealed, when set.
	Selection *Selection
	// PreserveFocus keeps the keyboard where it is.
	PreserveFocus bool
}

// DocumentTab is a tab of a text editor or of a diff editor, as Documents().Tabs lists it.
type DocumentTab struct {
	// ID identifies the tab to CloseTabs for as long as it is open.
	ID string
	// Group is the number of the editor group that holds it.
	Group int
	// Kind is "text" or "diff".
	Kind string
	// URI is the document of a text editor; Original and Modified are the two documents of a diff editor.
	URI, Original, Modified string
	// Dirty says the tab has unsaved changes.
	Dirty bool
	// Preview says the tab is a preview.
	Preview bool
}
