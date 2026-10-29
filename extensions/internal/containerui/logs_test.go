package containerui

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// linesOf are the texts of the lines a tab keeps.
func linesOf(s *logSession) []string {
	var out []string
	for _, l := range s.lines {
		out = append(out, l.text)
	}
	return out
}

// TestViewLogsFollowsAContainerInThePanel opens the tab of a container: its lines (stdout and stderr, told apart) come, a line the
// container writes later is followed, and a second View Logs shows the tab it has.
func TestViewLogsFollowsAContainerInThePanel(t *testing.T) {
	r := newRig(t, sampleEngine)
	id := r.id("shop-db-1")
	r.fake.Log(id, containerapi.StreamStdout, "database system is ready")
	r.fake.Log(id, containerapi.StreamStderr, "WARNING: a slow query")
	r.run("containers.viewLogs", key(keyOfContainer(id)))
	s := r.ext.logs.active
	if s == nil || s.title != "shop-db-1" || s.container != id {
		t.Fatalf("the tab: %+v", s)
	}
	r.wait("the first lines", func() bool { return len(s.lines) == 2 })
	if got := linesOf(s); !reflect.DeepEqual(got, []string{"database system is ready", "WARNING: a slow query"}) {
		t.Errorf("lines %v", got)
	}
	if s.lines[0].stderr || !s.lines[1].stderr {
		t.Errorf("the streams: %+v", s.lines)
	}
	r.fake.Log(id, containerapi.StreamStdout, "checkpoint complete")
	r.wait("the line that came later", func() bool { return len(s.lines) == 3 })
	if r.api.View(r.cfg.LogsViewID()).Reveals == nil {
		t.Error("the panel was not shown")
	}
	r.run("containers.viewLogs", key(keyOfContainer(id)))
	if len(r.ext.logs.sessions) != 1 {
		t.Errorf("a second tab was made: %d", len(r.ext.logs.sessions))
	}
	if r.api.ContextKey("emetos.containers.hasLogs") != true {
		t.Error("the key of the panel's buttons is not set")
	}
}

// TestTheLogFilterPauseAndClear holds the three controls of the tab: the filter keeps the lines that match, Pause holds the lines
// that come (and counts them) until Resume, and Clear empties the tab.
func TestTheLogFilterPauseAndClear(t *testing.T) {
	r := newRig(t, sampleEngine)
	id := r.id("shop-db-1")
	for i := 0; i < 5; i++ {
		r.fake.Log(id, containerapi.StreamStdout, "GET /page/"+strconv.Itoa(i))
	}
	r.fake.Log(id, containerapi.StreamStderr, "ERROR: boom")
	r.ext.viewLogs(id)
	s := r.ext.logs.active
	r.wait("six lines", func() bool { return len(s.lines) == 6 })

	s.filter.SetValue("error")
	s.refilter()
	if n := len(s.model.Rows()); n != 1 {
		t.Fatalf("filtered rows %d", n)
	}
	s.filter.SetValue("")
	s.refilter()
	if n := len(s.model.Rows()); n != 6 {
		t.Fatalf("rows %d", n)
	}

	r.run("logs.pause", nil)
	if r.api.ContextKey("emetos.containers.logsPaused") != true {
		t.Error("the paused key is not set")
	}
	r.fake.Log(id, containerapi.StreamStdout, "one while paused")
	r.fake.Log(id, containerapi.StreamStdout, "two while paused")
	r.wait("the lines to be held", func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.pending) == 2 })
	r.api.Pump()
	if len(s.lines) != 6 {
		t.Errorf("lines came in while paused: %d", len(s.lines))
	}
	r.run("logs.resume", nil)
	if len(s.lines) != 8 || r.api.ContextKey("emetos.containers.logsPaused") != false {
		t.Errorf("after Resume: %d lines", len(s.lines))
	}

	r.run("logs.clear", nil)
	if len(s.lines) != 0 || len(s.model.Rows()) != 0 {
		t.Errorf("after Clear: %d lines, %d rows", len(s.lines), len(s.model.Rows()))
	}
	r.fake.Log(id, containerapi.StreamStdout, "after the clear")
	r.wait("a new line", func() bool { return len(s.lines) == 1 })
}

// TestATabKeepsAtMostTenThousandLines feeds a tab more lines than it keeps: the oldest go.
func TestATabKeepsAtMostTenThousandLines(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.ext.viewLogs(r.id("shop-db-1"))
	s := r.ext.logs.active
	for i := 0; i < maxLogLines+250; i++ {
		r.ext.logs.add(s, logLine{text: "line " + strconv.Itoa(i)})
	}
	r.api.Pump()
	if len(s.lines) != maxLogLines {
		t.Fatalf("%d lines", len(s.lines))
	}
	if first, last := s.lines[0].text, s.lines[len(s.lines)-1].text; first != "line 250" || last != "line "+strconv.Itoa(maxLogLines+249) {
		t.Errorf("first %q, last %q", first, last)
	}
	if len(s.model.Rows()) != maxLogLines {
		t.Errorf("rows %d", len(s.model.Rows()))
	}
}

// TestClosingTheTabEndsItsStream closes the tab: its stream is cancelled, the key goes and the panel is hidden when it was the last.
func TestClosingTheTabEndsItsStream(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.ext.viewLogs(r.id("shop-db-1"))
	r.ext.viewLogs(r.id("shop-web-1"))
	if len(r.ext.logs.sessions) != 2 {
		t.Fatalf("sessions %d", len(r.ext.logs.sessions))
	}
	first := r.ext.logs.sessions[0]
	r.ext.logs.activate(first)
	r.run("logs.close", nil)
	if len(r.ext.logs.sessions) != 1 || r.ext.logs.active == nil || r.ext.logs.active.title != "shop-web-1" {
		t.Errorf("sessions %d, active %+v", len(r.ext.logs.sessions), r.ext.logs.active)
	}
	if first.cancel != nil {
		t.Error("the stream of the closed tab was not cancelled")
	}
	r.run("logs.close", nil)
	if r.ext.logs.active != nil || r.api.ContextKey("emetos.containers.hasLogs") != false || r.api.View(r.cfg.LogsViewID()).Hides != 1 {
		t.Errorf("active %+v, key %v, hides %d", r.ext.logs.active, r.api.ContextKey("emetos.containers.hasLogs"), r.api.View(r.cfg.LogsViewID()).Hides)
	}
	r.run("logs.close", nil) // nothing to close: nothing happens
}

// TestAStoppedContainersTabFollowsAgainWhenItStarts stops the container of a tab: the tab says so, and it follows the logs again
// by itself when the container starts (the event of the engine).
func TestAStoppedContainersTabFollowsAgainWhenItStarts(t *testing.T) {
	r := newRig(t, sampleEngine)
	id := r.id("shop-db-1")
	r.ext.viewLogs(id)
	s := r.ext.logs.active
	r.wait("following", func() bool { return s.status == "Following" })
	if err := r.fake.Client().StopContainer(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	r.wait("the end of the stream", func() bool { return s.ended })
	if !strings.Contains(s.status, "The container stopped") {
		t.Errorf("status %q", s.status)
	}
	if err := r.fake.Client().StartContainer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.wait("following again", func() bool { return !s.ended && s.status == "Following" })
}

// TestRunCommandShowsItsOutputInTheirOwnTab runs a command in a running container: the tab shows the command, its output (stdout
// and stderr) and the exit status; a command that fails shows its status.
func TestRunCommandShowsItsOutputInTheirOwnTab(t *testing.T) {
	r := newRig(t, sampleEngine)
	id := r.id("shop-web-1")
	r.run("containers.runCommand", key(keyOfContainer(id)))
	in := r.api.LastInput()
	if in.Validate(" ") == "" {
		t.Error("an empty command was accepted")
	}
	in.Accept("echo hello; echo oops >&2; exit 3")
	s := r.ext.logs.active
	if s.kind != sessionExec || s.title != "shop-web-1: echo hello; echo oops >&2; exit 3" {
		t.Fatalf("the tab: %+v", s)
	}
	r.wait("the end", func() bool { return s.ended })
	if got := linesOf(s); !reflect.DeepEqual(got, []string{"$ echo hello; echo oops >&2; exit 3", "hello", "oops"}) {
		t.Errorf("lines %v", got)
	}
	if !s.lines[2].stderr || s.status != "Exited with status 3" {
		t.Errorf("stderr %v, status %q", s.lines[2].stderr, s.status)
	}
	// a command on a container that does not run is not offered
	r.run("containers.runCommand", key(keyOfContainer(r.id("scratch"))))
	if !r.hasNote("There is no container to run a command in") && len(r.api.Picks()) == 0 {
		t.Errorf("notes %v", r.notes())
	}
}

// TestExploreFilesListsFoldersWhenTheyOpen opens the files of a running container: the root folder is listed through exec when the
// node opens, a folder is listed when it opens, and a file opens in a read-only tab of its language.
func TestExploreFilesListsFoldersWhenTheyOpen(t *testing.T) {
	r := newRig(t, sampleEngine)
	var listed recorder[string]
	r.fake.SetExec(func(cmd []string) (string, string, int) {
		listed.add(strings.Join(cmd, " "))
		switch strings.Join(cmd, " ") {
		case "ls -1Ap -- /":
			return "etc/\nusr/\nREADME\nbin/\n", "", 0
		case "ls -1Ap -- /etc/":
			return "hosts\nnginx/\n", "", 0
		case "cat -- /etc/hosts":
			return "127.0.0.1 localhost\n::1 localhost\n", "", 0
		case "ls -1Ap -- /usr/":
			return "", "ls: cannot open directory '/usr/': Permission denied\n", 1
		case "cat -- /README":
			return "a\x00b", "", 0
		}
		return "", "unexpected: " + strings.Join(cmd, " "), 127
	})
	id := r.id("shop-web-1")
	r.run("containers.exploreFiles", key(keyOfContainer(id)))
	v := r.view(secContainers)
	r.wait("the root folder", func() bool { return v.tree.Has(fileKey(id, "/etc/")) })
	var names []string
	for _, k := range v.tree.Children(keyOfContainer(id)) {
		n, _ := v.tree.Node(k)
		names = append(names, n.Label)
	}
	if want := []string{"bin", "etc", "usr", "README"}; !reflect.DeepEqual(names, want) {
		t.Errorf("entries %v, want %v", names, want)
	}
	if !v.tree.IsExpanded(keyOfContainer(id)) {
		t.Error("the container is not open")
	}

	v.tree.Expand(fileKey(id, "/etc/"))
	r.wait("the etc folder", func() bool { return v.tree.Has(fileKey(id, "/etc/hosts")) })

	// a list the container refuses is told, and the folder closes
	v.tree.Expand(fileKey(id, "/usr/"))
	r.wait("the refusal", func() bool { return r.hasNote("Permission denied") })
	if v.tree.IsExpanded(fileKey(id, "/usr/")) {
		t.Error("the folder that could not be listed stays open")
	}

	r.ext.openFile(id, "/etc/hosts")
	r.wait("the file's tab", func() bool { return len(r.api.EditorTabs()) == 1 })
	tab := r.api.EditorTabs()[0]
	if tab.Title() != "hosts (shop-web-1)" || tab.Hint() != "shop-web-1:/etc/hosts" {
		t.Errorf("the tab %q %q", tab.Title(), tab.Hint())
	}
	var st docState
	if err := jsonUnmarshalInto(tab.State(), &st); err != nil || st.Text != "127.0.0.1 localhost\n::1 localhost\n" {
		t.Errorf("state %+v %v", st, err)
	}
	r.ext.openFile(id, "/README")
	r.wait("the note about binary files", func() bool { return r.hasNote("is not a text file") })
	if len(r.api.EditorTabs()) != 1 {
		t.Error("a binary file was opened")
	}

	// a refresh keeps the folders that were listed (no new ls) and a container that stops loses them
	n := listed.len()
	r.ext.reload(resContainers)
	r.settle()
	if listed.len() != n || !v.tree.Has(fileKey(id, "/etc/hosts")) {
		t.Errorf("the folders were read again: %v", listed.all()[n:])
	}
	r.run("containers.refresh", nil) // Refresh forgets what was listed: the open folders are listed again
	r.wait("the folders listed again", func() bool { return listed.len() > n })
	if err := r.fake.Client().StopContainer(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	r.wait("the stop", func() bool { c, _ := r.ext.containerByID(id); return c.State != "running" })
	if _, ok := r.ext.files.get(rootKey(id)); ok {
		t.Error("the listing of a container that stopped was kept")
	}
	r.run("containers.exploreFiles", key(keyOfContainer(id)))
	if !r.hasNote("can be explored while it runs") && len(r.api.Picks()) == 0 {
		t.Errorf("notes %v", r.notes())
	}
}
