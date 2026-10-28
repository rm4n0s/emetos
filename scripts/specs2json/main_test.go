package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

const sample = "/* a comment */\nimport * as vscode from 'vscode';\n" + `
function ai(...args: any[]): undefined { return undefined; }
const re = /[(]\/x/g;
type T = { a: string };
interface Z { x: number }
const gen = {
  branches: {
    script: ["git", "branch"],
    postProcess: (out: string) => out.split("\n").map((b) => ({ name: b, icon: ` + "`vscode://icon?type=${1}`" + ` })),
  },
};
const common: Fig.Option[] = [
  { name: ["-q", "--quiet"], description: "Quiet" },
];
const completionSpec: Fig.Spec = {
  name: "tool",
  description: 'A "tool"',
  generateSpec: async (_, run) => { const { stdout } = await run({ command: "x" }); return { name: "tool" }; },
  subcommands: [
    {
      name: ["run", "r"],
      description: "Run it",
      options: [...common, { name: "--mode", args: { name: "mode", suggestions: ["fast", { name: "slow", description: "Slowly" }] }, isRepeatable: true }],
      args: [{ name: "file", template: "filepaths", isVariadic: true }, { name: "x", generators: gen.branches, isOptional: true }],
    },
    { name: "hidden", hidden: true, loadSpec: "other" },
    { name: ` + "`tpl`" + `, deprecated: "use run" },
  ],
  options: [{ name: "--version", description: cond ? "a" : "b" }],
  args: { name: "alias", isOptional: true, generators: { template: ["folders"] }, parserDirectives: { alias: async (t) => t } },
};
export default completionSpec;
`

func TestConvertSource(t *testing.T) {
	spec, err := convertSource(sample)
	if err != nil {
		t.Fatal(err)
	}
	b, err := encode(spec)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := `{"name":"tool","description":"A \"tool\"","subcommands":[` +
		`{"name":["run","r"],"description":"Run it","options":[{"name":["-q","--quiet"],"description":"Quiet"},` +
		`{"name":"--mode","args":[{"name":"mode","suggestions":[{"name":"fast"},{"name":"slow","description":"Slowly"}]}],"repeatable":true}],` +
		`"args":[{"name":"file","variadic":true,"templates":["filepaths"]},{"name":"x","optional":true}]},` +
		`{"name":"hidden","hidden":true},{"name":"tpl","deprecated":true}],` +
		`"options":[{"name":"--version"}],"args":[{"name":"alias","optional":true,"templates":["folders"]}]}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestParserEdgeCases(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"a name from a constant", "const n = 'x'; const s = { name: n }; export default s;", `{"name":"x"}`},
		{"shorthand", "const name = 'y'; export default { name };", `{"name":"y"}`},
		{"a spread of an object", "const base = { description: 'd' }; export default { name: 'z', ...base };", `{"name":"z","description":"d"}`},
		{"an arrow function in the middle", "export default { name: 'a', options: [{ name: '-x', args: { name: 'v', generators: (a) => ({}) } }] };", `{"name":"a","options":[{"name":"-x","args":[{"name":"v"}]}]}`},
		{"a regular expression with a bracket", "const r = /[)]/; export default { name: 'b', description: 'ok' };", `{"name":"b","description":"ok"}`},
		{"a division is not a regular expression", "const x = 4 / 2; export default { name: 'c' };", `{"name":"c"}`},
		{"as casts", "export default { name: 'd', args: { name: 'e' } as Fig.Arg } as Fig.Spec;", `{"name":"d","args":[{"name":"e"}]}`},
		{"a method and a getter", "export default { name: 'm', run() { return 1; }, get x() { return 2; }, description: 'after' };", `{"name":"m","description":"after"}`},
		{"a string with escapes", `export default { name: 'e', description: "it\'s \"q\" A\n" };`, `{"name":"e","description":"it's \"q\" A\n"}`},
		{"a number as the repeat count", "export default { name: 'n', options: [{ name: '-r', isRepeatable: 3 }] };", `{"name":"n","options":[{"name":"-r","repeatable":true}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := convertSource(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			b, err := encode(spec)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Errorf("got  %s\nwant %s", b, tt.want)
			}
		})
	}
}

func TestNoSpec(t *testing.T) {
	_, err := convertSource("const x = 1;")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("convertSource.Specs2jsonNoSpec") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name  string
		args  []string
		route string
	}{
		{"usage", []string{"nothing"}, "run.Specs2jsonUsage"},
		{"a missing spec file", []string{"specs", dir, filepath.Join(dir, "nope.ts")}, "run->convertSpec.Specs2jsonReadFailed"},
		{"a missing cache file", []string{"builtins", filepath.Join(dir, "nope.ts"), filepath.Join(dir, "o.json")}, "run->convertBuiltins.Specs2jsonReadFailed"},
		{"an output that cannot be written", []string{"builtins", writeTemp(t, dir, "c.ts", "export const c = {};"), dir}, "convertBuiltins->writeBuiltins->writeFile.Specs2jsonWriteFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConvertBuiltins(t *testing.T) {
	dir := t.TempDir()
	in := writeTemp(t, dir, "cache.ts", `export const zshBuiltinsCommandDescriptionsCache = {
  "cd": { "shortDescription": "Change directory", "description": "long", "args": "cd [ -qsLP ] [ arg ]\nmore" },
  ".": { "shortDescription": "Source a file", "description": "x" },
};`)
	out := filepath.Join(dir, "o.json")
	if err := run([]string{"builtins", in, out}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":".","description":"Source a file"},{"name":"cd","description":"Change directory","args":"cd [ -qsLP ] [ arg ]"}]`
	if strings.TrimSpace(string(b)) != want {
		t.Errorf("got %s\nwant %s", b, want)
	}
}

func TestTheJSONOfASpecRoundTrips(t *testing.T) {
	spec, err := convertSource(sample)
	if err != nil {
		t.Fatal(err)
	}
	first, err := encode(spec)
	if err != nil {
		t.Fatal(err)
	}
	var back Spec
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatal(err)
	}
	second, err := encode(&back)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("the spec changed through JSON:\n%s\n%s", first, second)
	}
	if len(back.Subcommands) != 3 || back.Subcommands[0].Name[1] != "r" || !back.Subcommands[1].Hidden || !back.Subcommands[2].Deprecated {
		t.Errorf("the decoded spec is wrong: %+v", back)
	}
}
