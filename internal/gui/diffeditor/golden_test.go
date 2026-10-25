package diffeditor

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

const goldenOriginal = `package main

import "fmt"

// greet prints a greeting.
func greet(name string) {
	fmt.Println("hello", name)
}

func add(a, b int) int {
	return a + b
}

func unused() {
	fmt.Println("this goes away")
	fmt.Println("and this too")
}

func main() {
	greet("world")
	fmt.Println(add(1, 2))
}
`

const goldenModified = `package main

import "fmt"

// greet prints a greeting for a person.
func greet(name string) {
	fmt.Println("hello,", name)
}

func add(a, b, c int) int {
	return a + b + c
}

func main() {
	greet("world")
	fmt.Println(add(1, 2, 3))
}

func square(x int) int {
	return x * x
}
`

var goldenThemes = []struct {
	name   string
	theme  string
	colors func() kit.Colors
}{
	{"dark", "Dark Modern", kit.DarkModern},
	{"light", "Light Modern", kit.LightModern},
}

func TestDiffEditorGolden(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, goldenOriginal, goldenModified, rigConfig{size: ui.Size{Width: 900, Height: 520}, colors: th.colors(), highlight: th.theme})
			r.settle()
			testutil.MatchesGolden(r.ts, "diff_"+th.name)
		})
	}
}

func TestDiffEditorWrappedLinesGolden(t *testing.T) {
	original := "func a() {\n\t// the comment above the call says what the call does and why it is made, in more words than a row has\n\tcall(first, second)\n}\n\nfunc b() {}\n"
	modified := "func a() {\n\t// the comment above the call says what it does\n\tcall(first, second, third, fourth, fifth, sixth, seventh, eighth, ninth, tenth, eleventh, twelfth, more)\n}\n\nfunc b() {}\n"
	wrap := func(o *editor.Options) {
		o.WordWrap, o.WrappingIndent, o.WrappingStrategy, o.StopRenderingLineAfter = "on", "same", "simple", 10000
	}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, original, modified, rigConfig{size: ui.Size{Width: 700, Height: 300}, colors: th.colors(), highlight: th.theme, tweak: wrap})
			r.waitFor("the filler of the wrapped lines", func() bool {
				return len(r.model.ZonesOf(Original))+len(r.model.ZonesOf(Modified)) >= 2
			})
			testutil.MatchesGolden(r.ts, "diff_wrapped_"+th.name)
		})
	}
}
