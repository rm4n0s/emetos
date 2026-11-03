package store

import (
	"context"
	"crypto/sha256"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// MaxDepth is how deep the folders of a collection go; deeper folders are not read.
const MaxDepth = 32

// scanner reads the files of the client into a Tree and fingerprints what it read.
type scanner struct {
	ctx    context.Context
	folder string // the workspace folder
	h      hash.Hash
}

// scan reads <root> (the .emetos/http folder). The fingerprint changes when a file the tree is made of, or the list of the files
// beside them, changes. A root that does not exist is an empty tree. The error is that of listing a folder of the root that cannot
// be listed (HttpDirReadFailed) or of ctx.
func scan(ctx context.Context, folder string) (*Tree, string, error) {
	s := &scanner{ctx: ctx, folder: folder, h: sha256.New()}
	root := filepath.Join(folder, filepath.FromSlash(format.Dir))
	tree := &Tree{}
	entries, err := readDir(filepath.Join(root, format.CollectionsDir))
	if err != nil {
		return nil, "", err
	}
	for _, e := range entries {
		if e.IsDir() && !hidden(e.Name()) {
			n, err := s.group(KindCollection, filepath.Join(root, format.CollectionsDir, e.Name()), 0)
			if err != nil {
				return nil, "", err
			}
			tree.Collections = append(tree.Collections, n)
		}
	}
	entries, err = readDir(filepath.Join(root, format.EnvironmentsDir))
	if err != nil {
		return nil, "", err
	}
	for _, e := range entries {
		if e.Type().IsRegular() && !hidden(e.Name()) && strings.HasSuffix(e.Name(), format.EnvironmentSuffix) {
			n, err := s.environment(filepath.Join(root, format.EnvironmentsDir, e.Name()))
			if err != nil {
				return nil, "", err
			}
			tree.Environments = append(tree.Environments, n)
		}
	}
	return tree, string(s.h.Sum(nil)), nil
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

// readDir lists a folder sorted by name; a folder that does not exist is empty.
func readDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.NewErr("HttpDirReadFailed", err).SetMetadata("path", dir)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// note adds a file to the fingerprint.
func (s *scanner) note(kind, name, text string) {
	s.h.Write([]byte(kind + "\x00" + name + "\x00" + text + "\x00"))
}

// read reads a file for the tree; a read that fails is the Err of the node, only a cancelled ctx stops the scan.
func (s *scanner) read(path string) (string, error, error) {
	text, err := format.ReadFile(s.ctx, path)
	if err != nil {
		if s.ctx.Err() != nil {
			return "", nil, err
		}
		s.note("err", path, err.Error())
		return "", err, nil
	}
	s.note("file", path, text)
	return text, nil, nil
}

// group reads a collection or a folder: its descriptor and its entries.
func (s *scanner) group(kind Kind, dir string, depth int) (*Node, error) {
	descriptor := format.FolderFile
	if kind == KindCollection {
		descriptor = format.CollectionFile
	}
	n := &Node{Kind: kind, Path: dir, File: filepath.Join(dir, descriptor), Key: filepath.Base(dir), Name: filepath.Base(dir)}
	text, readErr, err := s.read(n.File)
	if err != nil {
		return nil, err
	}
	switch {
	case readErr != nil && errors.Is(readErr, fs.ErrNotExist):
		n.Group = &format.Collection{Name: n.Name}
	case readErr != nil:
		n.HasFile, n.Err = true, readErr
	default:
		n.HasFile = true
		doc, perr := format.Parse[format.Collection](text)
		n.apply(doc, perr)
		if n.Err == nil || n.ReadOnly {
			n.Group = &doc
			if doc.Name != "" {
				n.Name = doc.Name
			}
		}
	}
	if depth >= MaxDepth {
		return n, nil
	}
	entries, err := readDir(dir)
	if err != nil {
		n.Err = err
		return n, nil
	}
	var order []string
	if n.Group != nil {
		order = n.Group.Order
	}
	byKey := map[string]*Node{}
	var keys []string
	for _, e := range entries {
		name := e.Name()
		if hidden(name) {
			continue
		}
		s.note("entry", filepath.Join(dir, name), "")
		var child *Node
		switch {
		case e.IsDir():
			if child, err = s.group(KindFolder, filepath.Join(dir, name), depth+1); err != nil {
				return nil, err
			}
		case e.Type().IsRegular():
			if _, ok := format.RequestStem(name); !ok {
				continue
			}
			if child, err = s.request(filepath.Join(dir, name)); err != nil {
				return nil, err
			}
		default:
			continue
		}
		byKey[child.Key] = child
		keys = append(keys, child.Key)
	}
	for _, k := range sortKeys(keys, order) {
		n.Children = append(n.Children, byKey[k])
	}
	return n, nil
}

// request reads a request file. A request that names a path outside the workspace is shown with that error.
func (s *scanner) request(path string) (*Node, error) {
	base := filepath.Base(path)
	stem, _ := format.RequestStem(base)
	n := &Node{Kind: KindRequest, Path: path, File: path, HasFile: true, Key: base, Name: stem}
	text, readErr, err := s.read(path)
	if err != nil {
		return nil, err
	}
	if readErr != nil {
		n.Err = readErr
		return n, nil
	}
	doc, perr := format.Parse[format.Request](text)
	n.applyRequest(doc, perr)
	if n.Err == nil {
		if err := format.CheckPaths(s.folder, doc); err != nil {
			n.Err = err
		}
	}
	return n, nil
}

// environment reads a file of environments/.
func (s *scanner) environment(path string) (*Node, error) {
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, format.EnvironmentSuffix)
	n := &Node{Kind: KindEnvironment, Path: path, File: path, HasFile: true, Key: stem, Name: stem}
	text, readErr, err := s.read(path)
	if err != nil {
		return nil, err
	}
	if readErr != nil {
		n.Err = readErr
		return n, nil
	}
	doc, perr := format.Parse[format.Environment](text)
	switch {
	case perr == nil:
		n.Environment = &doc
	case format.IsNewer(perr):
		n.Environment, n.ReadOnly, n.Err = &doc, true, perr
	default:
		n.Err = perr
	}
	if n.Environment != nil && doc.Name != "" {
		n.Name = doc.Name
	}
	return n, nil
}

// apply sets the error and read-only state of a group from the result of Parse.
func (n *Node) apply(doc format.Collection, perr error) {
	switch {
	case perr == nil:
	case format.IsNewer(perr):
		n.ReadOnly, n.Err = true, perr
	default:
		n.Err = perr
	}
}

func (n *Node) applyRequest(doc format.Request, perr error) {
	switch {
	case perr == nil:
		n.Request = &doc
	case format.IsNewer(perr):
		n.Request, n.ReadOnly, n.Err = &doc, true, perr
	default:
		n.Err = perr
	}
	if n.Request != nil && doc.Name != "" {
		n.Name = doc.Name
	}
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
