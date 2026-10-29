package redis

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

func pos(line, col int) extension.Position { return extension.Position{Line: line, Col: col} }

func TestLinesToRunPicksTheLinesOfTheSelection(t *testing.T) {
	text := "SET a 1\nSET b 2\n# comment\nGET a\n\nGET b"
	tests := []struct {
		name string
		sel  extension.Selection
		all  bool
		want []string
	}{
		{"a cursor takes its line", extension.Selection{Anchor: pos(1, 3), Active: pos(1, 3)}, false, []string{"SET b 2"}},
		{"a selection takes the lines it touches", extension.Selection{Anchor: pos(0, 4), Active: pos(1, 2)}, false, []string{"SET a 1", "SET b 2"}},
		{"a backward selection", extension.Selection{Anchor: pos(3, 1), Active: pos(2, 0)}, false, []string{"# comment", "GET a"}},
		{"a selection that ends at the start of a line leaves it out", extension.Selection{Anchor: pos(0, 0), Active: pos(2, 0)}, false, []string{"SET a 1", "SET b 2"}},
		{"an empty selection at the start of a line takes the line", extension.Selection{Anchor: pos(3, 0), Active: pos(3, 0)}, false, []string{"GET a"}},
		{"the last line", extension.Selection{Anchor: pos(5, 5), Active: pos(5, 5)}, false, []string{"GET b"}},
		{"a position past the end", extension.Selection{Anchor: pos(9, 0), Active: pos(9, 0)}, false, nil},
		{"everything", extension.Selection{}, true, strings.Split(text, "\n")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LinesToRun(text, tc.sel, tc.all); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if got := LinesToRun("A\r\nB", extension.Selection{}, true); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("CRLF = %q", got)
	}
}

// activate makes the file the active editor with a selection.
func (r *rig) activate(uri, text string, sel extension.Selection) {
	r.api.SetDocument(uri, text)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: RedisLanguageID, Version: 1, Selections: []extension.Selection{sel}})
}

func TestRunningAFileSendsItsCommandsToTheConsole(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.api.Pump()
	r.activate("file:///w/a.redis", "# demo\nPING\nGET greeting\nDBSIZE\n", extension.Selection{Anchor: pos(2, 0), Active: pos(2, 4)})
	if err := r.api.Run(CmdRun, nil); err != nil {
		t.Fatal(err)
	}
	c := r.ext.consoleFor(r.conn.ID)
	r.api.WaitFor("the run", func() bool { return !c.busy() && len(c.entries) >= 2 })
	if got := joined(c); got != "cmd db0> GET greeting\nreply \"hello world\"" {
		t.Errorf("console =\n%s", got)
	}
	if v := r.api.View(PanelViewID); v == nil || len(v.Reveals) == 0 {
		t.Error("the console was not shown")
	}
	c.clear()
	if err := r.api.Run(CmdRunAll, nil); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the run", func() bool { return !c.busy() && len(c.entries) >= 6 })
	if got := joined(c); got != "cmd db0> PING\nreply PONG\ncmd db0> GET greeting\nreply \"hello world\"\ncmd db0> DBSIZE\nreply (integer) 9" {
		t.Errorf("console =\n%s", got)
	}
}

func TestRunningAFileThatIsNotARedisFileSaysSo(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.api.SetActive(&extension.ActiveDocument{URI: "file:///w/a.txt", LanguageID: "plaintext"})
	if err := r.api.Run(CmdRun, nil); err != nil {
		t.Fatal(err)
	}
	r.api.SetActive(nil)
	if err := r.api.Run(CmdRunAll, nil); err != nil {
		t.Fatal(err)
	}
	notes := r.api.Notes()
	if len(notes) != 2 || !strings.Contains(notes[0].Message, "Open a .redis file") {
		t.Errorf("notes = %+v", notes)
	}
}

func TestRunningWithNoOpenConnectionOrNothingToRun(t *testing.T) {
	r := newRig(t, nil)
	r.activate("file:///w/a.redis", "PING", extension.Selection{})
	if err := r.api.Run(CmdRun, nil); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "No Redis connection is open") {
		t.Fatalf("notes = %+v", notes)
	}
	r.connect()
	r.api.Pump()
	r.activate("file:///w/b.redis", "# only a comment\n", extension.Selection{})
	if err := r.api.Run(CmdRunAll, nil); err != nil {
		t.Fatal(err)
	}
	r.activate("file:///w/c.redis", "", extension.Selection{})
	if err := r.api.Run(CmdRunAll, nil); err != nil {
		t.Fatal(err)
	}
	if c := r.ext.consoleFor(r.conn.ID); len(c.entries) != 0 {
		t.Errorf("console = %q", c.texts())
	}
}

func TestAFileAsksWhichConnectionWhenSeveralAreOpen(t *testing.T) {
	r := newRig(t, nil)
	second := fixtureServer()
	second.put(0, "other", &fakeKey{typ: TypeString, str: "from the second"})
	r.dial.servers["redis2.local:6379"] = second
	c2, _, err := r.hub.Save(context.Background(), dbconn.Connection{Name: "second", Engine: EngineID, Scope: dbconn.ScopeUser, Host: "redis2.local", Port: 6379,
		Options: map[string]string{OptSeparator: ":"}}, dbconn.PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	r.connect()
	p, err := r.hub.Prepare(context.Background(), c2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.hub.Connect(context.Background(), p, false); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	r.ext.target = "" // no tab chose one
	r.ext.setTarget("")
	r.activate("file:///w/a.redis", "GET other", extension.Selection{})
	// the status item names no connection yet
	r.ext.updateFileStatus()
	if it := r.api.StatusItem(statusID); it == nil || !strings.Contains(it.Text, "$(database)") {
		t.Fatalf("status = %+v", it)
	}
	if err := r.api.Run(CmdSelectConn, nil); err != nil {
		t.Fatal(err)
	}
	pick := r.api.LastPick()
	if pick == nil || len(pick.Labels()) != 2 {
		t.Fatalf("pick = %+v", pick)
	}
	idx := -1
	for i, l := range pick.Labels() {
		if l == "second" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("labels = %q", pick.Labels())
	}
	pick.Choose(idx)
	if it := r.api.StatusItem(statusID); it == nil || it.Text != "$(database) second" {
		t.Errorf("status = %+v", it)
	}
	if err := r.api.Run(CmdRunAll, nil); err != nil {
		t.Fatal(err)
	}
	c := r.ext.consoleFor(c2.ID)
	r.api.WaitFor("the run", func() bool { return !c.busy() && len(c.entries) >= 2 })
	if got := c.entries[len(c.entries)-1]; got.text != `"from the second"` {
		t.Errorf("last entry = %+v", got)
	}
	if r.ext.fileConn["file:///w/a.redis"] != c2.ID {
		t.Error("the choice was not kept")
	}
}

func TestTheStatusItemShowsTheConnectionOfARedisFileOnly(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.api.Pump()
	r.ext.updateFileStatus()
	if it := r.api.StatusItem(statusID); it != nil && !it.Hidden {
		t.Errorf("an item with no file: %+v", it)
	}
	r.activate("file:///w/a.redis", "PING", extension.Selection{})
	r.ext.updateFileStatus()
	it := r.api.StatusItem(statusID)
	if it == nil || it.Hidden || it.Text != "$(database) cache" || it.Command != CmdSelectConn {
		t.Fatalf("status = %+v", it)
	}
	r.api.SetActive(&extension.ActiveDocument{URI: "file:///w/a.go", LanguageID: "go"})
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "file:///w/a.go", LanguageID: "go"})
	r.api.Pump()
	if it := r.api.StatusItem(statusID); !it.Hidden {
		t.Errorf("an item for a Go file: %+v", it)
	}
}

func TestCompletionOffersTheCommandsAtTheStartOfALine(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.api.Pump()
	r.ext.setTarget(r.conn.ID)
	r.ext.ensureCommandDocs(r.conn.ID)
	r.api.WaitFor("the commands", func() bool { return len(r.ext.commandDocs(r.conn.ID)) == len(fakeDocs) })
	var reg *registered
	for _, p := range r.api.Providers() {
		if p.Feature == "completion" {
			reg = &registered{p.Provider.(extension.CompletionProvider), p.Selector}
		}
	}
	if reg == nil || !reflect.DeepEqual(reg.sel.Languages, []string{"redis"}) {
		t.Fatalf("provider = %+v", reg)
	}
	tests := []struct {
		name string
		text string
		pos  extension.Position
		want []string
	}{
		{"the start of a word", "SET a 1\nge", pos(1, 2), []string{"GET", "GETDEL", "GETEX"}},
		{"a capital word", "GETD", pos(0, 4), []string{"GETDEL"}},
		{"nothing typed offers every command", "", pos(0, 0), []string{"DEL", "GET", "GETDEL", "GETEX", "HGETALL", "PING", "SET"}},
		{"after the first word", "GET ke", pos(0, 6), nil},
		{"in a comment", "# ge", pos(0, 4), nil},
		{"a position past the text", "GET", pos(7, 0), nil},
		{"leading white space", "   pi", pos(0, 5), []string{"PING"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			list, err := reg.p.ProvideCompletion(context.Background(), extension.TextDocument{URI: "file:///w/a.redis", Text: tc.text}, tc.pos, extension.CompletionTrigger{})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, it := range list.Items {
				got = append(got, it.Label)
				if it.Range == nil || it.Range.End != tc.pos {
					t.Errorf("range of %s = %+v", it.Label, it.Range)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	list, _ := reg.p.ProvideCompletion(context.Background(), extension.TextDocument{Text: "GETD"}, pos(0, 4), extension.CompletionTrigger{})
	if len(list.Items) != 1 || list.Items[0].Detail != "Returns the string value of a key after deleting the key." || list.Items[0].Range.Start.Col != 0 {
		t.Errorf("item = %+v", list.Items)
	}
}

type registered struct {
	p   extension.CompletionProvider
	sel extension.DocumentSelector
}

func TestCompletionWithoutAConnectionUsesTheBuiltInNames(t *testing.T) {
	r := newRig(t, nil)
	items := CompletionItems(r.ext.docsForFile("file:///w/a.redis"), "zr", pos(0, 2))
	if len(items) < 10 || items[0].Label != "ZRANDMEMBER" {
		t.Errorf("items = %d, first %+v", len(items), items[0])
	}
}
