package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/ipc"
)

// Texts of the notices of a launch (SPEC §10.11).
const (
	// DiffUnavailableNotice is what --diff says until the diff editor exists (step 9.6).
	DiffUnavailableNotice = "The diff editor is not available yet."
	// MergeUnavailableNotice is what --merge says until the merge editor exists (step 9.7).
	MergeUnavailableNotice = "The merge editor is not available yet."
	// LauncherClosingMessage is the reply to a launch that comes when the window is gone.
	LauncherClosingMessage = "Emetos is closing."
)

// LaunchOutcome is how the handling of a launch ended: the exit code the client ends with, and a message for it
// when the code is not 0.
type LaunchOutcome struct {
	// ExitCode is the exit code of the client; 0 is success.
	ExitCode int
	// Message tells the client why it ends with a code other than 0.
	Message string
}

// Launcher brings the launches that arrive over the socket (core/ipc) to the windows on the UI thread. It is made
// before the process, so the socket can be served while the first window is still starting: a launch that comes
// before the windows are up waits in a queue and is delivered when the manager of the windows attaches (SPEC
// §10.11). The manager picks the window the launch opens in (Windows.HandleLaunch). It is safe for concurrent use.
type Launcher struct {
	mu    sync.Mutex
	wins  *Windows
	post  func(func())
	queue []func()
	// closed is set when the application ends; ended is closed with it.
	closed bool
	ended  chan struct{}
}

// NewLauncher makes a Launcher that has no windows yet.
func NewLauncher() *Launcher { return &Launcher{ended: make(chan struct{})} }

// attach connects the launcher to the manager of the windows and the function that runs a function on the UI
// thread, and delivers the launches that were waiting. It runs on the UI thread.
func (l *Launcher) attach(wins *Windows, post func(func())) {
	l.mu.Lock()
	l.wins, l.post = wins, post
	queued := l.queue
	l.queue = nil
	l.mu.Unlock()
	for _, f := range queued {
		post(f)
	}
}

// Close ends the launcher: launches that wait, and those that come later, get the reply that Emetos is closing.
// The application does it when it ends. It is safe to call more than once.
func (l *Launcher) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	l.wins, l.post, l.queue = nil, nil, nil
	close(l.ended)
}

// dispatch runs f on the UI thread, or queues it until the manager of the windows is attached. It reports false when
// the launcher is closed.
func (l *Launcher) dispatch(f func()) bool {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return false
	}
	if l.wins == nil {
		l.queue = append(l.queue, f)
		l.mu.Unlock()
		return true
	}
	post := l.post
	l.mu.Unlock()
	post(f)
	return true
}

// Handle carries out the launch of a client (an ipc.Handler for TypeOpen requests). It returns when the launch is
// started, or with args.Wait when the files it opened are closed or the window is. A cancelled ctx (the client
// went away) stops the wait. The reply is an error reply when the launch failed or ended with an exit code other
// than 0.
func (l *Launcher) Handle(ctx context.Context, args ipc.OpenArgs) ipc.Reply {
	out := make(chan LaunchOutcome, 1)
	// both are only used on the UI thread, by the function that starts the launch and by the one that cancels it
	var cancel func()
	cancelled := false
	started := l.dispatch(func() {
		if cancelled {
			return
		}
		wins := l.manager()
		if wins == nil {
			out <- LaunchOutcome{ExitCode: 1, Message: LauncherClosingMessage}
			return
		}
		cancel = wins.HandleLaunch(args, func(o LaunchOutcome) {
			select {
			case out <- o:
			default:
			}
		})
	})
	if !started {
		return ipc.Failed(LauncherClosingMessage)
	}
	reply := func(o LaunchOutcome) ipc.Reply {
		switch {
		case o.ExitCode != 0:
			r := ipc.Failed(o.Message)
			r.ExitCode = o.ExitCode
			return r
		case args.Wait:
			return ipc.Done(0)
		}
		return ipc.OK()
	}
	select {
	case o := <-out:
		return reply(o)
	case <-ctx.Done():
		l.dispatch(func() {
			cancelled = true
			if cancel != nil {
				cancel()
			}
		})
		return ipc.Failed("cancelled")
	case <-l.ended:
		// what the window finished while it closed comes first
		select {
		case o := <-out:
			return reply(o)
		default:
		}
		return ipc.Failed(LauncherClosingMessage)
	}
}

// manager is the manager of the windows the launcher is attached to (a function that runs on the UI thread, after
// attach).
func (l *Launcher) manager() *Windows {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.wins
}

// HandleLaunch carries out a launch in this window: what the command line of a second Emetos asked (SPEC §10.11),
// once Windows.HandleLaunch has picked the window. It must run on the UI thread, with the shell on screen. The window
// comes to the front; --diff and --merge show a notice and end with exit code 1 (they come with later steps);
// --profile says in a notice that the window keeps its profile, when it is not the one asked for (Windows.HandleLaunch
// picks or makes the window that has the profile); a folder that is not the folder of the window is opened as Open Folder does (the questions
// about unsaved changes included);
// then the files are opened in the active group, the first of them shown, with the cursor at the line and
// column of a --goto. done is called once on the UI thread: at once when the launch does not wait, else when
// every file that was opened is closed (or the window is, which counts as closed). The returned function
// stops waiting (the client went away); it may be called more than once.
func (w *Window) HandleLaunch(args ipc.OpenArgs, done func(LaunchOutcome)) (cancel func()) {
	if w.launcher == nil {
		done(LaunchOutcome{ExitCode: 1, Message: "the window is not ready"})
		return func() {}
	}
	return w.launcher(args, done)
}

// launchState is what the shell keeps of the launches.
type launchState struct {
	// waits are the launches that wait for their files to close.
	waits []*launchWait
	// stopWatch stops following the documents that close.
	stopWatch func()
}

// launchWait is a launch with --wait: it ends when every document it opened is closed.
type launchWait struct {
	// open are the documents it opened that are not closed yet.
	open map[*document.Document]bool
	// pending is how many files are still being read; failed is how many could not be opened.
	pending, failed int
	// tracked is how many documents it has opened and shown.
	tracked  int
	done     func(LaunchOutcome)
	finished bool
}

// initLaunch connects the shell to the launcher of the process and follows the documents that close.
func (s *shellState) initLaunch(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	win.launcher = s.launch
	s.launches.stopWatch = win.Documents.WatchClose(func(doc *document.Document) {
		binding.Post(func() {
			if s.Mounted() {
				s.launchDocClosed(doc)
			}
		})
	})
	// the command line of this process is handled as any other: the folder and the files were opened with the window
	if st := win.takeStartup(); st != nil {
		s.launchExtras(*st)
	}
	win.markReady()
}

// disposeLaunch ends the waits (a window that closes counts as its files being closed) and lets go of the launcher.
func (s *shellState) disposeLaunch() {
	l := &s.launches
	if l.stopWatch != nil {
		l.stopWatch()
		l.stopWatch = nil
	}
	for _, w := range slices.Clone(l.waits) {
		s.finishWait(w, LaunchOutcome{})
	}
	if win := s.win; win != nil {
		win.launcher = nil
		// the last window to go ends the launcher: a launch that comes after it has nobody to open in
		if win.Process.Windows.unregister(win) {
			if lc := win.Process.Launcher; lc != nil {
				lc.Close()
			}
		}
	}
}

// takeStartup gives the shell the command line of the first instance, once.
func (w *Window) takeStartup() *ipc.OpenArgs {
	st := w.startup
	w.startup = nil
	return st
}

// launchExtras shows the notices for the parts of a command line that come with later steps. It reports whether the
// launch is one that opens nothing else (--diff and --merge).
func (s *shellState) launchExtras(args ipc.OpenArgs) (opensNothing bool) {
	notify := PeekWindow(s.Context()).Notify
	if win := PeekWindow(s.Context()); args.Profile != "" && !win.Process.Windows.usesProfile(win, args.Profile) {
		// the windows are picked by the profile (Windows.HandleLaunch), so this is a folder that is open with
		// another profile, or a --reuse-window: the window keeps its profile and says so
		if win.Folder != "" && sameFolder(win.Folder, args.Folder) {
			notify.Info("Emetos", fmt.Sprintf(ProfileFolderOpenNotice, win.ProfileName()))
		} else {
			notify.Info("Emetos", fmt.Sprintf(ProfileWindowNotice, win.ProfileName()))
		}
	}
	switch {
	case len(args.Merge) > 0:
		// TODO(step 9.7): open the merge editor
		notify.Info("Emetos", MergeUnavailableNotice)
		return true
	case len(args.Diff) > 0:
		// TODO(step 9.6): open the diff editor
		notify.Info("Emetos", DiffUnavailableNotice)
		return true
	}
	return false
}

// launch is the shell's part of Window.HandleLaunch.
func (s *shellState) launch(args ipc.OpenArgs, done func(LaunchOutcome)) (cancel func()) {
	win := PeekWindow(s.Context())
	win.Raise()
	if s.launchExtras(args) {
		msg := DiffUnavailableNotice
		if len(args.Merge) > 0 {
			msg = MergeUnavailableNotice
		}
		done(LaunchOutcome{ExitCode: 1, Message: msg})
		return func() {}
	}
	w := &launchWait{done: done, open: map[*document.Document]bool{}}
	if args.Wait {
		s.launches.waits = append(s.launches.waits, w)
	} else {
		// the client does not wait: it is told now, whatever the questions about unsaved changes take
		done(LaunchOutcome{})
	}
	openFiles := func() {
		s.openLaunchFiles(args.Files, w, func() {
			if args.Wait {
				s.checkWait(w)
			}
		})
	}
	if args.Folder != "" && !sameFolder(win.Folder, args.Folder) {
		// the files come after the folder: opening the folder closes the editors
		s.openFolderThen(args.Folder, func(bool) { openFiles() })
	} else {
		openFiles()
	}
	return func() { s.finishWait(w, LaunchOutcome{}) }
}

// sameFolder tells whether two paths name the same folder.
func sameFolder(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// opened is a file read for a launch.
type opened struct {
	target ipc.FileTarget
	doc    *document.Document
	err    error
}

// openLaunchFiles reads the files in the background, in order, and shows them in the active group when all are
// read, the first one active, each with the cursor at its position. A file that cannot be opened is reported
// (a binary file with its notice). The documents are tracked by w. then is called once the files are shown.
func (s *shellState) openLaunchFiles(files []ipc.FileTarget, w *launchWait, then func()) {
	if len(files) == 0 {
		then()
		return
	}
	win := PeekWindow(s.Context())
	binding := s.binding
	w.pending += len(files)
	s.Go(func(life context.Context) {
		results := make([]opened, 0, len(files))
		for _, t := range files {
			doc, err := win.Documents.Acquire(life, t.Path, document.NewOptions())
			results = append(results, opened{t, doc, err})
		}
		binding.Post(func() {
			if !s.Mounted() {
				for _, r := range results {
					if r.err == nil {
						win.Documents.Release(r.doc)
					}
				}
				return
			}
			var shown []*document.Document
			for _, r := range results {
				w.pending--
				if r.err != nil {
					w.failed++
					if isBinaryError(r.err) {
						s.blockFile(r.target.Path)
					} else {
						win.Notify.Error("Emetos", "Cannot open "+r.target.Path+": "+describe(r.err))
					}
					continue
				}
				w.open[r.doc] = true
				w.tracked++
				shown = append(shown, r.doc)
				s.show(binding, r.doc, openOptions{Place: placeAt(r.doc, r.target.Line, r.target.Column)})
			}
			// the first file is the one that is shown
			if len(shown) > 1 {
				if first := s.inputFor(s.activeGroup(), shown[0]); first != nil {
					s.activeGroup().Model.SetActive(first)
				}
			}
			then()
		})
	})
}

// placeAt gives the function that puts the cursor of the editor of doc at a line and column counted from 1; nil
// when line is 0.
func placeAt(doc *document.Document, line, column int) func(*textEditor) {
	if line <= 0 {
		return nil
	}
	return func(e *textEditor) {
		model := edit.NewModel(doc.Snapshot())
		at := model.ValidatePosition(buffer.Position{Line: line - 1, Col: max(column-1, 0)})
		e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(at)})
	}
}

// launchDocClosed is told that a document was closed: a launch that waits for it may be over.
func (s *shellState) launchDocClosed(doc *document.Document) {
	for _, w := range slices.Clone(s.launches.waits) {
		if w.open[doc] {
			delete(w.open, doc)
			s.checkWait(w)
		}
	}
}

// checkWait ends a wait whose files are all read and closed: with exit code 0, or 1 when files were asked for and
// none of them could be opened.
func (s *shellState) checkWait(w *launchWait) {
	if w.pending > 0 || len(w.open) > 0 || w.finished {
		return
	}
	if w.failed > 0 && w.tracked == 0 {
		s.finishWait(w, LaunchOutcome{ExitCode: 1, Message: "none of the files could be opened"})
		return
	}
	s.finishWait(w, LaunchOutcome{})
}

// finishWait ends a wait once.
func (s *shellState) finishWait(w *launchWait, o LaunchOutcome) {
	if w.finished {
		return
	}
	w.finished = true
	s.launches.waits = slices.DeleteFunc(s.launches.waits, func(x *launchWait) bool { return x == w })
	w.done(o)
}
