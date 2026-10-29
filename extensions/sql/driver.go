package sql

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// catalogDriver is the dbconn.Driver of the SQL engines. One serves every window tab: the entries it adds to a menu are made for the tab
// the menu was opened in (ActionsIn).
type catalogDriver struct{ svc *Service }

var _ dbconn.APIDriver = (*catalogDriver)(nil)

// Engines implements dbconn.Driver.
func (*catalogDriver) Engines() []dbconn.Engine { return Engines() }

// Connect implements dbconn.Driver.
func (d *catalogDriver) Connect(ctx context.Context, c dbconn.Connection, password string) error {
	return d.svc.Open(ctx, c, password)
}

// Disconnect implements dbconn.Driver.
func (d *catalogDriver) Disconnect(c dbconn.Connection) error { return d.svc.Close(c.ID) }

// Children implements dbconn.Driver.
func (d *catalogDriver) Children(ctx context.Context, c dbconn.Connection, parent *dbconn.Node) ([]dbconn.Node, error) {
	return d.svc.Children(ctx, c, parent)
}

// Actions implements dbconn.Driver: the entries need the API of a window tab, so there are none without it.
func (*catalogDriver) Actions(dbconn.Connection, *dbconn.Node) []dbconn.Action { return nil }

// ActionsIn implements dbconn.APIDriver.
func (d *catalogDriver) ActionsIn(api extension.API, c dbconn.Connection, n *dbconn.Node) []dbconn.Action {
	return d.svc.actions(api, c, n)
}
