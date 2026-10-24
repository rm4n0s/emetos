package problems

import (
	"reflect"
	"testing"
)

// now runs a function at once, as the UI thread of a test does.
func now(f func()) { f() }

func fixture() *Service {
	s := NewService()
	s.Set("go", "/p/main.go", []Marker{
		{Severity: SeverityWarning, Message: "unused variable x", Source: "compiler", Code: "W100", StartLine: 9, StartColumn: 2},
		{Severity: SeverityError, Message: "undefined: foo", Source: "compiler", Code: "E200", StartLine: 12, StartColumn: 5},
		{Severity: SeverityError, Message: "missing return", Source: "vet", StartLine: 3, StartColumn: 1},
	})
	s.Set("go", "/p/pkg/util.go", []Marker{{Severity: SeverityInfo, Message: "could be simplified", Source: "lint", StartLine: 4, StartColumn: 1}})
	s.Set("css", "/p/web/style.css", []Marker{{Severity: SeverityWarning, Message: "unknown property", Source: "css", StartLine: 1, StartColumn: 1}})
	return s
}

func resourcesOf(m *Model) []string {
	var out []string
	for _, r := range m.Resources() {
		out = append(out, r.Name)
	}
	return out
}

func shownMessages(m *Model) []string {
	var out []string
	for _, r := range m.Resources() {
		for _, mk := range r.Markers {
			out = append(out, mk.Message)
		}
	}
	return out
}

func TestModelGroupsAndSorts(t *testing.T) {
	m := NewModel(fixture(), now)
	defer m.Close()
	// the file whose first marker is the most serious comes first, then by path; a file's markers
	// go errors first, each severity by position
	if got, want := resourcesOf(m), []string{"main.go", "style.css", "util.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("resources = %v, want %v", got, want)
	}
	if got, want := shownMessages(m)[:3], []string{"missing return", "undefined: foo", "unused variable x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("markers of main.go = %v, want %v", got, want)
	}
	if m.Total() != 5 || m.Filtered() != 5 {
		t.Errorf("total/filtered = %d/%d, want 5/5", m.Total(), m.Filtered())
	}
	r, ok := m.Resource("/p/main.go")
	if !ok || r.Total != 3 || len(r.Markers) != 3 {
		t.Errorf("Resource(main.go) = %+v, %v", r, ok)
	}
	if _, ok := m.Resource("/nope"); ok {
		t.Error("Resource found a file with no markers")
	}
}

func TestModelRelativeDir(t *testing.T) {
	tests := []struct {
		name, root, path, want string
	}{
		{"inside the folder", "/p", "/p/pkg/util.go", "pkg"},
		{"in the folder itself", "/p", "/p/main.go", ""},
		{"outside the folder", "/p", "/q/x.go", "/q"},
		{"a sibling with the same prefix", "/p", "/pq/x.go", "/pq"},
		{"no folder", "", "/p/main.go", "/p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService()
			s.Set("o", tt.path, []Marker{mk(SeverityError, "m", 1, 1)})
			m := NewModel(s, now)
			defer m.Close()
			m.SetRoot(tt.root)
			if got := m.Resources()[0].Dir; got != tt.want {
				t.Errorf("Dir = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModelFilters(t *testing.T) {
	tests := []struct {
		name   string
		opts   func(o *FilterOptions)
		want   []string
		stats  Stats
		files  []string
		active bool
	}{
		{"no filter", func(*FilterOptions) {}, []string{"missing return", "undefined: foo", "unused variable x", "unknown property", "could be simplified"}, Stats{Errors: 2, Warnings: 2, Infos: 1}, []string{"main.go", "style.css", "util.go"}, false},
		{"errors hidden", func(o *FilterOptions) { o.Severities = Severities(SeverityWarning | SeverityInfo) }, []string{"unused variable x", "unknown property", "could be simplified"}, Stats{Warnings: 2, Infos: 1}, []string{"main.go", "style.css", "util.go"}, false},
		{"only infos", func(o *FilterOptions) { o.Severities = Severities(SeverityInfo) }, []string{"could be simplified"}, Stats{Infos: 1}, []string{"util.go"}, false},
		{"no severity shows nothing", func(o *FilterOptions) { o.Severities = 0 }, nil, Stats{}, nil, false},
		{"text matches the message", func(o *FilterOptions) { o.Text = "UNDEFINED" }, []string{"undefined: foo"}, Stats{Errors: 1}, []string{"main.go"}, false},
		{"text matches the source", func(o *FilterOptions) { o.Text = "vet" }, []string{"missing return"}, Stats{Errors: 1}, []string{"main.go"}, false},
		{"text matches the code", func(o *FilterOptions) { o.Text = "w100" }, []string{"unused variable x"}, Stats{Warnings: 1}, []string{"main.go"}, false},
		{"text matches the file name and shows all its markers", func(o *FilterOptions) { o.Text = "util" }, []string{"could be simplified"}, Stats{Infos: 1}, []string{"util.go"}, false},
		{"negated text hides what matches", func(o *FilterOptions) { o.Text = "!undefined" }, []string{"missing return", "unused variable x", "unknown property", "could be simplified"}, Stats{Errors: 1, Warnings: 2, Infos: 1}, []string{"main.go", "style.css", "util.go"}, false},
		{"a glob keeps the files", func(o *FilterOptions) { o.Text = "**/*.css" }, []string{"unknown property"}, Stats{Warnings: 1}, []string{"style.css"}, false},
		{"an extension keeps the files", func(o *FilterOptions) { o.Text = ".css" }, []string{"unknown property"}, Stats{Warnings: 1}, []string{"style.css"}, false},
		{"a folder keeps the files in it", func(o *FilterOptions) { o.Text = "pkg" }, []string{"could be simplified"}, Stats{Infos: 1}, []string{"util.go"}, false},
		{"a negated glob hides the files", func(o *FilterOptions) { o.Text = "!**/*.css" }, []string{"missing return", "undefined: foo", "unused variable x", "could be simplified"}, Stats{Errors: 2, Warnings: 1, Infos: 1}, []string{"main.go", "util.go"}, false},
		{"patterns separated by commas", func(o *FilterOptions) { o.Text = "*.css, pkg" }, []string{"unknown property", "could be simplified"}, Stats{Warnings: 1, Infos: 1}, []string{"style.css", "util.go"}, false},
		{"a pattern and a negated pattern", func(o *FilterOptions) { o.Text = "**/*.go,!pkg" }, []string{"missing return", "undefined: foo", "unused variable x"}, Stats{Errors: 2, Warnings: 1}, []string{"main.go"}, false},
		{"a source", func(o *FilterOptions) { o.Text = "@source:lint" }, []string{"could be simplified"}, Stats{Infos: 1}, []string{"util.go"}, false},
		{"a quoted source", func(o *FilterOptions) { o.Text = `@source:"compiler"` }, []string{"undefined: foo", "unused variable x"}, Stats{Errors: 1, Warnings: 1}, []string{"main.go"}, false},
		{"a source and text", func(o *FilterOptions) { o.Text = "@source:compiler unused" }, []string{"unused variable x"}, Stats{Warnings: 1}, []string{"main.go"}, false},
		{"an excluded source", func(o *FilterOptions) { o.Text = "!@source:compiler" }, []string{"missing return", "unknown property", "could be simplified"}, Stats{Errors: 1, Warnings: 1, Infos: 1}, []string{"main.go", "style.css", "util.go"}, false},
		{"the active file only", func(o *FilterOptions) { o.ActiveFileOnly, o.ActiveFile = true, "/p/pkg/util.go" }, []string{"could be simplified"}, Stats{Infos: 1}, []string{"util.go"}, true},
		{"the active file only with no active file", func(o *FilterOptions) { o.ActiveFileOnly = true }, nil, Stats{}, nil, true},
		{"excluded files", func(o *FilterOptions) {
			o.Exclude = func(p string) bool { return p == "/p/web/style.css" }
		}, []string{"missing return", "undefined: foo", "unused variable x", "could be simplified"}, Stats{Errors: 2, Warnings: 1, Infos: 1}, []string{"main.go", "util.go"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(fixture(), now)
			defer m.Close()
			opts := DefaultFilter()
			tt.opts(&opts)
			m.SetOptions(opts)
			if got := shownMessages(m); !reflect.DeepEqual(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("markers = %v, want %v", got, tt.want)
			}
			if got := m.Stats(); got != tt.stats {
				t.Errorf("stats = %+v, want %+v", got, tt.stats)
			}
			if got := resourcesOf(m); !reflect.DeepEqual(got, tt.files) && (len(got) != 0 || len(tt.files) != 0) {
				t.Errorf("files = %v, want %v", got, tt.files)
			}
			if m.Total() != 5 {
				t.Errorf("Total = %d, want 5 whatever the filter", m.Total())
			}
			if m.Filtered() != tt.stats.Total() {
				t.Errorf("Filtered = %d, want %d", m.Filtered(), tt.stats.Total())
			}
		})
	}
}

func TestModelToggles(t *testing.T) {
	m := NewModel(fixture(), now)
	defer m.Close()
	if m.Filtering() {
		t.Error("the default filter narrows nothing")
	}
	m.ToggleSeverity(SeverityError)
	if m.Options().Severities.Has(SeverityError) || !m.Filtering() {
		t.Errorf("errors still shown after the toggle: %v", m.Options().Severities)
	}
	m.ToggleSeverity(SeverityError)
	if !m.Options().Severities.Has(SeverityError) || m.Filtering() {
		t.Error("the second toggle did not show errors again")
	}
	m.SetActiveFile("/p/main.go")
	m.ToggleActiveFileOnly()
	if got := resourcesOf(m); !reflect.DeepEqual(got, []string{"main.go"}) {
		t.Errorf("active file only shows %v", got)
	}
	m.SetText("zzz")
	m.ClearFilters()
	if m.Filtering() || m.Options().ActiveFile != "/p/main.go" || len(m.Resources()) != 3 {
		t.Errorf("ClearFilters left %+v with %d files", m.Options(), len(m.Resources()))
	}
}

func TestModelFollowsTheServiceAndTellsListeners(t *testing.T) {
	var queued []func()
	post := func(f func()) { queued = append(queued, f) }
	s := NewService()
	m := NewModel(s, post)
	defer m.Close()
	changes := 0
	stop := m.OnChange(func() { changes++ })

	// many changes before the UI thread runs are one refresh
	s.Set("o", "/a", []Marker{mk(SeverityError, "a", 1, 1)})
	s.Set("o", "/b", []Marker{mk(SeverityWarning, "b", 1, 1)})
	s.Remove("o", "/b")
	if len(queued) != 1 || changes != 0 {
		t.Fatalf("queued %d refreshes and %d changes, want 1 and 0", len(queued), changes)
	}
	queued[0]()
	if changes != 1 || !reflect.DeepEqual(resourcesOf(m), []string{"a"}) {
		t.Errorf("after the refresh: %d changes, files %v", changes, resourcesOf(m))
	}

	// a change after the refresh queues the next one
	s.Remove("o")
	if len(queued) != 2 {
		t.Fatalf("queued %d refreshes, want 2", len(queued))
	}
	queued[1]()
	if changes != 2 || len(m.Resources()) != 0 {
		t.Errorf("after the second refresh: %d changes, %d files", changes, len(m.Resources()))
	}

	stop()
	m.SetText("x")
	if changes != 2 {
		t.Errorf("a stopped listener heard %d changes", changes-2)
	}
	m.Close()
	s.Set("o", "/c", []Marker{mk(SeverityError, "c", 1, 1)})
	if len(queued) != 2 {
		t.Error("a closed model still follows the service")
	}
}
