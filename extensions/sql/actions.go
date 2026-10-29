package sql

import (
	"context"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// selectTopRows is how many rows "Select Top 1000" reads.
const selectTopRows = 1000

// generateTimeout bounds the reading of the catalog for a generated statement.
const generateTimeout = 30 * time.Second

// openText opens an untitled SQL editor with the text, in the window tab of api. A failure is told to the user.
func openText(api extension.API, text string) {
	if _, err := api.Documents().OpenUntitled("sql", text); err != nil {
		api.Log().Warn("an SQL editor cannot be opened", "err", err)
		api.Notifications().Error("Cannot open the SQL editor: " + err.Error())
	}
}

// actions are the entries the extension adds to the context menu of the connection (n is nil) and of its nodes, in the window tab of api.
func (s *Service) actions(api extension.API, c dbconn.Connection, n *dbconn.Node) []dbconn.Action {
	d, ok := dialect.ByID(c.Engine)
	if !ok {
		return nil
	}
	newQuery := dbconn.Action{Title: "New Query", Group: "1_query", Run: func() {
		openText(api, "") // TODO(step 19.3): bind the editor to the connection and database of the node
	}}
	if n == nil {
		return []dbconn.Action{newQuery}
	}
	data, ok := n.Data.(nodeData)
	if !ok {
		return nil
	}
	switch n.Kind {
	case NodeDatabase, NodeSchema:
		return []dbconn.Action{newQuery}
	case NodeTable, NodeView:
		return s.tableActions(api, c, d, data, newQuery)
	case NodeRoutine:
		return []dbconn.Action{
			newQuery,
			{Title: "Generate CREATE Statement", Group: "2_generate", Run: func() {
				s.generate(api, c.ID, "the CREATE statement", func(ctx context.Context) (string, error) { return s.Create(ctx, c.ID, data.Kind, data.Ref) })
			}},
			{Title: "Drop " + titleOf(data.Kind) + " (as text)", Group: "3_drop", Run: func() {
				openText(api, dialect.GenerateDrop(d, data.Kind, data.Ref))
			}},
		}
	}
	return nil
}

// tableActions are the entries of a table or a view.
func (s *Service) tableActions(api extension.API, c dbconn.Connection, d dialect.Dialect, data nodeData, newQuery dbconn.Action) []dbconn.Action {
	table := func(title string, gen func(dialect.Dialect, dialect.Table) string) dbconn.Action {
		return dbconn.Action{Title: title, Group: "2_generate", Run: func() {
			s.generate(api, c.ID, "the columns of "+data.Ref.Name, func(ctx context.Context) (string, error) {
				t, err := s.LoadTable(ctx, c.ID, data.Kind, data.Ref)
				if err != nil {
					return "", err
				}
				return gen(d, t), nil
			})
		}}
	}
	actions := []dbconn.Action{
		newQuery,
		{Title: "Select Top 1000", Group: "1_query", Run: func() {
			// TODO(step 19.3): run the statement in the editor, bound to the connection and database
			openText(api, d.SelectTop(data.Ref, selectTopRows)+"\n")
		}},
		table("Generate SELECT Statement", dialect.GenerateSelect),
	}
	if data.Kind == dialect.KindTable {
		actions = append(actions,
			table("Generate INSERT Statement", dialect.GenerateInsert),
			table("Generate UPDATE Statement", dialect.GenerateUpdate),
			table("Generate DELETE Statement", dialect.GenerateDelete))
	}
	actions = append(actions,
		dbconn.Action{Title: "Generate CREATE Statement", Group: "2_generate", Run: func() {
			s.generate(api, c.ID, "the CREATE statement", func(ctx context.Context) (string, error) { return s.Create(ctx, c.ID, data.Kind, data.Ref) })
		}},
		dbconn.Action{Title: "Drop " + titleOf(data.Kind) + " (as text)", Group: "3_drop", Run: func() {
			openText(api, dialect.GenerateDrop(d, data.Kind, data.Ref))
		}})
	return actions
}

// titleOf is the kind of an object as a word of a menu entry: Table, Materialized View.
func titleOf(k dialect.Kind) string {
	switch k {
	case dialect.KindView:
		return "View"
	case dialect.KindMaterializedView:
		return "Materialized View"
	case dialect.KindFunction:
		return "Function"
	case dialect.KindProcedure:
		return "Procedure"
	}
	return "Table"
}

// generate reads what a statement needs off the UI thread, then opens it in an editor of the window tab.
func (s *Service) generate(api extension.API, connID, what string, work func(ctx context.Context) (string, error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), generateTimeout)
		defer cancel()
		text, err := work(ctx)
		api.Post(func() {
			if err != nil {
				api.Log().Warn("a statement cannot be generated", "connection", connID, "err", err)
				api.Notifications().Error("Cannot read " + what + ": " + err.Error())
				return
			}
			openText(api, text)
		})
	}()
}
