package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The file dialog an extension asks for with FileChooser().Open (step 15.2) is the file chooser portal of the desktop.

type chooserResult struct {
	path string
	err  error
}

func TestAnExtensionAsksForAFileWithTheDesktopDialog(t *testing.T) {
	opts := extension.FileChooserOptions{
		Title: "Choose a public key", AcceptLabel: "Add", Folder: "/tmp",
		Filters: []extension.FileFilter{{Name: "Public keys", Patterns: []string{"*.pub"}}},
	}
	tests := []struct {
		name   string
		answer func(a *saveApp, path string)
		want   string
	}{
		{"a file is chosen", func(a *saveApp, path string) { a.fd.AnswerFileChooser(path) }, "chosen"},
		{"the dialog is closed", func(a *saveApp, _ string) { a.fd.CancelFileChooser() }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
			chosen := filepath.Join(t.TempDir(), "id.pub")
			res := make(chan chooserResult, 1)
			go func() {
				p, err := a.win.extensionFileChooser(context.Background(), opts)
				res <- chooserResult{p, err}
			}()
			a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
			tt.answer(a, chosen)
			select {
			case r := <-res:
				want := tt.want
				if want == "chosen" {
					want = chosen
				}
				if r.err != nil || r.path != want {
					t.Errorf("result = %q, %v; want %q", r.path, r.err, want)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the call did not return")
			}
		})
	}
}

func TestAnExtensionFileDialogThatCannotBeShown(t *testing.T) {
	// a window that is not on screen has no dialog to ask in
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)
	_, err := w.extensionFileChooser(context.Background(), extension.FileChooserOptions{})
	appRoute(t, err, "Workspace.extensionFileChooser.ExtensionFileChooserUnavailable")

	// a desktop with no file chooser portal
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.fd.DisableFileChooser()
	res := make(chan chooserResult, 1)
	go func() {
		p, err := a.win.extensionFileChooser(context.Background(), extension.FileChooserOptions{})
		res <- chooserResult{p, err}
	}()
	a.fd.WaitFor("the answer", func() bool { return len(res) == 1 })
	err = (<-res).err
	appRoute(t, err, "Workspace.extensionFileChooser.ExtensionFileChooserFailed")
	if !dialogUnavailable(err) {
		t.Errorf("the cause of the failure was lost: %v", err)
	}
}

// The banners the extensions show (Banners().Show, step 15.2) are in the workbench of the workspace, and the headless view of a
// workspace has none of them.
func TestABannerOfAnExtensionIsShownInTheWorkbench(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	if a.win.Banners == nil {
		t.Fatal("the workspace has no banners")
	}
	_, remove := a.win.Banners.Add(workbench.Banner{Text: "Remote Work: laptop B connected", Icon: "remote"})
	a.ts.Pump()
	a.ts.Pump()
	if !a.ts.Exists(gluttertest.ByText("Remote Work: laptop B connected")) {
		t.Fatal("the banner is not shown")
	}
	remove()
	a.ts.Pump()
	a.ts.Pump()
	if a.ts.Exists(gluttertest.ByText("Remote Work: laptop B connected")) {
		t.Error("the banner stays")
	}
}
