package sql

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// viewWidget builds the widget of the Databases view as the box does.
func viewWidget(r *rig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.dbAPI.View(dbconn.ViewID).Content.Build(bc) })
}

func TestGoldenTheExplorerOfASQLiteFile(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			// the file is under the test's home folder and written ~/library.db, so that no path of the machine is on the picture
			makeFixtureAt(t, filepath.Join(r.home, "library.db"))
			c := sqliteConn("~/library.db")
			c.Name, c.Color = "Library", "#4a90d9"
			c = r.save(c)
			r.connect(c)
			r.expand("Library", "main", "Tables", "books", "Columns")
			r.expand("Library", "main", "Tables", "books", "Indexes")
			r.expand("Library", "main", "Tables", "books", "Foreign Keys")
			r.expand("Library", "main", "Tables", "tags", "Columns")
			r.expand("Library", "main", "Views")
			r.dbAPI.Pump()
			ts := uitest.Pump(t, th, gui.Size{Width: 360, Height: 560}, viewWidget(r))
			uitest.MatchesGolden(ts, "explorer_sqlite_"+th.Name)
		})
	}
}

func TestGoldenTheExplorerWhileItFilters(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			makeFixtureAt(t, filepath.Join(r.home, "library.db"))
			c := sqliteConn("~/library.db")
			c.Name = "Library"
			c = r.save(c)
			r.connect(c)
			r.expand("Library", "main", "Tables", "authors", "Columns")
			r.expand("Library", "main", "Tables", "books", "Columns")
			r.expand("Library", "main", "Views")
			ts := uitest.Pump(t, th, gui.Size{Width: 360, Height: 220}, viewWidget(r))
			// typing in the tree opens its find box, which filters
			ts.Tap(gluttertest.ByTextContaining("authors"))
			for _, ch := range "auth" {
				key := services.LogicalKeyboardKey(ch)
				ts.SendKeyDownEvent(key, gluttertest.KeyOpts{Character: string(ch)})
				ts.SendKeyUpEvent(key)
			}
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "explorer_filter_"+th.Name)
		})
	}
}

// cannedDriver is a driver that makes the nodes of the explorer from rows a PostgreSQL server could have given, with no server: it
// shows how the real node-making code looks for the objects SQLite does not have (routines, materialized views, schemas).
type cannedDriver struct{ d dialect.Dialect }

func (cannedDriver) Engines() []dbconn.Engine                                 { return Engines()[:1] }
func (cannedDriver) Connect(context.Context, dbconn.Connection, string) error { return nil }
func (cannedDriver) Disconnect(dbconn.Connection) error                       { return nil }
func (cannedDriver) Actions(dbconn.Connection, *dbconn.Node) []dbconn.Action  { return nil }

func canned(rows ...[]string) []dialect.Row {
	out := make([]dialect.Row, len(rows))
	for i, r := range rows {
		out[i] = make(dialect.Row, len(r))
		for j, v := range r {
			if v != "\x00" {
				out[i][j].String, out[i][j].Valid = v, true
			}
		}
	}
	return out
}

func (c cannedDriver) Children(_ context.Context, _ dbconn.Connection, parent *dbconn.Node) ([]dbconn.Node, error) {
	public := dialect.Ref{Database: "shop", Schema: "public"}
	if parent == nil {
		return []dbconn.Node{
			{ID: "db:shop", Label: "shop", Icon: "database", Expandable: true, Kind: NodeDatabase, Data: nodeData{Ref: dialect.Ref{Database: "shop"}}},
			{ID: "db:postgres", Label: "postgres", Icon: "database", Expandable: true, Kind: NodeDatabase, Data: nodeData{Ref: dialect.Ref{Database: "postgres"}}},
		}, nil
	}
	data, _ := parent.Data.(nodeData)
	switch parent.Kind {
	case NodeDatabase:
		return []dbconn.Node{
			{ID: "schema:public", Label: "public", Icon: "symbol-namespace", Expandable: true, Kind: NodeSchema, Data: nodeData{Ref: public}},
			{ID: "schema:audit", Label: "audit", Icon: "symbol-namespace", Expandable: true, Kind: NodeSchema, Data: nodeData{Ref: dialect.Ref{Database: "shop", Schema: "audit"}}},
		}, nil
	case NodeSchema:
		return folders(c.d, data.Ref), nil
	case NodeFolder:
		switch data.Folder {
		case folderTables:
			return objectNodes(dialect.DecodeTables(canned([]string{"customers", "Who buys"}, []string{"orders", "\x00"}), public), NodeTable), nil
		case folderViews:
			return objectNodes(dialect.DecodeViews(canned([]string{"open_orders", "\x00", "view"}, []string{"sales_by_day", "Refreshed nightly", "materialized view"}), public), NodeView), nil
		case folderFunctions, folderProcedures:
			all := dialect.DecodeRoutines(canned(
				[]string{"order_total", "function", "order_id integer", "numeric", "16401"},
				[]string{"touch", "function", "", "trigger", "16402"},
				[]string{"archive_orders", "procedure", "before date, dry_run boolean", "\x00", "16403"},
			), public)
			var objs []dialect.Object
			for _, o := range all {
				if (data.Folder == folderFunctions) == (o.Kind == dialect.KindFunction) {
					objs = append(objs, o)
				}
			}
			return objectNodes(objs, NodeRoutine), nil
		case folderColumns:
			cols := dialect.DecodeColumns(canned(
				[]string{"id", "bigint", "0", "nextval('orders_id_seq'::regclass)", "1"},
				[]string{"customer_id", "bigint", "0", "\x00", "0"},
				[]string{"placed_at", "timestamp with time zone", "0", "now()", "0"},
				[]string{"note", "text", "1", "\x00", "0"},
			))
			dialect.MarkForeignKeys(cols, []dialect.ForeignKey{{Columns: []string{"customer_id"}}})
			return columnNodes(cols), nil
		case folderIndexes:
			return indexNodes(dialect.DecodeIndexes(canned(
				[]string{"orders_pkey", "id", "1", "1", "1"},
				[]string{"orders_customer_placed", "customer_id", "0", "0", "1"},
				[]string{"orders_customer_placed", "placed_at", "0", "0", "2"},
			))), nil
		case folderForeignKeys:
			return foreignKeyNodes(c.d, dialect.DecodeForeignKeys(canned([]string{"orders_customer_fk", "customer_id", "public", "customers", "id", "1"}))), nil
		}
	case NodeTable, NodeView:
		return tableFolders(data), nil
	}
	return nil, nil
}

func TestGoldenTheExplorerOfAPostgreSQLShapedServer(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			// replace the SQL driver with the canned one: it serves the same engines
			r.stop()
			waitUntil(t, "the SQL driver to go", func() bool { return len(r.db.Engines()) == 0 })
			t.Cleanup(r.db.RegisterDriver(cannedDriver{d: dialect.Postgres{}}))
			c := r.save(dbconn.Connection{Name: "Production", Engine: "postgres", Scope: dbconn.ScopeUser, Host: "db.example.com", Port: 5432, User: "me", ReadOnly: true, Color: "#d9534f", PasswordEnv: "${env:PW}"})
			t.Setenv("PW", "x")
			r.connect(c)
			r.expand("Production", "shop", "public", "Tables", "orders", "Columns")
			r.expand("Production", "shop", "public", "Tables", "orders", "Indexes")
			r.expand("Production", "shop", "public", "Tables", "orders", "Foreign Keys")
			r.expand("Production", "shop", "public", "Views")
			r.expand("Production", "shop", "public", "Functions")
			r.expand("Production", "shop", "public", "Procedures")
			ts := uitest.Pump(t, th, gui.Size{Width: 520, Height: 640}, viewWidget(r))
			uitest.MatchesGolden(ts, "explorer_postgres_"+th.Name)
		})
	}
}
