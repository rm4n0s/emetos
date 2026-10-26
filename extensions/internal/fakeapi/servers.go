package fakeapi

import (
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// serverState is a language server of the manifest as the fake keeps it.
type serverState struct {
	status   extension.ServerStatus
	subs     []*serverSub
	restarts int
	shown    int
}

type serverSub struct{ fn func(extension.ServerStatus) }

type serversAPI struct{ a *API }

// LanguageServers implements extension.API. The servers are those of the manifest; each is idle until SetServerStatus.
func (a *API) LanguageServers() extension.LanguageServers { return serversAPI{a} }

// server returns the state of the server, making it (idle) the first time. The caller holds a.mu.
func (a *API) server(id string) (*serverState, error) {
	if a.servers == nil {
		a.servers = map[string]*serverState{}
	}
	if st, ok := a.servers[id]; ok {
		return st, nil
	}
	for _, spec := range a.manifest.Contributes.LanguageServers {
		if spec.ID == id {
			st := &serverState{status: extension.ServerStatus{State: extension.ServerIdle}}
			a.servers[id] = st
			return st, nil
		}
	}
	return nil, errors.New("FakeServerUnknown", "the manifest declares no language server with this id", "server", id)
}

func (s serversAPI) Status(id string) (extension.ServerStatus, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return extension.ServerStatus{}, err
	}
	return st.status, nil
}

func (s serversAPI) OnChange(id string, fn func(extension.ServerStatus)) (func(), error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return nil, err
	}
	sub := &serverSub{fn: fn}
	st.subs = append(st.subs, sub)
	return func() {
		s.a.mu.Lock()
		defer s.a.mu.Unlock()
		st.subs = slices.DeleteFunc(st.subs, func(x *serverSub) bool { return x == sub })
	}, nil
}

func (s serversAPI) Restart(id string) error {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return err
	}
	st.restarts++
	return nil
}

func (s serversAPI) ShowOutput(id string, _ bool) error {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	st, err := s.a.server(id)
	if err != nil {
		return err
	}
	st.shown++
	return nil
}

// SetServerStatus changes the status of a server of the manifest and calls its listeners, on the test's goroutine, as the host
// does on the UI thread.
func (a *API) SetServerStatus(id string, status extension.ServerStatus) {
	a.mu.Lock()
	st, err := a.server(id)
	if err != nil {
		a.mu.Unlock()
		a.t.Fatal(err)
		return
	}
	st.status = status
	subs := slices.Clone(st.subs)
	a.mu.Unlock()
	for _, sub := range subs {
		sub.fn(status)
	}
}

// ServerRestarts is how many times the extension restarted the server.
func (a *API) ServerRestarts(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		return st.restarts
	}
	return 0
}

// ServerOutputShown is how many times the extension showed the Output channel of the server.
func (a *API) ServerOutputShown(id string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, err := a.server(id); err == nil {
		return st.shown
	}
	return 0
}

// ---- QuickPick ----

// RanCommand is a command a quick pick item ran, with its arguments.
type RanCommand struct {
	ID   string
	Args any
}

// Pick is a quick pick the extension showed.
type Pick struct {
	a    *API
	opts extension.QuickPickOptions
	done func(int)
	mu   sync.Mutex
	over bool
}

// Options are what the extension asked to show.
func (p *Pick) Options() extension.QuickPickOptions { return p.opts }

// Labels are the labels of the items.
func (p *Pick) Labels() []string {
	var out []string
	for _, it := range p.opts.Items {
		out = append(out, it.Label)
	}
	return out
}

// Choose chooses the item: its command is recorded (see RanCommands) and the extension is told the index.
func (p *Pick) Choose(index int) {
	p.mu.Lock()
	if p.over {
		p.mu.Unlock()
		p.a.t.Fatal("the quick pick is over already")
		return
	}
	p.over = true
	p.mu.Unlock()
	it := p.opts.Items[index]
	if it.Command != "" {
		p.a.mu.Lock()
		p.a.ranCommands = append(p.a.ranCommands, RanCommand{ID: it.Command, Args: it.Args})
		p.a.mu.Unlock()
	}
	if p.done != nil {
		p.done(index)
	}
}

// Close closes the quick pick with no choice.
func (p *Pick) Close() {
	p.mu.Lock()
	over := p.over
	p.over = true
	p.mu.Unlock()
	if !over && p.done != nil {
		p.done(-1)
	}
}

type quickPick struct{ a *API }

// QuickPick implements extension.API.
func (a *API) QuickPick() extension.QuickPick { return quickPick{a} }

func (q quickPick) Show(opts extension.QuickPickOptions, done func(int)) error {
	if !slices.ContainsFunc(opts.Items, func(it extension.QuickPickItem) bool { return it.Label != "" }) {
		return errors.New("FakeQuickPickInvalid", "a quick pick needs an item with a label")
	}
	q.a.mu.Lock()
	q.a.picks = append(q.a.picks, &Pick{a: q.a, opts: opts, done: done})
	q.a.mu.Unlock()
	return nil
}

// Picks are the quick picks the extension showed, the first first.
func (a *API) Picks() []*Pick {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.picks)
}

// LastPick is the quick pick shown last, or nil.
func (a *API) LastPick() *Pick {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.picks) == 0 {
		return nil
	}
	return a.picks[len(a.picks)-1]
}

// RanCommands are the commands that quick pick items ran.
func (a *API) RanCommands() []RanCommand {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.ranCommands)
}
