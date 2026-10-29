package containerui

import (
	"context"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// DialFunc finds the engine and connects to it. containerapi.Find is the real one; a test gives one that returns the Client of a
// fake engine.
type DialFunc func(ctx context.Context, opts containerapi.FindOptions) (*containerapi.Found, error)

// Config says what an extension that uses this package is: which engine, which ids and which sections.
type Config struct {
	// Kind is the engine, containerapi.KindDocker or containerapi.KindPodman.
	Kind containerapi.Kind
	// ID is the id of the extension (emetos.containers); the ids of its container, views, editor kind and context keys start
	// with it.
	ID string
	// Name is the name of the extension on the Extensions page and of the view container ("Containers"), Engine the name of the
	// engine in texts ("Docker").
	Name, Engine string
	// Description says in one sentence what the extension does.
	Description string
	// Prefix starts the ids of the commands and of the settings: containers (containers.containers.start, containers.host).
	Prefix string
	// Icon is the Codicon of the icon in the activity bar, Order its place there.
	Icon  string
	Order int
	// Contexts adds the Contexts section and the context picker (Docker).
	Contexts bool
	// FirstRunNote is the note at the top of the Containers section until the user dismisses it: what the access to the engine
	// means.
	FirstRunNote string
	// ListFilters narrow what the views list: the engine does the filtering, so the extension never sees the rest. A test against the
	// user's own engine lists only what it made itself (the label dev.emetos.test=1); a normal extension has none.
	ListFilters ListFilters
	// Dial finds the engine; nil is containerapi.Find.
	Dial DialFunc
	// Getenv reads the environment for Find; nil is os.Getenv. A test passes a map.
	Getenv func(string) string
	// Debounce is how long the refresh waits after an event of the engine so that a burst of them is one refresh; 0 is 150 ms.
	Debounce time.Duration
	// EventsRetry is how long the extension waits before it opens a lost event stream again; 0 is one second.
	EventsRetry time.Duration
	// Now is the clock the ages are counted from; nil is time.Now.
	Now func() time.Time
}

// ListFilters are the filters of the lists of the four sections.
type ListFilters struct {
	// Containers, Images, Networks and Volumes are the filters in the form the engines take (containerapi.Filters).
	Containers, Images, Networks, Volumes containerapi.Filters
}

// debounce returns the refresh delay.
func (c Config) debounce() time.Duration {
	if c.Debounce > 0 {
		return c.Debounce
	}
	return 150 * time.Millisecond
}

// eventsRetry returns the wait before a lost event stream is opened again.
func (c Config) eventsRetry() time.Duration {
	if c.EventsRetry > 0 {
		return c.EventsRetry
	}
	return time.Second
}

// now returns the time the ages are counted from.
func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// dial returns the function that finds the engine.
func (c Config) dial() DialFunc {
	if c.Dial != nil {
		return c.Dial
	}
	return containerapi.Find
}

// The names of the sections, the suffixes of their view ids.
const (
	secContainers = "containers"
	secImages     = "images"
	secNetworks   = "networks"
	secVolumes    = "volumes"
	secContexts   = "contexts"
)

// ContainerID is the id of the view container of the side bar.
func (c Config) ContainerID() string { return c.ID + ".container" }

// ViewID is the id of the view of a section.
func (c Config) ViewID(section string) string { return c.ID + "." + section }

// LogsViewID is the id of the panel view that shows the logs and the output of commands.
func (c Config) LogsViewID() string { return c.ID + ".logs" }

// DocumentKind is the id of the editor kind that shows a read-only text (inspect, history, a file of a container).
func (c Config) DocumentKind() string { return c.ID + ".document" }

// RunKind is the id of the editor kind of the form that runs an image.
func (c Config) RunKind() string { return c.ID + ".run" }

// Command is the id of a command of the extension: Command("containers.start") is containers.containers.start for Docker.
func (c Config) Command(name string) string { return c.Prefix + "." + name }

// HostSetting is the key of the setting that names the engine.
func (c Config) HostSetting() string { return c.Prefix + ".host" }

// ChangedTopic is the topic of the event published after a refresh that changed something.
func (c Config) ChangedTopic() string { return c.ID + ".changed" }

// The context keys of the extension (they start with the id, SPEC §10.15).
func (c Config) keyConnected() string          { return c.ID + ".connected" }
func (c Config) keyContainersFiltered() string { return c.ID + ".containersFiltered" }
func (c Config) keyImagesFiltered() string     { return c.ID + ".imagesFiltered" }
func (c Config) keyDanglingShown() string      { return c.ID + ".danglingShown" }
func (c Config) keyLogs() string               { return c.ID + ".hasLogs" }
func (c Config) keyLogsPaused() string         { return c.ID + ".logsPaused" }
