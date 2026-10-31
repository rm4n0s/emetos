package gitscm

import (
	"bytes"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The blame text of the editor and the status bar, against real repositories (step 24.22).

const blameFile = "src.txt"

// blameWorld is a repository with a file of six lines and three commits, the extension over it, and an editor of the file.
type blameWorld struct {
	t    *testing.T
	r    *rig
	repo *gittest.Repo
	uri  string
	text string
	// commits are the hashes of the commits in order: the file made by Ada, two lines changed by Grace Hopper, the file moved by Ada.
	commits []string
}

const blameBase = "one\ntwo\nthree\nfour\nfive\nsix\n"

// newBlameWorld makes the world. The two kinds of blame text are on unless the settings say otherwise.
func newBlameWorld(t *testing.T, settings map[string]any) *blameWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("blame")
	repo.Write(blameFile, blameBase)
	repo.Git("add", "-A")
	c1 := repo.CommitAs("Ada Example", "ada@example.com", "Add the file")
	repo.Write(blameFile, "one\ntwo\nTHREE\nFOUR\nfive\nsix\n")
	repo.Git("add", "-A")
	c2 := repo.CommitAs("Grace Hopper", "grace@example.com", "Shout two lines\n\nThe body of the message.")
	return startBlameWorld(t, e, repo, settings, []string{c1, c2})
}

func startBlameWorld(t *testing.T, e *gittest.Env, repo *gittest.Repo, settings map[string]any, commits []string) *blameWorld {
	t.Helper()
	merged := map[string]any{"git.blame.editorDecoration.enabled": true, "git.commandsToLog": []string{"blame", "log", "show"}}
	for k, v := range settings {
		merged[k] = v
	}
	r := newRig(t, e, rigOptions{folder: repo.Dir, settings: merged})
	r.x.blame.loc = time.UTC
	r.settle()
	w := &blameWorld{t: t, r: r, repo: repo, commits: commits}
	if _, err := os.Stat(repo.Path(blameFile)); err == nil {
		w.open(blameFile)
	}
	return w
}

// open makes the file the one of the editor, with the text it has on disk.
func (w *blameWorld) open(rel string) {
	w.t.Helper()
	w.uri = uriOf(w.repo.Path(rel))
	w.text = w.repo.Read(rel)
	w.r.api.SetDocument(w.uri, w.text)
}

// move puts the cursors on the lines (the first is the primary one), as the editor tells it, waits the 150 ms and for the blame to be shown.
func (w *blameWorld) move(lines ...int) {
	w.t.Helper()
	w.moveAndWait(w.askMove(lines...))
}

// askMove sets the cursors and tells the extension, without waiting.
func (w *blameWorld) askMove(lines ...int) extension.EditorSelection {
	var sels []extension.Selection
	for _, l := range lines {
		p := extension.Position{Line: l}
		sels = append(sels, extension.Selection{Anchor: p, Active: p})
	}
	w.r.api.SetActive(&extension.ActiveDocument{URI: w.uri, LanguageID: "plaintext", Version: 1, Selections: sels})
	ev := extension.EditorSelection{URI: w.uri, Selections: sels}
	w.r.api.Emit(extension.TopicEditorSelectionChanged, ev)
	return ev
}

// settled tells whether no blame is waiting or working.
func (w *blameWorld) settled() bool {
	b := w.r.x.blame
	return b.stopTimer == nil && b.cancel == nil
}

func (w *blameWorld) moveAndWait(extension.EditorSelection) {
	w.t.Helper()
	w.r.clock.Advance(blameDelay)
	w.r.api.WaitFor("the blame", func() bool { w.r.api.Pump(); return w.settled() })
}

// edit changes the text of the document, as typing does, and tells the extension.
func (w *blameWorld) edit(text string, lines ...int) {
	w.t.Helper()
	w.text = text
	w.r.api.SetDocument(w.uri, text)
	w.r.api.Emit(extension.TopicDocumentChanged, extension.DocumentChange{URI: w.uri, Version: 2, Cause: "edit"})
	w.move(lines...)
}

// texts are the texts of the annotations by line.
func (w *blameWorld) texts() map[int]string {
	out := map[int]string{}
	for _, a := range w.r.api.Annotations(w.uri) {
		out[a.Line] = a.Text
	}
	return out
}

// blameRuns is how many times git blame was started, from the Git channel.
func (w *blameWorld) blameRuns() int {
	n := 0
	for _, l := range w.r.api.Channel(ChannelName).Lines() {
		if strings.HasPrefix(l, "> git ") && strings.Contains(l, " blame ") {
			n++
		}
	}
	return n
}

var agoText = `\((now|\d+ \w+ ago)\)`

func matches(t *testing.T, got, pattern string) {
	t.Helper()
	if !regexp.MustCompile("^" + pattern + "$").MatchString(got) {
		t.Errorf("%q does not match %s", got, pattern)
	}
}

func TestBlameShowsTheCommitOfTheLineOfTheCursor(t *testing.T) {
	w := newBlameWorld(t, nil)
	w.move(0)
	got := w.texts()
	if len(got) != 1 {
		t.Fatalf("annotations %v", got)
	}
	matches(t, got[0], `Add the file, Ada Example `+agoText)
	w.move(2)
	got = w.texts()
	if len(got) != 1 {
		t.Fatalf("after moving the cursor: %v", got)
	}
	matches(t, got[2], `Shout two lines, Grace Hopper `+agoText)
	an := w.r.api.Annotations(w.uri)[0]
	if an.Color != "git.blame.editorDecorationForeground" {
		t.Errorf("the color is %q", an.Color)
	}
}

func TestBlameShowsEveryCursorAndTheStatusBarTheFirst(t *testing.T) {
	w := newBlameWorld(t, nil)
	w.move(3, 0, 3, 5)
	got := w.texts()
	if len(got) != 3 {
		t.Fatalf("annotations %v (three lines, the duplicate is one)", got)
	}
	matches(t, got[3], `Shout two lines, Grace Hopper `+agoText)
	matches(t, got[0], `Add the file, Ada Example `+agoText)
	matches(t, got[5], `Add the file, Ada Example `+agoText)
	item := w.r.api.StatusItem(blameKey)
	if item == nil || item.Hidden {
		t.Fatalf("the status item: %+v", item)
	}
	matches(t, item.Text, `\$\(git-commit\) Grace Hopper `+agoText)
	if item.Command != "git.viewCommit" || item.Alignment != extension.StatusRight || item.Priority != 200 {
		t.Errorf("the item is %+v", item.StatusItem)
	}
	args, _ := item.Args.(map[string]any)
	if args["id"] != w.commits[1] || args["revealUri"] != w.uri || args["root"] != w.repo.Dir {
		t.Errorf("a click opens %v, want the commit %s", item.Args, w.commits[1])
	}
	if !strings.Contains(item.Tooltip, "Shout two lines") || !strings.Contains(item.Tooltip, "grace@example.com") {
		t.Errorf("the tooltip %q", item.Tooltip)
	}
}

func TestBlameOfLinesChangedSinceTheCommitIsUncommittedWithTheNameOfTheUser(t *testing.T) {
	w := newBlameWorld(t, nil)
	// a line is added above line "three", one changed, and one at the end: the others keep their commits
	w.edit("zero\none\ntwo\nthree CHANGED\nFOUR\nfive\nsix\nseven\n", 0, 1, 4, 5, 7)
	got := w.texts()
	matches(t, got[0], `Uncommitted changes, Ada Example \(now\)`) // user.name of the isolated configuration
	matches(t, got[1], `Add the file, Ada Example `+agoText)
	matches(t, got[3-0], `.*`)
	matches(t, got[4], `Shout two lines, Grace Hopper `+agoText)
	matches(t, got[5], `Add the file, Ada Example `+agoText)
	matches(t, got[7], `Uncommitted changes, Ada Example \(now\)`)
	if _, ok := got[3]; ok {
		t.Errorf("a line with no cursor has a text: %q", got[3])
	}
	for _, a := range w.r.api.Annotations(w.uri) {
		if strings.HasPrefix(a.Text, "Uncommitted") && a.Hover != "" {
			t.Errorf("the hover of an uncommitted line is %q", a.Hover)
		}
	}
	item := w.r.api.StatusItem(blameKey)
	if item == nil || item.Command != "" {
		t.Errorf("an uncommitted line opens no commit: %+v", item)
	}
	matches(t, item.Text, `\$\(git-commit\) Ada Example \(now\)`)
}

func TestBlameNamesTheUserYouWhenGitHasNoName(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("blame")
	repo.Write(blameFile, blameBase)
	c1 := repo.Commit("Add the file")
	e.Identity = false // from here on git has no user.name, as on a machine where it was never set up
	w := startBlameWorld(t, e, repo, nil, []string{c1})
	w.edit(blameBase+"more\n", 6)
	matches(t, w.texts()[6], `Uncommitted changes, You \(now\)`)
}

func TestBlameFollowsTheEditorAndTheSettings(t *testing.T) {
	t.Run("the editor decoration off leaves the status bar", func(t *testing.T) {
		w := newBlameWorld(t, map[string]any{"git.blame.editorDecoration.enabled": false})
		w.move(0)
		if got := w.texts(); len(got) != 0 {
			t.Errorf("the text of the editor shows: %v", got)
		}
		if item := w.r.api.StatusItem(blameKey); item == nil || item.Hidden {
			t.Errorf("no status item: %+v", item)
		}
	})
	t.Run("the status bar off leaves the editor", func(t *testing.T) {
		w := newBlameWorld(t, map[string]any{"git.blame.statusBarItem.enabled": false})
		w.move(0)
		if len(w.texts()) != 1 {
			t.Errorf("no text: %v", w.texts())
		}
		if item := w.r.api.StatusItem(blameKey); item != nil {
			t.Errorf("a status item: %+v", item)
		}
	})
	t.Run("both off runs nothing", func(t *testing.T) {
		w := newBlameWorld(t, map[string]any{"git.blame.editorDecoration.enabled": false, "git.blame.statusBarItem.enabled": false})
		w.move(0)
		if got := w.blameRuns(); got != 0 || w.r.clock.Pending() != 0 || len(w.texts()) != 0 || w.r.api.StatusItem(blameKey) != nil {
			t.Errorf("blame ran %d times, %d timers, %v", got, w.r.clock.Pending(), w.texts())
		}
	})
	t.Run("turning the editor decoration on and off while the cursor stays", func(t *testing.T) {
		w := newBlameWorld(t, map[string]any{"git.blame.editorDecoration.enabled": false})
		w.move(2)
		w.r.api.SetSetting("git.blame.editorDecoration.enabled", true)
		w.r.clock.Advance(blameDelay)
		w.r.api.WaitFor("the text", func() bool { w.r.api.Pump(); return w.settled() && len(w.texts()) == 1 })
		w.r.api.SetSetting("git.blame.editorDecoration.enabled", false)
		w.r.clock.Advance(blameDelay)
		w.r.api.WaitFor("no text", func() bool { w.r.api.Pump(); return w.settled() && len(w.texts()) == 0 })
	})
	t.Run("the templates", func(t *testing.T) {
		w := newBlameWorld(t, map[string]any{
			"git.blame.editorDecoration.template": "${hashShort} ${authorEmail} ${unknown}", "git.blame.statusBarItem.template": "by ${authorName}",
			"git.commitShortHashLength": 9,
		})
		w.move(3)
		if got, want := w.texts()[3], w.commits[1][:9]+" grace@example.com ${unknown}"; got != want {
			t.Errorf("text %q, want %q", got, want)
		}
		if got := w.r.api.StatusItem(blameKey).Text; got != "$(git-commit) by Grace Hopper" {
			t.Errorf("status %q", got)
		}
	})
	t.Run("a long subject is cut at 50 characters", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("blame")
		repo.Write(blameFile, blameBase)
		repo.Git("add", "-A")
		subject := strings.Repeat("é", 70)
		c1 := repo.CommitAs("Zoë Müller", "zoe@example.com", subject)
		w := startBlameWorld(t, e, repo, map[string]any{"git.blame.editorDecoration.template": "${subject}|${authorName}"}, []string{c1})
		w.move(0)
		if got, want := w.texts()[0], strings.Repeat("é", 50)+"…|Zoë Müller"; got != want {
			t.Errorf("text %q, want %q", got, want)
		}
	})
}

func TestBlameIsNotShownForWhatHasNoBlame(t *testing.T) {
	t.Run("an untracked file", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		w.repo.Write("new.txt", "a\nb\n")
		rep := w.r.repo(w.repo.Dir)
		w.r.refresh(rep)
		w.open("new.txt")
		runs := w.blameRuns()
		w.move(0)
		if len(w.texts()) != 0 || w.blameRuns() != runs || w.r.api.StatusItem(blameKey) != nil && !w.r.api.StatusItem(blameKey).Hidden {
			t.Errorf("untracked: %v, runs %d -> %d", w.texts(), runs, w.blameRuns())
		}
	})
	t.Run("an ignored file", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		w.repo.Write(".gitignore", "*.log\n")
		w.repo.Write("debug.log", "a\nb\n")
		w.r.refresh(w.r.repo(w.repo.Dir))
		w.open("debug.log")
		runs := w.blameRuns()
		w.move(0)
		if len(w.texts()) != 0 || w.blameRuns() != runs {
			t.Errorf("ignored: %v, runs %d -> %d", w.texts(), runs, w.blameRuns())
		}
	})
	t.Run("a file that is too large", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		w.repo.Write("big.txt", strings.Repeat("a line of text\n", blameMaxFile/10))
		w.repo.Git("add", "big.txt")
		w.repo.Commit("Add a big file")
		w.r.refresh(w.r.repo(w.repo.Dir))
		w.open("big.txt")
		runs := w.blameRuns()
		w.move(0)
		if len(w.texts()) != 0 || w.blameRuns() != runs {
			t.Errorf("too large: %v, runs %d -> %d", w.texts(), runs, w.blameRuns())
		}
	})
	t.Run("a file outside every repository", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		outside := filepath.Join(t.TempDir(), "x.txt")
		writeFile(t, outside, "a\n")
		w.uri, w.text = uriOf(outside), "a\n"
		w.r.api.SetDocument(w.uri, w.text)
		runs := w.blameRuns()
		w.move(0)
		if len(w.texts()) != 0 || w.blameRuns() != runs {
			t.Errorf("outside: %v", w.texts())
		}
	})
	t.Run("a document that is not a file", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		w.uri = "untitled:Untitled-1"
		w.r.api.SetDocument(w.uri, "a\n")
		w.move(0)
		if len(w.texts()) != 0 {
			t.Errorf("untitled: %v", w.texts())
		}
	})
	t.Run("a file that is not in HEAD", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		w.repo.Write("staged.txt", "a\n")
		w.repo.Git("add", "staged.txt")
		w.r.refresh(w.r.repo(w.repo.Dir))
		w.open("staged.txt")
		w.move(0)
		if len(w.texts()) != 0 {
			t.Errorf("a staged new file: %v", w.texts())
		}
		w.move(0)
		w.move(0)
		if runs := w.blameRuns(); runs != 1 {
			t.Errorf("git blame ran %d times for a file that has none (the answer is cached)", runs)
		}
	})
	t.Run("a document that is another file than HEAD's", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		var other strings.Builder
		for i := range 2000 {
			other.WriteString("another file line " + strings.Repeat("x", i%50) + "\n")
		}
		w.edit(other.String(), 0)
		if len(w.texts()) != 0 {
			t.Errorf("a document that has nothing of HEAD's file: %v", w.texts())
		}
	})
	t.Run("no editor", func(t *testing.T) {
		w := newBlameWorld(t, nil)
		w.move(0)
		if len(w.texts()) != 1 {
			t.Fatal("no text to start with")
		}
		w.r.api.SetActive(nil)
		w.r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{})
		w.r.api.Pump()
		if len(w.texts()) != 0 || w.r.clock.Pending() != 0 {
			t.Errorf("after the last editor closed: %v, %d timers", w.texts(), w.r.clock.Pending())
		}
		if item := w.r.api.StatusItem(blameKey); item == nil || !item.Hidden {
			t.Errorf("the status item is still shown: %+v", item)
		}
	})
}

func TestBlameIsCachedByFileAndHeadAndStartedOncePerPause(t *testing.T) {
	w := newBlameWorld(t, nil)
	w.move(0)
	if w.blameRuns() != 1 {
		t.Fatalf("git blame ran %d times", w.blameRuns())
	}
	for _, l := range []int{1, 2, 3, 4, 5} {
		w.move(l)
	}
	if w.blameRuns() != 1 {
		t.Errorf("moving the cursor in the same file ran git blame %d times", w.blameRuns())
	}
	// a fast sequence of moves is one run of the blame: each cancels the wait of the one before
	runsBefore := w.blameRuns()
	for l := range 6 {
		w.askMove(l)
		w.r.clock.Advance(blameDelay / 3)
	}
	if w.r.clock.Pending() != 1 {
		t.Errorf("%d waits are pending, want one", w.r.clock.Pending())
	}
	w.r.clock.Advance(blameDelay)
	w.r.api.WaitFor("the blame", func() bool { w.r.api.Pump(); return w.settled() })
	if _, ok := w.texts()[5]; !ok || len(w.texts()) != 1 {
		t.Errorf("after the fast moves: %v", w.texts())
	}
	if w.blameRuns() != runsBefore {
		t.Errorf("the cached file was blamed again")
	}
	// another commit changes HEAD: the cache has no answer for it
	w.repo.Write(blameFile, "one\ntwo\nTHREE\nFOUR\nfive\nsix\nseven\n")
	w.repo.Git("add", "-A")
	w.repo.CommitAs("Ada Example", "ada@example.com", "Add a line")
	w.r.refresh(w.r.repo(w.repo.Dir))
	w.text = w.repo.Read(blameFile)
	w.r.api.SetDocument(w.uri, w.text)
	w.r.clock.Advance(blameDelay) // the new HEAD asked for a new blame
	w.r.api.WaitFor("the blame at the new HEAD", func() bool { w.r.api.Pump(); return w.settled() && w.blameRuns() == runsBefore+1 })
	// the file of the old HEAD stays cached: ignoring the white space is another key
	w.r.api.SetSetting("git.blame.ignoreWhitespace", true)
	w.r.clock.Advance(blameDelay)
	w.r.api.WaitFor("the blame ignoring white space", func() bool { w.r.api.Pump(); return w.settled() && w.blameRuns() == runsBefore+2 })
	var withW bool
	for _, l := range w.r.api.Channel(ChannelName).Lines() {
		withW = withW || strings.Contains(l, " blame ") && strings.Contains(l, " -w")
	}
	if !withW {
		t.Error("git.blame.ignoreWhitespace did not add -w")
	}
}

func TestBlameIsIdleWithoutAChangeOfTheCursor(t *testing.T) {
	w := newBlameWorld(t, nil)
	w.move(2)
	lines := len(w.r.api.Channel(ChannelName).Lines())
	for range 50 {
		w.r.clock.Advance(time.Second)
		w.r.api.Pump()
	}
	time.Sleep(50 * time.Millisecond)
	w.r.api.Pump()
	if w.r.clock.Pending() != 0 || len(w.r.api.Channel(ChannelName).Lines()) != lines || !w.settled() {
		t.Errorf("blame runs by itself: %d timers, %d new lines of the Git channel", w.r.clock.Pending(), len(w.r.api.Channel(ChannelName).Lines())-lines)
	}
}

func TestBlameIgnoringWhiteSpaceKeepsTheCommitOfAReindentedLine(t *testing.T) {
	w := newBlameWorld(t, nil)
	reindented := "one\ntwo\n  THREE\nFOUR\nfive\nsix\n"
	w.edit(reindented, 2)
	matches(t, w.texts()[2], `Uncommitted changes, Ada Example \(now\)`)
	w.r.api.SetSetting("git.blame.ignoreWhitespace", true)
	w.r.clock.Advance(blameDelay)
	w.r.api.WaitFor("the blame", func() bool { w.r.api.Pump(); return w.settled() && strings.HasPrefix(w.texts()[2], "Shout") })
}

func TestBlameFollowsAFileThroughARenameAndBoundariesOfAShallowClone(t *testing.T) {
	t.Run("a renamed file", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("blame")
		repo.Write("old/name.txt", blameBase)
		repo.Git("add", "-A")
		c1 := repo.CommitAs("Ada Example", "ada@example.com", "Add the file")
		repo.Git("mv", "old/name.txt", "new-name.txt")
		repo.CommitAs("Grace Hopper", "grace@example.com", "Rename the file")
		w := startBlameWorld(t, e, repo, nil, []string{c1})
		w.open("new-name.txt")
		w.move(1)
		matches(t, w.texts()[1], `Add the file, Ada Example `+agoText)
	})
	t.Run("the boundary of a shallow clone", func(t *testing.T) {
		e := gittest.NewEnv(t)
		origin := e.Init("origin")
		origin.Write(blameFile, blameBase)
		origin.Git("add", "-A")
		origin.CommitAs("Ada Example", "ada@example.com", "Add the file")
		origin.Write(blameFile, "one\ntwo\nTHREE\nfour\nfive\nsix\n")
		origin.Git("add", "-A")
		origin.CommitAs("Grace Hopper", "grace@example.com", "Change a line")
		dir := filepath.Join(e.Work, "shallow")
		e.Git(e.Work, "clone", "-q", "--depth", "1", "file://"+origin.Dir, dir)
		e.Trust(dir)
		clone := &gittest.Repo{Env: e, Dir: dir}
		w := startBlameWorld(t, e, clone, nil, nil)
		w.move(0)
		// every line of a shallow root is a boundary of the history that git knows: it names the commit that is there
		matches(t, w.texts()[0], `Change a line, Grace Hopper `+agoText)
	})
}

func TestTheHoverOfTheBlameTextHasTheDetailsAndTheLinks(t *testing.T) {
	w := newBlameWorld(t, nil)
	w.move(3)
	an := w.r.api.Annotations(w.uri)[0]
	for _, want := range []string{"**Grace Hopper**", "mailto:grace@example.com", "Shout two lines", "The body of the message.", "1 file changed", "[Open Commit](command:git.viewCommit?", "[Copy Hash](command:git.copyContentToClipboard?"} {
		if !strings.Contains(an.Hover, want) {
			t.Errorf("the hover does not have %q:\n%s", want, an.Hover)
		}
	}
	if strings.Contains(an.Hover, "$(") {
		t.Errorf("the hover has icon names the renderer does not know:\n%s", an.Hover)
	}
	// the links run the commands of the extension
	linkOf := func(label string) string {
		m := regexp.MustCompile(`\[` + label + `\]\((command:[^ )]+)`).FindStringSubmatch(an.Hover)
		if m == nil {
			t.Fatalf("no %s link in %s", label, an.Hover)
		}
		return m[1]
	}
	open := linkOf("Open Commit")
	id, args, err := extension.ParseCommandLink(open)
	if err != nil || id != "git.viewCommit" {
		t.Fatalf("the Open Commit link: %q %v %v", id, args, err)
	}
	if m, _ := args.(map[string]any); m["id"] != w.commits[1] || m["revealUri"] != w.uri || m["root"] != w.repo.Dir {
		t.Errorf("Open Commit opens %v", args)
	}
	w.r.api.FollowAnnotationLink(w.uri, 3, linkOf("Copy Hash"))
	if got := w.r.api.ClipboardTexts(); len(got) != 1 || got[0] != w.commits[1] {
		t.Errorf("Copy Hash copied %v, want %s", got, w.commits[1])
	}
	// Open Commit opens the multi-file diff editor of that commit, scrolled to the file of the blame (git.viewCommit, step 24.17)
	w.r.api.FollowAnnotationLink(w.uri, 3, open)
	w.r.api.WaitFor("the editor of the commit", func() bool { w.r.api.Pump(); return len(w.r.api.OpenedMultiDiffs()) == 1 })
	opened := w.r.api.OpenedMultiDiffs()[0]
	if opened.Key != commitKey(w.r.repo(w.repo.Dir).id, w.commits[1]) || !strings.Contains(opened.Title, "Shout two lines") || !strings.Contains(opened.Reveal, blameFile) {
		t.Errorf("Open Commit opened %+v, want the commit %s revealed at %s", opened, w.commits[1], blameFile)
	}
	// a link to another extension's command, and the Settings gear of VS Code, are not followed
	before := len(w.r.api.Notes())
	w.r.api.FollowAnnotationLink(w.uri, 3, "command:workbench.action.openSettings?"+url.QueryEscape(`["git.blame"]`))
	if len(w.r.api.Notes()) != before {
		t.Error("a command of the core was run from the hover")
	}
}

func TestTheHoverCanBeSwitchedOff(t *testing.T) {
	w := newBlameWorld(t, map[string]any{"git.blame.editorDecoration.disableHover": true})
	w.move(3)
	an := w.r.api.Annotations(w.uri)
	if len(an) != 1 || an[0].Hover != "" || an[0].Text == "" {
		t.Errorf("annotations %+v", an)
	}
}

func TestTheHoverEscapesTheNamesOfPeople(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("blame")
	repo.Write(blameFile, blameBase)
	repo.Git("add", "-A")
	c1 := repo.CommitAs("Ada *[the] Great*", "ada@example.com", "Add ![image](http://x) and `code`")
	w := startBlameWorld(t, e, repo, nil, []string{c1})
	w.move(0)
	hover := w.r.api.Annotations(w.uri)[0].Hover
	if !strings.Contains(hover, `Ada \*\[the\] Great\*`) || strings.Contains(hover, "![image]") {
		t.Errorf("hover %q", hover)
	}
}

func TestTheTogglesFlipTheirSettings(t *testing.T) {
	w := newBlameWorld(t, nil)
	if err := w.r.api.Run("git.blame.toggleEditorDecoration", nil); err != nil {
		t.Fatal(err)
	}
	if err := w.r.api.Run("git.blame.toggleStatusBarItem", nil); err != nil {
		t.Fatal(err)
	}
	got := w.r.api.SettingUpdates()
	want := []fakeUpdate{{"git.blame.editorDecoration.enabled", false}, {"git.blame.statusBarItem.enabled", false}}
	if len(got) < 2 {
		t.Fatalf("updates %v", got)
	}
	for i, u := range want {
		if got[len(got)-2+i].Key != u.key || got[len(got)-2+i].Value != u.value {
			t.Errorf("update %d is %v, want %v", i, got[len(got)-2+i], u)
		}
	}
}

type fakeUpdate struct {
	key   string
	value any
}

func TestAScopeWithoutEditorsOrStatusBarGetsNoWarning(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("blame")
	repo.Write(blameFile, blameBase)
	repo.Git("add", "-A")
	repo.Commit("Add the file")
	var logs lockedBuffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	r := newRig(t, e, rigOptions{folder: repo.Dir, logger: logger, settings: map[string]any{"git.blame.editorDecoration.enabled": true}})
	r.api.SetEditorDecorationsUnavailable(true)
	r.settle()
	w := &blameWorld{t: t, r: r, repo: repo}
	w.open(blameFile)
	w.move(0)
	if logs.String() != "" {
		t.Errorf("the extension warned:\n%s", logs.String())
	}
	if len(w.texts()) != 0 {
		t.Errorf("annotations in a scope with no editors: %v", w.texts())
	}
	if item := r.api.StatusItem(blameKey); item == nil {
		t.Error("the status bar item is not there")
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func TestTheBlameOfAFileThatCannotBeBlamedIsLoggedOnce(t *testing.T) {
	w := newBlameWorld(t, nil)
	// git fails for a reason that is not a missing path: the object database is damaged
	if err := os.RemoveAll(filepath.Join(w.repo.Dir, ".git", "objects")); err != nil {
		t.Fatal(err)
	}
	w.move(0)
	w.move(1)
	w.move(2)
	if len(w.texts()) != 0 {
		t.Errorf("a text for a broken repository: %v", w.texts())
	}
	n := 0
	for _, l := range w.r.api.Channel(ChannelName).Lines() {
		if strings.HasPrefix(l, "git blame failed") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the failure was reported %d times", n)
	}
}

func TestTurningGitOffTakesTheBlameTextAway(t *testing.T) {
	w := newBlameWorld(t, nil)
	w.move(2)
	if len(w.texts()) != 1 {
		t.Fatal("no text to start with")
	}
	w.r.api.SetSetting("git.enabled", false)
	w.r.clock.Advance(blameDelay)
	w.r.api.WaitFor("the text to go", func() bool { w.r.api.Pump(); return w.settled() && len(w.texts()) == 0 })
	if item := w.r.api.StatusItem(blameKey); item == nil || !item.Hidden {
		t.Errorf("the status item stays: %+v", item)
	}
}
