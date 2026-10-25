package app

import (
	"context"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/views/output"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// panelsState is what the shell keeps of the panels (SPEC §10.13): the views of the Problems and the
// Output, which live as long as the window is shown, whichever tab of the panel is open.
type panelsState struct {
	problems *problems.View
	output   *output.View
	// activeFile is the file of the active editor that the Problems were last told of.
	activeFile string
	// stops end what the panels follow; removeCommands take their handlers off the registry.
	stops          []func()
	removeCommands []func()
}

// initPanels makes the views of the panels, follows the markers (the badge on the tab of the Problems)
// and the settings of the Output, and binds the commands of the panels.
func (s *shellState) initPanels(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	p := &s.panels
	root := win.Folder
	if abs, err := filepath.Abs(root); err == nil && root != "" {
		root = abs
	}
	p.problems = problems.NewView(win.Markers, problems.ViewOpts{
		Post: binding.Post, Root: root, Keys: win.Keys, Logger: win.Process.Logger,
		OnOpen: func(m problems.Marker, pinned bool) { s.openMarker(binding, m, pinned) },
	})
	p.output = output.NewView(output.ViewOpts{
		Store: win.Process.Channels, Post: binding.Post, Keys: win.Keys, Logger: win.Process.Logger,
		AfterFrame: func(f func()) {
			scheduler := binding.Scheduler()
			scheduler.ScheduleFrame()
			scheduler.AddPostFrameCallback(func(time.Duration) {
				if s.Mounted() {
					f()
				}
			})
		},
		Options: func() editor.Options {
			desktop, _ := binding.PlatformDispatcher().MonospaceFont()
			return editor.OptionsFromSettings(win.Settings, document.DefaultLanguage, desktop, document.DefaultTabSize)
		},
		SmartScroll: func() bool { return settings.Get[bool](win.Settings, "output.smartScroll.enabled", "") },
		LogFile: func() string {
			if win.Process.Logs == nil {
				return ""
			}
			return win.Process.Logs.Path()
		},
	})
	win.Problems, win.Output = p.problems, p.output
	p.stops = append(p.stops,
		// the badge of the tab follows the markers
		win.Markers.Subscribe(func(problems.Change) {
			binding.Post(func() {
				if s.Mounted() {
					s.SetState(func() {})
				}
			})
		}),
		win.Views.OnChange(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		}),
	)
	s.bindPanelCommands()
	s.bindDevMarkers()
}

// disposePanels ends the views of the panels and gives the commands back.
func (s *shellState) disposePanels() {
	p := &s.panels
	for _, stop := range p.stops {
		stop()
	}
	for _, remove := range p.removeCommands {
		remove()
	}
	win := s.win
	if p.problems != nil {
		p.problems.Dispose()
	}
	if p.output != nil {
		p.output.Dispose()
	}
	win.Problems, win.Output = nil, nil
	for _, key := range []string{"problemFocus", "problemsFilterFocus", "problems.filter.errors", "problems.filter.warnings",
		"problems.filter.info", "problems.filter.activeFile", "inOutput", "outputView.scrollLock", "activeOutputChannel", "focusedView"} {
		win.Keys.Reset(key)
	}
	*p = panelsState{}
}

// bindPanelCommands takes over the commands of the panels in the registry while the shell is on screen.
// The commands that open and focus the views are the workbench's.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markers.contribution.ts and
// src/vs/workbench/contrib/output/browser/output.contribution.ts
func (s *shellState) bindPanelCommands() {
	win := PeekWorkspace(s.Context())
	p := &s.panels
	run := func(f func()) command.Handler {
		return func(command.Context, any) error { f(); return nil }
	}
	model := p.problems.Model()
	handlers := map[string]command.Handler{
		"problems.action.focusFilter":                                         run(p.problems.FocusFilter),
		"problems.action.focusProblemsFromFilter":                             run(p.problems.FocusTree),
		"problems.action.clearFilterText":                                     run(p.problems.ClearFilterText),
		"workbench.actions.treeView.workbench.panel.markers.view.collapseAll": run(p.problems.CollapseAll),
		"workbench.actions.workbench.panel.markers.view.toggleErrors":         run(func() { model.ToggleSeverity(problems.SeverityError) }),
		"workbench.actions.workbench.panel.markers.view.toggleWarnings":       run(func() { model.ToggleSeverity(problems.SeverityWarning) }),
		"workbench.actions.workbench.panel.markers.view.toggleInfos":          run(func() { model.ToggleSeverity(problems.SeverityInfo) }),
		"workbench.actions.workbench.panel.markers.view.toggleActiveFile":     run(model.ToggleActiveFileOnly),
		"workbench.output.action.clearOutput":                                 run(p.output.Clear),
		"workbench.output.action.toggleAutoScroll":                            run(p.output.ToggleScrollLock),
		"workbench.action.openActiveLogOutputFile": func(c command.Context, _ any) error {
			path := p.output.LogFile()
			if path == "" {
				win.Notify.Info("Emetos", "The channel "+p.output.Active()+" has no file.")
				return nil
			}
			return s.devOpenFile(widgets.WidgetsBindingOf(s.Context()), c.Ctx, path)
		},
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		p.removeCommands = append(p.removeCommands, remove)
	}
}

// panelTabs are the tabs of the panel: its view containers in order, the Problems with the count of
// problems on it.
func (s *shellState) panelTabs(win *Workspace) []workbench.PanelTab {
	var tabs []workbench.PanelTab
	for _, c := range win.Process.Views.Containers(command.ViewLocationPanel) {
		tab := workbench.PanelTab{ID: c.ID, Title: c.Title}
		if c.ID == command.ViewContainerMarkers {
			if total := win.Markers.Stats().Total(); total > 0 {
				tab.Badge, tab.BadgeTooltip = total, "Total "+strconv.Itoa(total)+" Problems"
			}
		}
		tabs = append(tabs, tab)
	}
	return tabs
}

// panelBody is the body of the panel: the view of the tab that is open.
func (s *shellState) panelBody(win *Workspace) widgets.Widget {
	var body widgets.Widget
	var viewID string
	switch win.Views.ActivePanel() {
	case command.ViewContainerMarkers:
		body, viewID = problems.NewBody(s.panels.problems), command.ViewMarkers
	case command.ViewContainerOutput:
		body, viewID = output.NewBody(s.panels.output, output.BodyOpts{
			Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes, Context: win.ctx,
			Clipboard: win.Clipboard, Find: win.Find, Zoom: win.Zoom,
		}), command.ViewOutput
	default:
		return nil
	}
	// focusedView is the view that has the keyboard focus
	return widgets.NewFocus(body, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "PanelView",
		OnFocusChange: func(has bool) {
			if has {
				win.Keys.Set("focusedView", viewID)
			} else if win.Keys.Get("focusedView") == viewID {
				win.Keys.Reset("focusedView")
			}
		},
	})
}

// panelActions are the actions of the tab that is open, in the title strip of the panel.
func (s *shellState) panelActions(win *Workspace) widgets.Widget {
	switch win.Views.ActivePanel() {
	case command.ViewContainerMarkers:
		return problems.NewActions(s.panels.problems)
	case command.ViewContainerOutput:
		return output.NewActions(s.panels.output)
	}
	return nil
}

// focusPanelView gives the keyboard focus to the content of a tab of the panel.
func (s *shellState) focusPanelView(container string) {
	switch container {
	case command.ViewContainerMarkers:
		if len(s.panels.problems.Model().Resources()) > 0 {
			s.panels.problems.FocusTree()
		} else {
			s.panels.problems.FocusFilter()
		}
	case command.ViewContainerOutput:
		s.panels.output.Focus().Request()
	}
}

// syncActiveFile tells the Problems which file the active editor shows, for the filter that shows the
// problems of that file only. It is called while the shell builds, so the change is made after it.
func (s *shellState) syncActiveFile(binding *widgets.WidgetsBinding) {
	path := ""
	if active := s.activeEditor(); active != nil {
		if uri := active.doc.URI(); uri.Scheme == document.SchemeFile {
			path = uri.Path
		}
	}
	if path == s.panels.activeFile {
		return
	}
	s.panels.activeFile = path
	binding.Post(func() {
		if s.Mounted() && s.panels.problems != nil {
			s.panels.problems.SetActiveFile(path)
		}
	})
}

// openMarker shows the file of a problem with its range selected, in the active group: as a preview
// editor for a click, kept for a double click. The file is read in the background; a file that cannot be
// opened is reported.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersView.ts (openFileAtElement)
func (s *shellState) openMarker(binding *widgets.WidgetsBinding, m problems.Marker, pinned bool) {
	win := PeekWorkspace(s.Context())
	s.Go(func(life context.Context) {
		doc, err := win.Documents.Acquire(life, m.Resource, document.NewOptions())
		binding.Post(func() {
			if !s.Mounted() {
				if err == nil {
					win.Documents.Release(doc)
				}
				return
			}
			if err != nil {
				if isBinaryError(err) {
					s.blockFile(m.Resource)
					return
				}
				win.Notify.Error("Emetos", "Cannot open "+m.Resource+": "+describe(err))
				return
			}
			place := func(e *textEditor) {
				model := edit.NewModel(doc.Snapshot())
				start := model.ValidatePosition(markerPosition(doc, m.StartLine, m.StartColumn))
				end := model.ValidatePosition(markerPosition(doc, m.EndLine, m.EndColumn))
				e.view.Cursors().SetSelections([]buffer.Selection{{Anchor: start, Active: end}})
			}
			s.show(binding, doc, openOptions{Preview: !pinned, Place: place})
		})
	})
}

// markerPosition is the position of a line and a column of a marker (counted from 1, a column in
// UTF-16 units) as the byte position in the document.
func markerPosition(doc *document.Document, line, column int) buffer.Position {
	text, _ := doc.Snapshot().LineContent(max(line-1, 0))
	return buffer.Position{Line: max(line-1, 0), Col: byteColumn(text, column)}
}

// byteColumn is the byte offset in a line of the character at a UTF-16 column counted from 1; a column
// past the end of the line is the end of the line.
func byteColumn(line string, column int) int {
	units := 0
	for i, r := range line {
		if units >= column-1 {
			return i
		}
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return len(line)
}
