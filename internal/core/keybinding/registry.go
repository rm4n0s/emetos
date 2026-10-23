package keybinding

import (
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// Weights order the built-in keybindings: of two bindings for the same key whose conditions
// both hold, the one with the greater weight wins (it comes later in the sorted list).
//
// Ported from VS Code: src/vs/platform/keybinding/common/keybindingsRegistry.ts
const (
	// WeightEditorCore is the weight of the editor's own commands.
	WeightEditorCore = 0
	// WeightEditorContrib is the weight of editor contributions.
	WeightEditorContrib = 100
	// WeightWorkbenchContrib is the weight of workbench commands.
	WeightWorkbenchContrib = 200
)

// Rule is a built-in keybinding as the code that registers a command writes it: a command
// with a primary key and secondary keys, in the syntax of keybindings.json. Keys are already
// those of Linux; an empty Primary registers no primary key.
type Rule struct {
	// Command is the command id, optionally with a "^" prefix.
	Command string
	// Weight orders rules (see WeightEditorCore).
	Weight int
	// Args are the arguments passed to the command.
	Args any
	// When is the condition for the keybinding; nil means always.
	When contextkey.Expr
	// Primary is the first key, such as "ctrl+shift+k".
	Primary string
	// Secondary are alternative keys for the same command.
	Secondary []string
}

// defaultItem is a built-in item with the two weights that order the keymap.
type defaultItem struct {
	item    Item
	weight1 int
	weight2 int
}

// Registry collects the built-in keybindings. Make one with NewRegistry; it has no global
// instance.
type Registry struct {
	items []defaultItem
}

// NewRegistry makes an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds the keys of a rule. The primary key has the second weight 0, the secondary
// keys -1, -2, … in the order given, so that the primary key sorts after them and is the one
// shown first for the command.
func (r *Registry) Register(rule Rule) error {
	type key struct {
		spec    string
		weight2 int
	}
	var keys []key
	if rule.Primary != "" {
		keys = append(keys, key{rule.Primary, 0})
	}
	for i, spec := range rule.Secondary {
		keys = append(keys, key{spec, -i - 1})
	}
	for _, k := range keys {
		kb, err := ParseKeybinding(k.spec)
		if err != nil {
			return err
		}
		if kb == nil {
			continue
		}
		r.items = append(r.items, defaultItem{
			item:    NewItem(kb, rule.Command, rule.Args, rule.When, true),
			weight1: rule.Weight, weight2: k.weight2,
		})
	}
	return nil
}

// MustRegister is Register for the built-in table; it panics on an invalid rule.
func (r *Registry) MustRegister(rules ...Rule) {
	for _, rule := range rules {
		if err := r.Register(rule); err != nil {
			panic(err)
		}
	}
}

// Defaults returns the built-in keybindings, ordered by weight, then by command, then by
// the second weight. The sort is stable, so rules registered first stay first among equals.
func (r *Registry) Defaults() []Item {
	sorted := append([]defaultItem(nil), r.items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.weight1 != b.weight1 {
			return a.weight1 < b.weight1
		}
		if a.item.Command != "" && b.item.Command != "" {
			if c := strings.Compare(a.item.Command, b.item.Command); c != 0 {
				return c < 0
			}
		}
		return a.weight2 < b.weight2
	})
	out := make([]Item, len(sorted))
	for i, d := range sorted {
		out[i] = d.item
	}
	return out
}
