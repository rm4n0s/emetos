package containerui

import (
	"context"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Explore Files (SPEC §10.20 "Container actions"): the files of a running container are a read-only tree under it, listed with
// `ls` and read with `cat` through exec, one folder at a time and only when it is opened; a file opens in a read-only tab. The
// archive endpoint is not used: exec is on both engines and needs nothing from the container but `ls` and `cat`.

// maxFileSize is the most bytes of a file that is read into a tab.
const maxFileSize = 1 << 20

// fileEntry is a name in a folder of a container.
type fileEntry struct {
	name string
	dir  bool
}

// fileCache keeps the folders that were listed, by the key of the node, so that a refresh of the containers does not read them
// again.
type fileCache struct {
	dirs map[string][]fileEntry
}

func newFileCache() *fileCache { return &fileCache{dirs: map[string][]fileEntry{}} }

func (c *fileCache) get(key string) ([]fileEntry, bool) {
	es, ok := c.dirs[key]
	return es, ok
}

func (c *fileCache) set(key string, es []fileEntry) { c.dirs[key] = es }

// forget drops everything that was listed.
func (c *fileCache) forget() { c.dirs = map[string][]fileEntry{} }

// keepRunning drops the folders of containers that do not run any more (or are gone).
func (c *fileCache) keepRunning(running map[string]bool) {
	for key := range c.dirs {
		if cid, _, _ := parseFileKey(key); !running[cid] {
			delete(c.dirs, key)
		}
	}
}

// rootKey is the key of the node of a container, which also stands for its root folder.
func rootKey(cid string) string { return keyContainer + cid }

// fileKey is the key of the node of a file or folder.
func fileKey(cid, p string) string { return keyFile + cid + ":" + p }

// parseFileKey splits the key of a container or of a file node into the container and the path; dir is true for a container and
// for a path that ends in a slash (the nodes of folders are written with one).
func parseFileKey(key string) (cid, p string, dir bool) {
	if rest, ok := strings.CutPrefix(key, keyContainer); ok {
		return rest, "/", true
	}
	rest := strings.TrimPrefix(key, keyFile)
	cid, p, _ = strings.Cut(rest, ":")
	return cid, p, strings.HasSuffix(p, "/")
}

// fileItems makes the nodes of the entries of a folder.
func (e *ext) fileItems(cid, dir string, entries []fileEntry) []ui.TreeItem {
	items := make([]ui.TreeItem, 0, len(entries))
	for _, en := range entries {
		p := path.Join(dir, en.name)
		if en.dir {
			p += "/"
		}
		it := ui.TreeItem{Key: fileKey(cid, p), Data: fileNode{container: cid, path: p, dir: en.dir}, Label: en.name}
		if en.dir {
			if sub, ok := e.files.get(fileKey(cid, p)); ok {
				it.Children = e.fileItems(cid, p, sub)
				it.HasChildren = true
			} else {
				it.Lazy = true
			}
		}
		items = append(items, it)
	}
	return items
}

// parseListing reads the output of `ls -1Ap`: one name a line, a folder with a slash at its end. Folders come first.
func parseListing(out string) []fileEntry {
	var es []fileEntry
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" || l == "./" || l == "../" {
			continue
		}
		dir := strings.HasSuffix(l, "/")
		es = append(es, fileEntry{name: strings.TrimSuffix(l, "/"), dir: dir})
	}
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].dir != es[j].dir {
			return es[i].dir
		}
		return es[i].name < es[j].name
	})
	return es
}

// loadFiles lists the folder a lazy node stands for, on a goroutine, and gives the node its children.
func (e *ext) loadFiles(key string) {
	v := e.views[secContainers]
	c, ok := e.client, e.client != nil
	if !ok || v == nil {
		return
	}
	cid, p, _ := parseFileKey(key)
	name := e.nameOf(cid)
	gen := e.gen
	go func() {
		res, err := c.Exec(e.ctx, cid, containerapi.ExecOptions{Cmd: []string{"ls", "-1Ap", "--", p}, MaxOutput: maxFileSize})
		failure := ""
		switch {
		case err != nil:
			failure = reasonOf(err)
		case res.ExitCode != 0:
			failure = strings.TrimSpace(res.Stderr)
			if failure == "" {
				failure = "ls ended with status " + strconv.Itoa(res.ExitCode)
			}
		}
		e.api.Post(func() {
			if gen != e.gen {
				return
			}
			if failure != "" {
				v.tree.FailLoad(key)
				e.api.Notifications().Warn("Cannot list " + p + " in " + name + ": " + failure)
				return
			}
			entries := parseListing(res.Stdout)
			e.files.set(key, entries)
			if err := v.tree.SetChildren(key, e.fileItems(cid, p, entries)); err != nil {
				e.api.Log().Warn("the files cannot be shown", "path", p, "err", err)
			}
			v.handle.Refresh()
		})
	}()
}

// exploreFiles shows the files of a running container: its node is selected and opened.
func (e *ext) exploreFiles(cid string) {
	v := e.views[secContainers]
	c, ok := e.containerByID(cid)
	if v == nil || !ok {
		return
	}
	if c.State != "running" {
		e.api.Notifications().Info("The files of " + c.Name() + " can be explored while it runs. Start it first.")
		return
	}
	key := rootKey(cid)
	v.handle.Reveal(true)
	v.tree.Reveal(key)
	v.tree.SetSelected(key)
	v.tree.SetFocus(key)
	v.tree.Expand(key)
	v.ctl.Reveal(key)
	v.handle.Refresh()
}

// openFile reads a file of a container and shows it in a read-only tab. A binary file and one over maxFileSize are not shown whole.
func (e *ext) openFile(cid, p string) {
	c, ok := e.ready()
	if !ok {
		return
	}
	name := e.nameOf(cid)
	langName := strings.TrimPrefix(path.Ext(p), ".")
	go func() {
		res, err := c.Exec(e.ctx, cid, containerapi.ExecOptions{Cmd: []string{"cat", "--", p}, MaxOutput: maxFileSize})
		e.api.Post(func() {
			switch {
			case err != nil:
				e.api.Notifications().Error("Cannot read " + p + " in " + name + ": " + reasonOf(err))
			case res.ExitCode != 0:
				msg := strings.TrimSpace(res.Stderr)
				if msg == "" {
					msg = "cat ended with status " + strconv.Itoa(res.ExitCode)
				}
				e.api.Notifications().Error("Cannot read " + p + " in " + name + ": " + msg)
			case strings.IndexByte(res.Stdout, 0) >= 0 || !utf8.ValidString(res.Stdout):
				e.api.Notifications().Info(p + " in " + name + " is not a text file, so it is not shown.")
			default:
				body := res.Stdout
				if res.Truncated {
					body += "\n[Only the first " + humanSize(maxFileSize) + " are shown.]\n"
				}
				e.docs.openLanguage(path.Base(p)+" ("+name+")", name+":"+p, body, langName)
			}
		})
	}()
}

var _ context.Context
