package markdown

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// imageView is a local image of a document: it asks the ImageCache for the pixels when it is built (which is when its block is
// near the viewport) and shows the alt text while they load and when they cannot be loaded.
type imageView struct {
	widgets.StatefulWidget
	cache    *ImageCache
	req      imageRequest
	alt      string
	fallback widgets.Widget
}

// newImageView makes the widget of an image.
func newImageView(cache *ImageCache, req imageRequest, alt string, fallback widgets.Widget) widgets.Widget {
	return &imageView{StatefulWidget: widgets.NewStatefulWidgetBase(), cache: cache, req: req, alt: alt, fallback: fallback}
}

// CreateState implements widgets.StatefulWidget.
func (v *imageView) CreateState() widgets.State { return &imageViewState{} }

type imageViewState struct {
	widgets.StateOf[*imageView]
	res    imageResult
	loaded bool
	cancel func()
}

// InitState asks for the image.
func (s *imageViewState) InitState() { s.request() }

// DidUpdateWidget asks again when the image or its size changed.
func (s *imageViewState) DidUpdateWidget(old *imageView) {
	if v := s.Widget(); v.req != old.req || v.cache != old.cache {
		s.release()
		s.loaded = false
		s.res = imageResult{}
		s.request()
	}
}

// Dispose stops waiting.
func (s *imageViewState) Dispose() { s.release() }

func (s *imageViewState) release() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// request takes the image from the cache or starts loading it.
func (s *imageViewState) request() {
	v := s.Widget()
	v.cache.attach(widgets.WidgetsBindingOf(s.Context()).Post)
	ctx := s.Lifetime()
	key, res, ok := v.cache.get(ctx, v.req)
	if ok {
		s.res, s.loaded = res, true
		return
	}
	s.cancel = v.cache.load(key, v.req, func(res imageResult) {
		s.cancel = nil
		if !s.Mounted() {
			return
		}
		s.SetState(func() { s.res, s.loaded = res, true })
	})
}

// Build implements widgets.State.
func (s *imageViewState) Build(widgets.BuildContext) widgets.Widget {
	v := s.Widget()
	if !s.loaded || s.res.pic == nil {
		return v.fallback
	}
	pic := s.res.pic
	img := widgets.NewRawImage(pic, widgets.RawImageOpts{
		Scale: float64(pic.Width()) / s.res.width, Fit: painting.BoxFitFill, FilterQuality: ui.FilterQualityMedium,
	})
	return widgets.NewAlign(widgets.NewSizedBox(img, widgets.SizedBoxOpts{Width: foundation.Some(s.res.width), Height: foundation.Some(s.res.height)}),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
}
