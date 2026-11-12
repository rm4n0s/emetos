package app

import (
	"image"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The dev commands dev.view.show, dev.view.run and dev.view.type (SPEC §15.2, step 24.25): a proof of the view of a Remote Work client in one
// window. dev.view.show makes a headless view of the workspace (the view a client of Remote Work has, §10.18) and draws its picture over the
// window; dev.view.run runs a command in the view's own registry (as the palette of the client would) and dev.view.type types text into the
// view with key events (as the client's keyboard does). They exist in debug builds only.

// devViewState is what a workspace keeps of its dev view; UI thread only.
type devViewState struct {
	view *headlessView
	pic  *kit.Picture
}

// registerDevView registers the dev commands of the view; the window binds their handlers while it is shown.
func registerDevView(r *command.Registry) {
	for _, meta := range []command.Meta{
		{ID: "dev.view.show", Description: `Make a headless view of the workspace and draw it over the window. Arguments: {"width": 1280, "height": 800}.`},
		{ID: "dev.view.run", Description: `Run a command in the view. Arguments: {"command": "<id>", "args": <any>}.`},
		{ID: "dev.view.type", Description: `Type text into the view with key events. Arguments: {"text": "<text>", "enter": true}.`},
	} {
		if err := RegisterDev(r, meta, func(command.Context, any) error {
			return errors.New("DevNoWindow", "no window is shown")
		}); err != nil {
			panic(err) // the ids are fixed, new and start with dev.
		}
	}
}

// bindDevView gives the dev commands of the view their handlers.
func (s *shellState) bindDevView() {
	if !s.win.Process.DevCommandsEnabled() {
		return
	}
	win := s.win
	for id, h := range map[string]command.Handler{
		"dev.view.show": func(_ command.Context, args any) error { return s.devViewShow(args) },
		"dev.view.run":  func(c command.Context, args any) error { return s.devViewRun(c, args) },
		"dev.view.type": func(_ command.Context, args any) error { return s.devViewType(args) },
	} {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			win.Process.Logger.Error("cannot bind a dev view command", "command", id, "err", err)
			continue
		}
		s.devViewRemove = append(s.devViewRemove, remove)
	}
}

// devViewShow is dev.view.show.
func (s *shellState) devViewShow(args any) error {
	win := s.win
	m, _ := args.(map[string]any)
	size := extension.Size{Width: 1280, Height: 800}
	if w, ok := m["width"].(float64); ok {
		size.Width = int(w)
	}
	if h, ok := m["height"].(float64); ok {
		size.Height = int(h)
	}
	if win.dev.view != nil {
		return errors.New("DevViewShown", "the view is shown already")
	}
	v, err := win.makeHeadlessView(size, headlessFrameRate)
	if err != nil {
		return err
	}
	win.dev.view = v
	v.OnFrame(func(img *image.RGBA) {
		pic, err := kit.NewPicture(img)
		if err != nil {
			win.Process.Logger.Warn("the picture of the dev view cannot be made", "err", err)
			return
		}
		win.onUI(func() {
			win.dev.pic = pic
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	})
	return nil
}

// devViewRun is dev.view.run.
func (s *shellState) devViewRun(c command.Context, args any) error {
	v := s.win.dev.view
	if v == nil {
		return errors.New("DevViewMissing", "dev.view.show comes first")
	}
	m, _ := args.(map[string]any)
	id, _ := m["command"].(string)
	return v.ws.Commands.Run(command.Context{Ctx: c.Ctx, Keys: v.ws.Keys, Source: command.SourcePalette}, id, m["args"])
}

// devViewType is dev.view.type.
func (s *shellState) devViewType(args any) error {
	v := s.win.dev.view
	if v == nil {
		return errors.New("DevViewMissing", "dev.view.show comes first")
	}
	m, _ := args.(map[string]any)
	text, _ := m["text"].(string)
	for _, r := range text {
		str := string(r)
		v.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Logical: uint64(r), Text: str})
		v.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Logical: uint64(r)})
	}
	if enter, _ := m["enter"].(bool); enter {
		v.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Physical: services.PhysicalKeyboardKeyEnter.USBHidUsage(), Logical: services.LogicalKeyboardKeyEnter.KeyID()})
		v.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Physical: services.PhysicalKeyboardKeyEnter.USBHidUsage(), Logical: services.LogicalKeyboardKeyEnter.KeyID()})
	}
	return nil
}

// devViewLayer is the picture of the dev view over the whole window, or nothing.
func (s *shellState) devViewLayer() widgets.Widget {
	pic := s.win.dev.pic
	if pic == nil {
		return widgets.NewSizedBox(nil)
	}
	return widgets.NewPositioned(kit.NewImage(pic, kit.ImageOpts{Upscale: true}), widgets.PositionedOpts{
		Left: foundation.Some(0.0), Top: foundation.Some(0.0), Right: foundation.Some(0.0), Bottom: foundation.Some(0.0),
	})
}
