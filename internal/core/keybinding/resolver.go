package keybinding

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// Item is one keybinding: a key sequence that runs a command when its when-clause holds.
//
// Ported from VS Code: src/vs/platform/keybinding/common/resolvedKeybindingItem.ts
type Item struct {
	// Keybinding is the key sequence as it was written; nil when the item has no key (a
	// removal that matches every key of the command, or an unbound command).
	Keybinding Keybinding
	// Chords are the dispatch strings of the keybinding (see Chord.Dispatch). They are empty
	// when the keybinding is nil or has a chord that cannot be dispatched.
	Chords []string
	// Command is the id of the command. A leading "-" marks a removal of the default
	// keybindings of that command; a leading "^" was cut off and set Bubble.
	Command string
	// Bubble is set when the command id started with "^": the key is not consumed by the
	// command, other handlers see it too.
	Bubble bool
	// Args are the JSON-compatible arguments passed to the command.
	Args any
	// When is the condition the context must satisfy; nil means always.
	When contextkey.Expr
	// IsDefault is set for the keybindings of the built-in keymap, false for the user's.
	IsDefault bool
	// FileEntry is the position of the entry in keybindings.json that made the item, counted from 1, or 0 for an
	// item that was not read from the file (the keybindings UI finds the entry to edit with it).
	FileEntry int

	// evalWhen is When with the constant keys (isLinux, …) replaced.
	evalWhen contextkey.Expr
}

// NewItem makes an Item. The command may start with "^" (see Item.Bubble) or "-" (a
// removal). A modifier-only keybinding such as "shift shift" is dispatched by the lone
// modifier names.
func NewItem(kb Keybinding, command string, args any, when contextkey.Expr, isDefault bool) Item {
	it := Item{Keybinding: kb, Args: args, When: when, IsDefault: isDefault}
	if kb != nil {
		it.Chords = kb.DispatchChords()
		if len(it.Chords) == 0 {
			it.Chords = kb.SingleModifierChords()
		}
	}
	it.Bubble = strings.HasPrefix(command, "^")
	it.Command = strings.TrimPrefix(command, "^")
	if when != nil {
		it.evalWhen = when.SubstituteConstants()
	}
	return it
}

// Replace makes the resolver what o is, in place: the holders of r (the menus, the status bar, the dispatcher of a
// window) see the keybindings of o from now on. It is how a window follows keybindings.json when the file changes.
func (r *Resolver) Replace(o *Resolver) { *r = *o }

// ResultKind tells what a key press resolved to.
type ResultKind int

// The kinds of resolution result.
const (
	// NoMatchingKb means no keybinding has this sequence of chords.
	NoMatchingKb ResultKind = iota
	// MoreChordsNeeded means a keybinding starts with this sequence but is longer.
	MoreChordsNeeded
	// KbFound means a keybinding matches and its command is to be run.
	KbFound
)

// Resolution is the result of resolving a key press.
type Resolution struct {
	Kind ResultKind
	// Command is the command to run for KbFound; empty when the matching keybinding has no
	// command (it unbinds the key).
	Command string
	// Args are the arguments of the command.
	Args any
	// Bubble tells that the key is not consumed (the command id had a "^" prefix).
	Bubble bool
}

// Resolver maps key sequences to commands and commands to key sequences.
//
// Ported from VS Code: src/vs/platform/keybinding/common/keybindingResolver.ts
type Resolver struct {
	log          func(string)
	defaults     []Item
	keybindings  []*Item
	defaultBound map[string]bool
	byFirstChord map[string][]*Item
	byCommand    map[string][]*Item
}

// NewResolver makes a resolver from the built-in keybindings and the user's, applying the
// removals ("-command" items) of both. log receives an explanation of every resolution; it
// may be nil.
func NewResolver(defaults, overrides []Item, log func(string)) *Resolver {
	if log == nil {
		log = func(string) {}
	}
	r := &Resolver{
		log:          log,
		defaults:     defaults,
		defaultBound: map[string]bool{},
		byFirstChord: map[string][]*Item{},
		byCommand:    map[string][]*Item{},
	}
	for _, d := range defaults {
		if d.Command != "" && !strings.HasPrefix(d.Command, "-") {
			r.defaultBound[d.Command] = true
		}
	}
	for _, it := range HandleRemovals(slices.Concat(defaults, overrides)) {
		r.keybindings = append(r.keybindings, &it)
	}
	for _, k := range r.keybindings {
		if len(k.Chords) == 0 {
			continue // unbound
		}
		if k.evalWhen != nil && k.evalWhen.Type() == contextkey.TypeFalse {
			continue // the condition can never hold
		}
		r.addKeyPress(k.Chords[0], k)
	}
	return r
}

// isTargetedForRemoval tells whether a removal with the given chords and when-clause
// removes the default keybinding defaultKb.
func isTargetedForRemoval(defaultKb Item, chords []string, when contextkey.Expr) bool {
	for i, c := range chords {
		if i >= len(defaultKb.Chords) || c != defaultKb.Chords[i] {
			return false
		}
	}
	// true means always, as does nil
	if when != nil && when.Type() != contextkey.TypeTrue {
		if defaultKb.When == nil {
			return false
		}
		// Implication rather than equality, so that a removal still matches when a later
		// version makes the when-clause of the default more specific (VS Code issue 293802).
		if !WhenIsEntirelyIncluded(defaultKb.When.SubstituteConstants(), when.SubstituteConstants()) {
			return false
		}
	}
	return true
}

// HandleRemovals drops the items whose command is a removal ("-command") and the default
// items they remove. Only default items can be removed.
func HandleRemovals(rules []Item) []Item {
	removals := map[string][]Item{}
	for _, rule := range rules {
		if strings.HasPrefix(rule.Command, "-") {
			cmd := rule.Command[1:]
			removals[cmd] = append(removals[cmd], rule)
		}
	}
	if len(removals) == 0 {
		return rules
	}
	var result []Item
	for _, rule := range rules {
		if rule.Command == "" {
			result = append(result, rule)
			continue
		}
		if strings.HasPrefix(rule.Command, "-") {
			continue
		}
		commandRemovals := removals[rule.Command]
		if len(commandRemovals) == 0 || !rule.IsDefault {
			result = append(result, rule)
			continue
		}
		removed := slices.ContainsFunc(commandRemovals, func(removal Item) bool {
			return isTargetedForRemoval(rule, removal.Chords, removal.When)
		})
		if !removed {
			result = append(result, rule)
		}
	}
	return result
}

// addKeyPress files an item under its first chord. An earlier item for the same first
// chord that the new one completely overrides is dropped from the command lookup.
func (r *Resolver) addKeyPress(keypress string, item *Item) {
	conflicts := r.byFirstChord[keypress]
	if len(conflicts) == 0 {
		r.byFirstChord[keypress] = []*Item{item}
		r.addToLookup(item)
		return
	}
	for i := len(conflicts) - 1; i >= 0; i-- {
		conflict := conflicts[i]
		if conflict.Command == item.Command {
			continue
		}
		// If the shorter keybinding is a prefix of the longer one it shadows it, and the
		// two conflict.
		isShorterPrefix := true
		for j := 1; j < len(conflict.Chords) && j < len(item.Chords); j++ {
			if conflict.Chords[j] != item.Chords[j] {
				isShorterPrefix = false
				break
			}
		}
		if !isShorterPrefix {
			continue
		}
		if WhenIsEntirelyIncluded(conflict.When, item.When) {
			r.removeFromLookup(conflict)
		}
	}
	r.byFirstChord[keypress] = append(conflicts, item)
	r.addToLookup(item)
}

func (r *Resolver) addToLookup(item *Item) {
	if item.Command == "" {
		return
	}
	r.byCommand[item.Command] = append(r.byCommand[item.Command], item)
}

func (r *Resolver) removeFromLookup(item *Item) {
	if item.Command == "" {
		return
	}
	arr := r.byCommand[item.Command]
	if i := slices.Index(arr, item); i >= 0 {
		r.byCommand[item.Command] = slices.Delete(arr, i, i+1)
	}
}

// WhenIsEntirelyIncluded reports whether it is provable that when-clause a implies b. A nil
// clause or true is "always".
func WhenIsEntirelyIncluded(a, b contextkey.Expr) bool {
	if b == nil || b.Type() == contextkey.TypeTrue {
		return true
	}
	if a == nil || a.Type() == contextkey.TypeTrue {
		return false
	}
	return contextkey.Implies(a, b)
}

// DefaultBoundCommands are the commands that have a built-in keybinding.
func (r *Resolver) DefaultBoundCommands() map[string]bool { return r.defaultBound }

// DefaultKeybindings are the built-in items, before removals.
func (r *Resolver) DefaultKeybindings() []Item { return r.defaults }

// Keybindings are all the items in effect, after removals, built-in ones first.
func (r *Resolver) Keybindings() []Item {
	out := make([]Item, len(r.keybindings))
	for i, k := range r.keybindings {
		out[i] = *k
	}
	return out
}

// LookupKeybindings returns the keybindings that run a command, the most specific (the
// last one defined) first.
func (r *Resolver) LookupKeybindings(command string) []Item {
	items := r.byCommand[command]
	out := make([]Item, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		out = append(out, *items[i])
	}
	return out
}

// LookupPrimaryKeybinding returns the keybinding to show for a command: the last one whose
// when-clause holds in ctx. When none holds it returns the last one, or nothing when
// enforceContextCheck is set.
func (r *Resolver) LookupPrimaryKeybinding(command string, ctx contextkey.Context, enforceContextCheck bool) (Item, bool) {
	items := r.byCommand[command]
	if len(items) == 0 {
		return Item{}, false
	}
	if len(items) == 1 && !enforceContextCheck {
		return *items[0], true
	}
	for i := len(items) - 1; i >= 0; i-- {
		if matches(ctx, items[i]) {
			return *items[i], true
		}
	}
	if enforceContextCheck {
		return Item{}, false
	}
	return *items[len(items)-1], true
}

func matches(ctx contextkey.Context, it *Item) bool {
	if it.evalWhen == nil {
		return true
	}
	return it.evalWhen.Evaluate(ctx)
}

// Resolve tells what happens when the user presses keypress (a Chord.Dispatch string) after
// the chords in currentChords: ctrl+k is pending and ctrl+c is pressed, for example.
func (r *Resolver) Resolve(ctx contextkey.Context, currentChords []string, keypress string) Resolution {
	pressed := append(slices.Clone(currentChords), keypress)
	r.log("| Resolving " + strings.Join(pressed, ","))

	candidates, ok := r.byFirstChord[pressed[0]]
	if !ok {
		r.log("\\ No keybinding entries.")
		return Resolution{Kind: NoMatchingKb}
	}
	lookup := candidates
	if len(pressed) >= 2 {
		lookup = nil
		for _, c := range candidates {
			if len(pressed) > len(c.Chords) {
				continue // a keybinding needs at least as many chords as were pressed
			}
			if slices.Equal(c.Chords[1:len(pressed)], pressed[1:]) {
				lookup = append(lookup, c)
			}
		}
	}

	var found *Item
	for i := len(lookup) - 1; i >= 0; i-- {
		if matches(ctx, lookup[i]) {
			found = lookup[i]
			break
		}
	}
	if found == nil {
		r.log("\\ From " + strconv.Itoa(len(lookup)) + " keybinding entries, no when clauses matched the context.")
		return Resolution{Kind: NoMatchingKb}
	}
	if len(pressed) < len(found.Chords) {
		r.log("\\ Awaiting more chords for " + found.Command + ".")
		return Resolution{Kind: MoreChordsNeeded}
	}
	r.log("\\ Matched " + found.Command + ".")
	return Resolution{Kind: KbFound, Command: found.Command, Args: found.Args, Bubble: found.Bubble}
}
