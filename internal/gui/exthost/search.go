package exthost

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/search"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// The search across files for extensions (SPEC §10.27 "As built (step 17e.2)"): Workspace().FindTextInFiles, NewTextReplacer
// and ReplaceInFiles over internal/core/search.

// UnsavedSource is the optional part of a DocumentSource that tells what the editors hold: the search reads the text of the
// documents that have unsaved changes instead of their files, and "Search Only in Open Editors" needs the open files. Both
// methods are called on the UI thread.
type UnsavedSource interface {
	// Unsaved returns the text of the open file documents that have unsaved changes, by absolute path.
	Unsaved() map[string]string
	// OpenFiles returns the absolute paths of the open documents that are files.
	OpenFiles() []string
}

func toSearchQuery(q extension.TextSearchQuery) search.Query {
	return search.Query{
		Pattern: q.Pattern, IsRegExp: q.IsRegExp, IsCaseSensitive: q.IsCaseSensitive, IsWordMatch: q.IsWordMatch,
		IsSmartCase: q.IsSmartCase, IsMultiline: q.IsMultiline,
	}
}

// editors asks the UI thread for the text of the editors that have unsaved changes, and for the open files. A context that
// ends first, or the end of the activation, gives up the wait; the search then runs without the editors (and stops at once if
// it was the context that ended).
func (in *instance) editors(ctx context.Context) (unsaved map[string]string, open []string) {
	src, ok := in.scope.opts.Documents.(UnsavedSource)
	if !ok {
		return nil, nil
	}
	done := make(chan struct{})
	in.scope.post(func() {
		unsaved, open = src.Unsaved(), src.OpenFiles()
		close(done)
	})
	select {
	case <-done:
		return unsaved, open
	case <-ctx.Done():
	case <-in.ctx.Done():
	}
	return nil, nil
}

// FindTextInFiles implements extension.Workspace.
func (w workspaceAPI) FindTextInFiles(ctx context.Context, query extension.TextSearchQuery, opts extension.TextSearchOptions, onFile func(extension.TextSearchFile)) (extension.TextSearchSummary, error) {
	in := w.inst
	if in.isDead() {
		return extension.TextSearchSummary{}, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	folder := in.scope.opts.Info.Folder
	if folder == "" {
		return extension.TextSearchSummary{}, errors.New("ExtensionNoFolder", "the scope has no folder", "extension", in.id)
	}
	cfg := workspace.DefaultIndexConfig()
	encoding := ""
	if st := in.scope.opts.Settings; st != nil {
		cfg = workspace.IndexConfigFromSettings(st)
		encoding = settings.Get[string](st, "files.encoding", "")
	}
	so := search.Options{
		Folder: folder, Include: search.ParsePatterns(opts.Include, folder), Exclude: search.ParsePatterns(opts.Exclude, folder),
		UseIgnoreFiles: cfg.UseIgnoreFiles && !opts.DisregardIgnoreFiles, UseParentIgnoreFiles: cfg.UseParentIgnoreFiles && !opts.DisregardIgnoreFiles,
		GlobalIgnoreFile: cfg.GlobalIgnoreFile, FollowSymlinks: cfg.FollowSymlinks, Encoding: encoding, MaxResults: opts.MaxResults,
	}
	if opts.DisregardIgnoreFiles {
		so.GlobalIgnoreFile = ""
	}
	if !opts.DisregardExcludeSettings {
		so.ExcludeSettings = cfg.Exclude
	}
	unsaved, open := in.editors(ctx)
	so.Unsaved = unsaved
	switch {
	case opts.OnlyOpenEditors:
		so.OnlyPaths = open
		if so.OnlyPaths == nil {
			so.OnlyPaths = []string{}
		}
	case !opts.DisregardExcludeSettings && !opts.DisregardIgnoreFiles && in.scope.opts.Files != nil && in.scope.opts.Settings != nil:
		// The file index has the files of the folder, with these exclusions applied, once its walk is done.
		// An empty list is a window with no index (or an empty folder, which walks in no time).
		if complete := in.scope.opts.FilesComplete; complete == nil || complete() {
			if files := in.scope.opts.Files(); len(files) > 0 {
				so.Files = append([]string{}, files...)
			}
		}
	}
	sum, err := search.Run(ctx, toSearchQuery(query), so, func(r search.FileResult) {
		if in.isDead() {
			return
		}
		f := extension.TextSearchFile{Path: r.Path, URI: document.FileURI(filepath.Join(folder, filepath.FromSlash(r.Path))).String(), Matches: make([]extension.TextSearchMatch, len(r.Matches))}
		for i, m := range r.Matches {
			f.Matches[i] = extension.TextSearchMatch{
				Range:   extension.Range{Start: extension.Position{Line: m.Line, Col: m.Col}, End: extension.Position{Line: m.EndLine, Col: m.EndCol}},
				Preview: m.Preview, PreviewStart: m.PreviewStart, PreviewEnd: m.PreviewEnd, Text: m.Text,
			}
		}
		onFile(f)
	})
	return extension.TextSearchSummary{
		Files: sum.Files, Matches: sum.Matches, LimitHit: sum.LimitHit, Searched: sum.Searched, Binary: sum.Binary, TooLarge: sum.TooLarge, Unreadable: sum.Unreadable,
	}, err
}

// textReplacer is the extension.TextReplacer over search.Replacer.
type textReplacer struct{ r *search.Replacer }

// Replace implements extension.TextReplacer.
func (t textReplacer) Replace(matchText string) string { return t.r.Replace(matchText) }

// NewTextReplacer implements extension.Workspace. The error of the search package is returned as it is.
func (w workspaceAPI) NewTextReplacer(query extension.TextSearchQuery, replacement string, preserveCase bool) (extension.TextReplacer, error) {
	r, err := search.NewReplacer(toSearchQuery(query), replacement, preserveCase)
	if err != nil {
		return nil, err
	}
	return textReplacer{r}, nil
}

// ReplaceInFiles implements extension.Workspace.
func (w workspaceAPI) ReplaceInFiles(ctx context.Context, files []extension.FileReplacement) (extension.ReplaceReport, error) {
	in := w.inst
	var report extension.ReplaceReport
	if in.isDead() {
		return report, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	folder := in.scope.opts.Info.Folder
	if folder == "" {
		return report, errors.New("ExtensionNoFolder", "the scope has no folder", "extension", in.id)
	}
	encoding := ""
	if st := in.scope.opts.Settings; st != nil {
		encoding = settings.Get[string](st, "files.encoding", "")
	}
	for _, f := range files {
		if !fs.ValidPath(f.Path) || f.Path == "." {
			return report, errors.New("ExtensionFilePathInvalid", "a path is relative to the folder, with forward slashes and no ..", "extension", in.id, "path", f.Path)
		}
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		abs, err := resolveInside(folder, f.Path)
		if err != nil {
			report.Failed = append(report.Failed, extension.ReplaceFailure{Path: f.Path, Message: err.Error()})
			continue
		}
		edits := make([]search.Edit, len(f.Replacements))
		for i, r := range f.Replacements {
			edits[i] = search.Edit{Line: r.Range.Start.Line, Col: r.Range.Start.Col, EndLine: r.Range.End.Line, EndCol: r.Range.End.Col, Expected: r.Expected, Text: r.Text}
		}
		res, err := search.ReplaceInFile(ctx, abs, encoding, edits)
		if err != nil {
			if ctx.Err() != nil {
				return report, err
			}
			report.Failed = append(report.Failed, extension.ReplaceFailure{Path: f.Path, Message: replaceMessage(err)})
			continue
		}
		report.Replaced += res.Applied
		report.Skipped += res.Skipped
		if res.Applied > 0 {
			report.Files = append(report.Files, f.Path)
		}
	}
	return report, nil
}

// resolveInside is the absolute path of a path relative to the folder, after links are followed: a link that points out of
// the folder is refused (the tag ExtensionFilePathInvalid).
func resolveInside(folder, rel string) (string, error) {
	abs := filepath.Join(folder, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return "", err
	}
	if real != root && !strings.HasPrefix(real, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator)) {
		return "", errors.New("ExtensionFilePathInvalid", "a link points out of the folder", "path", rel)
	}
	return real, nil
}

// replaceMessage is the text of an error of ReplaceInFile for the report.
func replaceMessage(err error) string {
	if e, ok := errors.FromError(err); ok && e.Message != "" {
		return e.Message
	}
	return err.Error()
}
