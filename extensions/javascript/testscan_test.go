package javascript

import (
	"fmt"
	"strings"
	"testing"
)

// dumpScan writes the nodes as indented lines: "suite" or "test", the name, "@line:col" and the modifiers.
func dumpScan(nodes []*scanNode, depth int, sb *strings.Builder) {
	for _, n := range nodes {
		kind := "test"
		if n.Suite {
			kind = "suite"
		}
		mods := ""
		for _, m := range []struct {
			on   bool
			name string
		}{{n.Skip, " skip"}, {n.Only, " only"}, {n.Todo, " todo"}, {n.Each, " each"}} {
			if m.on {
				mods += m.name
			}
		}
		fmt.Fprintf(sb, "%s%s %q @%d:%d%s\n", strings.Repeat("  ", depth), kind, n.Name, n.Line, n.Col, mods)
		dumpScan(n.Children, depth+1, sb)
	}
}

func TestScanFindsSuitesAndTestsOfAFile(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"a test", "test('adds', () => {});\n", "test \"adds\" @1:1\n"},
		{"nesting", "describe('a', () => {\n  it('b', () => {});\n  describe('c', function () {\n    test(\"d\", async () => {});\n  });\n});\n",
			"suite \"a\" @1:1\n  test \"b\" @2:3\n  suite \"c\" @3:3\n    test \"d\" @4:5\n"},
		{"modifiers", "it.skip('s', f); it.only('o', f); it.todo('t'); test.concurrent('c', f); describe.skip('d', () => { it('inner', f) });\n",
			"test \"s\" @1:1 skip\ntest \"o\" @1:18 only\ntest \"t\" @1:35 todo\ntest \"c\" @1:49\nsuite \"d\" @1:74 skip\n  test \"inner\" @1:101\n"},
		{"aliases", "xit('a', f); fit('b', f); xdescribe('c', () => {}); fdescribe('d', () => {}); suite('e', () => {});\n",
			"test \"a\" @1:1 skip\ntest \"b\" @1:14 only\nsuite \"c\" @1:27 skip\nsuite \"d\" @1:53 only\nsuite \"e\" @1:79\n"},
		{"each with a table", "test.each([[1, 2], [3, 4]])('adds %i', (a, b) => {});\ndescribe.each([1, 2])('suite %i', (n) => { it('x', f) });\n",
			"test \"adds %i\" @1:1 each\nsuite \"suite %i\" @2:1 each\n"},
		{"each with a template", "it.each`\n a | b\n ${1} | ${2}\n`('adds $a', ({ a }) => {});\n", "test \"adds $a\" @1:1 each\n"},
		{"vitest conditions", "it.runIf(process.env.CI)('only on CI', f);\nit.skipIf(x)('not on x', f);\n", "test \"only on CI\" @1:1\ntest \"not on x\" @2:1\n"},
		{"a template literal without a substitution", "test(`plain ${'x'}`, f);\ntest(`plain`, f);\n", "test \"plain\" @2:1\n"},
		{"names made at run time are not found, the suites below them are walked", "describe(name, () => { it('kept', f); });\ntest('a' + b, f);\ntest(fn, f);\n", "test \"kept\" @1:24\n"},
		{"calls in comments, strings and regular expressions are not calls", "// it('no', f)\n/* test('no', f) */\nconst s = \"it('no', f)\";\nconst r = /it\\('no'/;\nconst t = `test('no')`;\nit('yes', f);\n", "test \"yes\" @6:1\n"},
		{"members and declarations are not calls", "t.test('sub', f);\nfunction test(a) {}\nfoo.it('x', f);\ntest.extend({})('y', f);\n", ""},
		{"escapes in names", "it('it\\'s \\\"q\\\" \\u0041\\n', f);\n", "test \"it's \\\"q\\\" A\\n\" @1:1\n"},
		{"TypeScript", "import { it } from 'vitest';\nit<Ctx>('generic', async ({ a }: Ctx): Promise<void> => {\n  expect(a as number).toBe(1);\n});\ninterface X { test: string }\n", "test \"generic\" @2:1\n"},
		{"a division is not a regular expression", "const a = 4 / 2; const b = a / 3;\nit('after', f);\n", "test \"after\" @2:1\n"},
		{"an apostrophe in JSX text ends with its line", "const x = <b>don't</b>;\nit('after', f);\n", "test \"after\" @2:1\n"},
		{"a column counts bytes", "const é = 1; it('ü', f);\n", "test \"ü\" @1:15\n"},
		{"a name that is not closed", "it('open\ntest('ok', f);\n", "test \"ok\" @2:1\n"},
		{"unbalanced brackets", "describe('a', () => {\n  it('b', f);\n", "test \"b\" @2:3\n"},
		{"empty", "", ""},
	} {
		var sb strings.Builder
		dumpScan(scanTests(tc.src), 0, &sb)
		if sb.String() != tc.want {
			t.Errorf("%s:\n%s\nwant:\n%s", tc.name, sb.String(), tc.want)
		}
	}
}

func TestScanReadsTheFixtureFiles(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"jest/src/math.test.js", "suite \"math\" @3:1\n  test \"adds\" @4:3\n  test \"divides\" @8:3\n  test \"later\" @12:3 skip\n  suite \"nested\" @14:3\n    test \"throws on zero\" @15:5\n  test \"adds %i and %i\" @20:3 each\ntest \"prints and passes\" @28:1\n"},
		{"vitest/src/math.test.ts", "suite \"math\" @4:1\n  test \"adds\" @5:3\n  test \"divides\" @9:3\n  test \"later\" @13:3 skip\n  suite \"nested\" @15:3\n    test \"throws on zero\" @16:5\n  test \"adds %i and %i\" @21:3 each\ntest \"prints and passes\" @29:1\n"},
		{"node/test/math.test.js", "suite \"math\" @5:1\n  test \"adds\" @6:3\n  test \"divides\" @10:3\n  test \"later\" @14:3 skip\n  suite \"nested\" @16:3\n    test \"throws on zero\" @17:5\ntest \"prints and passes\" @23:1\n"},
	} {
		src := readFixture(t, "testdata/tests/"+tc.file)
		var sb strings.Builder
		dumpScan(scanTests(src), 0, &sb)
		if sb.String() != tc.want {
			t.Errorf("%s:\n%s\nwant:\n%s", tc.file, sb.String(), tc.want)
		}
	}
}
