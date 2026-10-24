package problems

import (
	"slices"
	"sort"
	"sync"
)

// Severity is how serious a marker is. The values are VS Code's MarkerSeverity, so they order by
// seriousness.
//
// Ported from VS Code: src/vs/platform/markers/common/markers.ts (MarkerSeverity)
type Severity int

// The severities of a marker.
const (
	// SeverityHint is a suggestion; the Problems panel does not show hints.
	SeverityHint Severity = 1
	// SeverityInfo is information.
	SeverityInfo Severity = 2
	// SeverityWarning is a warning.
	SeverityWarning Severity = 4
	// SeverityError is an error.
	SeverityError Severity = 8
)

// String is the name of the severity as VS Code shows it: "Error", "Warning" or "Info"; a hint has
// none.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "Error"
	case SeverityWarning:
		return "Warning"
	case SeverityInfo:
		return "Info"
	}
	return ""
}

// ParseSeverity reads the name of a severity: "error", "warning", "info" or "hint".
func ParseSeverity(name string) (Severity, bool) {
	switch name {
	case "error":
		return SeverityError, true
	case "warning":
		return SeverityWarning, true
	case "info":
		return SeverityInfo, true
	case "hint":
		return SeverityHint, true
	}
	return 0, false
}

// Severities is a set of severities, as a bit mask of their values.
type Severities int

// All lists the severities the Problems panel shows.
const All = Severities(SeverityError | SeverityWarning | SeverityInfo)

// Has tells whether the set holds a severity.
func (s Severities) Has(sev Severity) bool { return int(s)&int(sev) != 0 }

// RelatedInformation is a place that is part of a problem, such as the other declaration of a name
// that is declared twice.
type RelatedInformation struct {
	// Resource is the absolute path of the file.
	Resource string
	// Message says why the place is related.
	Message string
	// StartLine, StartColumn, EndLine and EndColumn are the range, counted from 1.
	StartLine, StartColumn, EndLine, EndColumn int
}

// Marker is a problem found in a file: an error, a warning or information about a range of it. It
// is what producers (language servers, tasks) give the Service and the Problems panel shows.
//
// The lines and columns count from 1, as the panel shows them ("[Ln 3, Col 5]"); a column counts
// UTF-16 units, as VS Code's and the language server protocol's do.
//
// Ported from VS Code: src/vs/platform/markers/common/markers.ts (IMarkerData, IMarker)
type Marker struct {
	// Owner is who set the marker; the Service fills it in.
	Owner string
	// Resource is the absolute path of the file; the Service fills it in.
	Resource string
	// Severity is how serious the problem is.
	Severity Severity
	// Code is the code of the problem ("S1234"); may be empty.
	Code string
	// Source is the tool that found the problem ("eslint"); may be empty.
	Source string
	// Message describes the problem. A marker with no message is not kept.
	Message string
	// StartLine, StartColumn, EndLine and EndColumn are the range, counted from 1.
	StartLine, StartColumn, EndLine, EndColumn int
	// Related are the places that are part of the problem.
	Related []RelatedInformation
}

// sanitize makes the range of a marker valid, as the marker service of VS Code does: positions
// count from 1 and the end is not before the start.
func (m Marker) sanitize() Marker {
	if m.StartLine < 1 {
		m.StartLine = 1
	}
	if m.StartColumn < 1 {
		m.StartColumn = 1
	}
	if m.EndLine < m.StartLine {
		m.EndLine = m.StartLine
	}
	if m.EndColumn < 1 {
		m.EndColumn = m.StartColumn
	}
	return m
}

// compareMarkers orders markers by seriousness (errors first), then by resource and position.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersModel.ts (ResourceMarkers._compareMarkers)
func compareMarkers(a, b Marker) int {
	switch {
	case a.Severity != b.Severity:
		return int(b.Severity) - int(a.Severity)
	case a.Resource != b.Resource:
		if a.Resource < b.Resource {
			return -1
		}
		return 1
	case a.StartLine != b.StartLine:
		return a.StartLine - b.StartLine
	case a.StartColumn != b.StartColumn:
		return a.StartColumn - b.StartColumn
	case a.EndLine != b.EndLine:
		return a.EndLine - b.EndLine
	}
	return a.EndColumn - b.EndColumn
}

// Stats counts markers by severity.
//
// Ported from VS Code: src/vs/platform/markers/common/markers.ts (MarkerStatistics)
type Stats struct {
	Errors, Warnings, Infos, Unknowns int
}

// Total is the number of errors, warnings and infos: what the badge of the Problems panel shows.
func (s Stats) Total() int { return s.Errors + s.Warnings + s.Infos }

func (s *Stats) add(m Marker) {
	switch m.Severity {
	case SeverityError:
		s.Errors++
	case SeverityWarning:
		s.Warnings++
	case SeverityInfo:
		s.Infos++
	default:
		s.Unknowns++
	}
}

// ReadFilter says which markers Service.Read gives.
type ReadFilter struct {
	// Owner, when set, limits the markers to those of an owner.
	Owner string
	// Resource, when set, limits them to those of a file.
	Resource string
	// Severities limits them to some severities; the zero value is every severity.
	Severities Severities
	// Take, when positive, is the most markers to give.
	Take int
}

// Change tells which resources a change of the Service touched.
type Change struct {
	// Resources are the absolute paths whose markers are not what they were, in the order they were
	// first touched.
	Resources []string
}

// Service holds the markers that producers set: per owner and per resource, replaced as a whole. It
// is safe for use from any goroutine; subscribers are called on the goroutine that changed it
// (after the change, with no lock held), so a UI subscriber hands the change to the UI thread
// itself.
//
// Ported from VS Code: src/vs/platform/markers/common/markerService.ts
type Service struct {
	mu sync.Mutex
	// data is the markers by resource and then by owner.
	data    map[string]map[string][]Marker
	subs    map[int]func(Change)
	nextSub int
}

// NewService makes a Service with no markers.
func NewService() *Service {
	return &Service{data: map[string]map[string][]Marker{}, subs: map[int]func(Change){}}
}

// Set replaces the markers an owner has for a resource, a path (relative paths are the producer's
// to resolve). An empty list removes them. A marker with no message is dropped and the range of a
// marker is made valid. The owner and resource fields of the markers are filled in.
func (s *Service) Set(owner, resource string, markers []Marker) {
	kept := make([]Marker, 0, len(markers))
	for _, m := range markers {
		if m.Message == "" {
			continue
		}
		m = m.sanitize()
		m.Owner, m.Resource = owner, resource
		kept = append(kept, m)
	}
	s.mu.Lock()
	changed := false
	byOwner := s.data[resource]
	if len(kept) == 0 {
		if _, had := byOwner[owner]; had {
			delete(byOwner, owner)
			if len(byOwner) == 0 {
				delete(s.data, resource)
			}
			changed = true
		}
	} else {
		if byOwner == nil {
			byOwner = map[string][]Marker{}
			s.data[resource] = byOwner
		}
		byOwner[owner] = kept
		changed = true
	}
	subs := s.subscribers()
	s.mu.Unlock()
	if changed {
		notify(subs, Change{Resources: []string{resource}})
	}
}

// Remove removes the markers of an owner: those of the given resources, or all of them when no
// resource is given.
func (s *Service) Remove(owner string, resources ...string) {
	s.mu.Lock()
	var touched []string
	targets := resources
	if len(targets) == 0 {
		for resource, byOwner := range s.data {
			if _, has := byOwner[owner]; has {
				targets = append(targets, resource)
			}
		}
		sort.Strings(targets)
	}
	for _, resource := range targets {
		byOwner := s.data[resource]
		if _, has := byOwner[owner]; !has {
			continue
		}
		delete(byOwner, owner)
		if len(byOwner) == 0 {
			delete(s.data, resource)
		}
		touched = append(touched, resource)
	}
	subs := s.subscribers()
	s.mu.Unlock()
	if len(touched) > 0 {
		notify(subs, Change{Resources: touched})
	}
}

// Read gives the markers that the filter accepts, sorted by seriousness and then by resource and
// position.
func (s *Service) Read(filter ReadFilter) []Marker {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Marker
	accept := func(m Marker) bool {
		return filter.Severities == 0 || filter.Severities.Has(m.Severity)
	}
	collect := func(byOwner map[string][]Marker) {
		for owner, markers := range byOwner {
			if filter.Owner != "" && owner != filter.Owner {
				continue
			}
			for _, m := range markers {
				if accept(m) {
					out = append(out, m)
				}
			}
		}
	}
	if filter.Resource != "" {
		collect(s.data[filter.Resource])
	} else {
		for _, byOwner := range s.data {
			collect(byOwner)
		}
	}
	slices.SortStableFunc(out, compareMarkers)
	if filter.Take > 0 && len(out) > filter.Take {
		out = out[:filter.Take]
	}
	return out
}

// Stats counts the markers of the service by severity.
func (s *Service) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st Stats
	for _, byOwner := range s.data {
		for _, markers := range byOwner {
			for _, m := range markers {
				st.add(m)
			}
		}
	}
	return st
}

// Subscribe calls fn after every change of the markers and returns the function that stops it. fn
// must not call Subscribe or the returned function from inside the call.
func (s *Service) Subscribe(fn func(Change)) (unsubscribe func()) {
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

// subscribers lists the subscribers in the order they subscribed; s.mu is held.
func (s *Service) subscribers() []func(Change) {
	fns := make([]func(Change), 0, len(s.subs))
	for id := 0; id < s.nextSub; id++ {
		if fn, ok := s.subs[id]; ok {
			fns = append(fns, fn)
		}
	}
	return fns
}

func notify(subs []func(Change), c Change) {
	for _, fn := range subs {
		fn(c)
	}
}
