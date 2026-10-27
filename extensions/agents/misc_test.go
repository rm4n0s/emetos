package agents

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func TestTerminalTextDropsEscapeSequencesAndKeepsTheText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"plain lines", []string{"one\ntwo\n"}, "one\ntwo"},
		{"crlf", []string{"one\r\ntwo\r\n"}, "one\ntwo"},
		{"colors", []string{"\x1b[1;32mgreen\x1b[0m text\n"}, "green text"},
		{"a sequence cut by the end of a chunk", []string{"a\x1b[3", "1mb\n"}, "ab"},
		{"a window title", []string{"\x1b]0;title\x07shown\n"}, "shown"},
		{"a window title ended by ESC backslash", []string{"\x1b]0;title\x1b\\shown\n"}, "shown"},
		{"a carriage return starts the line again", []string{"loading 10%\rloading 90%\rdone\n"}, "done"},
		{"a backspace erases", []string{"abc\b\bX\n"}, "aX"},
		{"a backspace over a multibyte character", []string{"añ\bb\n"}, "ab"},
		{"tabs", []string{"a\tb\n"}, "a    b"},
		{"the unfinished line is shown", []string{"done\nPaste the code > "}, "done\nPaste the code > "},
		{"control characters", []string{"a\x00\x07b\x7f\n"}, "ab"},
		{"a two-byte sequence", []string{"a\x1b=b\n"}, "ab"},
		{"invalid UTF-8", []string{"a\xffb\n"}, "a?b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var tt2 terminalText
			for _, c := range tt.chunks {
				tt2.write([]byte(c))
			}
			if got := tt2.text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalTextKeepsABoundedAmountOfOutput(t *testing.T) {
	var tt terminalText
	for i := range maxTerminalLines + 50 {
		tt.write([]byte("line " + string(rune('a'+i%26)) + "\n"))
	}
	if len(tt.lines) != maxTerminalLines {
		t.Errorf("%d lines kept, want %d", len(tt.lines), maxTerminalLines)
	}
	tt.write([]byte(strings.Repeat("x", maxTerminalLine*3) + "\n"))
	if got := tt.lines[len(tt.lines)-1]; len(got) != maxTerminalLine {
		t.Errorf("a long line is %d bytes, want %d", len(got), maxTerminalLine)
	}
}

func TestLoginEnvironmentChangesOnlyTheTerminal(t *testing.T) {
	got := loginEnvironment([]string{"A=1", "TERM=xterm", "NO_COLOR=0", "CLAUDE_CONFIG_DIR=x", "B=2"})
	// the environment of the user reaches the program as it is; the adapter does not add or remove anything else
	want := []string{"A=1", "CLAUDE_CONFIG_DIR=x", "B=2", "TERM=dumb", "NO_COLOR=1"}
	if !slices.Equal(got, want) {
		t.Errorf("env = %q, want %q", got, want)
	}
}

func TestThePtyErrorsHaveTheirOwnRoutes(t *testing.T) {
	// a file that is not a terminal master cannot be unlocked
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	_, err = openSlave(f)
	route(t, err, "openSlave.PtyOpenFailed")

	// a program that does not exist
	_, err = startPty(t.Context(), filepath.Join(t.TempDir(), "nope"), nil, "", os.Environ())
	route(t, err, "startPty.ProcessStartFailed")

	// writing to a terminal that is closed
	p, err := startPty(t.Context(), "/bin/sh", []string{"-c", "exit 0"}, "", os.Environ())
	if err != nil {
		t.Skipf("no /bin/sh: %v", err)
	}
	<-p.done
	p.pump(func([]byte) {}) // drains and closes the master
	route(t, p.write([]byte("x")), "write.PtyWriteFailed")
	if p.exit != 0 {
		t.Errorf("exit = %d", p.exit)
	}
}

func TestAPtyProgramSeesATerminalAndItsOutputReachesThePump(t *testing.T) {
	p, err := startPty(t.Context(), "/bin/sh", []string{"-c", `test -t 0 && test -t 1 && echo tty; read line; echo "got:$line"`}, "", os.Environ())
	if err != nil {
		t.Skipf("no /bin/sh: %v", err)
	}
	var out terminalText
	pumped := make(chan struct{})
	go func() {
		p.pump(func(b []byte) { out.write(b) })
		close(pumped)
	}()
	if err := p.write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	<-p.done
	<-pumped
	if got := out.text(); got != "tty\ngot:hello" {
		t.Errorf("output = %q (the terminal does not echo what is written)", got)
	}
}

func TestToolSummaries(t *testing.T) {
	for _, tt := range []struct {
		tool, input, want string
	}{
		{"Read", `{"file_path":"/work/a/b.go"}`, "a/b.go"},
		{"Read", `{"file_path":"/elsewhere/b.go"}`, "/elsewhere/b.go"},
		{"Bash", `{"command":"ls -la\nrm x"}`, "ls -la ..."},
		{"Grep", `{"pattern":"TODO"}`, "TODO"},
		{"Glob", `{"pattern":"**/*.go","path":"/work/src"}`, "**/*.go in src"},
		{"WebFetch", `{"url":"https://example.invalid/x"}`, "https://example.invalid/x"},
		{"WebSearch", `{"query":"go generics"}`, "go generics"},
		{"Task", `{"description":"find the bug","prompt":"long"}`, "find the bug"},
		{"TodoWrite", `{"todos":[1,2,3]}`, "3 items"},
		{"Custom", `{"path":"/work/x"}`, "/work/x"},
		{"Custom", `{"nothing":1}`, ""},
		{"Read", `not json`, ""},
		{"Read", ``, ""},
	} {
		t.Run(tt.tool+" "+tt.want, func(t *testing.T) {
			if got := toolSummary(tt.tool, []byte(tt.input), "/work"); got != tt.want {
				t.Errorf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClipCutsAtACharacterAndSaysSo(t *testing.T) {
	if got := clip("short", 10); got != "short" {
		t.Errorf("clip = %q", got)
	}
	got := clip("ññññññ", 5) // 2 bytes each: the cut falls inside a character
	if !strings.HasPrefix(got, "ññ") || !strings.HasSuffix(got, "(cut)") || strings.Contains(got, "�") {
		t.Errorf("clip = %q", got)
	}
}

func TestTheConversationIsBoundedInMemoryAndInTheSavedState(t *testing.T) {
	tr := transcript{}
	for range maxItems + 20 {
		tr.add(&item{Kind: kindText, Text: strings.Repeat("x", 2000)})
	}
	if len(tr.items) != maxItems {
		t.Errorf("%d items kept, want %d", len(tr.items), maxItems)
	}
	saved := tr.saved(maxSnapshotBytes)
	if len(saved) == 0 || len(saved) >= maxItems || len(saved) > maxSnapshotBytes/2000 {
		t.Errorf("%d items saved", len(saved))
	}
	if saved[len(saved)-1] != tr.items[len(tr.items)-1] {
		t.Error("the newest item must be the last one saved")
	}
	tr2 := transcript{}
	tr2.add(&item{Kind: kindText, Text: strings.Repeat("y", maxItemText*2)})
	if len(tr2.items[0].Text) > maxItemText+20 {
		t.Errorf("an item of %d bytes was kept whole", len(tr2.items[0].Text))
	}
}

func TestCompletionOfCommandsAndFiles(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.commands = []commandInfo{{Name: "compact", Description: "Summarize", Hint: "[text]"}, {Name: "review"}, {Name: "init"}, {Name: "recompile"}}
	labels := func(c completion) []string {
		var out []string
		for _, it := range c.items {
			out = append(out, it.Label)
		}
		return out
	}
	for _, tt := range []struct {
		name, before string
		want         []string
	}{
		{"every command", "/", []string{"/compact", "/review", "/init", "/recompile"}},
		{"by prefix, then by content", "/re", []string{"/review", "/recompile"}},
		{"case does not matter", "/COM", []string{"/compact", "/recompile"}},
		{"no match", "/zzz", nil},
		{"a command with an argument no longer completes", "/compact now", nil},
		{"a slash that is not at the start", "look /re", nil},
		{"files by the start of the name", "@u", []string{"util.go", "util_test.go"}},
		{"files by the start of the path", "see @util/", []string{"util.go", "util_test.go"}},
		{"files by a part of the name", "@overv", []string{"overview.md"}},
		{"a file after text and a space", "read @mai", []string{"main.go"}},
		{"an at sign inside a word is not a mention", "mail me@exa", nil},
		{"the word is over", "@main.go and", nil},
		{"a new line starts a new word", "x\n@READ", []string{"README.md"}},
		{"nothing", "hello", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := labels(tab.completionFor(tt.before)); !slices.Equal(got, tt.want) {
				t.Errorf("completion = %q, want %q", got, tt.want)
			}
		})
	}
	// a long list is cut to the rows the list shows
	for i := range 20 {
		tab.commands = append(tab.commands, commandInfo{Name: "cmd" + string(rune('a'+i))})
	}
	if got := len(tab.completionFor("/").items); got != maxCompletions {
		t.Errorf("%d rows", got)
	}
	// a file shows its folder after its name
	c := tab.completionFor("@util_t")
	if len(c.items) != 1 || c.items[0].Detail != "util/" || c.items[0].Insert != "@util/util_test.go " {
		t.Errorf("item = %+v", c.items)
	}
}

func TestAcceptingACompletionReplacesTheTypedPartOfTheInput(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.commands = []commandInfo{{Name: "compact"}}
	tab.input.SetValue("/co")
	tab.inputChanged()
	if len(tab.comp.items) != 1 {
		t.Fatalf("list = %+v", tab.comp)
	}
	tab.acceptCompletion()
	if tab.input.Value() != "/compact " || len(tab.comp.items) != 0 {
		t.Errorf("input %q", tab.input.Value())
	}
	tab.input.SetValue("read @ma and more")
	tab.input.Select(8, 8) // the caret is after "@ma"
	tab.inputChanged()
	if len(tab.comp.items) != 1 || tab.comp.start != 5 || tab.comp.end != 8 {
		t.Fatalf("list = %+v", tab.comp)
	}
	tab.acceptCompletion()
	if tab.input.Value() != "read @main.go  and more" {
		t.Errorf("input %q", tab.input.Value())
	}
	// a list that is stale (the text changed) takes nothing
	tab.comp = completion{items: []completionItem{{Insert: "x"}}, start: 90, end: 99}
	tab.acceptCompletion()
	if len(tab.comp.items) != 0 {
		t.Error("a stale list was kept")
	}
}

func TestReplayPlaysARecordedStreamIntoTheTabWithoutAProgram(t *testing.T) {
	r := newRig(t, "")
	// no program is wanted: the path of the setting points nowhere, so the new session is a tab with a missing program
	r.api.SetSetting(PathSetting, filepath.Join(r.folder, "none"))
	if err := r.api.Run(NewSessionCommand, nil); err != nil {
		t.Fatal(err)
	}
	tab := r.ext.tabs[0]
	if tab.phase != phaseMissing {
		t.Fatalf("phase = %v", tab.phase)
	}
	if err := r.api.Run(ReplayCommand, map[string]any{"fixture": proofFixture}); err != nil {
		t.Fatal(err)
	}
	if !tab.replay || tab.phase != phaseReady || tab.sess != nil || tab.busy {
		t.Errorf("replay %v phase %v", tab.replay, tab.phase)
	}
	want := []string{
		"user:Explain what main.go does and add a doc comment to main.", "thinking:I should read main.go before I say anything about it.",
		"text:I'll start by reading `main.go`.", "tool:Read:/work/greeter/main.go",
	}
	if got := texts(tab); len(got) != 8 || !slices.Equal(got[:4], want) {
		t.Errorf("the conversation is %q", got)
	}
	if got := rows(r); !slices.Equal(got, []string{"Explain what main.go does and add a doc comment to main."}) {
		t.Errorf("the list is %q", got)
	}
	if len(r.started()) != 0 {
		t.Error("a program ran")
	}
	// with no session the command opens one
	r2 := newRig(t, "")
	r2.api.SetSetting(PathSetting, filepath.Join(r2.folder, "none"))
	if err := r2.api.Run(ReplayCommand, map[string]any{"fixture": proofFixture}); err != nil {
		t.Fatal(err)
	}
	if len(r2.ext.tabs) != 1 || len(r2.ext.tabs[0].tr.items) != 8 {
		t.Errorf("tabs = %d", len(r2.ext.tabs))
	}
}

func TestReplayFailsWithTagsForABadFixture(t *testing.T) {
	r := newRig(t, "")
	r.openIdle("")
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	err := r.api.Run(ReplayCommand, nil)
	route(t, err, "replayCommand.ReplayFixtureInvalid")
	err = r.api.Run(ReplayCommand, map[string]any{"fixture": filepath.Join(dir, "missing.jsonl")})
	route(t, err, "replayCommand.ReplayFixtureUnreadable")
	err = r.api.Run(ReplayCommand, map[string]any{"fixture": write("bad.jsonl", "{\"type\":\"result\"}\nnot json\n")})
	route(t, err, "parseFixture.ReplayFixtureInvalid")
	err = r.api.Run(ReplayCommand, map[string]any{"fixture": write("shape.jsonl", `{"type":"assistant","message":{"content":3}}`)})
	route(t, err, "ParseLine->parseLine")
	route(t, err, "MessageInvalid")
	// directives of the fake program, empty lines and control messages are skipped
	events, err := parseFixture([]byte("\n{\"$\":\"wait\",\"for\":\"user\"}\n{\"type\":\"control_request\",\"request_id\":\"x\"}\n{\"type\":\"result\",\"subtype\":\"success\"}\n"))
	if err != nil || len(events) != 1 {
		t.Errorf("events = %v, err = %v", events, err)
	}
	if _, ok := events[0].msg.(*claude.ResultMessage); !ok {
		t.Errorf("event = %T", events[0].msg)
	}
}

func TestReplayNeedsASessionToPlayInto(t *testing.T) {
	r := newRig(t, "")
	r.ext.api = noEditorsAPI{r.api}
	err := r.api.Run(ReplayCommand, map[string]any{"fixture": proofFixture})
	if err == nil {
		t.Fatal("no error")
	}
	route(t, err, "replayCommand.ReplayNoSession")
}

// noEditorsAPI is an API whose editor tabs have no content, so that no session tab is ever made.
type noEditorsAPI struct{ extension.API }

func (noEditorsAPI) Editors() extension.Editors { return stubEditors{} }

type stubEditors struct{ extension.Editors }

func (stubEditors) Open(string, any, extension.EditorOpenOptions) (extension.EditorTab, error) {
	return &fakeapi.Tab{}, nil
}

func TestSendCommandSendsAMessageInTheSessionInFront(t *testing.T) {
	r := newRig(t, "chat.jsonl")
	tab := r.open()
	r.ready(tab)
	if err := r.api.Run(SendCommand, map[string]any{"text": "What is main.go?"}); err != nil {
		t.Fatal(err)
	}
	r.idle(tab)
	got := texts(tab)
	if len(got) != 5 || got[0] != "user:What is main.go?" || got[4] != "text:It is a Go file." {
		t.Errorf("the conversation is %q", got)
	}
}

func TestSendCommandFailsWithTags(t *testing.T) {
	r := newRig(t, "")
	err := r.api.Run(SendCommand, map[string]any{"text": "  "})
	route(t, err, "sendCommand.SendTextInvalid")
	err = r.api.Run(SendCommand, nil)
	route(t, err, "sendCommand.SendTextInvalid")
	err = r.api.Run(SendCommand, map[string]any{"text": "hello"})
	route(t, err, "sendCommand.SendNoSession")
}
