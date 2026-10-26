package app

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The save participants of the language features (SPEC §9.12 "Code actions", "Formatting", "As built (step 12.8)"): before a
// document is written the code actions of editor.codeActionsOnSave run, then the format on save, each bounded by its own timeout.
// They need the language servers, so they run before document.BeginSave, whose own participants are synchronous; the trim
// of trailing white space goes first, the final newline settings after the formatter, and every edit of the lot joins one undo
// step. A timeout, a failure or a document that changed meanwhile never stops the save: that step is dropped and the save goes on.
//
// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/saveParticipants.ts (CodeActionOnSaveParticipant,
// FormatOnSaveParticipant)

// codeActionsOnSaveKinds are the kinds of code actions to run for a save, and the kinds not to run, from the value of
// editor.codeActionsOnSave. A save by auto save runs none; a save by a change of focus runs those marked "always". The kinds are
// in the order they run: source.fixAll first, then the others as the setting lists them (an object by the kind's name), and a
// kind that another one contains is left out.
//
// Ported from VS Code: saveParticipants.ts (CodeActionOnSaveParticipant.applyOnSaveActions)
func codeActionsOnSaveKinds(value any, reason document.SaveReason) (kinds, excludes []string) {
	if reason == document.SaveAuto {
		return nil, nil
	}
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch mode := v[k].(type) {
			case bool:
				if !mode {
					excludes = append(excludes, k)
				} else if reason == document.SaveExplicit {
					kinds = append(kinds, k)
				}
			case string:
				switch mode {
				case "never":
					excludes = append(excludes, k)
				case "always":
					kinds = append(kinds, k)
				case "explicit":
					if reason == document.SaveExplicit {
						kinds = append(kinds, k)
					}
				}
			}
		}
	case []any:
		// the older form, an array of kinds, runs on explicit saves only
		if reason != document.SaveExplicit {
			return nil, nil
		}
		for _, k := range v {
			if s, ok := k.(string); ok {
				kinds = append(kinds, s)
			}
		}
	case []string:
		if reason == document.SaveExplicit {
			kinds = slices.Clone(v)
		}
	}
	kinds = slices.DeleteFunc(kinds, func(x string) bool {
		return slices.ContainsFunc(kinds, func(y string) bool { return y != x && langfeatures.KindContains(y, x) })
	})
	sort.SliceStable(kinds, func(i, j int) bool {
		a, b := langfeatures.KindContains(langfeatures.KindSourceFixAll, kinds[i]), langfeatures.KindContains(langfeatures.KindSourceFixAll, kinds[j])
		return a && !b
	})
	return kinds, excludes
}

// savePlan is what the language features do before a document is written.
type savePlan struct {
	s   *shellState
	win *Workspace
	doc *document.Document
	req saveRequest
	reg *langfeatures.Registry

	// kinds and excludes are the code actions to run, format says whether the formatter runs.
	kinds, excludes []string
	format          bool
	// aborted is set when the text changed under a step: the steps after it are not made.
	aborted bool
}

// savePlanFor decides what runs before the document is written, or returns nil when nothing does: a save without those
// participants costs nothing.
func (s *shellState) savePlanFor(doc *document.Document, req saveRequest) *savePlan {
	if req.skipParticipants || req.prepared {
		return nil
	}
	win := PeekWorkspace(s.Context())
	scope := win.Extensions()
	if scope == nil {
		return nil
	}
	reg := scope.Features()
	if reg == nil {
		return nil
	}
	lang := doc.LanguageID()
	ld := langfeatures.Document{URI: doc.URI(), LanguageID: lang, Version: doc.Version()}
	p := &savePlan{s: s, win: win, doc: doc, req: req, reg: reg}
	if reg.Has(ld, langfeatures.FeatureCodeActions) {
		p.kinds, p.excludes = codeActionsOnSaveKinds(settings.Get[any](win.Settings, "editor.codeActionsOnSave", lang), req.reason)
	}
	p.format = req.reason != document.SaveAuto && settings.Get[bool](win.Settings, "editor.formatOnSave", lang) &&
		(reg.Has(ld, langfeatures.FeatureFormatting) || reg.Has(ld, langfeatures.FeatureRangeFormatting))
	if len(p.kinds) == 0 && !p.format {
		return nil
	}
	return p
}

// run does the participants one after the other, then calls then (on the UI thread). The edits of all of them are one undo
// step.
func (p *savePlan) run(then func()) {
	doc := p.doc
	sync := p.win.saveParticipants(doc)
	auto := p.req.reason == document.SaveAuto
	// the trim of white space goes before the language features, the other participants after them
	doc.PushStackElement()
	first := edit.SaveParticipants{TrimTrailingWhitespace: sync.TrimTrailingWhitespace}
	p.logFailure("save participants", p.syncParticipants(first, auto))
	finish := func() {
		p.logFailure("save participants", p.syncParticipants(sync, auto))
		doc.PushStackElement()
		then()
	}
	p.codeActions(func() {
		p.formatDocument(finish)
	})
}

func (p *savePlan) logFailure(msg string, err error) {
	if err != nil && p.win.Process != nil && p.win.Process.Logger != nil {
		p.win.Process.Logger.Warn(msg+" failed", "uri", p.doc.URI().String(), "err", err)
	}
}

// syncParticipants make the edits of the white space settings, joined to the undo step of the save.
func (p *savePlan) syncParticipants(parts edit.SaveParticipants, auto bool) error {
	if !parts.Any() {
		return nil
	}
	if e := p.s.editorOf(p.doc); e != nil {
		return e.view.ApplySaveParticipantsJoined(parts, auto)
	}
	res := edit.NewCommands(edit.NewModel(p.doc.Snapshot()), edit.DefaultOptions()).SaveParticipants(nil, parts, auto)
	if len(res.Edits) == 0 {
		return nil
	}
	_, err := p.doc.Edit(res.Edits, document.EditOptions{Kind: res.Kind, Join: true})
	return err
}

// ask runs work on a goroutine with a context that ends at the deadline or when the shell goes, then calls back on the UI
// thread, saying whether the deadline passed.
func (p *savePlan) ask(deadline time.Time, work func(ctx context.Context), back func(timedOut bool)) {
	binding := widgets.WidgetsBindingOf(p.s.Context())
	p.s.Go(func(life context.Context) {
		ctx, cancel := context.WithDeadline(life, deadline)
		work(ctx)
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		binding.Post(func() {
			if p.s.Mounted() {
				back(timedOut)
			}
		})
	})
}

// languageDocument is the document as the providers get it.
func (p *savePlan) languageDocument() langfeatures.Document {
	d := p.doc
	return langfeatures.Document{URI: d.URI(), LanguageID: d.LanguageID(), Version: d.Version(), Text: d.Snapshot()}
}

// timedOut tells the user that a participant gave up.
func (p *savePlan) timedOut(what string, d time.Duration) {
	p.win.Notify.Notify(notify.Notification{
		Severity: notify.Info, Source: "Emetos", Silent: true,
		Message: "Aborted " + what + " on save after " + strconv.FormatInt(d.Milliseconds(), 10) + "ms",
	})
}

// apply makes an edit as part of the undo step of the save.
func (p *savePlan) apply(we *langfeatures.WorkspaceEdit) error {
	res, err := langfeatures.ApplyWith(p.win.ctx, workspaceEdits{p.win}, we, langfeatures.ApplyOptions{JoinUndo: true})
	for _, d := range res.Documents {
		if d != p.doc {
			d.PushStackElement() // the other documents get their step closed here: the save only closes its own
		}
	}
	return err
}

// ---- code actions ----

// codeActions runs the code actions of editor.codeActionsOnSave, one kind after the other, each asked for with the text the
// one before left. The first valid action of a kind is applied: its edit, then its command.
//
// Ported from VS Code: saveParticipants.ts (CodeActionOnSaveParticipant)
func (p *savePlan) codeActions(next func()) {
	if len(p.kinds) == 0 {
		next()
		return
	}
	timeout := time.Duration(settings.Get[int](p.win.Settings, "editor.codeActionsOnSaveTimeout", p.doc.LanguageID())) * time.Millisecond
	deadline := time.Now().Add(timeout)
	var step func(i int)
	step = func(i int) {
		if i >= len(p.kinds) || p.aborted {
			next()
			return
		}
		ld := p.languageDocument()
		var chosen *langfeatures.CodeAction
		p.ask(deadline, func(ctx context.Context) {
			set, errs := p.reg.GetCodeActions(ctx, ld, langfeatures.CodeActionRequest{Range: langfeatures.FullRange(ld.Text)}, langfeatures.CodeActionTrigger{
				Type: langfeatures.TriggerAutomatic, Source: langfeatures.SourceOnSave,
				Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(p.kinds[i]), Excludes: p.excludes, IncludeSourceActions: true},
			})
			for _, e := range errs {
				p.logFailure("code actions on save", e.Err)
			}
			if len(set.Valid) == 0 {
				return
			}
			a := set.Valid[0]
			if p.reg.CanResolveCodeAction(a) {
				resolved, err := p.reg.ResolveCodeAction(ctx, a)
				if err != nil {
					p.logFailure("code actions on save", err)
					return
				}
				a = resolved
			}
			chosen = &a
		}, func(timedOut bool) {
			switch {
			case timedOut:
				p.timedOut("code actions", timeout)
				next()
				return
			case p.doc.Version() != ld.Version:
				p.aborted = true // the text changed while the server worked: its answer is for the old text
				next()
				return
			case chosen != nil:
				if chosen.Edit != nil {
					p.logFailure("code action on save", p.apply(chosen.Edit))
				}
				if chosen.Command != nil {
					p.logFailure("code action on save", p.win.executeCompletionCommand(p.win.ctx, *chosen.Command))
				}
			}
			step(i + 1)
		})
	}
	step(0)
}

// ---- format on save ----

// formatDocument runs the formatter as editor.formatOnSaveMode says: the whole file, or the lines that differ from the file on disk.
//
// Ported from VS Code: saveParticipants.ts (FormatOnSaveParticipant), formatModified.ts
func (p *savePlan) formatDocument(next func()) {
	if !p.format || p.aborted {
		next()
		return
	}
	lang := p.doc.LanguageID()
	timeout := time.Duration(settings.Get[int](p.win.Settings, "editor.formatOnSaveTimeout", lang)) * time.Millisecond
	deadline := time.Now().Add(timeout)
	mode := settings.Get[string](p.win.Settings, "editor.formatOnSaveMode", lang)
	def := settings.Get[string](p.win.Settings, "editor.defaultFormatter", lang)
	ld := p.languageDocument()
	tab, spaces := p.doc.Indentation()
	opts := langfeatures.FormattingOptions{TabSize: tab, InsertSpaces: spaces}
	path := ""
	if u := p.doc.URI(); u.Scheme == document.SchemeFile {
		path = u.Path
	}
	ranges := p.reg.Has(ld, langfeatures.FeatureRangeFormatting)
	var edits []langfeatures.TextEdit
	p.ask(deadline, func(ctx context.Context) {
		var err error
		edits, err = formatForSave(ctx, p.reg, ld, opts, def, mode, path, ranges)
		p.logFailure("format on save", err)
	}, func(timedOut bool) {
		switch {
		case timedOut:
			p.timedOut("format", timeout)
		case p.doc.Version() != ld.Version:
			p.aborted = true
		case len(edits) > 0:
			v := ld.Version
			p.logFailure("format on save", p.apply(&langfeatures.WorkspaceEdit{
				Label:   "Format",
				Changes: []langfeatures.Change{{Kind: langfeatures.ChangeText, URI: p.doc.URI(), Version: &v, Edits: edits}},
			}))
		}
		next()
	})
}

// formatForSave asks for the edits of a format on save. It runs on a goroutine.
func formatForSave(ctx context.Context, reg *langfeatures.Registry, ld langfeatures.Document, opts langfeatures.FormattingOptions, def, mode, path string, ranges bool) ([]langfeatures.TextEdit, error) {
	whole := func() ([]langfeatures.TextEdit, error) {
		edits, err := reg.FormatDocument(ctx, ld, opts, def)
		return langfeatures.MinimalEdits(ld.Text, edits), err
	}
	if mode != "modifications" && mode != "modificationsIfAvailable" {
		return whole()
	}
	// The lines that differ from the file on disk: Emetos has no source control to say which, so the file is the baseline.
	var base []string
	available := false
	if path != "" && ranges {
		res, err := fileio.Read(ctx, path, fileio.ReadOptions{})
		if err == nil {
			available = true
			for _, line := range buffer.New(res.Text).Snapshot().Lines(0) {
				base = append(base, line)
			}
		}
	}
	if !available {
		if mode == "modificationsIfAvailable" {
			return whole()
		}
		return nil, nil
	}
	var text []string
	for _, line := range ld.Text.Lines(0) {
		text = append(text, line)
	}
	modified, err := langfeatures.ModifiedRanges(ctx, base, text)
	if err != nil {
		return nil, err
	}
	var all []langfeatures.TextEdit
	for _, r := range modified {
		edits, err := reg.FormatRange(ctx, ld, r, opts, def)
		if err != nil {
			return nil, err
		}
		all = append(all, edits...)
	}
	all = langfeatures.MinimalEdits(ld.Text, all)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Range.Start.IsBefore(all[j].Range.Start) })
	// formatted ranges that touch may give the same edit twice, and edits must not overlap
	var out []langfeatures.TextEdit
	for _, e := range all {
		if n := len(out); n > 0 && e.Range.Start.IsBefore(out[n-1].Range.End) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
