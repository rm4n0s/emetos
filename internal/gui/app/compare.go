package app

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The ways to open a diff (SPEC §9.11, step 9.6): Compare with Saved, Compare Active File With..., Compare Active File
// with Clipboard, the Explorer's Select for Compare, Compare with Selected and Compare Selected, and the commands that
// switch the diff editor between its views.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileCommands.ts (COMPARE_WITH_SAVED_COMMAND_ID,
// SELECT_FOR_COMPARE_COMMAND_ID, COMPARE_RESOURCE_COMMAND_ID, COMPARE_SELECTED_COMMAND_ID),
// src/vs/workbench/contrib/files/browser/fileActions.ts (GlobalCompareResourcesAction, CompareWithClipboardAction) and
// src/vs/workbench/browser/parts/editor/diffEditorCommandsService.ts (toggleRenderSideBySide, setViewMode)

const (
	// SavedPrefix is the start of the path of the virtual documents that hold a file as it is on disk: the URI of
	// the file /home/me/main.go is emetos-virtual:/saved/home/me/main.go. They are the original side of "Compare
	// with Saved" and of the "Compare" of a conflict with the file.
	SavedPrefix = "/saved/"
	// ClipboardPrefix is the start of the path of the virtual documents that hold what the clipboard had when
	// "Compare Active File with Clipboard" ran: emetos-virtual:/clipboard/<n>/<name of the file>.
	ClipboardPrefix = "/clipboard/"
)

// savedURI is the URI of the virtual document that holds a file as it is on disk.
func savedURI(path string) document.URI {
	return document.VirtualURI(SavedPrefix + strings.TrimPrefix(filepath.ToSlash(path), "/"))
}

// savedPathOf is the file a saved virtual URI stands for; false for any other URI.
func savedPathOf(uri document.URI) (string, bool) {
	if uri.Scheme != document.SchemeVirtual || !strings.HasPrefix(uri.Path, SavedPrefix) || len(uri.Path) == len(SavedPrefix) {
		return "", false
	}
	return filepath.FromSlash("/" + strings.TrimPrefix(uri.Path, SavedPrefix)), true
}

// clipboardTexts holds the texts that clipboard comparisons were opened with, by number. It is safe for concurrent
// use: the provider of the documents asks for them from any goroutine.
type clipboardTexts struct {
	mu    sync.Mutex
	next  int
	texts map[int]string
}

// add keeps a text and gives its number.
func (c *clipboardTexts) add(text string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	if c.texts == nil {
		c.texts = map[int]string{}
	}
	c.texts[c.next] = text
	return c.next
}

// get is the text of a number.
func (c *clipboardTexts) get(n int) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	text, ok := c.texts[n]
	return text, ok
}

// registerCompareDocuments registers the providers of the virtual documents that comparisons use: the file as it
// is on disk (read each time the document is opened, and again when the file changes) and the text the clipboard
// had.
func (p *Process) registerCompareDocuments() {
	languages := p.Languages
	err := p.Virtual.RegisterPrefix(SavedPrefix, document.VirtualPrefixProvider{
		Label: func(rest string) string { return filepath.Base(rest) + " (in file)" },
		Language: func(rest string) string {
			return languages.Detect(filepath.FromSlash("/"+rest), "")
		},
		Text: func(ctx context.Context, rest string) (string, error) {
			res, err := fileio.Read(ctx, filepath.FromSlash("/"+rest), fileio.ReadOptions{})
			if err != nil {
				return "", err
			}
			return res.Text, nil
		},
	})
	if err != nil {
		// the prefix is fixed and the registry is new: this is a broken build
		panic(err)
	}
	p.clipboard = &clipboardTexts{}
	err = p.Virtual.RegisterPrefix(ClipboardPrefix, document.VirtualPrefixProvider{
		// the tab of the comparison is "Clipboard ↔ <file name>", as VS Code's label
		Label: func(string) string { return "Clipboard" },
		Language: func(rest string) string {
			_, name, _ := strings.Cut(rest, "/")
			return languages.Detect(name, "")
		},
		Text: func(_ context.Context, rest string) (string, error) {
			num, _, _ := strings.Cut(rest, "/")
			n, convErr := strconv.Atoi(num)
			if convErr != nil {
				return "", errors.NewErr("CompareClipboardNumberInvalid", convErr).SetMetadata("rest", rest)
			}
			text, ok := p.clipboard.get(n)
			if !ok {
				return "", errors.New("CompareClipboardGone", "the text of this comparison with the clipboard is not kept any more", "number", n)
			}
			return text, nil
		},
	})
	if err != nil {
		panic(err)
	}
}

// refreshSaved reads the file of a saved document again, in the background, and gives the document the text when
// it differs: the comparison with the saved file follows a save and a change of the file on disk. A file that cannot be
// read (it is gone) leaves the text as it was.
func (s *shellState) refreshSaved(path string) {
	win := PeekWindow(s.Context())
	uri := savedURI(path)
	doc, ok := win.Documents.Get(uri)
	if !ok {
		return
	}
	binding := s.binding
	s.Go(func(life context.Context) {
		content, err := win.Process.Virtual.Generate(life, uri)
		if err != nil {
			win.Process.Logger.Debug("the saved text of a comparison cannot be read", "path", path, "err", err)
			return
		}
		binding.Post(func() {
			if s.Mounted() && doc.Text() != content.Text {
				doc.SetText(content.Text)
			}
		})
	})
}

// compareState is what the shell keeps for the comparisons.
type compareState struct {
	// source is the file that Select for Compare chose: Compare with Selected compares the file it is run on with it.
	source string
}

// activeDocument is the document the active editor edits: a text editor's, or the modified side of a diff editor.
func (s *shellState) activeDocument() *document.Document {
	if s.group.grid == nil {
		return nil
	}
	if input := s.activeGroup().Model.ActiveEditor(); input != nil {
		return editorgroup.EditedDocument(input)
	}
	return nil
}

// compareCommands are the commands that open a diff, and the ones that choose the view of the diff editor.
func (s *shellState) compareCommands() map[string]command.Handler {
	binding := widgets.WidgetsBindingOf(s.Context())
	return map[string]command.Handler{
		"workbench.files.action.compareWithSaved":     func(_ command.Context, args any) error { return s.compareWithSaved(args) },
		"workbench.files.action.compareFileWith":      func(command.Context, any) error { s.compareFileWith(); return nil },
		"workbench.files.action.compareWithClipboard": func(command.Context, any) error { s.compareWithClipboard(binding); return nil },
		"selectForCompare":                            func(_ command.Context, args any) error { s.selectForCompare(args); return nil },
		"compareFiles":                                func(_ command.Context, args any) error { s.compareWithSelected(args); return nil },
		"compareSelected":                             func(command.Context, any) error { s.compareSelected(); return nil },

		"toggle.diff.renderSideBySide": func(command.Context, any) error {
			return s.writeSetting("diffEditor.renderSideBySide", !settings.Get[bool](PeekWindow(s.Context()).Settings, "diffEditor.renderSideBySide", ""))
		},
		"diffEditor.toggleUseInlineViewWhenSpaceIsLimited": func(command.Context, any) error {
			return s.writeSetting("diffEditor.useInlineViewWhenSpaceIsLimited", !settings.Get[bool](PeekWindow(s.Context()).Settings, "diffEditor.useInlineViewWhenSpaceIsLimited", ""))
		},
		"diffEditor.setViewMode.inline": func(command.Context, any) error {
			return s.writeSetting("diffEditor.renderSideBySide", false)
		},
		"diffEditor.setViewMode.sideBySide": func(command.Context, any) error {
			if err := s.writeSetting("diffEditor.renderSideBySide", true); err != nil {
				return err
			}
			return s.writeSetting("diffEditor.useInlineViewWhenSpaceIsLimited", false)
		},
		"diffEditor.setViewMode.automatic": func(command.Context, any) error {
			if err := s.writeSetting("diffEditor.renderSideBySide", true); err != nil {
				return err
			}
			return s.writeSetting("diffEditor.useInlineViewWhenSpaceIsLimited", true)
		},
	}
}

// writeSetting changes a setting where the user has it: in the workspace settings when the workspace sets it, else in
// the user's.
func (s *shellState) writeSetting(key string, value any) error {
	win := PeekWindow(s.Context())
	target := settings.TargetUser
	if win.Folder != "" && win.Settings.Inspect(key, "").Workspace.Ok {
		target = settings.TargetWorkspace
	}
	return win.Settings.Update(win.ctx, key, value, target)
}

// compareWithSaved opens the diff of a file as it is on disk (left) and as the window has it (right). The file is the
// one the argument names (a path or a file URI), else the active editor's.
func (s *shellState) compareWithSaved(args any) error {
	win := PeekWindow(s.Context())
	var live *document.Document
	switch v := args.(type) {
	case nil, map[string]any:
		// the active editor's file (a menu passes the context of what was clicked, which names no file)
	case string:
		if v != "" {
			doc, found := win.Documents.Get(document.FileURI(strings.TrimPrefix(v, "file://")))
			if !found {
				return nil // a file that no editor has open has nothing but its saved text
			}
			live = doc
		}
	default:
		return errors.New("CompareResourceInvalid", "compare with saved takes the path of a file", "arguments", args)
	}
	if live == nil {
		live = s.activeDocument()
	}
	if live == nil || live.URI().Scheme != document.SchemeFile {
		return nil // only files have a saved version
	}
	return s.openSavedDiff(live)
}

// openSavedDiff shows the file of a document as it is on disk against the document.
func (s *shellState) openSavedDiff(live *document.Document) error {
	win := PeekWindow(s.Context())
	saved, err := win.Documents.AcquireVirtual(win.ctx, savedURI(live.URI().Path))
	if err != nil {
		return err
	}
	win.Documents.Retain(live)
	s.openDiff(saved, live, nil)
	return nil
}

// compareFileWith asks for a file in the quick open and opens the diff of the active editor's file (left) and the one
// that was picked (right); the quick open starts with the second item active, as the first is the file that is open.
func (s *shellState) compareFileWith() {
	win := PeekWindow(s.Context())
	active := s.activeDocument()
	if active == nil {
		return
	}
	win.QuickInput.Show("", quickinput.ShowOptions{
		Activation: quickinput.ItemSecond,
		OnFile: func(req quickinput.FileOpen) {
			if s.Mounted() {
				s.compareDocumentWith(active, req.Path)
			}
		},
	})
}

// compareDocumentWith opens the diff of a document (left) and a file (right), reading the file in the background.
func (s *shellState) compareDocumentWith(left *document.Document, path string) {
	win := PeekWindow(s.Context())
	win.Documents.Retain(left)
	s.Go(func(life context.Context) {
		right, err := win.Documents.Acquire(life, path, document.NewOptions())
		s.binding.Post(func() {
			if !s.Mounted() {
				win.Documents.Release(left)
				if err == nil {
					win.Documents.Release(right)
				}
				return
			}
			if err != nil {
				win.Documents.Release(left)
				win.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
				return
			}
			s.openDiff(left, right, nil)
		})
	})
}

// compareWithClipboard opens the diff of what the clipboard holds (left) and the active file (right).
func (s *shellState) compareWithClipboard(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	live := s.activeDocument()
	if live == nil || live.URI().Scheme == document.SchemeVirtual {
		return
	}
	binding.Clipboard().GetData(win.ctx, services.ClipboardKTextPlain, func(data *services.ClipboardData, err error) {
		if err != nil {
			win.Notify.Error("Emetos", "Cannot read the clipboard: "+describe(err))
			return
		}
		text := ""
		if data != nil {
			text = data.Text
		}
		if !s.Mounted() {
			return
		}
		n := win.Process.clipboard.add(text)
		uri := document.VirtualURI(ClipboardPrefix + strconv.Itoa(n) + "/" + live.URI().Title())
		orig, err := win.Documents.AcquireVirtual(win.ctx, uri)
		if err != nil {
			win.Notify.Error("Emetos", "Cannot compare with the clipboard: "+describe(err))
			return
		}
		win.Documents.Retain(live)
		s.openDiff(orig, live, nil)
	})
}

// selectForCompare remembers the file the command is run on: the focused row of the Explorer, or the file the
// argument names, or the active editor's. Compare with Selected then compares another file with it.
func (s *shellState) selectForCompare(args any) {
	win := PeekWindow(s.Context())
	if path, ok := s.compareTarget(args); ok {
		s.compare.source = path
		win.Keys.Set("resourceSelectedForCompare", true)
	}
}

// compareTarget is the file a compare command works on: the focused row of the Explorer when it is a file, else the
// files the arguments and the active editor name (commandPaths).
func (s *shellState) compareTarget(args any) (string, bool) {
	if ex := &s.explorer; ex.view != nil {
		if items := ex.view.Context(false); len(items) > 0 && !items[0].IsDir() {
			return items[0].Path(), true
		}
	}
	for _, p := range s.commandPaths(args) {
		if !isDir(p) {
			return p, true
		}
	}
	return "", false
}

// compareWithSelected opens the diff of the file chosen with Select for Compare (left) and the file the command is run on
// (right).
func (s *shellState) compareWithSelected(args any) {
	right, ok := s.compareTarget(args)
	if !ok || s.compare.source == "" {
		return
	}
	s.compareFiles(s.compare.source, right)
}

// compareSelected opens the diff of the two files that are selected in the Explorer: the first (left) and the second
// (right). With anything else selected it does nothing.
func (s *shellState) compareSelected() {
	ex := &s.explorer
	if ex.view == nil {
		return
	}
	items := ex.view.Context(true)
	if len(items) != 2 || items[0].IsDir() || items[1].IsDir() {
		return
	}
	s.compareFiles(items[0].Path(), items[1].Path())
}

// compareFiles opens the diff of two files, reading them in the background.
func (s *shellState) compareFiles(left, right string) {
	win := PeekWindow(s.Context())
	s.Go(func(life context.Context) {
		a, errA := win.Documents.Acquire(life, left, document.NewOptions())
		var b *document.Document
		var errB error
		if errA == nil {
			b, errB = win.Documents.Acquire(life, right, document.NewOptions())
		}
		s.binding.Post(func() {
			release := func() {
				if errA == nil {
					win.Documents.Release(a)
				}
				if b != nil && errB == nil {
					win.Documents.Release(b)
				}
			}
			if !s.Mounted() {
				release()
				return
			}
			if errA != nil || errB != nil {
				release()
				path, err := left, errA
				if err == nil {
					path, err = right, errB
				}
				win.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
				return
			}
			s.openDiff(a, b, nil)
		})
	})
}
