package outline

import (
	"slices"
	"strconv"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Opts configure an Outline; every member is optional.
type Opts struct {
	// Icons gives outline.icons (nil: true) and Show tells whether outline.showXxx shows a symbol kind (nil: all). Collapse gives
	// outline.collapseItems ("alwaysExpand", also when nil, or "alwaysCollapse").
	Icons    func() bool
	Show     func(kind int) bool
	Collapse func() string
	// OnReveal is called when the user chooses a symbol (a click or Enter): the editor and the symbol.
	OnReveal func(vm *editor.ViewModel, sym langfeatures.DocumentSymbol)
}

// Item is the data of a node of the tree: the symbol, and its key.
type Item struct {
	Symbol langfeatures.DocumentSymbol
	Key    string
}

// Outline is the model of the Outline view. It is used on the UI thread only.
type Outline struct {
	opts Opts
	tree *kit.TreeModel
	ctl  *kit.TreeController
	view *editor.ViewModel
	// handles follow the editor; shown is the symbols the tree was made from; follow says that the tree follows the cursor.
	selHandle, symHandle foundation.ListenerHandle
	shown                []langfeatures.DocumentSymbol
	has                  bool
	follow               bool
	listeners            []*func()
	// quiet keeps the selection that the follow makes from being taken for a choice of the user.
	quiet int
}

// New makes the model of an Outline that follows the cursor.
func New(opts Opts) *Outline {
	o := &Outline{opts: opts, tree: kit.NewTreeModel(), ctl: kit.NewTreeController(), follow: true}
	o.tree.SetToggleOnSelect(false)
	return o
}

// Tree is the model of the tree; Controller is the controller of the widget.
func (o *Outline) Tree() *kit.TreeModel { return o.tree }

// Controller is the controller of the tree widget.
func (o *Outline) Controller() *kit.TreeController { return o.ctl }

// Editor is the editor whose symbols the tree shows, or nil.
func (o *Outline) Editor() *editor.ViewModel { return o.view }

// HasSymbols tells whether the editor has an answer of a symbol provider.
func (o *Outline) HasSymbols() bool { return o.view != nil && o.has }

// Following tells whether the tree follows the cursor (outline.followCursor, on by default).
func (o *Outline) Following() bool { return o.follow }

// SetFollowing turns the following of the cursor on or off; on, the tree goes to the symbol of the cursor at once.
func (o *Outline) SetFollowing(on bool) {
	o.follow = on
	if on {
		o.followCursor()
	}
	o.notify()
}

// OnChange calls fn after the tree or what the view says changed, and returns the function that stops it.
func (o *Outline) OnChange(fn func()) (stop func()) {
	p := &fn
	o.listeners = append(o.listeners, p)
	return func() {
		if i := slices.Index(o.listeners, p); i >= 0 {
			o.listeners = slices.Delete(o.listeners, i, i+1)
		}
	}
}

func (o *Outline) notify() {
	for _, f := range slices.Clone(o.listeners) {
		(*f)()
	}
}

// SetEditor makes the tree show the symbols of an editor (nil for none): the active editor of the window.
func (o *Outline) SetEditor(vm *editor.ViewModel) {
	if vm == o.view {
		return
	}
	o.unfollowEditor()
	o.view = vm
	if vm != nil {
		o.selHandle = vm.AddSelectionListener(o.followCursor)
		o.symHandle = vm.AddSymbolsListener(o.Refresh)
	}
	o.shown, o.has = nil, false
	o.Refresh()
}

// Dispose stops following the editor.
func (o *Outline) Dispose() { o.unfollowEditor(); o.view = nil }

func (o *Outline) unfollowEditor() {
	if o.view != nil {
		o.view.RemoveSelectionListener(o.selHandle)
		o.view.RemoveSymbolsListener(o.symHandle)
	}
}

// Refresh makes the tree again from the symbols of the editor (the settings that filter the kinds changed, or the symbols did).
// The expansion and the selection stay by key; a tree that was empty starts expanded or collapsed as outline.collapseItems says.
func (o *Outline) Refresh() {
	var syms []langfeatures.DocumentSymbol
	has := false
	if o.view != nil {
		syms, has = o.view.Symbols()
	}
	first := !o.has || len(o.shown) == 0
	o.shown, o.has = syms, has
	items := o.items(syms, "")
	o.quiet++
	if err := o.tree.SetItems(items); err != nil {
		// the keys are made of unique names; a failure is a bug and leaves the old tree
		o.quiet--
		return
	}
	if first && o.opts.Collapse != nil && o.opts.Collapse() == "alwaysCollapse" {
		o.tree.CollapseAll()
	} else if first {
		o.tree.ExpandAll()
	}
	o.quiet--
	o.followCursor()
	o.notify()
}

// shownKind tells whether the settings show a kind.
func (o *Outline) shownKind(kind int) bool { return o.opts.Show == nil || o.opts.Show(kind) }

// keys are the keys of the symbols of one level: the name and the kind, and a count for a name that repeats, under the key of the
// symbol above.
func keys(parent string, syms []langfeatures.DocumentSymbol) []string {
	seen := map[string]int{}
	out := make([]string, len(syms))
	for i, s := range syms {
		k := parent + "/" + s.Name + "@" + strconv.Itoa(s.Kind)
		if n := seen[k]; n > 0 {
			out[i] = k + "~" + strconv.Itoa(n)
		} else {
			out[i] = k
		}
		seen[k]++
	}
	return out
}

// items are the nodes of a level of symbols; a symbol of a kind the settings hide is left out and its children take its place.
func (o *Outline) items(syms []langfeatures.DocumentSymbol, parent string) []kit.TreeItem {
	var out []kit.TreeItem
	for i, k := range keys(parent, syms) {
		s := syms[i]
		kids := o.items(s.Children, k)
		if !o.shownKind(s.Kind) {
			out = append(out, kids...)
			continue
		}
		out = append(out, kit.TreeItem{Key: k, Data: Item{Symbol: s, Key: k}, Children: kids, Label: s.Name})
	}
	return out
}

// followCursor selects the symbol that holds the cursor of the editor, with the folders above it open, and scrolls to it. It does
// nothing while the user turned the following off.
//
// Ported from VS Code: documentSymbolsOutline.ts (the reveal of the symbol at the cursor) and outlinePane.ts (followCursor)
func (o *Outline) followCursor() {
	if !o.follow || o.view == nil || !o.has {
		return
	}
	key := o.keyAt(o.shown, "", o.view.Cursors().Primary().Position())
	if key == "" || !o.tree.Has(key) {
		return
	}
	if cur, ok := o.tree.Selected(); ok && cur == key {
		return
	}
	o.quiet++
	for _, a := range o.tree.Ancestors(key) {
		o.tree.Expand(a)
	}
	o.tree.SetSelected(key)
	o.tree.SetFocus(key)
	o.quiet--
	o.ctl.Reveal(key)
	o.notify()
}

// keyAt is the key of the innermost shown symbol that holds a position.
func (o *Outline) keyAt(syms []langfeatures.DocumentSymbol, parent string, pos langfeatures.Position) string {
	ks := keys(parent, syms)
	for i, s := range syms {
		if !s.Range.ContainsPosition(pos) {
			continue
		}
		if inner := o.keyAt(s.Children, ks[i], pos); inner != "" {
			return inner
		}
		if o.shownKind(s.Kind) {
			return ks[i]
		}
		return ""
	}
	return ""
}

// Choose goes to the symbol of a node: the editor puts its cursor at the name of the symbol.
func (o *Outline) Choose(key string) {
	if o.quiet > 0 || o.view == nil {
		return
	}
	data, ok := o.tree.Data(key)
	if !ok {
		return
	}
	item, ok := data.(Item)
	if !ok {
		return
	}
	if o.opts.OnReveal != nil {
		o.opts.OnReveal(o.view, item.Symbol)
		return
	}
	o.view.RevealSymbol(&item.Symbol)
}

// Focus gives the keyboard focus to the tree.
func (o *Outline) Focus() { o.ctl.Focus() }

// CollapseAll folds every symbol.
func (o *Outline) CollapseAll() { o.tree.CollapseAll(); o.notify() }

// IconsOn tells whether the rows have icons.
func (o *Outline) IconsOn() bool { return o.opts.Icons == nil || o.opts.Icons() }
