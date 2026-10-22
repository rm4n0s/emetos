package app

import (
	"context"
	"log/slog"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

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

// Colors of the empty window: Dark Modern's editor.background, foreground and
// descriptionForeground. Step 3.1 replaces them with the theme.
var (
	background = ui.NewColor(0xFF1F1F1F)
	foreground = ui.NewColor(0xFFCCCCCC)
	muted      = ui.NewColor(0xFF9D9D9D)
)

// Options configure Run.
type Options struct {
	// Logger gets the log of the engine and of the framework's error reporter;
	// slog.Default() when nil.
	Logger *slog.Logger
}

// Run opens the window and runs the event loop until the last window is closed
// or ctx is canceled, which asks the application to exit. It must be called on
// the main goroutine. A ctx that is already canceled opens nothing. The failures
// of the environment (no display, no GPU driver) come back as the tagged errors
// of Glutter.
func Run(ctx context.Context, opts Options) error {
	if ctx.Err() != nil {
		return nil
	}
	return widgets.RunApp(newApp(ctx), widgets.RunAppOpts{
		AppID:  AppID,
		Window: widgets.WindowConfig{Title: Title, Size: ui.Size{Width: windowWidth, Height: windowHeight}},
		Fonts:  fonts.Sources(),
		Logger: opts.Logger,
	})
}

// newApp is the whole app so far: a WidgetsApp with one route, whose page is the
// empty window. The route gives the page the Overlay and Navigator it will need.
func newApp(ctx context.Context) widgets.Widget {
	return widgets.NewWidgetsApp(background, widgets.WidgetsAppOpts{
		Title: Title,
		// The window is a proof image: it has no debug ribbon.
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return newHomePage(ctx)
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	})
}

// homePage is the empty window: the Emetos icon and name on the editor
// background. It also turns the cancellation of the context of Run into an exit.
type homePage struct {
	widgets.StatefulWidget
	ctx context.Context
}

func newHomePage(ctx context.Context) *homePage {
	return &homePage{StatefulWidget: widgets.NewStatefulWidgetBase(), ctx: ctx}
}

// CreateState implements widgets.StatefulWidget.
func (p *homePage) CreateState() widgets.State { return &homePageState{} }

type homePageState struct {
	widgets.StateOf[*homePage]
}

// InitState starts the goroutine that waits for the context to be canceled. It
// ends with the state, so a page that is gone stops waiting.
func (s *homePageState) InitState() {
	binding := widgets.WidgetsBindingOf(s.Context())
	done := s.Widget().ctx.Done()
	s.Go(func(life context.Context) {
		select {
		case <-done:
			binding.Post(func() { binding.ExitApplication(ui.AppExitTypeRequired) })
		case <-life.Done():
		}
	})
}

// Build implements widgets.State.
func (s *homePageState) Build(bc widgets.BuildContext) widgets.Widget {
	return widgets.NewDefaultTextStyle(uiTextStyle(bc, 14, foreground),
		widgets.NewContainer(
			widgets.NewCenter(widgets.NewColumn([]widgets.Widget{
				widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconCode),
					widgets.IconOpts{Size: foundation.Some(96.0), Color: foundation.Some(muted)}),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(16.0)}),
				widgets.NewText(Title, widgets.TextOpts{Style: uiTextStyle(bc, 28, foreground)}),
			}, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})),
			widgets.ContainerOpts{Color: foundation.Some(background)}))
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
