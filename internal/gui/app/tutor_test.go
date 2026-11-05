package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/tutor"
)

// The Emetos Tutor (step 30.1, SPEC §10.35).

// tutorOpen runs Help: Emetos Tutor in a workspace and waits for the new tab.
func (a *multiApp) tutorOpen(from *Workspace) *Workspace {
	a.t.Helper()
	before := a.m.Count()
	if err := a.command(from, TutorCommand, nil); err != nil {
		a.t.Fatal(err)
	}
	a.waitFor("the tutor's tab", func() bool { return a.m.Count() == before+1 })
	w := a.m.Active()
	a.waitFor("the tutor's lessons", func() bool {
		return w.shell != nil && w.shell.activeEditor() != nil && filepath.Base(w.shell.activeEditor().doc.URI().Path) == tutor.MainFile
	})
	return w
}

func TestTheTutorOpensAFreshPracticeCopyInANewTab(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	first := a.m.Active()
	w := a.tutorOpen(first)
	want := filepath.Join(tutor.Base(env.paths.CacheHome), "tutor")
	if w.Folder != want {
		t.Fatalf("the tutor's folder is %s, want %s", w.Folder, want)
	}
	text, err := os.ReadFile(filepath.Join(w.Folder, tutor.MainFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "{{key:") {
		t.Errorf("a placeholder is left in the lessons")
	}
	// the user's key of Go to Line is in the lessons
	if !strings.Contains(string(text), "Ctrl+G") {
		t.Errorf("the lessons do not name Ctrl+G")
	}
	// the practice copy is not a recently opened folder or file
	g, err := a.p.State.LoadGlobal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(g.Recent.Folders, w.Folder) || slices.ContainsFunc(g.Recent.Files, func(f string) bool { return strings.HasPrefix(f, w.Folder) }) {
		t.Errorf("the tutor is in Open Recent: %v %v", g.Recent.Folders, g.Recent.Files)
	}
	if !slices.Contains(g.Recent.Folders, one) {
		t.Errorf("the user's folder is not in Open Recent: %v", g.Recent.Folders)
	}
	// a second tutor while the first is open gets a folder of its own
	second := a.tutorOpen(first)
	if want := filepath.Join(tutor.Base(env.paths.CacheHome), "tutor-2"); second.Folder != want {
		t.Errorf("the second tutor's folder is %s, want %s", second.Folder, want)
	}
}

func TestATutorLaunchOpensTheTutor(t *testing.T) {
	env := newMultiEnv(t, nil)
	a := env.run(Options{Folder: env.folder("one", "a.go")})
	var got []LaunchOutcome
	a.m.HandleLaunch(ipc.OpenArgs{Tutor: true}, func(o LaunchOutcome) { got = append(got, o) })
	a.waitFor("the launch to end", func() bool { return len(got) == 1 })
	if got[0].ExitCode != 0 {
		t.Fatalf("the launch ended with %+v", got[0])
	}
	if want := filepath.Join(tutor.Base(env.paths.CacheHome), "tutor"); a.m.Active().Folder != want {
		t.Errorf("the active tab is %s, want the tutor", a.m.Active().Folder)
	}
}

func TestATutorThatCannotBeCopiedIsReported(t *testing.T) {
	env := newMultiEnv(t, nil)
	a := env.run(Options{Folder: env.folder("one", "a.go")})
	// a file where the practice folder's parent should be
	base := tutor.Base(env.paths.CacheHome)
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []LaunchOutcome
	a.m.HandleLaunch(ipc.OpenArgs{Tutor: true}, func(o LaunchOutcome) { got = append(got, o) })
	a.waitFor("the launch to end", func() bool { return len(got) == 1 })
	if got[0].ExitCode != 1 || !strings.Contains(got[0].Message, "Emetos Tutor") {
		t.Errorf("the launch ended with %+v", got[0])
	}
	if a.m.Count() != 1 {
		t.Errorf("%d tabs", a.m.Count())
	}
}
