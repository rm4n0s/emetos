package gitscm

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The history of a repository for the Graph against real repositories (step 24.20): the provider's pages, references and common
// ancestors, the current references the Graph is drawn around, and the commands of its menus.

// branching is a repository with a merge, two branches, a tag, a remote that is a commit ahead and two behind, and a branch made from
// the remote's:
//
//   - m  (main) Merge topic
//     |\
//     | * d  (topic) Topic two
//     | * c  Topic one
//   - | b2 Main two     (tag v1)
//     |/
//   - b1 Main one
//   - a  Add a
type branching struct {
	env        *gittest.Env
	work, bare *gittest.Repo
	hashes     map[string]string
}

func newBranching(t *testing.T) *branching {
	t.Helper()
	e := gittest.NewEnv(t)
	w := e.Init("work")
	h := map[string]string{}
	w.Write("a.txt", "a\n")
	h["a"] = w.Commit("Add a")
	w.Write("b.txt", "b\n")
	h["b1"] = w.Commit("Main one")
	bare := w.AddRemote("origin")
	w.Git("push", "-q", "-u", "origin", "main")
	w.Branch("topic", "")
	w.Checkout("topic")
	w.Write("c.txt", "c\n")
	w.Git("add", "-A")
	h["c"] = w.CommitAs("Grace Hopper", "grace@example.com", "Topic one\n\nWith a body.")
	w.Write("d.txt", "d\nd2\n")
	h["d"] = w.Commit("Topic two")
	w.Checkout("main")
	w.Write("e.txt", "e\n")
	h["b2"] = w.Commit("Main two")
	w.Tag("v1", "")
	if err := w.Merge("topic"); err != nil {
		t.Fatal(err)
	}
	h["m"] = w.Head()
	return &branching{env: e, work: w, bare: bare, hashes: h}
}

// open activates the extension on the work repository.
func (b *branching) open(t *testing.T) (*rig, *repository, *fakeapi.FakeHistory) {
	t.Helper()
	r := newRig(t, b.env, rigOptions{folder: b.work.Dir})
	r.settle()
	rp := r.repo(b.work.Dir)
	r.api.WaitFor("the history provider", func() bool { r.api.Pump(); return r.source(rp.root).History() != nil })
	return r, rp, r.source(rp.root).History()
}

func idsOf(items []extension.HistoryItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Subject)
	}
	return out
}

func TestHistoryItemsOfABranchingHistory(t *testing.T) {
	b := newBranching(t)
	_, _, h := b.open(t)
	ctx := context.Background()
	items, err := h.Items(ctx, extension.HistoryOptions{Limit: 50, RefIDs: []string{b.hashes["m"]}})
	if err != nil {
		t.Fatal(err)
	}
	// parents come after their children (--topo-order)
	got := idsOf(items)
	pos := map[string]int{}
	for i, it := range items {
		pos[it.ID] = i
	}
	if len(items) != 6 || got[0] != "Merge topic" || got[len(got)-1] != "Add a" {
		t.Fatalf("items = %v", got)
	}
	for _, it := range items {
		for _, p := range it.ParentIDs {
			if pos[p] <= pos[it.ID] {
				t.Errorf("%s comes after its parent %s", it.Subject, p)
			}
		}
	}
	merge := items[0]
	if !reflect.DeepEqual(merge.ParentIDs, []string{b.hashes["b2"], b.hashes["d"]}) {
		t.Errorf("the parents of the merge = %v, want main's and topic's", merge.ParentIDs)
	}
	if merge.DisplayID != b.hashes["m"][:7] || merge.Author != "Ada Example" || merge.AuthorEmail != "ada@example.com" || merge.Timestamp.IsZero() {
		t.Errorf("merge = %+v", merge)
	}
	// the references of a commit: the branch HEAD is on first, then the remote branches, then the tags
	var names []string
	for _, ref := range merge.References {
		names = append(names, ref.ID+"|"+ref.Name+"|"+ref.Icon+"|"+ref.Category)
	}
	if want := []string{"refs/heads/main|main|target|branches", "refs/heads/topic|topic|git-branch|branches"}; len(names) == 0 || names[0] != want[0] {
		t.Errorf("references of the merge = %v, want %v first", names, want)
	}
	byName := map[string]extension.HistoryItem{}
	for _, it := range items {
		byName[it.Subject] = it
	}
	if refs := byName["Main two"].References; len(refs) != 1 || refs[0].ID != "refs/tags/v1" || refs[0].Icon != "tag" || refs[0].Category != "tags" {
		t.Errorf("references of the tagged commit = %+v", refs)
	}
	if refs := byName["Main one"].References; len(refs) != 1 || refs[0].ID != "refs/remotes/origin/main" || refs[0].Icon != "cloud" || refs[0].Category != "remote branches" {
		t.Errorf("references of the commit the remote is at = %+v", refs)
	}
	topicOne := byName["Topic one"]
	if topicOne.Author != "Grace Hopper" || topicOne.Message != "Topic one\n\nWith a body." || topicOne.Subject != "Topic one" {
		t.Errorf("Topic one = %+v", topicOne)
	}
	// the statistics: the first-parent diff, also for the merge
	if s := byName["Topic two"].Statistics; s == nil || s.Files != 1 || s.Insertions != 2 || s.Deletions != 0 {
		t.Errorf("statistics of Topic two = %+v", s)
	}
	if s := merge.Statistics; s == nil || s.Files != 2 {
		t.Errorf("statistics of the merge = %+v, want the 2 files of topic", s)
	}
}

// TestHistoryPagesJoinWithoutGapsOrDuplicates reads the history in pages of every size: a page ends inside the merge, and the pages
// together are the history at once.
func TestHistoryPagesJoinWithoutGapsOrDuplicates(t *testing.T) {
	b := newBranching(t)
	_, _, h := b.open(t)
	ctx := context.Background()
	whole, err := h.Items(ctx, extension.HistoryOptions{Limit: 100, RefIDs: []string{b.hashes["m"]}})
	if err != nil {
		t.Fatal(err)
	}
	for size := 1; size <= 4; size++ {
		var got []extension.HistoryItem
		for skip := 0; ; skip += size {
			page, err := h.Items(ctx, extension.HistoryOptions{Skip: skip, Limit: size, RefIDs: []string{b.hashes["m"]}})
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, page...)
			if len(page) < size {
				break
			}
		}
		if len(got) != len(whole) {
			t.Fatalf("pages of %d: %d items, want %d", size, len(got), len(whole))
		}
		for i := range got {
			if got[i].ID != whole[i].ID {
				t.Errorf("pages of %d: item %d is %s, want %s", size, i, got[i].Subject, whole[i].Subject)
			}
		}
	}
}

func TestHistoryItemsOfSeveralReferencesAndOfNone(t *testing.T) {
	b := newBranching(t)
	_, _, h := b.open(t)
	ctx := context.Background()
	none, err := h.Items(ctx, extension.HistoryOptions{Limit: 10})
	if err != nil || len(none) != 0 {
		t.Errorf("no reference gave %v, %v", idsOf(none), err)
	}
	// a hash twice is read once; the walk from d reaches c, b1 and a only
	items, err := h.Items(ctx, extension.HistoryOptions{Limit: 10, RefIDs: []string{b.hashes["d"], b.hashes["d"]}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Topic two", "Topic one", "Main one", "Add a"}; !reflect.DeepEqual(idsOf(items), want) {
		t.Errorf("from topic = %v, want %v", idsOf(items), want)
	}
	// two starting points
	items, err = h.Items(ctx, extension.HistoryOptions{Limit: 10, RefIDs: []string{b.hashes["d"], b.hashes["b2"]}})
	if err != nil || len(items) != 5 {
		t.Errorf("from topic and main two = %v, %v", idsOf(items), err)
	}
	// an option-like revision is refused
	if _, err := h.Items(ctx, extension.HistoryOptions{Limit: 10, RefIDs: []string{"--all"}}); err == nil {
		t.Error("a revision that starts with a dash was accepted")
	}
}

func TestHistorySearchFindsByMessageAuthorAndHash(t *testing.T) {
	b := newBranching(t)
	_, _, h := b.open(t)
	ctx := context.Background()
	opts := func(text string) extension.HistoryOptions {
		return extension.HistoryOptions{Limit: 10, RefIDs: []string{b.hashes["m"]}, FilterText: text}
	}
	tests := []struct {
		name, text string
		want       []string
	}{
		{"a word of a message, in any case", "TOPIC", []string{"Topic two", "Topic one", "Merge topic"}},
		{"the body of a message", "with a body", []string{"Topic one"}},
		{"an author", "grace", []string{"Topic one"}},
		{"an author's address", "ada@example.com", []string{"Merge topic", "Topic two", "Main two", "Main one", "Add a"}},
		{"the beginning of a hash", b.hashes["b1"][:8], []string{"Main one"}},
		{"text that is a pattern is taken as text", "Main (one", nil},
		{"nothing", "no such commit", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := h.Items(ctx, opts(tt.text))
			if err != nil {
				t.Fatal(err)
			}
			got := idsOf(items)
			slicesSort(got)
			want := append([]string(nil), tt.want...)
			slicesSort(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%q found %v, want %v", tt.text, got, want)
			}
		})
	}
	// pages of a search
	first, _ := h.Items(ctx, extension.HistoryOptions{Limit: 2, RefIDs: []string{b.hashes["m"]}, FilterText: "ada@example.com"})
	second, _ := h.Items(ctx, extension.HistoryOptions{Skip: 2, Limit: 2, RefIDs: []string{b.hashes["m"]}, FilterText: "ada@example.com"})
	if len(first) != 2 || len(second) != 2 || first[0].ID == second[0].ID || first[1].ID == second[0].ID {
		t.Errorf("pages of a search: %v then %v", idsOf(first), idsOf(second))
	}
}

func slicesSort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestHistoryRefsAreBranchesThenRemoteBranchesThenTags(t *testing.T) {
	b := newBranching(t)
	_, _, h := b.open(t)
	ctx := context.Background()
	refs, err := h.Refs(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	if len(ids) != 4 || !strings.HasPrefix(ids[0], "refs/heads/") || !strings.HasPrefix(ids[1], "refs/heads/") || ids[2] != "refs/remotes/origin/main" || ids[3] != "refs/tags/v1" {
		t.Fatalf("refs = %v", ids)
	}
	for _, r := range refs {
		if r.Revision == "" || r.Description == "" || r.Name == "" {
			t.Errorf("ref %+v lacks a field", r)
		}
	}
	if refs[2].Description != "Remote branch at "+refs[2].Revision[:7] || refs[3].Description != "Tag at "+refs[3].Revision[:7] || refs[0].Description != refs[0].Revision[:7] {
		t.Errorf("descriptions = %q %q %q", refs[0].Description, refs[2].Description, refs[3].Description)
	}
	only, err := h.Refs(ctx, []string{"refs/tags/v1"})
	if err != nil || len(only) != 1 || only[0].Name != "v1" || only[0].Revision != b.hashes["b2"] {
		t.Errorf("the refs asked for = %+v, %v", only, err)
	}
}

func TestHistoryCommonAncestor(t *testing.T) {
	b := newBranching(t)
	r, rp, h := b.open(t)
	ctx := context.Background()
	// two references: their merge base
	// (main merged topic, so the best common ancestor is the tip of topic)
	got, err := h.CommonAncestor(ctx, []string{"main", "topic"})
	if err != nil || got != b.hashes["d"] {
		t.Errorf("main and topic = %q, %v; want %s", got, err, b.hashes["d"])
	}
	if got, err := h.CommonAncestor(ctx, []string{"topic", "origin/main"}); err != nil || got != b.hashes["b1"] {
		t.Errorf("topic and origin/main = %q, %v; want %s", got, err, b.hashes["b1"])
	}
	// more than two
	if got, _ := h.CommonAncestor(ctx, []string{"main", "topic", "origin/main"}); got != b.hashes["d"] {
		t.Errorf("three references = %q", got)
	}
	// one reference that is the current branch: the commit it shares with the branch it tracks
	if got, _ := h.CommonAncestor(ctx, []string{"refs/heads/main"}); got != b.hashes["b1"] {
		t.Errorf("the current branch alone = %q, want the commit the remote is at", got)
	}
	// no reference, a reference that is not the current branch
	if got, _ := h.CommonAncestor(ctx, nil); got != "" {
		t.Errorf("no reference = %q", got)
	}
	if got, _ := h.CommonAncestor(ctx, []string{"refs/heads/topic"}); got != "" {
		t.Errorf("another branch alone = %q", got)
	}
	if _, err := h.CommonAncestor(ctx, []string{"--all", "main"}); err == nil {
		t.Error("a revision that starts with a dash was accepted")
	}
	// the current branch with no remote and no base: the first commit
	b.work.Git("branch", "--unset-upstream")
	r.refresh(rp)
	if got, _ := h.CommonAncestor(ctx, []string{"refs/heads/main"}); got != b.hashes["a"] {
		t.Errorf("a branch with no remote = %q, want the first commit %s", got, b.hashes["a"])
	}
}

func TestTheCurrentReferencesTrackTheBranchItsRemoteAndItsBase(t *testing.T) {
	w := newSyncWorld(t)
	w.localCommit("mine.txt")
	remote := w.remoteCommit("theirs.txt")
	w.work.Git("fetch", "-q")
	w.work.Branch("feature", "main")
	w.work.Checkout("feature")
	r, rp := w.open(t, rigOptions{})
	h := r.source(rp.root).History()
	if h == nil {
		t.Fatal("no history provider")
	}
	cur, rem, _ := h.CurrentRefs()
	if cur == nil || cur.ID != "refs/heads/feature" || cur.Name != "feature" || cur.Revision != w.work.Head() || cur.Icon != "target" {
		t.Errorf("current = %+v", cur)
	}
	// the branch has no upstream of its own: no remote reference, and the base is the branch it was made from (main), which tracks origin/main
	if rem != nil {
		t.Errorf("remote = %+v, want none for a branch with no upstream", rem)
	}
	r.api.WaitFor("the base reference", func() bool { r.api.Pump(); _, _, b := h.CurrentRefs(); return b != nil })
	_, _, base := h.CurrentRefs()
	if base == nil || base.ID != "refs/remotes/origin/main" || base.Name != "origin/main" || base.Revision != remote || base.Icon != "cloud" {
		t.Errorf("base = %+v, want origin/main at %s", base, remote)
	}
	// main tracks origin/main: its remote is that, and it has no base of its own that differs
	w.work.Checkout("main")
	r.refresh(rp)
	cur, rem, _ = h.CurrentRefs()
	if cur.ID != "refs/heads/main" || rem == nil || rem.ID != "refs/remotes/origin/main" || rem.Name != "origin/main" || rem.Revision != remote {
		t.Errorf("on main: current %+v remote %+v", cur, rem)
	}
	r.api.WaitFor("the base to go", func() bool { r.api.Pump(); _, _, b := h.CurrentRefs(); return b == nil })
	// a detached HEAD is the commit; a tag is the tag
	w.work.Git("checkout", "-q", "--detach", "HEAD~1")
	r.refresh(rp)
	cur, rem, base = h.CurrentRefs()
	if cur == nil || cur.ID != w.work.Head() || cur.Name != cur.ID || rem != nil || base != nil {
		t.Errorf("detached: %+v %+v %+v", cur, rem, base)
	}
}

func TestTheReferencesChangeIsToldWhenABranchMovesComesOrGoes(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{})
	h := r.source(rp.root).History()
	r.api.WaitFor("the first references", func() bool { r.api.Pump(); c, _, _ := h.CurrentRefs(); return c != nil })
	n := len(h.RefsChanges())
	w.localCommit("two.txt")
	r.refresh(rp)
	changes := h.RefsChanges()[n:]
	if len(changes) != 1 || len(changes[0].Modified) != 1 || changes[0].Modified[0].ID != "refs/heads/main" || changes[0].Silent {
		t.Fatalf("after a commit: %+v", changes)
	}
	n = len(h.RefsChanges())
	w.work.Branch("topic", "")
	w.work.Tag("v2", "")
	r.refresh(rp)
	changes = h.RefsChanges()[n:]
	if len(changes) != 1 || len(changes[0].Added) != 2 {
		t.Fatalf("after a branch and a tag: %+v", changes)
	}
	n = len(h.RefsChanges())
	w.work.Git("branch", "-D", "topic")
	r.refresh(rp)
	changes = h.RefsChanges()[n:]
	if len(changes) != 1 || len(changes[0].Removed) != 1 || changes[0].Removed[0].ID != "refs/heads/topic" || changes[0].Removed[0].Name != "topic" {
		t.Fatalf("after a branch was deleted: %+v", changes)
	}
	n = len(h.RefsChanges())
	r.refresh(rp)
	if len(h.RefsChanges()) != n {
		t.Errorf("a status that found nothing new told %+v", h.RefsChanges()[n:])
	}
}

func TestARepositoryWithNoCommitsHasNoHistory(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("empty")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.api.WaitFor("the history provider", func() bool { r.api.Pump(); return r.source(rp.root).History() != nil })
	h := r.source(rp.root).History()
	items, err := h.Items(context.Background(), extension.HistoryOptions{Limit: 10, RefIDs: []string{"HEAD"}})
	if err != nil || len(items) != 0 {
		t.Errorf("Items = %v, %v", items, err)
	}
	refs, err := h.Refs(context.Background(), nil)
	if err != nil || len(refs) != 0 {
		t.Errorf("Refs = %v, %v", refs, err)
	}
	if got, err := h.CommonAncestor(context.Background(), []string{"refs/heads/main"}); got != "" || err != nil {
		t.Errorf("CommonAncestor = %q, %v", got, err)
	}
}

func TestTheHistoryOfManyCommitsIsReadInPages(t *testing.T) {
	repo := gittest.Fixture(t, "history", t.TempDir()+"/many", gittest.FixtureOptions{})
	r := newRig(t, repo.Env, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.api.WaitFor("the history provider", func() bool { r.api.Pump(); return r.source(rp.root).History() != nil })
	h := r.source(rp.root).History()
	ctx := context.Background()
	total := 0
	for skip := 0; ; skip += 50 {
		page, err := h.Items(ctx, extension.HistoryOptions{Skip: skip, Limit: 50, RefIDs: []string{repo.Head()}})
		if err != nil {
			t.Fatal(err)
		}
		total += len(page)
		if len(page) < 50 {
			break
		}
	}
	if want := len(strings.Fields(repo.Git("rev-list", "HEAD"))); total != want {
		t.Errorf("the pages hold %d commits, git has %d", total, want)
	}
}

func TestTheHistoryOfAGeneratedRepositoryIsReadInPagesThatJoin(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.LongHistory(t.TempDir()+"/long", 1200)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.api.WaitFor("the history provider", func() bool { r.api.Pump(); return r.source(rp.root).History() != nil })
	h := r.source(rp.root).History()
	var got []string
	for skip := 0; ; skip += 300 {
		page, err := h.Items(context.Background(), extension.HistoryOptions{Skip: skip, Limit: 300, RefIDs: []string{repo.Head()}})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page {
			got = append(got, it.ID)
		}
		if len(page) < 300 {
			break
		}
	}
	want := strings.Fields(repo.Git("rev-list", "--topo-order", "HEAD"))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the pages hold %d commits (first %v), git lists %d (first %v)", len(got), got[:min(2, len(got))], len(want), want[:2])
	}
}
