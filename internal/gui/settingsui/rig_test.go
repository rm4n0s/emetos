package settingsui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// rig is a settings editor in a window over a store whose files are in isolated folders.
type rig struct {
	t        *testing.T
	ts       *gluttertest.Tester
	store    *settings.Store
	model    *settings.UIModel
	ctl      *Controller
	userPath string
	wsPath   string
	folder   string

	opened []openCall
	errs   []error
	states []editorgroup.SettingsViewState
	copied []string
}

type openCall struct {
	target settings.Target
	key    string
}

type rigOpts struct {
	colors    kit.Colors
	size      ui.Size
	hasFolder bool
	userText  string
	wsText    string
	state     editorgroup.SettingsViewState
	// registry adds settings to the registry of Emetos.
	registry []settings.Setting
}

func newRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	if o.colors == (kit.Colors{}) {
		o.colors = kit.DarkModern()
	}
	if o.size == (ui.Size{}) {
		o.size = ui.Size{Width: 1000, Height: 700}
	}
	paths := testutil.Isolate(t)
	userPath, err := settings.UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), "project")
	r := &rig{t: t, userPath: userPath, folder: folder, wsPath: settings.WorkspaceSettingsPath(folder), ctl: NewController()}
	reg := settings.DefaultRegistry()
	reg.MustRegister(o.registry...)
	opts := settings.StoreOptions{UserPath: userPath}
	if o.hasFolder {
		opts.WorkspacePath = r.wsPath
	}
	r.store = settings.NewStore(reg, opts)
	r.write(userPath, o.userText)
	if o.hasFolder {
		r.write(r.wsPath, o.wsText)
	}
	if _, err := r.store.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.model = settings.NewUIModel(r.store)

	ts := testutil.New(t, o.size)
	r.ts = ts
	ts.PumpWidget(widgets.NewWidgetsApp(o.colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(rs widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewContainer(r.editor(o), widgets.ContainerOpts{Color: foundation.Some(o.colors.EditorBackground)})
				return kit.NewTheme(o.colors, page, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: rs}}})
		},
	}))
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	return r
}

func (r *rig) editor(o rigOpts) widgets.Widget {
	return NewEditor(Options{
		Model: r.model, Context: context.Background(), State: o.state, HasFolder: o.hasFolder, Controller: r.ctl,
		CodeFont: testutil.MonoFamily, Languages: testLanguages,
		OnState: func(s editorgroup.SettingsViewState) { r.states = append(r.states, s) },
		OnOpenJSON: func(t settings.Target, key string) {
			r.opened = append(r.opened, openCall{t, key})
		},
		OnError:  func(err error) { r.errs = append(r.errs, err) },
		CopyText: func(s string) { r.copied = append(r.copied, s) },
	})
}

func (r *rig) write(path, text string) {
	r.t.Helper()
	if text == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) read(path string) string {
	r.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		r.t.Fatal(err)
	}
	return string(data)
}

var _ = painting.AlignmentTopLeft

// testLanguages finds Go, JSON and Markdown by id or name, whatever the case.
func testLanguages(name string) (id, display string, ok bool) {
	switch strings.ToLower(name) {
	case "go":
		return "go", "Go", true
	case "json":
		return "json", "JSON", true
	case "markdown":
		return "markdown", "Markdown", true
	}
	return "", "", false
}
