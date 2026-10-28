//go:build emetos_display

package main

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
	"github.com/rm4n0s/emetos/internal/testenv/realinput"
)

// Keys that arrive fast (step 17b.6, D5 of docs/DESKTOP-PASS.md): a fast typist, a held key that repeats and an input method
// that hands over several keys at once must never lose a key or send it to the wrong place. Each test runs at the pace of the
// harness (10 ms a key), with no pause between keys, and with every key of the scenario written to the compositor in one batch.

// keyMode is how a test sends its keys.
type keyMode struct {
	name string
	// delay is the harness's key delay (realinput.Options.KeyDelay).
	delay time.Duration
	// batch writes all the keys of a call at once.
	batch bool
}

var keyModes = []keyMode{
	{name: "10ms", delay: 10 * time.Millisecond},
	{name: "no delay", delay: realinput.NoKeyDelay},
	{name: "one batch", batch: true},
}

// forEachKeyMode runs fn as a subtest for each way of sending keys, with an editor started with that mode's delay.
func forEachKeyMode(t *testing.T, opts display.Options, fn func(t *testing.T, a *display.App, m keyMode)) {
	t.Helper()
	for _, m := range keyModes {
		t.Run(m.name, func(t *testing.T) {
			o := opts
			o.KeyDelay = m.delay
			a := display.Start(t, o)
			a.Settle()
			fn(t, a, m)
		})
	}
}

// keys is the key combinations of some shortcuts followed by the keys that type text.
func keys(t *testing.T, shortcuts []string, text string, after ...string) []string {
	t.Helper()
	typed, err := realinput.TextCombos(text)
	if err != nil {
		t.Fatal(err)
	}
	return append(append(append([]string(nil), shortcuts...), typed...), after...)
}

// TestDesktopFastKeysFind: Ctrl+F and the word at once leave the editor's cursor on the first match, as typing it slowly does.
func TestDesktopFastKeysFind(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{".", "words.txt"}, Project: navProject}, func(t *testing.T, a *display.App, m keyMode) {
		a.Keys(m.batch, keys(t, []string{"ctrl+f"}, "one")...)
		a.WaitFocusedText("one")
		a.WaitText("1 of 3")
		a.Eventually("the cursor on the end of the first match", func() (bool, string) {
			return a.Editor().Column == 4, itoa(a.Editor().Column)
		})
		a.Keys(m.batch, "Enter")
		a.WaitText("2 of 3")
		if got := editorText(a); got != navProject["words.txt"] {
			t.Errorf("the document changed to %q", got)
		}
	})
}

// TestDesktopFastKeysRename: a letter typed right after F2 goes into the rename input, not into the tree's own find box, and Enter
// renames the file with it.
func TestDesktopFastKeysRename(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{"."}, Project: explorerProject}, func(t *testing.T, a *display.App, m keyMode) {
		clickTextWhere(t, a, "junk.txt", inRect(0, 90, 348, 750))
		a.Eventually("junk.txt to open", func() (bool, string) { return activeTab(a) == "junk.txt", joinNames(tabNames(a)) })
		a.Keys(m.batch, keys(t, []string{"F2"}, "fast", "Enter")...)
		waitFile(a, "fast.txt", true)
		waitFile(a, "junk.txt", false)
		a.Settle()
		in := a.Inputs()
		shot(t, a, "fastkeys-rename-"+m.name)
		if len(in) != 0 {
			t.Errorf("an input is left open after the rename: %+v", in)
		}
	})
}

// TestDesktopFastKeysNewFile: the name typed at once after the New File button's command, then Enter.
func TestDesktopFastKeysNewFile(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{"."}, Project: explorerProject}, func(t *testing.T, a *display.App, m keyMode) {
		a.Click(200, 600) // the empty part of the tree: the root folder gets the new file
		a.Dev("explorer.newFile", nil)
		a.Keys(m.batch, keys(t, nil, "quick.txt", "Enter")...)
		waitFile(a, "quick.txt", true)
	})
}

// TestDesktopFastKeysQuickOpen: Ctrl+P, the name and Enter in a burst open the file.
func TestDesktopFastKeysQuickOpen(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{".", "app.go"}, Project: navProject, Prepare: withFakeGopls(t, goplsOptions{})}, func(t *testing.T, a *display.App, m keyMode) {
		a.Keys(m.batch, keys(t, []string{"ctrl+p"}, "more")...)
		a.WaitFocusedText("more")
		a.WaitText("more.go sub/deep")
		a.Keys(m.batch, "Enter")
		a.Eventually("more.go to open", func() (bool, string) { return activeTab(a) == "more.go", joinNames(tabNames(a)) })
		if got := editorText(a); got != "package deep\n" {
			t.Errorf("the document is %q", got)
		}
	})
}

// TestDesktopFastKeysCommandPalette: Ctrl+Shift+P and the name of a command at once.
func TestDesktopFastKeysCommandPalette(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{".", "app.go"}, Project: navProject, Prepare: withFakeGopls(t, goplsOptions{})}, func(t *testing.T, a *display.App, m keyMode) {
		a.Keys(m.batch, keys(t, []string{"ctrl+shift+p"}, "go to line")...)
		a.WaitFocusedText(">go to line")
		a.Keys(m.batch, "Enter")
		a.WaitFocusedText(":")
		a.Keys(m.batch, keys(t, nil, "3", "Enter")...)
		a.Eventually("the cursor on line 3", func() (bool, string) { return a.Editor().Line == 3, itoa(a.Editor().Line) })
		if got := editorText(a); got != goApp {
			t.Errorf("the document changed to %q", got)
		}
	})
}

// TestDesktopFastKeysSettingsSearch: Ctrl+, and the search at once.
func TestDesktopFastKeysSettingsSearch(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{".", "app.go"}, Project: navProject}, func(t *testing.T, a *display.App, m keyMode) {
		a.Keys(m.batch, keys(t, []string{"ctrl+comma"}, "tab size")...)
		a.WaitFocusedText("tab size")
		a.Settle()
		a.WaitText("Tab Size")
	})
}

// TestDesktopFastKeysKeybindingsSearch: Ctrl+K Ctrl+S and the search at once.
func TestDesktopFastKeysKeybindingsSearch(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{".", "app.go"}, Project: navProject}, func(t *testing.T, a *display.App, m keyMode) {
		a.Keys(m.batch, keys(t, []string{"ctrl+k", "ctrl+s"}, "format")...)
		a.WaitFocusedText("format")
		a.Settle()
		if got := activeTab(a); got == "app.go" {
			t.Errorf("the active tab is still %q", got)
		}
	})
}

// TestDesktopFastKeysNotes: a new note's title is typed at once.
func TestDesktopFastKeysNotes(t *testing.T) {
	forEachKeyMode(t, display.Options{Args: []string{".", "app.go"}, Project: navProject}, func(t *testing.T, a *display.App, m keyMode) {
		a.Dev("emetos.notes.newNote", nil)
		a.Keys(m.batch, keys(t, nil, "Shopping list")...)
		a.WaitFocusedText("Shopping list")
	})
}

// TestDesktopFastKeysHeld: a key held down repeats (the compositor tells the rate and the delay, Glutter makes the repeats; 25
// and 50 a second after 600 ms) into the editor and into an input box, without a lost or doubled character: the characters are
// the held key's, and their number is the first press and the repeats the time allows.
func TestDesktopFastKeysHeld(t *testing.T) {
	const hold = 1600 * time.Millisecond
	for _, rate := range []int{25, 50} {
		// the first press, then a repeat every 1/rate seconds once the delay (600 ms) is over, give or take the timers of the
		// editor and of this process
		want := 1 + rate*int((hold-600*time.Millisecond)/time.Second)
		within := func(t *testing.T, n int) {
			t.Helper()
			if n < want-rate/4 || n > want+rate/4 {
				t.Errorf("%d characters from a key held for %v at %d a second, want about %d", n, hold, rate, want)
			}
		}
		t.Run(itoa(rate)+" a second", func(t *testing.T) {
			t.Run("editor", func(t *testing.T) {
				a := display.Start(t, display.Options{Args: []string{".", "words.txt"}, Project: navProject})
				a.SetKeyRepeat(rate, 600*time.Millisecond)
				a.Settle()
				a.Hold("x", hold)
				a.Settle()
				text := editorText(a)
				n := len(text) - len(navProject["words.txt"])
				if n < 1 || text[:n] != repeatString("x", n) || text[n:] != navProject["words.txt"] {
					t.Fatalf("the text is %q", text)
				}
				within(t, n)
			})
			t.Run("input box", func(t *testing.T) {
				a := display.Start(t, display.Options{Args: []string{".", "words.txt"}, Project: navProject})
				a.SetKeyRepeat(rate, 600*time.Millisecond)
				a.Settle()
				a.Press("ctrl+f")
				a.Settle()
				a.Hold("x", hold)
				a.Settle()
				got := a.FocusedText()
				if got != repeatString("x", len(got)) {
					t.Fatalf("the find box holds %q", got)
				}
				within(t, len(got))
				if text := editorText(a); text != navProject["words.txt"] {
					t.Errorf("the document changed to %q", text)
				}
			})
		})
	}
}

func repeatString(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
