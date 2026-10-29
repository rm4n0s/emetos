package containerscaffold

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

var update = flag.Bool("update-scaffold", false, "write the files of testdata/ from what Generate makes")

// project makes the folder of a project with the files the platforms read.
func project(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "My App")
	put := func(rel, text string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("api/package.json", `{"name":"@me/My-API","version":"2.1.0","scripts":{"start":"node server.js"}}`)
	put("main-only/package.json", `{"name":"mainonly","main":"lib/index.js"}`)
	put("bare/package.json", `{}`)
	put("App.csproj", `<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><TargetFramework>net8.0</TargetFramework><AssemblyName>Shop.Web</AssemblyName></PropertyGroup></Project>`)
	put("old/Old.csproj", `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFrameworks>netcoreapp3.1;net6.0</TargetFrameworks></PropertyGroup></Project>`)
	put("bad/Bad.csproj", `<Project><PropertyGroup><TargetFramework>unknown</TargetFramework></PropertyGroup></Project>`)
	put("web/mysite/wsgi.py", "")
	put("web/manage.py", "")
	return dir
}

// TestGenerate holds the files of every platform against testdata/<case>/: the text of the templates of Container Tools, filled.
// Run it with -update-scaffold to write them again, and read the diff.
func TestGenerate(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want []string // the paths of the files, in order
	}{
		{"go", Input{Platform: Go, Ports: []int{3000}, Compose: true}, []string{".dockerignore", "Dockerfile", "compose.yaml"}},
		{"other", Input{Platform: Other}, []string{".dockerignore", "Dockerfile"}},
		{"node", Input{Platform: NodeJS, Artifact: "api/package.json", Ports: []int{3000, 9229}, Compose: true}, []string{"api/.dockerignore", "api/Dockerfile", "compose.yaml"}},
		{"node-main", Input{Platform: NodeJS, Artifact: "main-only/package.json"}, []string{"main-only/.dockerignore", "main-only/Dockerfile"}},
		{"node-bare", Input{Platform: NodeJS, Artifact: "bare/package.json", Ports: []int{3000}}, []string{"bare/.dockerignore", "bare/Dockerfile"}},
		{"aspnet", Input{Platform: NetCore, Artifact: "App.csproj", Ports: []int{5000}, Compose: true}, []string{".dockerignore", "Dockerfile", "compose.yaml"}},
		{"aspnet-nested", Input{Platform: NetCore, Artifact: "old/Old.csproj", Ports: []int{80}, Compose: true}, []string{".dockerignore", "old/Dockerfile", "compose.yaml"}},
		{"net-console", Input{Platform: NetConsole, Artifact: "App.csproj"}, []string{".dockerignore", "Dockerfile"}},
		{"flask", Input{Platform: Flask, Artifact: "src/app.py", Ports: []int{5000}, Compose: true}, []string{".dockerignore", "Dockerfile", "requirements.txt", "compose.yaml"}},
		{"django", Input{Platform: Django, Artifact: "web/manage.py", Ports: []int{8000}}, []string{".dockerignore", "Dockerfile", "requirements.txt"}},
		{"django-nowsgi", Input{Platform: Django, Artifact: "manage.py"}, []string{".dockerignore", "Dockerfile", "requirements.txt"}},
		{"fastapi-module", Input{Platform: FastAPI, Module: "pkg.main", Ports: []int{80}}, []string{".dockerignore", "Dockerfile", "requirements.txt"}},
		{"python", Input{Platform: PythonOther, Artifact: "main.py"}, []string{".dockerignore", "Dockerfile", "requirements.txt"}},
		{"containerfile", Input{Platform: Go, Ports: []int{8080}, Compose: true, FileName: "Containerfile"}, []string{".dockerignore", "Containerfile", "compose.yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Folder = project(t)
			files, err := Generate(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, f := range files {
				paths = append(paths, f.Path)
			}
			if !reflect.DeepEqual(paths, tc.want) {
				t.Fatalf("files %q, want %q", paths, tc.want)
			}
			for _, f := range files {
				golden := filepath.Join("testdata", tc.name, filepath.FromSlash(f.Path))
				if *update {
					if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, []byte(f.Content), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (run with -update-scaffold)", err)
				}
				if f.Content != string(want) {
					t.Errorf("%s differs from %s:\n%s\nwant:\n%s", f.Path, golden, f.Content, want)
				}
			}
			// requirements.txt keeps the packages the user has; nothing else does
			for _, f := range files {
				if f.KeepExisting != (f.Path == "requirements.txt") {
					t.Errorf("%s: KeepExisting is %v", f.Path, f.KeepExisting)
				}
			}
		})
	}
}

// TestGeneratedFacts holds the facts that are read from the project, apart from the text around them.
func TestGeneratedFacts(t *testing.T) {
	gen := func(in Input) map[string]string {
		in.Folder = project(t)
		files, err := Generate(in)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, f := range files {
			out[f.Path] = f.Content
		}
		return out
	}
	tests := []struct {
		name string
		in   Input
		file string
		has  []string
		not  []string
	}{
		{"npm start", Input{Platform: NodeJS, Artifact: "api/package.json"}, "api/Dockerfile", []string{`CMD ["npm", "start"]`}, nil},
		{"main of package.json", Input{Platform: NodeJS, Artifact: "main-only/package.json"}, "main-only/Dockerfile", []string{`CMD ["node", "lib/index.js"]`}, nil},
		{"the name and version of package.json", Input{Platform: NodeJS, Artifact: "api/package.json", Compose: true}, "compose.yaml",
			[]string{"memyapi:", "context: api", "dockerfile: ./Dockerfile", "NODE_ENV: production"}, nil},
		{"the version in the Go label", Input{Platform: Go}, "Dockerfile", []string{"LABEL Name=myapp Version=0.0.1"}, []string{"EXPOSE"}},
		{".NET 8 runs as app", Input{Platform: NetCore, Artifact: "App.csproj", Ports: []int{5000}}, "Dockerfile", []string{"aspnet:8.0", "sdk:8.0", "USER app", "ASPNETCORE_URLS=http://+:5000", `"Shop.Web.dll"`}, []string{"adduser"}},
		{".NET Core 3.1 makes a user", Input{Platform: NetConsole, Artifact: "old/Old.csproj"}, "old/Dockerfile", []string{"runtime:3.1", "adduser -u 5678", `"Old.dll"`, `COPY ["old/Old.csproj", "old/"]`}, []string{"USER app\n"}},
		{"a port below 1024 keeps root", Input{Platform: Flask, Artifact: "app.py", Ports: []int{80}}, "Dockerfile", []string{"Warning: A port below 1024", "0.0.0.0:80"}, []string{"adduser", "USER appuser"}},
		{"Flask module", Input{Platform: Flask, Artifact: "src/app.py"}, "Dockerfile", []string{`"src.app:app"`, "0.0.0.0:5000"}, nil},
		{"FastAPI worker", Input{Platform: FastAPI, Module: "pkg.main"}, "Dockerfile", []string{`"uvicorn.workers.UvicornWorker", "pkg.main:app"]`, "0.0.0.0:8000"}, nil},
		{"Django finds wsgi", Input{Platform: Django, Artifact: "web/manage.py"}, "Dockerfile", []string{`"mysite.wsgi"]`}, []string{"wsgi.py was not found"}},
		{"Django asks for wsgi", Input{Platform: Django, Artifact: "manage.py"}, "Dockerfile", []string{"# File wsgi.py was not found.", `"pythonPath.to.wsgi"]`}, nil},
		{"python runs the file", Input{Platform: PythonOther, Artifact: "src/run.py"}, "Dockerfile", []string{`CMD ["python", "src/run.py"]`}, nil},
		{"python runs the module", Input{Platform: PythonOther, Module: "pkg.run"}, "Dockerfile", []string{`CMD ["python", "-m", "pkg.run"]`}, nil},
		{"Flask packages", Input{Platform: Flask, Artifact: "app.py"}, "requirements.txt", []string{"flask==3.0.1", "gunicorn==21.2.0"}, nil},
		{"general Python has no packages", Input{Platform: PythonOther, Artifact: "app.py"}, "requirements.txt", nil, []string{"gunicorn", "flask"}},
		{"Python ignores", Input{Platform: Flask, Artifact: "app.py"}, ".dockerignore", []string{"**/__pycache__", "**/.venv", "**/bin"}, nil},
		{"Node does not ignore bin", Input{Platform: NodeJS, Artifact: "api/package.json"}, "api/.dockerignore", []string{"**/node_modules"}, []string{"**/bin", "__pycache__"}},
		{"Containerfile in the compose file", Input{Platform: Go, FileName: "Containerfile", Compose: true}, "compose.yaml", []string{"dockerfile: ./Containerfile"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := gen(tc.in)[tc.file]
			if got == "" {
				t.Fatalf("no file %s", tc.file)
			}
			for _, s := range tc.has {
				if !strings.Contains(got, s) {
					t.Errorf("%s has no %q:\n%s", tc.file, s, got)
				}
			}
			for _, s := range tc.not {
				if strings.Contains(got, s) {
					t.Errorf("%s has %q:\n%s", tc.file, s, got)
				}
			}
		})
	}
}

// TestGenerateFailures holds every failure with its tag and its route.
func TestGenerateFailures(t *testing.T) {
	tests := []struct {
		name  string
		in    Input
		route string
	}{
		{"a platform that is not one", Input{Platform: "Cobol"}, "Generate.ContainerScaffoldPlatformUnknown"},
		{"a port that is not one", Input{Platform: Go, Ports: []int{70000}}, "Generate.ContainerScaffoldPortsInvalid"},
		{"a port of zero", Input{Platform: Go, Ports: []int{0}}, "Generate.ContainerScaffoldPortsInvalid"},
		{"no package.json", Input{Platform: NodeJS}, "Generate.ContainerScaffoldArtifactMissing"},
		{"no project file", Input{Platform: NetCore}, "Generate.ContainerScaffoldArtifactMissing"},
		{"no entry file", Input{Platform: Flask}, "Generate.ContainerScaffoldArtifactMissing"},
		{"a package.json that is not there", Input{Platform: NodeJS, Artifact: "nothing/package.json"}, "context.gatherNode.ContainerScaffoldArtifactInvalid"},
		{"a package.json that is not JSON", Input{Platform: NodeJS, Artifact: "api/Dockerfile", ReadFile: func(string) ([]byte, error) { return []byte("{"), nil }}, "context.gatherNode.ContainerScaffoldArtifactInvalid"},
		{"a project file that is not there", Input{Platform: NetCore, Artifact: "nothing.csproj"}, "context.gatherNet.ContainerScaffoldArtifactInvalid"},
		{"a project file that is not XML", Input{Platform: NetCore, Artifact: "App.csproj", ReadFile: func(string) ([]byte, error) { return []byte("<a><b>"), nil }}, "context.gatherNet.ContainerScaffoldArtifactInvalid"},
		{"a project file with no known framework", Input{Platform: NetCore, Artifact: "bad/Bad.csproj"}, "context.gatherNet.ContainerScaffoldArtifactInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Folder = project(t)
			_, err := Generate(tc.in)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), tc.route)
			}
		})
	}
}

func TestTemplatesThatAreMissingOrBroken(t *testing.T) {
	c := &context{files: fstest.MapFS{
		"templates/broken.tmpl": {Data: []byte("{{ .Nothing.Deeper }}")},
		"templates/syntax.tmpl": {Data: []byte("{{ if }}")},
	}}
	for name, tag := range map[string]string{"missing.tmpl": "ContainerScaffoldTemplateMissing", "broken.tmpl": "ContainerScaffoldTemplateInvalid", "syntax.tmpl": "ContainerScaffoldTemplateInvalid"} {
		_, err := c.render(name)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("context.render."+tag) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestParsePorts(t *testing.T) {
	tests := []struct {
		in      string
		want    []int
		invalid bool
	}{
		{"", nil, false}, {"  ", nil, false}, {"3000", []int{3000}, false}, {"80, 443,8080", []int{80, 443, 8080}, false},
		{"0", nil, true}, {"65536", nil, true}, {"-1", nil, true}, {"80,,81", nil, true}, {"http", nil, true}, {"80;81", nil, true},
	}
	for _, tc := range tests {
		got, err := ParsePorts(tc.in)
		if tc.invalid {
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("ParsePorts.ContainerScaffoldPortsInvalid") {
				t.Errorf("ParsePorts(%q): %v", tc.in, err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParsePorts(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestImageNameAndPlatformFacts(t *testing.T) {
	for in, want := range map[string]string{"My App": "myapp", "@me/My-API": "memyapi", "---": "image", "": "image", "Üñí9": "9"} {
		if got := ImageName(in); got != want {
			t.Errorf("ImageName(%q) = %q, want %q", in, got, want)
		}
	}
	asks := map[Platform]bool{NodeJS: true, Go: true, Django: true, FastAPI: true, Flask: true, NetCore: true, NetConsole: false, PythonOther: false, Other: false}
	for p, want := range asks {
		if p.AsksPorts() != want {
			t.Errorf("%s asks for ports: %v", p, p.AsksPorts())
		}
		if (len(p.DefaultPorts()) > 0) != want {
			t.Errorf("%s has default ports %v", p, p.DefaultPorts())
		}
	}
	kinds := map[Platform]Artifact{NodeJS: PackageJSON, NetCore: ProjectFile, NetConsole: ProjectFile, Flask: PythonEntry, PythonOther: PythonEntry, Go: NoArtifact, Other: NoArtifact}
	for p, want := range kinds {
		if p.ArtifactKind() != want {
			t.Errorf("%s needs %v", p, p.ArtifactKind())
		}
	}
	if len(Platforms) != 9 {
		t.Errorf("%d platforms", len(Platforms))
	}
}
