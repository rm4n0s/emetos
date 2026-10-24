package quickinput

import "strings"

// Ported from VS Code: src/vs/platform/quickinput/browser/helpQuickAccess.ts

// HelpPrefix is the prefix of the help mode.
const HelpPrefix = "?"

// NewHelpProvider makes the provider of the `?` mode: it lists the modes the registry has, with
// what to type and the key of the command that opens each, and choosing one (or typing its
// prefix) opens that mode. keybinding gives the label of the key of a command, and may be nil.
func NewHelpProvider(registry *Registry, keybinding func(commandID string) string) Provider {
	return &helpProvider{registry: registry, keybinding: keybinding}
}

type helpProvider struct {
	registry   *Registry
	keybinding func(string) string
}

// Info implements Provider.
func (p *helpProvider) Info() ProviderInfo {
	return ProviderInfo{
		Prefix: HelpPrefix, Placeholder: "Type '?' to get help on the actions you can take from here.",
		Help: []HelpEntry{{Description: "Show all Quick Access Providers"}},
	}
}

// helpItems lists the help entries of the providers, ordered by prefix; the help itself is left out.
func (p *helpProvider) helpItems() []Item {
	var items []Item
	for _, prov := range p.registry.Providers() {
		info := prov.Info()
		if info.Prefix == HelpPrefix {
			continue
		}
		for _, h := range info.Help {
			prefix := h.Prefix
			if prefix == "" {
				prefix = info.Prefix
			}
			label := prefix
			if label == "" {
				label = "…"
			}
			it := Item{ID: prefix, Label: label, Description: h.Description, Data: prefix}
			if h.Command != "" && p.keybinding != nil {
				it.Keybinding = p.keybinding(h.Command)
			}
			items = append(items, it)
		}
	}
	return items
}

// Provide implements Provider.
func (p *helpProvider) Provide(s *Session) Handler {
	s.Picker().SetItems(p.helpItems())
	return &helpHandler{p: p, s: s}
}

type helpHandler struct {
	BaseHandler
	p *helpProvider
	s *Session
}

// ValueChanged implements Handler: typing the prefix of a mode opens that mode.
func (h *helpHandler) ValueChanged(string) {
	rest := strings.TrimPrefix(h.s.Picker().Value(), HelpPrefix)
	if prov := h.p.registry.Resolve(rest); prov != nil {
		if prefix := prov.Info().Prefix; prefix != "" && prefix != HelpPrefix {
			h.s.Show(prefix, ShowOptions{PreserveValue: true})
		}
	}
}

// Accept implements Handler: the mode of the row opens.
func (h *helpHandler) Accept(row Row, _ KeyMods) {
	if prefix, ok := row.Item.Data.(string); ok {
		h.s.Show(prefix, ShowOptions{PreserveValue: true})
	}
}
