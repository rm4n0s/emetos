package containerui

import (
	"context"
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// resource is a kind of thing the engine lists and a section of the view shows.
type resource int

// The things the view lists.
const (
	resContainers resource = iota
	resImages
	resNetworks
	resVolumes
	resContexts
	resPods
	resConnections
	resCount
)

// sectionOf is the section name of a resource.
func sectionOf(r resource) string {
	return [...]string{secContainers, secImages, secNetworks, secVolumes, secContexts, secPods, secConnections}[r]
}

// connStatus says how the connection to the engine is.
type connStatus int

// The states of the connection.
const (
	// statusIdle is before the first connect.
	statusIdle connStatus = iota
	// statusConnecting is while the engine is being looked for.
	statusConnecting
	// statusConnected is a Client that answered.
	statusConnected
	// statusMissing is no engine, or one that went away; reason says why.
	statusMissing
)

// loadState is the loading of one resource.
type loadState struct {
	// busy is a list request in flight, again a change that came during it, loaded that a list arrived once.
	busy, again, loaded bool
	// err is why the last list failed, "" when it did not.
	err string
}

// ext is the extension of one scope. Every field is used on the UI thread, except evMu and what it guards.
type ext struct {
	cfg Config
	api extension.API
	ctx context.Context

	// the connection
	gen          int
	status       connStatus
	reason       string
	found        *containerapi.Found
	client       *containerapi.Client
	engineCancel context.CancelFunc
	// picked is the Docker context chosen in the view (empty is the one the Docker CLI uses), or for Podman the name of the connection
	// chosen in the view (empty is the automatic search) and pickedHost the address of that connection.
	picked, pickedHost string
	// lastHost is the value of the host setting the connection was made with.
	lastHost string

	// the lists
	containers   []containerapi.Container
	images       []containerapi.Image
	networks     []containerapi.Network
	volumes      []containerapi.Volume
	contexts     []containerapi.DockerContext
	pods         []containerapi.Pod
	connections  []connection
	current      string
	loads        [resCount]loadState
	showDangling bool
	filters      [resCount]string

	views   map[string]*sectionView
	confirm *confirmation
	note    noteState
	// missingScroll scrolls the text of the missing-engine view.
	missingScroll ui.ScrollHandle
	ops           []*operation
	files         *fileCache
	logs          *logsPanel
	docs          *documents
	runs          *runForms
	events        *eventBatch

	// the workflows of step 18.3: how Compose is run (found once), the tag last given to each Dockerfile, and the services last
	// chosen for each Compose file
	composeBase []string
	tags        map[string]string
	subsets     map[string][]string
}

// New returns the Factory-made extension of a Config.
func New(cfg Config) extension.Extension {
	return &ext{cfg: cfg, views: map[string]*sectionView{}, tags: map[string]string{}, subsets: map[string][]string{}}
}

// Manifest implements extension.Extension.
func (e *ext) Manifest() extension.Manifest { return Manifest(e.cfg) }

// Activate implements extension.Extension: the commands, the views and the editor kinds are provided, and the engine is looked for
// once the state of the views has been restored.
func (e *ext) Activate(ctx context.Context, api extension.API) error {
	e.api, e.ctx = api, ctx
	e.files = newFileCache()
	e.docs = newDocuments(e)
	e.runs = newRunForms(e)
	e.logs = newLogsPanel(e)
	e.events = newEventBatch(e)
	e.showDangling = true
	for _, def := range commandTable(e.cfg) {
		if err := api.Commands().Handle(e.cfg.Command(def.name), def.handler(e)); err != nil {
			return err
		}
	}
	for _, sec := range e.sections() {
		v := newSectionView(e, sec)
		e.views[sec.name()] = v
		if err := v.provide(); err != nil {
			return err
		}
	}
	if err := e.logs.provide(); err != nil {
		return err
	}
	if err := e.docs.provide(); err != nil {
		return err
	}
	if err := e.runs.provide(); err != nil {
		return err
	}
	e.setKey(e.cfg.keyConnected(), false)
	e.setKey(e.cfg.keyContainersFiltered(), false)
	e.setKey(e.cfg.keyImagesFiltered(), false)
	e.setKey(e.cfg.keyDanglingShown(), e.showDangling)
	e.setKey(e.cfg.keyLogs(), false)
	e.setKey(e.cfg.keyLogsPaused(), false)
	cancel := api.Settings().OnChange(e.settingChanged)
	context.AfterFunc(ctx, cancel)
	context.AfterFunc(ctx, e.closeEngine)
	api.Post(e.connect)
	return nil
}

// settingChanged looks for the engine again when the setting that names it changed.
func (e *ext) settingChanged() {
	if e.status == statusIdle {
		return
	}
	if host := e.hostSetting(); host != e.lastHost {
		e.lastHost = host
		e.connect()
	}
}

// hostSetting reads the setting that names the engine.
func (e *ext) hostSetting() string {
	host, err := extension.SettingValue[string](e.api.Settings(), e.cfg.HostSetting())
	if err != nil {
		e.api.Log().Warn("the host setting cannot be read", "err", err)
		return ""
	}
	return host
}

// setKey sets a context key; a failure is logged (the keys are the extension's own, so it is a bug).
func (e *ext) setKey(key string, value any) {
	if err := e.api.Context().Set(key, value); err != nil {
		e.api.Log().Warn("a context key cannot be set", "key", key, "err", err)
	}
}

// sections are the sections the Config has, in the order of the side bar.
func (e *ext) sections() []section {
	secs := []section{&containersSection{}}
	if e.cfg.Pods {
		secs = append(secs, &podsSection{})
	}
	secs = append(secs, &imagesSection{}, &networksSection{}, &volumesSection{})
	if e.cfg.Contexts {
		secs = append(secs, &contextsSection{})
	}
	if e.cfg.Connections {
		secs = append(secs, &connectionsSection{})
	}
	return secs
}

// viewState is what the Containers view keeps between sessions.
type viewState struct {
	// NoteSeen says the first-run note was dismissed.
	NoteSeen bool `json:"noteSeen,omitempty"`
	// Context is the Docker context picked in the view.
	Context string `json:"context,omitempty"`
	// Connection and ConnectionHost are the Podman connection picked in the view and its address.
	Connection     string `json:"connection,omitempty"`
	ConnectionHost string `json:"connectionHost,omitempty"`
	// HideDangling hides the images that have no name.
	HideDangling bool `json:"hideDangling,omitempty"`
}

// saveState returns the state the Containers view keeps.
func (e *ext) saveState() (any, error) {
	st := viewState{NoteSeen: e.note.seen, Context: e.picked, HideDangling: !e.showDangling}
	if e.cfg.Connections {
		st.Context, st.Connection, st.ConnectionHost = "", e.picked, e.pickedHost
	}
	if st == (viewState{}) {
		return nil, nil
	}
	return st, nil
}

// restoreState reads the state the Containers view kept; a context that differs from the one in use connects again.
func (e *ext) restoreState(raw json.RawMessage) error {
	var st viewState
	if err := json.Unmarshal(raw, &st); err != nil {
		return errors.NewErr("ContainerUIStateInvalid", err)
	}
	e.note.seen = st.NoteSeen
	if e.showDangling == st.HideDangling {
		e.showDangling = !st.HideDangling
		e.setKey(e.cfg.keyDanglingShown(), e.showDangling)
	}
	picked, host := st.Context, ""
	if e.cfg.Connections {
		picked, host = st.Connection, st.ConnectionHost
	}
	if picked != e.picked || host != e.pickedHost {
		e.picked, e.pickedHost = picked, host
		if e.status != statusIdle {
			e.connect()
		}
	}
	return nil
}
