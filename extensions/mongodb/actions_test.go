package mongodb

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// nodesOf walks the fixture server to the nodes the actions tests ask about.
func nodesOf(t *testing.T, r *rig, c dbconn.Connection) map[string]dbconn.Node {
	t.Helper()
	out := map[string]dbconn.Node{}
	shop := find(t, r.children(c, nil), "shop")
	out["database"] = shop
	colls := r.children(c, &shop)
	orders := find(t, colls, "orders")
	out["collection"] = orders
	out["view"] = find(t, colls, "open_orders")
	groups := r.children(c, &orders)
	out["indexes"] = find(t, groups, "Indexes")
	out["schema"] = find(t, groups, "Schema")
	idx := r.children(c, ptrTo(out["indexes"]))
	out["index"] = find(t, idx, "sku_1_created_-1")
	out["idindex"] = find(t, idx, "_id_")
	out["field"] = r.children(c, ptrTo(out["schema"]))[0]
	return out
}

func ptrTo[T any](v T) *T { return &v }

func TestTheContextMenuOfEachNode(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	nodes := nodesOf(t, r, c)
	cases := []struct {
		name string
		node *dbconn.Node // nil: the connection
		want []string     // "title (group)"
	}{
		{"the connection", nil, []string{"New Playground (1-open)"}},
		{"a database", ptrTo(nodes["database"]), []string{"New Playground (1-open)", "Create Collection (2-create)", "Drop Database (3-drop)"}},
		{"a collection", ptrTo(nodes["collection"]), []string{"New Playground (1-open)", "Create Index (2-create)", "Drop Collection (3-drop)"}},
		{"a view", ptrTo(nodes["view"]), []string{"New Playground (1-open)", "Drop Collection (3-drop)"}},
		{"the indexes of a collection", ptrTo(nodes["indexes"]), []string{"Create Index (2-create)"}},
		{"an index", ptrTo(nodes["index"]), []string{"Drop Index (3-drop)"}},
		{"the index of _id", ptrTo(nodes["idindex"]), nil},
		{"the schema", ptrTo(nodes["schema"]), nil},
		{"a field", ptrTo(nodes["field"]), nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, a := range r.hub.Actions(c.ID, tt.node) {
				got = append(got, a.Title+" ("+a.Group+")")
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("actions = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTheActionsOpenPlaygroundsWithTheStatementWritten runs every action and checks the playground it opens: the exact text, the
// language, and that the text reads without a mistake and holds the statement an action promised.
func TestTheActionsOpenPlaygroundsWithTheStatementWritten(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	nodes := nodesOf(t, r, c)
	cases := []struct {
		name  string
		node  *dbconn.Node
		title string
		text  string
		stmts string // the statements the playground parses to
	}{
		{"new playground on the connection", nil, "New Playground", "// Shop\n\n", ``},
		{"new playground on a database", ptrTo(nodes["database"]), "New Playground", "// Shop\nuse shop\n\n", `use shop`},
		{"new playground on a collection", ptrTo(nodes["collection"]), "New Playground", "// Shop\nuse shop\n\ndb.getCollection(\"orders\").find({}).limit(20)\n", `use shop ; db["orders"].find({}).limit(20)`},
		{"create a collection", ptrTo(nodes["database"]), "Create Collection",
			"// Create a collection: write its name, then run this statement.\nuse shop\n\ndb.runCommand({ create: \"newCollection\" })\n", `use shop ; db.runCommand({ create: "newCollection" })`},
		{"drop a database", ptrTo(nodes["database"]), "Drop Database",
			"// Drops the database shop with all of its collections. Read it, then run it.\nuse shop\n\ndb.runCommand({ dropDatabase: 1 })\n", `use shop ; db.runCommand({ dropDatabase: 1 })`},
		{"create an index on a collection", ptrTo(nodes["collection"]), "Create Index",
			"// Create an index: write the fields (1 ascending, -1 descending), then run this statement.\nuse shop\n\ndb.getCollection(\"orders\").createIndex({ field: 1 }, { name: \"field_1\" })\n",
			`use shop ; db["orders"].createIndex({ field: 1 }, { name: "field_1" })`},
		{"create an index from the indexes", ptrTo(nodes["indexes"]), "Create Index",
			"// Create an index: write the fields (1 ascending, -1 descending), then run this statement.\nuse shop\n\ndb.getCollection(\"orders\").createIndex({ field: 1 }, { name: \"field_1\" })\n",
			`use shop ; db["orders"].createIndex({ field: 1 }, { name: "field_1" })`},
		{"drop a collection", ptrTo(nodes["collection"]), "Drop Collection",
			"// Drops the collection orders and all of its documents. Read it, then run it.\nuse shop\n\ndb.getCollection(\"orders\").drop()\n", `use shop ; db["orders"].drop()`},
		{"drop an index", ptrTo(nodes["index"]), "Drop Index",
			"// Drops the index sku_1_created_-1 of orders. Read it, then run it.\nuse shop\n\ndb.getCollection(\"orders\").dropIndex(\"sku_1_created_-1\")\n", `use shop ; db["orders"].dropIndex("sku_1_created_-1")`},
	}
	for i, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var run func()
			for _, a := range r.hub.Actions(c.ID, tt.node) {
				if a.Title == tt.title {
					run = a.Run
				}
			}
			if run == nil {
				t.Fatalf("no action %q", tt.title)
			}
			run()
			docs := r.mgAPI.UntitledDocuments()
			if len(docs) != i+1 {
				t.Fatalf("%d untitled documents, want %d", len(docs), i+1)
			}
			d := docs[i]
			if d.LanguageID != "mongodb" || d.Text != tt.text {
				t.Errorf("playground = %q in %q\nwant %q", d.Text, d.LanguageID, tt.text)
			}
			s := playground.Parse(d.Text)
			var got []string
			for _, it := range s.Items {
				if it.Err != nil {
					t.Fatalf("the playground has a mistake: %v\n%s", it.Err, d.Text)
				}
				got = append(got, renderStatement(it.Statement))
			}
			if strings.Join(got, " ; ") != tt.stmts {
				t.Errorf("statements = %q, want %q", strings.Join(got, " ; "), tt.stmts)
			}
		})
	}
}

// renderStatement writes a statement the way the tests compare them.
func renderStatement(s playground.Statement) string {
	switch x := s.(type) {
	case *playground.Use:
		return "use " + x.Database
	case *playground.Command:
		return "db.runCommand(" + playground.Format(x.Document) + ")"
	case *playground.CollectionCall:
		var args []string
		for _, a := range x.Args {
			args = append(args, playground.Format(a))
		}
		out := "db[" + playground.Quote(x.Collection) + "]." + x.Method + "(" + strings.Join(args, ", ") + ")"
		for _, c := range x.Chain {
			var cargs []string
			for _, a := range c.Args {
				cargs = append(cargs, playground.Format(a))
			}
			out += "." + c.Name + "(" + strings.Join(cargs, ", ") + ")"
		}
		return out
	}
	return "?"
}

func TestPlaygroundsForNamesThatNeedQuotes(t *testing.T) {
	cases := []struct {
		name, db, coll string
	}{
		{"spaces and quotes in the collection", "shop", `order "items"`},
		{"a collection with a dot", "shop", "system.users"},
		{"a dash in the database", "my-shop", "orders"},
		{"unicode", "καφές", "παραγγελίες"},
		{"a line break in the name of the connection", "shop", "orders\nsecond line"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			texts := map[string]string{
				"new":         playgroundText("a\nb", tt.db, tt.coll),
				"createColl":  createCollectionText(tt.db),
				"dropColl":    dropCollectionText(tt.db, tt.coll),
				"dropDB":      dropDatabaseText(tt.db),
				"createIndex": createIndexText(tt.db, tt.coll),
				"dropIndex":   dropIndexText(tt.db, tt.coll, tt.coll+"_1"),
			}
			for what, text := range texts {
				s := playground.Parse(text)
				for _, it := range s.Items {
					if it.Err != nil {
						t.Errorf("%s: %v\n%s", what, it.Err, text)
					}
				}
				if what != "new" || tt.coll == "" {
					continue
				}
				// the collection and the database come back as they were
				var db, coll string
				for _, it := range s.Items {
					switch x := it.Statement.(type) {
					case *playground.Use:
						db = x.Database
					case *playground.CollectionCall:
						coll = x.Collection
					}
				}
				if db != tt.db || coll != tt.coll {
					t.Errorf("the playground names %q and %q, want %q and %q\n%s", db, coll, tt.db, tt.coll, text)
				}
			}
		})
	}
}

func TestTheDatabaseNameWithASpaceIsWrittenAsACall(t *testing.T) {
	// a database name cannot have a space, but a name that "use" cannot read in its short form is written as a call, so that the line is
	// never read as something else
	if got := useLine("a b"); got != `use("a b")` {
		t.Errorf("useLine = %q", got)
	}
	if got := useLine("shop"); got != "use shop" {
		t.Errorf("useLine = %q", got)
	}
}

func TestTheNewPlaygroundCommand(t *testing.T) {
	r := newRig(t)
	c := r.save(dbconn.Connection{Name: "Reports", Engine: EngineID, Host: "h", Port: 27017})
	withDB := r.save(dbconn.Connection{Name: "Orders", Engine: EngineID, Host: "h", Port: 27017, Database: "reports"})
	cases := []struct {
		name string
		args any
		want string
	}{
		{"no arguments", nil, "// MongoDB playground\n\n"},
		{"a database", map[string]any{"database": "shop"}, "// MongoDB playground\nuse shop\n\n"},
		{"a connection, a database and a collection", map[string]any{"connection": c.ID, "database": "shop", "collection": "orders"},
			"// Reports\nuse shop\n\ndb.getCollection(\"orders\").find({}).limit(20)\n"},
		{"a connection with a default database", map[string]any{"connection": withDB.ID}, "// Orders\nuse reports\n\n"},
		{"a connection that does not exist", map[string]any{"connection": "nope"}, "// MongoDB playground\n\n"},
		{"arguments of the wrong kind", "shop", "// MongoDB playground\n\n"},
	}
	for i, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := r.mgAPI.Run(NewPlaygroundCommand, tt.args); err != nil {
				t.Fatal(err)
			}
			docs := r.mgAPI.UntitledDocuments()
			if len(docs) != i+1 || docs[i].Text != tt.want || docs[i].LanguageID != LanguageID {
				t.Errorf("documents = %+v, want the text %q", docs, tt.want)
			}
		})
	}
}

// TestAPlaygroundOpensInTheWindowTabOfTheMenu checks that one driver serves every window tab: the menu opened in a second tab opens
// the playground in that tab, not in the tab of the extension that registered the driver.
func TestAPlaygroundOpensInTheWindowTabOfTheMenu(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	second := fakeapi.New(t, r.ext.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "other", Folder: r.folder, ID: r.folder})
	acts := r.hub.ActionsIn(second, c.ID, nil)
	if len(acts) != 1 || acts[0].Title != "New Playground" {
		t.Fatalf("actions = %+v", acts)
	}
	acts[0].Run()
	if got := second.UntitledDocuments(); len(got) != 1 || got[0].LanguageID != LanguageID {
		t.Errorf("the second tab has %+v", got)
	}
	if got := r.mgAPI.UntitledDocuments(); len(got) != 0 {
		t.Errorf("the first tab has %+v", got)
	}
}
