//go:build emetos_display

package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The language servers of milestone M24b in the real editor (step 24b.10, SPEC §14 "Language servers"): for each real server, the
// time from the start of the process to its first diagnostics in the Problems panel on its project of testdata/, and the frames
// of the UI thread while text is typed with the server running (the same frame statistics as the typing of scripts/perf.sh). They
// print what they measured and are not part of the default run of scripts/realinput.sh; they run alone, on a release build:
//
//	go build -tags emetos_dev -o <binary> ./cmd/emetos
//	EMETOS_DISPLAY_BINARY=<binary> scripts/heavy.sh --alone -- scripts/headless-wayland.sh -- go test -tags emetos_display ./cmd/emetos -run TestPerfLangServers -v
//
// The servers are the ones of /var/tmp/emetos-tools (scripts/fetch-tools.sh) and the system's clangd, Java and dotnet; a case is
// skipped when its program is missing. The editor's HOME and XDG folders are the temporary ones of the harness, and the JVM and
// the .NET SDK get the folders and settings the real-server tests of the packs give them.

// langPerfCase is one server.
type langPerfCase struct {
	// name is the label of the row.
	name string
	// fixture is the folder of testdata/ the project is copied from, open the file to open and comment the comment mark of the
	// language.
	fixture, open, comment string
	// want is a text of the message of the first diagnostic that the editor's own text does not hold; empty for a case with no
	// server.
	want string
	// wait is how long the server may take to its first diagnostics.
	wait time.Duration
	// prepare gives the user settings and the environment of the editor, or a reason to skip the case.
	prepare func(t *testing.T) (settings map[string]any, env []string, skip string)
	// patch, when set, turns the copy of the fixture into the project (the compile commands, a library).
	patch func(t *testing.T, dir string)
}

// toolsFolder is the folder of the tools the real-server tests use.
func toolsFolder() string {
	if tools := os.Getenv("EMETOS_TOOLS"); tools != "" {
		return tools
	}
	return "/var/tmp/emetos-tools"
}

// copyTestdata copies a folder of testdata/ into dir.
func copyTestdata(t *testing.T, fixture, dir string) {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", filepath.FromSlash(fixture))
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// writeProjectFile writes a file of the project.
func writeProjectFile(t *testing.T, dir, rel, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// toolOrSkip is the path of a program of the tools folder, or the reason to skip.
func toolOrSkip(rel string) (string, string) {
	path := filepath.Join(toolsFolder(), filepath.FromSlash(rel))
	if _, err := os.Stat(path); err != nil {
		return "", rel + " is not installed (scripts/fetch-tools.sh)"
	}
	return path, ""
}

// pythonCase is a Python server.
func pythonCase(label, server, pathKey, program, want string) langPerfCase {
	return langPerfCase{name: label, fixture: "python/project", open: "main.py", comment: "#", want: want, wait: 120 * time.Second,
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			path, skip := toolOrSkip(program)
			if skip != "" {
				return nil, nil, skip
			}
			return map[string]any{"python.languageServer": server, pathKey: path}, nil, ""
		}}
}

var langPerfCases = []langPerfCase{
	{name: "rust-analyzer", fixture: "rust/crate", open: "src/main.rs", comment: "//", want: "expected i32", wait: 180 * time.Second,
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			path, skip := toolOrSkip("bin/rust-analyzer")
			if skip != "" {
				return nil, nil, skip
			}
			if _, err := exec.LookPath("cargo"); err != nil {
				return nil, nil, "cargo is not installed"
			}
			return map[string]any{"rust-analyzer.server.path": path}, []string{"CARGO_HOME=" + t.TempDir(), "CARGO_TARGET_DIR=" + t.TempDir()}, ""
		},
		// the fixture has no error; this is the text of the real-server test of the pack. rust-analyzer's own diagnostic ("expected
		// i32, found &'static str") comes first; `cargo check` follows with rustc's "mismatched types"
		patch: func(t *testing.T, dir string) {
			writeProjectFile(t, dir, "src/main.rs", "mod geometry;\n\nfn main() {\n    let unused = 5;\n    let wrong: i32 = \"text\";\n}\n")
		}},
	{name: "clangd", fixture: "cpp/project", open: "src/main.cpp", comment: "//", want: "undeclared", wait: 120 * time.Second,
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			path, err := exec.LookPath("clangd")
			if err != nil {
				return nil, nil, "clangd is not installed"
			}
			return map[string]any{"clangd.path": path}, nil, ""
		},
		patch: func(t *testing.T, dir string) {
			in, err := os.ReadFile(filepath.Join(dir, "compile_commands.json.in"))
			if err != nil {
				t.Fatal(err)
			}
			writeProjectFile(t, dir, "compile_commands.json", strings.ReplaceAll(string(in), "@DIR@", dir))
		}},
	pythonCase("basedpyright", "basedpyright", "basedpyright.path", "venv/bin/basedpyright-langserver", "is not defined"),
	pythonCase("pyright", "pyright", "pyright.path", "node/node_modules/.bin/pyright-langserver", "is not defined"),
	pythonCase("pylsp", "pylsp", "pylsp.path", "venv/bin/pylsp", "undefined name"),
	{name: "typescript-language-server", fixture: "typescript/project", open: "src/main.ts", comment: "//", want: "not assignable", wait: 120 * time.Second,
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			path, skip := toolOrSkip("node/node_modules/.bin/typescript-language-server")
			if skip != "" {
				return nil, nil, skip
			}
			if _, skip := toolOrSkip("node/node_modules/typescript"); skip != "" {
				return nil, nil, skip
			}
			node, err := exec.LookPath("node")
			if err != nil {
				return nil, nil, "node is not installed"
			}
			return map[string]any{"typescript.languageServer.path": path, "typescript.disableAutomaticTypeAcquisition": true},
				[]string{"PATH=" + filepath.Dir(node) + ":/usr/bin:/bin"}, ""
		},
		// a library with a declaration file, as the packages of npm have (the real-server test of the pack does the same)
		patch: func(t *testing.T, dir string) {
			writeProjectFile(t, dir, "node_modules/emetoslib/package.json", `{"name":"emetoslib","version":"1.0.0","type":"module","main":"index.js","types":"index.d.ts"}`)
			writeProjectFile(t, dir, "node_modules/emetoslib/index.d.ts", "/** Say hello to name. */\nexport declare function greet(name: string): string;\n")
			writeProjectFile(t, dir, "node_modules/emetoslib/index.js", "export function greet(name) {\n  return \"hello \" + name;\n}\n")
		}},
	{name: "jdtls", fixture: "java/plain", open: "src/main/java/dev/emetos/app/App.java", comment: "//", want: "Type mismatch", wait: 180 * time.Second,
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			jdtls, skip := toolOrSkip("jdtls")
			if skip != "" {
				return nil, nil, skip
			}
			java, err := exec.LookPath("java")
			if err != nil {
				return nil, nil, "java is not installed"
			}
			// HOME and XDG_CACHE_HOME are the harness's temporary folders; JAVA_HOME is not set
			return map[string]any{"java.jdt.ls.path": jdtls}, []string{"PATH=" + filepath.Dir(java) + ":/usr/bin:/bin"}, ""
		}},
	{name: "csharp-ls", fixture: "csharp/solution", open: "App/Program.cs", comment: "//", want: "Cannot implicitly convert", wait: 240 * time.Second,
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			path, skip := toolOrSkip("dotnet-tools/csharp-ls")
			if skip != "" {
				return nil, nil, skip
			}
			dotnet, err := exec.LookPath("dotnet")
			if err != nil {
				return nil, nil, "dotnet is not installed"
			}
			// the isolation of the C# step (isolateDotnet): HOME and the XDG folders are the harness's temporary ones, the SDK's
			// folders are temporary ones too, and its telemetry and first-run experience are off
			home := t.TempDir()
			return map[string]any{"csharp.languageServer.path": path}, []string{
				"DOTNET_CLI_HOME=" + filepath.Join(home, "dotnet-home"),
				"NUGET_PACKAGES=" + filepath.Join(home, "nuget-packages"),
				"NUGET_HTTP_CACHE_PATH=" + filepath.Join(home, "nuget-http"),
				"DOTNET_CLI_TELEMETRY_OPTOUT=1",
				"DOTNET_NOLOGO=1",
				"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
				"DOTNET_ROOT=",
				"PATH=" + filepath.Dir(dotnet) + ":/usr/bin:/bin",
			}, ""
		}},
}

// noServerCases are the same typing in a file of the language with no server: the baseline of each grammar (the cost of the
// frame that shows a typed character is mostly the TextMate grammar's, which differs between languages). Python has its server
// turned off; the others name a program that is not there, so the server never runs.
var noServerCases = []langPerfCase{
	{name: "no server (Python)", fixture: "python/project", open: "main.py", comment: "#",
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			return map[string]any{"python.languageServer": "none"}, nil, ""
		}},
	{name: "no server (TypeScript)", fixture: "typescript/project", open: "src/main.ts", comment: "//",
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			return map[string]any{"typescript.languageServer.path": filepath.Join(t.TempDir(), "none")}, nil, ""
		}},
	{name: "no server (C++)", fixture: "cpp/project", open: "src/main.cpp", comment: "//",
		prepare: func(t *testing.T) (map[string]any, []string, string) {
			return map[string]any{"clangd.path": filepath.Join(t.TempDir(), "none")}, nil, ""
		}},
}

// typedLine is a line of the typing of the measurement: a comment, so that the text stays valid and no server has more to say.
func typedLine(comment string) string {
	return comment + " the quick brown fox jumps over the lazy dog"
}

// typedLines is how many lines are typed, each followed by Return: about 620 characters, as a person types them.
const typedLines = 14

// signalEditor sends a signal to the editor process (SIGUSR2 starts and ends a CPU profile of a build with emetos_dev).
func signalEditor(t *testing.T, a *display.App, sig syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(a.Pid(), sig); err != nil {
		t.Fatal(err)
	}
}

// runLangPerf measures one case: the time to the first diagnostics, then the frames of the typing.
func runLangPerf(t *testing.T, c langPerfCase) {
	settings, env, skip := c.prepare(t)
	if skip != "" {
		t.Skip(skip)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	profile := os.Getenv("EMETOS_LANGPERF_PROFILE") // a folder: a CPU profile of the typing goes there, one folder for each case
	if profile != "" {
		env = append(env, "EMETOS_PROFILE_DIR="+filepath.Join(profile, strings.ReplaceAll(c.name, " ", "-")))
	}
	started := time.Now()
	a := display.Start(t, display.Options{
		Args:   []string{".", c.open},
		Config: map[string]string{"settings.json": string(data)},
		Prepare: func(dir string) {
			copyTestdata(t, c.fixture, dir)
			if c.patch != nil {
				c.patch(t, dir)
			}
		},
		// the Problems panel is open from the first frame, so that the first diagnostics are seen when they arrive
		Env: append([]string{"GLUTTER_FRAME_STATS=1", `EMETOS_DEV_COMMANDS=[["workbench.actions.view.problems"]]`}, env...),
	})
	first := "no diagnostics wanted"
	if c.want != "" {
		a.EventuallyFor(c.wait, "the first diagnostics of "+c.name, func() (bool, string) {
			for _, tx := range a.Texts() {
				if strings.Contains(tx.Text, c.want) {
					return true, ""
				}
			}
			return false, "no text with " + c.want + "; the window shows:\n" + a.Dump()
		})
		first = time.Since(started).Round(10*time.Millisecond).String() + " after the process started"
		if f, ok := a.FirstFrame(); ok {
			first += " (the first frame at " + f.SinceProcessStart.Round(10*time.Millisecond).String() + ")"
		}
	}
	// the cursor goes to the end of the file and the text is typed there
	a.Click(editorColumnX(30), editorLine(2))
	a.Press("ctrl+End", "Return")
	a.Settle()
	settleFrameWindows(a)
	before := len(a.FrameWindows())
	line := typedLine(c.comment)
	typing := time.Now()
	if profile != "" {
		signalEditor(t, a, syscall.SIGUSR2)
	}
	for range typedLines {
		a.Type(line)
		a.Press("Return")
	}
	a.Eventually("the typed text", func() (bool, string) {
		got := a.Editor().Text
		return strings.Count(got, line) >= typedLines, got
	})
	took := time.Since(typing).Round(10 * time.Millisecond)
	if profile != "" {
		signalEditor(t, a, syscall.SIGUSR2)
		time.Sleep(time.Second) // the profile is written
	}
	settleFrameWindows(a)
	s := display.Summarize(a.FrameWindows()[before:])
	t.Logf("PERF langserver %s: first diagnostics %s; typing %d characters in %v: %d frames, the UI thread %v on average and %v at worst, the renderer %v and %v, %d frames over 16 ms",
		c.name, first, typedLines*(len(line)+1), took, s.Frames, s.BuildAvg, s.BuildMax, s.RasterAvg, s.RasterMax, s.Over16)
}

// TestPerfLangServers measures every real server of M24b, and the baseline with no server.
func TestPerfLangServers(t *testing.T) {
	for _, c := range append(slices.Clone(noServerCases), langPerfCases...) {
		t.Run(c.name, func(t *testing.T) { runLangPerf(t, c) })
	}
}
