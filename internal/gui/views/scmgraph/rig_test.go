package scmgraph

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/scm"
)

// epoch is the day of the first commit of the tests, and the clock of the panes is three days after the last commit.
var epoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// fakeHistory is a history provider over a fixed list of commits, newest first with the parents after the children: Items gives the
// page of the list (the references are not walked: the list is the history), and the filter keeps the commits with the text.
type fakeHistory struct {
	mu      sync.Mutex
	all     []scm.HistoryItem
	refs    []scm.HistoryRef
	base    string
	open    string
	queries []scm.HistoryQuery
	gate    chan struct{}
	fail    error
	asked   [][]string
}

func (f *fakeHistory) Items(ctx context.Context, q scm.HistoryQuery) ([]scm.HistoryItem, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	gate, fail := f.gate, f.fail
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if fail != nil {
		return nil, fail
	}
	list := f.all
	if q.Filter != "" {
		list = nil
		for _, it := range f.all {
			if strings.Contains(strings.ToLower(it.Subject+" "+it.Author), strings.ToLower(q.Filter)) || strings.HasPrefix(it.ID, q.Filter) {
				list = append(list, it)
			}
		}
	}
	if q.Skip >= len(list) {
		return nil, nil
	}
	return slices.Clone(list[q.Skip:min(q.Skip+q.Limit, len(list))]), nil
}

func (f *fakeHistory) Refs(_ context.Context, ids []string) ([]scm.HistoryRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ids == nil {
		return slices.Clone(f.refs), nil
	}
	var out []scm.HistoryRef
	for _, r := range f.refs {
		if slices.Contains(ids, r.ID) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeHistory) CommonAncestor(_ context.Context, refs []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, refs)
	return f.base, nil
}

func (f *fakeHistory) OpenCommand() string { return f.open }

func (f *fakeHistory) Queries() []scm.HistoryQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.queries)
}

// at is the time n hours after the epoch.
func at(n int) time.Time { return epoch.Add(time.Duration(n) * time.Hour) }

func ref(id, name, rev, category, icon string) scm.HistoryRef {
	return scm.HistoryRef{ID: id, Name: name, Revision: rev, Category: category, Icon: icon, Description: rev}
}

// branching is the history of the Git tests of the step, with a remote branch that is a commit ahead:
//
//	m  (main)        Merge topic
//	r1 (origin/main) Remote one (made on b1)
//	b2 (v1)          Main two
//	d  (topic)       Topic two
//	c                Topic one
//	b1               Main one
//	a                Add a
func branching() *fakeHistory {
	it := func(id, subject, author string, hours int, parents ...string) scm.HistoryItem {
		return scm.HistoryItem{ID: id, ParentIDs: parents, Subject: subject, Message: subject + "\n\nA longer message.", DisplayID: id,
			Author: author, Timestamp: at(hours), Stats: &scm.HistoryStats{Files: 2, Insertions: 3, Deletions: 1}}
	}
	main := ref("refs/heads/main", "main", "m", "branches", "git-branch")
	topic := ref("refs/heads/topic", "topic", "d", "branches", "git-branch")
	remote := ref("refs/remotes/origin/main", "origin/main", "r1", "remote branches", "cloud")
	tag := ref("refs/tags/v1", "v1", "b2", "tags", "tag")
	f := &fakeHistory{
		refs: []scm.HistoryRef{main, topic, remote, tag},
		base: "b1",
		open: "git.viewCommit",
	}
	f.all = []scm.HistoryItem{
		it("m", "Merge topic", "Ada Example", 60, "b2", "d"),
		it("r1", "Remote one", "Bo Example", 55, "b1"),
		it("b2", "Main two", "Ada Example", 50, "b1"),
		it("d", "Topic two", "Grace Hopper", 40, "c"),
		it("c", "Topic one", "Grace Hopper", 30, "b1"),
		it("b1", "Main one", "Ada Example", 20, "a"),
		it("a", "Add a", "Ada Example", 10),
	}
	f.all[0].Refs = []scm.HistoryRef{main}
	f.all[1].Refs = []scm.HistoryRef{remote}
	f.all[2].Refs = []scm.HistoryRef{tag}
	f.all[3].Refs = []scm.HistoryRef{topic}
	return f
}

// rig is a pane over a model with one repository that has a history provider, driven from the test goroutine: the answers of the
// providers are posted and run by settle, as the UI thread would.
type rig struct {
	t       *testing.T
	model   *scm.Model
	src     *scm.Source
	hist    *fakeHistory
	pane    *Pane
	mu      sync.Mutex
	posted  []func()
	visible bool
	atTop   bool
	pageSz  int
	onScrl  bool
	badges  string
	runs    []ran
	saved   []map[string]Filter
	after   []func()
	changes int
	ts      *gluttertest.Tester
}

type ran struct {
	ID   string
	Args any
}

// newRig makes a rig with a repository whose branch main is at m and tracks origin/main, which is at r1.
func newRig(t *testing.T, hist *fakeHistory) *rig {
	t.Helper()
	r := &rig{t: t, model: scm.NewModel(scm.DefaultConfig()), hist: hist, visible: true, atTop: true, pageSz: 50, onScrl: true, badges: "filter"}
	r.src = r.model.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///w/repo"})
	r.src.SetHistoryProvider(hist)
	refs := scm.HistoryRefs{Current: &hist.refs[0]}
	if len(hist.refs) > 2 {
		refs.Remote = &hist.refs[2]
	}
	r.src.SetHistoryRefs(refs)
	r.newPane(nil)
	return r
}

func (r *rig) newPane(filters map[string]Filter) {
	ctx, cancel := context.WithCancel(context.Background())
	r.t.Cleanup(cancel)
	r.pane = NewPane(PaneOpts{
		Ctx: ctx, Model: r.model, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Post: func(f func()) {
			r.mu.Lock()
			r.posted = append(r.posted, f)
			r.mu.Unlock()
		},
		Location:     time.UTC,
		Now:          func() time.Time { return at(60).Add(72 * time.Hour) },
		PageSize:     func() int { return r.pageSz },
		PageOnScroll: func() bool { return r.onScrl },
		Badges:       func() string { return r.badges },
		ShowIncoming: func() bool { return true },
		ShowOutgoing: func() bool { return true },
		Visible:      func() bool { return r.visible },
		AtTop:        func() bool { return r.atTop },
		Run:          func(id string, args any) { r.runs = append(r.runs, ran{id, args}) },
		Filters:      filters, FiltersChanged: func(m map[string]Filter) { r.saved = append(r.saved, m) },
		AfterFunc: func(d time.Duration, f func()) func() {
			i := len(r.after)
			r.after = append(r.after, f)
			return func() { r.after[i] = nil }
		},
	})
	r.pane.OnChange(func() { r.changes++ })
	r.t.Cleanup(r.pane.Dispose)
	r.pane.VisibilityChanged()
}

// run runs what the providers posted so far.
func (r *rig) run() {
	for {
		r.mu.Lock()
		list := r.posted
		r.posted = nil
		r.mu.Unlock()
		if len(list) == 0 {
			return
		}
		for _, f := range list {
			f()
		}
	}
}

// busy says that a page is being read.
func (r *rig) busy() bool {
	for _, st := range r.pane.states {
		if st.loading || st.loadingMore {
			return true
		}
	}
	return false
}

// settle runs the posted work until no page is being read.
func (r *rig) settle() {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.run()
		if !r.busy() {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatal("a page is still being read")
		}
		time.Sleep(time.Millisecond)
	}
}

// subjects are the subjects of the rows, the made-up rows by their names and the load-more row as "..." .
func (r *rig) subjects() []string {
	var out []string
	for _, row := range r.pane.Rows() {
		if row.Kind == RowLoadMore {
			out = append(out, "...")
			continue
		}
		out = append(out, row.Item.Subject)
	}
	return out
}

// historyArg reads the arguments of a command of the Graph as the Git extension does.
func historyArg(args any) (extension.HistoryArg, bool) { return extension.HistoryArgs(args) }
