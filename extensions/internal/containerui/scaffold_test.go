package containerui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// pickLabel chooses the item with a label in the quick pick that is shown.
func pickLabel(t *testing.T, r *rig, label string) {
	t.Helper()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatalf("no quick pick is shown (wanted %q)", label)
	}
	for i, l := range pick.Labels() {
		if l == label {
			pick.Choose(i)
			return
		}
	}
	t.Fatalf("the quick pick %q has no %q", pick.Labels(), label)
}

// text reads a file of the workspace; "" when it is not there.
func (r *rig) text(rel string) string {
	data, err := os.ReadFile(filepath.Join(r.api.Scope().Folder, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestAddDockerFilesAsksThePlatformThePortAndComposeAndWritesTheFiles(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.run("configure", nil)
	pick := r.api.LastPick()
	want := []string{"Node.js", "ASP.NET Core", ".NET: Console", "Python: Django", "Python: FastAPI", "Python: Flask", "Python: General", "Go", "Other"}
	if !reflect.DeepEqual(pick.Labels(), want) {
		t.Fatalf("platforms %q", pick.Labels())
	}
	pickLabel(t, r, "Go")
	in := r.api.LastInput()
	if in.Options().Value != "3000" || in.Validate("70000") == "" || in.Validate("80, x") == "" || in.Validate("") != "" || in.Validate("80, 443") != "" {
		t.Fatalf("the port question: %+v", in.Options())
	}
	in.Accept("8080, 9090")
	if got := r.api.LastPick().Labels(); !reflect.DeepEqual(got, []string{"No", "Yes"}) {
		t.Fatalf("compose question %q", got)
	}
	if r.text("Dockerfile") != "" {
		t.Fatal("a file was written before the last question")
	}
	pickLabel(t, r, "Yes")

	if df := r.text("Dockerfile"); !strings.Contains(df, "FROM golang:alpine AS builder") || !strings.Contains(df, "EXPOSE 8080\nEXPOSE 9090\n") {
		t.Errorf("Dockerfile:\n%s", df)
	}
	if !strings.Contains(r.text(".dockerignore"), "**/node_modules") || !strings.Contains(r.text("compose.yaml"), "- 9090:9090") {
		t.Errorf("the other files are wrong")
	}
	if !r.hasNote("Added .dockerignore, Dockerfile, compose.yaml.") {
		t.Errorf("notes %v", r.notes())
	}
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != "file://"+filepath.Join(r.api.Scope().Folder, "Dockerfile") {
		t.Errorf("the Dockerfile was not opened: %+v", shown)
	}
}

func TestAddDockerFilesNeverReplacesAFileWithoutAsking(t *testing.T) {
	run := func(t *testing.T, r *rig) {
		t.Helper()
		r.run("configure", nil)
		pickLabel(t, r, "Other")
		pickLabel(t, r, "Yes")
	}
	t.Run("Cancel stops and keeps the file", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		mustWrite(t, filepath.Join(r.api.Scope().Folder, "Dockerfile"), "FROM mine\n")
		run(t, r)
		pick := r.api.LastPick()
		if pick.Options().Placeholder != "Do you want to overwrite 'Dockerfile'?" || !reflect.DeepEqual(pick.Labels(), []string{"Overwrite", "Overwrite All", "Cancel"}) {
			t.Fatalf("the question %+v", pick.Options())
		}
		if r.text("Dockerfile") != "FROM mine\n" {
			t.Fatal("the file changed before the answer")
		}
		pickLabel(t, r, "Cancel")
		if r.text("Dockerfile") != "FROM mine\n" || r.text("compose.yaml") != "" || r.text(".dockerignore") == "" {
			t.Errorf("Dockerfile %q, compose %q, ignore %q", r.text("Dockerfile"), r.text("compose.yaml"), r.text(".dockerignore"))
		}
		if !r.hasNote("Added .dockerignore.") {
			t.Errorf("notes %v", r.notes())
		}
	})
	t.Run("Escape is Cancel", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		mustWrite(t, filepath.Join(r.api.Scope().Folder, "Dockerfile"), "FROM mine\n")
		run(t, r)
		r.api.LastPick().Close()
		if r.text("Dockerfile") != "FROM mine\n" || r.text("compose.yaml") != "" {
			t.Error("Escape wrote something")
		}
	})
	t.Run("Overwrite asks again for the next file", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		mustWrite(t, filepath.Join(r.api.Scope().Folder, "Dockerfile"), "FROM mine\n")
		mustWrite(t, filepath.Join(r.api.Scope().Folder, "compose.yaml"), "services: {}\n")
		run(t, r)
		pickLabel(t, r, "Overwrite")
		if q := r.api.LastPick().Options().Placeholder; q != "Do you want to overwrite 'compose.yaml'?" {
			t.Fatalf("the second question %q", q)
		}
		if !strings.Contains(r.text("Dockerfile"), "FROM alpine") || r.text("compose.yaml") != "services: {}\n" {
			t.Error("the files after the first answer")
		}
		pickLabel(t, r, "Cancel")
	})
	t.Run("Overwrite All does not ask again", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		mustWrite(t, filepath.Join(r.api.Scope().Folder, "Dockerfile"), "FROM mine\n")
		mustWrite(t, filepath.Join(r.api.Scope().Folder, "compose.yaml"), "services: {}\n")
		run(t, r)
		n := len(r.api.Picks())
		pickLabel(t, r, "Overwrite All")
		if len(r.api.Picks()) != n || !strings.Contains(r.text("compose.yaml"), "image: ") {
			t.Error("Overwrite All asked again or did not write")
		}
	})
	t.Run("a file with the text it would get is not asked about", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		run(t, r)
		first := r.text("Dockerfile")
		if first == "" {
			t.Fatal("nothing written")
		}
		n := len(r.api.Picks())
		run(t, r)
		if len(r.api.Picks()) != n+2 { // the platform and the compose question, and none about a file
			t.Errorf("%d quick picks for the second run", len(r.api.Picks())-n)
		}
	})
	t.Run("the packages of the user are kept", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		mustWrite(t, filepath.Join(r.api.Scope().Folder, "requirements.txt"), "mine==1\n")
		r.file("app.py", "")
		r.run("configure", nil)
		pickLabel(t, r, "Python: General")
		pickLabel(t, r, "app.py")
		pickLabel(t, r, "No")
		if r.text("requirements.txt") != "mine==1\n" || !strings.Contains(r.text("Dockerfile"), `CMD ["python", "app.py"]`) {
			t.Errorf("requirements %q", r.text("requirements.txt"))
		}
		if len(r.api.Picks()) != 3 {
			t.Error("the user was asked about requirements.txt")
		}
	})
}

func TestAddDockerFilesWritesNothingOutsideTheFolder(t *testing.T) {
	r := newRig(t, sampleEngine)
	outside := filepath.Join(t.TempDir(), "secret")
	mustWrite(t, outside, "do not touch")
	if err := os.Symlink(outside, filepath.Join(r.api.Scope().Folder, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	r.run("configure", nil)
	pickLabel(t, r, "Other")
	pickLabel(t, r, "No")
	data, _ := os.ReadFile(outside)
	if string(data) != "do not touch" {
		t.Errorf("the file outside was written: %q", data)
	}
	if !r.hasNote("Cannot write Dockerfile:") {
		t.Errorf("notes %v", r.notes())
	}
	err := writeUnder(r.api.Scope().Folder, "Dockerfile", []byte("x"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("writeUnder.ContainerUIScaffoldWriteFailed") {
		t.Errorf("route %v", err)
	}
	if err := writeUnder("/nonexistent/folder", "x", nil); err == nil {
		t.Error("a folder that is not there is no error")
	}
}

func TestAddDockerFilesAsksForTheFileThatStartsTheApp(t *testing.T) {
	t.Run("Node.js with two package.json", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		r.file("api/package.json", `{"name":"api","scripts":{"start":"node ."}}`)
		r.file("web/package.json", `{"name":"web","main":"index.js"}`)
		r.file("node_modules/dep/package.json", `{}`)
		r.run("configure", nil)
		pickLabel(t, r, "Node.js")
		if got := r.api.LastPick().Labels(); !reflect.DeepEqual(got, []string{"api/package.json", "web/package.json"}) {
			t.Fatalf("package.json files %q", got)
		}
		pickLabel(t, r, "web/package.json")
		r.api.LastInput().Accept("")
		pickLabel(t, r, "No")
		if df := r.text("web/Dockerfile"); !strings.Contains(df, `CMD ["node", "index.js"]`) || strings.Contains(df, "EXPOSE") {
			t.Errorf("web/Dockerfile:\n%s", df)
		}
	})
	t.Run("one project file is used without asking", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		r.file("App.csproj", `<Project><PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup></Project>`)
		r.run("configure", nil)
		pickLabel(t, r, ".NET: Console")
		pickLabel(t, r, "No") // straight to the compose question: no file question, no ports for a console app
		if df := r.text("Dockerfile"); !strings.Contains(df, "runtime:9.0") || !strings.Contains(df, `"App.dll"`) {
			t.Errorf("Dockerfile:\n%s", df)
		}
	})
	t.Run("no package.json", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		r.run("configure", nil)
		pickLabel(t, r, "Node.js")
		if !r.hasNote("No package.json was found in the workspace.") {
			t.Errorf("notes %v", r.notes())
		}
	})
	t.Run("Python: the entry points, manage.py first, or a module", func(t *testing.T) {
		r := newRig(t, sampleEngine)
		r.file("zed.py", "")
		r.file("app.py", "")
		r.file("web/manage.py", "")
		r.file("web/mysite/wsgi.py", "")
		r.file(".venv/lib/x.py", "")
		r.run("configure", nil)
		pickLabel(t, r, "Python: Django")
		labels := r.api.LastPick().Labels()
		if labels[0] != "web/manage.py" || labels[1] != "app.py" || labels[len(labels)-1] != "Enter a Python module instead..." {
			t.Fatalf("entry points %q", labels)
		}
		for _, l := range labels {
			if strings.Contains(l, "wsgi.py") || strings.Contains(l, ".venv") {
				t.Errorf("%q is offered", l)
			}
		}
		pickLabel(t, r, "Enter a Python module instead...")
		in := r.api.LastInput()
		if in.Validate("my app") == "" || in.Validate("a..b") == "" || in.Validate("pkg.manage") != "" {
			t.Error("the module name is not checked")
		}
		in.Accept("pkg.manage")
		r.api.LastInput().Accept("8001")
		pickLabel(t, r, "No")
		if df := r.text("Dockerfile"); !strings.Contains(df, "EXPOSE 8001") || !strings.Contains(df, `"0.0.0.0:8001"`) {
			t.Errorf("Dockerfile:\n%s", df)
		}
	})
}

func TestAddDockerFilesNeedsAFolderAndAPodmanProjectGetsAContainerfile(t *testing.T) {
	r := newRig(t, sampleEngine)
	r.ext.api = noFolderAPI{r.api}
	r.run("configure", nil)
	if !r.hasNote("To add Docker files you must first open a folder.") {
		t.Errorf("notes %v", r.notes())
	}
	if r.cfg.scaffoldFileName() != "Dockerfile" {
		t.Error("Docker's file is not a Dockerfile")
	}
	podman := Config{Kind: "podman"}
	if podman.scaffoldFileName() != "Containerfile" || podman.cliName() != "podman" || podman.composeFallbackName() != "podman-compose" {
		t.Errorf("Podman's names: %s %s %s", podman.scaffoldFileName(), podman.cliName(), podman.composeFallbackName())
	}
}
