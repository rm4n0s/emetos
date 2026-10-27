package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/snippet"
)

// keyEditor shows a ruled editor (the language's brackets and quotes) of a language in a tester that has the focus and its
// text input connected.
func keyEditor(t *testing.T, languageID, text string) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	vm := ruled(t, languageID, text)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	return vm, ts
}

// typeOnKeys types a text one character at a time as the platform's text input does: each is a commit of the input method,
// which then gets the whole line again (the keys of the Linux platform arrive the same way).
func typeOnKeys(ts *gluttertest.Tester, text string) {
	for _, c := range text {
		ts.SendTextInputEvent(commit(string(c)))
	}
}

// TestKeysTypeOverOnlyTheClosingCharactersTheEditorTyped: a closing character typed in front of the same character is a typing,
// which types over only the characters the editor closed (editor.autoClosingOvertype auto, SPEC §8.6.4). The input method
// gives the whole line again, so what changed is told from the selection and not from the first and last differing characters
// (a ")" before a ")" is the same line either way).
func TestKeysTypeOverOnlyTheClosingCharactersTheEditorTyped(t *testing.T) {
	cases := []struct {
		name, language, text string
		keys                 func(vm *ViewModel, ts *gluttertest.Tester)
		want                 string
	}{
		{"a bracket the user typed is not typed over", "go", "", func(vm *ViewModel, ts *gluttertest.Tester) {
			typeOnKeys(ts, "// z)")
			if err := vm.RunCommand("cursorLeft", nil); err != nil {
				t.Fatal(err)
			}
			typeOnKeys(ts, ")")
		}, "// z))"},
		{"a bracket the editor closed is typed over", "go", "", func(vm *ViewModel, ts *gluttertest.Tester) {
			typeOnKeys(ts, "f(x)")
		}, "f(x)"},
		{"a quote the editor closed is typed over", "go", "", func(vm *ViewModel, ts *gluttertest.Tester) {
			typeOnKeys(ts, `f("x")`)
		}, `f("x")`},
		{"an inner bracket is typed over and the outer one stays", "go", "", func(vm *ViewModel, ts *gluttertest.Tester) {
			typeOnKeys(ts, `f(("x")`)
		}, `f(("x"))`},
		{"a bracket typed after the closed one is not typed over", "go", "", func(vm *ViewModel, ts *gluttertest.Tester) {
			typeOnKeys(ts, "f(x))")
		}, "f(x))"},
		{"a bracket in front of a line's own bracket", "go", "g()", func(vm *ViewModel, ts *gluttertest.Tester) {
			if err := vm.RunCommand("cursorLeft", nil); err != nil {
				t.Fatal(err)
			}
			typeOnKeys(ts, ")")
		}, "g())"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, ts := keyEditor(t, tc.language, tc.text)
			tc.keys(vm, ts)
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			noErrors(t, ts)
		})
	}
}

// TestEditedStretch: the stretch of the old line that the new line replaced, told from the selection of the input method.
func TestEditedStretch(t *testing.T) {
	selection := func(a, b int) services.TextSelection { return services.NewTextSelection(a, b) }
	cases := []struct {
		name, old, now string
		sel            services.TextSelection
		from, to       int
		text           string
	}{
		{"a character in the middle", "abc", "aXbc", selection(1, 1), 1, 1, "X"},
		{"the same character as the one after the cursor", "// z)", "// z))", selection(4, 4), 4, 4, ")"},
		{"the same character as the one before the cursor", "// z)", "// z))", selection(5, 5), 5, 5, ")"},
		{"a double quote in front of a double quote", `f("x")`, `f("x"")`, selection(5, 5), 5, 5, `"`},
		{"over a selection", "abcd", "aXd", selection(1, 3), 1, 3, "X"},
		{"over a selection by the same character", "abc", "abc", selection(1, 2), 1, 2, "b"},
		{"a character at the end", "ab", "abc", selection(2, 2), 2, 2, "c"},
		{"a character at the start", "ab", "cab", selection(0, 0), 0, 0, "c"},
		// the method replaced text away from the cursor: the stretch is found from the differing characters
		{"a deletion before the cursor", "abc", "ac", selection(2, 2), 1, 2, ""},
		{"a deletion after the cursor", "abc", "ab", selection(1, 1), 2, 3, ""},
		{"a replacement of the characters before the cursor", "ab", "aX", selection(2, 2), 1, 2, "X"},
		{"a composition that replaces the characters around the cursor", "abcd", "aXd", selection(2, 2), 1, 3, "X"},
		{"an invalid selection", "abc", "abXc", selection(-1, -1), 2, 2, "X"},
		{"a selection past the end", "abc", "abXc", selection(9, 9), 2, 2, "X"},
		{"a character is never cut", "aβb", "aγβb", selection(1, 1), 1, 1, "γ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, text := editedStretch(services.TextEditingValue{Text: tc.old, Selection: tc.sel}, tc.now)
			if from != tc.from || to != tc.to || text != tc.text {
				t.Errorf("[%d,%d) %q, want [%d,%d) %q", from, to, text, tc.from, tc.to, tc.text)
			}
			if got := tc.old[:from] + text + tc.old[to:]; got != tc.now {
				t.Errorf("the stretch turns %q into %q, want %q", tc.old, got, tc.now)
			}
		})
	}
}

// TestASnippetsClosingCharacterIsTypedOver: the pair at the end of an inserted snippet is tracked as the editor's own
// (VS Code's _findAutoClosingPairs for the edits of a snippet), so the user's closing character types over it and the session
// of the tab stop inside the pair ends there.
func TestASnippetsClosingCharacterIsTypedOver(t *testing.T) {
	cases := []struct {
		name, language, text, template, typed, want string
		session                                     bool
		tabStops                                    int
	}{
		{"a call with an empty tab stop", "go", "fmt.", "Sprint(${1:})", `"x")`, `fmt.Sprint("x")`, false, 0},
		{"a call with only the final tab stop", "go", "fmt.", "Sprint($0)", `"x")`, `fmt.Sprint("x")`, false, 0},
		{"a call with a placeholder", "go", "fmt.", "Sprint(${1:a})", `x)`, `fmt.Sprint(x)`, false, 0},
		{"the call is closed once", "go", "fmt.", "Sprint($0)", `x))`, `fmt.Sprint(x))`, false, 0},
		{"an index", "typescript", "a", "[${1:i}]", `0]`, `a[0]`, false, 0},
		{"a string", "typescript", "const s = ", "'${1:text}'", `hi'`, `const s = 'hi'`, false, 0},
		{"the cursor starts after the pair", "go", "fmt.", "Sprint()$1", ")", `fmt.Sprint())`, true, 2},
		{"text after the closing character", "go", "", "f($0) // call", ")", `f() // call`, false, 0},
		{"a closing character in the text after the pair takes the pair", "go", "", "f($0) /* ) */", ")", `f()) /* ) */`, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := ruled(t, tc.language, tc.text)
			if err := vm.InsertSnippet(tc.template, snippet.DefaultInsertOptions()); err != nil {
				t.Fatal(err)
			}
			typeKeys(t, vm, tc.typed)
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			if st := vm.DevState(); st.InSnippet != tc.session || st.TabStops != tc.tabStops {
				t.Errorf("session %v with %d tab stops, want %v with %d", st.InSnippet, st.TabStops, tc.session, tc.tabStops)
			}
		})
	}
}

// TestASnippetWithOnlyTheFinalTabStopEndsAtOnce: the session of a snippet whose only tab stop is $0 ends when the snippet is
// inserted, and nothing is drawn or grows as the user types (SPEC §9.9, VS Code's SnippetController2 _updateState).
func TestASnippetWithOnlyTheFinalTabStopEndsAtOnce(t *testing.T) {
	cases := []struct{ name, template, typed, want string }{
		{"in parentheses", "Sprintf($0)", "abc\ndef", "fmt.Sprintf(abc\ndef)"},
		{"alone", "$0", "abc\ndef", "fmt.abc\ndef"},
		{"between texts", "a$0b", "x", "fmt.axb"},
		{"as a placeholder with a default", "f(${0:x})", "y", "fmt.f(y)"},
		{"no tab stop", "plain()", "x", "fmt.plain()x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := ruled(t, "go", "fmt.")
			if err := vm.InsertSnippet(tc.template, snippet.DefaultInsertOptions()); err != nil {
				t.Fatal(err)
			}
			if st := vm.DevState(); st.InSnippet || st.TabStops != 0 {
				t.Errorf("after the insertion: session %v with %d tab stops, want none", st.InSnippet, st.TabStops)
			}
			if vm.SnippetState().InSnippet {
				t.Error("the context key inSnippetMode is on")
			}
			for _, c := range tc.typed {
				key := string(c)
				if c == '\n' {
					if err := vm.RunCommand("type", "\n"); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if err := vm.RunCommand("type", key); err != nil {
					t.Fatal(err)
				}
				if st := vm.DevState(); st.InSnippet || st.TabStops != 0 {
					t.Fatalf("after typing %q: session %v with %d tab stops, want none", key, st.InSnippet, st.TabStops)
				}
			}
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDevStateReportsTheModes: the session of a snippet with its tab stops, for the dev server (dev.editor).
func TestDevStateReportsTheModes(t *testing.T) {
	vm := ruled(t, "go", "")
	if st := vm.DevState(); st != (DevState{}) {
		t.Errorf("a new editor: %+v", st)
	}
	if err := vm.InsertSnippet("f(${1:a}, ${2:b})$0", snippet.DefaultInsertOptions()); err != nil {
		t.Fatal(err)
	}
	if st := vm.DevState(); !st.InSnippet || st.TabStops != 3 || st.SuggestVisible || st.SignatureVisible {
		t.Errorf("in a snippet of 3 tab stops: %+v", st)
	}
	vm.snippets().Cancel(false)
	if st := vm.DevState(); st != (DevState{}) {
		t.Errorf("after the session: %+v", st)
	}
}
