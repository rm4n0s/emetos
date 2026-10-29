package containerui

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// connect looks for the engine and connects to it, on a goroutine; the views show "connecting" meanwhile. An engine that was
// connected before is let go. It is the command Retry, and what a changed setting or a picked context does.
func (e *ext) connect() {
	e.closeEngine()
	e.gen++
	gen := e.gen
	e.status, e.reason = statusConnecting, ""
	e.lastHost = e.hostSetting()
	e.setKey(e.cfg.keyConnected(), false)
	e.refreshViews()
	ctx, cancel := context.WithCancel(e.ctx)
	e.engineCancel = cancel
	opts := containerapi.FindOptions{Kind: e.cfg.Kind, Host: e.lastHost, Context: e.picked, Getenv: e.cfg.Getenv}
	if e.cfg.Connections {
		// a connection picked in the view wins over the setting, as a Docker context does; a Podman has no contexts
		opts.Context = ""
		if e.pickedHost != "" {
			opts.Host = e.pickedHost
		}
		e.loadConnections()
	}
	dial := e.cfg.dial()
	go func() {
		found, err := dial(ctx, opts)
		var drop func() bool
		if err == nil {
			drop = context.AfterFunc(e.ctx, found.Close) // closed when the extension ends, also if the post below is never run
		}
		e.api.Post(func() {
			if gen != e.gen {
				if found != nil {
					drop()
					found.Close()
				}
				return
			}
			if err != nil {
				e.lose(err)
				return
			}
			e.found, e.client = found, found.Client
			e.status, e.reason = statusConnected, ""
			e.setKey(e.cfg.keyConnected(), true)
			e.startEvents(ctx, found.Client)
			e.reloadAll()
		})
	}()
}

// closeEngine ends the event stream and lets go of the engine.
func (e *ext) closeEngine() {
	if e.engineCancel != nil {
		e.engineCancel()
		e.engineCancel = nil
	}
	if e.found != nil {
		e.found.Close()
		e.found = nil
	}
	e.client = nil
}

// lose records that there is no engine: the views show why, and the lists are emptied.
func (e *ext) lose(err error) {
	e.reason = reasonOf(err)
	e.status = statusMissing
	e.client = nil
	e.containers, e.images, e.networks, e.volumes, e.pods = nil, nil, nil, nil, nil
	connections := e.loads[resConnections] // the connections are read from the command line, not from the engine
	e.loads = [resCount]loadState{}
	e.loads[resConnections] = connections
	e.confirm = nil
	e.setKey(e.cfg.keyConnected(), false)
	e.api.Log().Info("no container engine", "engine", e.cfg.Engine, "err", err)
	e.rebuildAll()
}

// reasonOf is the text that tells the user why the engine cannot be used.
func reasonOf(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// retry is the Retry button and the command of the same name.
func (e *ext) retry() { e.connect() }

// reloadAll lists everything again.
func (e *ext) reloadAll() {
	e.reload(resContainers, resImages, resNetworks, resVolumes)
	if e.cfg.Pods {
		e.reload(resPods)
	}
	if e.cfg.Contexts {
		e.loadContexts()
	}
}

// reload lists the resources again, on goroutines. A list that is being read when another is asked for is read once more when it
// ends, so the last answer is always of a time after the last change.
func (e *ext) reload(rs ...resource) {
	for _, r := range rs {
		e.load(r)
	}
}

// load lists one resource.
func (e *ext) load(r resource) {
	if e.client == nil {
		return
	}
	l := &e.loads[r]
	if l.busy {
		l.again = true
		return
	}
	l.busy = true
	client, gen, ctx := e.client, e.gen, e.ctx
	showDangling, filters := e.showDangling, e.cfg.ListFilters
	go func() {
		var apply func()
		var err error
		switch r {
		case resContainers:
			var cs []containerapi.Container
			cs, err = client.ListContainers(ctx, containerapi.ListContainersOptions{All: true, Filters: filters.Containers})
			apply = func() {
				e.containers = cs
				running := map[string]bool{}
				for _, c := range cs {
					running[c.ID] = c.State == "running"
				}
				e.files.keepRunning(running)
			}
		case resImages:
			var is []containerapi.Image
			is, err = client.ListImages(ctx, containerapi.ListImagesOptions{All: showDangling, Filters: filters.Images})
			apply = func() { e.images = is }
		case resNetworks:
			var ns []containerapi.Network
			ns, err = client.ListNetworks(ctx, filters.Networks)
			sort.SliceStable(ns, func(i, j int) bool { return strings.ToLower(ns[i].Name) < strings.ToLower(ns[j].Name) })
			apply = func() { e.networks = ns }
		case resVolumes:
			var vs []containerapi.Volume
			vs, err = client.ListVolumes(ctx, filters.Volumes)
			sort.SliceStable(vs, func(i, j int) bool { return strings.ToLower(vs[i].Name) < strings.ToLower(vs[j].Name) })
			apply = func() { e.volumes = vs }
		case resPods:
			var ps []containerapi.Pod
			ps, err = client.ListPods(ctx, filters.Pods)
			sort.SliceStable(ps, func(i, j int) bool { return strings.ToLower(ps[i].Name) < strings.ToLower(ps[j].Name) })
			apply = func() { e.pods = ps }
		}
		e.api.Post(func() {
			l.busy = false
			if gen != e.gen {
				return
			}
			if err != nil {
				e.listFailed(r, err)
			} else {
				apply()
				l.loaded, l.err = true, ""
			}
			if l.again {
				l.again = false
				e.load(r)
			}
			e.rebuild(r)
		})
	}()
}

// listFailed records that a list could not be read. An engine that cannot be reached any more is the missing engine; any other
// failure is shown in the section of the list.
func (e *ext) listFailed(r resource, err error) {
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ContainerAPIConnectFailed" {
		e.lose(err)
		return
	}
	e.loads[r].err = reasonOf(err)
	e.api.Log().Warn("a list cannot be read", "section", sectionOf(r), "err", err)
}

// loadContexts reads the Docker contexts for the picker; a failure leaves the list empty.
func (e *ext) loadContexts() {
	list, err := containerapi.DockerContexts(e.getenv)
	if err != nil {
		e.loads[resContexts].err = reasonOf(err)
		e.contexts = nil
	} else {
		e.contexts, e.loads[resContexts].err = list, ""
	}
	e.current = e.currentContext()
	e.loads[resContexts].loaded = true
	e.rebuild(resContexts)
}

// getenv reads the environment as the engine search does.
func (e *ext) getenv(name string) string {
	if e.cfg.Getenv != nil {
		return e.cfg.Getenv(name)
	}
	return osGetenv(name)
}

// currentContext is the name of the context in use: the one picked in the view, else the one the Docker CLI uses.
func (e *ext) currentContext() string {
	if e.picked != "" {
		return e.picked
	}
	for _, c := range e.contexts {
		if c.Current {
			return c.Name
		}
	}
	return "default"
}

// ---- the event stream ----

// evKey names what an event is about, so that a burst of events about one thing is one event.
type evKey struct{ kind, id string }

// eventBatch collects the events of the engine until the refresh they cause is made.
type eventBatch struct {
	e       *ext
	mu      sync.Mutex
	kinds   [resCount]bool
	pending map[evKey]containerapi.Event
	order   []evKey
	timer   *time.Timer
}

func newEventBatch(e *ext) *eventBatch {
	return &eventBatch{e: e, pending: map[evKey]containerapi.Event{}}
}

// maxPublished is the most events published after one refresh.
const maxPublished = 50

// note records an event, on the goroutine that reads the stream, and arms the timer of the refresh.
func (b *eventBatch) note(ev containerapi.Event, gen int) {
	rs := resourcesOfEvent(ev)
	if len(rs) == 0 {
		return
	}
	if b.e.cfg.Pods && (ev.Type == "container" || ev.Type == "pod") {
		rs = append(rs, resPods) // the state of a pod follows its containers
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range rs {
		b.kinds[r] = true
	}
	k := evKey{ev.Type, ev.ID}
	if _, seen := b.pending[k]; !seen {
		b.order = append(b.order, k)
	}
	b.pending[k] = ev
	if b.timer == nil {
		b.timer = time.AfterFunc(b.e.cfg.debounce(), func() { b.flush(gen) })
	}
}

// flush hands the collected events to the UI thread.
func (b *eventBatch) flush(gen int) {
	b.mu.Lock()
	kinds := b.kinds
	evs := make([]containerapi.Event, 0, len(b.order))
	for _, k := range b.order {
		evs = append(evs, b.pending[k])
	}
	b.kinds = [resCount]bool{}
	b.pending = map[evKey]containerapi.Event{}
	b.order = nil
	b.timer = nil
	b.mu.Unlock()
	e := b.e
	e.api.Post(func() {
		if gen != e.gen {
			return
		}
		for r := resource(0); r < resCount; r++ {
			if kinds[r] {
				e.load(r)
			}
		}
		e.afterEvents(evs)
	})
}

// changed is the value of the events the extension publishes: what changed, in the words of the engine.
type changed struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Action string `json:"action"`
}

// afterEvents publishes what the engine told, one event per thing that changed (at most maxPublished), and tells the logs panel.
func (e *ext) afterEvents(evs []containerapi.Event) {
	for i, ev := range evs {
		if i == maxPublished {
			break
		}
		if err := e.api.Events().Publish(e.cfg.ChangedTopic(), changed{Kind: ev.Type, ID: ev.ID, Action: ev.Action}); err != nil {
			e.api.Log().Warn("an event cannot be published", "err", err)
			break
		}
	}
	e.logs.containersChanged(evs)
}

// resourcesOfEvent are the lists an event changes; none for an event that changes no list (an exec, an attach). A container that
// goes or comes changes the containers; an image that is pulled or removed changes the images and the "image" of the containers.
func resourcesOfEvent(ev containerapi.Event) []resource {
	switch ev.Type {
	case "container", "pod":
		a := ev.Action
		if strings.HasPrefix(a, "exec_") || a == "attach" || a == "resize" || a == "top" || a == "export" || a == "copy" {
			return nil
		}
		return []resource{resContainers}
	case "image":
		return []resource{resImages, resContainers}
	case "network":
		return []resource{resNetworks}
	case "volume":
		return []resource{resVolumes}
	}
	return nil
}

// eventsRetries is how many times a lost event stream is opened again before the engine counts as gone.
const eventsRetries = 3

// startEvents follows the event stream of the engine on a goroutine: no polling, the lists are read when the engine says
// something changed. A stream that ends is opened again a few times; then the engine is gone.
func (e *ext) startEvents(ctx context.Context, client *containerapi.Client) {
	gen := e.gen
	batch := e.events
	go func() {
		failures := 0
		for ctx.Err() == nil {
			err := client.Events(ctx, containerapi.EventsOptions{
				Ready: func() { failures = 0 },
			}, func(ev containerapi.Event) error {
				batch.note(ev, gen)
				return nil
			})
			if ctx.Err() != nil {
				return
			}
			failures++
			if failures > eventsRetries {
				e.api.Post(func() {
					if gen == e.gen {
						e.lose(err)
					}
				})
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(e.cfg.eventsRetry()):
			}
		}
	}()
}
