package quickinput

import (
	"slices"
	"sort"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Ported from VS Code: src/vs/platform/quickinput/browser/commandsQuickAccess.ts and
// src/vs/workbench/contrib/quickaccess/browser/commandsQuickAccess.ts

// The prefix of the command palette and the id of the command that opens it.
const (
	// CommandsPrefix is the prefix of the mode that lists the commands.
	CommandsPrefix = ">"
	// ShowCommandsCommand is Show All Commands.
	ShowCommandsCommand = "workbench.action.showCommands"
	// defaultHistoryLength is the default of workbench.commandPalette.history.
	defaultHistoryLength = 50
)

// CommandsHistory remembers the commands that were run, the most recent last, up to a limit
// (VS Code's CommandsHistory, in memory until the UI state is persisted, SPEC §10.10). A command
// has a counter that grows with each run; the palette puts the commands that have one first, the
// highest counter first.
type CommandsHistory struct {
	counter map[string]int
	next    int
	limit   int
}

// NewCommandsHistory makes a history that keeps at most limit commands (0 keeps none).
func NewCommandsHistory(limit int) *CommandsHistory {
	return &CommandsHistory{counter: map[string]int{}, next: 1, limit: limit}
}

// SetLimit changes the number of commands kept; the oldest are dropped.
func (h *CommandsHistory) SetLimit(limit int) {
	h.limit = max(limit, 0)
	h.trim()
}

// Push records that a command was run.
func (h *CommandsHistory) Push(id string) {
	if h.limit <= 0 {
		return
	}
	h.counter[id] = h.next
	h.next++
	h.trim()
}

// Peek is the counter of a command, or 0 when it is not in the history.
func (h *CommandsHistory) Peek(id string) int { return h.counter[id] }

// Remove takes a command out of the history.
func (h *CommandsHistory) Remove(id string) { delete(h.counter, id) }

// Clear forgets every command.
func (h *CommandsHistory) Clear() {
	clear(h.counter)
	h.next = 1
}

// IDs lists the commands in the history, the most recent first.
func (h *CommandsHistory) IDs() []string {
	ids := make([]string, 0, len(h.counter))
	for id := range h.counter {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return h.counter[ids[i]] > h.counter[ids[j]] })
	return ids
}

func (h *CommandsHistory) trim() {
	if len(h.counter) <= h.limit {
		return
	}
	ids := h.IDs()
	for _, id := range ids[h.limit:] {
		delete(h.counter, id)
	}
}

// CommandsOptions are what the provider of the command palette needs.
type CommandsOptions struct {
	// Commands is the registry of the commands: those with a title are listed.
	Commands *command.Registry
	// Menus, when set, is asked for the CommandPalette menu: a command with an item there whose
	// When does not hold is left out of the palette.
	Menus *command.MenuRegistry
	// Context gives the context keys the preconditions and the keys are looked up in: the keys of
	// the widget that had the focus when the palette opened.
	Context func() contextkey.Context
	// Keybinding gives the label of the key of a command ("Ctrl+G"), or "".
	Keybinding func(id string, ctx contextkey.Context) string
	// Available, when set, is asked about each command with a title: false leaves it out. The window
	// uses it to list the commands of the editor only while an editor is open (VS Code lists the actions
	// of the active text editor, not of the editor that has the focus).
	Available func(id string, ctx contextkey.Context) bool
	// Run runs a command by id; the palette calls it after it is hidden.
	Run func(id string) error
	// OnError is told of a command that failed.
	OnError func(id string, err error)
	// History is the recently used commands; the provider makes one when nil.
	History *CommandsHistory
	// HistoryLimit gives the number of commands the history keeps (workbench.commandPalette.history);
	// the default of 50 when nil.
	HistoryLimit func() int
	// PreserveInput tells whether the last input is restored when the palette opens
	// (workbench.commandPalette.preserveInput).
	PreserveInput func() bool
}

// NewCommandsProvider makes the provider of the command palette: the commands that have a title
// and whose precondition holds, as "Category: Title" with their key, those that were used lately
// first (separated from the others by "recently used" and "other commands"), filtered by the words
// of the title.
func NewCommandsProvider(o CommandsOptions) *ListProvider {
	if o.History == nil {
		o.History = NewCommandsHistory(defaultHistoryLength)
	}
	p := &ListProvider{
		ProviderInfo: ProviderInfo{
			Prefix: CommandsPrefix, Placeholder: "Type the name of a command to run.", ContextKey: "inCommandsPicker",
			Help: []HelpEntry{{Description: "Show and Run Commands", Command: ShowCommandsCommand}},
		},
		NoResults: "No matching commands",
	}
	p.Picks = func(filter string) []Item { return commandPicks(o, filter) }
	if o.PreserveInput != nil {
		p.DefaultFilter = func() DefaultFilterKind {
			if o.PreserveInput() {
				return DefaultFilterLast
			}
			return DefaultFilterPreserve
		}
	}
	return p
}

// commandPick is a command as the palette lists it, before it is made an item.
type commandPick struct {
	id, label, category string
	highlights          []fuzzy.Match
}

// hiddenFromPalette collects the commands the CommandPalette menu hides.
func hiddenFromPalette(menus *command.MenuRegistry, ctx contextkey.Context) map[string]bool {
	hidden := map[string]bool{}
	if menus == nil {
		return hidden
	}
	for _, it := range menus.All() {
		if it.Menu == command.CommandPalette && it.Command != "" && it.When != nil && !it.When.Evaluate(ctx) {
			hidden[it.Command] = true
		}
	}
	return hidden
}

// paletteCommands lists the commands the palette offers in a context.
func paletteCommands(o CommandsOptions) []commandPick {
	var ctx contextkey.Context = contextkey.MapContext{}
	if o.Context != nil {
		if c := o.Context(); c != nil {
			ctx = c
		}
	}
	hidden := hiddenFromPalette(o.Menus, ctx)
	var picks []commandPick
	for _, m := range o.Commands.All() {
		if m.Title == "" || hidden[m.ID] || !o.Commands.Enabled(m.ID, ctx) || (o.Available != nil && !o.Available(m.ID, ctx)) {
			continue
		}
		label := m.Title
		if m.Category != "" {
			label = m.Category + ": " + m.Title
		}
		picks = append(picks, commandPick{id: m.ID, label: label, category: m.Category})
	}
	return picks
}

func commandPicks(o CommandsOptions, filter string) []Item {
	var ctx contextkey.Context = contextkey.MapContext{}
	if o.Context != nil {
		if c := o.Context(); c != nil {
			ctx = c
		}
	}
	if o.HistoryLimit != nil {
		o.History.SetLimit(o.HistoryLimit())
	}
	all := paletteCommands(o)
	picks := all[:0:0]
	for _, c := range all {
		if filter != "" {
			m, ok := fuzzy.MatchesCommandWords(filter, c.label)
			switch {
			case ok:
				c.highlights = m
			case filter == c.id:
			default:
				continue
			}
		}
		picks = append(picks, c)
	}
	// commands with the same label get their id as a description
	seen := map[string]int{}
	described := map[string]bool{}
	for i, c := range picks {
		if j, ok := seen[c.label]; ok {
			described[picks[j].id], described[c.id] = true, true
		} else {
			seen[c.label] = i
		}
	}
	h := o.History
	slices.SortStableFunc(picks, func(a, b commandPick) int {
		ca, cb := h.Peek(a.id), h.Peek(b.id)
		switch {
		case ca > 0 && cb > 0:
			return cb - ca
		case ca > 0:
			return -1
		case cb > 0:
			return 1
		}
		da, db := a.category == "Developer", b.category == "Developer"
		switch {
		case da && !db:
			return 1
		case !da && db:
			return -1
		}
		return fuzzy.Collate(a.label, b.label)
	})
	var items []Item
	addOther := false
	for i, c := range picks {
		inHistory := h.Peek(c.id) > 0
		if i == 0 && inHistory {
			items = append(items, Item{Separator: true, Label: "recently used"})
			addOther = true
		}
		if addOther && !inHistory {
			items = append(items, Item{Separator: true, Label: "other commands"})
			addOther = false
		}
		items = append(items, commandItem(o, c, inHistory, described[c.id], ctx))
	}
	return items
}

// commandItem makes the item of a command pick.
func commandItem(o CommandsOptions, c commandPick, recent, describe bool, ctx contextkey.Context) Item {
	it := Item{ID: c.id, Label: c.label, Highlights: Highlights{Label: c.highlights}}
	if describe {
		it.Description = c.id
	}
	if o.Keybinding != nil {
		it.Keybinding = o.Keybinding(c.id, ctx)
	}
	id := c.id
	it.Accept = func(KeyMods) {
		o.History.Push(id)
		if err := o.Run(id); err != nil && o.OnError != nil {
			o.OnError(id, err)
		}
	}
	if recent {
		it.Buttons = []Button{{Icon: fonts.CodiconClose, Tooltip: "Remove from Recently Used"}}
		it.Trigger = func(int, KeyMods) TriggerAction {
			o.History.Remove(id)
			return TriggerRemove
		}
	}
	return it
}

// ClearHistoryCommand is Clear Command History.
const ClearHistoryCommand = "workbench.action.clearCommandHistory"
