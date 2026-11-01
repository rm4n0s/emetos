package rust

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The Run lenses: the runnables rust-analyzer answers become lenses that run cargo through the Tasks extension.

// lensProvider is the provider of code lenses the pack registered.
func (r *rig) lensProvider() extension.CodeLensProvider {
	r.t.Helper()
	for _, p := range r.api.Providers() {
		if p.Feature == "codeLens" {
			if got := p.Selector.Languages; len(got) != 1 || got[0] != "rust" {
				r.t.Errorf("the lenses are for %v", got)
			}
			return p.Provider.(extension.CodeLensProvider)
		}
	}
	r.t.Fatal("no provider of code lenses")
	return nil
}

// runnables is what rust-analyzer answers for a file with a main, a test and a doctest.
func runnables(uri string) string {
	loc := func(line int) string {
		return `{"targetUri":"` + uri + `","targetRange":{"start":{"line":` + itoaT(line) + `,"character":0},"end":{"line":` + itoaT(line+2) + `,"character":1}},` +
			`"targetSelectionRange":{"start":{"line":` + itoaT(line) + `,"character":3},"end":{"line":` + itoaT(line) + `,"character":7}}}`
	}
	return `[
	{"label":"cargo run -p pointy --bin pointy","location":` + loc(1) + `,"kind":"cargo","args":{"cwd":"@CWD@","workspaceRoot":"@CWD@","cargoArgs":["run","--package","pointy","--bin","pointy"],"executableArgs":[]}},
	{"label":"test halfway","location":` + loc(10) + `,"kind":"cargo","args":{"cwd":"@CWD@/sub dir","workspaceRoot":"@CWD@","cargoArgs":["test","--package","pointy","--bin","pointy"],"executableArgs":["tests::halfway","--exact","--show-output"],"environment":{"RUST_BACKTRACE":"short","BAD NAME":"x","MSG":"it's"}}},
	{"label":"test-mod tests","location":` + loc(8) + `,"kind":"cargo","args":{"cwd":"@CWD@","workspaceRoot":"@CWD@","cargoArgs":["test","--package","pointy","--bin","pointy"],"executableArgs":["tests"]}},
	{"label":"bench fast","location":` + loc(14) + `,"kind":"cargo","args":{"cwd":"@CWD@","workspaceRoot":"@CWD@","cargoArgs":["bench"],"executableArgs":["fast"],"overrideCargo":"my cargo"}},
	{"label":"doctest","location":` + loc(3) + `,"kind":"cargo","args":{"cwd":"@CWD@","workspaceRoot":"@CWD@","cargoArgs":["test","--doc","--package","pointy"],"executableArgs":["geometry::midpoint"]}},
	{"label":"script","location":` + loc(5) + `,"kind":"shell","args":{"cwd":"@CWD@","program":"./run.sh","args":["a b"]}},
	{"label":"empty shell","location":` + loc(5) + `,"kind":"shell","args":{"program":""}},
	{"label":"something new","location":` + loc(5) + `,"kind":"future","args":{}},
	{"label":"in another file","location":{"targetUri":"file:///other.rs","targetRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}},"kind":"cargo","args":{"cargoArgs":["run"]}},
	{"label":"no place","kind":"cargo","args":{"cargoArgs":["run"]}}
]`
}

func TestRunnablesBecomeLensesThatRunThroughTheTasksExtension(t *testing.T) {
	r := newRig(t, true)
	r.run()
	uri := "file://" + r.folder + "/src/main.rs"
	r.answer(map[string]string{"experimental/runnables": strings.ReplaceAll(runnables(uri), "@CWD@", r.folder)})
	lenses, err := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri, LanguageID: "rust", Text: strings.Repeat("let x = 1;\n", 20)})
	if err != nil {
		t.Fatal(err)
	}
	type lens struct {
		title, cmdline string
		line, col      int
	}
	var got []lens
	for _, l := range lenses {
		if l.Command.ID != RunTaskCommand || len(l.Command.Arguments) != 1 {
			t.Fatalf("lens %+v", l)
		}
		var cmdline string
		if err := json.Unmarshal(l.Command.Arguments[0], &cmdline); err != nil {
			t.Fatal(err)
		}
		got = append(got, lens{l.Command.Title, cmdline, l.Range.Start.Line, l.Range.Start.Col})
	}
	want := []lens{
		{"Run", "cargo run --package pointy --bin pointy", 1, 3},
		{"Run Test", `cd '` + r.folder + `/sub dir' && MSG='it'\''s' RUST_BACKTRACE=short cargo test --package pointy --bin pointy -- tests::halfway --exact --show-output`, 10, 3},
		{"Run Tests", "cargo test --package pointy --bin pointy -- tests", 8, 3},
		{"Run Bench", "'my cargo' bench -- fast", 14, 3},
		{"Run Doctest", "cargo test --doc --package pointy -- geometry::midpoint", 3, 3},
		{"Run", `./run.sh 'a b'`, 5, 3},
	}
	if len(got) != len(want) {
		t.Fatalf("lenses = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("lens %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	reqs := r.requests("experimental/runnables")
	if len(reqs) != 1 || reqs[0] != `{"textDocument":{"uri":"`+uri+`"},"position":null}` {
		t.Errorf("requests = %v", reqs)
	}
}

func TestTheLensesCountColumnsInTheEncodingOfTheServer(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.api.SetServerEncoding(ServerID, "utf-16")
	uri := "file://" + r.folder + "/src/main.rs"
	one := `[{"label":"run","location":{"targetUri":"` + uri + `","targetRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":9}},"targetSelectionRange":{"start":{"line":0,"character":5},"end":{"line":0,"character":9}}},"kind":"cargo","args":{"cargoArgs":["run"]}}]`
	r.answer(map[string]string{"experimental/runnables": one})
	// "😀" is two UTF-16 units and four bytes: unit 5 is byte 7
	lenses, err := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri, Text: "fn 😀 main() {}\n"})
	if err != nil || len(lenses) != 1 || lenses[0].Range.Start.Col != 7 {
		t.Errorf("lenses = %+v, %v", lenses, err)
	}
}

func TestTheLensesAreOffWhenTheSettingsSayOrTheServerIsNot(t *testing.T) {
	uri := "file:///work/src/main.rs"
	t.Run("lens.enable is off", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.answer(map[string]string{"experimental/runnables": runnables(uri)})
		before := r.api.CodeLensRefreshes()
		r.api.SetSetting(lensEnableSetting, false)
		if r.api.CodeLensRefreshes() != before+1 {
			t.Error("the editors were not asked for the lenses again")
		}
		lenses, err := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri})
		if err != nil || len(lenses) != 0 || len(r.api.ServerRequests(ServerID)) != 0 {
			t.Errorf("lenses = %+v, %v; requests %v", lenses, err, r.api.ServerRequests(ServerID))
		}
		r.api.SetSetting(lensEnableSetting, true)
		if r.api.CodeLensRefreshes() != before+2 {
			t.Error("the editors were not asked for the lenses again when they came back")
		}
		if lenses, _ := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri, Text: strings.Repeat("\n", 20)}); len(lenses) == 0 {
			t.Error("no lenses when they are on again")
		}
	})
	t.Run("lens.run.enable is off", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.api.SetSetting(lensRunEnableSetting, false)
		if lenses, _ := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri}); len(lenses) != 0 {
			t.Errorf("lenses = %+v", lenses)
		}
	})
	t.Run("the server does not run", func(t *testing.T) {
		r := newRig(t, true)
		lenses, err := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri})
		if err != nil || len(lenses) != 0 {
			t.Errorf("lenses = %+v, %v", lenses, err)
		}
	})
	t.Run("the request fails", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) { return nil, errors.New("TestFail", "boom") })
		if _, err := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri}); err == nil {
			t.Error("the failure was hidden")
		}
	})
	t.Run("nothing to run", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.answer(map[string]string{"experimental/runnables": `[]`})
		if lenses, err := r.lensProvider().ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri}); err != nil || len(lenses) != 0 {
			t.Errorf("lenses = %+v, %v", lenses, err)
		}
	})
}

func TestShellWordsAreQuotedForSh(t *testing.T) {
	tests := []struct{ in, want string }{
		{"cargo", "cargo"},
		{"tests::halfway", "tests::halfway"},
		{"--package=a-b_c.d/e,f@g%h+i", "--package=a-b_c.d/e,f@g%h+i"},
		{"", "''"},
		{"a b", "'a b'"},
		{"it's", `'it'\''s'`},
		{"$HOME", "'$HOME'"},
		{"a;rm -rf /", "'a;rm -rf /'"},
		{"x\ny", "'x\ny'"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}
