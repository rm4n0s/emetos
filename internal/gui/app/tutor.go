package app

import (
	"path/filepath"

	"github.com/rm4n0s/emetos/assets"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/tutor"
)

// The Emetos Tutor (SPEC §10.35): a fresh practice copy of the lessons, opened as a workspace tab.

// TutorCommand is the command that opens the tutor (Help: Emetos Tutor).
const TutorCommand = "emetos.tutor.open"

// tutorKeys tells the tutor the keys and titles of the commands of a workspace, as its Command Palette shows them.
type tutorKeys struct{ w *Workspace }

func (k tutorKeys) KeyLabel(id string) string { return k.w.statusKeyLabel(id) }

func (k tutorKeys) CommandTitle(id string) (string, bool) {
	m, ok := k.w.Commands.Lookup(id)
	if !ok {
		return "", false
	}
	if m.Category != "" {
		return m.Category + ": " + m.Title, true
	}
	return m.Title, true
}

// tutorCommands are the handlers of the tutor's commands for a shell.
func (s *shellState) tutorCommands() map[string]command.Handler {
	return map[string]command.Handler{
		TutorCommand: func(command.Context, any) error {
			win := PeekWorkspace(s.Context())
			win.Process.Workspaces.openTutor(win, "", nil)
			return nil
		},
	}
}

// openTutor makes a fresh practice copy of the tutor and opens it in a new workspace tab with TUTOR.md. The keys of
// the lessons are those of the workspace from; profile is the profile of the new tab (the one of from when empty).
// It runs on the UI thread; the files are written on another goroutine. done, when set, is told how it ended.
func (m *Workspaces) openTutor(from *Workspace, profile string, done func(LaunchOutcome)) {
	fail := func(err error) {
		m.p.Logger.Error("the tutor cannot be opened", "err", err)
		from.Notify.Error("Emetos", "Cannot open the Emetos Tutor: "+describe(err))
		if done != nil {
			done(LaunchOutcome{ExitCode: 1, Message: "cannot open the Emetos Tutor: " + describe(err)})
		}
	}
	if profile == "" {
		profile = from.ProfileName()
	}
	ids, err := tutor.Placeholders(assets.Tutor(), assets.TutorRoot)
	if err != nil {
		fail(err)
		return
	}
	expansions := tutor.Expansions(ids, tutorKeys{from})
	dir, err := tutor.Folder(m.p.Paths.CacheHome, func(d string) bool { return m.workspaceForFolder(d) != nil })
	if err != nil {
		fail(err)
		return
	}
	open := func(err error) {
		if err != nil {
			fail(err)
			return
		}
		w, err := m.openFrom(WorkspaceSpec{Folder: dir, Profile: profile, Files: []string{filepath.Join(dir, tutor.MainFile)}}, from)
		if err != nil {
			fail(err)
			return
		}
		w.Raise()
		if done != nil {
			done(LaunchOutcome{})
		}
	}
	ctx := m.ctxOr()
	post := m.poster()
	if post == nil {
		// not bound to a binding yet (tests that drive the manager by hand): do it here
		open(tutor.Copy(ctx, assets.Tutor(), assets.TutorRoot, dir, expansions))
		return
	}
	go func() {
		err := tutor.Copy(ctx, assets.Tutor(), assets.TutorRoot, dir, expansions)
		post(func() { open(err) })
	}()
}

// isTutorPractice tells whether a path is in a practice copy of the tutor, which is kept out of Open Recent.
func (p *Process) isTutorPractice(path string) bool { return tutor.IsPractice(p.Paths.CacheHome, path) }
