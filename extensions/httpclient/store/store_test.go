package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

var bg = context.Background()

func TestOpenAnEmptyWorkspaceWatchesTheFolderAndMovesTheWatchDeeper(t *testing.T) {
	s, h, folder := open(t)
	if got := s.Snapshot(); len(got.Collections) != 0 || len(got.Environments) != 0 {
		t.Fatalf("the tree of an empty workspace = %+v", got)
	}
	spec := h.nextWatch()
	if spec.Path != folder || spec.Recursive {
		t.Errorf("first watch = %+v, want the folder, not recursive", spec)
	}
	// .emetos appears (a git pull): the watch moves to it, then to the client's folder, which is watched whole
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.fire(filepath.Join(folder, ".emetos"))
	if spec := h.nextWatch(); spec.Path != filepath.Join(folder, ".emetos") || spec.Recursive {
		t.Errorf("second watch = %+v", spec)
	}
	ch := changes(s)
	write(t, filepath.Join(folder, ".emetos/http/collections/api/collection.jsonc"), `{"emetosHttp": 1, "name": "API"}`)
	h.fire(filepath.Join(folder, ".emetos/http"))
	if spec := h.nextWatch(); spec.Path != filepath.Join(folder, ".emetos/http") || !spec.Recursive {
		t.Errorf("third watch = %+v", spec)
	}
	waitChange(t, ch)
	if c := s.Snapshot().Collections; len(c) != 1 || c[0].Name != "API" {
		t.Errorf("collections = %+v", c)
	}
	if n := len(h.active()); n != 1 {
		t.Errorf("%d watches are active, want 1", n)
	}
	if s.WatchError() != nil {
		t.Errorf("WatchError = %v", s.WatchError())
	}
	// changes of other files do not read anything
	before := s.Snapshot()
	h.fire(filepath.Join(folder, "main.go"))
	if s.Snapshot() != before {
		t.Error("a change of an unrelated file made a new tree")
	}
}

func TestAWatchThatCannotBePlacedIsReported(t *testing.T) {
	folder := t.TempDir()
	h := newFakeHost(t)
	h.failErr = errors.New("WatcherAddFailed", "no inotify")
	s, err := Open(bg, folder, h, Options{})
	if err != nil {
		t.Fatal(err)
	}
	h.nextWatch()
	for i := 0; i < 500 && s.WatchError() == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	s.Close()
	if s.WatchError() == nil {
		t.Error("WatchError = nil")
	}
	s.Close() // twice is fine
}

func TestExternalChangesAreSeen(t *testing.T) {
	s, h, folder := open(t)
	h.nextWatch()
	root := filepath.Join(folder, ".emetos/http")
	ch := changes(s)
	req := filepath.Join(root, "collections/api/get.request.jsonc")
	write(t, filepath.Join(root, "collections/api/collection.jsonc"), `{"emetosHttp": 1, "name": "API"}`)
	write(t, req, `{"emetosHttp": 1, "name": "Get", "method": "GET", "url": "u"}`)
	write(t, filepath.Join(root, "environments/dev.jsonc"), `{"emetosHttp": 1, "name": "Dev"}`)
	h.fire(root)
	h.nextWatch() // the watch is moved to the new folder
	waitChange(t, ch)
	tree := s.Snapshot()
	if got := tree.Find(req); got == nil || got.Request == nil || got.Request.Method != "GET" {
		t.Fatalf("request not in the tree: %+v", got)
	}
	if len(tree.Environments) != 1 || tree.Environments[0].Name != "Dev" {
		t.Errorf("environments = %+v", tree.Environments)
	}

	// an edit by hand
	write(t, req, `{"emetosHttp": 1, "name": "Get", "method": "PUT", "url": "u"}`)
	h.fire(req)
	waitChange(t, ch)
	if got := s.Snapshot().Find(req).Request.Method; got != "PUT" {
		t.Errorf("method = %s after the edit", got)
	}
	// the same text again is no change
	write(t, req, `{"emetosHttp": 1, "name": "Get", "method": "PUT", "url": "u"}`)
	h.fire(req)
	h.fire(req)
	select {
	case <-ch:
		t.Error("a listener was called for a file that did not change")
	case <-time.After(200 * time.Millisecond):
	}

	// a file that is broken, then fixed, then of a newer version
	write(t, req, `{"emetosHttp": 1, "name": "Get" "method": "PUT"}`)
	h.fire(req)
	waitChange(t, ch)
	n := s.Snapshot().Find(req)
	wantRoute(t, n.Err, "HttpFileSyntax")
	if n.Request != nil || n.ReadOnly {
		t.Errorf("a broken file has a document: %+v", n)
	}
	if appErr, _ := errors.FromError(n.Err); appErr.Metadata["line"] != 1 {
		t.Errorf("the error has no position: %v", appErr.Metadata)
	}
	write(t, req, `{"emetosHttp": 3, "name": "From the future", "method": "PUT", "url": "u"}`)
	h.fire(req)
	waitChange(t, ch)
	n = s.Snapshot().Find(req)
	if !n.ReadOnly || n.Request == nil || n.Name != "From the future" || !format.IsNewer(n.Err) {
		t.Errorf("a newer file = %+v", n)
	}

	// a delete on disk
	if err := os.Remove(req); err != nil {
		t.Fatal(err)
	}
	h.fire(req)
	waitChange(t, ch)
	if s.Snapshot().Find(req) != nil {
		t.Error("a deleted request is still in the tree")
	}
}

func TestTheTreeShowsAnythingOnDisk(t *testing.T) {
	folder := t.TempDir()
	root := filepath.Join(folder, ".emetos/http")
	coll := filepath.Join(root, "collections/shop")
	write(t, filepath.Join(coll, "collection.jsonc"), `// the shop
{"emetosHttp": 1, "name": "Shop", "order": ["z.request.jsonc", "sub", "gone.request.jsonc", "z.request.jsonc"]}`)
	write(t, filepath.Join(coll, "z.request.jsonc"), `{"emetosHttp": 1, "name": "Z", "method": "GET", "url": "u"}`)
	write(t, filepath.Join(coll, "a.request.jsonc"), `{"emetosHttp": 1, "name": "A", "method": "GET", "url": "u"}`)
	write(t, filepath.Join(coll, "b.request.jsonc"), `{"emetosHttp": 1, "name": "B", "method": "GET", "url": "u"}`)
	write(t, filepath.Join(coll, "b.body.json"), `{}`)
	write(t, filepath.Join(coll, ".hidden.request.jsonc"), `{}`)
	write(t, filepath.Join(coll, "notes.txt"), `not a request`)
	write(t, filepath.Join(coll, "sub/folder.jsonc"), `{"emetosHttp": 1, "name": "Sub", "variables": [{"name": "v", "value": "1"}]}`)
	write(t, filepath.Join(coll, "sub/deep/x.request.jsonc"), `{"emetosHttp": 1, "name": "X", "method": "GET", "url": "u"}`)
	write(t, filepath.Join(coll, "bad.request.jsonc"), `[`)
	write(t, filepath.Join(coll, "out.request.jsonc"), `{"emetosHttp": 1, "name": "Out", "method": "POST", "url": "u", "body": {"type": "binary", "path": "../../../../../etc/hostname"}}`)
	write(t, filepath.Join(root, "collections/nofile/r.request.jsonc"), `{"emetosHttp": 1, "name": "R", "method": "GET", "url": "u"}`)
	write(t, filepath.Join(root, "environments/prod.jsonc"), `{"emetosHttp": 1, "name": "Production", "variables": [{"name": "a", "value": "1"}]}`)
	write(t, filepath.Join(root, "environments/dev.jsonc"), `{"emetosHttp": 1}`)
	write(t, filepath.Join(root, "environments/.draft.jsonc"), `{}`)
	write(t, filepath.Join(root, "environments/readme.md"), `x`)

	s, err := Open(bg, folder, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tree := s.Snapshot()
	if len(tree.Collections) != 2 || tree.Collections[0].Key != "nofile" || tree.Collections[1].Key != "shop" {
		t.Fatalf("collections = %v", tree.Collections)
	}
	nofile := tree.Collections[0]
	if nofile.HasFile || nofile.Name != "nofile" || nofile.Group == nil || len(nofile.Children) != 1 {
		t.Errorf("a collection without its file = %+v", nofile)
	}
	shop := tree.Collections[1]
	if shop.Name != "Shop" || shop.Kind != KindCollection {
		t.Errorf("shop = %+v", shop)
	}
	// the order lists z, sub (and a name that is not there, and z twice), the rest is by name
	want := []string{"z.request.jsonc", "sub", "a.request.jsonc", "b.request.jsonc", "bad.request.jsonc", "out.request.jsonc"}
	if got := keys(shop); !reflect.DeepEqual(got, want) {
		t.Errorf("shop entries = %v, want %v", got, want)
	}
	sub := tree.Find(filepath.Join(coll, "sub"))
	if sub.Kind != KindFolder || sub.Name != "Sub" || len(sub.Group.Variables) != 1 {
		t.Errorf("sub = %+v", sub)
	}
	if deep := tree.Find(filepath.Join(coll, "sub/deep")); deep == nil || deep.HasFile || len(deep.Children) != 1 {
		t.Errorf("deep = %+v", deep)
	}
	wantRoute(t, tree.Find(filepath.Join(coll, "bad.request.jsonc")).Err, "HttpFileSyntax")
	wantRoute(t, tree.Find(filepath.Join(coll, "out.request.jsonc")).Err, "HttpPathOutsideWorkspace")
	if got := tree.Find(filepath.Join(coll, "b.request.jsonc")); got.Err != nil || got.Name != "B" {
		t.Errorf("b = %+v", got)
	}
	envs := tree.Environments
	if len(envs) != 2 || envs[0].Key != "dev" || envs[0].Name != "dev" || envs[1].Name != "Production" {
		t.Errorf("environments = %+v", envs)
	}
	if tree.Parent(tree.Find(filepath.Join(coll, "sub/deep"))) != sub || tree.Parent(shop) != nil {
		t.Error("Parent is wrong")
	}
	var walked int
	tree.Walk(func(*Node) bool { walked++; return true })
	if walked != 13 {
		t.Errorf("Walk visited %d nodes", walked)
	}
	var first int
	tree.Walk(func(*Node) bool { first++; return false })
	if first != 1 {
		t.Errorf("Walk did not stop: %d", first)
	}
	var nilTree *Tree
	if nilTree.Find("x") != nil {
		t.Error("Find on a nil tree")
	}
}

func TestOpenFailsWhenAFolderCannotBeListed(t *testing.T) {
	folder := t.TempDir()
	write(t, filepath.Join(folder, ".emetos/http/collections"), "a file where a folder should be")
	_, err := Open(bg, folder, nil, Options{})
	wantRoute(t, err, "scan")
	wantRoute(t, err, "HttpDirReadFailed")
	folder = t.TempDir()
	write(t, filepath.Join(folder, ".emetos/http/environments"), "a file")
	_, err = Open(bg, folder, nil, Options{})
	wantRoute(t, err, "HttpDirReadFailed")
	// a cancelled context stops the scan of files
	folder = t.TempDir()
	write(t, filepath.Join(folder, ".emetos/http/collections/a/collection.jsonc"), `{"emetosHttp": 1}`)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, err = Open(ctx, folder, nil, Options{})
	wantRoute(t, err, "HttpCancelled")
}

func TestSortKeys(t *testing.T) {
	tests := []struct {
		name        string
		keys, order []string
		want        []string
	}{
		{"no order is by name", []string{"b", "c", "a"}, nil, []string{"a", "b", "c"}},
		{"order first", []string{"a", "b", "c"}, []string{"c", "a"}, []string{"c", "a", "b"}},
		{"unknown and repeated names", []string{"a", "b"}, []string{"x", "b", "b"}, []string{"b", "a"}},
		{"empty", nil, []string{"a"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sortKeys(tt.keys, tt.order); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sortKeys = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRelevantPaths(t *testing.T) {
	s := &Store{folder: "/w", kick: make(chan struct{}, 1)}
	tests := []struct {
		path string
		want bool
	}{
		{"", false}, {".", true}, {".emetos", true}, {".emetos/http/x", true}, {"/w/.emetos/http/x", true},
		{"/w", true}, {"/w/src/main.go", false}, {"src/.emetos", false}, {"/other/.emetos", false},
	}
	for _, tt := range tests {
		if got := s.relevant(tt.path); got != tt.want {
			t.Errorf("relevant(%q) = %v", tt.path, got)
		}
	}
	// a rescan always counts
	s.onChanges([]extension.FileChange{{Kind: extension.FileRescan, Path: "src"}})
	if len(s.kick) != 1 {
		t.Error("a rescan did not poke the loop")
	}
}
