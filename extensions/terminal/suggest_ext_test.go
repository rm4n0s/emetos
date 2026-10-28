package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// suggestRigWith is the extension over the real bash with a PATH of its own: a folder with a fake git and the other programs the
// tests complete, so that the real git is never run and nothing of the machine decides what is offered.
func suggestRigWith(t *testing.T, quick map[string]any) (*extRig, *entry) {
	t.Helper()
	r := bashExt(t)
	bin := filepath.Join(r.dir, "fakebin")
	for _, name := range []string{"git", "gitk", "cat", "grep"} {
		putFile(t, filepath.Join(bin, name), 0o755)
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho fake "+name+" \"$@\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// the scripts of shell integration call these two
	for _, name := range []string{"uname", "sed"} {
		if real, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(real, filepath.Join(bin, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var vars []string
	for _, kv := range r.ext.env.vars {
		if !strings.HasPrefix(kv, "PATH=") {
			vars = append(vars, kv)
		}
	}
	r.ext.env.vars = append(vars, "PATH="+bin)
	if quick != nil {
		r.api.SetSetting(settingSugQuick, quick)
		r.ext.settingsChanged()
	}
	putFile(t, filepath.Join(r.dir, "a.txt"), 0o644)
	putFile(t, filepath.Join(r.dir, "b.txt"), 0o644)
	putFile(t, filepath.Join(r.dir, "sub", "c.go"), 0o644)
	e := r.openBash()
	e.pv.focused = true // the pane is on screen in the real thing; the list follows the focus
	r.ext.paneFocus = e
	r.ext.syncKeys()
	return r, e
}

// keys types text as the keys of a user would: each character on its own.
func (r *extRig) keys(e *entry, text string) {
	r.t.Helper()
	for _, ch := range text {
		e.pv.userInput([]byte(string(ch)))
		time.Sleep(5 * time.Millisecond)
		e.term.TakeChanged() // the painting of a frame
		r.api.Pump()
	}
}

// input is the line at the prompt.
func inputOf(e *entry) promptInput {
	var f = &e.pv.sug.frame
	return readPromptInput(e.term, f)
}

func (r *extRig) waitLine(e *entry, want string) {
	r.t.Helper()
	r.waitFor("the line "+want, func() bool { e.term.TakeChanged(); in := inputOf(e); return in.ok && in.value == want })
}

func (r *extRig) waitList(e *entry, what string, f func(*sugSession) bool) *sugSession {
	r.t.Helper()
	r.waitFor(what, func() bool { e.term.TakeChanged(); s := e.pv.sug.sess; return s != nil && f(s) })
	return e.pv.sug.sess
}

func (r *extRig) waitNoList(e *entry) {
	r.t.Helper()
	r.waitFor("the list to close", func() bool { e.term.TakeChanged(); return e.pv.sug.sess == nil })
	if v := r.api.ContextKey(keySuggestVisible); v != false {
		r.t.Errorf("%s = %v with the list closed", keySuggestVisible, v)
	}
}

func labelsIn(s *sugSession) []string {
	var out []string
	for _, it := range s.items {
		out = append(out, it.label)
	}
	return out
}

var quickOn = map[string]any{"commands": "on", "arguments": "on", "unknown": "off"}

func TestTypingACommandOffersItAndTabCompletesIt(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "gi")
	s := r.waitList(e, "the list for gi", func(s *sugSession) bool { return s.lc.cur.text == "gi" && len(s.items) > 0 })
	if got := labelsIn(s); got[0] != "git" || !slices.Contains(got, "gitk") {
		t.Fatalf("items = %q", got)
	}
	if v := r.api.ContextKey(keySuggestVisible); v != true {
		t.Errorf("%s = %v with the list open", keySuggestVisible, v)
	}
	r.run(CmdAcceptSuggest, nil)
	r.waitLine(e, "git")
	r.waitNoList(e)
	// the line is the command: run it
	e.pv.userInput([]byte("\r"))
	waitText(t, e.term, "fake git")
}

func TestTheSubcommandsOfASpecAreOffered(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "git che")
	s := r.waitList(e, "the list for git che", func(s *sugSession) bool { return s.lc.cur.text == "che" && len(s.items) > 0 })
	got := labelsIn(s)
	if got[0] != "checkout" || !slices.Contains(got, "cherry-pick") {
		t.Fatalf("items = %q", got)
	}
	if s.items[0].detail != "Switch branches or restore working tree files" {
		t.Errorf("the description of checkout = %q", s.items[0].detail)
	}
	r.run(CmdAcceptSuggest, nil)
	r.waitLine(e, "git checkout")
}

func TestTheFilesOfTheFolderAreOfferedForAnArgument(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "cat ")
	r.run(CmdTriggerSuggest, nil)
	s := r.waitList(e, "the files", func(s *sugSession) bool { return s.explicit && len(s.items) > 0 })
	got := labelsIn(s)
	for _, want := range []string{"./a.txt", "./b.txt", "./sub/", "."} {
		if !slices.Contains(got, want) {
			t.Errorf("%q is not offered: %q", want, got)
		}
	}
	// typing narrows the list
	r.keys(e, "b")
	s = r.waitList(e, "the list for b", func(s *sugSession) bool { return s.lc.cur.text == "b" && len(s.items) > 0 })
	if got := labelsIn(s); got[0] != "./b.txt" {
		t.Errorf("items = %q", got)
	}
	r.run(CmdAcceptSuggest, nil)
	r.waitLine(e, "cat ./b.txt")
}

func TestEscapeClosesTheList(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "gi")
	r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) > 0 })
	r.run(CmdHideSuggest, nil)
	r.waitNoList(e)
	if in := inputOf(e); in.value != "gi" {
		t.Errorf("the line = %q: closing the list changes nothing", in.value)
	}
}

func TestNothingOpensByItselfWithTheDefaultSettings(t *testing.T) {
	r, e := suggestRigWith(t, nil)
	r.keys(e, "gi")
	r.waitLine(e, "gi")
	time.Sleep(100 * time.Millisecond)
	r.api.Pump()
	if e.pv.sug.sess != nil {
		t.Fatalf("the list opened with quickSuggestions off")
	}
	// Ctrl+Space opens it
	if v := r.api.ContextKey(keySuggestEnabled); v != true {
		t.Errorf("%s = %v", keySuggestEnabled, v)
	}
	r.run(CmdTriggerSuggest, nil)
	s := r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) > 0 })
	if !s.explicit || labelsIn(s)[0] != "git" {
		t.Errorf("explicit = %v, items = %q", s.explicit, labelsIn(s))
	}
}

func TestEnterAcceptsOnlyARowTheUserChose(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "gi")
	r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) >= 2 })
	// partial mode: Enter is the shell's, the line "gi" runs (and is not found)
	r.run(CmdAcceptSuggestEnter, nil)
	r.waitNoList(e)
	r.waitFor("the line to run", func() bool { return len(e.term.si.Commands()) >= 1 })
	if got := e.term.si.Commands()[0].text; got != "gi" {
		t.Errorf("the command that ran = %q, want gi", got)
	}
	// after Down, Enter accepts the second row
	r.keys(e, "gi")
	s := r.waitList(e, "the list again", func(s *sugSession) bool { return len(s.items) >= 2 })
	second := s.items[1].label
	r.run(CmdSelectNextSuggest, nil)
	if e.pv.sug.sess.focus != 1 {
		t.Fatalf("focus = %d after Down", e.pv.sug.sess.focus)
	}
	r.run(CmdAcceptSuggestEnter, nil)
	r.waitLine(e, second)
}

func TestSelectionModes(t *testing.T) {
	for _, tt := range []struct {
		mode      string
		wantFocus int
		tabLine   string
	}{
		{"partial", 0, "git"},
		{"always", 0, "git"},
		{"never", -1, "gi"}, // Tab is the shell's (it completes by itself) until the user moves
	} {
		t.Run(tt.mode, func(t *testing.T) {
			r, e := suggestRigWith(t, quickOn)
			r.api.SetSetting(settingSugSelect, tt.mode)
			r.ext.settingsChanged()
			r.keys(e, "gi")
			s := r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) > 0 })
			if s.focus != tt.wantFocus {
				t.Errorf("focus = %d, want %d", s.focus, tt.wantFocus)
			}
			if tt.mode == "never" {
				r.run(CmdSelectNextSuggest, nil)
				if f := e.pv.sug.sess.focus; f != 0 {
					t.Errorf("focus after Down = %d, want 0", f)
				}
				r.run(CmdAcceptSuggest, nil)
				r.waitLine(e, "git")
				return
			}
			r.run(CmdAcceptSuggest, nil)
			r.waitLine(e, tt.tabLine)
		})
	}
}

func TestTheListFollowsWhatIsTypedAndAFolderOpensItAgain(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "gi")
	r.waitList(e, "the list for gi", func(s *sugSession) bool { return s.lc.cur.text == "gi" && len(s.items) > 0 })
	// a character that matches nothing closes the list
	r.keys(e, "x")
	r.waitLine(e, "gix")
	r.waitNoList(e)
	// Backspace edits the line; it does not open a closed list, typing does
	r.keys(e, "\x7f")
	r.waitLine(e, "gi")
	time.Sleep(100 * time.Millisecond)
	r.api.Pump()
	if e.pv.sug.sess != nil {
		t.Errorf("Backspace opened the list")
	}
	r.keys(e, "t")
	r.waitList(e, "the list for git", func(s *sugSession) bool { return s.lc.cur.text == "git" && len(s.items) > 0 })
	// Backspace with the list open follows it
	r.keys(e, "\x7f")
	r.waitList(e, "the list for gi", func(s *sugSession) bool { return s.lc.cur.text == "gi" && len(s.items) > 0 })
	// a folder: accepting it lists what is in it
	e.pv.userInput([]byte("\x15"))
	r.waitLine(e, "")
	r.waitNoList(e)
	r.keys(e, "cat su")
	r.waitList(e, "the list for su", func(s *sugSession) bool { return s.lc.cur.text == "su" && len(s.items) > 0 })
	r.run(CmdAcceptSuggest, nil)
	r.waitLine(e, "cat ./sub/")
	s := r.waitList(e, "the content of sub", func(s *sugSession) bool { return slices.Contains(labelsIn(s), "./sub/c.go") })
	if slices.Contains(labelsIn(s), "./a.txt") {
		t.Errorf("a file of the parent is in the list of sub: %q", labelsIn(s))
	}
}

func TestUpOnTheFirstRowIsTheShellsHistory(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.typeLine(e, "echo earlier", 1)
	r.keys(e, "ec")
	r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) > 0 })
	r.run(CmdSelectPrevSuggest, nil)
	r.waitNoList(e)
	r.waitLine(e, "echo earlier")
	// with the setting off Up moves to the last row
	r.api.SetSetting(settingSugUpArrow, false)
	r.ext.settingsChanged()
	e.pv.userInput([]byte("\x15")) // Ctrl+U clears the line
	r.waitLine(e, "")
	r.keys(e, "gi")
	s := r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) >= 2 })
	r.run(CmdSelectPrevSuggest, nil)
	if f := e.pv.sug.sess.focus; f != len(s.items)-1 {
		t.Errorf("focus = %d, want the last row", f)
	}
}

func TestTheHistoryOfTheSessionIsOffered(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.typeLine(e, "git status --short", 1)
	r.keys(e, "git st")
	s := r.waitList(e, "the list", func(s *sugSession) bool { return s.lc.cur.text == "st" && len(s.items) > 0 })
	if got := labelsIn(s); !slices.Contains(got, "git status --short") || !slices.Contains(got, "status") {
		t.Fatalf("items = %q", got)
	}
	for i, it := range s.items {
		if it.label == "git status --short" {
			r.run(CmdSelectNextSuggest, nil)
			for e.pv.sug.sess.focus != i {
				r.run(CmdSelectNextSuggest, nil)
			}
		}
	}
	r.run(CmdAcceptSuggest, nil)
	r.waitLine(e, "git status --short")
}

func TestTheDetailsPaneToggles(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "git che")
	r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) > 0 })
	r.run(CmdToggleSuggestDetails, nil)
	if !e.pv.sug.sess.details {
		t.Errorf("the details did not open")
	}
	r.run(CmdToggleSuggestDetails, nil)
	if e.pv.sug.sess.details {
		t.Errorf("the details did not close")
	}
}

func TestEnterTypedClosesTheListAndTheSettingsTurnItOff(t *testing.T) {
	r, e := suggestRigWith(t, quickOn)
	r.keys(e, "gi")
	r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) > 0 })
	e.pv.userInput([]byte("\r"))
	r.waitNoList(e)
	r.waitFor("the command to end", func() bool { return len(e.term.si.Commands()) >= 1 })
	// off in the settings: the key is the shell's and nothing opens
	r.api.SetSetting(settingSugEnabled, false)
	r.ext.settingsChanged()
	r.keys(e, "gi")
	time.Sleep(100 * time.Millisecond)
	r.api.Pump()
	if e.pv.sug.sess != nil {
		t.Errorf("the list opened with terminal.integrated.suggest.enabled off")
	}
	if v := r.api.ContextKey(keySuggestEnabled); v != false {
		t.Errorf("%s = %v with the setting off", keySuggestEnabled, v)
	}
}

func TestRunOnEnter(t *testing.T) {
	for _, tt := range []struct {
		mode string
		line string // typed before the list is chosen
		want int    // commands that ended after Enter on the first row
	}{
		{"never", "git", 0},
		{"always", "gi", 1},
		{"exactMatch", "git", 1},
		{"exactMatch", "gi", 0},
	} {
		t.Run(tt.mode+" "+tt.line, func(t *testing.T) {
			r, e := suggestRigWith(t, quickOn)
			r.api.SetSetting(settingSugRunEnter, tt.mode)
			r.api.SetSetting(settingSugSelect, "always")
			r.ext.settingsChanged()
			r.keys(e, tt.line)
			r.waitList(e, "the list", func(s *sugSession) bool { return len(s.items) > 0 })
			r.run(CmdAcceptSuggestEnter, nil)
			r.waitFor("the line", func() bool { in := inputOf(e); return in.ok && (in.value == "git" || len(e.term.si.Commands()) > 0) })
			time.Sleep(300 * time.Millisecond)
			if got := len(e.term.si.Commands()); got != tt.want {
				t.Errorf("%d commands ended, want %d", got, tt.want)
			}
		})
	}
}

func TestAFolderOpensTheListAgainWhenTheUserOpenedIt(t *testing.T) {
	r, e := suggestRigWith(t, nil) // the defaults: nothing opens by itself
	r.keys(e, "cat su")
	r.run(CmdTriggerSuggest, nil)
	r.waitList(e, "the list for su", func(s *sugSession) bool { return s.lc.cur.text == "su" && len(s.items) > 0 })
	r.run(CmdAcceptSuggest, nil)
	r.waitLine(e, "cat ./sub/")
	s := r.waitList(e, "the content of sub", func(s *sugSession) bool { return slices.Contains(labelsIn(s), "./sub/c.go") })
	if !s.explicit {
		t.Errorf("the list that follows a folder of an opened list is not the user's")
	}
	// a file ends it
	r.keys(e, "c")
	r.waitList(e, "the list for c", func(s *sugSession) bool { return s.lc.cur.text == "./sub/c" })
	r.run(CmdAcceptSuggest, nil)
	r.waitLine(e, "cat ./sub/c.go")
	r.waitNoList(e)
}
