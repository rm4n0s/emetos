package fakeapi

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheFakeBannersKeepWhatTheExtensionShowed(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	_, err := a.Banners().Show(extension.Banner{Text: " "})
	route(t, err, "banners.Show.ExtensionBannerInvalid")
	pressed := 0
	h, err := a.Banners().Show(extension.Banner{Text: "Remote Work: laptop B connected", Icon: "remote", Actions: []extension.BannerAction{{Label: "Disconnect", Run: func() { pressed++ }}}})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := a.Banners().Show(extension.Banner{Text: "two"})
	if got := a.ShownBanners(); len(got) != 2 || got[0].Banner().Text != "Remote Work: laptop B connected" {
		t.Fatalf("shown = %v", got)
	}
	a.ShownBanners()[0].Press("Disconnect")
	if pressed != 1 {
		t.Errorf("pressed %d", pressed)
	}
	h.Update(extension.Banner{Text: "changed"})
	if a.AllBanners()[0].Banner().Text != "changed" {
		t.Error("Update did not change the banner")
	}
	h.Dispose()
	h.Update(extension.Banner{Text: "after"})
	if got := a.ShownBanners(); len(got) != 1 || got[0].Banner().Text != "two" || len(a.AllBanners()) != 2 || a.AllBanners()[0].Banner().Text != "changed" {
		t.Errorf("shown = %v, all = %v", got, a.AllBanners())
	}
	_ = second
}

func TestTheFakeFileChooserAnswersWhatTheTestQueued(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	ctx := context.Background()
	if p, err := a.FileChooser().Open(ctx, extension.FileChooserOptions{}); p != "" || err != nil {
		t.Errorf("with no answer queued: %q, %v", p, err)
	}
	a.AnswerFileChooser("/x/id.pub", nil)
	a.AnswerFileChooser("", errors.New("FakeFailure", "no portal"))
	opts := extension.FileChooserOptions{Title: "Choose", Filters: []extension.FileFilter{{Name: "Keys", Patterns: []string{"*.pub"}}}}
	if p, err := a.FileChooser().Open(ctx, opts); p != "/x/id.pub" || err != nil {
		t.Errorf("first answer: %q, %v", p, err)
	}
	_, err := a.FileChooser().Open(ctx, extension.FileChooserOptions{})
	route(t, err, "FakeFailure")
	if calls := a.FileChooserCalls(); len(calls) != 3 || calls[0].Options.Title != "" || calls[1].Options.Title != "Choose" {
		t.Errorf("calls = %+v", calls)
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.FileChooser().Open(done, extension.FileChooserOptions{}); err == nil {
		t.Error("a cancelled context was answered")
	}
}
