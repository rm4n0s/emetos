package dbconn

import (
	"context"
	"strconv"
	"strings"
	"unicode"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The topics of Connect to Database (SPEC §10.24 "A container's database"). A container extension publishes its own topic with a
// Prefill for the container the user chose; each database extension that has the engine asks the extension "Databases" to open its
// form with TopicOpenForm.
const (
	// TopicOpenForm is the request the extension "Databases" handles: the payload is a Prefill, and the reply {"opened": true}. The
	// form opens, prefilled, in the window tab; nothing is saved before the user presses Save.
	TopicOpenForm = ID + ".openForm"
	// TopicContainersConnectDatabase and TopicPodmanConnectDatabase are the events the Containers (Docker) and Podman extensions
	// publish for Connect to Database: their ids and ".connectDatabase" (containerui.Config.ConnectDatabaseTopic).
	TopicContainersConnectDatabase = "emetos.containers.connectDatabase"
	TopicPodmanConnectDatabase     = "emetos.podman.connectDatabase"
)

// containerTopics maps the topics of Connect to Database to the id of the extension that owns them: an event on one of them is
// believed only when that extension published it (a client of Relay may publish any topic, but under its own id).
var containerTopics = map[string]string{
	TopicContainersConnectDatabase: "emetos.containers",
	TopicPodmanConnectDatabase:     "emetos.podman",
}

// ContainerTopics lists the events of Connect to Database, for the manifests of the database extensions (Events.Subscribes).
func ContainerTopics() []string {
	return []string{TopicContainersConnectDatabase, TopicPodmanConnectDatabase}
}

// maxPrefillText is the longest text a prefilled field takes, in characters; a longer one is left out.
const maxPrefillText = 256

// Prefill is what a form starts with when it is opened from another extension (a container's database): the engine and the parts of
// a connection that are known. It has no password and no way to carry one: a field that is not here is not read from the payload.
type Prefill struct {
	// Engine is the id of the engine ("postgres", "mysql", "sqlserver", "mongodb", "redis").
	Engine string `json:"engine"`
	// Name is the name of the connection (the container's name), Host and Port where the server listens, User and Database the user
	// and the database the image makes.
	Name     string `json:"name,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Database string `json:"database,omitempty"`
}

// PrefillOfArgs reads the argument of a command or the payload of a request into a Prefill: a string is the engine, a JSON object
// {"engine", "name", "host", "port", "user", "database"} the rest. Any other key (a "password" in particular) is ignored.
func PrefillOfArgs(args any) Prefill {
	switch a := args.(type) {
	case string:
		return Prefill{Engine: a}
	case map[string]any:
		p := Prefill{Engine: textOf(a["engine"]), Name: textOf(a["name"]), Host: textOf(a["host"]), User: textOf(a["user"]), Database: textOf(a["database"])}
		switch port := a["port"].(type) {
		case float64:
			if port >= 1 && port <= 65535 && port == float64(int(port)) {
				p.Port = int(port)
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(port)); err == nil && n >= 1 && n <= 65535 {
				p.Port = n
			}
		}
		return p
	}
	return Prefill{}
}

// textOf reads a prefilled text: a string without control characters, at most maxPrefillText characters, and no "${" (the form
// expands ${env:NAME} when a connection opens, and what a container says must not name a variable of this computer). Anything else is
// left out.
func textOf(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if len([]rune(s)) > maxPrefillText || strings.Contains(s, "${") {
		return ""
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return s
}

// Apply puts the known parts into a new form: the fields the engine has, never the password.
func (p Prefill) Apply(m *FormModel) {
	m.Name = p.Name
	if m.Engine.File {
		return
	}
	m.Host = p.Host
	if p.Port != 0 {
		m.Port = strconv.Itoa(p.Port)
	}
	if !m.Engine.NoUser {
		m.User = p.User
	}
	if !m.Engine.NoDatabase {
		m.Database = p.Database
	}
}

// openPrefilled opens the form of a new connection with the parts of the prefill. It fails with DatabasesEngineNotActive when no
// extension that is active has the engine.
func (e *Extension) openPrefilled(p Prefill) error {
	for _, eng := range e.hub.Engines() {
		if eng.ID != p.Engine {
			continue
		}
		m := NewFormModel(eng, e.folder())
		p.Apply(m)
		e.openForm(formOpen{Draft: m.Draft()})
		return nil
	}
	return errors.New("DatabasesEngineNotActive", "no extension that is active has the engine "+p.Engine, "engine", p.Engine)
}

// handleOpenForm answers TopicOpenForm. A request of a client of Relay is refused: a form is the host's to open.
func (e *Extension) handleOpenForm(_ context.Context, ev extension.Event) (any, error) {
	if ev.Publisher == extension.RelayID {
		return nil, errors.New("DatabasesOpenFormRefused", "a client of a shared workspace may not open the connection form", "publisher", ev.Publisher)
	}
	var raw map[string]any
	if err := ev.Decode(&raw); err != nil {
		return nil, err
	}
	p := PrefillOfArgs(raw)
	if err := e.openPrefilled(p); err != nil {
		return nil, err
	}
	return map[string]bool{"opened": true}, nil
}

// HandleContainerDatabase makes the database extension of the engines open the connection form when a container extension says the
// user chose Connect to Database on a container of one of them: it listens to the events of ContainerTopics and asks the extension
// "Databases" (TopicOpenForm) to open the form with what the event carries. Only the events the container extensions published are
// believed; an event for another engine is left to the extension that has it. The subscriptions end with the extension.
func HandleContainerDatabase(ctx context.Context, api extension.API, engines ...string) error {
	listen := func(ev extension.Event) {
		if owner, ok := containerTopics[ev.Topic]; !ok || ev.Publisher != owner {
			return
		}
		var raw map[string]any
		if decErr := ev.Decode(&raw); decErr != nil {
			api.Log().Warn("an event of Connect to Database cannot be read", "topic", ev.Topic, "err", decErr)
			return
		}
		p := PrefillOfArgs(raw)
		if p.Engine == "" || !containsFold(engines, p.Engine) {
			return
		}
		reqErr := api.Events().Request(ctx, TopicOpenForm, p, func(_ extension.Event, err error) {
			if err != nil {
				api.Log().Warn("the connection form cannot be opened", "engine", p.Engine, "err", err)
				api.Notifications().Warn("Cannot open the connection form: " + err.Error())
			}
		})
		if reqErr != nil {
			api.Log().Warn("the connection form cannot be asked for", "engine", p.Engine, "err", reqErr)
		}
	}
	for _, topic := range ContainerTopics() {
		if _, err := api.Events().Subscribe(topic, listen); err != nil {
			return err
		}
	}
	return nil
}
