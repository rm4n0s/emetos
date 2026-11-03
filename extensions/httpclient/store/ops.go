package store

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// RequestFiles is a request with the text of its body files: what the request tab edits and saves.
type RequestFiles struct {
	Request format.Request
	// Body is the text of the body file (Request.Body.File); Variables the text of the GraphQL variables file.
	Body      string
	Variables string
}

// DefaultRequestName is the name of a new request that has none.
const DefaultRequestName = "New Request"

// find returns the node with the path in the current tree. The error has the tag HttpNodeNotFound.
func (s *Store) find(path string) (*Node, error) {
	n := s.Snapshot().Find(path)
	if n == nil {
		return nil, errors.New("HttpNodeNotFound", "there is no such collection, folder, request or environment", "path", path)
	}
	return n, nil
}

// group returns the node with the path when it is a collection or a folder. The errors have the tags HttpNodeNotFound and
// HttpNodeKindInvalid.
func (s *Store) group(path string) (*Node, error) {
	n, err := s.find(path)
	if err != nil {
		return nil, err
	}
	if n.Kind != KindCollection && n.Kind != KindFolder {
		return nil, errors.New("HttpNodeKindInvalid", "this must be a collection or a folder", "path", path)
	}
	return n, nil
}

// finish refreshes the tree after an operation wrote, and pokes the watcher loop so that it moves the watch to a folder that was made.
func (s *Store) finish(ctx context.Context) error {
	_, err := s.reload(ctx)
	s.poke()
	return err
}

// CreateCollection makes the collection folder and its collection.jsonc for the name, and returns the path of the folder. The
// errors have the tags HttpNameEmpty and those of the file helpers.
func (s *Store) CreateCollection(ctx context.Context, name string) (string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("HttpNameEmpty", "the name is empty")
	}
	dir := filepath.Join(s.root, format.CollectionsDir)
	stem := format.UniqueName(format.SafeName(name), func(n string) bool { return exists(filepath.Join(dir, n)) })
	path := filepath.Join(dir, stem)
	text, err := format.Marshal(format.Collection{Name: name})
	if err != nil {
		return "", err
	}
	if err := format.WriteFile(ctx, filepath.Join(path, format.CollectionFile), text); err != nil {
		return "", err
	}
	return path, s.finish(ctx)
}

// CreateFolder makes a folder in the collection or folder at parent, last in its order, and returns its path.
func (s *Store) CreateFolder(ctx context.Context, parent, name string) (string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("HttpNameEmpty", "the name is empty")
	}
	p, err := s.group(parent)
	if err != nil {
		return "", err
	}
	if err := orderWritable(p); err != nil {
		return "", err
	}
	stem := format.UniqueName(format.SafeName(name), func(n string) bool { return exists(filepath.Join(p.Path, n)) })
	path := filepath.Join(p.Path, stem)
	text, err := format.Marshal(format.Folder{Name: name})
	if err != nil {
		return "", err
	}
	if err := format.WriteFile(ctx, filepath.Join(path, format.FolderFile), text); err != nil {
		return "", err
	}
	if err := s.editOrder(ctx, p, func(keys []string) []string { return append(keys, stem) }); err != nil {
		return "", err
	}
	return path, s.finish(ctx)
}

// CreateRequest makes a request file in the collection or folder at parent, last in its order, with the files of its body, and
// returns the path of the request file. A request without a name is called DefaultRequestName, without a method is a GET. The
// names of the body files are made from the name of the request (Request.Body.File is set when there is text).
func (s *Store) CreateRequest(ctx context.Context, parent string, files RequestFiles) (string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	p, err := s.group(parent)
	if err != nil {
		return "", err
	}
	if err := orderWritable(p); err != nil {
		return "", err
	}
	req := files.Request
	if strings.TrimSpace(req.Name) == "" {
		req.Name = DefaultRequestName
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	stem := format.UniqueName(format.SafeName(req.Name), func(n string) bool { return stemTaken(p.Path, n) })
	path := filepath.Join(p.Path, format.RequestFileName(stem))
	if err := s.writeBodies(ctx, p.Path, stem, nil, &req, files); err != nil {
		return "", err
	}
	text, err := format.Marshal(req)
	if err != nil {
		return "", err
	}
	if err := format.WriteFile(ctx, path, text); err != nil {
		return "", err
	}
	key := format.RequestFileName(stem)
	if err := s.editOrder(ctx, p, func(keys []string) []string { return append(keys, key) }); err != nil {
		return "", err
	}
	return path, s.finish(ctx)
}

// ReadRequest reads a request file and the files of its body (a body file that is not there is empty text). A file of a newer
// version is returned with the error HttpFileNewerVersion (format.IsNewer), as format.Parse does.
func (s *Store) ReadRequest(ctx context.Context, path string) (RequestFiles, error) {
	text, err := format.ReadFile(ctx, path)
	if err != nil {
		return RequestFiles{}, err
	}
	req, perr := format.Parse[format.Request](text)
	if perr != nil && !format.IsNewer(perr) {
		return RequestFiles{}, perr
	}
	out := RequestFiles{Request: req}
	dir := filepath.Dir(path)
	if req.Body != nil {
		if out.Body, err = readSibling(ctx, dir, req.Body.File); err != nil {
			return RequestFiles{}, err
		}
		if out.Variables, err = readSibling(ctx, dir, req.Body.Variables); err != nil {
			return RequestFiles{}, err
		}
	}
	return out, perr
}

// readSibling reads a body file beside a request; a name that is empty or a file that does not exist is empty text.
func readSibling(ctx context.Context, dir, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	text, err := format.ReadFile(ctx, filepath.Join(dir, name))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return text, err
}

// SaveRequest writes a request and its body files. The request file is changed by edits (the comments and the keys a user added
// stay), the body files are written whole, and a body file the request no longer uses is removed. It fails with the tags of
// format.Update, among them HttpFileNewerVersion.
func (s *Store) SaveRequest(ctx context.Context, path string, files RequestFiles) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	old, err := format.ReadFile(ctx, path)
	if err != nil {
		return err
	}
	oldReq, perr := format.Parse[format.Request](old)
	if perr != nil && !format.IsNewer(perr) {
		return perr
	}
	stem, _ := format.RequestStem(filepath.Base(path))
	req := files.Request
	if err := s.writeBodies(ctx, filepath.Dir(path), stem, oldReq.Body, &req, files); err != nil {
		return err
	}
	text, err := format.Update(old, req)
	if err != nil {
		return err
	}
	if err := format.WriteFile(ctx, path, text); err != nil {
		return err
	}
	return s.finish(ctx)
}

// writeBodies writes the body files of a request whose stem is stem into dir and sets the names in req.Body: a body that has text
// gets the name it has, or one made from the stem; a body of a type that has no file has none. The files the old body used and the
// new one does not are removed.
func (s *Store) writeBodies(ctx context.Context, dir, stem string, oldBody *format.Body, req *format.Request, files RequestFiles) error {
	if req.Body != nil {
		b := *req.Body
		req.Body = &b
		if format.BodyExtension(b.Type) == "" {
			req.Body.File, req.Body.Variables = "", ""
		} else {
			if req.Body.File == "" && files.Body != "" {
				req.Body.File = format.BodyFileName(stem, b.Type)
			}
			if b.Type != format.BodyGraphQL {
				req.Body.Variables = ""
			} else if req.Body.Variables == "" && files.Variables != "" {
				req.Body.Variables = format.VariablesFileName(stem)
			}
		}
		if req.Body.File != "" {
			if err := format.WriteFile(ctx, filepath.Join(dir, req.Body.File), files.Body); err != nil {
				return err
			}
		}
		if req.Body.Variables != "" {
			if err := format.WriteFile(ctx, filepath.Join(dir, req.Body.Variables), files.Variables); err != nil {
				return err
			}
		}
	}
	if oldBody == nil {
		return nil
	}
	for _, name := range []string{oldBody.File, oldBody.Variables} {
		if name == "" || !format.ValidSiblingName(name) {
			continue
		}
		if req.Body != nil && (req.Body.File == name || req.Body.Variables == name) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", filepath.Join(dir, name), "op", "remove")
		}
	}
	return nil
}

// SaveGroup writes the collection.jsonc or folder.jsonc of the collection or folder at path by edits (it makes the file when the
// folder has none).
func (s *Store) SaveGroup(ctx context.Context, path string, doc format.Collection) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	n, err := s.group(path)
	if err != nil {
		return err
	}
	if err := writeGroupDoc(ctx, n, doc); err != nil {
		return err
	}
	return s.finish(ctx)
}

// writeGroupDoc writes the descriptor of a group.
func writeGroupDoc(ctx context.Context, n *Node, doc format.Collection) error {
	var text string
	if n.HasFile {
		old, err := format.ReadFile(ctx, n.File)
		if err != nil {
			return err
		}
		if text, err = format.Update(old, doc); err != nil {
			return err
		}
	} else {
		var err error
		if text, err = format.Marshal(doc); err != nil {
			return err
		}
	}
	return format.WriteFile(ctx, n.File, text)
}

// editOrder rewrites the order of the collection or folder: change is given the keys of its entries as they are shown now
// and returns the new order. The folder.jsonc is made when the folder has none.
func (s *Store) editOrder(ctx context.Context, g *Node, change func(keys []string) []string) error {
	keys := make([]string, 0, len(g.Children)+1)
	for _, c := range g.Children {
		keys = append(keys, c.Key)
	}
	// the tree may not have an entry that was just made: list the folder
	entries, err := readDir(g.Path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		key := e.Name()
		if hidden(key) || slices.Contains(keys, key) {
			continue
		}
		if e.IsDir() {
			keys = append(keys, key)
		} else if _, ok := format.RequestStem(key); ok {
			keys = append(keys, key)
		}
	}
	doc := format.Collection{Name: g.Name}
	if g.Group != nil {
		doc = *g.Group
	}
	// an entry made in this operation is last; the others are in the order shown
	doc.Order = change(keys)
	return writeGroupDoc(ctx, g, doc)
}

// Rename gives the collection, folder, request or environment at path a new name, renames its files to the safe form of the name
// (a number is added on a clash) and updates the order of its parent. It returns the new path.
func (s *Store) Rename(ctx context.Context, path, name string) (string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("HttpNameEmpty", "the name is empty")
	}
	n, err := s.find(path)
	if err != nil {
		return "", err
	}
	parent := s.Snapshot().Parent(n)
	if err := orderWritable(parent); err != nil {
		return "", err
	}
	var newPath string
	switch n.Kind {
	case KindRequest:
		newPath, err = s.renameRequest(ctx, n, name)
	case KindEnvironment:
		newPath, err = s.renameEnvironment(ctx, n, name)
	default:
		newPath, err = s.renameGroup(ctx, n, name)
	}
	if err != nil {
		return "", err
	}
	if parent != nil && newPath != n.Path {
		oldKey, newKey := n.Key, filepath.Base(newPath)
		if err := s.editOrder(ctx, parent, func(keys []string) []string {
			return replace(keys, oldKey, newKey)
		}); err != nil {
			return "", err
		}
	}
	return newPath, s.finish(ctx)
}

func replace(keys []string, oldKey, newKey string) []string {
	out := slices.Clone(keys)
	for i, k := range out {
		if k == oldKey {
			out[i] = newKey
		}
	}
	return out
}

func (s *Store) renameGroup(ctx context.Context, n *Node, name string) (string, error) {
	if n.Group == nil || n.ReadOnly {
		return "", errors.New("HttpNodeNotWritable", "the file cannot be read or is of a newer version, so it is not changed", "path", n.Path)
	}
	doc := *n.Group
	doc.Name = name
	if err := writeGroupDoc(ctx, n, doc); err != nil {
		return "", err
	}
	dir := filepath.Dir(n.Path)
	stem := format.UniqueName(format.SafeName(name), func(c string) bool { return c != n.Key && exists(filepath.Join(dir, c)) })
	if stem == n.Key {
		return n.Path, nil
	}
	newPath := filepath.Join(dir, stem)
	if err := os.Rename(n.Path, newPath); err != nil {
		return "", errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", n.Path, "to", newPath, "op", "rename")
	}
	return newPath, nil
}

func (s *Store) renameEnvironment(ctx context.Context, n *Node, name string) (string, error) {
	if n.Environment == nil || n.ReadOnly {
		return "", errors.New("HttpNodeNotWritable", "the file cannot be read or is of a newer version, so it is not changed", "path", n.Path)
	}
	doc := *n.Environment
	doc.Name = name
	old, err := format.ReadFile(ctx, n.File)
	if err != nil {
		return "", err
	}
	text, err := format.Update(old, doc)
	if err != nil {
		return "", err
	}
	if err := format.WriteFile(ctx, n.File, text); err != nil {
		return "", err
	}
	dir := filepath.Dir(n.Path)
	stem := format.UniqueName(format.SafeName(name), func(c string) bool {
		return c != n.Key && exists(filepath.Join(dir, format.EnvironmentFileName(c)))
	})
	if stem == n.Key {
		return n.Path, nil
	}
	newPath := filepath.Join(dir, format.EnvironmentFileName(stem))
	if err := os.Rename(n.Path, newPath); err != nil {
		return "", errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", n.Path, "to", newPath, "op", "rename")
	}
	return newPath, nil
}

func (s *Store) renameRequest(ctx context.Context, n *Node, name string) (string, error) {
	if n.Request == nil || n.ReadOnly {
		return "", errors.New("HttpNodeNotWritable", "the file cannot be read or is of a newer version, so it is not changed", "path", n.Path)
	}
	dir := filepath.Dir(n.Path)
	oldStem := stemOfKey(n.Key)
	stem := format.UniqueName(format.SafeName(name), func(c string) bool { return c != oldStem && stemTaken(dir, c) })
	req := *n.Request
	req.Name = name
	if req.Body != nil {
		b := *req.Body
		req.Body = &b
		req.Body.File = renamedSibling(b.File, oldStem, stem)
		req.Body.Variables = renamedSibling(b.Variables, oldStem, stem)
	}
	old, err := format.ReadFile(ctx, n.File)
	if err != nil {
		return "", err
	}
	text, err := format.Update(old, req)
	if err != nil {
		return "", err
	}
	if err := format.WriteFile(ctx, n.File, text); err != nil {
		return "", err
	}
	if stem == oldStem {
		return n.Path, nil
	}
	if n.Request.Body != nil {
		for _, pair := range [][2]string{{n.Request.Body.File, req.Body.File}, {n.Request.Body.Variables, req.Body.Variables}} {
			if pair[0] == pair[1] || pair[0] == "" {
				continue
			}
			if err := os.Rename(filepath.Join(dir, pair[0]), filepath.Join(dir, pair[1])); err != nil && !os.IsNotExist(err) {
				return "", errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", pair[0], "to", pair[1], "op", "rename")
			}
		}
	}
	newPath := filepath.Join(dir, format.RequestFileName(stem))
	if err := os.Rename(n.Path, newPath); err != nil {
		return "", errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", n.Path, "to", newPath, "op", "rename")
	}
	return newPath, nil
}

// renamedSibling is the name of a body file after its request got a new stem: a name made from the old stem follows it, any other
// name (one a user chose) stays.
func renamedSibling(file, oldStem, newStem string) string {
	if rest, ok := strings.CutPrefix(file, oldStem+"."); ok {
		return newStem + "." + rest
	}
	return file
}

// Move moves the request or folder at path into the collection or folder at dest, to the place index among its entries (a
// negative index or one past the end is last), and returns the new path. Moving inside the same parent reorders. Both orders are
// updated; the name gets a number on a clash.
func (s *Store) Move(ctx context.Context, path, dest string, index int) (string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	n, err := s.find(path)
	if err != nil {
		return "", err
	}
	if n.Kind != KindRequest && n.Kind != KindFolder {
		return "", errors.New("HttpNodeKindInvalid", "only a request or a folder can be moved", "path", path)
	}
	d, err := s.group(dest)
	if err != nil {
		return "", err
	}
	if n.Kind == KindFolder && (d.Path == n.Path || strings.HasPrefix(d.Path, n.Path+string(filepath.Separator))) {
		return "", errors.New("HttpMoveIntoItself", "a folder cannot be moved into itself", "path", path, "dest", dest)
	}
	snap := s.Snapshot()
	oldParent := snap.Parent(n)
	if err := orderWritable(d, oldParent); err != nil {
		return "", err
	}
	same := oldParent != nil && oldParent.Path == d.Path
	newPath := n.Path
	newKey := n.Key
	if !same {
		switch n.Kind {
		case KindFolder:
			stem := format.UniqueName(n.Key, func(c string) bool { return exists(filepath.Join(d.Path, c)) })
			newPath, newKey = filepath.Join(d.Path, stem), stem
			if err := os.Rename(n.Path, newPath); err != nil {
				return "", errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", n.Path, "to", newPath, "op", "move")
			}
		default:
			oldStem := stemOfKey(n.Key)
			stem := format.UniqueName(oldStem, func(c string) bool { return stemTaken(d.Path, c) })
			if err := s.moveRequestFiles(ctx, n, d.Path, stem); err != nil {
				return "", err
			}
			newKey = format.RequestFileName(stem)
			newPath = filepath.Join(d.Path, newKey)
		}
		if oldParent != nil {
			if err := s.editOrder(ctx, oldParent, func(keys []string) []string { return removeKey(keys, n.Key) }); err != nil {
				return "", err
			}
		}
	}
	// the destination must list the entry that was just moved in
	d2 := *d
	if err := s.editOrder(ctx, &d2, func(keys []string) []string {
		keys = removeKey(keys, n.Key)
		keys = removeKey(keys, newKey)
		at := index
		if at < 0 || at > len(keys) {
			at = len(keys)
		}
		return slices.Insert(keys, at, newKey)
	}); err != nil {
		return "", err
	}
	return newPath, s.finish(ctx)
}

func removeKey(keys []string, key string) []string {
	return slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return k == key })
}

// moveRequestFiles moves the request file and its body files into dir under the stem, and rewrites the names in the request.
func (s *Store) moveRequestFiles(ctx context.Context, n *Node, dir, stem string) error {
	if n.Request == nil || n.ReadOnly {
		return errors.New("HttpNodeNotWritable", "the file cannot be read or is of a newer version, so it is not changed", "path", n.Path)
	}
	oldDir, oldStem := filepath.Dir(n.Path), stemOfKey(n.Key)
	req := *n.Request
	if req.Body != nil {
		b := *req.Body
		req.Body = &b
		req.Body.File = renamedSibling(b.File, oldStem, stem)
		req.Body.Variables = renamedSibling(b.Variables, oldStem, stem)
	}
	old, err := format.ReadFile(ctx, n.File)
	if err != nil {
		return err
	}
	text, err := format.Update(old, req)
	if err != nil {
		return err
	}
	if err := format.WriteFile(ctx, filepath.Join(dir, format.RequestFileName(stem)), text); err != nil {
		return err
	}
	if n.Request.Body != nil {
		for _, pair := range [][2]string{{n.Request.Body.File, req.Body.File}, {n.Request.Body.Variables, req.Body.Variables}} {
			if pair[0] == "" {
				continue
			}
			if err := os.Rename(filepath.Join(oldDir, pair[0]), filepath.Join(dir, pair[1])); err != nil && !os.IsNotExist(err) {
				return errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", pair[0], "to", pair[1], "op", "move")
			}
		}
	}
	if err := os.Remove(n.Path); err != nil {
		return errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", n.Path, "op", "remove")
	}
	return nil
}

// Duplicate copies the collection, folder, request or environment at path, with the files of its body and what is inside it, under
// the name "<name> copy" (a number is added on a clash), right after the original in the order of its parent. It returns the path
// of the copy.
func (s *Store) Duplicate(ctx context.Context, path string) (string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	n, err := s.find(path)
	if err != nil {
		return "", err
	}
	parent := s.Snapshot().Parent(n)
	if err := orderWritable(parent); err != nil {
		return "", err
	}
	name := n.Name + " copy"
	var newPath string
	switch n.Kind {
	case KindRequest:
		newPath, err = s.duplicateRequest(ctx, n, name)
	case KindEnvironment:
		newPath, err = s.duplicateEnvironment(ctx, n, name)
	default:
		newPath, err = s.duplicateGroup(ctx, n, name)
	}
	if err != nil {
		return "", err
	}
	if parent != nil {
		after, key := n.Key, filepath.Base(newPath)
		if err := s.editOrder(ctx, parent, func(keys []string) []string {
			keys = removeKey(keys, key)
			return slices.Insert(keys, slices.Index(keys, after)+1, key)
		}); err != nil {
			return "", err
		}
	}
	return newPath, s.finish(ctx)
}

func (s *Store) duplicateRequest(ctx context.Context, n *Node, name string) (string, error) {
	if n.Request == nil || n.ReadOnly {
		return "", errors.New("HttpNodeNotWritable", "the file cannot be read or is of a newer version, so it is not changed", "path", n.Path)
	}
	dir, oldStem := filepath.Dir(n.Path), stemOfKey(n.Key)
	stem := format.UniqueName(format.SafeName(name), func(c string) bool { return stemTaken(dir, c) })
	req := *n.Request
	req.Name = name
	files := RequestFiles{Request: req}
	if req.Body != nil {
		b := *req.Body
		req.Body = &b
		var err error
		if files.Body, err = readSibling(ctx, dir, b.File); err != nil {
			return "", err
		}
		if files.Variables, err = readSibling(ctx, dir, b.Variables); err != nil {
			return "", err
		}
		req.Body.File = renamedSibling(b.File, oldStem, stem)
		req.Body.Variables = renamedSibling(b.Variables, oldStem, stem)
		if req.Body.File == b.File {
			req.Body.File = "" // a name a user chose belongs to the original: the copy gets its own
		}
		if req.Body.Variables == b.Variables {
			req.Body.Variables = ""
		}
		files.Request = req
	}
	if err := s.writeBodies(ctx, dir, stem, nil, &req, files); err != nil {
		return "", err
	}
	text, err := format.Marshal(req)
	if err != nil {
		return "", err
	}
	newPath := filepath.Join(dir, format.RequestFileName(stem))
	return newPath, format.WriteFile(ctx, newPath, text)
}

func (s *Store) duplicateEnvironment(ctx context.Context, n *Node, name string) (string, error) {
	if n.Environment == nil || n.ReadOnly {
		return "", errors.New("HttpNodeNotWritable", "the file cannot be read or is of a newer version, so it is not changed", "path", n.Path)
	}
	dir := filepath.Dir(n.Path)
	stem := format.UniqueName(format.SafeName(name), func(c string) bool { return exists(filepath.Join(dir, format.EnvironmentFileName(c))) })
	doc := *n.Environment
	doc.Name = name
	text, err := format.Marshal(doc)
	if err != nil {
		return "", err
	}
	newPath := filepath.Join(dir, format.EnvironmentFileName(stem))
	return newPath, format.WriteFile(ctx, newPath, text)
}

func (s *Store) duplicateGroup(ctx context.Context, n *Node, name string) (string, error) {
	dir := filepath.Dir(n.Path)
	stem := format.UniqueName(format.SafeName(name), func(c string) bool { return exists(filepath.Join(dir, c)) })
	newPath := filepath.Join(dir, stem)
	if err := copyTree(ctx, n.Path, newPath); err != nil {
		return "", err
	}
	if n.Group != nil && !n.ReadOnly && n.HasFile {
		doc := *n.Group
		doc.Name = name
		copied := &Node{Kind: n.Kind, Path: newPath, File: filepath.Join(newPath, filepath.Base(n.File)), HasFile: true}
		if err := writeGroupDoc(ctx, copied, doc); err != nil {
			return "", err
		}
	} else if n.Group != nil && !n.HasFile {
		doc := *n.Group
		doc.Name = name
		text, err := format.Marshal(doc)
		if err != nil {
			return "", err
		}
		if err := format.WriteFile(ctx, filepath.Join(newPath, filepath.Base(n.File)), text); err != nil {
			return "", err
		}
	}
	return newPath, nil
}

// copyTree copies a folder with everything in it (files and folders; links are not followed).
func copyTree(ctx context.Context, from, to string) error {
	entries, err := readDir(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", to, "op", "mkdir")
	}
	for _, e := range entries {
		src, dst := filepath.Join(from, e.Name()), filepath.Join(to, e.Name())
		switch {
		case e.IsDir():
			if err := copyTree(ctx, src, dst); err != nil {
				return err
			}
		case e.Type().IsRegular():
			text, err := os.ReadFile(src)
			if err != nil {
				return errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", src, "op", "read")
			}
			if err := ctx.Err(); err != nil {
				return errors.NewErr("HttpCancelled", err).SetMetadata("path", src)
			}
			if err := os.WriteFile(dst, text, 0o644); err != nil {
				return errors.NewErr("HttpFileOpFailed", err).SetMetadata("path", dst, "op", "write")
			}
		}
	}
	return nil
}

// Delete moves the collection, folder, request or environment at path to the trash, with the body files of a request and what is
// inside a folder, and returns the function that takes it back. The order of the parent is not changed (an entry that is not
// there is skipped when it is read), so taking the delete back puts the entry in its place. It fails with HttpTrashUnavailable
// when the store has no host, and with the tags of the trash.
func (s *Store) Delete(ctx context.Context, path string) (undo func(ctx context.Context) error, err error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	n, err := s.find(path)
	if err != nil {
		return nil, err
	}
	if s.host == nil {
		return nil, errors.New("HttpTrashUnavailable", "this workspace has no trash", "path", path)
	}
	paths := []string{n.Path}
	if n.Kind == KindRequest && n.Request != nil && n.Request.Body != nil {
		for _, name := range []string{n.Request.Body.File, n.Request.Body.Variables} {
			if name != "" && format.ValidSiblingName(name) && exists(filepath.Join(filepath.Dir(n.Path), name)) {
				paths = append(paths, filepath.Join(filepath.Dir(n.Path), name))
			}
		}
	}
	uris := make([]string, len(paths))
	for i, p := range paths {
		uris[i] = (&url.URL{Scheme: "file", Path: p}).String()
	}
	hostUndo, err := s.host.TrashFiles(ctx, uris)
	if rerr := s.finish(ctx); rerr != nil && err == nil {
		err = rerr
	}
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		if hostUndo == nil {
			return nil
		}
		uerr := hostUndo(ctx)
		if _, rerr := s.Refresh(ctx); rerr != nil && uerr == nil {
			uerr = rerr
		}
		return uerr
	}, nil
}

// CreateEnvironment makes an environment file for the name and returns its path.
func (s *Store) CreateEnvironment(ctx context.Context, name string) (string, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("HttpNameEmpty", "the name is empty")
	}
	dir := filepath.Join(s.root, format.EnvironmentsDir)
	stem := format.UniqueName(format.SafeName(name), func(c string) bool { return exists(filepath.Join(dir, format.EnvironmentFileName(c))) })
	text, err := format.Marshal(format.Environment{Name: name})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, format.EnvironmentFileName(stem))
	if err := format.WriteFile(ctx, path, text); err != nil {
		return "", err
	}
	return path, s.finish(ctx)
}

// SaveEnvironment writes the environment at path by edits.
func (s *Store) SaveEnvironment(ctx context.Context, path string, doc format.Environment) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	n, err := s.find(path)
	if err != nil {
		return err
	}
	if n.Kind != KindEnvironment {
		return errors.New("HttpNodeKindInvalid", "this must be an environment", "path", path)
	}
	old, err := format.ReadFile(ctx, n.File)
	if err != nil {
		return err
	}
	text, err := format.Update(old, doc)
	if err != nil {
		return err
	}
	if err := format.WriteFile(ctx, n.File, text); err != nil {
		return err
	}
	return s.finish(ctx)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// stemTaken reports whether a request with the stem, or one of its body files, is in dir, or a folder is named like it.
func stemTaken(dir, stem string) bool {
	if exists(filepath.Join(dir, format.RequestFileName(stem))) || exists(filepath.Join(dir, stem)) {
		return true
	}
	if exists(filepath.Join(dir, format.VariablesFileName(stem))) {
		return true
	}
	for _, t := range []string{format.BodyJSON, format.BodyXML, format.BodyText, format.BodyHTML, format.BodyJavaScript, format.BodyGraphQL} {
		if exists(filepath.Join(dir, format.BodyFileName(stem, t))) {
			return true
		}
	}
	return false
}

// orderWritable checks that the descriptors of the groups can be edited before an operation changes any file: a collection or
// folder whose file cannot be read or is of a newer version keeps its order. A nil group is skipped.
func orderWritable(groups ...*Node) error {
	for _, g := range groups {
		if g != nil && (g.ReadOnly || (g.HasFile && g.Group == nil)) {
			return errors.New("HttpNodeNotWritable", "the file cannot be read or is of a newer version, so its order is not changed", "path", g.Path)
		}
	}
	return nil
}
