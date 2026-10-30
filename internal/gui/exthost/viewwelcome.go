package exthost

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The welcome content of views (SPEC §10.30, step 24.3; VS Code's contributes.viewsWelcome and ViewsRegistry
// .registerViewWelcomeContent). A manifest declares pieces of Markdown with a condition for a view, which may be a view of
// Emetos (the Source Control view and the Explorer); the view shows the pieces that apply while it has nothing else to show.
//
// Ported from VS Code: src/vs/workbench/common/views.ts (ViewContentGroups, compareViewContentDescriptors) and
// src/vs/workbench/api/browser/viewsExtensionPoint.ts (viewsWelcome)

// viewWelcomeAliases are the names VS Code's manifests use for views of the workbench: "scm" is the Changes view of the Source
// Control container and "explorer" the Explorer when it has no folder.
var viewWelcomeAliases = map[string]string{
	"scm":      command.ViewSCM,
	"explorer": command.ViewExplorerEmpty,
}

// defaultWelcomeGroup is the group of content that names none (VS Code's ViewContentGroups.More).
const defaultWelcomeGroup = "9_more"

// WelcomeContent is a piece of welcome content that applies to a view in a context.
type WelcomeContent struct {
	// Extension is the id of the extension that declared the piece; its command links run commands of the window.
	Extension string
	// Contents is the Markdown.
	Contents string
	// Group and Order place the piece among the others of the view (the Group of the manifest, "5_scm@1", split at the "@").
	Group string
	Order int
	// enablement is the condition under which the buttons are enabled; nil is always.
	enablement contextkey.Expr
}

// Enabled tells whether the buttons of the piece are enabled in a context.
func (w WelcomeContent) Enabled(keys contextkey.Context) bool {
	return w.enablement == nil || keys == nil || w.enablement.Evaluate(keys)
}

// welcomeDecl is a piece of welcome content in the scope.
type welcomeDecl struct {
	WelcomeContent
	view string
	when contextkey.Expr
	slot *slot
}

// installViewWelcome adds the welcome content of a manifest to the scope and returns the functions that take it away. A piece
// whose condition does not parse is logged and left out.
func (s *Scope) installViewWelcome(sl *slot) (removers []func()) {
	m := sl.info.Manifest
	for _, spec := range m.Contributes.ViewWelcome {
		when, err := parseWhen(spec.When)
		if err != nil {
			s.log.Warn("welcome content of an extension is left out", "extension", m.ID, "view", spec.View, "err", err)
			continue
		}
		enablement, err := parseWhen(spec.Enablement)
		if err != nil {
			s.log.Warn("welcome content of an extension is left out", "extension", m.ID, "view", spec.View, "err", err)
			continue
		}
		view := spec.View
		if alias, ok := viewWelcomeAliases[view]; ok {
			view = alias
		}
		group, order := splitWelcomeGroup(spec.Group)
		d := &welcomeDecl{WelcomeContent: WelcomeContent{Extension: m.ID, Contents: spec.Contents, Group: group, Order: order, enablement: enablement},
			view: view, when: when, slot: sl}
		s.mu.Lock()
		s.welcomes = append(s.welcomes, d)
		s.mu.Unlock()
		removers = append(removers, func() {
			s.mu.Lock()
			s.welcomes = slices.DeleteFunc(s.welcomes, func(o *welcomeDecl) bool { return o == d })
			s.mu.Unlock()
		})
	}
	return removers
}

// splitWelcomeGroup splits "5_scm@1" into the group and the place in it (5 when there is none, VS Code's default).
func splitWelcomeGroup(g string) (group string, order int) {
	group, rest, found := strings.Cut(g, "@")
	if group == "" {
		group = defaultWelcomeGroup
	}
	order = 5
	if found {
		if n, err := strconv.Atoi(rest); err == nil {
			order = n
		}
	}
	return group, order
}

// ViewWelcome is the welcome content of a view that holds in a context, in the order the view shows it: by group, then by the
// place in the group, then as the manifests came. Content of a failed extension is not shown.
func (s *Scope) ViewWelcome(viewID string, keys contextkey.Context) []WelcomeContent {
	s.mu.Lock()
	var out []WelcomeContent
	for _, d := range s.welcomes {
		if d.view != viewID || d.slot.state == StateFailed {
			continue
		}
		if d.when != nil && keys != nil && !d.when.Evaluate(keys) {
			continue
		}
		out = append(out, d.WelcomeContent)
	}
	s.mu.Unlock()
	slices.SortStableFunc(out, func(a, b WelcomeContent) int {
		if c := strings.Compare(a.Group, b.Group); c != 0 {
			return c
		}
		return a.Order - b.Order
	})
	return out
}

// HasViewWelcome tells whether the extensions of the scope declare any welcome content for the view, whatever the context says.
func (s *Scope) HasViewWelcome(viewID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.welcomes, func(d *welcomeDecl) bool { return d.view == viewID })
}
