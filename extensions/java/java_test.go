package java

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The Java pack over the fake API with a fake jdtls folder (the files jdtls has that the pack looks at) and a fake java (a script that
// prints what java -version prints): folders the tests make. Nothing here reads the home folder, the PATH or the environment of the
// machine: every test sets its own (t.Setenv).

// rig is the pack activated in a tab with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *java
	base   string
	folder string
	bin    string
	home   string
	cache  string
	// jdtls is the fake jdtls folder.
	jdtls string
}

// rigOptions say what the machine of a test has.
type rigOptions struct {
	// javaVersion is what the fake java prints in the quotes ("25.0.4.1"); "" for no java at all.
	javaVersion string
	// noJDTLS leaves the path setting empty and the folder out of the usual places.
	noJDTLS bool
	// settings are set before the pack is activated.
	settings map[string]any
	// files are written to the folder of the tab (name to text).
	files map[string]string
	// noFolder makes the tab a tab without a folder.
	noFolder bool
}

// newRig activates the pack.
func newRig(t *testing.T, o rigOptions) *rig { t.Helper(); return newRigWith(t, o, nil) }

// newRigWith activates the pack; before makes the fake API ready (its logger) before the activation.
func newRigWith(t *testing.T, o rigOptions, before func(*fakeapi.API)) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, base: base, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin"), home: filepath.Join(base, "home"),
		cache: filepath.Join(base, "cache"), jdtls: filepath.Join(base, "jdtls")}
	for _, d := range []string{r.folder, r.bin, r.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range o.files {
		writeFile(t, r.folder, name, text, 0o644)
	}
	fakeJDTLS(t, r.jdtls, "1.63.0.202610191322")
	if o.javaVersion != "" {
		fakeJava(t, r.bin, o.javaVersion)
	}
	t.Setenv("PATH", r.bin)
	t.Setenv("HOME", r.home)
	t.Setenv("XDG_CACHE_HOME", r.cache)
	t.Setenv("JAVA_HOME", "")
	ext := New()
	if err := extension.CheckManifest(ext.Manifest()); err != nil {
		t.Fatal(err)
	}
	info := extension.ScopeInfo{Name: "tab", Folder: r.folder}
	if o.noFolder {
		info.Folder = ""
	}
	r.api = fakeapi.New(t, ext.Manifest(), info)
	if before != nil {
		before(r.api)
	}
	if !o.noJDTLS {
		r.api.SetSetting(PathSetting, r.jdtls)
	}
	for k, v := range o.settings {
		r.api.SetSetting(k, v)
	}
	if err := ext.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = ext.(*java)
	t.Cleanup(r.settle)
	r.settle()
	return r
}

// fakeJDTLS makes the files of a jdtls folder that the pack looks at: the launcher jar, the core jar (its name has the version), and the
// configuration of the system.
func fakeJDTLS(t *testing.T, dir, version string) {
	t.Helper()
	writeFile(t, dir, "plugins/org.eclipse.equinox.launcher_1.8.0.v20260804-1928.jar", "jar", 0o644)
	writeFile(t, dir, "plugins/org.eclipse.equinox.launcher.gtk.linux.x86_64_1.2.1700.v20260913-1737.jar", "jar", 0o644)
	writeFile(t, dir, "plugins/org.eclipse.jdt.ls.core_"+version+".jar", "jar", 0o644)
	writeFile(t, dir, configFolder()+"/config.ini", "osgi.bundles=reference\\:file\\:plugins/a.jar@4\n", 0o644)
	writeFile(t, dir, "bin/jdtls", "#!/bin/sh\n", 0o755)
}

// fakeJava makes a java in the folder that prints a version as java -version does (on the standard error).
func fakeJava(t *testing.T, dir, version string) string {
	t.Helper()
	return writeFile(t, dir, "java", "#!/bin/sh\necho 'openjdk version \""+version+"\" 2026-08-18' >&2\necho 'OpenJDK Runtime Environment' >&2\n", 0o755)
}

// writeFile writes a file (and its folders) under dir and returns its path.
func writeFile(t *testing.T, dir, rel, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// settle pumps the tab.
func (r *rig) settle() {
	r.t.Helper()
	for i := 0; i < 5; i++ {
		r.api.Pump()
	}
	r.ext.wait()
	r.api.Pump()
}

// run starts the server as the host does (the launcher, then the state running) and waits for the pack to see it.
func (r *rig) run() (extension.ServerLaunch, error) {
	r.t.Helper()
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStarting})
	l, err := r.api.Launch(context.Background(), ServerID)
	if err != nil {
		r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
	} else {
		r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning, Starts: 1})
	}
	r.settle()
	return l, err
}

// editor puts a file of the language in front, with the selections (a cursor at the start when none is given).
func (r *rig) editor(name, language, text string, sels ...extension.Selection) string {
	r.t.Helper()
	if len(sels) == 0 {
		sels = []extension.Selection{{}}
	}
	uri := "file://" + filepath.Join(r.folder, name)
	r.api.SetDocument(uri, text)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: language, Selections: sels})
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: language})
	r.settle()
	return uri
}

// item is the status item of the server.
func (r *rig) item() *fakeapi.StatusItem { return r.api.StatusItem(langserver.StatusID(ServerID)) }

// notes are the messages of the notifications so far.
func (r *rig) notes() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Message)
	}
	return out
}

// note is the message of the last notification, "" when there is none.
func (r *rig) note() string {
	if n := r.notes(); len(n) > 0 {
		return n[len(n)-1]
	}
	return ""
}

// requests are the params of the requests of a method the pack sent.
func (r *rig) requests(method string) []string {
	var out []string
	for _, req := range r.api.ServerRequests(ServerID) {
		if req.Method == method {
			out = append(out, string(req.Params))
		}
	}
	return out
}

// answer sets the answers of the server to the requests of the pack, by method (a JSON text); a method with no answer is answered with null.
func (r *rig) answer(answers map[string]string) {
	r.api.OnServerRequest(ServerID, func(req fakeapi.ServerRequest) (json.RawMessage, error) {
		if a, ok := answers[req.Method]; ok {
			return json.RawMessage(a), nil
		}
		return json.RawMessage("null"), nil
	})
}

// wantRoute checks the route and the tag of a failure.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// logged is a writer whose lines a test can read: the log of an activation.
type logged struct {
	mu    sync.Mutex
	lines []string
}

func (l *logged) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.lines = append(l.lines, string(p))
	l.mu.Unlock()
	return len(p), nil
}

func (l *logged) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "")
}

// launchOptions is the initialization options of the launch as a map.
func launchOptions(t *testing.T, l extension.ServerLaunch) map[string]any {
	t.Helper()
	raw, err := json.Marshal(l.InitializationOptions)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
