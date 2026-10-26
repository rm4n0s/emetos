package langfeatures

import (
	"context"
	"slices"
	"strings"
)

// Code actions (SPEC §9.12 "Code actions", "As built (step 12.8)"): the kinds and the filters that choose among them, the
// request with a trigger, the set of answers sorted the way the light bulb and the list show them, and the resolve of an action
// that the provider left without an edit.
//
// Ported from VS Code: src/vs/editor/contrib/codeAction/common/types.ts, src/vs/editor/contrib/codeAction/browser/codeAction.ts

// The kinds of code action (LSP's CodeActionKind; a kind is a dotted path and contains the kinds below it).
const (
	KindQuickFix            = "quickfix"
	KindRefactor            = "refactor"
	KindRefactorExtract     = "refactor.extract"
	KindRefactorInline      = "refactor.inline"
	KindRefactorMove        = "refactor.move"
	KindRefactorRewrite     = "refactor.rewrite"
	KindSurroundWith        = "refactor.surround"
	KindSource              = "source"
	KindSourceOrganize      = "source.organizeImports"
	KindSourceFixAll        = "source.fixAll"
	kindNotebook            = "notebook"
	maxSupportedKindsListed = 64
)

// KindContains tells whether the kind parent contains the kind child: they are equal, or child is below parent in the dotted
// path; the empty kind contains every kind (VS Code's HierarchicalKind.contains).
func KindContains(parent, child string) bool {
	return parent == child || parent == "" || strings.HasPrefix(child, parent+".")
}

// KindIntersects tells whether one of the kinds contains the other.
func KindIntersects(a, b string) bool { return KindContains(a, b) || KindContains(b, a) }

// CodeActionTriggerType says who asked for the actions (LSP's CodeActionTriggerKind).
type CodeActionTriggerType int

// The kinds of trigger.
const (
	// TriggerInvokedAction is the user asking (Ctrl+., Refactor..., Source Action...).
	TriggerInvokedAction CodeActionTriggerType = 1
	// TriggerAutomatic is the editor asking by itself (the light bulb, the hover, the save participants).
	TriggerAutomatic CodeActionTriggerType = 2
)

// CodeActionSource names what led to the request (VS Code's CodeActionTriggerSource); it is for the log and the tests.
type CodeActionSource string

// The sources of a request.
const (
	SourceDefault         CodeActionSource = "other (default)"
	SourceLightbulb       CodeActionSource = "lightbulb"
	SourceQuickFix        CodeActionSource = "quick fix action"
	SourceRefactor        CodeActionSource = "refactor"
	SourceRefactorPreview CodeActionSource = "refactor preview"
	SourceSourceAction    CodeActionSource = "source action"
	SourceFixAll          CodeActionSource = "fix all"
	SourceOrganizeImports CodeActionSource = "organize imports"
	SourceAutoFix         CodeActionSource = "auto fix"
	SourceQuickFixHover   CodeActionSource = "quick fix hover window"
	SourceOnSave          CodeActionSource = "save participants"
)

// CodeActionAutoApply says when a command applies an action without showing the list.
type CodeActionAutoApply int

// The auto apply modes, VS Code's CodeActionAutoApply.
const (
	// AutoApplyNever always shows the list.
	AutoApplyNever CodeActionAutoApply = iota
	// AutoApplyIfSingle applies the only valid action.
	AutoApplyIfSingle
	// AutoApplyFirst applies the first valid action.
	AutoApplyFirst
)

// CodeActionFilter chooses among the kinds of action.
type CodeActionFilter struct {
	// Include, when set, is the kind wanted: the actions of that kind and the kinds below it. Nil is every kind (the empty
	// string includes every kind but wants the action to have one).
	Include *string
	// Excludes are kinds that are left out, unless Include is below them.
	Excludes []string
	// IncludeSourceActions lets the source kinds through, which are left out unless asked for.
	IncludeSourceActions bool
	// OnlyPreferred leaves out the actions that are not preferred.
	OnlyPreferred bool
}

// Kind is a filter that wants the kind.
func Kind(kind string) *string { return &kind }

// excludes tells whether the exclude kind removes the provided kind: it contains it, and the included kind is not more specific
// than the excluded one.
func excludes(provided, exclude string, include *string) bool {
	if !KindContains(exclude, provided) {
		return false
	}
	return include == nil || !KindContains(exclude, *include)
}

// MayInclude tells whether a provider of the kind may answer actions the filter wants: the kind may be a subset or a superset of
// the kind filtered for (VS Code's mayIncludeActionsOfKind).
func (f CodeActionFilter) MayInclude(provided string) bool {
	if f.Include != nil && !KindIntersects(*f.Include, provided) {
		return false
	}
	if slices.ContainsFunc(f.Excludes, func(e string) bool { return excludes(provided, e, f.Include) }) {
		return false
	}
	return f.IncludeSourceActions || !KindContains(KindSource, provided)
}

// Filters tells whether the action passes the filter (VS Code's filtersAction).
func (f CodeActionFilter) Filters(a CodeAction) bool {
	if f.Include != nil && (a.Kind == "" || !KindContains(*f.Include, a.Kind)) {
		return false
	}
	if a.Kind != "" {
		if slices.ContainsFunc(f.Excludes, func(e string) bool { return excludes(a.Kind, e, f.Include) }) {
			return false
		}
		if !f.IncludeSourceActions && KindContains(KindSource, a.Kind) {
			return false
		}
	}
	return !f.OnlyPreferred || a.Preferred
}

// CodeActionTrigger says how the actions are asked for.
type CodeActionTrigger struct {
	Type   CodeActionTriggerType
	Source CodeActionSource
	Filter CodeActionFilter
	// AutoApply is what the command does with the actions that came.
	AutoApply CodeActionAutoApply
}

// CodeActionSet is the answer of the providers, sorted as the list shows it (VS Code's ManagedCodeActionSet).
type CodeActionSet struct {
	// All lists every action, the disabled ones too; Valid those that can be applied.
	All, Valid []CodeAction
}

// NewCodeActionSet sorts the actions and separates the valid ones from the disabled ones: the actions that fix a diagnostic
// come first, and within the two groups the preferred ones come first.
//
// Ported from VS Code: codeAction.ts (ManagedCodeActionSet)
func NewCodeActionSet(actions []CodeAction) CodeActionSet {
	all := slices.Clone(actions)
	slices.SortStableFunc(all, func(a, b CodeAction) int {
		ad, bd := len(a.Diagnostics) > 0, len(b.Diagnostics) > 0
		switch {
		case ad && !bd:
			return -1
		case !ad && bd:
			return 1
		case a.Preferred && !b.Preferred:
			return -1
		case !a.Preferred && b.Preferred:
			return 1
		}
		return 0
	})
	set := CodeActionSet{All: all}
	for _, a := range all {
		if a.Disabled == "" {
			set.Valid = append(set.Valid, a)
		}
	}
	return set
}

// HasAutoFix tells whether a valid action is a preferred quick fix (the light bulb is then the auto fix one).
func (s CodeActionSet) HasAutoFix() bool {
	return slices.ContainsFunc(s.Valid, func(a CodeAction) bool {
		return a.Kind != "" && KindContains(KindQuickFix, a.Kind) && a.Preferred
	})
}

// CodeActionKinds is implemented by a provider that knows the kinds of the actions it answers (the codeActionKinds of a
// language server): the registry asks only the providers that may answer the kind wanted.
type CodeActionKinds interface {
	CodeActionKinds() []string
}

// CodeActionResolver is a provider that leaves the edit of its actions to be asked for when one is applied (codeAction/resolve).
type CodeActionResolver interface {
	// CanResolveAction tells whether ResolveCodeAction would ask the provider for the edit of the action.
	CanResolveAction(a CodeAction) bool
	// ResolveCodeAction returns the action with its edit.
	ResolveCodeAction(ctx context.Context, a CodeAction) (CodeAction, error)
}

// GetCodeActions asks the providers of the document for the actions the trigger wants, filters them and sorts them. The
// request carries the kinds wanted (`only`) when the filter names one. Providers that say which kinds they answer are not asked
// for others.
//
// Ported from VS Code: codeAction.ts (getCodeActions)
func (r *Registry) GetCodeActions(ctx context.Context, doc Document, req CodeActionRequest, trig CodeActionTrigger) (CodeActionSet, []ProviderError) {
	if trig.Filter.Include != nil {
		req.Only = []string{*trig.Filter.Include}
	}
	req.Automatic = trig.Type == TriggerAutomatic
	// an automatic request leaves the notebook actions out
	filter := trig.Filter
	if trig.Type == TriggerAutomatic {
		filter.Excludes = append(slices.Clone(filter.Excludes), kindNotebook)
	}
	var regs []Registered[CodeActionProvider]
	for _, reg := range r.codeActions.ordered(doc) {
		if !offered(reg.Provider, FeatureCodeActions) {
			continue
		}
		if kp, ok := reg.Provider.(CodeActionKinds); ok {
			if kinds := kp.CodeActionKinds(); len(kinds) > 0 && !slices.ContainsFunc(kinds, filter.MayInclude) {
				continue
			}
		}
		regs = append(regs, reg)
	}
	rs := fanOut(ctx, regs, func(ctx context.Context, p CodeActionProvider) ([]CodeAction, error) {
		return p.ProvideCodeActions(ctx, doc, req)
	})
	var out []CodeAction
	for _, res := range rs {
		if res.err != nil {
			continue
		}
		src, _ := res.provider.(CodeActionProvider)
		for _, a := range res.val {
			if !filter.Filters(a) {
				continue
			}
			if a.Owner == "" {
				a.Owner = res.owner
			}
			a.source = src
			out = append(out, a)
		}
	}
	return NewCodeActionSet(out), errorsOf(rs)
}

// CanResolveCodeAction tells whether the provider that answered with the action can resolve its edit.
func (r *Registry) CanResolveCodeAction(a CodeAction) bool {
	rv, ok := a.source.(CodeActionResolver)
	return ok && rv.CanResolveAction(a)
}

// ResolveCodeAction asks the provider that answered with the action for its edit, when it has none and the provider can
// resolve it (VS Code's CodeActionItem.resolve); the action is returned as it was otherwise.
func (r *Registry) ResolveCodeAction(ctx context.Context, a CodeAction) (CodeAction, error) {
	rv, ok := a.source.(CodeActionResolver)
	if !ok || a.Edit != nil || !rv.CanResolveAction(a) {
		return a, nil
	}
	return rv.ResolveCodeAction(ctx, a)
}

// CodeActionKinds lists the kinds of action the providers of the document say they answer, without duplicates (the context key
// supportedCodeAction is these joined by spaces).
func (r *Registry) CodeActionKinds(doc Document) []string {
	var out []string
	for _, reg := range r.codeActions.ordered(doc) {
		if !offered(reg.Provider, FeatureCodeActions) {
			continue
		}
		if kp, ok := reg.Provider.(CodeActionKinds); ok {
			for _, k := range kp.CodeActionKinds() {
				if !slices.Contains(out, k) && len(out) < maxSupportedKindsListed {
					out = append(out, k)
				}
			}
		}
	}
	return out
}

// SupportsCodeActionKind tells whether a provider of the document may answer actions of the kind (the keys of Refactor...,
// Source Action... and Organize Imports: VS Code's contextKeyForSupportedActions).
func (r *Registry) SupportsCodeActionKind(doc Document, kind string) bool {
	return slices.ContainsFunc(r.CodeActionKinds(doc), func(k string) bool { return KindContains(kind, k) })
}
