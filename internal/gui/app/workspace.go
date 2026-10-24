package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// watchState is what a window keeps of its watcher (SPEC §10.5).
type watchState struct {
	mu sync.Mutex
	// watcher is nil until StartWatching.
	watcher *workspace.Watcher
	// ctx is the context of the watcher.
	ctx context.Context
	// folder are the stops of the targets of the folder, restarted when the watcher settings change.
	folder []func()
	// files are the stops of the files that are watched one by one, by path; a nil stop is a file
	// whose target is being added.
	files map[string]func()
	// userSettings and workspaceSettings are the settings files, userDir the folder of the user's
	// configuration.
	userSettings, workspaceSettings, userDir string
}

// SnippetWorkspace is what snippets know of the folder of the window: WORKSPACE_NAME and
// WORKSPACE_FOLDER (SPEC §9.9). A window with no folder has neither.
func (w *Window) SnippetWorkspace() snippet.WorkspaceResolver {
	if w.Workspace == nil {
		return snippet.WorkspaceResolver{}
	}
	return snippet.WorkspaceResolver{Name: w.Workspace.Name(), Folder: w.Workspace.Path()}
}

// openWorkspace opens the folder of the window and makes the tree follow the settings.
func (w *Window) openWorkspace(ctx context.Context, folder string) error {
	cfg := workspace.ConfigFromSettings(w.Settings)
	ws, err := workspace.Open(ctx, folder, workspace.Options{Config: &cfg})
	if err != nil {
		return err
	}
	w.Workspace, w.Folder = ws, ws.Path()
	return nil
}

// followWorkspaceSettings applies the settings of the tree when they change, and restarts the
// watch of the folder when the settings of the watcher change. The change may come from any
// goroutine, and reading folders is slow, so the work runs on a goroutine of its own.
func (w *Window) followWorkspaceSettings() {
	w.Settings.OnChange(func(e settings.ChangeEvent) {
		tree := w.Workspace != nil && (e.Affects("explorer", "") || e.Affects("files.exclude", ""))
		watch := w.Workspace != nil && (e.Affects("files.watcherExclude", "") || e.Affects("files.watcherInclude", ""))
		if !tree && !watch {
			return
		}
		go func() {
			if tree && w.Workspace.SetConfig(workspace.ConfigFromSettings(w.Settings)) {
				if err := w.Workspace.Refresh(w.ctx, nil); err != nil {
					w.Process.Logger.Debug("refreshing the folder after a change of settings", "err", err)
				}
			}
			if watch {
				w.watchFolder()
			}
		}()
	})
}

// StartWatching starts the watcher of the window: the folder (recursively), the user's
// configuration folder (settings and snippets are read again when their files change), and the
// files that are open outside the folder. It stops with ctx. The targets are added on a goroutine
// (placing the watches reads the folders); the returned channel is closed when that is done.
// Failures of the watcher are logged, and the ones the user should hear of (no inotify, the
// limit of watches) become one warning. It does nothing unless the process was made with
// ProcessOptions.WatchFiles.
func (w *Window) StartWatching(ctx context.Context) (ready <-chan struct{}) {
	st := &w.watch
	st.mu.Lock()
	if st.watcher != nil || !w.Process.WatchFiles {
		st.mu.Unlock()
		done := make(chan struct{})
		close(done)
		return done
	}
	st.ctx = ctx
	st.watcher = workspace.NewWatcher(ctx, workspace.WatcherOptions{
		OnEvents: func(events []workspace.Event) { w.onFileEvents(ctx, events) },
		OnNotice: func(n workspace.Notice) { w.Process.Notify.Warn("Emetos", n.Message) },
		Logger:   w.Process.Logger,
	})
	// TODO(step 7.1): call w.Watcher().Suppress(path, stat) with the FileStat that saving a document returns.
	st.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.watchFolder()
		w.watchUserFolder()
		for _, doc := range w.Documents.All() {
			if path, ok := filePath(doc.URI().Scheme, doc.URI().Path); ok {
				w.followFile(path)
			}
		}
	}()
	return done
}

// Watcher is the watcher of the window, nil until StartWatching.
func (w *Window) Watcher() *workspace.Watcher {
	w.watch.mu.Lock()
	defer w.watch.mu.Unlock()
	return w.watch.watcher
}

// watchFolder (re)starts the targets of the folder: the folder itself and the paths of
// files.watcherInclude, with files.watcherExclude.
func (w *Window) watchFolder() {
	if w.Workspace == nil {
		return
	}
	st := &w.watch
	st.mu.Lock()
	watcher, ctx := st.watcher, st.ctx
	old := st.folder
	st.folder = nil
	st.mu.Unlock()
	if watcher == nil {
		return
	}
	for _, stop := range old {
		stop()
	}
	cfg := workspace.WatchSettingsFromSettings(w.Settings)
	folder := w.Workspace.Path()
	paths := []string{folder}
	for _, p := range cfg.Include {
		if !filepath.IsAbs(p) {
			p = filepath.Join(folder, p)
		} else if p = filepath.Clean(p); p != folder && !strings.HasPrefix(p, folder+string(filepath.Separator)) {
			continue // VS Code only takes the absolute paths that are in the folder
		}
		if !contains(paths, p) {
			paths = append(paths, p)
		}
	}
	var stops []func()
	for _, p := range paths {
		stop, err := watcher.Watch(ctx, workspace.Target{Path: p, Recursive: true, Exclude: cfg.Exclude})
		if err != nil {
			w.Process.Logger.Warn("cannot watch the folder", "path", p, "err", err)
			continue
		}
		stops = append(stops, stop)
	}
	st.mu.Lock()
	st.folder = stops
	st.mu.Unlock()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// watchUserFolder watches the folder of the user's configuration, where settings.json and the
// snippets are. The folder is made if it is not there, so that a settings.json written later is
// seen.
func (w *Window) watchUserFolder() {
	st := &w.watch
	st.mu.Lock()
	watcher, ctx, dir := st.watcher, st.ctx, st.userDir
	st.mu.Unlock()
	if watcher == nil || dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.Process.Logger.Warn("cannot make the configuration folder", "path", dir, "err", err)
		return
	}
	if _, err := watcher.Watch(ctx, workspace.Target{Path: dir, Recursive: true}); err != nil {
		w.Process.Logger.Warn("cannot watch the configuration folder", "path", dir, "err", err)
	}
}

// followFile watches a file that is open, if it is outside the folder of the window (the folder
// is watched as a whole).
func (w *Window) followFile(path string) {
	st := &w.watch
	path = filepath.Clean(path)
	if w.Workspace != nil && (path == w.Workspace.Path() || strings.HasPrefix(path, w.Workspace.Path()+string(filepath.Separator))) {
		return
	}
	st.mu.Lock()
	watcher, ctx := st.watcher, st.ctx
	if watcher == nil {
		st.mu.Unlock()
		return // StartWatching goes through the open documents
	}
	if st.files == nil {
		st.files = map[string]func(){}
	}
	if _, ok := st.files[path]; ok {
		st.mu.Unlock()
		return
	}
	st.files[path] = nil
	st.mu.Unlock()
	stop, err := watcher.Watch(ctx, workspace.Target{Path: path, File: true})
	st.mu.Lock()
	defer st.mu.Unlock()
	if err != nil {
		delete(st.files, path)
		w.Process.Logger.Debug("cannot watch an open file", "path", path, "err", err)
		return
	}
	if _, still := st.files[path]; !still { // closed meanwhile
		stop()
		return
	}
	st.files[path] = stop
}

// unfollowFile stops watching a file that was closed.
func (w *Window) unfollowFile(path string) {
	st := &w.watch
	path = filepath.Clean(path)
	st.mu.Lock()
	stop, ok := st.files[path]
	delete(st.files, path)
	st.mu.Unlock()
	if ok && stop != nil {
		stop()
	}
}

// filePath tells the path of a document that is a file.
func filePath(scheme, path string) (string, bool) {
	return path, scheme == "file" && path != ""
}

// onFileEvents handles a batch of events of the watcher, on the goroutine of the watcher: it
// reads the settings and the snippets again when their files changed, and updates the tree of
// the folder.
func (w *Window) onFileEvents(ctx context.Context, events []workspace.Event) {
	st := &w.watch
	var settingsChanged, snippetsChanged bool
	snippetsDir := ""
	if w.Process.Snippets != nil {
		snippetsDir = w.Process.Snippets.Dir()
	}
	touches := func(p, file string) bool { return file != "" && (p == file || isBelow(file, p)) }
	for _, e := range events {
		for _, p := range []string{e.Path, e.OldPath} {
			if p == "" {
				continue
			}
			// a rescan or a folder that was deleted or renamed covers the files below it
			if touches(p, st.userSettings) || touches(p, st.workspaceSettings) {
				settingsChanged = true
			}
			if snippetsDir != "" && (p == snippetsDir || isBelow(p, snippetsDir) || isBelow(snippetsDir, p)) {
				snippetsChanged = true
			}
		}
	}
	if settingsChanged {
		if _, err := w.Settings.Reload(ctx); err != nil {
			w.Process.Logger.Warn("reading the settings again failed", "err", err)
		}
	}
	if snippetsChanged {
		if err := w.Process.Snippets.Reload(ctx); err != nil {
			w.Process.Logger.Warn("reading the snippets again failed", "err", err)
		}
	}
	if w.Workspace != nil {
		w.Workspace.Apply(ctx, events)
	}
}

// isBelow tells whether path is inside dir.
func isBelow(path, dir string) bool {
	return len(path) > len(dir) && strings.HasPrefix(path, dir) && path[len(dir)] == filepath.Separator
}

// settingsPaths remembers the files the watcher reloads.
func (w *Window) rememberSettingsPaths(userSettings string, p *Process) {
	st := &w.watch
	st.userSettings = userSettings
	if w.Folder != "" {
		st.workspaceSettings = settings.WorkspaceSettingsPath(w.Folder)
	}
	if dir, err := p.Paths.ConfigDir(storage.DefaultProfile); err == nil {
		st.userDir = dir
	}
}
