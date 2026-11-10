package langserver

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The controller over the fake API: the status item, the menu, the notification for a missing server, the answers to it, and the
// restart on a change of the settings. The host is not here; the fake keeps the launcher and the test runs it (api.Launch).

// rig is a controller in a tab with a folder, a bin folder with the server, and the fake API.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ctl    *Controller
	folder string
	bin    string
	cfg    Config
	// reloads counts the runs of the entry of the pack in the menu.
	reloads int
}

const (
	serverID = "ra"
	pathKey  = "ra.server.path"
	modeKey  = "ra.mode"
)

// newRig makes the rig. The server is "ra" in the bin folder, which is in the PATH of the machine for the tests that need it
// (t.Setenv), unless installed is false.
func newRig(t *testing.T, installed bool, tweak func(*Config)) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin")}
	writeFile(t, r.folder, "Cargo.toml", "", 0o644)
	if installed {
		program(t, r.bin, "ra", "rust-analyzer 1.78.0")
	}
	t.Setenv("PATH", r.bin)
	r.cfg = Config{
		ID: serverID, Name: "rust-analyzer", Languages: []string{"rust"}, Category: "Rust", Purpose: "the language features of Rust",
		Programs: []Program{{
			Name: "rust-analyzer", Exe: "ra", PathSetting: pathKey, After: []string{"~/.cargo/bin"},
			Version: VersionCheck{Args: []string{"--version"}, Min: "1.70"},
			Install: Install{Docs: "https://example.test/ra", Commands: []InstallCommand{
				{Family: FamilyAny, Command: "rustup component add rust-analyzer"},
				{Family: FamilyFedora, Command: "sudo dnf install rust-analyzer"},
			}},
		}},
		RootMarkers: []string{"Cargo.toml"},
		Customize: func(l *Launch, env Env) error {
			l.InitializationOptions = map[string]any{"mode": env.Settings.String(modeKey)}
			return nil
		},
		Menu: []MenuEntry{{Label: "Reload Workspace", Run: func() { r.reloads++ }}},
	}
	if tweak != nil {
		tweak(&r.cfg)
	}
	m := extension.Manifest{ID: "emetos.test", Name: "Test"}
	m.Contributes.LanguageServers = []extension.LanguageServerSpec{r.cfg.Spec()}
	m.Contributes.Settings = append(r.cfg.SettingSpecs(), extension.SettingSpec{Key: modeKey, Type: "string", Default: "fast"})
	m.Events.Subscribes = r.cfg.Subscribes()
	r.api = fakeapi.New(t, m, extension.ScopeInfo{Name: "tab", Folder: r.folder})
	return r
}

// attach attaches the controller, with a Fedora system.
func (r *rig) attach(opts ...Option) {
	r.t.Helper()
	ctl, err := Attach(context.Background(), r.api, r.cfg, append([]Option{WithOSRelease("ID=fedora\n")}, opts...)...)
	if err != nil {
		r.t.Fatal(err)
	}
	r.ctl = ctl
}

// editor puts an editor of the language in front.
func (r *rig) editor(language string) {
	r.t.Helper()
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "file:///work/a." + language, LanguageID: language})
	r.api.Pump()
}

// item is the status item.
func (r *rig) item() *fakeapi.StatusItem {
	r.t.Helper()
	it := r.api.StatusItem(StatusID(serverID))
	if it == nil {
		r.t.Fatal("no status item")
	}
	return it
}

// start runs the launcher as the host does when it starts the server, and tells the controller the new state.
func (r *rig) start() (extension.ServerLaunch, error) {
	r.t.Helper()
	r.api.SetServerStatus(serverID, extension.ServerStatus{State: extension.ServerStarting})
	l, err := r.api.Launch(context.Background(), serverID)
	r.api.Pump()
	if err != nil {
		r.api.SetServerStatus(serverID, extension.ServerStatus{State: extension.ServerMissing})
	} else {
		r.api.SetServerStatus(serverID, extension.ServerStatus{State: extension.ServerRunning, Starts: 1})
	}
	return l, err
}

func TestAttachNeedsAWholeConfig(t *testing.T) {
	r := newRig(t, true, nil)
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"no id", func(c *Config) { c.ID = "" }},
		{"no name", func(c *Config) { c.Name = "" }},
		{"no languages", func(c *Config) { c.Languages = nil }},
		{"no program", func(c *Config) { c.Programs = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := r.cfg
			tt.edit(&cfg)
			_, err := Attach(context.Background(), r.api, cfg)
			wantRoute(t, err, "Attach.ServerConfigInvalid")
		})
	}
}

func TestAttachFailsForAServerTheManifestDoesNotDeclare(t *testing.T) {
	r := newRig(t, true, nil)
	cfg := r.cfg
	cfg.ID = "other"
	_, err := Attach(context.Background(), r.api, cfg)
	wantRoute(t, err, "FakeServerUnknown")
}

func TestTheLauncherStartsTheServerTheConfigDescribes(t *testing.T) {
	r := newRig(t, true, nil)
	r.attach()
	r.api.SetSetting(modeKey, "slow")
	l, err := r.start()
	if err != nil {
		t.Fatal(err)
	}
	if l.Program != filepath.Join(r.bin, "ra") || l.Root != r.folder {
		t.Errorf("launch = %+v", l)
	}
	opts, _ := l.InitializationOptions.(map[string]any)
	if opts["mode"] != "slow" {
		t.Errorf("initializationOptions = %v", l.InitializationOptions)
	}
	if r.ctl.Version() != "1.78.0" || r.ctl.Found().Path != l.Program || r.ctl.Found().Source != FromPath {
		t.Errorf("version %q, found %+v", r.ctl.Version(), r.ctl.Found())
	}
	r.editor("rust")
	tip := r.item().Tooltip
	if !strings.Contains(tip, "rust-analyzer 1.78.0: "+l.Program) {
		t.Errorf("tooltip = %q", tip)
	}
}

func TestTheStatusItemFollowsTheStateOfTheServer(t *testing.T) {
	r := newRig(t, true, nil)
	r.attach()
	r.editor("rust")
	tests := []struct {
		name    string
		status  extension.ServerStatus
		text    string
		tooltip string
		visible bool
	}{
		{"not started", extension.ServerStatus{State: extension.ServerIdle}, "rust-analyzer", "rust-analyzer has not started", true},
		{"starting", extension.ServerStatus{State: extension.ServerStarting}, "$(sync~spin) rust-analyzer", "rust-analyzer is starting", true},
		{"running", extension.ServerStatus{State: extension.ServerRunning}, "$(check) rust-analyzer", "rust-analyzer is running", true},
		{"running and working", extension.ServerStatus{State: extension.ServerRunning, Progress: "Indexing 40%"}, "$(sync~spin) rust-analyzer: Indexing 40%", "rust-analyzer: Indexing 40%", true},
		{"crashed too often", extension.ServerStatus{State: extension.ServerFailed, Crashes: 5}, "$(error) rust-analyzer: rust-analyzer stopped", "not started again", true},
		{"the activation ended", extension.ServerStatus{State: extension.ServerStopped}, "rust-analyzer", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.api.SetServerStatus(serverID, tt.status)
			it := r.item()
			if it.Text != tt.text || !strings.Contains(it.Tooltip, tt.tooltip) || it.Hidden == tt.visible || !strings.HasSuffix(it.Tooltip, "Click for the menu.") || it.OnClick == nil {
				t.Errorf("item = %+v", it)
			}
		})
	}
}

func TestTheStatusItemSaysWhyTheServerIsMissing(t *testing.T) {
	tests := []struct {
		name      string
		installed bool
		setup     func(r *rig)
		text      string
		tooltip   string
	}{
		{"not found", false, nil, "$(warning) rust-analyzer: rust-analyzer not found", "rust-analyzer was not found"},
		{"a wrong path", true, func(r *rig) { r.api.SetSetting(pathKey, "/nowhere/ra") }, "$(warning) rust-analyzer: wrong path", "ra.server.path is set to /nowhere/ra, which does not exist"},
		{"too old", true, func(r *rig) {
			program(t, r.bin, "ra", "rust-analyzer 1.2.0")
		}, "$(warning) rust-analyzer: rust-analyzer too old", "rust-analyzer 1.2.0 is too old: 1.70 or newer is needed"},
		{"another failure", true, func(r *rig) {
			r.cfg.Customize = func(*Launch, Env) error { return errors.New("OtherFailure", "no luck") }
		}, "$(warning) rust-analyzer: rust-analyzer cannot start", "rust-analyzer cannot be started: no luck"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.installed, nil)
			if tt.setup != nil {
				tt.setup(r)
			}
			r.attach()
			r.editor("rust")
			if _, err := r.start(); err == nil {
				t.Fatal("the launch worked")
			}
			it := r.item()
			if it.Text != tt.text || !strings.Contains(it.Tooltip, tt.tooltip) {
				t.Errorf("item = %q / %q; want %q / %q", it.Text, it.Tooltip, tt.text, tt.tooltip)
			}
		})
	}
}

func TestTheStatusItemShowsForTheLanguagesOfTheServerOnly(t *testing.T) {
	r := newRig(t, true, nil)
	r.attach()
	r.api.SetServerStatus(serverID, extension.ServerStatus{State: extension.ServerRunning})
	tests := []struct {
		name  string
		event extension.ActiveEditor
		want  bool
	}{
		{"nothing in front yet", extension.ActiveEditor{}, false},
		{"a Rust file", extension.ActiveEditor{URI: "file:///a.rs", LanguageID: "rust"}, true},
		{"a Markdown file", extension.ActiveEditor{URI: "file:///a.md", LanguageID: "markdown"}, false},
		{"no text editor in front: the last one stays", extension.ActiveEditor{}, false},
		{"a Rust file again", extension.ActiveEditor{URI: "file:///b.rs", LanguageID: "rust"}, true},
		{"no text editor in front after a Rust file", extension.ActiveEditor{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.api.Emit(extension.TopicEditorActiveChanged, tt.event)
			r.api.Pump()
			if got := !r.item().Hidden; got != tt.want {
				t.Errorf("visible = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestThePackCanSetTheLabelAndTheDetailOfTheItem(t *testing.T) {
	r := newRig(t, true, nil)
	r.attach()
	r.editor("rust")
	r.ctl.SetLabel("Rust 1.80 (stable)")
	r.ctl.SetDetail("Toolchain: /home/u/.rustup/toolchains/stable")
	it := r.item()
	if it.Text != "Rust 1.80 (stable)" || !strings.Contains(it.Tooltip, "Toolchain: /home/u/.rustup/toolchains/stable") || !strings.HasPrefix(it.Tooltip, "Rust 1.80 (stable)\n") {
		t.Errorf("item = %q / %q", it.Text, it.Tooltip)
	}
	r.ctl.SetLabel("")
	if got := r.item().Text; got != "rust-analyzer" {
		t.Errorf("text after an empty label = %q", got)
	}
	r.ctl.Refresh()
}

func TestThePackCanTellHowTheServerIsDoing(t *testing.T) {
	r := newRig(t, true, nil)
	r.attach()
	r.editor("rust")
	if _, err := r.start(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		health   Health
		icon     string
		tooltips []string
	}{
		{"running and idle", Health{}, "$(check)", []string{"rust-analyzer is running"}},
		{"ok and idle", Health{Level: HealthOK}, "$(check)", []string{"rust-analyzer is running"}},
		{"busy", Health{Level: HealthOK, Busy: true}, "$(sync~spin)", []string{"rust-analyzer is working"}},
		{"a warning", Health{Level: HealthWarning, Message: "No build scripts"}, "$(warning)", []string{"running, with warnings", "No build scripts"}},
		{"an error beats busy", Health{Level: HealthError, Busy: true, Message: "No workspace"}, "$(error)", []string{"running, with errors", "No workspace"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.ctl.SetHealth(tt.health)
			it := r.item()
			if !strings.HasPrefix(it.Text, tt.icon) {
				t.Errorf("text = %q, want the icon %s", it.Text, tt.icon)
			}
			for _, want := range tt.tooltips {
				if !strings.Contains(it.Tooltip, want) {
					t.Errorf("tooltip = %q, want %q", it.Tooltip, want)
				}
			}
		})
	}
	// the progress the server reports comes first; a server that is not running has no health
	r.ctl.SetHealth(Health{Level: HealthWarning})
	r.api.SetServerStatus(serverID, extension.ServerStatus{State: extension.ServerRunning, Progress: "Indexing 40%"})
	if it := r.item(); !strings.HasPrefix(it.Text, "$(sync~spin)") || !strings.Contains(it.Text, "Indexing 40%") {
		t.Errorf("text = %q", it.Text)
	}
	r.api.SetServerStatus(serverID, extension.ServerStatus{State: extension.ServerStarting})
	r.api.SetServerStatus(serverID, extension.ServerStatus{State: extension.ServerRunning})
	if it := r.item(); !strings.HasPrefix(it.Text, "$(check)") {
		t.Errorf("text = %q after the server started again", it.Text)
	}
}

func TestAMissingServerIsToldOncePerTab(t *testing.T) {
	r := newRig(t, false, nil)
	r.attach()
	missing := extension.ServerStatus{State: extension.ServerMissing}
	_, _ = r.start()
	r.api.SetServerStatus(serverID, missing)
	r.api.SetServerStatus(serverID, missing)
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || notes[0].Message != "rust-analyzer was not found." {
		t.Fatalf("notes = %+v", notes)
	}
	if want := []string{"How to Install", "Copy Install Command", "Set Path..."}; !slices.Equal(notes[0].Actions, want) {
		t.Errorf("actions = %v, want %v", notes[0].Actions, want)
	}
	// the user installs an old one: another problem, told again; the same one again is not
	program(t, r.bin, "ra", "rust-analyzer 1.2.0")
	_, _ = r.start()
	_, _ = r.start()
	notes = r.api.Notes()
	if len(notes) != 2 || notes[1].Message != "rust-analyzer 1.2.0 is too old: 1.70 or newer is needed." {
		t.Fatalf("notes = %+v", notes)
	}
	// a wrong path offers Set Path first
	r.api.SetSetting(pathKey, "/nowhere/ra")
	_, _ = r.start()
	notes = r.api.Notes()
	if len(notes) != 3 || !slices.Equal(notes[2].Actions, []string{"Set Path...", "How to Install"}) {
		t.Fatalf("notes = %+v", notes)
	}
}

// A server that is missing is the user's to install and nobody asked for the notice: it is told once, and not again at the next start
// of Emetos (the global state of the pack remembers the message), until the problem is another one or the server ran.
func TestAMissingServerIsNotToldAgainInALaterSession(t *testing.T) {
	const notFound, tooOld = "rust-analyzer was not found", "rust-analyzer 1.2.0 is too old: 1.70 or newer is needed"
	type step struct {
		name string
		do   func(r *rig)
		// notes is the number of notifications after the step, and told what the global state remembers ("" for nothing).
		notes int
		told  string
	}
	start := func(r *rig) { _, _ = r.start() }
	install := func(version string) func(r *rig) {
		return func(r *rig) {
			program(r.t, r.bin, "ra", "rust-analyzer "+version)
			_, _ = r.start()
		}
	}
	tests := []struct {
		name string
		// told is what an earlier session told the user.
		told  string
		steps []step
	}{
		{"the first session tells it and remembers", "", []step{
			{"the server is missing", start, 1, notFound},
			{"and again", start, 1, notFound},
		}},
		{"a later session is quiet", notFound, []step{
			{"the server is missing", start, 0, notFound},
			{"and again", start, 0, notFound},
		}},
		{"another problem is told", notFound, []step{
			{"an old server was installed", install("1.2.0"), 1, tooOld},
			{"and again", start, 1, tooOld},
		}},
		{"a restart the user asked for is answered", notFound, []step{
			{"the server is missing", start, 0, notFound},
			{"the user restarts it", func(r *rig) { r.ctl.restart(); _, _ = r.start() }, 1, notFound},
			{"the start after it is not asked for", start, 1, notFound},
		}},
		{"a server that ran is told once more when it goes away", notFound, []step{
			{"the server was installed", install("1.78.0"), 0, ""},
			{"it is gone", func(r *rig) {
				if err := os.Remove(filepath.Join(r.bin, "ra")); err != nil {
					r.t.Fatal(err)
				}
				_, _ = r.start()
			}, 1, notFound},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, false, nil)
			if tt.told != "" {
				r.api.SetGlobalState("told:server:"+serverID, tt.told)
			}
			r.attach()
			for _, st := range tt.steps {
				st.do(r)
				if n := len(r.api.Notes()); n != st.notes {
					t.Fatalf("%s: %d notifications, want %d: %+v", st.name, n, st.notes, r.api.Notes())
				}
				told, _ := extension.StateValue[string](r.api.GlobalState(), "told:server:"+serverID)
				if told != st.told {
					t.Fatalf("%s: the global state remembers %q, want %q", st.name, told, st.told)
				}
			}
		})
	}
}

// A server that is there and cannot start is not the kind of problem that is told once for good: every session tells it.
func TestAServerThatFailsIsToldInEverySession(t *testing.T) {
	r := newRig(t, true, func(c *Config) {
		c.Customize = func(*Launch, Env) error { return errors.New("TestBroken", "the workspace cannot be read") }
	})
	r.api.SetGlobalState("told:server:"+serverID, "rust-analyzer cannot be started: the workspace cannot be read")
	r.attach()
	_, _ = r.start()
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "cannot be started") {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestAServerThatCannotStartForAnotherReasonOffersTheOutput(t *testing.T) {
	r := newRig(t, true, func(c *Config) {
		c.Customize = func(*Launch, Env) error { return errors.New("OtherFailure", "no luck") }
	})
	r.attach()
	_, _ = r.start()
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Message != "rust-analyzer cannot be started: no luck." || !slices.Equal(notes[0].Actions, []string{"Show Output"}) {
		t.Fatalf("notes = %+v", notes)
	}
	r.api.RunNoteAction(0, "Show Output")
	if r.api.ServerOutputShown(serverID) != 1 {
		t.Error("the output was not shown")
	}
}

func TestNoNotificationWhenThereIsNoCommandToCopy(t *testing.T) {
	r := newRig(t, false, func(c *Config) { c.Programs[0].Install = Install{Docs: "https://example.test/ra"} })
	r.attach()
	_, _ = r.start()
	notes := r.api.Notes()
	if len(notes) != 1 || !slices.Equal(notes[0].Actions, []string{"How to Install", "Set Path..."}) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestCopyInstallCommand(t *testing.T) {
	tests := []struct {
		name      string
		osRelease string
		want      string
	}{
		{"Fedora", "ID=fedora\n", "sudo dnf install rust-analyzer"},
		{"a system without its own command", "ID=debian\n", "rustup component add rust-analyzer"},
		{"an unknown system", "", "rustup component add rust-analyzer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, false, nil)
			r.attach(WithOSRelease(tt.osRelease))
			_, _ = r.start()
			r.api.RunNoteAction(0, "Copy Install Command")
			if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{tt.want}) {
				t.Errorf("clipboard = %v, want %q", got, tt.want)
			}
			// the command is copied, never run: the only program that ran is the server's own search
			notes := r.api.Notes()
			if last := notes[len(notes)-1]; last.Message != "Copied: "+tt.want {
				t.Errorf("last notification = %q", last.Message)
			}
		})
	}
}

func TestCopyInstallCommandWhereThereIsNoClipboard(t *testing.T) {
	r := newRig(t, false, nil)
	r.attach()
	_, _ = r.start()
	r.api.FailClipboard(errors.New("ExtensionClipboardUnavailable", "no clipboard"))
	r.api.RunNoteAction(0, "Copy Install Command")
	if len(r.api.ClipboardTexts()) != 0 || len(r.api.Notes()) != 1 {
		t.Errorf("clipboard %v, notes %+v", r.api.ClipboardTexts(), r.api.Notes())
	}
}

func TestHowToInstallShowsTheInstructions(t *testing.T) {
	r := newRig(t, false, nil)
	r.attach()
	_, _ = r.start()
	r.api.RunNoteAction(0, "How to Install")
	dialogs := r.api.OpenDialogs()
	if len(dialogs) != 1 {
		t.Fatalf("%d dialogs", len(dialogs))
	}
	spec := dialogs[0].Spec()
	if spec.Message != "How to install rust-analyzer" || spec.Type != extension.DialogInfo {
		t.Errorf("dialog = %+v", spec)
	}
	for _, want := range []string{
		"rust-analyzer provides the language features of Rust.",
		"Emetos does not install language servers.",
		"For this system:\n    sudo dnf install rust-analyzer",
		"Also:\n    Any system: rustup component add rust-analyzer",
		"Instructions: https://example.test/ra",
		"set ra.server.path (Set Path... in the menu)",
	} {
		if !strings.Contains(spec.Detail, want) {
			t.Errorf("the text lacks %q:\n%s", want, spec.Detail)
		}
	}
	if len(spec.Buttons) != 3 || spec.Buttons[0].Label != "Copy Install Command" || spec.Buttons[1].Label != "Open Instructions on GitHub" || !spec.Buttons[2].Cancel {
		t.Errorf("buttons = %+v", spec.Buttons)
	}
	dialogs[0].Press("Copy Install Command")
	r.api.Pump()
	if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{"sudo dnf install rust-analyzer"}) {
		t.Errorf("clipboard = %v", got)
	}
	// closing it copies nothing
	r.api.RunNoteAction(0, "How to Install")
	r.api.OpenDialogs()[0].Cancel()
	r.api.Pump()
	if len(r.api.ClipboardTexts()) != 1 {
		t.Errorf("clipboard = %v", r.api.ClipboardTexts())
	}
}

func TestHowToInstallWithNoCommandHasOnlyClose(t *testing.T) {
	r := newRig(t, false, func(c *Config) {
		c.Purpose = ""
		c.Programs[0].Install = Install{}
		c.Programs[0].PathSetting = ""
	})
	r.attach()
	_, _ = r.start()
	r.api.RunNoteAction(0, "How to Install")
	spec := r.api.OpenDialogs()[0].Spec()
	if len(spec.Buttons) != 2 || spec.Buttons[0].Label != "Open Instructions on GitHub" || spec.Buttons[1].Label != "Close" || strings.Contains(spec.Detail, "For this system") {
		t.Errorf("dialog = %+v", spec)
	}
}

// waitPumped pumps the posted functions until check holds: the address is handed to the desktop on a goroutine.
func waitPumped(t *testing.T, r *rig, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.api.Pump()
		if check() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHowToInstallOpensTheReadmeSection(t *testing.T) {
	tests := []struct {
		name     string
		fail     error
		wantNote string // the start of the message of the notification, "" for none
		wantSev  string
	}{
		{"opened", nil, "", ""},
		{"the desktop cannot open it", errors.New("ExtensionOpenURIUnavailable", "the scope cannot open a URI"), "The instructions are at " + InstructionsURL, "info"},
		{"the portal refuses", errors.New("PortalRefused", "no browser"), "Cannot open " + InstructionsURL, "warn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, false, nil)
			r.attach()
			_, _ = r.start()
			r.api.FailOpenURI(tt.fail)
			notes := len(r.api.Notes())
			r.api.RunNoteAction(0, "How to Install")
			r.api.OpenDialogs()[0].Press("Open Instructions on GitHub")
			if tt.wantNote == "" {
				waitPumped(t, r, "the address to be opened", func() bool { return len(r.api.OpenedURIs()) == 1 })
				if got := r.api.OpenedURIs(); got[0] != "https://github.com/rm4n0s/emetos#languages" {
					t.Errorf("opened %v", got)
				}
				if len(r.api.Notes()) != notes {
					t.Errorf("notes = %+v", r.api.Notes())
				}
				return
			}
			waitPumped(t, r, "the notification", func() bool { return len(r.api.Notes()) > notes })
			note := r.api.Notes()[notes]
			if !strings.HasPrefix(note.Message, tt.wantNote) || !strings.EqualFold(note.Severity, tt.wantSev) || len(r.api.OpenedURIs()) != 0 {
				t.Errorf("note = %+v, opened %v", note, r.api.OpenedURIs())
			}
		})
	}
}

func TestSetPathSavesAValidPath(t *testing.T) {
	r := newRig(t, false, nil)
	r.attach()
	_, _ = r.start()
	elsewhere := program(t, filepath.Join(filepath.Dir(r.bin), "elsewhere"), "ra", "rust-analyzer 1.80.0")
	r.api.RunNoteAction(0, "Set Path...")
	in := r.api.LastInput()
	if in == nil {
		t.Fatal("no input box")
	}
	opts := in.Options()
	if !strings.Contains(opts.Prompt, "rust-analyzer") || !strings.Contains(opts.Message, pathKey) {
		t.Errorf("input = %+v", opts)
	}
	if msg := in.Validate(filepath.Join(r.bin, "nowhere")); !strings.Contains(msg, "does not exist") {
		t.Errorf("validation of a missing file = %q", msg)
	}
	if msg := in.Validate(elsewhere); msg != "" {
		t.Errorf("validation of a program = %q", msg)
	}
	in.Accept("  " + elsewhere + "  ")
	r.api.Pump()
	if got := r.api.SettingUpdates(); len(got) != 1 || got[0].Key != pathKey || got[0].Value != elsewhere {
		t.Errorf("updates = %+v", got)
	}
	// the server read the setting at its start: the change restarts it
	if r.api.ServerRestarts(serverID) != 1 {
		t.Errorf("restarts = %d", r.api.ServerRestarts(serverID))
	}
	// and the new start finds it
	l, err := r.api.Launch(context.Background(), serverID)
	if err != nil || l.Program != elsewhere {
		t.Errorf("launch = %+v, %v", l, err)
	}
}

func TestSetPathCancelledAndFromAClient(t *testing.T) {
	r := newRig(t, false, nil)
	r.attach()
	_, _ = r.start()
	r.api.RunNoteAction(0, "Set Path...")
	r.api.LastInput().Cancel()
	if len(r.api.SettingUpdates()) != 0 {
		t.Error("a cancelled box saved the path")
	}
	// a client of a hosted workspace cannot change the setting of the host
	r.api.RunAs(extension.Origin(3), func() { r.api.RunNoteAction(0, "Set Path...") })
	if len(r.api.Inputs()) != 1 {
		t.Errorf("%d input boxes: the client got one", len(r.api.Inputs()))
	}
	notes := r.api.Notes()
	if last := notes[len(notes)-1]; !strings.Contains(last.Message, "setting of the host") {
		t.Errorf("last notification = %q", last.Message)
	}
	if len(r.api.SettingUpdates()) != 0 {
		t.Error("a client saved the path")
	}
}

func TestSettingsTheStartReadRestartTheServerWhenTheyChange(t *testing.T) {
	r := newRig(t, true, nil)
	r.attach()
	_, _ = r.start()
	// a setting the start did not read, and one that did not change
	r.api.SetSetting("editor.tabSize", 2)
	r.api.SetSetting(modeKey, "fast")
	if n := r.api.ServerRestarts(serverID); n != 0 {
		t.Fatalf("restarts = %d after unrelated settings", n)
	}
	r.api.SetSetting(modeKey, "slow") // the initialization options come from it
	if n := r.api.ServerRestarts(serverID); n != 1 {
		t.Fatalf("restarts = %d after a setting of the options", n)
	}
	// until the new start has read them again, more changes do not restart it again
	r.api.SetSetting(modeKey, "slower")
	if n := r.api.ServerRestarts(serverID); n != 1 {
		t.Fatalf("restarts = %d before the new start", n)
	}
	_, _ = r.start()
	r.api.SetSetting(pathKey, "ra")
	if n := r.api.ServerRestarts(serverID); n != 2 {
		t.Fatalf("restarts = %d after the path", n)
	}
}

func TestNothingRestartsBeforeTheFirstStart(t *testing.T) {
	r := newRig(t, true, nil)
	r.attach()
	r.api.SetSetting(modeKey, "slow")
	r.api.SetSetting(pathKey, "ra")
	if n := r.api.ServerRestarts(serverID); n != 0 {
		t.Errorf("restarts = %d", n)
	}
}

func TestTheMenu(t *testing.T) {
	r := newRig(t, false, nil)
	r.attach()
	r.editor("rust")
	r.item().OnClick()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no menu")
	}
	want := []string{"$(debug-restart) Restart Language Server", "$(output) Show rust-analyzer Output", "$(settings-gear) Open Settings", "Reload Workspace", "$(folder-opened) Set Path..."}
	if got := pick.Labels(); !slices.Equal(got, want) {
		t.Fatalf("running server menu = %v", got)
	}
	pick.Choose(0)
	pick.Close()
	if r.api.ServerRestarts(serverID) != 1 {
		t.Error("Restart did not restart")
	}
	// the menu of a missing server has the ways to get it
	_, _ = r.start()
	r.item().OnClick()
	pick = r.api.LastPick()
	want = []string{"$(debug-restart) Restart Language Server", "$(output) Show rust-analyzer Output", "$(settings-gear) Open Settings", "Reload Workspace", "$(book) How to Install", "$(copy) Copy Install Command", "$(folder-opened) Set Path..."}
	if got := pick.Labels(); !slices.Equal(got, want) {
		t.Fatalf("missing server menu = %v", got)
	}
	items := pick.Options().Items
	if items[2].Command != "workbench.action.openSettings" || items[2].Args != "rust-analyzer" {
		t.Errorf("Open Settings = %+v", items[2])
	}
	pick.Choose(1)
	if r.api.ServerOutputShown(serverID) != 1 {
		t.Error("Show Output did not show it")
	}
	r.item().OnClick()
	r.api.LastPick().Choose(3)
	if r.reloads != 1 {
		t.Error("the entry of the pack did not run")
	}
	r.item().OnClick()
	r.api.LastPick().Choose(4)
	if len(r.api.OpenDialogs()) != 1 {
		t.Error("How to Install showed no dialog")
	}
	r.item().OnClick()
	r.api.LastPick().Choose(5)
	if len(r.api.ClipboardTexts()) != 1 {
		t.Error("Copy Install Command copied nothing")
	}
	r.item().OnClick()
	r.api.LastPick().Choose(6)
	if r.api.LastInput() == nil {
		t.Error("Set Path... showed no box")
	}
}

func TestSettingsQueryOfTheMenu(t *testing.T) {
	r := newRig(t, true, func(c *Config) { c.SettingsQuery = "rust-analyzer.cargo"; c.Menu = nil })
	r.attach()
	r.item().OnClick()
	if it := r.api.LastPick().Options().Items[2]; it.Args != "rust-analyzer.cargo" || it.Description != "rust-analyzer.cargo" {
		t.Errorf("Open Settings = %+v", it)
	}
}

func TestTheControllerIsQuietWhereThereIsNoWorkbench(t *testing.T) {
	// the benign errors of a host with no dialogs, boxes or lists are not failures
	for _, tag := range []string{"ExtensionDialogsUnavailable", "ExtensionInputBoxUnavailable", "ExtensionQuickPickUnavailable", "ExtensionClipboardUnavailable", "ExtensionDeactivated"} {
		if !benign(errors.New(tag, "x")) {
			t.Errorf("%s is not benign", tag)
		}
	}
	if benign(errors.New("SomethingElse", "x")) || benign(context.Canceled) {
		t.Error("another failure is benign")
	}
}

func TestASwitchedOffServerShowsAsDisabledAndTellsNobody(t *testing.T) {
	r := newRig(t, true, func(c *Config) {
		c.Disabled = func(s *Settings) string {
			if s.String(modeKey) == "none" {
				return modeKey + " is none"
			}
			return ""
		}
	})
	r.api.SetSetting(modeKey, "none")
	r.attach()
	r.editor("rust")
	if _, err := r.start(); err == nil {
		t.Fatal("the launch worked")
	}
	it := r.item()
	if it.Text != "$(circle-slash) rust-analyzer: disabled" || !strings.Contains(it.Tooltip, "rust-analyzer is turned off: ra.mode is none") {
		t.Errorf("item = %q / %q", it.Text, it.Tooltip)
	}
	if len(r.api.Notes()) != 0 {
		t.Errorf("the user was told: %v", r.api.Notes())
	}
	// turning it on again restarts the server, because the launch read the setting
	r.api.SetSetting(modeKey, "fast")
	r.api.Pump()
	if got := r.api.ServerRestarts(serverID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
}

func TestSetPathChecksTheValueWithTheCheckOfTheProgram(t *testing.T) {
	r := newRig(t, false, func(c *Config) {
		c.Programs[0].CheckPath = func(env Env, text string) string {
			if strings.HasSuffix(text, "/jdtls") {
				return ""
			}
			return "not a jdtls folder"
		}
	})
	r.attach()
	_, _ = r.start()
	r.api.RunNoteAction(0, "Set Path...")
	in := r.api.LastInput()
	if msg := in.Validate("/opt/jdtls"); msg != "" {
		t.Errorf("validation of what the program accepts = %q", msg)
	}
	if msg := in.Validate(r.bin); msg != "not a jdtls folder" {
		t.Errorf("validation of a folder = %q: the check of the program was not used", msg)
	}
}

func TestInstructionsURLNamesASectionOfTheReadme(t *testing.T) {
	const prefix = "https://github.com/rm4n0s/emetos#"
	if !strings.HasPrefix(InstructionsURL, prefix) {
		t.Fatalf("InstructionsURL = %q", InstructionsURL)
	}
	anchor := strings.TrimPrefix(InstructionsURL, prefix)
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	// GitHub's anchor of a heading is its text in lower case with spaces turned into hyphens
	for _, line := range strings.Split(string(data), "\n") {
		if text, ok := strings.CutPrefix(line, "## "); ok && strings.ReplaceAll(strings.ToLower(strings.TrimSpace(text)), " ", "-") == anchor {
			return
		}
	}
	t.Errorf("README.md has no section \"## ...\" whose anchor is #%s", anchor)
}
