package scm

import "testing"

// dump lists the rows of the tree view below a node, folders with a trailing slash, each level indented, down to the depth.
func dump(t *Tree, n *Node, compact bool, depth int) []string {
	var out []string
	var walk func(*Node, string, int)
	walk = func(n *Node, indent string, d int) {
		for _, e := range t.Children(n, compact) {
			if e.IsFolder() {
				out = append(out, indent+e.Label+"/")
				if d > 1 {
					walk(e.Node, indent+"  ", d-1)
				}
			} else {
				out = append(out, indent+e.Label)
			}
		}
	}
	walk(n, "", depth)
	return out
}

func TestTreeRows(t *testing.T) {
	paths := []string{
		"/repo/src/a/b/c.go", "/repo/src/a/b/d.go", "/repo/src/x.go", "/repo/docs/guide/intro.md", "/repo/README.md",
		"/repo/lib/only/one/file.go", "/repo/lib/only/one/two/deeper.go", "/repo/Zed/y.go", "/repo/src/a/b/c.go",
		"/repo/ünï/日本/a.go", "/repo/b10.txt", "/repo/b9.txt", "/repo/a/main.go", "/repo/b/main.go",
	}
	tests := []struct {
		name    string
		compact bool
		want    []string
	}{
		{
			"compact folders: single-folder chains are one row, files are never joined",
			true,
			[]string{
				"a/", "  main.go",
				"b/", "  main.go",
				"docs/guide/", "  intro.md",
				"lib/only/one/", "  two/", "    deeper.go", "  file.go",
				"src/", "  a/b/", "    c.go", "    d.go", "  x.go",
				"ünï/日本/", "  a.go",
				"Zed/", "  y.go",
				"b9.txt", "b10.txt", "README.md",
			},
		},
		{
			"without compact folders",
			false,
			[]string{
				"a/", "  main.go",
				"b/", "  main.go",
				"docs/", "  guide/", "    intro.md",
				"lib/", "  only/", "    one/", "      two/", "        deeper.go", "      file.go",
				"src/", "  a/", "    b/", "      c.go", "      d.go", "  x.go",
				"ünï/", "  日本/", "    a.go",
				"Zed/", "  y.go",
				"b9.txt", "b10.txt", "README.md",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := NewTree(resources(paths...), "/repo")
			if got := dump(tree, tree.Root(), tt.compact, 10); !equalStrings(got, tt.want) {
				t.Errorf("rows:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestTreeNodes(t *testing.T) {
	tree := NewTree(resources("/repo/src/a.go", "/repo/src/sub/b.go", "/repo/c.go", "/repo/src/a.go", "/elsewhere/d.go"), "/repo")
	root := tree.Root()
	if root.Parent() != nil || root.Path() != "" {
		t.Errorf("root = %q parent %v", root.Path(), root.Parent())
	}
	var src *Node
	for _, e := range tree.Children(root, false) {
		if e.Label == "src" {
			src = e.Node
		}
	}
	rows := tree.Children(src, false)
	var sub *Node
	for _, e := range rows {
		if e.Label == "sub" {
			sub = e.Node
		}
	}
	if sub == nil || sub.Path() != "src/sub" || !sub.IsFolder() || sub.Name() != "sub" || sub.Parent().Name() != "src" {
		t.Fatalf("sub = %+v", sub)
	}
	all := tree.Root().Resources()
	if got := len(all); got != 4 {
		t.Errorf("the resources of the root = %d, want 4 (a path put in twice is one)", got)
	}
	gotPaths := []string{}
	for _, r := range all {
		gotPaths = append(gotPaths, r.Path)
	}
	// depth first, in the order the paths were put in
	if want := []string{"/repo/src/a.go", "/repo/src/sub/b.go", "/repo/c.go", "/elsewhere/d.go"}; !equalStrings(gotPaths, want) {
		t.Errorf("collected %v, want %v", gotPaths, want)
	}
	if got := sub.Resources(); len(got) != 1 || got[0].Path != "/repo/src/sub/b.go" {
		t.Errorf("below sub: %v", got)
	}
}
