package editor

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Code actions in the editor (SPEC §9.12 "Code actions", "As built (step 12.8)"): the light bulb, which asks for the actions of
// the cursor 250 ms after it stops moving, the list that Ctrl+., Refactor... and Source Action... open, and the application of
// an action: its edit (after codeAction/resolve when the server left it out) and then its command.
//
// Ported from VS Code: src/vs/editor/contrib/codeAction/browser/codeActionModel.ts, codeActionController.ts, codeActionCommands.ts,
// codeAction.ts (applyCodeAction), lightBulbWidget.ts (the state) and codeActionMenu.ts

// codeActionDelay is how long the cursor rests before the light bulb asks (the delay of VS Code's CodeActionOracle).
const codeActionDelay = 250

// The values of editor.lightbulb.enabled.
const (
	lightbulbOff    = "off"
	lightbulbOnCode = "onCode"
	lightbulbOn     = "on"
)

// CodeActionOptions are the settings of the code actions (editor.lightbulb.enabled and editor.codeActionWidget.*).
type CodeActionOptions struct {
	// Lightbulb is editor.lightbulb.enabled: "off", "onCode" (also when empty) or "on".
	Lightbulb string
	// ShowHeaders is editor.codeActionWidget.showHeaders: the list groups the actions under headers.
	ShowHeaders bool
	// IncludeNearbyQuickFixes is editor.codeActionWidget.includeNearbyQuickFixes: Quick Fix... also lists the fixes of the
	// diagnostics on the cursor's line when there are none at the cursor.
	IncludeNearbyQuickFixes bool
}

// LightBulbIcon says which icon the light bulb has.
type LightBulbIcon int

// The icons of the light bulb.
const (
	// LightBulbPlain is the light bulb of actions.
	LightBulbPlain LightBulbIcon = iota
	// LightBulbAutoFix is the light bulb with the sparks of a preferred quick fix.
	LightBulbAutoFix
)

// LightBulb is what the light bulb shows: it stands at the line of the cursor and opens the actions that came.
type LightBulb struct {
	// Pos is the position the actions were asked at; the bulb is at its line.
	Pos buffer.Position
	// Icon is the plain bulb, or the auto fix one when a valid action is a preferred quick fix.
	Icon LightBulbIcon
	// Title is the tooltip.
	Title string
	// Actions are the actions of the position.
	Actions langfeatures.CodeActionSet
}

// ActionListItem is a row of the list of actions: a header, or an action.
type ActionListItem struct {
	// Header says that the row is a group header, with the group's title as Title.
	Header bool
	// Title is the text of the row: the title of the action, or its reason when it is disabled.
	Title string
	// Action is the action of a row that is not a header.
	Action langfeatures.CodeAction
	// Disabled says that the action cannot be applied; the row shows its reason and cannot be chosen.
	Disabled bool
	// Key is the label of the key of the action ("Shift+Alt+O"), or empty.
	Key string
	// Icon is the codepoint of the Codicon in front of the row; 0 is the light bulb in its color.
	Icon int
	// CanPreview says that Ctrl+Enter shows the changes of the action before they are made.
	CanPreview bool
}

// ActionList is the list of actions that is open.
type ActionList struct {
	Items []ActionListItem
	// Focus is the row that Enter applies, or -1.
	Focus int
	// Anchor is the position the list hangs from: the line of the cursor or of the light bulb.
	Anchor buffer.Position
	// Bar are the links under the list ("Show Disabled").
	Bar []ActionBarItem

	set             langfeatures.CodeActionSet
	version         int
	fromLightbulb   bool
	includeDisabled bool
}

// ActionBarItem is a link of the bar under the list of actions.
type ActionBarItem struct {
	Label string
	Run   func()
}

// codeActionState is what a view model keeps for its code actions.
type codeActionState struct {
	// cancelDelay ends the wait before the light bulb asks; autoCancel the request in flight; autoGen numbers the requests.
	cancelDelay func()
	autoCancel  context.CancelFunc
	autoGen     int
	bulb        *LightBulb
	// opCancel ends the request a command made (Ctrl+.) and opGen numbers them.
	opCancel context.CancelFunc
	opGen    int
	list     *ActionList
	// showDisabled is the state of the link "Show Disabled" of the list.
	showDisabled bool
	unsubDoc     func()
	changed      *foundation.ChangeNotifier
	// applied is the last action applied, for the tests.
	applied *langfeatures.CodeAction
}

func newCodeActionState() codeActionState {
	return codeActionState{changed: foundation.NewChangeNotifier()}
}

// AddCodeActionListener calls fn when the light bulb or the list of actions shows, changes or goes.
func (vm *ViewModel) AddCodeActionListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.ca.changed.AddListener(fn)
}

// RemoveCodeActionListener removes a listener of AddCodeActionListener.
func (vm *ViewModel) RemoveCodeActionListener(h foundation.ListenerHandle) {
	vm.ca.changed.RemoveListener(h)
}

func (vm *ViewModel) codeActionsChanged() {
	vm.ca.changed.NotifyListeners()
	vm.notifyKeys()
}

// CodeActionsHasProvider tells whether a code action provider matches the document (the key editorHasCodeActionsProvider).
func (vm *ViewModel) CodeActionsHasProvider() bool {
	f := vm.hoverFeatures()
	return f != nil && f.Has(vm.langDocument(), langfeatures.FeatureCodeActions)
}

// SupportedCodeActions is the key supportedCodeAction: the kinds the providers answer, joined by spaces.
func (vm *ViewModel) SupportedCodeActions() string {
	f := vm.hoverFeatures()
	if f == nil {
		return ""
	}
	return strings.Join(f.CodeActionKinds(vm.langDocument()), " ")
}

// LightBulb is the light bulb that shows now, or nil.
func (vm *ViewModel) LightBulb() *LightBulb { return vm.ca.bulb }

// CodeActionList is the list of actions that is open, or nil.
func (vm *ViewModel) CodeActionList() *ActionList { return vm.ca.list }

// CodeActionMenuVisible tells whether the list is open (the key codeActionMenuVisible).
func (vm *ViewModel) CodeActionMenuVisible() bool { return vm.ca.list != nil }

// codeActionsHost is the host of the editor when it can answer code actions.
func (vm *ViewModel) codeActionsHost() (*SuggestHost, *langfeatures.Registry, bool) {
	h := vm.sg.host
	if h == nil || h.Features == nil || vm.ReadOnly() {
		return nil, nil, false
	}
	return h, h.Features, h.Features.Has(vm.langDocument(), langfeatures.FeatureCodeActions)
}

// hostCtx is the context requests of the editor run under: it ends with the window tab.
func (vm *ViewModel) hostCtx() context.Context {
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		return h.Ctx
	}
	return context.Background()
}

// ---- the light bulb ----

// codeActionsCursorsChanged follows the cursor: the light bulb leaves its line when the cursor does, and asks again once the
// cursor rests (VS Code's oracle also asks again when the markers of the file change: codeActionsDiagnosticsChanged).
func (vm *ViewModel) codeActionsCursorsChanged() {
	c := &vm.ca
	if c.bulb != nil && c.bulb.Pos.Line != vm.cursors.Primary().Position().Line {
		vm.hideLightBulb()
	}
	if vm.sg.host == nil {
		return
	}
	vm.codeActionsSchedule(codeActionDelay)
}

// codeActionsDiagnosticsChanged asks again for the actions of the cursor when the diagnostics of the document are replaced.
func (vm *ViewModel) codeActionsDiagnosticsChanged() {
	if vm.sg.host == nil {
		return
	}
	vm.codeActionsSchedule(codeActionDelay)
}

// codeActionsSchedule asks for the actions of the light bulb after a delay; a new call restarts the wait.
func (vm *ViewModel) codeActionsSchedule(delay int) {
	c := &vm.ca
	vm.codeActionsCancelAuto()
	if vm.sg.host == nil || vm.opts.CodeActions.Lightbulb == lightbulbOff {
		vm.hideLightBulb()
		return
	}
	if delay <= 0 || vm.afterTimer == nil {
		vm.codeActionsAutoTrigger()
		return
	}
	c.cancelDelay = vm.afterTimer(time.Duration(delay)*time.Millisecond, func() {
		c.cancelDelay = nil
		vm.codeActionsAutoTrigger()
	})
}

func (vm *ViewModel) codeActionsCancelAuto() {
	c := &vm.ca
	if c.cancelDelay != nil {
		c.cancelDelay()
		c.cancelDelay = nil
	}
	if c.autoCancel != nil {
		c.autoCancel()
		c.autoCancel = nil
	}
	c.autoGen++
}

// lightbulbWanted tells whether the selection is a place for the light bulb (editor.lightbulb.enabled): with onCode a cursor
// between two blanks, or at the start or end of a blank line, is not.
//
// Ported from VS Code: codeActionModel.ts (_getRangeOfSelectionUnlessWhitespaceEnclosed)
func (vm *ViewModel) lightbulbWanted() bool {
	sel := vm.cursors.Primary().Selection
	switch vm.opts.CodeActions.Lightbulb {
	case lightbulbOff:
		return false
	case lightbulbOn:
		return true
	}
	if !sel.IsEmpty() {
		return true
	}
	pos := sel.Active
	line, err := vm.doc.Snapshot().LineContent(pos.Line)
	if err != nil || line == "" {
		return false
	}
	col := min(pos.Col, len(line))
	isSpace := func(i int) bool { return i >= 0 && i < len(line) && (line[i] == ' ' || line[i] == '\t') }
	switch {
	case col == 0:
		return !isSpace(0)
	case col == len(line):
		return !isSpace(len(line) - 1)
	}
	return !isSpace(col-1) || !isSpace(col)
}

// codeActionsAutoTrigger asks for the actions at the cursor for the light bulb: an automatic request without `only`, whose
// answer is dropped when the document changed, the request was replaced, or the cursor left the line.
func (vm *ViewModel) codeActionsAutoTrigger() {
	c := &vm.ca
	_, reg, ok := vm.codeActionsHost()
	if !ok || !vm.lightbulbWanted() {
		vm.hideLightBulb()
		return
	}
	sel := vm.cursors.Primary().Selection
	doc := vm.langDocument()
	req := langfeatures.CodeActionRequest{Range: sel.Range(), Diagnostics: diagnosticsTouching(vm.allDiagnostics(), sel.Range())}
	trig := langfeatures.CodeActionTrigger{Type: langfeatures.TriggerAutomatic, Source: langfeatures.SourceDefault}
	ctx, cancel := context.WithCancel(vm.hostCtx())
	c.autoCancel = cancel
	c.autoGen++
	gen, start := c.autoGen, sel.Start()
	go func() {
		set, errs := reg.GetCodeActions(ctx, doc, req, trig)
		vm.post(func() { vm.lightBulbResult(gen, doc.Version, start, set, errs) })
	}()
}

// lightBulbResult takes the answer of the request of the light bulb.
func (vm *ViewModel) lightBulbResult(gen, version int, start buffer.Position, set langfeatures.CodeActionSet, errs []langfeatures.ProviderError) {
	c := &vm.ca
	if gen != c.autoGen || vm.doc.Version() != version || vm.cursors.Primary().Selection.Start().Line != start.Line {
		return
	}
	if c.autoCancel != nil {
		c.autoCancel()
		c.autoCancel = nil
	}
	vm.logProviderErrors("code action provider", errs)
	if len(set.Valid) == 0 || !vm.focused {
		vm.hideLightBulb()
		return
	}
	icon := LightBulbPlain
	title := "Show Code Actions"
	key := func(id string) string {
		if h := vm.sg.host; h != nil && h.KeyLabel != nil {
			return h.KeyLabel(id)
		}
		return ""
	}
	if set.HasAutoFix() {
		icon = LightBulbAutoFix
		title = "Show Code Actions. Preferred Quick Fix Available"
		if k := key("editor.action.autoFix"); k != "" {
			title += " (" + k + ")"
		}
	} else if k := key("editor.action.quickFix"); k != "" {
		title += " (" + k + ")"
	}
	c.bulb = &LightBulb{Pos: start, Icon: icon, Title: title, Actions: set}
	vm.codeActionDocListen()
	vm.codeActionsChanged()
}

func (vm *ViewModel) hideLightBulb() {
	if vm.ca.bulb != nil {
		vm.ca.bulb = nil
		vm.codeActionsChanged()
	}
}

// codeActionDocListen hides the light bulb and the list when the text changes: their actions are for the old text.
func (vm *ViewModel) codeActionDocListen() {
	c := &vm.ca
	if c.unsubDoc != nil {
		return
	}
	c.unsubDoc = vm.doc.Subscribe(func(document.ChangeEvent) {
		vm.hideLightBulb()
		vm.CodeActionHide()
	})
}

// logProviderErrors writes the failures of the providers to the log of the host, except a request that was cancelled.
func (vm *ViewModel) logProviderErrors(what string, errs []langfeatures.ProviderError) {
	h := vm.sg.host
	if h == nil || h.Log == nil {
		return
	}
	for _, e := range errs {
		if !errors.Is(e.Err, context.Canceled) {
			h.Log(what+" "+e.Owner+" failed", e.Err)
		}
	}
}

// allDiagnostics are the diagnostics of the editor as the providers take them in the context of a request; read on the UI thread
// and handed to the goroutine of the request.
func (vm *ViewModel) allDiagnostics() []langfeatures.Diagnostic {
	ds := vm.Diagnostics()
	out := make([]langfeatures.Diagnostic, len(ds))
	for i, d := range ds {
		out[i] = langfeatures.Diagnostic{
			Range: d.Range, Severity: severityOf(d.Severity), Message: d.Message, Source: d.Source, Code: d.Code, CodeHref: d.CodeHref,
			Unnecessary: d.Unnecessary, Deprecated: d.Deprecated, Raw: json.RawMessage(d.Data),
		}
	}
	return out
}

// diagnosticsTouching are the diagnostics that touch the range: the context of a request.
func diagnosticsTouching(ds []langfeatures.Diagnostic, r buffer.Range) []langfeatures.Diagnostic {
	var out []langfeatures.Diagnostic
	for _, d := range ds {
		if !d.Range.End.IsBefore(r.Start) && !r.End.IsBefore(d.Range.Start) {
			out = append(out, d)
		}
	}
	return out
}

// severityOf maps the severity of the editor's diagnostics to the providers'.
func severityOf(s DiagnosticSeverity) langfeatures.Severity {
	switch s {
	case DiagnosticWarning:
		return langfeatures.SeverityWarning
	case DiagnosticInfo:
		return langfeatures.SeverityInfo
	case DiagnosticHint:
		return langfeatures.SeverityHint
	}
	return langfeatures.SeverityError
}

// LightBulbClick opens the list of the actions of the light bulb.
func (vm *ViewModel) LightBulbClick() {
	b := vm.ca.bulb
	if b == nil {
		return
	}
	vm.showCodeActionList(b.Actions, b.Pos, true, false)
}

// ---- commands ----

// TriggerCodeActions asks for the actions at the cursor for a command and does what the trigger says with them (VS Code's
// manualTriggerAtCurrentPosition): applies the only one or the first when the trigger says so, tells the user when there are
// none (notAvailable), or opens the list. The answer is dropped when the cursor moved or the text changed meanwhile.
func (vm *ViewModel) TriggerCodeActions(trig langfeatures.CodeActionTrigger, notAvailable string) {
	c := &vm.ca
	_, reg, ok := vm.codeActionsHost()
	if !ok {
		return
	}
	vm.CodeActionHide()
	vm.HideHover()
	if c.opCancel != nil {
		c.opCancel()
	}
	c.opGen++
	gen := c.opGen
	ctx, cancel := context.WithCancel(vm.hostCtx())
	c.opCancel = cancel
	sel := vm.cursors.Primary().Selection
	doc := vm.langDocument()
	pos, start := sel.Active, sel.Start()
	nearby := vm.opts.CodeActions.IncludeNearbyQuickFixes
	diags := vm.allDiagnostics()
	go func() {
		set, errs := requestCodeActions(ctx, reg, doc, diags, sel.Range(), pos, trig, nearby)
		vm.post(func() { vm.codeActionsInvoked(gen, doc.Version, start, trig, notAvailable, set, errs) })
	}()
}

// requestCodeActions asks the providers for the actions of a range. For Quick Fix with no quick fix at the cursor it also asks
// at the end of each diagnostic of the cursor's line, closest first (editor.codeActionWidget.includeNearbyQuickFixes).
//
// Ported from VS Code: codeActionModel.ts (the asynchronous function of the oracle)
func requestCodeActions(ctx context.Context, reg *langfeatures.Registry, doc langfeatures.Document, diags []langfeatures.Diagnostic, rng buffer.Range, cursor buffer.Position, trig langfeatures.CodeActionTrigger, nearby bool) (langfeatures.CodeActionSet, []langfeatures.ProviderError) {
	req := langfeatures.CodeActionRequest{Range: rng, Diagnostics: diagnosticsTouching(diags, rng)}
	set, errs := reg.GetCodeActions(ctx, doc, req, trig)
	quickFix := trig.Source == langfeatures.SourceQuickFix || trig.Filter.Include != nil && langfeatures.KindContains(*trig.Filter.Include, langfeatures.KindQuickFix)
	if !nearby || trig.Type != langfeatures.TriggerInvokedAction || !quickFix || ctx.Err() != nil {
		return set, errs
	}
	if slices.ContainsFunc(set.Valid, func(a langfeatures.CodeAction) bool {
		return a.Kind != "" && langfeatures.KindContains(langfeatures.KindQuickFix, a.Kind)
	}) {
		return set, errs
	}
	var near []langfeatures.CodeAction
	distance := int(^uint(0) >> 1)
	for _, d := range diags {
		if d.Range.Start.Line != cursor.Line && d.Range.End.Line != cursor.Line {
			continue
		}
		at := d.Range.End
		nt := langfeatures.CodeActionTrigger{Type: trig.Type, Source: trig.Source, Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindQuickFix)}}
		r := buffer.Range{Start: at, End: at}
		more, merrs := reg.GetCodeActions(ctx, doc, langfeatures.CodeActionRequest{Range: r, Diagnostics: diagnosticsTouching(diags, r)}, nt)
		errs = append(errs, merrs...)
		if ctx.Err() != nil {
			return set, errs
		}
		if len(more.Valid) == 0 {
			continue
		}
		away := at.Col - cursor.Col
		if away < 0 {
			away = -away
		}
		if away < distance {
			near = append(slices.Clone(more.Valid), near...)
		} else {
			near = append(near, more.Valid...)
		}
		distance = away
	}
	if len(near) == 0 {
		return set, errs
	}
	seen := map[string]bool{}
	near = slices.DeleteFunc(append(slices.Clone(set.Valid), near...), func(a langfeatures.CodeAction) bool {
		dup := seen[a.Title]
		seen[a.Title] = true
		return dup
	})
	slices.SortStableFunc(near, func(a, b langfeatures.CodeAction) int {
		switch {
		case a.Preferred && !b.Preferred:
			return -1
		case !a.Preferred && b.Preferred:
			return 1
		}
		return 0
	})
	return langfeatures.CodeActionSet{All: slices.Concat(set.All, near), Valid: near}, errs
}

// codeActionsInvoked takes the answer of a command.
func (vm *ViewModel) codeActionsInvoked(gen, version int, start buffer.Position, trig langfeatures.CodeActionTrigger, notAvailable string, set langfeatures.CodeActionSet, errs []langfeatures.ProviderError) {
	c := &vm.ca
	if gen != c.opGen || vm.doc.Version() != version || vm.cursors.Primary().Selection.Start().Line != start.Line {
		return
	}
	if c.opCancel != nil {
		c.opCancel()
		c.opCancel = nil
	}
	vm.logProviderErrors("code action provider", errs)
	includeDisabled := trig.Filter.Include != nil
	if includeDisabled {
		// a command for a kind of action applies the only valid action or the first, without the list
		if len(set.Valid) > 0 && (trig.AutoApply == langfeatures.AutoApplyFirst || trig.AutoApply == langfeatures.AutoApplyIfSingle && len(set.Valid) == 1) {
			vm.hideLightBulb()
			vm.ApplyCodeAction(set.Valid[0], false, false)
			return
		}
		// an action that would have been applied but is disabled says why
		if len(set.All) > 0 && (trig.AutoApply == langfeatures.AutoApplyFirst && len(set.Valid) == 0 || trig.AutoApply == langfeatures.AutoApplyIfSingle && len(set.All) == 1) {
			if i := slices.IndexFunc(set.All, func(a langfeatures.CodeAction) bool { return a.Disabled != "" }); i >= 0 {
				vm.ShowMessage(set.All[i].Disabled)
				return
			}
		}
	}
	if len(set.All) == 0 || !includeDisabled && len(set.Valid) == 0 {
		vm.ShowMessage(notAvailable)
		return
	}
	vm.showCodeActionList(set, vm.cursors.Primary().Selection.Active, false, includeDisabled)
}

// ---- the list ----

// codeActionGroups are the headers of the list and the kinds each takes, in order; the last takes what the others did not.
//
// Ported from VS Code: codeActionMenu.ts (codeActionGroups)
var codeActionGroups = []struct {
	kind, title string
	icon        int
}{
	{langfeatures.KindQuickFix, "Quick Fix", 0},
	{langfeatures.KindRefactorExtract, "Extract", fonts.CodiconTools},
	{langfeatures.KindRefactorInline, "Inline", fonts.CodiconTools},
	{langfeatures.KindRefactorRewrite, "Rewrite", fonts.CodiconTools},
	{langfeatures.KindRefactorMove, "Move", fonts.CodiconTools},
	{langfeatures.KindSurroundWith, "Surround With", fonts.CodiconSurroundWith},
	{langfeatures.KindSource, "Source Action", codiconSymbolFile},
	{"", "More Actions...", 0},
}

// codiconSymbolFile is the icon of the source actions (the Codicon symbol-file).
const codiconSymbolFile = 0xEA7B

// codeActionKeyFor is the label of the key of an action: Organize Imports and Fix All have commands of their own.
func (vm *ViewModel) codeActionKeyFor(a langfeatures.CodeAction) string {
	h := vm.sg.host
	if h == nil || h.KeyLabel == nil || a.Kind == "" {
		return ""
	}
	switch {
	case langfeatures.KindContains(langfeatures.KindSourceOrganize, a.Kind):
		return h.KeyLabel("editor.action.organizeImports")
	case langfeatures.KindContains(langfeatures.KindSourceFixAll, a.Kind):
		return h.KeyLabel("editor.action.fixAll")
	}
	return ""
}

// actionListItems are the rows of the list.
//
// Ported from VS Code: codeActionMenu.ts (toMenuItems)
func (vm *ViewModel) actionListItems(actions []langfeatures.CodeAction) []ActionListItem {
	canPreview := func(a langfeatures.CodeAction) bool { return a.Edit != nil }
	if !vm.opts.CodeActions.ShowHeaders {
		items := make([]ActionListItem, len(actions))
		for i, a := range actions {
			title := a.Title
			if a.Disabled != "" {
				title = a.Disabled
			}
			items[i] = ActionListItem{Title: title, Action: a, Disabled: a.Disabled != "", CanPreview: canPreview(a)}
		}
		return items
	}
	groups := make([][]langfeatures.CodeAction, len(codeActionGroups))
	for _, a := range actions {
		// the last group has the empty kind, which holds every kind
		for g, group := range codeActionGroups {
			if langfeatures.KindContains(group.kind, a.Kind) {
				groups[g] = append(groups[g], a)
				break
			}
		}
	}
	var items []ActionListItem
	for g, as := range groups {
		if len(as) == 0 {
			continue
		}
		items = append(items, ActionListItem{Header: true, Title: codeActionGroups[g].title})
		for _, a := range as {
			items = append(items, ActionListItem{
				Title: a.Title, Action: a, Disabled: a.Disabled != "", Key: vm.codeActionKeyFor(a),
				Icon: codeActionGroups[g].icon, CanPreview: canPreview(a),
			})
		}
	}
	return items
}

// showCodeActionList opens the list of the actions at a position.
//
// Ported from VS Code: codeActionController.ts (showCodeActionList)
func (vm *ViewModel) showCodeActionList(set langfeatures.CodeActionSet, at buffer.Position, fromLightbulb, includeDisabled bool) {
	c := &vm.ca
	actions := set.Valid
	if includeDisabled && (c.showDisabled || len(set.Valid) == 0) {
		actions = set.All
	}
	if len(actions) == 0 {
		return
	}
	l := &ActionList{Anchor: at, set: set, version: vm.doc.Version(), fromLightbulb: fromLightbulb, includeDisabled: includeDisabled}
	l.Items = vm.actionListItems(actions)
	l.Focus = -1
	l.Focus = nextEnabledItem(l.Items, -1, 1)
	if !fromLightbulb {
		if includeDisabled && len(set.Valid) > 0 && len(set.All) != len(set.Valid) {
			label := "Show Disabled"
			if c.showDisabled {
				label = "Hide Disabled"
			}
			l.Bar = append(l.Bar, ActionBarItem{Label: label, Run: func() {
				c.showDisabled = !c.showDisabled
				vm.showCodeActionList(set, at, fromLightbulb, includeDisabled)
			}})
		}
	}
	c.list = l
	vm.codeActionDocListen()
	vm.codeActionsChanged()
}

// nextEnabledItem is the row after (dir 1) or before (dir -1) the row `from` that can be chosen, cycling; -1 when none can.
func nextEnabledItem(items []ActionListItem, from, dir int) int {
	n := len(items)
	for i := 1; i <= n; i++ {
		j := ((from+dir*i)%n + n) % n
		if !items[j].Header && !items[j].Disabled {
			return j
		}
	}
	return -1
}

// CodeActionHide closes the list of actions.
func (vm *ViewModel) CodeActionHide() {
	if vm.ca.list == nil {
		return
	}
	vm.ca.list = nil
	vm.codeActionsChanged()
}

// CodeActionMenuMove moves the focus of the list to the next (1) or the previous (-1) row that can be chosen.
func (vm *ViewModel) CodeActionMenuMove(dir int) {
	l := vm.ca.list
	if l == nil {
		return
	}
	if j := nextEnabledItem(l.Items, l.Focus, dir); j >= 0 && j != l.Focus {
		l.Focus = j
		vm.codeActionsChanged()
	}
}

// CodeActionMenuFocus focuses a row, as the pointer moving over it does.
func (vm *ViewModel) CodeActionMenuFocus(i int) {
	l := vm.ca.list
	if l == nil || i < 0 || i >= len(l.Items) || l.Items[i].Header || l.Items[i].Disabled || l.Focus == i {
		return
	}
	l.Focus = i
	vm.codeActionsChanged()
}

// CodeActionMenuAccept applies the focused action, or shows its changes first with preview.
func (vm *ViewModel) CodeActionMenuAccept(preview bool) {
	l := vm.ca.list
	if l == nil || l.Focus < 0 || l.Focus >= len(l.Items) {
		return
	}
	it := l.Items[l.Focus]
	if it.Header || it.Disabled {
		return
	}
	vm.CodeActionHide()
	vm.ApplyCodeAction(it.Action, true, preview)
}

// CodeActionMenuClick applies the action of a row.
func (vm *ViewModel) CodeActionMenuClick(i int) {
	vm.CodeActionMenuFocus(i)
	vm.CodeActionMenuAccept(false)
}

// ---- applying ----

// ApplyCodeAction applies an action: it asks the provider for the edit when the action has none and the provider can resolve
// it (codeAction/resolve), makes the edit (after showing it when preview is set), and then runs the command. With retrigger the
// light bulb asks again afterwards. Everything but the edit and the command happens off the UI thread.
//
// Ported from VS Code: codeAction.ts (applyCodeAction)
func (vm *ViewModel) ApplyCodeAction(a langfeatures.CodeAction, retrigger, preview bool) {
	h := vm.sg.host
	if h == nil || h.Features == nil {
		return
	}
	reg := h.Features
	ctx := vm.hostCtx()
	done := func() {
		if retrigger {
			vm.codeActionsSchedule(codeActionDelay)
		}
	}
	finish := func(a langfeatures.CodeAction) {
		vm.ca.applied = &a
		after := func() {
			if a.Command != nil && h.Execute != nil {
				if err := h.Execute(ctx, *a.Command); err != nil {
					vm.reportError(err)
				}
			}
			done()
		}
		if a.Edit == nil {
			after()
			return
		}
		if preview && vm.opts.RenamePreview {
			vm.PreviewEdit(a.Title, a.Edit, func(err error) {
				if err == nil {
					after()
				}
			})
			return
		}
		if h.ApplyEdit == nil {
			return
		}
		if err := h.ApplyEdit(ctx, a.Edit); err != nil {
			vm.reportError(err)
			return
		}
		after()
	}
	if a.Edit != nil || !reg.CanResolveCodeAction(a) {
		finish(a)
		return
	}
	go func() {
		resolved, err := reg.ResolveCodeAction(ctx, a)
		vm.post(func() {
			if err != nil {
				if h.Log != nil && !errors.Is(err, context.Canceled) {
					h.Log("code action resolve failed", err)
				}
				resolved = a
			}
			finish(resolved)
		})
	}()
}

// ---- the actions ----

// codeActionArgs are the arguments of the command editor.action.codeAction: the kind wanted, whether to apply the action without
// the list, and whether only the preferred actions count.
type codeActionArgs struct {
	kind      string
	hasKind   bool
	apply     langfeatures.CodeActionAutoApply
	preferred bool
}

// parseCodeActionArgs reads the arguments of a command, with the defaults of the command.
//
// Ported from VS Code: src/vs/editor/contrib/codeAction/common/types.ts (CodeActionCommandArgs.fromUser)
func parseCodeActionArgs(args any, defKind string, defApply langfeatures.CodeActionAutoApply) codeActionArgs {
	out := codeActionArgs{kind: defKind, apply: defApply}
	m, ok := args.(map[string]any)
	if !ok {
		return out
	}
	if k, ok := m["kind"].(string); ok {
		out.kind, out.hasKind = k, true
	}
	if a, ok := m["apply"].(string); ok {
		switch strings.ToLower(a) {
		case "first":
			out.apply = langfeatures.AutoApplyFirst
		case "never":
			out.apply = langfeatures.AutoApplyNever
		case "ifsingle":
			out.apply = langfeatures.AutoApplyIfSingle
		}
	}
	if p, ok := m["preferred"].(bool); ok {
		out.preferred = p
	}
	return out
}

// noneMessage is the message of a command that found nothing: "No <what> available", for the kind and the preferred ones.
func noneMessage(a codeActionArgs, what, def string) string {
	switch {
	case a.hasKind && a.preferred:
		return "No preferred " + what + " for '" + a.kind + "' available"
	case a.hasKind:
		return "No " + what + " for '" + a.kind + "' available"
	case a.preferred:
		return "No preferred " + what + " available"
	}
	return def
}

// codeActionActions are the commands of the code actions and of the list.
//
// Ported from VS Code: codeActionCommands.ts and src/vs/platform/actionWidget/browser/actionWidget.ts
func codeActionActions(a map[string]action) {
	trigger := func(trig langfeatures.CodeActionTrigger, msg string) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions, vm.skipReveal = true, true
			vm.TriggerCodeActions(trig, msg)
			return nil
		}
	}
	invoked := func(src langfeatures.CodeActionSource, f langfeatures.CodeActionFilter, apply langfeatures.CodeActionAutoApply) langfeatures.CodeActionTrigger {
		return langfeatures.CodeActionTrigger{Type: langfeatures.TriggerInvokedAction, Source: src, Filter: f, AutoApply: apply}
	}
	a["editor.action.quickFix"] = trigger(invoked(langfeatures.SourceQuickFix, langfeatures.CodeActionFilter{}, langfeatures.AutoApplyNever), "No code actions available")
	a["editor.action.codeAction"] = func(vm *ViewModel, args any) error {
		p := parseCodeActionArgs(args, "", langfeatures.AutoApplyIfSingle)
		msg := noneMessage(p, "code actions", "No code actions available")
		f := langfeatures.CodeActionFilter{Include: langfeatures.Kind(p.kind), IncludeSourceActions: true, OnlyPreferred: p.preferred}
		return trigger(invoked(langfeatures.SourceDefault, f, p.apply), msg)(vm, nil)
	}
	a["editor.action.refactor"] = func(vm *ViewModel, args any) error {
		p := parseCodeActionArgs(args, langfeatures.KindRefactor, langfeatures.AutoApplyNever)
		kind := p.kind
		if !langfeatures.KindContains(langfeatures.KindRefactor, kind) {
			kind = "none"
		}
		f := langfeatures.CodeActionFilter{Include: langfeatures.Kind(kind), OnlyPreferred: p.preferred}
		return trigger(invoked(langfeatures.SourceRefactor, f, p.apply), noneMessage(p, "refactorings", "No refactorings available"))(vm, nil)
	}
	a["editor.action.sourceAction"] = func(vm *ViewModel, args any) error {
		p := parseCodeActionArgs(args, langfeatures.KindSource, langfeatures.AutoApplyNever)
		kind := p.kind
		if !langfeatures.KindContains(langfeatures.KindSource, kind) {
			kind = "none"
		}
		f := langfeatures.CodeActionFilter{Include: langfeatures.Kind(kind), IncludeSourceActions: true, OnlyPreferred: p.preferred}
		return trigger(invoked(langfeatures.SourceSourceAction, f, p.apply), noneMessage(p, "source actions", "No source actions available"))(vm, nil)
	}
	a["editor.action.organizeImports"] = trigger(invoked(langfeatures.SourceOrganizeImports,
		langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindSourceOrganize), IncludeSourceActions: true}, langfeatures.AutoApplyIfSingle),
		"No organize imports action available")
	a["editor.action.fixAll"] = trigger(invoked(langfeatures.SourceFixAll,
		langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindSourceFixAll), IncludeSourceActions: true}, langfeatures.AutoApplyIfSingle),
		"No fix all action available")
	a["editor.action.autoFix"] = trigger(invoked(langfeatures.SourceAutoFix,
		langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindQuickFix), OnlyPreferred: true}, langfeatures.AutoApplyIfSingle),
		"No auto fixes available")

	only := func(f func(vm *ViewModel)) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions, vm.skipReveal = true, true
			f(vm)
			return nil
		}
	}
	a["hideCodeActionWidget"] = only(func(vm *ViewModel) { vm.CodeActionHide() })
	a["selectPrevCodeAction"] = only(func(vm *ViewModel) { vm.CodeActionMenuMove(-1) })
	a["selectNextCodeAction"] = only(func(vm *ViewModel) { vm.CodeActionMenuMove(1) })
	a["acceptSelectedCodeAction"] = only(func(vm *ViewModel) { vm.CodeActionMenuAccept(false) })
	a["previewSelectedCodeAction"] = only(func(vm *ViewModel) { vm.CodeActionMenuAccept(true) })
}
