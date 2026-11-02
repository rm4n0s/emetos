package javascript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The rig of the tests of the JavaScript pack's debugger: the pack activated over the fake API in a tab with a folder, on a machine
// whose home folder, XDG folders (the data folder js-debug is downloaded into among them), PATH and processes are the test's own.

// cfg is a configuration.
type cfg = extension.DebugConfiguration

// rig is the pack activated in a tab with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *javascript
	base   string
	folder string
	bin    string
	home   string
	// data is the folder of the data home ($XDG_DATA_HOME), data/emetos/js-debug the folder of the downloads.
	data string
	proc string
}

// rigOptions say what the machine of a test has.
type rigOptions struct {
	// node is the output of the fake node's --version ("v22.1.0"); empty has the default and noNode has no node at all.
	node   string
	noNode bool
	// files are written to the folder of the tab (name to text).
	files map[string]string
	// settings are set before the pack is activated.
	settings map[string]any
	// noFolder makes the tab a tab without a folder.
	noFolder bool
	// os is the text of /etc/os-release.
	os string
	// editor is the editor in front: its path below the folder and its language.
	editor, language string
	// noDebug gives the host no debug service.
	noDebug bool
	// before changes the extension before it is activated.
	before func(*javascript)
}

// newRig activates the pack.
func newRig(t *testing.T, o rigOptions) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, base: base, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin"), home: filepath.Join(base, "home"),
		data: filepath.Join(base, "data"), proc: filepath.Join(base, "proc")}
	for _, d := range []string{r.folder, r.bin, r.home, r.data, r.proc, filepath.Join(base, "config"), filepath.Join(base, "state"), filepath.Join(base, "cache")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range o.files {
		writeFile(t, r.folder, name, text, 0o644)
	}
	if !o.noNode {
		version := o.node
		if version == "" {
			version = "v22.1.0"
		}
		writeFile(t, r.bin, "node", "#!/bin/sh\necho '"+version+"'\n", 0o755)
	}
	t.Setenv("PATH", r.bin)
	t.Setenv("HOME", r.home)
	t.Setenv("XDG_DATA_HOME", r.data)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	ext := New().(*javascript)
	ext.procRoot = r.proc
	ext.osRelease = func() string { return o.os }
	if o.before != nil {
		o.before(ext)
	}
	if err := extension.CheckManifest(ext.Manifest()); err != nil {
		t.Fatal(err)
	}
	info := extension.ScopeInfo{Name: "tab", Folder: r.folder}
	if o.noFolder {
		info.Folder = ""
	}
	r.ext = ext
	// typescript.nodePath is the setting of another extension, which the fake API only knows when its manifest lists it
	m := ext.Manifest()
	m.Contributes.Settings = append(m.Contributes.Settings, extension.SettingSpec{Key: NodePathSetting, Type: "string", Default: ""})
	r.api = fakeapi.New(t, m, info)
	for k, v := range o.settings {
		r.api.SetSetting(k, v)
	}
	r.api.SetDebugUnavailable(o.noDebug)
	if err := ext.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	if o.editor != "" {
		r.setEditor(o.editor, o.language)
	}
	return r
}

// setEditor tells the pack which editor is in front.
func (r *rig) setEditor(rel, language string) {
	r.t.Helper()
	uri := "file://" + filepath.ToSlash(filepath.Join(r.folder, rel))
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: language})
}

// dataDir is the folder the downloads go to.
func (r *rig) dataDir() string { return filepath.Join(r.data, "emetos", "js-debug") }

// writeFile writes a file below dir and returns its path.
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

// fakeJsDebug makes a js-debug that is only a file in the place it is looked for, below dir, and returns the path of the program.
func fakeJsDebug(t *testing.T, dir string) string {
	t.Helper()
	return writeFile(t, dir, "js-debug/src/dapDebugServer.js", "// a js-debug\n", 0o644)
}

// noteList lists the notifications with their severity.
func (r *rig) noteList() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

// noteContaining reports whether a notification has the text.
func (r *rig) noteContaining(text string) bool {
	for _, n := range r.noteList() {
		if strings.Contains(n, text) {
			return true
		}
	}
	return false
}
