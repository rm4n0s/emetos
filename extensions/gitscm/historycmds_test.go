package gitscm

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

var errClipboard = &errors.Error{Tag: "FakeClipboard", Message: "no clipboard"}

// The commands of the menus of the Source Control Graph run the right git commands (step 24.20).

// graphArgs are the arguments the Graph gives a command for a commit: the source control, the commit with its references, and the
// reference of a badge when the menu is one.
func graphArgs(rp *repository, id, subject string, refs []map[string]any, refID string) []any {
	args := []any{
		sourceArg(rp),
		map[string]any{extension.HistoryArgItem: map[string]any{
			"id": id, "subject": subject, "message": subject + "\n\nWith a body.", "displayId": id[:7], "references": toAnys(refs),
		}},
	}
	if refID != "" {
		args = append(args, map[string]any{extension.HistoryArgRef: refID})
	}
	return args
}

func toAnys(refs []map[string]any) []any {
	var out []any
	for _, r := range refs {
		out = append(out, r)
	}
	return out
}

func refOf(id, name, rev string) map[string]any {
	return map[string]any{"id": id, "name": name, "revision": rev}
}

func TestGraphCheckout(t *testing.T) {
	b := newBranching(t)
	r, rp, _ := b.open(t)
	// a local branch, by name
	topic := refOf("refs/heads/topic", "topic", b.hashes["d"])
	r.run(rp, "git.graph.checkout", graphArgs(rp, b.hashes["d"], "Topic two", []map[string]any{topic}, "refs/heads/topic"))
	if got := branchOf(b.work); got != "topic" {
		t.Errorf("on %q after checking out a branch", got)
	}
	// a tag: the work tree is detached at the tag
	tag := refOf("refs/tags/v1", "v1", b.hashes["b2"])
	r.run(rp, "git.graph.checkout", graphArgs(rp, b.hashes["b2"], "Main two", []map[string]any{tag}, "refs/tags/v1"))
	if got := branchOf(b.work); got != "HEAD" || b.work.Head() != b.hashes["b2"] {
		t.Errorf("on %q at %s after checking out a tag", got, b.work.Head())
	}
	// a remote branch that a local branch tracks: that branch
	remote := refOf("refs/remotes/origin/main", "origin/main", b.hashes["b1"])
	r.run(rp, "git.graph.checkout", graphArgs(rp, b.hashes["b1"], "Main one", []map[string]any{remote}, "refs/remotes/origin/main"))
	if got := branchOf(b.work); got != "main" {
		t.Errorf("on %q after checking out the remote branch of main", got)
	}
	// a remote branch with no local branch: a new branch that tracks it
	b.work.Git("push", "-q", "origin", "topic:feat")
	b.work.Git("fetch", "-q")
	r.refresh(rp)
	feat := refOf("refs/remotes/origin/feat", "origin/feat", b.hashes["d"])
	r.run(rp, "git.graph.checkout", graphArgs(rp, b.hashes["d"], "Topic two", []map[string]any{feat}, "refs/remotes/origin/feat"))
	if got := branchOf(b.work); got != "feat" {
		t.Errorf("on %q after checking out a remote branch with no local one", got)
	}
	if got := strings.TrimSpace(b.work.Git("rev-parse", "--abbrev-ref", "feat@{upstream}")); got != "origin/feat" {
		t.Errorf("feat tracks %q", got)
	}
	// no reference in the arguments, or no commit: nothing happens
	for _, args := range []any{nil, sourceArg(rp), graphArgs(rp, b.hashes["d"], "x", nil, "refs/heads/nope")} {
		if err := r.api.Run("git.graph.checkout", args); err != nil {
			t.Fatal(err)
		}
	}
	r.idle(rp)
	if got := branchOf(b.work); got != "feat" {
		t.Errorf("on %q after calls with nothing to check out", got)
	}
}

func TestGraphCheckoutDetached(t *testing.T) {
	b := newBranching(t)
	r, rp, _ := b.open(t)
	r.run(rp, "git.graph.checkoutDetached", graphArgs(rp, b.hashes["c"], "Topic one", nil, ""))
	if got := branchOf(b.work); got != "HEAD" || b.work.Head() != b.hashes["c"] {
		t.Errorf("on %q at %s", got, b.work.Head())
	}
	if err := r.api.Run("git.graph.checkoutDetached", nil); err != nil {
		t.Fatal(err)
	}
}

func TestGraphDeleteBranchAndTag(t *testing.T) {
	b := newBranching(t)
	r, rp, h := b.open(t)
	r.api.WaitFor("the current references", func() bool { r.api.Pump(); c, _, _ := h.CurrentRefs(); return c != nil })
	branches := func() string { return strings.TrimSpace(b.work.Git("branch", "--format=%(refname:short)")) }
	// the current branch is kept
	main := refOf("refs/heads/main", "main", b.hashes["m"])
	r.run(rp, "git.graph.deleteBranch", graphArgs(rp, b.hashes["m"], "Merge topic", []map[string]any{main}, "refs/heads/main"))
	if !strings.Contains(branches(), "main") || len(notesWith(r, "active branch cannot be deleted")) != 1 {
		t.Errorf("branches %q, notes %v", branches(), r.noteTexts())
	}
	// the remote branch of the current branch is kept too
	remote := refOf("refs/remotes/origin/main", "origin/main", b.hashes["b1"])
	r.api.WaitFor("the remote reference", func() bool { r.api.Pump(); _, rem, _ := h.CurrentRefs(); return rem != nil })
	r.run(rp, "git.graph.deleteBranch", graphArgs(rp, b.hashes["b1"], "Main one", []map[string]any{remote}, "refs/remotes/origin/main"))
	if len(notesWith(r, "remote branch of the active branch")) != 1 || r.quiet() == false {
		t.Errorf("notes %v", r.noteTexts())
	}
	// another branch is deleted at once
	topic := refOf("refs/heads/topic", "topic", b.hashes["d"])
	r.run(rp, "git.graph.deleteBranch", graphArgs(rp, b.hashes["d"], "Topic two", []map[string]any{topic}, "refs/heads/topic"))
	if strings.Contains(branches(), "topic") {
		t.Errorf("branches %q after deleting topic", branches())
	}
	// a remote branch is deleted after a question, from the server
	b.work.Git("push", "-q", "origin", "main:old")
	b.work.Git("fetch", "-q")
	r.refresh(rp)
	old := refOf("refs/remotes/origin/old", "origin/old", b.hashes["m"])
	r.start(rp, "git.graph.deleteBranch", graphArgs(rp, b.hashes["m"], "Merge topic", []map[string]any{old}, "refs/remotes/origin/old"))
	r.api.WaitFor("the question", func() bool { r.api.Pump(); return !r.quiet() })
	if !strings.Contains(r.theOnlyDialog().Spec().Message, `branch "old" from the remote "origin"`) {
		t.Errorf("question %+v", r.theOnlyDialog().Spec())
	}
	r.answer("Delete Remote Branch")
	r.idle(rp)
	if got := b.bare.Git("branch", "--list", "old"); strings.TrimSpace(got) != "" {
		t.Errorf("the remote still has %q", got)
	}
	// a tag
	tag := refOf("refs/tags/v1", "v1", b.hashes["b2"])
	r.run(rp, "git.graph.deleteTag", graphArgs(rp, b.hashes["b2"], "Main two", []map[string]any{tag}, "refs/tags/v1"))
	if got := strings.TrimSpace(b.work.Git("tag", "--list")); got != "" {
		t.Errorf("tags %q after deleting v1", got)
	}
}

func TestGraphCherryPick(t *testing.T) {
	b := newBranching(t)
	r, rp, _ := b.open(t)
	b.work.Checkout("topic")
	r.refresh(rp)
	r.run(rp, "git.graph.cherryPick", graphArgs(rp, b.hashes["b2"], "Main two", nil, ""))
	if got := b.work.Read("e.txt"); got != "e\n" {
		t.Errorf("e.txt = %q after the cherry-pick of Main two", got)
	}
	if subject := strings.TrimSpace(b.work.Git("log", "-1", "--format=%s")); subject != "Main two" {
		t.Errorf("the last commit is %q", subject)
	}
}

func TestGraphCopyCommit(t *testing.T) {
	b := newBranching(t)
	r, rp, _ := b.open(t)
	args := graphArgs(rp, b.hashes["c"], "Topic one", nil, "")
	for _, cmd := range []string{"git.copyCommitId", "git.copyCommitMessage"} {
		if err := r.api.Run(cmd, args); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.api.ClipboardTexts(); len(got) != 2 || got[0] != b.hashes["c"] || got[1] != "Topic one\n\nWith a body." {
		t.Errorf("clipboard %q", got)
	}
	// a clipboard that fails is a notification, not a crash
	r.api.FailClipboard(errClipboard)
	if err := r.api.Run("git.copyCommitId", args); err != nil {
		t.Fatal(err)
	}
	if len(notesWith(r, "Cannot copy to the clipboard")) != 1 {
		t.Errorf("notes %v", r.noteTexts())
	}
	r.api.FailClipboard(nil)
	if err := r.api.Run("git.copyCommitId", nil); err != nil {
		t.Fatal(err)
	}
}

func TestGraphCompare(t *testing.T) {
	b := newBranching(t)
	r, rp, h := b.open(t)
	r.api.WaitFor("the current references", func() bool { r.api.Pump(); _, rem, _ := h.CurrentRefs(); return rem != nil })
	open := func(cmd string, args any) extension.MultiDiffSpec {
		t.Helper()
		before := len(r.api.OpenedMultiDiffs())
		if err := r.api.Run(cmd, args); err != nil {
			t.Fatal(err)
		}
		r.settleUntil("the editor", func() bool { return len(r.api.OpenedMultiDiffs()) > before })
		return r.api.OpenedMultiDiffs()[before]
	}
	// compare with the remote branch: from origin/main to the commit
	spec := open("git.graph.compareWithRemote", graphArgs(rp, b.hashes["d"], "Topic two", []map[string]any{refOf("refs/heads/topic", "topic", b.hashes["d"])}, ""))
	if spec.Kind != MultiDiffKind || spec.Key != rangeKey(rp.id, b.hashes["b1"], b.hashes["d"]) || spec.Title != "origin/main ↔ topic" {
		t.Errorf("spec %+v", spec)
	}
	files, err := r.multiFiles(spec.Key)
	if err != nil || len(files) != 2 {
		t.Errorf("files %v, %v", files, err)
	}
	// no change between the two ends
	before := len(r.api.Notes())
	if err := r.api.Run("git.graph.compareWithRemote", graphArgs(rp, b.hashes["b1"], "Main one", nil, "")); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the note", func() bool { r.api.Pump(); return len(r.api.Notes()) > before })
	if note := r.api.Notes()[before]; !strings.Contains(note.Message, "There are no changes between") {
		t.Errorf("note %+v", note)
	}
	// compare with the merge base: needs a base reference
	b.work.Branch("feature", "main")
	b.work.Checkout("feature")
	r.refresh(rp)
	r.api.WaitFor("the base reference", func() bool { r.api.Pump(); _, _, base := h.CurrentRefs(); return base != nil })
	spec = open("git.graph.compareWithMergeBase", graphArgs(rp, b.hashes["m"], "Merge topic", nil, ""))
	if spec.Key != rangeKey(rp.id, b.hashes["b1"], b.hashes["m"]) || !strings.HasPrefix(spec.Title, "origin/main ↔ ") {
		t.Errorf("spec %+v", spec)
	}
	// compare with a picked reference
	pick := r.startPick("git.graph.compareRef", graphArgs(rp, b.hashes["m"], "Merge topic", nil, ""))
	chosen := false
	for i, label := range pick.Labels() {
		if strings.HasSuffix(label, " v1") {
			pick.Choose(i)
			chosen = true
			break
		}
	}
	if !chosen {
		t.Fatalf("the picker has %q", pick.Labels())
	}
	r.settleUntil("the editor of the tag", func() bool {
		for _, s := range r.api.OpenedMultiDiffs() {
			if s.Key == rangeKey(rp.id, b.hashes["b2"], b.hashes["m"]) {
				return true
			}
		}
		return false
	})
}

func TestViewCommitOpensARange(t *testing.T) {
	b := newBranching(t)
	r, rp, _ := b.open(t)
	args := []any{
		sourceArg(rp),
		map[string]any{extension.HistoryArgRange: map[string]any{"base": b.hashes["b1"], "head": b.hashes["m"], "title": "Incoming Changes - a"}},
	}
	if err := r.api.Run("git.viewCommit", args); err != nil {
		t.Fatal(err)
	}
	r.settleUntil("the editor", func() bool { return len(r.api.OpenedMultiDiffs()) == 1 })
	spec := r.api.OpenedMultiDiffs()[0]
	if spec.Title != "Incoming Changes - a" || spec.Key != rangeKey(rp.id, b.hashes["b1"], b.hashes["m"]) {
		t.Errorf("spec %+v", spec)
	}
	files, err := r.multiFiles(spec.Key)
	if err != nil || len(files) != 3 {
		t.Errorf("files %d, %v", len(files), err)
	}
	// the item of a row opens that commit
	args = graphArgs(rp, b.hashes["c"], "Topic one", nil, "")
	if err := r.api.Run("git.viewCommit", args); err != nil {
		t.Fatal(err)
	}
	r.settleUntil("the commit", func() bool { return len(r.api.OpenedMultiDiffs()) == 2 })
	if got := r.api.OpenedMultiDiffs()[1].Key; got != commitKey(rp.id, b.hashes["c"]) {
		t.Errorf("key %q", got)
	}
}

func TestBranchAndTagFromACommitOfTheGraph(t *testing.T) {
	b := newBranching(t)
	r, rp, _ := b.open(t)
	args := graphArgs(rp, b.hashes["c"], "Topic one", nil, "")
	r.start(rp, "git.branch", args)
	r.nextInput(0).Accept("from-c")
	r.idle(rp)
	if got := branchOf(b.work); got != "from-c" || b.work.Head() != b.hashes["c"] {
		t.Errorf("on %q at %s, want from-c at Topic one", got, b.work.Head())
	}
	r.start(rp, "git.createTag", args)
	r.nextInput(1).Accept("at-c")
	r.nextInput(2).Accept("")
	r.idle(rp)
	if got := strings.TrimSpace(b.work.Git("rev-parse", "at-c")); got != b.hashes["c"] {
		t.Errorf("at-c names %s, want %s", got, b.hashes["c"])
	}
}

func TestTheTitleActionsOfTheGraphDefaultToTheBranchTheCurrentOneTracks(t *testing.T) {
	w := newSyncWorld(t)
	w.other.Git("push", "-q", "origin", "main:topic")
	head := w.remoteCommit("b.txt")
	r, rp := w.open(t, rigOptions{})
	h := r.source(rp.root).History()
	r.api.WaitFor("the remote reference", func() bool { r.api.Pump(); _, rem, _ := h.CurrentRefs(); return rem != nil })
	// the source control alone is the argument of a title action: the tracked branch is fetched, and no other
	r.run(rp, "git.fetchRef", []any{sourceArg(rp)})
	if got := w.work.Git("rev-parse", "origin/main"); got != head {
		t.Errorf("origin/main = %s, want %s", got, head)
	}
	if got := w.work.Git("branch", "-r", "--list", "origin/topic"); got != "" {
		t.Errorf("another branch was fetched: %q", got)
	}
	r.run(rp, "git.pullRef", []any{sourceArg(rp)})
	if got := w.work.Head(); got != head {
		t.Errorf("HEAD = %s, want %s", got, head)
	}
}
