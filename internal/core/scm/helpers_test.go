package scm

import "testing"

// resources makes resources from paths (the root of the file system is "/repo").
func resources(paths ...string) []Resource {
	out := make([]Resource, len(paths))
	for i, p := range paths {
		out[i] = Resource{URI: "file://" + p}
		out[i].prepare()
	}
	return out
}

// names lists the paths of resources.
func paths(rs []*Resource) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Path
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// newGroup makes a model with one source control and one group holding the resources.
func newGroup(t testing.TB, rs []Resource) (*Model, *Source, *Group) {
	t.Helper()
	m := NewModel(DefaultConfig())
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///repo", ContextValue: "repository", Owner: "emetos.gitscm"})
	g := s.AddGroup("workingTree", "Changes", false, "")
	g.SetResources(rs)
	return m, s, g
}
