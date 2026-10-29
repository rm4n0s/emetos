package dbconn

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"sort"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Status is the state of a connection in the view.
type Status int

// The states of a connection.
const (
	// Disconnected: nothing is open.
	Disconnected Status = iota
	// Connecting: the driver is opening the connection.
	Connecting
	// Connected: the connection is open and its tree can be listed.
	Connected
	// Failed: the last attempt failed; State.Err says why.
	Failed
)

// String names the status.
func (s Status) String() string {
	return [...]string{"disconnected", "connecting", "connected", "failed"}[s]
}

// State is the state of one connection.
type State struct {
	Status Status
	// Err is why the last attempt failed, for the user; empty unless Status is Failed.
	Err string
}

// HubOptions configure a Hub.
type HubOptions struct {
	// StorePath is the connection file; $XDG_STATE_HOME/emetos/db-connections.json when empty.
	StorePath string
	// Keyring keeps the saved passwords; the desktop's Secret Service when nil. NoKeyring turns the keyring off.
	Keyring Keyring
	// NoKeyring makes every password stay in memory (a desktop without a keyring, and tests).
	NoKeyring bool
	// LookupEnv reads the environment; os.LookupEnv when nil.
	LookupEnv LookupEnv
}

// Hub is what the three database extensions share, one for the whole of Emetos (extensions.All makes it): the connections and
// their file, the passwords, the engines the extensions registered and the state of each connection. The extension "Databases" of
// every window tab shows it. It is safe for use from several goroutines; its Subscribe callbacks run on the goroutine that
// changed something, so a subscriber hands the news to its UI thread with Post.
type Hub struct {
	storePath string
	storeOnce sync.Once
	storeVal  *Store
	pw        *Passwords
	env       LookupEnv

	mu      sync.Mutex
	drivers []driverEntry
	states  map[string]State
	subs    map[int]func()
	nextSub int
	nextDrv int

	// reloadSubs are those who asked to hear of Reload (hooks.go).
	reloadSubs reloads
}

// driverEntry is a registered driver and the token that removes it.
type driverEntry struct {
	token  int
	driver Driver
}

// NewHub makes a hub. It reads nothing and talks to nothing: the connection file is read when the hub first needs it, and a
// connection file that cannot be read leaves the hub with no connections (StoreErr says why).
func NewHub(opts HubOptions) *Hub {
	env := opts.LookupEnv
	if env == nil {
		env = os.LookupEnv
	}
	kr := opts.Keyring
	if kr == nil && !opts.NoKeyring {
		kr = NewSecretService()
	}
	return &Hub{storePath: opts.StorePath, pw: NewPasswords(kr, env), env: env, states: map[string]State{}, subs: map[int]func(){}}
}

// Store is the connection file. It is found and read when it is first needed (the first time an extension shows its view), not when
// the hub is made: the hub is made when Emetos starts, and a run that never looks at a database never reads the file.
func (h *Hub) Store() *Store {
	h.storeOnce.Do(func() {
		path, pathErr := h.storePath, error(nil)
		if path == "" {
			path, pathErr = StorePath()
		}
		if pathErr != nil {
			h.storeVal = &Store{loadErr: pathErr}
			return
		}
		h.storeVal = NewStore(path)
	})
	return h.storeVal
}

// Passwords decides where passwords come from and go.
func (h *Hub) Passwords() *Passwords { return h.pw }

// StoreErr is why the connection file could not be read, or nil.
func (h *Hub) StoreErr() error { return h.Store().Err() }

// LookupEnv is the function that reads the environment for ${env:NAME}.
func (h *Hub) LookupEnv() LookupEnv { return h.env }

// Subscribe calls fn after the connections, the engines or a state change, and returns the function that stops the calls. fn
// runs on the goroutine that made the change and must not block.
func (h *Hub) Subscribe(fn func()) (cancel func()) {
	h.mu.Lock()
	id := h.nextSub
	h.nextSub++
	h.subs[id] = fn
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
	}
}

// changed tells the subscribers.
func (h *Hub) changed() {
	h.mu.Lock()
	fns := make([]func(), 0, len(h.subs))
	for _, fn := range h.subs {
		fns = append(fns, fn)
	}
	h.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// RegisterDriver adds the driver of a database extension; its engines show in the new-connection list, and its connections can be
// connected. It returns the function that removes the driver (the extension was deactivated), which closes the connections that
// were open through it, except those of an engine that another registration still serves (the window tabs of Emetos each register
// the one driver of an extension). Two drivers may not serve one engine: the first one registered keeps it.
func (h *Hub) RegisterDriver(d Driver) (unregister func()) {
	h.mu.Lock()
	token := h.nextDrv
	h.nextDrv++
	h.drivers = append(h.drivers, driverEntry{token: token, driver: d})
	h.mu.Unlock()
	h.changed()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			// the registration goes first, in the same locked section that looks at the others: two tabs that close at once
			// would otherwise each see the other still there, and neither would close the connections
			h.drivers = slices.DeleteFunc(h.drivers, func(e driverEntry) bool { return e.token == token })
			// an engine another registration still serves keeps its connections: the window tabs of Emetos each register the one
			// driver of an extension, and closing one tab must not close what the others use
			engines := slices.DeleteFunc(slices.Clone(d.Engines()), func(e Engine) bool {
				return slices.ContainsFunc(h.drivers, func(o driverEntry) bool {
					return o.token != token && slices.ContainsFunc(o.driver.Engines(), func(oe Engine) bool { return oe.ID == e.ID })
				})
			})
			var open []string
			for id, st := range h.states {
				if st.Status == Connected {
					open = append(open, id)
				}
			}
			h.mu.Unlock()
			for _, id := range open {
				c, ok := h.Store().Get(id)
				if !ok || !slices.ContainsFunc(engines, func(e Engine) bool { return e.ID == c.Engine }) {
					continue
				}
				if err := d.Disconnect(c); err != nil { // the extension is going away; there is nobody to tell but the log
					slog.Warn("a connection did not close when its extension went away", "connection", c.Name, "err", err)
				}
				h.setState(id, State{})
			}
			h.changed()
		})
	}
}

// Engines lists the engines of the registered drivers by name.
func (h *Hub) Engines() []Engine {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Engine
	for _, e := range h.drivers {
		for _, eng := range e.driver.Engines() {
			if !slices.ContainsFunc(out, func(x Engine) bool { return x.ID == eng.ID }) {
				out = append(out, eng)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Engine returns the engine with the ID, and the driver that serves it.
func (h *Hub) Engine(id string) (Engine, Driver, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.drivers {
		for _, eng := range e.driver.Engines() {
			if eng.ID == id {
				return eng, e.driver, true
			}
		}
	}
	return Engine{}, nil, false
}

// State returns the state of a connection.
func (h *Hub) State(id string) State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.states[id]
}

// setState records a state and tells the subscribers.
func (h *Hub) setState(id string, s State) {
	h.mu.Lock()
	if s == (State{}) {
		delete(h.states, id)
	} else {
		h.states[id] = s
	}
	h.mu.Unlock()
	h.changed()
}

// Fail records that a connection could not be opened, with the reason for the user (a mistake found before the driver was asked).
func (h *Hub) Fail(id string, err error) {
	h.setState(id, State{Status: Failed, Err: err.Error()})
}

// PasswordChange says what to do with the password when a connection is saved.
type PasswordChange struct {
	// Set says that Value is the new password; false keeps what the connection has.
	Set bool
	// Value is the new password, possibly empty.
	Value string
}

// SaveResult tells what Save did besides writing the file.
type SaveResult struct {
	// Saved says where a new password went; Saved.In is SourceNone when the password was not changed.
	Saved Saved
	// KeyringErr is why a secret could not be removed from the keyring (a connection that stopped saving its password).
	KeyringErr error
}

// Save validates a connection against its engine, writes it to the file and handles its password: a new password goes to the
// keyring when the connection asks for it and to memory otherwise; a connection that stops saving its password has the keyring
// entry removed. It runs the keyring calls, so it is called from a goroutine. The errors are those of Engine.Validate and
// Store.Save, and ConnectionEngineUnknown for an engine no driver serves.
func (h *Hub) Save(ctx context.Context, c Connection, pw PasswordChange) (Connection, SaveResult, error) {
	eng, _, ok := h.Engine(c.Engine)
	if !ok {
		return Connection{}, SaveResult{}, errors.New("ConnectionEngineUnknown", "no extension serves this engine", "engine", c.Engine)
	}
	if eng.NoPassword {
		c.SavePassword, c.PasswordEnv = false, ""
	}
	if err := eng.Validate(c); err != nil {
		return Connection{}, SaveResult{}, err
	}
	before, existed := h.Store().Get(c.ID)
	kept, err := h.Store().Save(c)
	if err != nil {
		return Connection{}, SaveResult{}, err
	}
	var res SaveResult
	switch {
	case pw.Set && !eng.NoPassword:
		res.Saved = h.pw.Save(ctx, kept, pw.Value)
	case existed && before.SavePassword && !kept.SavePassword:
		res.KeyringErr = h.pw.Delete(ctx, kept.ID)
	}
	h.changed()
	return kept, res, nil
}

// Delete closes a connection, removes its password from memory and from the keyring, and removes it from the file. A keyring that
// fails is reported in err of the result and does not stop the delete.
func (h *Hub) Delete(ctx context.Context, id string) (keyringErr error, err error) {
	c, ok := h.Store().Get(id)
	if !ok {
		return nil, errors.New("ConnectionNotFound", "there is no connection with this ID", "id", id)
	}
	if h.State(id).Status == Connected {
		if err := h.Disconnect(id); err != nil {
			slog.Warn("a connection did not close cleanly before it was deleted", "connection", c.Name, "err", err)
		}
	}
	keyringErr = h.pw.Delete(ctx, id)
	if _, err := h.Store().Delete(c.ID); err != nil {
		return keyringErr, err
	}
	h.setState(id, State{})
	return keyringErr, nil
}

// Prepared is what Prepare found out about the password of a connection.
type Prepared struct {
	Connection Connection
	Driver     Driver
	Engine     Engine
	// Password is the password found; Source says where, SourceNone when the user has to be asked.
	Password Resolved
	// Ask says that the user has to be asked for the password.
	Ask bool
}

// Prepare gets a connection ready to open: its ${env:} references are expanded and its password looked up (the environment, the
// memory, the keyring). It runs the keyring, so it is called from a goroutine. The errors are ConnectionNotFound,
// ConnectionEngineUnknown, ConnectionEnvUnset and those of the keyring; a keyring that fails is not fatal for the caller, which
// can ask the user instead (use Prepare's result err to tell it).
func (h *Hub) Prepare(ctx context.Context, id string) (Prepared, error) {
	c, ok := h.Store().Get(id)
	if !ok {
		return Prepared{}, errors.New("ConnectionNotFound", "there is no connection with this ID", "id", id)
	}
	eng, drv, ok := h.Engine(c.Engine)
	if !ok {
		return Prepared{}, errors.New("ConnectionEngineUnknown", "no extension serves this engine", "engine", c.Engine)
	}
	expanded, err := c.Expanded(h.env)
	if err != nil {
		return Prepared{}, err
	}
	p := Prepared{Connection: expanded, Driver: drv, Engine: eng}
	if eng.NoPassword {
		return p, nil
	}
	res, err := h.pw.Resolve(ctx, c)
	p.Password = res
	if err != nil {
		p.Ask = c.PasswordEnv == "" // a keyring that fails can be answered by the user; a variable that is not set cannot
		return p, err
	}
	p.Ask = res.Source == SourceNone
	return p, nil
}

// Connect opens the connection with the driver, with the password the caller found (or asked for). The state goes to Connecting
// and then to Connected or Failed. A password the user typed is kept in memory until Disconnect. It blocks until the driver
// answers, so it is called from a goroutine. The errors are those of Prepare and of the driver.
func (h *Hub) Connect(ctx context.Context, p Prepared, typed bool) error {
	id := p.Connection.ID
	h.setState(id, State{Status: Connecting})
	if err := p.Driver.Connect(ctx, p.Connection, p.Password.Password); err != nil {
		h.setState(id, State{Status: Failed, Err: err.Error()})
		return err
	}
	if typed {
		h.pw.Remember(id, p.Password.Password)
	}
	h.setState(id, State{Status: Connected})
	return nil
}

// Test opens the connection and closes it again, to tell the user that the settings work; the state of a saved connection does
// not change. It blocks, so it is called from a goroutine.
func (h *Hub) Test(ctx context.Context, c Connection, password string) error {
	eng, drv, ok := h.Engine(c.Engine)
	if !ok {
		return errors.New("ConnectionEngineUnknown", "no extension serves this engine", "engine", c.Engine)
	}
	if err := eng.Validate(c); err != nil {
		return err
	}
	expanded, err := c.Expanded(h.env)
	if err != nil {
		return err
	}
	if err := drv.Connect(ctx, expanded, password); err != nil {
		return err
	}
	return drv.Disconnect(expanded)
}

// Disconnect closes a connection. The password the user typed for it is forgotten (one that is saved stays in the keyring). A
// failure of the driver is returned, and the connection counts as closed in any case.
func (h *Hub) Disconnect(id string) error {
	c, ok := h.Store().Get(id)
	if !ok {
		return errors.New("ConnectionNotFound", "there is no connection with this ID", "id", id)
	}
	_, drv, found := h.Engine(c.Engine)
	var err error
	if found {
		err = drv.Disconnect(c)
	}
	h.pw.Forget(id)
	h.setState(id, State{})
	return err
}

// Children lists the nodes below parent of a connected connection (a nil parent: right below the connection).
func (h *Hub) Children(ctx context.Context, id string, parent *Node) ([]Node, error) {
	c, ok := h.Store().Get(id)
	if !ok {
		return nil, errors.New("ConnectionNotFound", "there is no connection with this ID", "id", id)
	}
	_, drv, ok := h.Engine(c.Engine)
	if !ok {
		return nil, errors.New("ConnectionEngineUnknown", "no extension serves this engine", "engine", c.Engine)
	}
	expanded, err := c.Expanded(h.env)
	if err != nil {
		return nil, err
	}
	return drv.Children(ctx, expanded, parent)
}

// Actions are the context menu entries of the extension that serves the connection for a node (nil: the connection).
func (h *Hub) Actions(id string, n *Node) []Action {
	c, ok := h.Store().Get(id)
	if !ok {
		return nil
	}
	_, drv, ok := h.Engine(c.Engine)
	if !ok {
		return nil
	}
	return drv.Actions(c, n)
}

// ActionsIn is Actions for the window tab of api: a driver that is an APIDriver is given the API, so that its entries open editors
// in that tab; any other driver answers as Actions does.
func (h *Hub) ActionsIn(api extension.API, id string, n *Node) []Action {
	c, ok := h.Store().Get(id)
	if !ok {
		return nil
	}
	_, drv, ok := h.Engine(c.Engine)
	if !ok {
		return nil
	}
	if ad, ok := drv.(APIDriver); ok {
		return ad.ActionsIn(api, c, n)
	}
	return drv.Actions(c, n)
}
