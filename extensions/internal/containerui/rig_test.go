package containerui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// testConfig is the configuration of the tests: Docker's, with the engine given.
func testConfig(dial DialFunc) Config {
	start := time.Now() // the fake makes its things "now", so the ages of the tests are "less than a minute ago"
	return Config{
		Kind: containerapi.KindDocker, ID: "emetos.containers", Name: "Containers", Engine: "Docker",
		Description: "Containers of Docker", Prefix: "containers", Icon: "package", Order: 60, Contexts: true,
		FirstRunNote: "Access to Docker is power over this computer.",
		Dial:         dial, Debounce: 5 * time.Millisecond, EventsRetry: 5 * time.Millisecond, Now: func() time.Time { return start },
		Getenv: func(string) string { return "" },
	}
}

// rig is the extension activated over the fake API and a fake engine.
type rig struct {
	t    *testing.T
	fake *containerapitest.Fake
	api  *fakeapi.API
	ext  *ext
	cfg  Config
}

// newRig starts a fake Docker engine with the containers, images and so on that setup adds, and activates the extension over it.
func newRig(t *testing.T, setup func(t testing.TB, f *containerapitest.Fake)) *rig {
	t.Helper()
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	if setup != nil {
		setup(t, fake)
	}
	cfg := testConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: fake.Client(), Source: "the fake engine"}, nil
	})
	return activate(t, fake, cfg)
}

// activate activates the extension of a Config and waits until the engine answered.
func activate(t *testing.T, fake *containerapitest.Fake, cfg Config) *rig {
	t.Helper()
	r := startRig(t, fake, cfg)
	r.api.WaitFor("the first lists", func() bool {
		return r.ext.status == statusMissing || (r.ext.status == statusConnected && r.ext.loads[resContainers].loaded && r.ext.loads[resImages].loaded &&
			r.ext.loads[resNetworks].loaded && r.ext.loads[resVolumes].loaded)
	})
	return r
}

// startRig activates the extension and returns at once.
func startRig(t *testing.T, fake *containerapitest.Fake, cfg Config) *rig {
	t.Helper()
	e := New(cfg).(*ext)
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	return &rig{t: t, fake: fake, api: api, ext: e, cfg: cfg}
}

// wait pumps until cond holds.
func (r *rig) wait(what string, cond func() bool) {
	r.t.Helper()
	r.api.WaitFor(what, cond)
}

// settle pumps what the extension posted and waits a moment for the goroutines that are still at work.
func (r *rig) settle() {
	r.t.Helper()
	for i := 0; i < 5; i++ {
		r.api.Pump()
		time.Sleep(15 * time.Millisecond)
	}
	r.api.Pump()
}

// run runs a command of the extension.
func (r *rig) run(name string, args any) {
	r.t.Helper()
	if err := r.api.Run(r.cfg.Command(name), args); err != nil {
		r.t.Fatalf("%s: %v", name, err)
	}
}

// view returns a section view.
func (r *rig) view(section string) *sectionView {
	r.t.Helper()
	v := r.ext.views[section]
	if v == nil {
		r.t.Fatalf("no view %s", section)
	}
	return v
}

// shown is a view in a window. The window builds the view again when the extension asked for it (Refresh), as the host does.
type shown struct {
	*gluttertest.Tester
	r      *rig
	viewID string
	set    widgets.StateSetter
	seen   int
}

// show shows a view in a window.
func (r *rig) show(th uitest.Theme, size gui.Size, viewID string) *shown {
	r.t.Helper()
	s := &shown{r: r, viewID: viewID}
	s.Tester = uitest.Pump(r.t, th, size, widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		s.set = set
		return r.api.View(viewID).Content.Build(bc)
	}))
	s.seen, _, _ = r.api.View(viewID).Snapshot()
	return s
}

// refresh runs what the extension posted, builds the view again when it asked for that, and draws a frame.
func (s *shown) refresh() {
	s.r.t.Helper()
	s.r.api.Pump()
	if n, _, _ := s.r.api.View(s.viewID).Snapshot(); n != s.seen {
		s.seen = n
		s.set(func() {})
	}
	s.Pump()
}

// tap presses the widget with the text and lets the extension answer.
func (s *shown) tap(text string) {
	s.r.t.Helper()
	if !s.Exists(gluttertest.ByText(text)) {
		s.r.t.Fatalf("there is nothing with the text %q", text)
	}
	s.Tap(gluttertest.ByText(text))
	s.refresh()
}

// has tells whether a text is shown.
func (s *shown) has(text string) bool { return s.Exists(gluttertest.ByText(text)) }

// hasPart tells whether a text with the words is shown.
func (s *shown) hasPart(text string) bool { return s.Exists(gluttertest.ByTextContaining(text)) }

// labels lists the labels of the rows of a view's tree.
func (r *rig) labels(section string) []string {
	var out []string
	for _, n := range r.view(section).tree.Rows() {
		out = append(out, n.Label)
	}
	return out
}

// notes lists the messages of the notifications, with their severity.
func (r *rig) notes() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

// hasNote tells whether a notification has a text.
func (r *rig) hasNote(sub string) bool {
	for _, n := range r.notes() {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

// destructive lists the requests the fake received that change something an action of this step must ask about.
func destructive(f *containerapitest.Fake) []string {
	var out []string
	for _, rq := range f.Requests() {
		if rq.Method == "DELETE" || strings.Contains(rq.Path, "/prune") || strings.HasSuffix(rq.Path, "/kill") || strings.HasSuffix(rq.Path, "/stop") ||
			strings.HasSuffix(rq.Path, "/restart") || strings.HasSuffix(rq.Path, "/pause") {
			out = append(out, rq.Method+" "+rq.Path)
		}
	}
	return out
}

// keyOfContainer is the key of the node of a container of the fake.
func keyOfContainer(id string) string { return keyContainer + id }

// sampleEngine fills a fake with the containers of two Compose projects and of none, images (one without a name), a network and a
// volume. shop-web-1 publishes a port.
func sampleEngine(t testing.TB, f *containerapitest.Fake) {
	t.Helper()
	compose := func(project, service string) map[string]string {
		return map[string]string{containerapi.LabelCompose: project, containerapi.LabelComposeService: service}
	}
	f.AddImage(187_000_000, "nginx:1.27", "nginx:latest")
	f.AddImage(432_000_000, "postgres:16")
	f.AddImage(5_000_000, "busybox:1.37.0")
	f.AddImage(90_000_000)
	c := f.Client()
	ctx := context.Background()
	res, err := c.CreateContainer(ctx, containerapi.ContainerSpec{Name: "shop-web-1", Image: "nginx:1.27", Labels: compose("shop", "web"),
		Ports: []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 8080, ContainerPort: 80}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StartContainer(ctx, res.ID); err != nil {
		t.Fatal(err)
	}
	f.AddContainer("shop-db-1", "postgres:16", compose("shop", "db"), "running")
	f.AddContainer("blog-app-1", "ghost:5", compose("blog", "app"), "exited")
	f.AddContainer("scratch", "busybox:1.37.0", nil, "exited")
	if _, err := c.CreateNetwork(ctx, containerapi.NetworkSpec{Name: "shop_default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateVolume(ctx, containerapi.VolumeSpec{Name: "pgdata"}); err != nil {
		t.Fatal(err)
	}
}

// containerapiContextDir is the folder name of a Docker context in the store: the hex SHA-256 of its name.
func containerapiContextDir(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// id is the id of the container with a name in the list of the extension.
func (r *rig) id(name string) string {
	r.t.Helper()
	for _, c := range r.ext.containers {
		if c.Name() == name {
			return c.ID
		}
	}
	r.t.Fatalf("no container %s in %v", name, containerNames(r.ext.containers))
	return ""
}

// state is the state of a container in the engine itself (not in the list of the extension).
func (r *rig) state(name string) string {
	r.t.Helper()
	cs, err := r.fake.Client().ListContainers(context.Background(), containerapi.ListContainersOptions{All: true})
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range cs {
		if c.Name() == name {
			return c.State
		}
	}
	return "(gone)"
}

// engineNames are the names of the containers the engine has.
func (r *rig) engineNames() []string {
	r.t.Helper()
	cs, err := r.fake.Client().ListContainers(context.Background(), containerapi.ListContainersOptions{All: true})
	if err != nil {
		r.t.Fatal(err)
	}
	return containerNames(sortedContainers(cs))
}

// key is the argument of a command that was run on the row of a node.
func key(k string) map[string]any { return map[string]any{"key": k} }

// asked is the confirmation that is shown now; the test fails when there is none.
func (r *rig) asked() *confirmation {
	r.t.Helper()
	r.api.Pump()
	if r.ext.confirm == nil {
		r.t.Fatal("no question is asked")
	}
	return r.ext.confirm
}

// choices are the labels of the buttons of a question.
func choiceLabels(c *confirmation) []string {
	var out []string
	for _, ch := range c.choices {
		out = append(out, ch.label)
	}
	return out
}

// hasRequest tells whether the fake received a request with the method and a path that ends with the suffix.
func hasRequest(f *containerapitest.Fake, method, suffix string) bool {
	for _, rq := range f.Requests() {
		if rq.Method == method && strings.HasSuffix(rq.Path, suffix) {
			return true
		}
	}
	return false
}

// sameStrings compares two lists without regard to order.
func sameStrings(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

// widgetFrom makes a widget of a Build function, as the box of the host does.
func widgetFrom(build func(widgets.BuildContext) widgets.Widget) widgets.Widget {
	return widgets.NewBuilder(build)
}

// hasText tells whether a text is shown in a window.
func hasText(ts *gluttertest.Tester, text string) bool {
	return ts.Exists(gluttertest.ByTextContaining(text))
}

// jsonUnmarshalInto reads the JSON a tab saved.
func jsonUnmarshalInto(data []byte, v any) error { return json.Unmarshal(data, v) }

// jsonTokens is a small tokenizer of JSON with the scopes of VS Code's JSON grammar, for the tests that look at a highlighted
// document (the real one is the grammar of Emetos).
func jsonTokens(_, text string) ([][]extension.Token, error) {
	re := regexp.MustCompile(`"(?:[^"\\]|\\.)*"\s*:|"(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null`)
	var out [][]extension.Token
	for _, line := range strings.Split(text, "\n") {
		var toks []extension.Token
		for _, m := range re.FindAllStringIndex(line, -1) {
			s := line[m[0]:m[1]]
			scope := "constant.numeric.json"
			end := m[1]
			switch {
			case strings.HasSuffix(s, ":"):
				scope, end = "support.type.property-name.json", m[0]+strings.LastIndex(s, `"`)+1
			case strings.HasPrefix(s, `"`):
				scope = "string.quoted.double.json"
			case s == "true" || s == "false" || s == "null":
				scope = "constant.language.json"
			}
			toks = append(toks, extension.Token{Start: m[0], End: end, Scopes: []string{"source.json", scope}})
		}
		out = append(out, toks)
	}
	return out, nil
}

// recorder keeps what a goroutine of the extension reports to a test, safely.
type recorder[T any] struct {
	mu    sync.Mutex
	items []T
}

// add records an item.
func (r *recorder[T]) add(v T) { r.mu.Lock(); r.items = append(r.items, v); r.mu.Unlock() }

// len is how many items were recorded.
func (r *recorder[T]) len() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.items) }

// at returns the item at an index.
func (r *recorder[T]) at(i int) T { r.mu.Lock(); defer r.mu.Unlock(); return r.items[i] }

// all returns a copy of the items.
func (r *recorder[T]) all() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]T(nil), r.items...)
}
