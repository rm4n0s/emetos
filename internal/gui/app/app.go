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
	for _, path := range opts.Files {
		doc, err := win.Documents.OpenFile(ctx, path, document.NewOptions())
		if err != nil {
			process.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
			continue
		}
		if shown == nil {
			shown = doc
		}
	}
	steps := devSteps(process, opts.DevCommands)
	return widgets.RunApp(newApp(ctx, win, shown, steps), widgets.RunAppOpts{
		AppID:  AppID,
		Window: widgets.WindowConfig{Title: Title, Size: ui.Size{Width: windowWidth, Height: windowHeight}},
		Fonts:  fonts.Sources(),
		Logger: opts.Logger,
	})
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
	return newAppWithPage(win, func() widgets.Widget { return newShell(ctx, doc, steps) })
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
	// removeOpenFile takes the handler of dev.openFile off the registry.
	removeOpenFile func()
}

// InitState starts the goroutine that waits for the context to be canceled, and schedules
// the dev commands. Both end with the state, so a page that is gone stops waiting.
func (s *shellState) InitState() {
	binding := widgets.WidgetsBindingOf(s.Context())
	if doc := s.Widget().doc; doc != nil {
		s.initEditor(binding, doc)
	}
	s.bindDevOpenFile(binding)
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
	win := PeekWindow(s.Context())
	options := func() editor.Options {
		desktop, _ := binding.PlatformDispatcher().MonospaceFont()
		tabSize, _ := doc.Indentation()
		return editor.OptionsFromSettings(win.Settings, doc.LanguageID(), desktop, tabSize)
	}
	s.view = editor.NewViewModel(doc, options(), nil)
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
	s.view.SetAutoClosing(win.Process.AutoClosing().Language(win.Process.Languages.Code(doc.LanguageID())))
	s.unsubscribe = win.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("editor", "") {
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
	s.disposeEditor()
}

// Build implements widgets.State.
func (s *shellState) Build(bc widgets.BuildContext) widgets.Widget {
	if s.view != nil {
		win := PeekWindow(bc)
		binding := widgets.WidgetsBindingOf(bc)
		text := editor.NewEditor(s.view, editor.EditorOpts{
			Commands: win.Process.Commands, Keys: win.Keys, Scopes: win.Scopes, Autofocus: true, Context: win.ctx, ClipboardMemory: win.Clipboard, FindMemory: win.Find, Zoom: win.Zoom,
		})
		uri := s.doc.URI()
		if uri.Scheme != document.SchemeFile {
			return text
		}
		// the path of the file above the text, until the workbench (6.x) owns the layout
		bar := editor.NewBreadcrumbs(win.ctx, uri.Path, editor.BreadcrumbsOptionsFromSettings(win.Settings), editor.BreadcrumbsOpts{
			OnOpen: func(path string) { s.openPath(binding, path) },
		})
		return widgets.NewColumn([]widgets.Widget{bar, widgets.NewExpanded(text)},
			widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
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
