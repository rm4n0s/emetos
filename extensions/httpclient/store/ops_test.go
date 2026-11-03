package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

func exist(path string) bool { _, err := os.Lstat(path); return err == nil }

func TestCreateTheWholeTree(t *testing.T) {
	s, _, folder := open(t)
	root := filepath.Join(folder, ".emetos/http")
	coll, err := s.CreateCollection(bg, "My API")
	if err != nil {
		t.Fatal(err)
	}
	if coll != filepath.Join(root, "collections/my-api") {
		t.Fatalf("collection path = %s", coll)
	}
	if got := read(t, filepath.Join(coll, "collection.jsonc")); got != "{\n  \"emetosHttp\": 1,\n  \"name\": \"My API\"\n}\n" {
		t.Errorf("collection.jsonc = %q", got)
	}
	users, err := s.CreateFolder(bg, coll, "Users & Roles")
	if err != nil {
		t.Fatal(err)
	}
	if users != filepath.Join(coll, "users-roles") {
		t.Errorf("folder path = %s", users)
	}
	req, err := s.CreateRequest(bg, users, RequestFiles{
		Request: format.Request{Name: "Create user", URL: "{{base}}/users", Method: "POST", Body: &format.Body{Type: format.BodyJSON}},
		Body:    "{\"name\": \"Ada\"}\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if req != filepath.Join(users, "create-user.request.jsonc") {
		t.Errorf("request path = %s", req)
	}
	if got := read(t, filepath.Join(users, "create-user.body.json")); got != "{\"name\": \"Ada\"}\n" {
		t.Errorf("body file = %q", got)
	}
	files, err := s.ReadRequest(bg, req)
	if err != nil {
		t.Fatal(err)
	}
	if files.Request.Body.File != "create-user.body.json" || files.Body != "{\"name\": \"Ada\"}\n" || files.Request.Method != "POST" {
		t.Errorf("ReadRequest = %+v", files)
	}
	// a second request with the same name, one with no name and no method, a graphql one with variables
	req2, err := s.CreateRequest(bg, users, RequestFiles{Request: format.Request{Name: "Create user", URL: "u"}})
	if err != nil || filepath.Base(req2) != "create-user-2.request.jsonc" {
		t.Errorf("clash: %s, %v", req2, err)
	}
	req3, err := s.CreateRequest(bg, coll, RequestFiles{})
	if err != nil || filepath.Base(req3) != "new-request.request.jsonc" {
		t.Errorf("default: %s, %v", req3, err)
	}
	if f, _ := s.ReadRequest(bg, req3); f.Request.Method != "GET" || f.Request.Name != DefaultRequestName {
		t.Errorf("defaults = %+v", f.Request)
	}
	gq, err := s.CreateRequest(bg, coll, RequestFiles{
		Request: format.Request{Name: "Q", Method: "POST", URL: "u", Body: &format.Body{Type: format.BodyGraphQL}},
		Body:    "{ me { id } }", Variables: "{\"a\": 1}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(coll, "q.variables.json")) != "{\"a\": 1}" || read(t, filepath.Join(coll, "q.body.graphql")) != "{ me { id } }" {
		t.Error("the graphql files are not written")
	}
	// a form body has no file; a body without text has none either
	if f, err := s.CreateRequest(bg, coll, RequestFiles{Request: format.Request{Name: "Form", Body: &format.Body{Type: format.BodyForm, File: "x.txt", Fields: []format.Pair{{Name: "a", Value: "1", Enabled: true}}}}}); err != nil {
		t.Fatal(err)
	} else if g, _ := s.ReadRequest(bg, f); g.Request.Body.File != "" {
		t.Errorf("form body = %+v", g.Request.Body)
	}
	if f, err := s.CreateRequest(bg, coll, RequestFiles{Request: format.Request{Name: "Empty", Body: &format.Body{Type: format.BodyJSON}}}); err != nil {
		t.Fatal(err)
	} else if g, _ := s.ReadRequest(bg, f); g.Request.Body.File != "" || exist(filepath.Join(coll, "empty.body.json")) {
		t.Errorf("empty body = %+v", g.Request.Body)
	}
	// the orders list the entries as made
	tree := s.Snapshot()
	if got := keys(tree.Find(coll)); !reflect.DeepEqual(got, []string{"users-roles", "new-request.request.jsonc", "q.request.jsonc", "form.request.jsonc", "empty.request.jsonc"}) {
		t.Errorf("collection order = %v", got)
	}
	if got := keys(tree.Find(users)); !reflect.DeepEqual(got, []string{"create-user.request.jsonc", "create-user-2.request.jsonc"}) {
		t.Errorf("folder order = %v", got)
	}
	_ = gq
	// a second collection and an environment
	if c2, err := s.CreateCollection(bg, "my api"); err != nil || filepath.Base(c2) != "my-api-2" {
		t.Errorf("second collection: %s, %v", c2, err)
	}
	env, err := s.CreateEnvironment(bg, "Dev / Local")
	if err != nil || env != filepath.Join(root, "environments/dev-local.jsonc") {
		t.Fatalf("environment: %s, %v", env, err)
	}
	if e2, err := s.CreateEnvironment(bg, "dev local"); err != nil || filepath.Base(e2) != "dev-local-2.jsonc" {
		t.Errorf("second environment: %s, %v", e2, err)
	}
	if err := s.SaveEnvironment(bg, env, format.Environment{Name: "Dev / Local", Variables: []format.Variable{{Name: "base", Value: "http://localhost", Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	if e := s.Snapshot().Find(env); e.Environment == nil || len(e.Environment.Variables) != 1 {
		t.Errorf("environment = %+v", e)
	}
}

func TestNamesMadeSafeAndClashes(t *testing.T) {
	s, _, _ := open(t)
	coll, err := s.CreateCollection(bg, "C")
	if err != nil {
		t.Fatal(err)
	}
	// a folder and a request with the same safe name do not clash on disk (a request has a suffix) but take distinct stems
	f1, _ := s.CreateFolder(bg, coll, "Get")
	r1, err := s.CreateRequest(bg, coll, RequestFiles{Request: format.Request{Name: "Get"}})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(f1) != "get" || filepath.Base(r1) != "get-2.request.jsonc" {
		t.Errorf("folder %s, request %s", f1, r1)
	}
	// the body files of a request count as taken
	write(t, filepath.Join(coll, "x.body.json"), "{}")
	r2, err := s.CreateRequest(bg, coll, RequestFiles{Request: format.Request{Name: "x"}})
	if err != nil || filepath.Base(r2) != "x-2.request.jsonc" {
		t.Errorf("request next to a body file = %s, %v", r2, err)
	}
	for _, name := range []string{"../../etc/passwd", "..", "a/b/c", "  spaced  out  ", "UPPER"} {
		p, err := s.CreateRequest(bg, coll, RequestFiles{Request: format.Request{Name: name}})
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if filepath.Dir(p) != coll {
			t.Errorf("%q made %s outside the collection", name, p)
		}
	}
}

func TestOperationsRefuseWhatTheyCannotDo(t *testing.T) {
	s, _, folder := open(t)
	coll, _ := s.CreateCollection(bg, "C")
	req, _ := s.CreateRequest(bg, coll, RequestFiles{Request: format.Request{Name: "R"}})
	env, _ := s.CreateEnvironment(bg, "E")
	sub, _ := s.CreateFolder(bg, coll, "Sub")
	missing := filepath.Join(folder, "nothing")
	tests := []struct {
		name  string
		do    func() error
		route string
	}{
		{"collection with no name", func() error { _, err := s.CreateCollection(bg, "  "); return err }, "Store.CreateCollection.HttpNameEmpty"},
		{"folder with no name", func() error { _, err := s.CreateFolder(bg, coll, ""); return err }, "Store.CreateFolder.HttpNameEmpty"},
		{"environment with no name", func() error { _, err := s.CreateEnvironment(bg, ""); return err }, "Store.CreateEnvironment.HttpNameEmpty"},
		{"rename with no name", func() error { _, err := s.Rename(bg, req, " "); return err }, "Store.Rename.HttpNameEmpty"},
		{"folder in a request", func() error { _, err := s.CreateFolder(bg, req, "x"); return err }, "Store.CreateFolder->Store.group.HttpNodeKindInvalid"},
		{"request in a missing folder", func() error { _, err := s.CreateRequest(bg, missing, RequestFiles{}); return err }, "Store.CreateRequest->Store.group->Store.find.HttpNodeNotFound"},
		{"rename of a missing node", func() error { _, err := s.Rename(bg, missing, "x"); return err }, "Store.Rename->Store.find.HttpNodeNotFound"},
		{"move of a missing node", func() error { _, err := s.Move(bg, missing, coll, 0); return err }, "Store.Move->Store.find.HttpNodeNotFound"},
		{"duplicate of a missing node", func() error { _, err := s.Duplicate(bg, missing); return err }, "Store.Duplicate->Store.find.HttpNodeNotFound"},
		{"delete of a missing node", func() error { _, err := s.Delete(bg, missing); return err }, "Store.Delete->Store.find.HttpNodeNotFound"},
		{"move a collection", func() error { _, err := s.Move(bg, coll, sub, 0); return err }, "Store.Move.HttpNodeKindInvalid"},
		{"move an environment", func() error { _, err := s.Move(bg, env, coll, 0); return err }, "Store.Move.HttpNodeKindInvalid"},
		{"move into a request", func() error { _, err := s.Move(bg, sub, req, 0); return err }, "Store.Move->Store.group.HttpNodeKindInvalid"},
		{"move a folder into itself", func() error { _, err := s.Move(bg, sub, sub, 0); return err }, "Store.Move.HttpMoveIntoItself"},
		{"save an environment as a request", func() error { return s.SaveEnvironment(bg, req, format.Environment{}) }, "Store.SaveEnvironment.HttpNodeKindInvalid"},
		{"save a group that is a request", func() error { return s.SaveGroup(bg, req, format.Collection{}) }, "Store.SaveGroup->Store.group.HttpNodeKindInvalid"},
		{"save a missing environment", func() error { return s.SaveEnvironment(bg, missing, format.Environment{}) }, "Store.SaveEnvironment->Store.find.HttpNodeNotFound"},
		{"invalid document", func() error {
			return s.SaveRequest(bg, req, RequestFiles{Request: format.Request{Auth: &format.Auth{Type: "x"}}})
		}, "Store.SaveRequest->Update[...].HttpFileValueInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantRoute(t, tt.do(), tt.route)
		})
	}
	// into a subfolder of itself
	deep, _ := s.CreateFolder(bg, sub, "Deep")
	_, err := s.Move(bg, sub, deep, 0)
	wantRoute(t, err, "HttpMoveIntoItself")
}

func TestRenameKeepsCommentsAndUpdatesTheOrder(t *testing.T) {
	s, _, folder := open(t)
	coll := filepath.Join(folder, ".emetos/http/collections/api")
	write(t, filepath.Join(coll, "collection.jsonc"), "// the API\n{\n  \"emetosHttp\": 1,\n  \"name\": \"API\",\n  \"order\": [\"b.request.jsonc\", \"get-user.request.jsonc\"]\n}\n")
	write(t, filepath.Join(coll, "get-user.request.jsonc"), "// a note\n{\n  \"emetosHttp\": 1,\n  \"name\": \"Get user\", // the name\n  \"method\": \"GET\",\n  \"url\": \"u\",\n  \"body\": {\"type\": \"json\", \"file\": \"get-user.body.json\", \"variables\": \"custom-vars.json\"},\n  \"x-owner\": \"me\"\n}\n")
	write(t, filepath.Join(coll, "get-user.body.json"), "{}")
	write(t, filepath.Join(coll, "custom-vars.json"), "{}")
	write(t, filepath.Join(coll, "b.request.jsonc"), `{"emetosHttp": 1, "name": "B", "method": "GET", "url": "u"}`)
	if _, err := s.Refresh(bg); err != nil {
		t.Fatal(err)
	}
	got, err := s.Rename(bg, filepath.Join(coll, "get-user.request.jsonc"), "Fetch the user")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(coll, "fetch-the-user.request.jsonc") {
		t.Fatalf("new path = %s", got)
	}
	text := read(t, got)
	for _, want := range []string{"// a note", "// the name", `"name": "Fetch the user"`, `"file": "fetch-the-user.body.json"`, `"variables": "custom-vars.json"`, `"x-owner": "me"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the renamed file lacks %q:\n%s", want, text)
		}
	}
	if exist(filepath.Join(coll, "get-user.request.jsonc")) || exist(filepath.Join(coll, "get-user.body.json")) || !exist(filepath.Join(coll, "fetch-the-user.body.json")) || !exist(filepath.Join(coll, "custom-vars.json")) {
		t.Error("the files were not renamed as expected")
	}
	if c := read(t, filepath.Join(coll, "collection.jsonc")); !strings.Contains(c, "// the API") || !strings.Contains(c, `"b.request.jsonc"`) || !strings.Contains(c, `"fetch-the-user.request.jsonc"`) || strings.Contains(c, "get-user") {
		t.Errorf("collection.jsonc = %s", c)
	}
	if k := keys(s.Snapshot().Find(coll)); !reflect.DeepEqual(k, []string{"b.request.jsonc", "fetch-the-user.request.jsonc"}) {
		t.Errorf("order = %v", k)
	}
	// renaming to the same safe name changes the name in the file only; a clash gets a number
	same, err := s.Rename(bg, got, "Fetch The User!")
	if err != nil || same != got || !strings.Contains(read(t, got), `"name": "Fetch The User!"`) {
		t.Errorf("same stem: %s, %v", same, err)
	}
	clash, err := s.Rename(bg, got, "b")
	if err != nil || filepath.Base(clash) != "b-2.request.jsonc" {
		t.Errorf("clash: %s, %v", clash, err)
	}
}

func TestRenameFoldersCollectionsAndEnvironments(t *testing.T) {
	s, _, folder := open(t)
	root := filepath.Join(folder, ".emetos/http")
	coll, _ := s.CreateCollection(bg, "Alpha")
	other, _ := s.CreateCollection(bg, "Beta")
	f, _ := s.CreateFolder(bg, coll, "Inner")
	r, _ := s.CreateRequest(bg, f, RequestFiles{Request: format.Request{Name: "R"}})
	env, _ := s.CreateEnvironment(bg, "Dev")
	_, _ = s.CreateEnvironment(bg, "Prod")

	f2, err := s.Rename(bg, f, "Renamed Folder")
	if err != nil || f2 != filepath.Join(coll, "renamed-folder") {
		t.Fatalf("folder: %s, %v", f2, err)
	}
	if !exist(filepath.Join(f2, filepath.Base(r))) || !strings.Contains(read(t, filepath.Join(f2, "folder.jsonc")), `"Renamed Folder"`) {
		t.Error("the folder did not move with its request")
	}
	if k := keys(s.Snapshot().Find(coll)); !reflect.DeepEqual(k, []string{"renamed-folder"}) {
		t.Errorf("order = %v", k)
	}
	c2, err := s.Rename(bg, coll, "Beta")
	if err != nil || filepath.Base(c2) != "beta-2" {
		t.Fatalf("collection: %s, %v", c2, err)
	}
	if s.Snapshot().Find(other) == nil || s.Snapshot().Find(c2).Name != "Beta" {
		t.Error("the collections are wrong after the rename")
	}
	e2, err := s.Rename(bg, env, "Staging")
	if err != nil || e2 != filepath.Join(root, "environments/staging.jsonc") {
		t.Fatalf("environment: %s, %v", e2, err)
	}
	if e := s.Snapshot().Find(e2); e.Name != "Staging" || e.Environment.Name != "Staging" {
		t.Errorf("environment node = %+v", e)
	}
	if e3, err := s.Rename(bg, e2, "Prod"); err != nil || filepath.Base(e3) != "prod-2.jsonc" {
		t.Errorf("environment clash: %s, %v", e3, err)
	}
}

func TestMove(t *testing.T) {
	s, _, _ := open(t)
	c1, _ := s.CreateCollection(bg, "One")
	c2, _ := s.CreateCollection(bg, "Two")
	f, _ := s.CreateFolder(bg, c1, "F")
	g, _ := s.CreateFolder(bg, c1, "G")
	a, _ := s.CreateRequest(bg, c1, RequestFiles{Request: format.Request{Name: "A", Body: &format.Body{Type: format.BodyText}}, Body: "hello"})
	b, _ := s.CreateRequest(bg, c1, RequestFiles{Request: format.Request{Name: "B"}})
	c, _ := s.CreateRequest(bg, c1, RequestFiles{Request: format.Request{Name: "C"}})
	if got := keys(s.Snapshot().Find(c1)); !reflect.DeepEqual(got, []string{"f", "g", "a.request.jsonc", "b.request.jsonc", "c.request.jsonc"}) {
		t.Fatalf("start = %v", got)
	}

	// reorder inside a parent: c to the front, a to the end
	if p, err := s.Move(bg, c, c1, 0); err != nil || p != c {
		t.Fatalf("reorder: %s, %v", p, err)
	}
	if _, err := s.Move(bg, a, c1, -1); err != nil {
		t.Fatal(err)
	}
	if got := keys(s.Snapshot().Find(c1)); !reflect.DeepEqual(got, []string{"c.request.jsonc", "f", "g", "b.request.jsonc", "a.request.jsonc"}) {
		t.Errorf("after reorder = %v", got)
	}
	// into a folder: the request and its body file go together
	p, err := s.Move(bg, a, f, 99)
	if err != nil || p != filepath.Join(f, "a.request.jsonc") {
		t.Fatalf("move: %s, %v", p, err)
	}
	if !exist(filepath.Join(f, "a.body.txt")) || exist(filepath.Join(c1, "a.body.txt")) || exist(a) {
		t.Error("the body file did not move")
	}
	if got := keys(s.Snapshot().Find(f)); !reflect.DeepEqual(got, []string{"a.request.jsonc"}) {
		t.Errorf("folder = %v", got)
	}
	// a request with the same name in the destination gets a number, and a body file named like it too
	other, _ := s.CreateRequest(bg, c2, RequestFiles{Request: format.Request{Name: "B"}})
	moved, err := s.Move(bg, b, c2, 0)
	if err != nil || filepath.Base(moved) != "b-2.request.jsonc" {
		t.Fatalf("clash move: %s, %v", moved, err)
	}
	if got := keys(s.Snapshot().Find(c2)); !reflect.DeepEqual(got, []string{"b-2.request.jsonc", filepath.Base(other)}) {
		t.Errorf("destination = %v", got)
	}
	// a folder with its content goes to another collection, a folder into a folder
	fm, err := s.Move(bg, f, c2, -1)
	if err != nil || fm != filepath.Join(c2, "f") || !exist(filepath.Join(fm, "a.request.jsonc")) {
		t.Fatalf("folder move: %s, %v", fm, err)
	}
	gm, err := s.Move(bg, g, fm, 0)
	if err != nil || gm != filepath.Join(fm, "g") {
		t.Fatalf("folder into folder: %s, %v", gm, err)
	}
	if got := keys(s.Snapshot().Find(c1)); !reflect.DeepEqual(got, []string{"c.request.jsonc"}) {
		t.Errorf("source after the moves = %v", got)
	}
	if got := keys(s.Snapshot().Find(fm)); !reflect.DeepEqual(got, []string{"g", "a.request.jsonc"}) {
		t.Errorf("moved folder = %v", got)
	}
	// a folder of the same name in the destination
	f2, _ := s.CreateFolder(bg, c1, "F")
	if m, err := s.Move(bg, f2, c2, 0); err != nil || filepath.Base(m) != "f-2" {
		t.Errorf("folder clash: %s, %v", m, err)
	}
}

func TestDuplicate(t *testing.T) {
	s, _, _ := open(t)
	c, _ := s.CreateCollection(bg, "C")
	f, _ := s.CreateFolder(bg, c, "F")
	r, _ := s.CreateRequest(bg, f, RequestFiles{Request: format.Request{Name: "Req", Body: &format.Body{Type: format.BodyJSON}}, Body: "{}"})
	r2, _ := s.CreateRequest(bg, f, RequestFiles{Request: format.Request{Name: "Last"}})
	env, _ := s.CreateEnvironment(bg, "Dev")
	if err := s.SaveEnvironment(bg, env, format.Environment{Name: "Dev", Variables: []format.Variable{{Name: "k", Value: "v", Enabled: true}}}); err != nil {
		t.Fatal(err)
	}

	dup, err := s.Duplicate(bg, r)
	if err != nil || dup != filepath.Join(f, "req-copy.request.jsonc") {
		t.Fatalf("request: %s, %v", dup, err)
	}
	files, err := s.ReadRequest(bg, dup)
	if err != nil || files.Request.Name != "Req copy" || files.Request.Body.File != "req-copy.body.json" || files.Body != "{}" {
		t.Errorf("duplicate = %+v, %v", files, err)
	}
	if !exist(filepath.Join(f, "req.body.json")) {
		t.Error("the original lost its body file")
	}
	if got := keys(s.Snapshot().Find(f)); !reflect.DeepEqual(got, []string{"req.request.jsonc", "req-copy.request.jsonc", filepath.Base(r2)}) {
		t.Errorf("the copy is not right after the original: %v", got)
	}
	dup2, err := s.Duplicate(bg, r)
	if err != nil || filepath.Base(dup2) != "req-copy-2.request.jsonc" {
		t.Errorf("second copy: %s, %v", dup2, err)
	}
	fd, err := s.Duplicate(bg, f)
	if err != nil || fd != filepath.Join(c, "f-copy") {
		t.Fatalf("folder: %s, %v", fd, err)
	}
	node := s.Snapshot().Find(fd)
	if node.Name != "F copy" || len(node.Children) != 4 {
		t.Errorf("folder copy = %s with %v", node.Name, keys(node))
	}
	cd, err := s.Duplicate(bg, c)
	if err != nil || filepath.Base(cd) != "c-copy" {
		t.Fatalf("collection: %s, %v", cd, err)
	}
	if n := s.Snapshot().Find(cd); n.Name != "C copy" || len(n.Children) != 2 {
		t.Errorf("collection copy = %s %v", n.Name, keys(n))
	}
	ed, err := s.Duplicate(bg, env)
	if err != nil || filepath.Base(ed) != "dev-copy.jsonc" {
		t.Fatalf("environment: %s, %v", ed, err)
	}
	if e := s.Snapshot().Find(ed); e.Name != "Dev copy" || len(e.Environment.Variables) != 1 {
		t.Errorf("environment copy = %+v", e)
	}
	// a folder without a descriptor file gets one in its copy
	bare := filepath.Join(c, "bare")
	write(t, filepath.Join(bare, "x.request.jsonc"), `{"emetosHttp": 1, "name": "X", "method": "GET", "url": "u"}`)
	if _, err := s.Refresh(bg); err != nil {
		t.Fatal(err)
	}
	bd, err := s.Duplicate(bg, bare)
	if err != nil || !exist(filepath.Join(bd, "folder.jsonc")) {
		t.Errorf("bare folder: %s, %v", bd, err)
	}
}

func TestDeleteGoesToTheTrashWithTheBodyFiles(t *testing.T) {
	s, h, _ := open(t)
	c, _ := s.CreateCollection(bg, "C")
	f, _ := s.CreateFolder(bg, c, "F")
	r, _ := s.CreateRequest(bg, f, RequestFiles{Request: format.Request{Name: "R", Body: &format.Body{Type: format.BodyGraphQL}}, Body: "q", Variables: "{}"})
	r2, _ := s.CreateRequest(bg, f, RequestFiles{Request: format.Request{Name: "Keep"}})

	undo, err := s.Delete(bg, r)
	if err != nil {
		t.Fatal(err)
	}
	if exist(r) || exist(filepath.Join(f, "r.body.graphql")) || exist(filepath.Join(f, "r.variables.json")) {
		t.Error("files of the request are still there")
	}
	trashed, _ := os.ReadDir(h.trashDir)
	if len(trashed) != 3 {
		t.Errorf("%d items in the trash, want 3", len(trashed))
	}
	if s.Snapshot().Find(r) != nil {
		t.Error("the request is still in the tree")
	}
	// the order keeps the name: taking the delete back puts the request where it was
	if c := read(t, filepath.Join(f, "folder.jsonc")); !strings.Contains(c, "r.request.jsonc") {
		t.Errorf("the order lost the entry: %s", c)
	}
	if err := undo(bg); err != nil {
		t.Fatal(err)
	}
	if got := keys(s.Snapshot().Find(f)); !reflect.DeepEqual(got, []string{"r.request.jsonc", filepath.Base(r2)}) {
		t.Errorf("after undo = %v", got)
	}
	if !exist(filepath.Join(f, "r.body.graphql")) {
		t.Error("the body file did not come back")
	}
	// a folder and an environment
	if _, err := s.Delete(bg, f); err != nil || exist(f) {
		t.Errorf("folder: %v", err)
	}
	env, _ := s.CreateEnvironment(bg, "E")
	if _, err := s.Delete(bg, env); err != nil || s.Snapshot().Find(env) != nil {
		t.Errorf("environment: %v", err)
	}
}

func TestDeleteWithoutATrash(t *testing.T) {
	folder := t.TempDir()
	s, err := Open(bg, folder, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, _ := s.CreateCollection(bg, "C")
	_, err = s.Delete(bg, c)
	wantRoute(t, err, "Store.Delete.HttpTrashUnavailable")
}

func TestSaveRequestKeepsTheFileAndCleansTheBodyFiles(t *testing.T) {
	s, _, folder := open(t)
	c := filepath.Join(folder, ".emetos/http/collections/api")
	path := filepath.Join(c, "r.request.jsonc")
	write(t, filepath.Join(c, "collection.jsonc"), `{"emetosHttp": 1, "name": "API"}`)
	write(t, path, "// keep me\n{\n  \"emetosHttp\": 1,\n  \"name\": \"R\",\n  \"method\": \"POST\",\n  \"url\": \"u\",\n  \"body\": {\"type\": \"json\", \"file\": \"r.body.json\"}\n}\n")
	write(t, filepath.Join(c, "r.body.json"), "{}")
	if _, err := s.Refresh(bg); err != nil {
		t.Fatal(err)
	}
	files, err := s.ReadRequest(bg, path)
	if err != nil {
		t.Fatal(err)
	}
	files.Body = "{\"a\": 1}"
	files.Request.URL = "http://x"
	if err := s.SaveRequest(bg, path, files); err != nil {
		t.Fatal(err)
	}
	text := read(t, path)
	if !strings.HasPrefix(text, "// keep me\n") || !strings.Contains(text, `"url": "http://x"`) || read(t, filepath.Join(c, "r.body.json")) != "{\"a\": 1}" {
		t.Errorf("saved = %s", text)
	}
	if got := s.Snapshot().Find(path).Request.URL; got != "http://x" {
		t.Errorf("the tree has url %s", got)
	}
	// the type changes to xml: the new file is made and the old one goes
	files.Request.Body = &format.Body{Type: format.BodyXML}
	files.Body = "<a/>"
	if err := s.SaveRequest(bg, path, files); err != nil {
		t.Fatal(err)
	}
	if exist(filepath.Join(c, "r.body.json")) || read(t, filepath.Join(c, "r.body.xml")) != "<a/>" {
		t.Error("the body file of the old type was not replaced")
	}
	// to a form: no body file at all
	files.Request.Body = &format.Body{Type: format.BodyForm, Fields: []format.Pair{{Name: "a", Value: "b", Enabled: true}}}
	if err := s.SaveRequest(bg, path, files); err != nil {
		t.Fatal(err)
	}
	if exist(filepath.Join(c, "r.body.xml")) {
		t.Error("the body file of a form stayed")
	}
	// the body is dropped
	files.Request.Body = nil
	if err := s.SaveRequest(bg, path, files); err != nil || strings.Contains(read(t, path), `"body"`) {
		t.Errorf("body removed: %v\n%s", err, read(t, path))
	}
	// a request that cannot be read is refused
	write(t, path, "[")
	_, err = s.ReadRequest(bg, path)
	wantRoute(t, err, "HttpFileSyntax")
	err = s.SaveRequest(bg, path, files)
	wantRoute(t, err, "Store.SaveRequest->Parse[...]")
	_, err = s.ReadRequest(bg, filepath.Join(c, "none.request.jsonc"))
	wantRoute(t, err, "HttpFileReadFailed")
	err = s.SaveRequest(bg, filepath.Join(c, "none.request.jsonc"), files)
	wantRoute(t, err, "HttpFileReadFailed")
}

func TestNewerAndBrokenFilesAreNeverChanged(t *testing.T) {
	s, _, folder := open(t)
	c := filepath.Join(folder, ".emetos/http/collections/api")
	write(t, filepath.Join(c, "collection.jsonc"), `{"emetosHttp": 1, "name": "API"}`)
	newer := filepath.Join(c, "new.request.jsonc")
	broken := filepath.Join(c, "broken.request.jsonc")
	write(t, newer, `{"emetosHttp": 2, "name": "New", "method": "GET", "url": "u"}`)
	write(t, broken, `{"emetosHttp": 1, "name": }`)
	newerEnv := filepath.Join(folder, ".emetos/http/environments/n.jsonc")
	write(t, newerEnv, `{"emetosHttp": 2, "name": "N"}`)
	newerGroup := filepath.Join(c, "futuregroup")
	write(t, filepath.Join(newerGroup, "folder.jsonc"), `{"emetosHttp": 2, "name": "Future"}`)
	brokenGroup := filepath.Join(c, "brokengroup")
	write(t, filepath.Join(brokenGroup, "folder.jsonc"), `{`)
	if _, err := s.Refresh(bg); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, p := range []string{newer, broken, newerEnv, filepath.Join(newerGroup, "folder.jsonc"), filepath.Join(brokenGroup, "folder.jsonc")} {
		before[p] = read(t, p)
	}
	tests := []struct {
		name  string
		do    func() error
		route string
	}{
		{"rename a newer request", func() error { _, err := s.Rename(bg, newer, "x"); return err }, "Store.Rename->Store.renameRequest.HttpNodeNotWritable"},
		{"rename a broken request", func() error { _, err := s.Rename(bg, broken, "x"); return err }, "Store.renameRequest.HttpNodeNotWritable"},
		{"rename a newer environment", func() error { _, err := s.Rename(bg, newerEnv, "x"); return err }, "Store.renameEnvironment.HttpNodeNotWritable"},
		{"rename a newer folder", func() error { _, err := s.Rename(bg, newerGroup, "x"); return err }, "Store.renameGroup.HttpNodeNotWritable"},
		{"rename a broken folder", func() error { _, err := s.Rename(bg, brokenGroup, "x"); return err }, "Store.renameGroup.HttpNodeNotWritable"},
		{"move a newer request", func() error { _, err := s.Move(bg, newer, newerGroup, 0); return err }, "Store.Move->orderWritable.HttpNodeNotWritable"},
		{"move into a folder of a newer version", func() error { _, err := s.Move(bg, broken, newerGroup, 0); return err }, "Store.Move->orderWritable.HttpNodeNotWritable"},
		{"move a newer request elsewhere", func() error {
			other, _ := s.CreateFolder(bg, c, "Other")
			_, err := s.Move(bg, newer, other, 0)
			return err
		}, "Store.moveRequestFiles.HttpNodeNotWritable"},
		{"duplicate a newer request", func() error { _, err := s.Duplicate(bg, newer); return err }, "Store.duplicateRequest.HttpNodeNotWritable"},
		{"duplicate a newer environment", func() error { _, err := s.Duplicate(bg, newerEnv); return err }, "Store.duplicateEnvironment.HttpNodeNotWritable"},
		{"create in a newer folder", func() error { _, err := s.CreateRequest(bg, newerGroup, RequestFiles{}); return err }, "Store.CreateRequest->orderWritable.HttpNodeNotWritable"},
		{"create a folder in a broken folder", func() error { _, err := s.CreateFolder(bg, brokenGroup, "x"); return err }, "Store.CreateFolder->orderWritable.HttpNodeNotWritable"},
		{"save a newer request", func() error { return s.SaveRequest(bg, newer, RequestFiles{Request: format.Request{Name: "n"}}) }, "Store.SaveRequest->Update[...].HttpFileNewerVersion"},
		{"save a newer folder", func() error { return s.SaveGroup(bg, newerGroup, format.Collection{Name: "x"}) }, "Store.SaveGroup->writeGroupDoc->Update[...].HttpFileNewerVersion"},
		{"save a newer environment", func() error { return s.SaveEnvironment(bg, newerEnv, format.Environment{Name: "x"}) }, "Store.SaveEnvironment->Update[...].HttpFileNewerVersion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.do()
			wantRoute(t, err, tt.route)
		})
	}
	for p, text := range before {
		if got := read(t, p); got != text {
			t.Errorf("%s was changed:\n%s", p, got)
		}
	}
}

func TestFileOperationsThatFailAreReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the mode of a folder")
	}
	s, _, folder := open(t)
	c, _ := s.CreateCollection(bg, "C")
	f, _ := s.CreateFolder(bg, c, "F")
	// the parent of the folder cannot be changed: the file is written, the rename fails
	if err := os.Chmod(c, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(c, 0o755) })
	_, err := s.Rename(bg, f, "Other")
	wantRoute(t, err, "HttpFileOpFailed")
	// a copy into a folder that cannot be made
	_, err = s.Duplicate(bg, f)
	wantRoute(t, err, "copyTree.HttpFileOpFailed")
	_ = folder
}

func TestOperationsStopWhenTheContextEnds(t *testing.T) {
	s, _, _ := open(t)
	c, _ := s.CreateCollection(bg, "C")
	f, _ := s.CreateFolder(bg, c, "F")
	write(t, filepath.Join(f, "x.request.jsonc"), `{"emetosHttp": 1, "name": "X", "method": "GET", "url": "u"}`)
	if _, err := s.Refresh(bg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := cancelled()
	defer cancel()
	err := copyTree(ctx, f, filepath.Join(c, "copy"))
	wantRoute(t, err, "copyTree.HttpCancelled")
	_, err = s.CreateCollection(ctx, "Z")
	wantRoute(t, err, "HttpCancelled")
}
