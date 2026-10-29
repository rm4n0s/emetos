package extension

// ExplorerArgs are the arguments of a command that the context menu of the Explorer (MenuExplorerContext) runs, when the
// command is an extension's (SPEC §10.15 "Menus", step 18.3): {"uri": "file:///path", "uris": ["file:///path", ...]}. uri is the
// row that was pressed (the folder of the workspace when the empty part of the tree was), uris are the rows the command works on:
// the whole selection when the pressed row is one of them, else the pressed row alone. A menu item whose own Args are an object
// keeps them: the two members are added to it.
const (
	// ExplorerArgURI and ExplorerArgURIs are the names of the two members.
	ExplorerArgURI  = "uri"
	ExplorerArgURIs = "uris"
)

// ResourceURIs reads the files or folders a command works on from the arguments the Explorer's context menu gave it: the URIs of
// the selected rows, the pressed one first; the one URI of the member "uri" when there is no list; nothing for any other argument
// (the command was run from the palette or from another menu).
func ResourceURIs(args any) []string {
	m, ok := args.(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if s, ok := m[ExplorerArgURI].(string); ok {
		add(s)
	}
	switch list := m[ExplorerArgURIs].(type) {
	case []any:
		for _, v := range list {
			if s, ok := v.(string); ok {
				add(s)
			}
		}
	case []string:
		for _, s := range list {
			add(s)
		}
	}
	return out
}
