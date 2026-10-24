package app

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

const (
	// AppID is the application id: the Wayland app_id and the X11 WM_CLASS. It
	// matches the .desktop file of step 10.2.
	AppID = "dev.emetos.Emetos"
	// Title is the title of the first window.
	Title = "Emetos"
)

// The size of the first window, in logical pixels.
const (
	windowWidth  = 1280
	windowHeight = 800
)

// Options configure Run.
type Options struct {
	// Process holds the services of the process. When nil, Run makes one from the user's
	// folders and Logger.
	Process *Process
	// Logger gets the log of the engine and of the framework's error reporter, and is the
	// logger of a Process that Run makes; slog.Default() when nil.
	Logger *slog.Logger
	// Folder is the folder to open, or empty for none.
	Folder string
	// Files are the files to open, each in a tab of the editor group. The first one is shown.
	Files []string
	// Open is the command line of this process, when it has one: the folder and the files to open (they
	// replace Folder and Files when those are empty, and a file gets the cursor at its line and column), and the
	// parts that get a notice (--diff, --merge, --profile). Nil is none.
	Open *ipc.OpenArgs
	// DevCommands is the text of EMETOS_DEV_COMMANDS; Run reads the variable when it is
	// empty. It is ignored in builds that do not run dev commands.
	DevCommands string
}

// Run opens the window and runs the event loop until the last window is closed
// or ctx is canceled, which asks the application to exit. It must be called on
// the main goroutine. A ctx that is already canceled opens nothing. The failures
// of the environment (no display, no GPU driver) come back as the tagged errors
// of Glutter. A file that cannot be opened is reported as a notification (and in the
// log), and the window opens without it.
func Run(ctx context.Context, opts Options) error {
	if ctx.Err() != nil {
		return nil
	}
	process := opts.Process
	if process == nil {
		paths, err := storage.ResolvePaths()
		if err != nil {
			return err
		}
		process = NewProcess(ProcessOptions{Paths: paths, Logger: opts.Logger, WatchFiles: true})
	}
	if opts.Open != nil {
		if opts.Folder == "" {
			opts.Folder = opts.Open.Folder
		}
		if len(opts.Files) == 0 {
			for _, f := range opts.Open.Files {
				opts.Files = append(opts.Files, f.Path)
			}
		}
	}
	m := process.Windows
	m.ctx, m.watch = ctx, true
	entry, first, err := m.start(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		// what is left to write (the recently opened list changed after the window closed)
		if err := process.State.Flush(context.WithoutCancel(ctx)); err != nil {
			process.Logger.Warn("the UI state cannot be written", "err", err)
		}
	}()
	first.steps = devSteps(process, opts.DevCommands)
	return widgets.RunApp(m.hostWidget(entry), widgets.RunAppOpts{
		AppID:  AppID,
		Window: startupWindowConfig(first.win, first.geometry),
		Fonts:  fonts.Sources(),
		Logger: opts.Logger,
	})
}

// Texts of the notices about files that are not shown as usual.
const (
	// BinaryFileNotice is what the window says in place of a file that looks binary.
	BinaryFileNotice = "The file is not displayed in the editor because it is either binary or uses an unsupported text encoding."
	// LargeFileNotice is the notification for a document in large file mode (SPEC §6.4).
	LargeFileNotice = "This file is large: syntax highlighting, bracket colors, folding, sticky scroll, the minimap and word wrap are turned off to keep the editor responsive. Set editor.largeFileOptimizations to false to turn them back on."
)

// isBinaryError tells whether a file was not opened because it looks binary.
func isBinaryError(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && appErr.Tag == "FileBinary"
}

// describe is the one-line text of an error for a notification.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// devSteps reads the dev commands from text or, when it is empty, from the environment. It
// gives none in a build that does not run them, and reports a text that does not parse.
func devSteps(p *Process, text string) []DevStep {
	if !DevCommandsEnabled() {
		return nil
	}
	if text == "" {
		text = os.Getenv(DevCommandsEnv)
	}
	steps, err := ParseDevCommands(text)
	if err != nil {
		p.Notify.Error("Emetos", DevCommandsEnv+" is not valid: "+describe(err))
		return nil
	}
	return steps
}

// newApp is the whole app so far: the window's page is the shell.
func newApp(ctx context.Context, win *Window, doc *document.Document, steps []DevStep) widgets.Widget {
	return newAppBlocked(ctx, win, doc, steps, "")
}

// newAppDocs is newApp for the documents of the command line: each opens in the editor group, kept
// (not as a preview), and the first is the one that is shown.
func newAppDocs(ctx context.Context, win *Window, docs []*document.Document, steps []DevStep, blocked string) widgets.Widget {
	return newAppWithPage(win, func() widgets.Widget {
		sh := newShell(ctx, docs, steps)
		sh.blocked = blocked
		return sh
	})
}

// newAppBlocked is newApp for a window whose file looks binary: with no document it shows the
// notice of the file at the path blocked, with a button that opens it anyway.
func newAppBlocked(ctx context.Context, win *Window, doc *document.Document, steps []DevStep, blocked string) widgets.Widget {
	var docs []*document.Document
	if doc != nil {
		docs = append(docs, doc)
	}
	return newAppDocs(ctx, win, docs, steps, blocked)
}

// newAppWithPage is a WidgetsApp with one route, whose page is made by page. The route gives
// the page the Overlay and Navigator it will need. The window and the theme sit above the
// app, so that everything in it can read them.
func newAppWithPage(win *Window, page func() widgets.Widget) widgets.Widget {
	return NewWindowScope(win, NewKeyDispatcher(newThemeHost(func(colors kit.Colors) widgets.Widget {
		return widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
			Title: Title,
			// The window is a proof image: it has no debug ribbon.
			DebugShowCheckedModeBanner: foundation.Some(false),
			// the zoom of the window is above the navigator, so that the pages and the overlays are zoomed together
			Builder: func(_ widgets.BuildContext, navigator widgets.Widget) widgets.Widget { return newZoomHost(navigator) },
			OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
				return widgets.NewPageRouteBuilder(
					func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
						return page()
					},
					widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
			},
		})
	})))
}

// shell is the page of the window: the open file, or the Emetos icon and name when there is
// none. It also turns the cancellation of the context of Run into an exit, and starts the
// dev commands after the first frame.
type shell struct {
	widgets.StatefulWidget
	ctx context.Context
	// docs are the documents the window opens with, the first of them shown.
	docs  []*document.Document
	steps []DevStep
	// blocked is the path of a file that looks binary and is not shown, or "".
	blocked string
}

func newShell(ctx context.Context, docs []*document.Document, steps []DevStep) *shell {
	return &shell{StatefulWidget: widgets.NewStatefulWidgetBase(), ctx: ctx, docs: docs, steps: steps}
}

// CreateState implements widgets.StatefulWidget.
func (p *shell) CreateState() widgets.State { return &shellState{} }

type shellState struct {
	widgets.StateOf[*shell]
	// win and binding are kept from InitState: Dispose cannot look an ancestor up (the element is
	// inactive by then).
	win     *Window
	binding *widgets.WidgetsBinding
	// group is the editor group of the window: the tabs and their editors.
	group groupState
	// nav is the history of locations and the closed editors.
	nav navState
	// panels are the views of the Problems and the Output.
	panels panelsState
	// explorer is the Explorer: the tree of the folder and the Open Editors.
	explorer explorerState
	// blocked is the path of the file that looks binary and that a notice offers to open anyway,
	// while there is no editor.
	blocked string
	// removeOpenFile and removeOpenMenu take the handlers of dev.openFile and dev.openMenu off the
	// registry.
	removeOpenFile, removeOpenMenu func()
	// quickPrefixes are the prefixes of the quick access providers the shell registered, and
	// removeQuickCommands give back the commands that open the quick input.
	quickPrefixes       []string
	removeQuickCommands []func()
	// fileIndex is the list of files of the folder that the shell walks for the quick open, and
	// stopFolderWatch stops following the folder of the window.
	fileIndex       *workspace.FileIndex
	stopFolderWatch func()
	// quickWin is the window the quick input was connected to, for the disposal.
	quickWin *Window
	// endPicks ends the previews of the theme picker that is open (themepickers.go); nil when none is.
	endPicks func()
	// menus turns the menu registry into the menus of the window; menuBar, manage and editorMenu open
	// the menu bar, the Manage menu and the context menu of the editor from outside.
	menus      *workbench.MenuSource
	menuBar    *kit.MenuBarController
	manage     *workbench.ManageController
	editorMenu *kit.ContextMenuController
	// explorerMenu opens the context menu of the Explorer.
	explorerMenu *kit.ContextMenuController
	// unsubscribeTitle stops following the settings of the title.
	unsubscribeTitle func()
	// window is the controller of the window, which gets the title; nil where there is none.
	window interface{ SetTitle(string) }
	// status is what the shell keeps of the status bar (statusbar.go).
	status statusState
	// saves is what the shell keeps of saving (save.go).
	saves saveState
	// closing is what the shell keeps of closing with unsaved changes (closing.go).
	closing closeState
	// autosave is what the shell keeps of auto save (autosave.go).
	autosave autoSaveState
	// ext is what the shell keeps of the files changed on disk (external.go).
	ext externalState
	// state is what the shell keeps of saving the UI state of the window (state.go).
	state stateState
	// launches is what the shell keeps of the launches of other processes (launch.go).
	launches launchState
	// modesStops stop following the full screen mode, zen mode and the zoom of the window (centered.go).
	modesStops []func()
}

// InitState starts the goroutine that waits for the context to be canceled, and schedules
// the dev commands. Both end with the state, so a page that is gone stops waiting.
func (s *shellState) InitState() {
	binding := widgets.WidgetsBindingOf(s.Context())
	s.binding, s.win = binding, PeekWindow(s.Context())
	s.win.shell = s
	s.win.Process.Windows.register(s.win)
	s.blocked = s.Widget().blocked
	s.initBackups(binding)
	s.initAutoSave(binding)
	s.initExternal() // before the editors are made: they are followed as they appear
	s.initGroup(binding, s.Widget().docs)
	s.initMenus(binding)
	s.bindGroupCommands()
	s.initPanels(binding)
	s.initExplorer(binding)
	// the watcher of the folder, of the configuration and of the open files outside the folder
	PeekWindow(s.Context()).StartWatching(s.Lifetime())
	s.initQuickInput(binding)
	s.initStatus(binding)
	s.initSave(binding)
	s.showRestoredConflicts()
	s.initClosing(binding)
	s.initState(binding)
	s.initModes(binding)
	s.initLaunch(binding)
	s.bindDevOpenFile(binding)
	s.bindDevOpenMenu()
	// the title of the window follows its template
	s.unsubscribeTitle = PeekWindow(s.Context()).Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("window.title", "") || e.Affects("window.titleSeparator", "") {
			binding.Post(func() {
				if s.Mounted() {
					s.SetState(func() {})
				}
			})
		}
	})
	if !s.win.managed() {
		// a window on its own (a test of one window) ends the program itself; the windows the manager hosts are ended
		// together by it (Windows.watchSignal)
		done := s.Widget().ctx.Done()
		s.Go(func(life context.Context) {
			select {
			case <-done:
				binding.Post(func() {
					// the signal that ends the program does not go through the question about unsaved changes, but
					// the UI state is still written
					if s.Mounted() {
						s.backupOnSignal()
						s.flushState()
					}
					binding.ExitApplication(ui.AppExitTypeRequired)
				})
			case <-life.Done():
			}
		})
	}
	if steps := s.Widget().steps; len(steps) > 0 {
		win := PeekWindow(s.Context())
		binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
			runner := &DevRunner{
				Steps:    steps,
				Commands: win.Commands,
				Post: func(f func()) {
					// A step is done when the frames after it are: what a command changes (a new
					// editor, a focus) is there for the next one.
					finished := make(chan struct{})
					binding.Post(func() {
						f()
						scheduler := binding.Scheduler()
						scheduler.ScheduleFrame()
						scheduler.AddPostFrameCallback(func(time.Duration) {
							scheduler.ScheduleFrame()
							scheduler.AddPostFrameCallback(func(time.Duration) { close(finished) })
						})
					})
					select {
					case <-finished:
					case <-s.Widget().ctx.Done():
					}
				},
				OnError: func(step DevStep, err error) {
					win.Notify.Error("Emetos", "Dev command "+step.Command+" failed: "+describe(err))
				},
			}
			s.Go(runner.Run)
		})
	}
}

// Dispose stops following the settings, the model of the group and the documents.
func (s *shellState) Dispose() {
	if s.win != nil && s.win.shell == s {
		s.win.shell = nil
	}
	s.disposeLaunch() // before the documents close: a wait ends with the window
	if s.removeOpenFile != nil {
		s.removeOpenFile()
	}
	if s.removeOpenMenu != nil {
		s.removeOpenMenu()
	}
	if s.unsubscribeTitle != nil {
		s.unsubscribeTitle()
	}
	s.disposeModes()
	s.disposeBackups() // before the documents close: their backups stay
	s.disposeState()   // before the groups go: it saves them
	s.disposeClosing()
	s.disposeAutoSave(s.binding)
	s.disposeExternal()
	s.disposeSave()
	s.disposeStatus()
	s.disposeQuickInput()
	s.disposeExplorer()
	s.disposePanels()
	s.disposeGroup()
}

// Build implements widgets.State: the workbench of the window, with what the shell has in the editor
// area.
func (s *shellState) Build(bc widgets.BuildContext) widgets.Widget {
	win := PeekWindow(bc)
	s.syncTitle(bc, win)
	s.syncActiveFile(widgets.WidgetsBindingOf(bc))
	s.syncExplorer(widgets.WidgetsBindingOf(bc), win)
	bench := workbench.New(workbench.Options{
		Layout: win.Layout, Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes,
		Settings: win.Settings, Dialogs: win.Dialogs, Notifications: win.Notifications, QuickInput: win.QuickInput, Context: win.ctx,
		Editor:     s.editorArea(bc),
		FullScreen: win.IsFullScreen(),
		StatusBar:  s.statusBarOptions(win),
		NoFolder:   win.Folder == "",
		Menus:      s.menus.BarMenus(), OnRunMenu: s.menus.Run, MenuBar: s.menuBar,
		Views: win.Views, ViewContext: win.Keys, ManageItems: s.menus.ManageItems, Manage: s.manage,
		ViewBody: s.viewBody, ViewName: s.viewName, ViewActions: s.viewActions, ViewBodyHeight: s.viewBodyHeight,
		OnViewFocus: s.viewFocused,
		PanelTabs:   s.panelTabs(win), ActivePanel: win.Views.ActivePanel(), Panel: s.panelBody(win),
		PanelActions: s.panelActions(win), FocusPanelView: s.focusPanelView,
		OnSelectPanel: func(id string) { win.Views.OpenPanel(id) },
	})
	// the drag of a tab shows above everything
	return widgets.NewStack([]widgets.Widget{bench, s.dragLayer()}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// syncTitle makes the title of the window from the template and the active editor, and passes it on
// to the window when it changed.
func (s *shellState) syncTitle(bc widgets.BuildContext, win *Window) {
	if s.window == nil {
		// Glutter: the controller of the window that WindowScope gives is a BaseWindowController, which
		// has no SetTitle (RegularWindowController has it, promoted from an unexported base), so the
		// title is set through an interface assertion. Glutter should put SetTitle in the interface.
		if scope, ok := widgets.GetInherited[*widgets.WindowScope](bc); ok {
			s.window, _ = scope.Controller().(interface{ SetTitle(string) })
			// Glutter: Activate is in the same position (RegularWindowController has it, BaseWindowController does not)
			win.controller = scope.Controller()
			defer win.controllerKnown() // after the controller is followed below
			if src, ok := scope.Controller().(geometrySource); ok {
				s.trackWindow(src)
			}
		}
	}
	in := workbench.TitleInput{
		Template:  settings.Get[string](win.Settings, "window.title", ""),
		Separator: settings.Get[string](win.Settings, "window.titleSeparator", ""),
		AppName:   Title,
		Folder:    win.Folder,
	}
	if home, err := os.UserHomeDir(); err == nil {
		in.Home = home
	}
	if active := s.activeEditor(); active != nil {
		in.LanguageID = active.doc.LanguageID()
		in.Dirty = active.doc.Dirty()
		if uri := active.doc.URI(); uri.Scheme == document.SchemeFile {
			in.ActiveFile = uri.Path
		} else {
			in.ActiveName = uri.Path
		}
	}
	title := in.Title()
	if title == win.title {
		return
	}
	win.title = title
	if s.window != nil {
		controller := s.window
		widgets.WidgetsBindingOf(bc).Post(func() { controller.SetTitle(title) })
	}
}

// homePage is the editor area of a window with no editor: the Emetos icon and name.
func (s *shellState) homePage(bc widgets.BuildContext) widgets.Widget {
	colors := kit.ThemeOf(bc)
	return widgets.NewDefaultTextStyle(uiTextStyle(bc, 14, colors.Foreground),
		widgets.NewContainer(
			widgets.NewCenter(widgets.NewColumn([]widgets.Widget{
				widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconCode),
					widgets.IconOpts{Size: foundation.Some(96.0), Color: foundation.Some(ui.NewColor(0xFF9D9D9D))}),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(16.0)}),
				widgets.NewText(Title, widgets.TextOpts{Style: uiTextStyle(bc, 28, colors.Foreground)}),
			}, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})),
			widgets.ContainerOpts{Color: foundation.Some(colors.EditorBackground)}))
}

// uiTextStyle is a text style in the desktop's UI font (SPEC D14), or the
// generic sans-serif when the desktop names none.
func uiTextStyle(bc widgets.BuildContext, size float64, color ui.Color) *painting.TextStyle {
	family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().UIFont()
	if family == "" {
		family = "sans-serif"
	}
	return painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily:         family,
		FontFamilyFallback: []string{"sans-serif"},
		FontSize:           foundation.Some(size),
		Color:              foundation.Some(color),
	})
}
