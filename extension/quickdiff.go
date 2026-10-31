package extension

import "context"

// The Quick Diff API (SPEC §10.30 "Quick diff", step 24.11; after QuickDiffProvider and SourceControl.quickDiffProvider of vscode.d.ts and
// extHostSCM.ts). As in VS Code the workbench draws the marks of the changes of a file in the gutter, the overview ruler and the minimap and
// moves between them, and a source control only says what a file is compared with: the provider answers, for a file, with the URI of
// the document that holds its original text (the version in the index, for Git). The document is one that the extension provides
// (Documents().ProvideContent), so the host reads it the way it reads any provided document.
//
// Everything here may be called from any goroutine, except where a comment says that Emetos calls the extension.

// QuickDiffKind tells how a provider's changes are shown (VS Code's QuickDiffProviderKind: primary, secondary).
type QuickDiffKind int

const (
	// QuickDiffPrimary is the provider whose changes are the marks of the file, and whose original the format on save of modifications
	// compares with (VS Code's SourceControl.quickDiffProvider).
	QuickDiffPrimary QuickDiffKind = iota
	// QuickDiffSecondary is a provider whose changes are drawn in the secondary colors, where the primary provider's changes do not
	// cover them (VS Code's SourceControl.secondaryQuickDiffProvider; Git's staged changes).
	QuickDiffSecondary
)

// QuickDiffProvider says what the files of a source control are compared with.
type QuickDiffProvider struct {
	// Label names the provider ("Git Local Changes (Working Tree)"); the peek of step 24.12 shows it.
	Label string
	// Kind says whether the provider is the primary one of its source control or a secondary one.
	Kind QuickDiffKind
	// Original returns the URI of the document that holds the original text of the file with the URI (file:///path), or "" when the file
	// has no original under this provider (an untracked file, a file of another repository). The host reads the document the way it reads
	// a provided one, and compares it with the file; it asks again after RefreshQuickDiff and after the provider's content changed
	// (ContentRegistration.Changed), not on every edit. It runs off the UI thread and must stop when ctx does; a failure means no
	// original for the file, and is logged by the host at debug level. Only files below the root of the source control are asked.
	Original func(ctx context.Context, uri string) (string, error)
}

// QuickDiffRegistration is a registered QuickDiffProvider.
type QuickDiffRegistration interface {
	// Dispose takes the provider away: the marks it made go. It does nothing the second time, and happens by itself when the activation
	// ends.
	Dispose()
}
