package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The TypeScript that is chosen for a start (Decide): folders the tests make, nothing from the machine.

// layout is a machine of a test: the npm -g folder with the server and, when given, a TypeScript beside it, the project folder with a
// TypeScript of its own, and the home folder.
type layout struct {
	base, server, folder, home string
}

// makeLayout makes the folders; globalTS and workspaceTS are the versions of the packages ("" for none).
func makeLayout(t *testing.T, globalTS, workspaceTS string) layout {
	t.Helper()
	base := t.TempDir()
	l := layout{base: base, folder: filepath.Join(base, "home", "project"), home: filepath.Join(base, "home")}
	l.server = writeFile(t, base, "npm/lib/node_modules/typescript-language-server/lib/cli.mjs", "", 0o755)
	if err := os.MkdirAll(l.folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if globalTS != "" {
		fakeTypeScript(t, filepath.Join(base, "npm", "lib", "node_modules", "typescript"), globalTS, true)
	}
	if workspaceTS != "" {
		fakeTypeScript(t, filepath.Join(l.folder, "node_modules", "typescript"), workspaceTS, true)
	}
	return l
}

func (l layout) env() langserver.Env {
	return langserver.Env{Folder: l.folder, Home: l.home, Path: ""}
}

func TestDecideChoosesTheTypeScriptTheUserAllowed(t *testing.T) {
	tests := []struct {
		name                  string
		global, workspace     string
		tsdk, chosen          string
		wantUse, wantSource   string
		wantGlobal, wantSpace bool
	}{
		{"the one beside the server by default", "6.0.3", "5.9.3", "", "", "6.0.3", SourceGlobal, true, true},
		{"the one beside the server when only that is chosen", "6.0.3", "5.9.3", "", ChoiceGlobal, "6.0.3", SourceGlobal, true, true},
		{"the folder's own only when chosen", "6.0.3", "5.9.3", "", ChoiceWorkspace, "5.9.3", SourceWorkspace, true, true},
		{"the choice of the folder's own when it has none", "6.0.3", "", "", ChoiceWorkspace, "6.0.3", SourceGlobal, true, false},
		{"the folder has none", "6.0.3", "", "", "", "6.0.3", SourceGlobal, true, false},
		{"the folder's own is the only one, and chosen", "", "5.9.3", "", ChoiceWorkspace, "5.9.3", SourceWorkspace, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := makeLayout(t, tt.global, tt.workspace)
			p, err := Decide(l.env(), l.server, l.folder, tt.tsdk, tt.chosen)
			if err != nil {
				t.Fatal(err)
			}
			if p.Use.Number != tt.wantUse || p.Use.Source != tt.wantSource {
				t.Errorf("use = %+v, want %s from %s", p.Use, tt.wantUse, tt.wantSource)
			}
			if (p.Global != nil) != tt.wantGlobal || (p.Workspace != nil) != tt.wantSpace {
				t.Errorf("global %v, workspace %v", p.Global, p.Workspace)
			}
		})
	}
}

func TestDecideFailsWhenOnlyTheFoldersOwnTypeScriptIsThereAndNothingChoseIt(t *testing.T) {
	l := makeLayout(t, "", "5.9.3")
	_, err := Decide(l.env(), l.server, l.folder, "", "")
	if err == nil {
		t.Fatal("the folder's own TypeScript was used without the user's say")
	}
	wantRoute(t, err, "Decide.TypeScriptNotFound")
	for _, want := range []string{"no TypeScript was found beside the language server", "its own TypeScript, 5.9.3", "only if you choose it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message lacks %q: %v", want, err)
		}
	}
	l = makeLayout(t, "", "")
	_, err = Decide(l.env(), l.server, l.folder, "", "")
	wantRoute(t, err, "Decide.TypeScriptNotFound")
	if !strings.Contains(err.Error(), "npm install -g typescript@6") || strings.Contains(err.Error(), "workspace") {
		t.Errorf("message = %v", err)
	}
}

func TestATypeScriptWithoutATsserverIsNotUsable(t *testing.T) {
	// TypeScript 7 is the native compiler: no tsserver.js, so neither beside the server nor in the folder can it run
	base := t.TempDir()
	server := writeFile(t, base, "lib/node_modules/typescript-language-server/lib/cli.mjs", "", 0o755)
	fakeTypeScript(t, filepath.Join(base, "lib", "node_modules", "typescript"), "7.0.2", false)
	folder := filepath.Join(base, "home", "project")
	fakeTypeScript(t, filepath.Join(folder, "node_modules", "typescript"), "7.0.2", false)
	_, err := Decide(langserver.Env{Folder: folder, Home: filepath.Join(base, "home")}, server, folder, "", ChoiceWorkspace)
	wantRoute(t, err, "Decide.TypeScriptNotFound")
}

func TestTheTsdkSettingNamesTheLibTheFolderThePackageOrTheFile(t *testing.T) {
	l := makeLayout(t, "6.0.3", "")
	other := filepath.Join(l.base, "elsewhere", "typescript")
	fakeTypeScript(t, other, "5.8.3", true)
	empty := filepath.Join(l.base, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, tsdk string
		want       string
		route      string
	}{
		{"the lib folder", filepath.Join(other, "lib"), "5.8.3", ""},
		{"the package folder", other, "5.8.3", ""},
		{"tsserver.js", filepath.Join(other, "lib", "tsserver.js"), "5.8.3", ""},
		{"relative to the folder", "../../elsewhere/typescript/lib", "5.8.3", ""},
		{"with ~", "~/../elsewhere/typescript", "5.8.3", ""},
		{"a folder with no TypeScript", empty, "", "TsdkVersion.TypeScriptTsdkInvalid"},
		{"a file that is not there", filepath.Join(l.base, "no", "lib"), "", "TsdkVersion.TypeScriptTsdkInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Decide(l.env(), l.server, l.folder, tt.tsdk, "")
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				if !strings.Contains(err.Error(), "typescript.tsdk is set to") {
					t.Errorf("message = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Use.Number != tt.want || p.Use.Source != SourceTsdk {
				t.Errorf("use = %+v", p.Use)
			}
		})
	}
}

func TestTheTypeScriptBesideTheServerIsFoundTheWayNodeResolvesIt(t *testing.T) {
	base := t.TempDir()
	// a flat node_modules, as pnpm and a local install make it: the server is in node_modules/<name>/lib, the TypeScript beside it
	flat := writeFile(t, base, "flat/node_modules/typescript-language-server/lib/cli.mjs", "", 0o755)
	fakeTypeScript(t, filepath.Join(base, "flat", "node_modules", "typescript"), "6.0.3", true)
	// the server's own node_modules holds one
	own := writeFile(t, base, "own/node_modules/typescript-language-server/lib/cli.mjs", "", 0o755)
	fakeTypeScript(t, filepath.Join(base, "own", "node_modules", "typescript-language-server", "node_modules", "typescript"), "6.0.2", true)
	// the server is reached through a link in a bin folder
	link := filepath.Join(base, "bin", "typescript-language-server")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(flat, link); err != nil {
		t.Fatal(err)
	}
	// nothing beside this one
	none := writeFile(t, base, "none/lib/cli.mjs", "", 0o755)
	tests := []struct {
		name, server, want string
	}{
		{"beside the server", flat, "6.0.3"},
		{"through the link of a bin folder", link, "6.0.3"},
		{"in the server's own node_modules", own, "6.0.2"},
		{"nothing", none, ""},
		{"no such server", filepath.Join(base, "no-such-server"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := BesideServer(tt.server)
			if ok != (tt.want != "") || v.Number != tt.want {
				t.Errorf("BesideServer = %+v, %v; want %q", v, ok, tt.want)
			}
		})
	}
}

func TestTheFoldersOwnTypeScriptIsFoundWithinTheProject(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repo := filepath.Join(home, "repo")
	fakeTypeScript(t, filepath.Join(repo, "node_modules", "typescript"), "5.9.3", true)
	writeFile(t, repo, ".git/HEAD", "", 0o644)
	for _, d := range []string{filepath.Join(repo, "packages", "a", "src"), filepath.Join(home, "notes")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a node_modules in the home folder is not a project's
	fakeTypeScript(t, filepath.Join(home, "node_modules", "typescript"), "4.0.0", true)
	// a package of the repository with its own
	fakeTypeScript(t, filepath.Join(repo, "packages", "b", "node_modules", "typescript"), "5.8.3", true)
	tests := []struct {
		name, root, want string
	}{
		{"in the folder itself", filepath.Join(repo), "5.9.3"},
		{"a folder of the repository finds the repository's", filepath.Join(repo, "packages", "a", "src"), "5.9.3"},
		{"the nearest wins", filepath.Join(repo, "packages", "b"), "5.8.3"},
		{"the home folder is not climbed into", filepath.Join(home, "notes"), ""},
		{"an empty root", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := WorkspaceVersion(tt.root, home)
			if ok != (tt.want != "") || v.Number != tt.want {
				t.Errorf("WorkspaceVersion(%s) = %+v, %v; want %q", tt.root, v, ok, tt.want)
			}
			if ok && v.Source != SourceWorkspace {
				t.Errorf("source = %q", v.Source)
			}
		})
	}
}

func TestTheLabelsOfAVersion(t *testing.T) {
	tests := []struct {
		v    Version
		want string
	}{
		{Version{Source: SourceGlobal, Number: "6.0.3"}, "TypeScript 6.0.3"},
		{Version{Source: SourceTsdk, Number: "5.8.3"}, "TypeScript 5.8.3"},
		{Version{Source: SourceWorkspace, Number: "5.9.3"}, "TypeScript 5.9.3 (workspace)"},
		{Version{Source: SourceGlobal}, "TypeScript"},
	}
	for _, tt := range tests {
		if got := tt.v.Label(); got != tt.want {
			t.Errorf("Label(%+v) = %q, want %q", tt.v, got, tt.want)
		}
	}
	if got := (Version{Lib: "/x/lib"}).Server(); got != "/x/lib/tsserver.js" {
		t.Errorf("Server = %q", got)
	}
}

func TestPackageVersionReadsOnlyATypeScriptPackage(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "a/package.json", `{"name":"typescript","version":"6.0.3"}`, 0o644)
	writeFile(t, base, "b/package.json", `{"name":"not-typescript","version":"1.0.0"}`, 0o644)
	writeFile(t, base, "c/package.json", `{bad`, 0o644)
	for _, tt := range []struct{ dir, want string }{{"a", "6.0.3"}, {"b", ""}, {"c", ""}, {"d", ""}} {
		if got := packageVersion(filepath.Join(base, tt.dir, "lib")); got != tt.want {
			t.Errorf("packageVersion(%s) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}
