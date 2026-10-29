package redis

import (
	"context"
	"log/slog"
	"strconv"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// EngineID is the id of the engine in the connection file.
const EngineID = "redis"

// Engine is the description of Redis for the connection form and the Databases view.
func Engine() dbconn.Engine {
	return dbconn.Engine{
		ID: EngineID, Name: "Redis", Icon: "database", DefaultPort: DefaultPort, URLSchemes: []string{"redis", "rediss"}, NoDatabase: true,
		Fields: []dbconn.Field{
			{Key: OptDB, Label: "Database number", Kind: dbconn.FieldNumber, Placeholder: "0"},
			{Key: OptSeparator, Label: "Key separator (the folders of the tree; \"none\" for a flat list)", Kind: dbconn.FieldText, Default: DefaultSeparator, Placeholder: DefaultSeparator},
			{Key: OptMode, Label: "Mode", Kind: dbconn.FieldChoice, Default: ModeStandalone, Choices: []dbconn.Choice{
				{Value: ModeStandalone, Label: "Standalone"}, {Value: ModeSentinel, Label: "Sentinel"}, {Value: ModeCluster, Label: "Cluster"}}},
			{Key: OptMaster, Label: "Sentinel master name", Kind: dbconn.FieldText, Placeholder: "mymaster"},
			{Key: OptNodes, Label: "More Sentinels or cluster nodes (host:port, separated by commas)", Kind: dbconn.FieldText},
		},
	}
}

// session is a connection that is open: the server, and what the explorer holds of its databases.
type session struct {
	conn    dbconn.Connection
	server  Server
	sep     string
	dbIndex int // the database the connection selects

	mu  sync.Mutex
	dbs map[int]*dbState
}

// state returns the explorer's state for a database of the session.
func (s *session) state(index int) (*dbState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.dbs[index]; ok {
		return st, nil
	}
	db, err := s.server.DB(index)
	if err != nil {
		return nil, err
	}
	st := newDBState(index, db, s.sep)
	s.dbs[index] = st
	return st, nil
}

// states lists the states made so far.
func (s *session) states() []*dbState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*dbState, 0, len(s.dbs))
	for _, st := range s.dbs {
		out = append(out, st)
	}
	return out
}

// Driver is the dbconn.Driver of Redis: it opens the connections, lists the databases and keys below them, and opens a key in the
// key editor. It also implements dbconn.Opener and dbconn.Refresher.
type Driver struct {
	x      *Extension
	dialer Dialer

	mu       sync.Mutex
	sessions map[string]*session
}

var (
	_ dbconn.Driver    = (*Driver)(nil)
	_ dbconn.Opener    = (*Driver)(nil)
	_ dbconn.Refresher = (*Driver)(nil)
)

func newDriver(x *Extension, dialer Dialer) *Driver {
	return &Driver{x: x, dialer: dialer, sessions: map[string]*session{}}
}

// Engines implements dbconn.Driver.
func (d *Driver) Engines() []dbconn.Engine { return []dbconn.Engine{Engine()} }

// Connect implements dbconn.Driver.
func (d *Driver) Connect(ctx context.Context, c dbconn.Connection, password string) error {
	spec, err := BuildSpec(ctx, c, password)
	if err != nil {
		return err
	}
	srv, err := d.dialer.Dial(ctx, spec)
	if err != nil {
		return err
	}
	if c.ReadOnly {
		srv = ReadOnly(srv)
	}
	s := &session{conn: c, server: srv, sep: separatorOf(c), dbIndex: spec.DB, dbs: map[int]*dbState{}}
	d.mu.Lock()
	old := d.sessions[c.ID]
	d.sessions[c.ID] = s
	d.mu.Unlock()
	if old != nil {
		if err := old.server.Close(); err != nil {
			slog.Warn("a connection that was replaced did not close", "connection", c.Name, "err", err)
		}
	}
	d.x.opened(c)
	return nil
}

// Disconnect implements dbconn.Driver.
func (d *Driver) Disconnect(c dbconn.Connection) error {
	d.mu.Lock()
	s := d.sessions[c.ID]
	delete(d.sessions, c.ID)
	d.mu.Unlock()
	if s == nil {
		return nil
	}
	d.x.closed(c)
	return s.server.Close()
}

// session returns the open session of a connection.
func (d *Driver) session(connID string) (*session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sessions[connID]
	if s == nil {
		return nil, errors.New("RedisNotConnected", "the connection is not open: connect it in the Databases view", "connection", connID)
	}
	return s, nil
}

// Connected tells whether the connection has an open session.
func (d *Driver) Connected(connID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[connID] != nil
}

// database returns the database of a connection that is open, and the session.
func (d *Driver) database(connID string, index int) (DB, *session, error) {
	s, err := d.session(connID)
	if err != nil {
		return nil, nil, err
	}
	st, err := s.state(index)
	if err != nil {
		return nil, nil, err
	}
	return st.db, s, nil
}

// Refresh implements dbconn.Refresher: the keys that were scanned are forgotten, to be scanned again when a row is opened.
func (d *Driver) Refresh(c dbconn.Connection) {
	s, err := d.session(c.ID)
	if err != nil {
		return
	}
	for _, st := range s.states() {
		st.refresh()
	}
}

// Children implements dbconn.Driver.
func (d *Driver) Children(ctx context.Context, c dbconn.Connection, parent *dbconn.Node) ([]dbconn.Node, error) {
	s, err := d.session(c.ID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return d.databases(ctx, s)
	}
	data, ok := parent.Data.(nodeData)
	if !ok {
		return nil, nil
	}
	switch data.Kind {
	case kindDB, kindFolder:
		st, err := s.state(data.DB)
		if err != nil {
			return nil, err
		}
		if err := st.ensureLoaded(ctx, d.x.scanCount()); err != nil {
			return nil, err
		}
		return keyNodes(st, data), nil
	}
	return nil, nil
}

// databases lists the databases of the server: those with keys, and the one the connection selects.
func (d *Driver) databases(ctx context.Context, s *session) ([]dbconn.Node, error) {
	infos, err := s.server.Databases(ctx)
	if err != nil {
		return nil, err
	}
	want := s.dbIndex
	found := false
	for _, in := range infos {
		if in.Index == want {
			found = true
		}
	}
	if !found {
		infos = append(infos, DBInfo{Index: want})
		for i := len(infos) - 1; i > 0 && infos[i].Index < infos[i-1].Index; i-- {
			infos[i], infos[i-1] = infos[i-1], infos[i]
		}
	}
	nodes := make([]dbconn.Node, 0, len(infos))
	for _, in := range infos {
		desc := plural(in.Keys, "key", "keys")
		filter := ""
		if st, err := s.state(in.Index); err == nil {
			filter = st.filterText()
		}
		if filter != "" {
			desc += ", filter " + filter
		}
		tip := "Database " + strconv.Itoa(in.Index) + ": " + plural(in.Keys, "key", "keys")
		if in.Expires > 0 {
			tip += ", " + strconv.FormatInt(in.Expires, 10) + " with a TTL"
		}
		nodes = append(nodes, dbconn.Node{
			ID: "db" + strconv.Itoa(in.Index), Label: "db" + strconv.Itoa(in.Index), Description: desc, Icon: "database", Tooltip: tip,
			Expandable: true, Kind: kindDB, Data: nodeData{Kind: kindDB, DB: in.Index},
		})
	}
	return nodes, nil
}

// keyNodes makes the rows below a database or a folder from the tree.
func keyNodes(st *dbState, at nodeData) []dbconn.Node {
	path := ""
	if at.Kind == kindFolder {
		path = at.Path
	}
	snap := st.level(path)
	nodes := make([]dbconn.Node, 0, len(snap.entries)+1)
	for _, e := range snap.entries {
		label := e.Name
		if label == "" {
			label = "(empty)"
		}
		if e.Folder {
			nodes = append(nodes, dbconn.Node{
				ID: "d:" + e.Path, Label: label, Description: strconv.Itoa(e.Count), Icon: "folder", Expandable: true, Kind: kindFolder,
				Tooltip: plural(int64(e.Count), "key", "keys") + " below " + e.Path,
				Data:    nodeData{Kind: kindFolder, DB: st.index, Path: e.Path},
			})
			continue
		}
		l := e.Leaf
		nodes = append(nodes, dbconn.Node{
			ID: "k:" + l.Key, Label: label, Icon: IconOf(l.Type), Kind: kindKey,
			Tooltip: l.Key + "\n" + LabelOf(l.Type) + ", TTL " + FormatTTL(l.TTLMillis),
			Data:    nodeData{Kind: kindKey, DB: st.index, Key: l.Key, Type: l.Type, TTLMillis: l.TTLMillis},
		})
	}
	if path == "" {
		switch {
		case snap.more:
			desc := Commas(int64(snap.loaded)) + " keys loaded"
			if snap.size > 0 && snap.filter == "" {
				desc = Commas(int64(snap.loaded)) + " of " + Commas(snap.size)
			}
			nodes = append(nodes, dbconn.Node{ID: "more", Label: "Load more keys...", Description: desc, Icon: "ellipsis", Kind: kindMore,
				Tooltip: "Scan the next page of keys", Data: nodeData{Kind: kindMore, DB: st.index}})
		case len(nodes) == 0 && snap.filter != "":
			nodes = append(nodes, dbconn.Node{ID: "empty", Label: "No key matches " + snap.filter, Icon: "info", Kind: kindEmpty, Data: nodeData{Kind: kindEmpty, DB: st.index}})
		case len(nodes) == 0:
			nodes = append(nodes, dbconn.Node{ID: "empty", Label: "No keys", Icon: "info", Kind: kindEmpty, Data: nodeData{Kind: kindEmpty, DB: st.index}})
		}
	}
	return nodes
}

// Open implements dbconn.Opener: a key opens in the key editor and "Load more keys..." scans the next page.
func (d *Driver) Open(c dbconn.Connection, n dbconn.Node) {
	data, ok := n.Data.(nodeData)
	if !ok {
		return
	}
	switch data.Kind {
	case kindKey:
		d.x.openKey(KeyRef{Conn: c.ID, DB: data.DB, Key: data.Key})
	case kindMore:
		d.x.loadMore(c.ID, data.DB)
	}
}

// Actions implements dbconn.Driver.
func (d *Driver) Actions(c dbconn.Connection, n *dbconn.Node) []dbconn.Action {
	if n == nil {
		return d.connectionActions(c)
	}
	data, ok := n.Data.(nodeData)
	if !ok {
		return nil
	}
	x := d.x
	ref := KeyRef{Conn: c.ID, DB: data.DB, Key: data.Key}
	write := !c.ReadOnly
	var out []dbconn.Action
	add := func(title, group string, run func()) {
		out = append(out, dbconn.Action{Title: title, Group: group, Run: run})
	}
	switch data.Kind {
	case kindDB:
		add("Open Console", "0", func() { x.openConsoleOn(c.ID, data.DB) })
		if write {
			add("New Key...", "1", func() { x.newKey(c.ID, data.DB, "") })
		}
		add("Filter Keys...", "2", func() { x.askFilter(c.ID, data.DB) })
		if x.hasFilter(c.ID, data.DB) {
			add("Clear Filter", "2", func() { x.setFilter(c.ID, data.DB, "") })
		}
		add("Refresh Keys", "3", func() { x.refreshDB(c.ID, data.DB) })
	case kindFolder:
		if write {
			add("New Key Here...", "1", func() { x.newKey(c.ID, data.DB, data.Path) })
		}
		add("Copy Prefix", "2", func() { x.copyText(data.Path) })
	case kindKey:
		add("Open", "1", func() { x.openKey(ref) })
		add("Copy Key", "1", func() { x.copyText(data.Key) })
		if write {
			add("Rename...", "2", func() { x.renameKey(ref) })
			add("Set TTL...", "2", func() { x.setTTL(ref) })
			if data.TTLMillis >= 0 {
				add("Persist", "2", func() { x.persistKey(ref) })
			}
			add("Delete...", "3", func() { x.deleteKey(ref) })
		}
	case kindMore:
		add("Load More", "1", func() { x.loadMore(c.ID, data.DB) })
	}
	return out
}

// connectionActions are the entries of the menu of a connection that is open: the console, Pub/Sub, Monitor and the server information
// (SPEC §10.23).
func (d *Driver) connectionActions(c dbconn.Connection) []dbconn.Action {
	if !d.Connected(c.ID) {
		return nil
	}
	x := d.x
	return []dbconn.Action{
		{Title: "Open Console", Group: "0", Run: func() { x.openConsoleOn(c.ID, -1) }},
		{Title: "Open Pub/Sub", Group: "0", Run: func() { x.openPanel(PubSubViewID, c.ID) }},
		{Title: "Open Monitor", Group: "0", Run: func() { x.openPanel(MonitorViewID, c.ID) }},
		{Title: "Server Information", Group: "0", Run: func() { x.openServerInfo(c.ID) }},
	}
}
