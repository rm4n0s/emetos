package fakeapi

import (
	"context"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// FakeBanner is a banner the extension showed: a test reads what it shows and presses its buttons.
type FakeBanner struct {
	a      *API
	mu     sync.Mutex
	banner extension.Banner
	gone   bool
}

// Banners implements extension.API.
func (a *API) Banners() extension.Banners { return banners{a} }

type banners struct{ a *API }

// Show implements extension.Banners: like the real one it refuses a banner with no text.
func (b banners) Show(banner extension.Banner) (extension.BannerHandle, error) {
	if strings.TrimSpace(banner.Text) == "" {
		return nil, errors.New("ExtensionBannerInvalid", "a banner needs a text")
	}
	fb := &FakeBanner{a: b.a, banner: banner}
	b.a.mu.Lock()
	b.a.banners = append(b.a.banners, fb)
	b.a.mu.Unlock()
	return fb, nil
}

// Update implements extension.BannerHandle.
func (b *FakeBanner) Update(banner extension.Banner) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.gone {
		b.banner = banner
	}
}

// Dispose implements extension.BannerHandle.
func (b *FakeBanner) Dispose() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gone = true
}

// Banner is what the banner shows now.
func (b *FakeBanner) Banner() extension.Banner {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.banner
}

// Shown tells whether the banner is still up (it was not disposed).
func (b *FakeBanner) Shown() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.gone
}

// Press presses the button with the label, as the user does; it fails the test when there is none.
func (b *FakeBanner) Press(label string) {
	b.a.t.Helper()
	for _, act := range b.Banner().Actions {
		if act.Label == label {
			if act.Run != nil {
				act.Run()
			}
			return
		}
	}
	b.a.t.Fatalf("the banner %q has no button %q", b.Banner().Text, label)
}

// AllBanners are the banners the extension showed, the first first, the disposed ones included (see FakeBanner.Shown).
func (a *API) AllBanners() []*FakeBanner {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*FakeBanner(nil), a.banners...)
}

// ShownBanners are the banners that are up now.
func (a *API) ShownBanners() []*FakeBanner {
	var out []*FakeBanner
	for _, b := range a.AllBanners() {
		if b.Shown() {
			out = append(out, b)
		}
	}
	return out
}

// ChooserCall is a call of FileChooser().Open.
type ChooserCall struct {
	Options extension.FileChooserOptions
}

// FileChooser implements extension.API.
func (a *API) FileChooser() extension.FileChooser { return fileChooser{a} }

type fileChooser struct{ a *API }

// Open implements extension.FileChooser: it answers with the oldest answer a test queued with AnswerFileChooser, and with
// "" (a dialog that was closed) when there is none.
func (c fileChooser) Open(ctx context.Context, opts extension.FileChooserOptions) (string, error) {
	c.a.mu.Lock()
	c.a.chooserCalls = append(c.a.chooserCalls, ChooserCall{Options: opts})
	var ans *chooserAnswer
	if len(c.a.chooserAnswers) > 0 {
		ans = c.a.chooserAnswers[0]
		c.a.chooserAnswers = c.a.chooserAnswers[1:]
	}
	c.a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if ans == nil {
		return "", nil
	}
	return ans.path, ans.err
}

type chooserAnswer struct {
	path string
	err  error
}

// AnswerFileChooser queues the answer of the next call of FileChooser().Open: the path chosen ("" for a dialog that was
// closed), or the error.
func (a *API) AnswerFileChooser(path string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chooserAnswers = append(a.chooserAnswers, &chooserAnswer{path: path, err: err})
}

// FileChooserCalls are the calls of FileChooser().Open so far.
func (a *API) FileChooserCalls() []ChooserCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ChooserCall(nil), a.chooserCalls...)
}
