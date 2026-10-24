package problems

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// ResourceMarkers are the markers of one file that the filter lets through, as the Problems panel
// shows them.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersModel.ts (ResourceMarkers)
type ResourceMarkers struct {
	// Resource is the absolute path of the file.
	Resource string
	// Name is the name of the file.
	Name string
	// Dir is the folder of the file as the panel shows it: relative to the folder of the window
	// when the file is inside it, else absolute; empty for a file in the folder itself.
	Dir string
	// Markers are the markers that are shown, errors first and each severity by position.
	Markers []Marker
	// Total is how many markers the file has in all.
	Total int
}

// Model is the Problems panel's view of a Service: the markers grouped by file, narrowed by a
// filter, sorted the way the panel sorts them. It is used on the UI thread only; the Service tells
// it about changes through the poster it was made with.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersModel.ts (MarkersModel)
type Model struct {
	svc  *Service
	post func(func())
	stop func()

	pendingMu sync.Mutex
	pending   bool

	root      string
	opts      FilterOptions
	parsed    *filter
	resources []ResourceMarkers
	total     int
	filtered  int
	listeners []*func()
}

// NewModel makes a model of the markers of svc, with the default filter. post hands a function to
// the UI thread (the binding's Post); changes of the service are read there, many in one go.
func NewModel(svc *Service, post func(func())) *Model {
	m := &Model{svc: svc, post: post, opts: DefaultFilter()}
	m.parsed = parseFilter(m.opts)
	m.stop = svc.Subscribe(func(Change) { m.schedule() })
	m.Refresh()
	return m
}

// Close stops following the service.
func (m *Model) Close() { m.stop() }

// schedule asks for a refresh on the UI thread, once for any number of changes before it runs.
func (m *Model) schedule() {
	m.pendingMu.Lock()
	if m.pending {
		m.pendingMu.Unlock()
		return
	}
	m.pending = true
	m.pendingMu.Unlock()
	m.post(func() {
		m.pendingMu.Lock()
		m.pending = false
		m.pendingMu.Unlock()
		m.Refresh()
	})
}

// Service is the service the model shows.
func (m *Model) Service() *Service { return m.svc }

// Root is the folder of the window, which the folders of the files are shown relative to.
func (m *Model) Root() string { return m.root }

// SetRoot sets the folder of the window.
func (m *Model) SetRoot(root string) {
	if root == m.root {
		return
	}
	m.root = root
	m.Refresh()
}

// Options are the options of the filter.
func (m *Model) Options() FilterOptions { return m.opts }

// SetOptions changes the filter.
func (m *Model) SetOptions(opts FilterOptions) {
	m.opts = opts
	m.parsed = parseFilter(opts)
	m.Refresh()
}

// SetText changes the text of the filter.
func (m *Model) SetText(text string) {
	if text == m.opts.Text {
		return
	}
	opts := m.opts
	opts.Text = text
	m.SetOptions(opts)
}

// ToggleSeverity shows the markers of a severity, or hides them.
func (m *Model) ToggleSeverity(s Severity) {
	opts := m.opts
	opts.Severities ^= Severities(s)
	m.SetOptions(opts)
}

// SetActiveFile tells the model which file the active editor shows ("" for none).
func (m *Model) SetActiveFile(path string) {
	if path == m.opts.ActiveFile {
		return
	}
	opts := m.opts
	opts.ActiveFile = path
	m.SetOptions(opts)
}

// ToggleActiveFileOnly shows the markers of the active file only, or those of every file.
func (m *Model) ToggleActiveFileOnly() {
	opts := m.opts
	opts.ActiveFileOnly = !opts.ActiveFileOnly
	m.SetOptions(opts)
}

// ClearFilters resets the text, the severities and the active file switch.
func (m *Model) ClearFilters() {
	opts := DefaultFilter()
	opts.ActiveFile, opts.Exclude = m.opts.ActiveFile, m.opts.Exclude
	m.SetOptions(opts)
}

// Filtering tells whether the filter narrows anything: it has text, hides a severity or shows the
// active file only.
func (m *Model) Filtering() bool {
	return strings.TrimSpace(m.opts.Text) != "" || m.opts.Severities != All || m.opts.ActiveFileOnly
}

// Resources are the files with markers that the filter lets through, in the order the panel shows
// them: files whose first marker is more serious come first, then by path.
func (m *Model) Resources() []ResourceMarkers { return m.resources }

// Resource gives the markers of a file that are shown.
func (m *Model) Resource(path string) (ResourceMarkers, bool) {
	for _, r := range m.resources {
		if r.Resource == path {
			return r, true
		}
	}
	return ResourceMarkers{}, false
}

// Total is how many markers there are in all (errors, warnings and infos).
func (m *Model) Total() int { return m.total }

// Filtered is how many markers the filter lets through.
func (m *Model) Filtered() int { return m.filtered }

// Stats counts the markers the filter lets through by severity.
func (m *Model) Stats() Stats {
	var s Stats
	for _, r := range m.resources {
		for _, mk := range r.Markers {
			s.add(mk)
		}
	}
	return s
}

// OnChange calls fn after the markers or the filter changed, and returns the function that stops
// it.
func (m *Model) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	m.listeners = append(m.listeners, p)
	return func() {
		if i := slices.Index(m.listeners, p); i >= 0 {
			m.listeners = slices.Delete(m.listeners, i, i+1)
		}
	}
}

// Refresh reads the service again and applies the filter.
func (m *Model) Refresh() {
	all := m.svc.Read(ReadFilter{Severities: All})
	byResource := map[string][]Marker{}
	var order []string
	for _, mk := range all {
		if _, ok := byResource[mk.Resource]; !ok {
			order = append(order, mk.Resource)
		}
		byResource[mk.Resource] = append(byResource[mk.Resource], mk)
	}
	m.total, m.filtered = len(all), 0
	m.resources = m.resources[:0:0]
	for _, path := range order {
		markers := byResource[path]
		verdict := m.parsed.resource(path)
		if verdict == verdictHide {
			continue
		}
		var shown []Marker
		for _, mk := range markers {
			if m.parsed.marker(mk, verdict) {
				shown = append(shown, mk)
			}
		}
		if len(shown) == 0 {
			continue
		}
		m.filtered += len(shown)
		m.resources = append(m.resources, ResourceMarkers{
			Resource: path, Name: filepath.Base(path), Dir: m.relativeDir(path), Markers: shown, Total: len(markers),
		})
	}
	slices.SortStableFunc(m.resources, compareResources)
	for _, f := range slices.Clone(m.listeners) {
		(*f)()
	}
}

// relativeDir is the folder of a file as the panel shows it.
func (m *Model) relativeDir(path string) string {
	dir := filepath.Dir(path)
	if m.root != "" {
		if rel, err := filepath.Rel(m.root, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return ""
			}
			return rel
		}
	}
	return dir
}

// compareResources orders files: the one whose first marker is more serious first, then by path
// and name.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersModel.ts (compareResourceMarkers)
func compareResources(a, b ResourceMarkers) int {
	if len(a.Markers) > 0 && len(b.Markers) > 0 && a.Markers[0].Severity != b.Markers[0].Severity {
		return int(b.Markers[0].Severity) - int(a.Markers[0].Severity)
	}
	if c := strings.Compare(a.Resource, b.Resource); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}
