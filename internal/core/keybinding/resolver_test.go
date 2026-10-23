package keybinding

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/errors"
)

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// mustKB parses a keybinding in the syntax of keybindings.json; "" is no keybinding.
func mustKB(t testing.TB, spec string) Keybinding {
	t.Helper()
	kb, err := ParseKeybinding(spec)
	if err != nil {
		t.Fatalf("ParseKeybinding(%q): %v", spec, err)
	}
	return kb
}

// kbItem is the helper of VS Code's keybindingResolver.test.ts.
func kbItem(t testing.TB, spec, command string, args any, when contextkey.Expr, isDefault bool) Item {
	t.Helper()
	return NewItem(mustKB(t, spec), command, args, when, isDefault)
}

func mustParseWhen(t testing.TB, s string) contextkey.Expr {
	t.Helper()
	if s == "" {
		return nil
	}
	e, err := contextkey.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return e
}

// describe prints the parts of an item the tests compare.
func describe(it Item) string {
	when := "<none>"
	if it.When != nil {
		when = it.When.Serialize()
	}
	return strings.Join(it.Chords, ",") + " | " + it.Command + " | " + when
}

func assertItems(t *testing.T, got, want []Item) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %v", len(got), len(want), got)
	}
	for i := range got {
		if describe(got[i]) != describe(want[i]) || got[i].IsDefault != want[i].IsDefault || got[i].Bubble != want[i].Bubble {
			t.Errorf("item %d: got %q, want %q", i, describe(got[i]), describe(want[i]))
		}
	}
}

func dispatchOf(t *testing.T, spec string) string {
	t.Helper()
	kb := mustKB(t, spec)
	if len(kb) != 1 {
		t.Fatalf("%q is not a single chord", spec)
	}
	return kb[0].Dispatch()
}

// TestVSCodeResolveKey ports 'resolve key' and 'resolve key with arguments'.
func TestVSCodeResolveKey(t *testing.T) {
	const keys = "ctrl+shift+z"
	rules := contextkey.Equals("bar", "baz")
	if !rules.Evaluate(contextkey.MapContext{"bar": "baz"}) || rules.Evaluate(contextkey.MapContext{"bar": "bz"}) {
		t.Fatal("the when-clause does not evaluate as expected")
	}
	args := map[string]any{"text": "no"}
	r := NewResolver([]Item{kbItem(t, keys, "yes", args, rules, true)}, nil, nil)

	r1 := r.Resolve(contextkey.MapContext{"bar": "baz"}, nil, dispatchOf(t, keys))
	if r1.Kind != KbFound || r1.Command != "yes" {
		t.Errorf("got %+v, want yes", r1)
	}
	if r1.Args == nil || r1.Args.(map[string]any)["text"] != "no" {
		t.Errorf("arguments not passed: %v", r1.Args)
	}
	r2 := r.Resolve(contextkey.MapContext{"bar": "bz"}, nil, dispatchOf(t, keys))
	if r2.Kind != NoMatchingKb {
		t.Errorf("got %+v, want NoMatchingKb", r2)
	}
}

// TestVSCodeHandleRemovals ports the 'handle keybinding removals' suite.
func TestVSCodeHandleRemovals(t *testing.T) {
	eq := func(k, v string) contextkey.Expr { return contextkey.Equals(k, v) }
	cases := []struct {
		name  string
		rules func(t *testing.T) []Item
		want  func(t *testing.T) []Item
	}{
		{"simple 1",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), false)}
			},
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), false)}
			}},
		{"simple 2",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "c", "yes3", nil, eq("3", "c"), false)}
			},
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "c", "yes3", nil, eq("3", "c"), false)}
			}},
		{"removal with not matching when",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "a", "-yes1", nil, eq("1", "b"), false)}
			},
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true)}
			}},
		{"removal with not matching keybinding",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "b", "-yes1", nil, eq("1", "a"), false)}
			},
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true)}
			}},
		{"removal with matching keybinding and when",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "a", "-yes1", nil, eq("1", "a"), false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "b", "yes2", nil, eq("2", "b"), true)} }},
		{"removal with unspecified keybinding",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "", "-yes1", nil, eq("1", "a"), false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "b", "yes2", nil, eq("2", "b"), true)} }},
		{"removal with unspecified when",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "a", "-yes1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "b", "yes2", nil, eq("2", "b"), true)} }},
		{"removal with unspecified when and unspecified keybinding",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "", "-yes1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "b", "yes2", nil, eq("2", "b"), true)} }},
		{"issue #138997 - removal in default list",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "yes1", nil, nil, true), kbItem(t, "b", "yes2", nil, nil, true), kbItem(t, "", "-yes1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "b", "yes2", nil, nil, true)} }},
		{"issue #612 cannot remove keybindings for commands with ^",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "^yes1", nil, eq("1", "a"), true), kbItem(t, "b", "yes2", nil, eq("2", "b"), true), kbItem(t, "a", "-yes1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "b", "yes2", nil, eq("2", "b"), true)} }},
		{"issue #140884 unable to reassign F1 as keybinding for Show All Commands",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "command1", nil, nil, true), kbItem(t, "a", "-command1", nil, nil, false), kbItem(t, "a", "command1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "a", "command1", nil, nil, false)} }},
		{"issue #141638 change when expression might remove keybinding",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "command1", nil, nil, true), kbItem(t, "a", "command1", nil, eq("a", "1"), false), kbItem(t, "a", "-command1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "a", "command1", nil, eq("a", "1"), false)} }},
		{"issue #157751 auto-quoting of context keys prevents removal",
			func(t *testing.T) []Item {
				return []Item{
					kbItem(t, "a", "command1", nil, mustParseWhen(t, `editorTextFocus && activeEditor != workbench.editor.notebook && editorLangId in julia.supportedLanguageIds`), true),
					kbItem(t, "a", "-command1", nil, mustParseWhen(t, `editorTextFocus && activeEditor != 'workbench.editor.notebook' && editorLangId in 'julia.supportedLanguageIds'`), false),
				}
			},
			func(t *testing.T) []Item { return nil }},
		{"issue #293802 removal matches when the default becomes more specific",
			func(t *testing.T) []Item {
				return []Item{
					kbItem(t, "a", "command1", nil, contextkey.And(contextkey.Has("inChatInput"), contextkey.Not("withinEditSessionDiff")), true),
					kbItem(t, "a", "-command1", nil, contextkey.Has("inChatInput"), false),
				}
			},
			func(t *testing.T) []Item { return nil }},
		{"removal with more specific when does not match a broader default",
			func(t *testing.T) []Item {
				return []Item{
					kbItem(t, "a", "command1", nil, contextkey.Has("inChatInput"), true),
					kbItem(t, "a", "-command1", nil, contextkey.And(contextkey.Has("inChatInput"), contextkey.Not("withinEditSessionDiff")), false),
				}
			},
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "command1", nil, contextkey.Has("inChatInput"), true)}
			}},
		{"issue #160604 remove keybindings with when clause true",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "command1", nil, nil, true), kbItem(t, "a", "-command1", nil, contextkey.True(), false)}
			},
			func(t *testing.T) []Item { return nil }},
		{"a removal longer than the default key does not match",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "command1", nil, nil, true), kbItem(t, "a b", "-command1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "a", "command1", nil, nil, true)} }},
		{"a user keybinding is never removed",
			func(t *testing.T) []Item {
				return []Item{kbItem(t, "a", "command1", nil, nil, false), kbItem(t, "a", "-command1", nil, nil, false)}
			},
			func(t *testing.T) []Item { return []Item{kbItem(t, "a", "command1", nil, nil, false)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertItems(t, HandleRemovals(tc.rules(t)), tc.want(t))
		})
	}
}

// TestVSCodeWhenIsEntirelyIncluded ports 'contextIsEntirelyIncluded'.
func TestVSCodeWhenIsEntirelyIncluded(t *testing.T) {
	included := [][2]string{
		{"", ""}, {"key1", ""}, {"key1", "key1"}, {"!key1", ""}, {"!key1", "!key1"},
		{"key2", ""}, {"key2", "key2"}, {"key1 && key1 && key2 && key2", "key2"},
		{"key1 && key2", "key2"}, {"key1 && key2", "key1"}, {"key1 && key2", ""},
		{"key1", "key1 || key2"}, {"key1 || !key1", "key2 || !key2"}, {"key1", "key1 || key2 && key3"},
	}
	for _, c := range included {
		if !WhenIsEntirelyIncluded(mustParseWhen(t, c[0]), mustParseWhen(t, c[1])) {
			t.Errorf("%q should include %q", c[0], c[1])
		}
	}
	if !WhenIsEntirelyIncluded(nil, contextkey.True()) || !WhenIsEntirelyIncluded(contextkey.True(), nil) || !WhenIsEntirelyIncluded(contextkey.True(), contextkey.True()) {
		t.Error("true and nothing are the same")
	}
	if !WhenIsEntirelyIncluded(mustParseWhen(t, "key1"), contextkey.True()) {
		t.Error("everything implies true")
	}
	excluded := [][2]string{
		{"key1", "!key1"}, {"!key1", "key1"}, {"key1 && key2", "key3"}, {"key1 && key2", "key4"},
		{"key1", "key2"}, {"key1 || key2", "key2"}, {"", "key2"},
	}
	for _, c := range excluded {
		if WhenIsEntirelyIncluded(mustParseWhen(t, c[0]), mustParseWhen(t, c[1])) {
			t.Errorf("%q should not include %q", c[0], c[1])
		}
	}
}

// resolveCommandItems is the list of the 'resolve command' suite.
func resolveCommandItems(t *testing.T) []Item {
	eqTrue := func(k string) contextkey.Expr { return contextkey.Equals(k, true) }
	it := func(spec, command string, when contextkey.Expr) Item {
		return kbItem(t, spec, command, nil, when, true)
	}
	return []Item{
		// never matches: its "when" is always overwritten by another one
		it("x", "first", contextkey.And(eqTrue("key1"), contextkey.NotEquals("key2", false))),
		// always overwrites first
		it("x", "second", eqTrue("key2")),
		// a secondary mapping for second
		it("z", "second", nil),
		// sometimes overwrites first
		it("x", "third", eqTrue("key3")),
		// always overwritten by another one
		it("ctrl+y", "fourth", eqTrue("key4")),
		// overwrites the previous one with a chord
		it("ctrl+y z", "fifth", nil),
		// has no keybinding
		it("", "sixth", nil),
		it("ctrl+k ctrl+u", "seventh", nil),
		it("ctrl+k ctrl+k", "seventh", nil),
		it("ctrl+k ctrl+u", "uncomment lines", nil),
		it("ctrl+k ctrl+c", "comment lines", nil),
		it("ctrl+g ctrl+c", "unreachablechord", nil),
		it("ctrl+g", "eleven", nil),
		it("ctrl+k a b", "long multi chord", nil),
		it("ctrl+b ctrl+c", "shadowed by long-multi-chord-2", nil),
		it("ctrl+b ctrl+c i", "long-multi-chord-2", nil),
	}
}

// TestVSCodeResolveCommand ports the 'resolve command' suite: the lookup of the keys of a
// command and the resolution of its chords.
func TestVSCodeResolveCommand(t *testing.T) {
	resolver := NewResolver(resolveCommandItems(t), nil, nil)

	testLookup := func(t *testing.T, command string, want ...string) {
		t.Helper()
		got := resolver.LookupKeybindings(command)
		if len(got) != len(want) {
			t.Fatalf("lookup of %q: got %d keybindings, want %d", command, len(got), len(want))
		}
		for i := range got {
			if label := got[i].Keybinding.UserSettingsLabel(); label != want[i] {
				t.Errorf("lookup of %q [%d]: got %q, want %q", command, i, label, want[i])
			}
		}
	}
	testResolve := func(t *testing.T, ctx contextkey.Context, spec, command string) {
		t.Helper()
		kb := mustKB(t, spec)
		var previous []string
		for i, c := range kb {
			chord := c.Dispatch()
			res := resolver.Resolve(ctx, previous, chord)
			if i == len(kb)-1 {
				if res.Kind != KbFound || res.Command != command {
					t.Errorf("%s: chord %d resolved to %+v, want %s", command, i, res, command)
				}
			} else if res.Kind != MoreChordsNeeded {
				t.Errorf("%s: chord %d resolved to %+v, want MoreChordsNeeded", command, i, res)
			}
			previous = append(previous, chord)
		}
	}
	empty := contextkey.MapContext{}

	t.Run("1", func(t *testing.T) { testLookup(t, "first") })
	t.Run("2", func(t *testing.T) {
		testLookup(t, "second", "z", "x")
		testResolve(t, contextkey.MapContext{"key2": true}, "x", "second")
		testResolve(t, empty, "z", "second")
	})
	t.Run("3", func(t *testing.T) {
		testLookup(t, "third", "x")
		testResolve(t, contextkey.MapContext{"key3": true}, "x", "third")
	})
	t.Run("4", func(t *testing.T) { testLookup(t, "fourth") })
	t.Run("5", func(t *testing.T) {
		testLookup(t, "fifth", "ctrl+y z")
		testResolve(t, empty, "ctrl+y z", "fifth")
	})
	t.Run("6", func(t *testing.T) {
		testLookup(t, "seventh", "ctrl+k ctrl+k")
		testResolve(t, empty, "ctrl+k ctrl+k", "seventh")
	})
	t.Run("7", func(t *testing.T) {
		testLookup(t, "uncomment lines", "ctrl+k ctrl+u")
		testResolve(t, empty, "ctrl+k ctrl+u", "uncomment lines")
	})
	t.Run("8", func(t *testing.T) {
		testLookup(t, "comment lines", "ctrl+k ctrl+c")
		testResolve(t, empty, "ctrl+k ctrl+c", "comment lines")
	})
	t.Run("9", func(t *testing.T) { testLookup(t, "unreachablechord") })
	t.Run("10", func(t *testing.T) {
		testLookup(t, "eleven", "ctrl+g")
		testResolve(t, empty, "ctrl+g", "eleven")
	})
	t.Run("11", func(t *testing.T) { testLookup(t, "sixth") })
	t.Run("12", func(t *testing.T) {
		testLookup(t, "long multi chord", "ctrl+k a b")
		testResolve(t, empty, "ctrl+k a b", "long multi chord")
	})
	t.Run("KBs having common prefix - the one defined later is returned", func(t *testing.T) {
		testResolve(t, empty, "ctrl+b ctrl+c i", "long-multi-chord-2")
	})
}

func TestResolveResults(t *testing.T) {
	r := NewResolver([]Item{
		kbItem(t, "ctrl+k ctrl+c", "comment", nil, nil, true),
		kbItem(t, "ctrl+s", "save", nil, nil, true),
		kbItem(t, "ctrl+s", "saveHere", nil, contextkey.Has("here"), true),
		kbItem(t, "ctrl+q", "^bubble", nil, nil, true),
		kbItem(t, "ctrl+w", "", nil, nil, true),
	}, nil, nil)
	cases := []struct {
		name    string
		ctx     contextkey.Context
		current []string
		press   string
		want    Resolution
	}{
		{"unknown first chord", contextkey.MapContext{}, nil, "ctrl+x", Resolution{Kind: NoMatchingKb}},
		{"first chord of a sequence", contextkey.MapContext{}, nil, "ctrl+k", Resolution{Kind: MoreChordsNeeded}},
		{"the last defined wins when its condition holds", contextkey.MapContext{"here": true}, nil, "ctrl+s", Resolution{Kind: KbFound, Command: "saveHere"}},
		{"an earlier one when the later condition fails", contextkey.MapContext{}, nil, "ctrl+s", Resolution{Kind: KbFound, Command: "save"}},
		{"bubble", contextkey.MapContext{}, nil, "ctrl+q", Resolution{Kind: KbFound, Command: "bubble", Bubble: true}},
		{"a binding without a command", contextkey.MapContext{}, nil, "ctrl+w", Resolution{Kind: KbFound}},
		{"wrong second chord", contextkey.MapContext{}, []string{"ctrl+k"}, "ctrl+x", Resolution{Kind: NoMatchingKb}},
		{"right second chord", contextkey.MapContext{}, []string{"ctrl+k"}, "ctrl+c", Resolution{Kind: KbFound, Command: "comment"}},
		{"too many chords", contextkey.MapContext{}, []string{"ctrl+s"}, "ctrl+c", Resolution{Kind: NoMatchingKb}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var current []string
			for _, c := range tc.current {
				current = append(current, dispatchOf(t, c))
			}
			got := r.Resolve(tc.ctx, current, dispatchOf(t, tc.press))
			if got.Kind != tc.want.Kind || got.Command != tc.want.Command || got.Bubble != tc.want.Bubble {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestResolverLookups(t *testing.T) {
	defaults := []Item{
		kbItem(t, "a", "cmd", nil, contextkey.Has("one"), true),
		kbItem(t, "b", "cmd", nil, contextkey.Has("two"), true),
		kbItem(t, "c", "other", nil, contextkey.False(), true),
		kbItem(t, "d", "-removed", nil, nil, true),
	}
	r := NewResolver(defaults, []Item{kbItem(t, "e", "mine", nil, nil, false)}, nil)

	if !r.DefaultBoundCommands()["cmd"] || r.DefaultBoundCommands()["-removed"] || r.DefaultBoundCommands()["mine"] {
		t.Errorf("default bound commands: %v", r.DefaultBoundCommands())
	}
	if len(r.DefaultKeybindings()) != len(defaults) {
		t.Errorf("default keybindings: %d", len(r.DefaultKeybindings()))
	}
	if got := len(r.Keybindings()); got != 4 {
		t.Errorf("keybindings after removals: %d", got)
	}
	if got := r.LookupKeybindings("other"); len(got) != 0 {
		t.Errorf("a binding whose condition is false is not registered: %v", got)
	}
	if got := r.LookupKeybindings("nothing"); len(got) != 0 {
		t.Errorf("unknown command: %v", got)
	}

	prim, ok := r.LookupPrimaryKeybinding("cmd", contextkey.MapContext{"one": true}, false)
	if !ok || prim.Chords[0] != "A" {
		t.Errorf("primary with one: %v %v", prim.Chords, ok)
	}
	prim, ok = r.LookupPrimaryKeybinding("cmd", contextkey.MapContext{"two": true}, false)
	if !ok || prim.Chords[0] != "B" {
		t.Errorf("primary with two: %v %v", prim.Chords, ok)
	}
	prim, ok = r.LookupPrimaryKeybinding("cmd", contextkey.MapContext{}, false)
	if !ok || prim.Chords[0] != "B" {
		t.Errorf("the last one when none holds: %v %v", prim.Chords, ok)
	}
	if _, ok = r.LookupPrimaryKeybinding("cmd", contextkey.MapContext{}, true); ok {
		t.Error("enforced context check found a binding")
	}
	if _, ok = r.LookupPrimaryKeybinding("nothing", contextkey.MapContext{}, false); ok {
		t.Error("unknown command found")
	}
	prim, ok = r.LookupPrimaryKeybinding("mine", contextkey.MapContext{}, false)
	if !ok || prim.IsDefault {
		t.Errorf("single binding: %+v %v", prim, ok)
	}
}

func TestResolverLog(t *testing.T) {
	var lines []string
	r := NewResolver([]Item{kbItem(t, "ctrl+k ctrl+c", "comment", nil, nil, true)}, nil, func(s string) { lines = append(lines, s) })
	r.Resolve(contextkey.MapContext{}, nil, dispatchOf(t, "ctrl+k"))
	r.Resolve(contextkey.MapContext{}, nil, dispatchOf(t, "ctrl+x"))
	r.Resolve(contextkey.MapContext{}, []string{dispatchOf(t, "ctrl+k")}, dispatchOf(t, "ctrl+c"))
	if len(lines) != 6 || !strings.Contains(lines[0], "Resolving ctrl+K") {
		t.Errorf("log: %q", lines)
	}
}

// TestModifierOnlyKeybinding checks that a keybinding made of a lone modifier is dispatched
// by its name, as VS Code does for "shift shift".
func TestModifierOnlyKeybinding(t *testing.T) {
	it := kbItem(t, "shift shift", "double", nil, nil, true)
	if len(it.Chords) != 2 || it.Chords[0] != "shift" || it.Chords[1] != "shift" {
		t.Errorf("chords: %v", it.Chords)
	}
	r := NewResolver([]Item{it}, nil, nil)
	if res := r.Resolve(contextkey.MapContext{}, []string{"shift"}, "shift"); res.Kind != KbFound || res.Command != "double" {
		t.Errorf("got %+v", res)
	}
}
