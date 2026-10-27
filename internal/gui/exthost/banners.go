package exthost

import (
	"context"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// bannersAPI is the extension.Banners of one activation.
type bannersAPI struct{ inst *instance }

var _ extension.Banners = bannersAPI{}

// Banners implements extension.API.
func (a *api) Banners() extension.Banners { return bannersAPI{a.inst} }

// Show implements extension.Banners. The banner goes when the activation ends.
func (b bannersAPI) Show(banner extension.Banner) (extension.BannerHandle, error) {
	in := b.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if strings.TrimSpace(banner.Text) == "" {
		return nil, errors.New("ExtensionBannerInvalid", "a banner needs a text", "extension", in.id)
	}
	model := in.scope.opts.Banners
	if model == nil {
		return nil, errors.New("ExtensionBannersUnavailable", "the scope has no workbench to show a banner in", "extension", in.id)
	}
	update, remove := model.Add(b.convert(banner))
	h := &bannerHandle{inst: in, update: update, remove: remove}
	if !in.track(h.Dispose) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return h, nil
}

// convert turns the banner of an extension into the one the workbench shows; its actions run guarded.
func (b bannersAPI) convert(banner extension.Banner) workbench.Banner {
	in := b.inst
	out := workbench.Banner{Text: banner.Text, Icon: banner.Icon}
	for _, a := range banner.Actions {
		a := a
		act := workbench.BannerAction{Label: a.Label}
		if a.Run != nil {
			act.Run = func() { in.scope.runBannerAction(in, a.Label, a.Run) }
		}
		out.Actions = append(out.Actions, act)
	}
	return out
}

// bannerHandle is the extension.BannerHandle of a banner.
type bannerHandle struct {
	inst   *instance
	update func(workbench.Banner)
	remove func()
	once   sync.Once
	mu     sync.Mutex
	gone   bool
}

// Update implements extension.BannerHandle.
func (h *bannerHandle) Update(banner extension.Banner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.gone || h.inst.isDead() || strings.TrimSpace(banner.Text) == "" {
		return
	}
	h.update(bannersAPI{h.inst}.convert(banner))
}

// Dispose implements extension.BannerHandle.
func (h *bannerHandle) Dispose() {
	h.once.Do(func() {
		h.mu.Lock()
		h.gone = true
		h.mu.Unlock()
		h.remove()
	})
}

// runBannerAction calls the action of a banner an extension showed. A panic fails the extension with
// ExtensionBannerActionPanicked.
func (s *Scope) runBannerAction(inst *instance, label string, f func()) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionBannerActionPanicked", "it panicked in the button %q of a banner: %v", label, r).SetMetadata("extension", inst.id, "action", label, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "banner button "+label)
		}
	}()
	f()
}

// fileChooserAPI is the extension.FileChooser of one activation.
type fileChooserAPI struct{ inst *instance }

var _ extension.FileChooser = fileChooserAPI{}

// FileChooser implements extension.API.
func (a *api) FileChooser() extension.FileChooser { return fileChooserAPI{a.inst} }

// Open implements extension.FileChooser.
func (c fileChooserAPI) Open(ctx context.Context, opts extension.FileChooserOptions) (string, error) {
	in := c.inst
	if in.isDead() {
		return "", errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	choose := in.scope.opts.FileChooser
	if choose == nil {
		return "", errors.New("ExtensionFileChooserUnavailable", "the scope has no file dialog to ask in", "extension", in.id)
	}
	return choose(ctx, opts)
}
