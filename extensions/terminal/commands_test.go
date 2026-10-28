package terminal

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fileURI is the URI of a path.
func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// inEditor tells the extension, as the core events do, that the editor in front shows a document with a selection (zero-based
// lines and byte columns).
func (r *extRig) inEditor(uri, text string, anchor, active extension.Position) {
	r.t.Helper()
	r.api.SetDocument(uri, text)
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "shellscript"})
	r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: []extension.Selection{{Anchor: anchor, Active: active}}})
	r.api.Pump()
}

// typed waits until the active terminal shows the text.
func (r *extRig) typed(want string) {
	r.t.Helper()
	waitText(r.t, r.ext.activeEntry().term, want)
}

func TestRunSelectedTextSendsTheSelectionOrTheLineToTheTerminal(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		anchor, active extension.Position
		want           []string
	}{
		{"a selection of one line", "echo one\necho two\n", extension.Position{Line: 0, Col: 0}, extension.Position{Line: 0, Col: 8}, []string{"one"}},
		{"a selection of two lines", "echo one\necho two\necho three\n", extension.Position{Line: 0, Col: 0}, extension.Position{Line: 1, Col: 8}, []string{"one", "two"}},
		{"a selection made backward", "echo one\necho two\n", extension.Position{Line: 1, Col: 8}, extension.Position{Line: 1, Col: 0}, []string{"two"}},
		{"no selection: the line of the cursor, trimmed", "echo one\n   echo two   \necho three\n", extension.Position{Line: 1, Col: 5}, extension.Position{Line: 1, Col: 5}, []string{"two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			r.run(CmdNew, nil)
			r.typed("$")
			r.inEditor("file:///project/run.sh", tt.text, tt.anchor, tt.active)
			r.run(CmdRunSelectedText, nil)
			for _, w := range tt.want {
				r.typed("\n" + w + "\n")
			}
			got := allText(r.ext.activeEntry().term)
			if strings.Contains(got, "three") {
				t.Errorf("text outside the selection ran:\n%s", got)
			}
			// the panel shows with the keyboard staying in the editor
			if v := r.api.View(ViewID); len(v.Reveals) == 0 || v.Reveals[len(v.Reveals)-1] {
				t.Errorf("reveals (with focus) %v: the last must not take the keyboard", v.Reveals)
			}
		})
	}
}

func TestRunSelectedTextWithoutAnEditorOrATerminal(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdRunSelectedText, nil)
	if n, _ := r.counts(); n != 0 {
		t.Errorf("a terminal was made with no editor: %d", n)
	}
	// with an editor and no terminal one is made
	r.inEditor("file:///project/run.sh", "echo made\n", extension.Position{}, extension.Position{Line: 0, Col: 10})
	r.run(CmdRunSelectedText, nil)
	if n, _ := r.counts(); n != 1 {
		t.Fatalf("%d terminals after running a selection with none open", n)
	}
	r.typed("made")
	// closing the document makes the command forget it
	r.api.Emit(extension.TopicDocumentClosed, extension.DocumentClosed{URI: "file:///project/run.sh"})
	r.api.Pump()
	if r.ext.editor.uri != "" {
		t.Errorf("the closed document is still the editor: %+v", r.ext.editor)
	}
}

func TestRunActiveFileSendsItsQuotedPath(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	r.typed("$")
	path := filepath.Join(r.dir, "my script.sh")
	r.inEditor(fileURI(path), "echo hi\n", extension.Position{}, extension.Position{})
	r.run(CmdRunActiveFile, nil)
	r.typed("'" + path + "'")
	// a document that is not a file on disk cannot be run
	r.inEditor("untitled:1", "echo hi\n", extension.Position{}, extension.Position{})
	r.run(CmdRunActiveFile, nil)
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "Only files on disk") {
		t.Errorf("notifications = %+v", notes)
	}
}

func TestPreparePathForShell(t *testing.T) {
	tests := []struct {
		name, path, shell, title, want string
	}{
		{"a plain path", "/home/me/run.sh", "/bin/bash", "bash", `'/home/me/run.sh'`},
		{"a space", "/home/me/my script.sh", "/bin/zsh", "zsh", `'/home/me/my script.sh'`},
		{"a single quote", "/home/me/it's.sh", "/bin/bash", "bash", `'/home/me/it'\''s.sh'`},
		{"both quotes", `/home/me/it's "a".sh`, "/bin/bash", "bash", `$'/home/me/it\'s "a".sh'`},
		{"fish and a single quote", "/home/me/it's.sh", "/usr/bin/fish", "fish", `'/home/me/it\'s.sh'`},
		{"the characters a shell acts on", "/home/me/a$b;c&d.sh", "/bin/bash", "bash", `'/home/me/abcd.sh'`},
		{"a control character", "/home/me/a\rb.sh", "/bin/bash", "bash", `'/home/me/ab.sh'`},
		{"a backslash", `/home/me/a\b.sh`, "/bin/bash", "bash", `'/home/me/a\\b.sh'`},
		{"pwsh with a space", "/home/me/my script.ps1", "/usr/bin/pwsh", "pwsh", `& '/home/me/my script.ps1'`},
		{"pwsh with parentheses", "/home/me/a(1).ps1", "/usr/bin/pwsh", "pwsh", `& '/home/me/a(1).ps1'`},
		{"pwsh with a quote", "/home/me/it's.ps1", "/usr/bin/pwsh", "pwsh", `& '/home/me/it''s.ps1'`},
		{"pwsh plain", "/home/me/run.ps1", "/usr/bin/pwsh", "pwsh", `'/home/me/run.ps1'`},
		{"no shell known", "/home/me/run.sh", "", "", `/home/me/run.sh`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := preparePathForShell(tt.path, tt.shell, tt.title); got != tt.want {
				t.Errorf("preparePathForShell(%q, %q) = %q, want %q", tt.path, tt.shell, got, tt.want)
			}
		})
	}
}

func TestSendSequenceSendsTheTextAsItIs(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	r.typed("$")
	// the argument {text}, with the variables of the workspace; no Enter is added
	r.run(CmdSendSequence, map[string]any{"text": "echo in ${workspaceFolderBasename} ${env:EMETOS_NOT_SET}|${nothing}"})
	line := "echo in " + filepath.Base(r.ext.folder) + " |${nothing}"
	r.typed(line)
	if strings.Contains(allText(r.ext.activeEntry().term), "\r\nin ") {
		t.Error("the sequence ran: Enter was added")
	}
	// with no argument the user is asked, and \n, \r and \xAB are understood
	r.run(CmdSendSequence, nil)
	in := r.api.LastInput()
	if in == nil || !strings.Contains(in.Options().Prompt, "Enter sequence to send") {
		t.Fatalf("the input box = %+v", in)
	}
	in.Accept(strings.Repeat(`\x7f`, len(line)) + `echo seq\r`) // the shell erases the line first
	r.typed("\nseq\n")
	// no terminal: nothing happens and nothing is made
	r.run(CmdKill, nil)
	r.run(CmdSendSequence, map[string]any{"text": "x"})
	if n, _ := r.counts(); n != 0 {
		t.Errorf("%d terminals after sending a sequence to none", n)
	}
}

func TestRenameAsksForTheNameAndRenameWithArgsTakesIt(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	// the title follows the foreground process, which the pty reports on its own goroutine: the box shows the title of the
	// moment the command ran, which is the one just before or just after it
	before := r.ext.title(r.ext.activeEntry())
	r.run(CmdRename, nil)
	in := r.api.LastInput()
	if in == nil || in.Options().Prompt != "Enter terminal name" ||
		in.Options().Value != before && in.Options().Value != r.ext.title(r.ext.activeEntry()) {
		t.Fatalf("the input box = %+v, the title was %q", in, before)
	}
	in.Accept("builder")
	if got := r.ext.title(r.ext.activeEntry()); got != "builder" {
		t.Errorf("the tab is %q", got)
	}
	// an empty name changes nothing
	r.run(CmdRename, nil)
	r.api.LastInput().Accept("  ")
	r.run(CmdRename, nil)
	r.api.LastInput().Cancel()
	if got := r.ext.title(r.ext.activeEntry()); got != "builder" {
		t.Errorf("the tab is %q after an empty name and a cancel", got)
	}
	r.run(CmdRenameWithArgs, map[string]any{"name": "tests"})
	if got := r.ext.title(r.ext.activeEntry()); got != "tests" {
		t.Errorf("the tab is %q after renameWithArgs", got)
	}
	r.run(CmdRenameWithArgs, nil)
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Message != "No name argument provided" {
		t.Errorf("notifications = %+v", notes)
	}
}

func TestSelectDefaultShellWritesTheSettingOfTheUser(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdSelectDefaultProfile, nil)
	pick := r.api.LastPick()
	if pick == nil || pick.Options().Placeholder != "Select your default terminal profile" || len(pick.Labels()) != 1 {
		t.Fatalf("the quick pick = %+v", pick)
	}
	if !strings.Contains(pick.Options().Items[0].Label, "test") || pick.Options().Items[0].Detail != "default" {
		t.Errorf("the item = %+v", pick.Options().Items[0])
	}
	pick.Choose(0)
	ups := r.api.SettingUpdates()
	if len(ups) != 1 || ups[0].Key != settingDefaultProfile || ups[0].Value != "test" {
		t.Errorf("the settings written = %+v", ups)
	}
}

func TestTheFindCommandsOpenCloseAndMoveInTheFindWidget(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	r.typed("$")
	for _, c := range []string{"echo needle one", "echo hay", "echo needle two"} {
		r.run(CmdSendSequence, map[string]any{"text": c + "\r"})
	}
	r.typed("\nneedle two\n")
	if r.api.ContextKey(keyFindVisible) != false {
		t.Errorf("terminalFindVisible = %v before the find", r.api.ContextKey(keyFindVisible))
	}
	r.run(CmdFocusFind, nil)
	pv := r.ext.activeEntry().pv
	if !pv.find.visible || r.api.ContextKey(keyFindVisible) != true {
		t.Fatalf("the find widget is not shown: %v, terminalFindVisible %v", pv.find.visible, r.api.ContextKey(keyFindVisible))
	}
	pv.findTextChanged("needle") // the widget is not on screen in this test: the input does not tell
	r.api.WaitFor("the search", func() bool { r.api.Pump(); return pv.find.searched })
	if len(pv.find.matches) != 4 || pv.find.cur != 3 { // the typed lines and the outputs
		t.Fatalf("%d matches, current %d", len(pv.find.matches), pv.find.cur)
	}
	// Find Previous (Enter in the input) goes up, Find Next down
	r.run(CmdFindPrevious, nil)
	if pv.find.cur != 2 {
		t.Errorf("current = %d after Find Previous", pv.find.cur)
	}
	r.run(CmdFindNext, nil)
	r.run(CmdFindNext, nil)
	if pv.find.cur != 0 {
		t.Errorf("current = %d after Find Next around the end", pv.find.cur)
	}
	// the switches
	r.run(CmdToggleFindCaseSens, nil)
	r.run(CmdToggleFindWholeWord, nil)
	r.run(CmdToggleFindRegex, nil)
	if !pv.find.query.matchCase || !pv.find.query.wholeWord || !pv.find.query.regex {
		t.Errorf("the switches = %+v", pv.find.query)
	}
	r.run(CmdHideFind, nil)
	if pv.find.visible || r.api.ContextKey(keyFindVisible) != false || r.api.ContextKey(keyFindInputFocus) != false {
		t.Errorf("the widget stays: visible %v, terminalFindVisible %v", pv.find.visible, r.api.ContextKey(keyFindVisible))
	}
	// Find Next with the widget closed opens it
	r.run(CmdFindNext, nil)
	if !pv.find.visible {
		t.Error("Find Next did not open the widget")
	}
}

func TestFocusAtIndexFocusesTheTerminalsInTheOrderOfTheList(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	r.run(CmdNew, nil)
	r.run(CmdSplit, nil)
	all := r.ext.all()
	for i := 1; i <= len(all); i++ {
		r.run(focusAtIndex(i), nil)
		if r.ext.activeEntry() != all[i-1] {
			t.Errorf("focusAtIndex%d focused another terminal", i)
		}
	}
	r.run(focusAtIndex(9), nil) // none there: nothing happens
}

func TestLinksOpenFilesAndAddressesAndAskForSchemesThatAreNotAllowed(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	file := filepath.Join(r.ext.folder, "a b.go")
	r.ext.openFile(file, 7, none)
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != "file://"+strings.ReplaceAll(file, " ", "%20") || shown[0].Line != 7 || shown[0].Column != 1 || shown[0].Preview {
		t.Errorf("shown = %+v", shown)
	}
	// an address the output printed opens, and so does a hyperlink of an allowed scheme
	r.ext.openLink("https://example.com/a", false)
	r.ext.openLink("mailto:a@example.com", true)
	// a hyperlink of a scheme that is not allowed asks first
	r.ext.openLink("ssh://host/path", true)
	if got := r.api.OpenedURIs(); len(got) != 2 {
		t.Fatalf("opened %q before the user allowed ssh", got)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || !strings.Contains(notes[0].Message, "scheme ssh") || len(notes[0].Actions) != 1 || notes[0].Actions[0] != "Allow ssh" {
		t.Fatalf("notifications = %+v", notes)
	}
	r.api.RunNoteAction(0, "Allow ssh")
	ups := r.api.SettingUpdates()
	if got := r.api.OpenedURIs(); len(got) != 3 || got[2] != "ssh://host/path" {
		t.Errorf("opened %q after the user allowed ssh", got)
	}
	if len(ups) != 1 || ups[0].Key != settingLinkSchemes {
		t.Fatalf("settings written = %+v", ups)
	}
	if list, _ := ups[0].Value.([]any); len(list) != 7 || list[6] != "ssh" {
		t.Errorf("the schemes = %v", ups[0].Value)
	}
	// a link with no scheme is ignored
	r.ext.openLink("example.com", true)
	if got := r.api.OpenedURIs(); len(got) != 3 {
		t.Errorf("opened %q for a link with no scheme", got)
	}
}

// selectNeedle selects the word "needle" of the line that the shell printed on its own ("xneedlex"), as a drag over it would.
func (r *extRig) selectNeedle() {
	r.t.Helper()
	pv := r.ext.activeEntry().pv
	pv.sendText("echo xneedlex", true, false)
	r.typed("\nxneedlex")
	em := pv.term.Emulator()
	for i := 0; i < em.LineCount(); i++ {
		if strings.TrimSpace(em.Text(i, i)) == "xneedlex" {
			pv.setSelection(selection{active: true, anchor: pos{int64(i), 1}, head: pos{int64(i), 6}})
			if got := pv.selectedText(); got != "needle" {
				r.t.Fatalf("the selection is %q", got)
			}
			return
		}
	}
	r.t.Fatalf("the line is not on the screen:\n%s", allText(r.ext.activeEntry().term))
}

func TestSearchWorkspaceRunsFindInFilesWithTheSelection(t *testing.T) {
	r := newExtRig(t, "")
	var got []extension.CommandsExecuteRequest
	fakeapi.OnRequestValue(r.api, extension.TopicCommandsExecute, func(_ context.Context, p extension.CommandsExecuteRequest) (any, error) {
		got = append(got, p)
		return extension.CommandsExecuteReply{Result: json.RawMessage("null")}, nil
	})
	// with no terminal, and with a terminal that has nothing selected, nothing is asked
	r.run(CmdSearchWorkspace, nil)
	r.run(CmdNew, nil)
	r.typed("$")
	r.run(CmdSearchWorkspace, nil)
	if len(got) != 0 {
		t.Fatalf("a search was asked for with nothing selected: %+v", got)
	}
	r.selectNeedle()
	r.run(CmdSearchWorkspace, nil)
	if len(got) != 1 || got[0].ID != "workbench.action.findInFiles" {
		t.Fatalf("the requests = %+v", got)
	}
	var args struct {
		Query         string `json:"query"`
		TriggerSearch bool   `json:"triggerSearch"`
	}
	if err := json.Unmarshal(got[0].Args, &args); err != nil || args.Query != "needle" || !args.TriggerSearch {
		t.Errorf("the arguments are %s (%v)", got[0].Args, err)
	}
	if notes := r.api.Notes(); len(notes) != 0 {
		t.Errorf("notifications = %+v", notes)
	}
}

func TestSearchWorkspaceSaysWhenTheSearchIsNotThere(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantNote bool
	}{
		{"the Search extension is off", errors.New("CoreCommandNotFound", "no command has this id", "command", "workbench.action.findInFiles"), true},
		{"the command is disabled", errors.New("CoreCommandDisabled", "no handler", "command", "workbench.action.findInFiles"), true},
		{"another failure is only logged", errors.New("CoreCommandFailed", "it failed"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			fakeapi.OnRequestValue(r.api, extension.TopicCommandsExecute, func(context.Context, extension.CommandsExecuteRequest) (any, error) {
				return nil, tt.err
			})
			r.run(CmdNew, nil)
			r.typed("$")
			r.selectNeedle()
			r.run(CmdSearchWorkspace, nil)
			notes := r.api.Notes()
			if tt.wantNote != (len(notes) == 1 && notes[0].Severity == "warn" && strings.Contains(notes[0].Message, "Search extension")) {
				t.Errorf("notifications = %+v", notes)
			}
		})
	}
}

func TestTheCommandsFindTheEditorInFrontWithoutAnEvent(t *testing.T) {
	// the extension is activated by the command, after the editor events it would have followed: the window tells it
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	r.typed("$")
	r.api.SetDocument("file:///project/run.sh", "echo one\necho from the window\n")
	r.api.SetActive(&extension.ActiveDocument{URI: "file:///project/run.sh", Selections: []extension.Selection{{Anchor: extension.Position{Line: 1}, Active: extension.Position{Line: 1, Col: 20}}}})
	r.run(CmdRunSelectedText, nil)
	r.typed("\nfrom the window\n")
	if got := r.ext.editor.uri; got != "file:///project/run.sh" {
		t.Errorf("the editor is %q", got)
	}
}
