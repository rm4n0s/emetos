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

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/editor"
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
	// Files are the files to open. The first one is shown; the others are opened in the
	// window (there is one editor area until the workbench exists).
	Files []string
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
		process = NewProcess(ProcessOptions{Paths: paths, Logger: opts.Logger})
	}
	win, err := NewWindow(ctx, process, opts.Folder)
	if err != nil {
		return err
	}
	var shown *document.Document
	var blocked string
	for _, path := range opts.Files {
		doc, err := win.Documents.OpenFile(ctx, path, document.NewOptions())
		if err != nil {
			if isBinaryError(err) {
				// the window shows a notice with a button to open the file anyway
				if shown == nil && blocked == "" {
					blocked = path
				}
				continue
			}
			process.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
			continue
		}
		if shown == nil {
			shown = doc
		}
	}
	steps := devSteps(process, opts.DevCommands)
	return widgets.RunApp(newAppBlocked(ctx, win, shown, steps, blocked), widgets.RunAppOpts{
		AppID:  AppID,
		Window: widgets.WindowConfig{Title: Title, Size: ui.Size{Width: windowWidth, Height: windowHeight}},
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

// newAppBlocked is newApp for a window whose file looks binary: with no document it shows the
// notice of the file at the path blocked, with a button that opens it anyway.
func newAppBlocked(ctx context.Context, win *Window, doc *document.Document, steps []DevStep, blocked string) widgets.Widget {
	return newAppWithPage(win, func() widgets.Widget {
		sh := newShell(ctx, doc, steps)
		sh.blocked = blocked
		return sh
	})
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
	ctx   context.Context
	doc   *document.Document
	steps []DevStep
	// blocked is the path of a file that looks binary and is not shown, or "".
	blocked string
}

func newShell(ctx context.Context, doc *document.Document, steps []DevStep) *shell {
	return &shell{StatefulWidget: widgets.NewStatefulWidgetBase(), ctx: ctx, doc: doc, steps: steps}
}

// CreateState implements widgets.StatefulWidget.
func (p *shell) CreateState() widgets.State { return &shellState{} }

type shellState struct {
	widgets.StateOf[*shell]
	// view is the view model of the editor of the file, or nil without a file.
	view *editor.ViewModel
	// unsubscribe stops following the settings.
	unsubscribe func()
	// tokenizer keeps the tokens of the document the editor shows.
	tokenizer *tokens.Tokenizer
	// stopBrackets stops the bracket pairs of the document following it, and stopGuides the
	// indentation levels the guides are drawn from.
	stopBrackets func()
	stopGuides   func()
	stopFolding  func()
	// doc is the document the editor shows, or nil; it starts as the one the page was given and
	// changes with dev.openFile.
	doc *document.Document
	// blocked is the path of the file that looks binary and that a notice offers to open anyway,
	// while there is no editor.
	blocked string
	// removeOpenFile and removeOpenMenu take the handlers of dev.openFile and dev.openMenu off the
	// registry.
	removeOpenFile, removeOpenMenu func()
	// menus turns the menu registry into the menus of the window; menuBar, manage and editorMenu open
	// the menu bar, the Manage menu and the context menu of the editor from outside.
	menus      *workbench.MenuSource
	menuBar    *kit.MenuBarController
	manage     *workbench.ManageController
	editorMenu *kit.ContextMenuController
	// unsubscribeDoc stops following the changes of the document (its dirty mark is in the title),
	// and unsubscribeTitle those of the settings of the title.
	unsubscribeDoc, unsubscribeTitle func()
	// lastDirty is the dirty mark the title was last made with.
	lastDirty bool
	// window is the controller of the window, which gets the title; nil where there is none.
	window interface{ SetTitle(string) }
}

// InitState starts the goroutine that waits for the context to be canceled, and schedules
// the dev commands. Both end with the state, so a page that is gone stops waiting.
func (s *shellState) InitState() {
	binding := widgets.WidgetsBindingOf(s.Context())
	s.blocked = s.Widget().blocked
	if doc := s.Widget().doc; doc != nil {
		s.initEditor(binding, doc)
	}
	s.initMenus(binding)
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
	done := s.Widget().ctx.Done()
	s.Go(func(life context.Context) {
		select {
		case <-done:
			binding.Post(func() { binding.ExitApplication(ui.AppExitTypeRequired) })
		case <-life.Done():
		}
	})
	if steps := s.Widget().steps; len(steps) > 0 {
		win := PeekWindow(s.Context())
		binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
			runner := &DevRunner{
				Steps:    steps,
				Commands: win.Process.Commands,
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
					win.Process.Notify.Error("Emetos", "Dev command "+step.Command+" failed: "+describe(err))
				},
			}
			s.Go(runner.Run)
		})
	}
}

// initEditor makes the view model of the file, and keeps its options current as the settings
// change (the change may come from any goroutine, so it is posted to the UI thread).
func (s *shellState) initEditor(binding *widgets.WidgetsBinding, doc *document.Document) {
	s.doc = doc
	s.blocked = ""
	win := PeekWindow(s.Context())
	options := func() editor.Options {
		desktop, _ := binding.PlatformDispatcher().MonospaceFont()
		tabSize, _ := doc.Indentation()
		return editor.OptionsFromSettings(win.Settings, doc.LanguageID(), desktop, tabSize)
	}
	s.view = editor.NewViewModel(doc, options(), nil)
	// the dirty mark of the document is in the title of the window
	s.lastDirty = doc.Dirty()
	s.unsubscribeDoc = doc.Subscribe(func(document.ChangeEvent) {
		binding.Post(func() {
			if s.Mounted() && s.doc == doc && doc.Dirty() != s.lastDirty {
				s.SetState(func() {})
			}
		})
	})
	if win.Documents.AnnounceLarge(doc) {
		win.Process.Notify.Warn("Emetos", LargeFileNotice)
	}
	// The background work of the editor (the folding regions, the tokens) hands its results to the
	// UI thread from the start, before the editor widget exists to say how.
	s.view.SetPoster(binding.Post)
	s.view.SetIndentationDefaults(func() (int, bool) {
		p := win.indentationFor(doc.LanguageID())
		return p.TabSize, p.InsertSpaces
	})
	s.startTokenizer(win, doc)
	s.startBrackets(win, doc)
	s.startGuides(win, doc)
	s.startFolding(win, doc)
	s.view.SetIndenter(win.Process.Indenter(), win.Process.Languages.Code(doc.LanguageID()))
	s.view.SetComments(win.Process.Comments())
	s.view.SetSnippets(win.Process.Snippets)
	s.view.SetAutoClosing(win.Process.AutoClosing().Language(win.Process.Languages.Code(doc.LanguageID())))
	s.unsubscribe = win.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("editor", "") || e.Affects("emmet", "") {
			binding.Post(func() { s.view.SetOptions(options()) })
		}
		if e.Affects("breadcrumbs", "") {
			binding.Post(func() {
				if s.Mounted() {
					s.SetState(func() {})
				}
			})
		}
	})
}

// startTokenizer makes the tokenizer of the document, with the grammar of its language, gives it
// to the editor and starts its background loop, which ends with the state. A language with no
// grammar has none; a grammar that cannot be made is reported and the text stays plain.
func (s *shellState) startTokenizer(win *Window, doc *document.Document) {
	languageID := doc.LanguageID()
	maxLine := int(settings.Get[float64](win.Settings, "editor.maxTokenizationLineLength", languageID))
	grammar, err := win.Process.Grammar(languageID, maxLine)
	if err != nil {
		win.Process.Notify.Warn("Emetos", "Cannot highlight the "+languageID+" file: "+describe(err))
	}
	s.tokenizer = tokens.New(doc, grammar, tokens.Options{})
	s.view.SetTokenizer(s.tokenizer)
	s.Go(s.tokenizer.Run)
}

// startBrackets makes the bracket pairs of the document and gives them to the editor.
func (s *shellState) startBrackets(win *Window, doc *document.Document) {
	code := win.Process.Languages.Code(doc.LanguageID())
	ix, stop := brackets.Attach(doc, s.tokenizer, win.Process.BracketLanguages(), code)
	s.stopBrackets = stop
	s.view.SetBrackets(ix)
}

// startGuides makes the indentation levels of the document, which its indent guides and bracket
// pair guides are drawn from, and gives them to the editor.
func (s *shellState) startGuides(win *Window, doc *document.Document) {
	tabSize, _ := doc.Indentation()
	levels, stop := indent.AttachLevels(doc, tabSize)
	s.stopGuides = stop
	offSide := false
	if c := win.Process.Languages.Configuration(doc.LanguageID()); c != nil && c.Folding != nil {
		offSide = c.Folding.OffSide
	}
	s.view.SetGuides(levels, offSide)
}

// startFolding makes the folding of the document: the regions come from the indentation of its
// lines and the folding rules of its language, up to editor.foldingMaximumRegions, and are
// computed in the background.
func (s *shellState) startFolding(win *Window, doc *document.Document) {
	provider := folding.IndentProvider{
		Limit: folding.Limit{Max: int(settings.Get[float64](win.Settings, "editor.foldingMaximumRegions", doc.LanguageID()))},
	}
	if c := win.Process.Languages.Configuration(doc.LanguageID()); c != nil && c.Folding != nil {
		provider.OffSide = c.Folding.OffSide
		if c.Folding.MarkersStart != nil && c.Folding.MarkersEnd != nil {
			provider.Markers = &folding.Markers{Start: c.Folding.MarkersStart, End: c.Folding.MarkersEnd}
		}
	}
	s.view.SetFoldingMarkers(provider.Markers)
	s.stopFolding = s.view.StartFolding(provider, 0)
}

// disposeEditor stops following the settings and the document, and drops the view model.
func (s *shellState) disposeEditor() {
	if s.view == nil {
		return
	}
	if s.unsubscribeDoc != nil {
		s.unsubscribeDoc()
		s.unsubscribeDoc = nil
	}
	s.view.DisposeFind()
	s.view.DisposeWrap()
	s.view.DisposeHighlights()
	if s.stopFolding != nil {
		s.stopFolding()
		s.stopFolding = nil
	}
	if s.stopGuides != nil {
		s.view.SetGuides(nil, false)
		s.stopGuides()
		s.stopGuides = nil
	}
	if s.unsubscribe != nil {
		s.unsubscribe()
		s.unsubscribe = nil
	}
	if s.stopBrackets != nil {
		s.view.SetBrackets(nil)
		s.stopBrackets()
		s.stopBrackets = nil
	}
	if s.tokenizer != nil {
		s.view.SetTokenizer(nil)
		s.tokenizer.Close()
		s.tokenizer = nil
	}
}

// Dispose stops following the settings and the document.
func (s *shellState) Dispose() {
	if s.removeOpenFile != nil {
		s.removeOpenFile()
	}
	if s.removeOpenMenu != nil {
		s.removeOpenMenu()
	}
	if s.unsubscribeTitle != nil {
		s.unsubscribeTitle()
	}
	s.disposeEditor()
}

// Build implements widgets.State: the workbench of the window, with what the shell has in the editor
// area.
func (s *shellState) Build(bc widgets.BuildContext) widgets.Widget {
	win := PeekWindow(bc)
	s.syncTitle(bc, win)
	return workbench.New(workbench.Options{
		Layout: win.Layout, Commands: win.Process.Commands, Keys: win.Keys, Scopes: win.Scopes,
		Settings: win.Settings, Dialogs: win.Dialogs, Context: win.ctx,
		Editor:   s.editorArea(bc),
		NoFolder: win.Folder == "",
		Menus:    s.menus.BarMenus(), OnRunMenu: s.menus.Run, MenuBar: s.menuBar,
		Views: win.Views, ViewContext: win.Keys, ManageItems: s.menus.ManageItems, Manage: s.manage,
	})
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
	if s.doc != nil {
		in.LanguageID = s.doc.LanguageID()
		in.Dirty = s.doc.Dirty()
		if uri := s.doc.URI(); uri.Scheme == document.SchemeFile {
			in.ActiveFile = uri.Path
		} else {
			in.ActiveName = uri.Path
		}
	}
	s.lastDirty = in.Dirty
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

// editorArea is what the editor area shows: the editor of the file with its breadcrumbs, the notice
// of a file that cannot be shown, or the home page.
func (s *shellState) editorArea(bc widgets.BuildContext) widgets.Widget {
	if s.view != nil {
		win := PeekWindow(bc)
		binding := widgets.WidgetsBindingOf(bc)
		text := editor.NewEditor(s.view, editor.EditorOpts{
			Commands: win.Process.Commands, Keys: win.Keys, Scopes: win.Scopes, Autofocus: true, Context: win.ctx, ClipboardMemory: win.Clipboard, FindMemory: win.Find, Zoom: win.Zoom,
		})
		// a right click opens the context menu of the editor
		var area widgets.Widget = kit.NewContextMenuRegion(text, s.editorMenuItems, kit.ContextMenuOpts{Controller: s.editorMenu, OnRun: s.menus.Run})
		uri := s.doc.URI()
		if uri.Scheme != document.SchemeFile {
			return area
		}
		// the path of the file above the text
		bar := editor.NewBreadcrumbs(win.ctx, uri.Path, editor.BreadcrumbsOptionsFromSettings(win.Settings), editor.BreadcrumbsOpts{
			OnOpen: func(path string) { s.openPath(binding, path) },
		})
		return widgets.NewColumn([]widgets.Widget{bar, widgets.NewExpanded(area)},
			widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	if s.blocked != "" {
		return editor.NewNotice(BinaryFileNotice, editor.NoticeOpts{Action: "Open Anyway", OnAction: func() { s.openAnyway(widgets.WidgetsBindingOf(bc), s.blocked) }})
	}
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
