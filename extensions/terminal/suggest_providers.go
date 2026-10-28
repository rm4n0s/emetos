package terminal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The providers of terminal IntelliSense (SPEC §10.25 "Terminal IntelliSense"): the commands found in PATH and the builtins of the
// shell, the files and folders of the shell's folder, the command lines of the history and the subcommands, options and arguments
// of the completion specs. Each provider runs on the goroutine of the request, with a context that stops it, and reads the file
// system only; none runs a program.
//
// Ported from VS Code: extensions/terminal-suggest/src (terminalSuggestMain.ts, env/pathExecutableCache.ts, shell/*.ts) and
// src/vs/workbench/contrib/terminalContrib/suggest/browser/terminalCompletionService.ts (resolveResources)

// itemKind is what a suggestion is, as VS Code's TerminalCompletionItemKind names it. It gives the icon and the order.
type itemKind int

const (
	kindFile itemKind = iota
	kindFolder
	kindMethod
	kindAlias
	kindArgument
	kindOption
	kindOptionValue
	kindFlag
	kindSymlinkFile
	kindSymlinkFolder
	kindHistory
)

// isResource tells whether the kind is a file or a folder.
func (k itemKind) isResource() bool {
	return k == kindFile || k == kindFolder || k == kindSymlinkFile || k == kindSymlinkFolder
}

// rawItem is a suggestion before it is filtered by the typed word.
type rawItem struct {
	// label is what the list shows and, unless insert says otherwise, what is typed in place of the word.
	label  string
	insert string
	kind   itemKind
	// detail is the description at the right of the row, labelDetail the text after the label.
	detail, labelDetail string
	// docs is the longer text of the details pane.
	docs string
	// replaceCommand says that the item replaces the whole command typed so far and not just the word being typed (a line of the
	// history).
	replaceCommand bool
	// provider is the id of the provider that made it (the keys of terminal.integrated.suggest.providers).
	provider string
}

// The ids of the providers (the keys of terminal.integrated.suggest.providers).
const (
	providerCommands = "terminal-suggest"
	providerFiles    = "path"
	providerHistory  = "history"
)

// suggestEnv is what the providers need to know about the terminal and the shell.
type suggestEnv struct {
	// shell is the kind of shell ("bash", "zsh", "fish", "pwsh", "sh"), cwd the folder the shell is in, home the home folder.
	shell, cwd, home string
	// environ is the environment of the terminal (PATH, CDPATH).
	environ []string
	// history are the command lines of the history, the most recent first; loadHistory makes them (it reads files, so the request
	// calls it on its own goroutine).
	history     []string
	loadHistory func() []string
	// cdPath is the setting terminal.integrated.suggest.cdPath ("off", "relative" or "absolute").
	cdPath string
	// providers say which providers are on; a provider that is not named is on.
	providers map[string]bool
}

func (e suggestEnv) enabled(id string) bool {
	on, ok := e.providers[id]
	return !ok || on
}

func (e suggestEnv) get(name string) string {
	for i := len(e.environ) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(e.environ[i], name+"="); ok {
			return v
		}
	}
	return ""
}

// pathIndex remembers the executables of the folders of PATH. A folder is read again when its modification time changes.
type pathIndex struct {
	mu   sync.Mutex
	dirs map[string]*pathDir
}

type pathDir struct {
	mtime   time.Time
	entries []pathEntry
}

// pathEntry is an executable file: its name and where it is.
type pathEntry struct{ name, path string }

func newPathIndex() *pathIndex { return &pathIndex{dirs: map[string]*pathDir{}} }

// maxPathEntries bounds the executables read from one folder.
const maxPathEntries = 20000

// executables lists the commands of the folders in the PATH value, each name once (the first folder wins).
func (p *pathIndex) executables(ctx context.Context, pathValue string) []pathEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []pathEntry
	seen := map[string]bool{}
	for _, dir := range strings.Split(pathValue, string(os.PathListSeparator)) {
		if ctx.Err() != nil {
			return out
		}
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		d := p.dirs[dir]
		if d == nil || !d.mtime.Equal(info.ModTime()) {
			d = &pathDir{mtime: info.ModTime(), entries: readExecutables(ctx, dir)}
			if ctx.Err() != nil {
				return out // a partial read is not kept
			}
			p.dirs[dir] = d
		}
		for _, e := range d.entries {
			if !seen[e.name] {
				seen[e.name] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// readExecutables lists the files of a folder that can be run.
func readExecutables(ctx context.Context, dir string) []pathEntry {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []pathEntry
	for i, de := range des {
		if i >= maxPathEntries || ctx.Err() != nil {
			break
		}
		full := filepath.Join(dir, de.Name())
		if isExecutable(de, full) {
			out = append(out, pathEntry{name: de.Name(), path: full})
		}
	}
	return out
}

// isExecutable tells whether a directory entry is a file (or a link to one) that someone may run.
func isExecutable(de os.DirEntry, full string) bool {
	info, err := de.Info()
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if info, err = os.Stat(full); err != nil {
			return false
		}
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// commandSuggestions are the commands that can be typed as the first word: the builtins of the shell and the executables of PATH,
// with the description of a spec when the command has one.
func (s *suggester) commandSuggestions(ctx context.Context, env suggestEnv) []rawItem {
	var items []rawItem
	have := map[string]bool{}
	builtins, err := builtinsOf(s.specs.data, env.shell)
	if err != nil {
		s.log.Warn("the builtins of the shell cannot be read", "shell", env.shell, "err", err)
	}
	for _, b := range builtins {
		have[b.Name] = true
		items = append(items, rawItem{label: b.Name, kind: kindMethod, detail: b.Description, labelDetail: argsDetail(b.Args, b.Name), provider: providerCommands})
	}
	for _, e := range s.path.executables(ctx, env.get("PATH")) {
		if have[e.name] {
			continue
		}
		have[e.name] = true
		it := rawItem{label: e.name, kind: kindMethod, detail: e.path, provider: providerCommands}
		if spec, err := s.specs.get(e.name); err != nil {
			s.log.Warn("a completion spec cannot be read", "command", e.name, "err", err)
		} else if spec != nil && spec.Description != "" {
			it.detail = spec.Description
			it.docs = e.path
		}
		items = append(items, it)
	}
	return items
}

// argsDetail is the usage a builtin's description cache gives, less the name of the command: "cd [-L] dir" is " [-L] dir".
func argsDetail(args, name string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), name))
	if rest == "" || rest == strings.TrimSpace(args) && !strings.HasPrefix(strings.TrimSpace(args), name) {
		return ""
	}
	return " " + rest
}

// hasCommand tells whether the shell can run the command: a builtin, or an executable of PATH.
func (s *suggester) hasCommand(ctx context.Context, env suggestEnv, name string) bool {
	if builtins, err := builtinsOf(s.specs.data, env.shell); err == nil {
		for _, b := range builtins {
			if b.Name == name {
				return true
			}
		}
	}
	for _, e := range s.path.executables(ctx, env.get("PATH")) {
		if e.name == name {
			return true
		}
	}
	return false
}

// historySuggestions are the command lines of the history that start with what was typed of the command (VS Code's inline
// suggestions of the shell, here made from the history Emetos keeps).
func historySuggestions(env suggestEnv, lc lineContext) []rawItem {
	typed := lc.line[lc.cmdStart:lc.cursor]
	if strings.TrimSpace(typed) == "" || lc.cmdStart != 0 {
		return nil
	}
	var out []rawItem
	seen := map[string]bool{}
	for _, h := range env.history {
		if len(out) >= 10 {
			break
		}
		if seen[h] || h == typed || !strings.HasPrefix(h, typed) || strings.Contains(h, "\n") {
			continue
		}
		seen[h] = true
		out = append(out, rawItem{label: h, kind: kindHistory, detail: "history", replaceCommand: true, provider: providerHistory})
	}
	return out
}

// ---- files and folders ----

// maxDirEntries bounds the entries read from the folder being completed.
const maxDirEntries = 5000

// fileSuggestions are the files and folders to offer for the word being typed (VS Code's resolveResources): the folder the word
// names (the shell's folder when the word has no slash), the folder itself as ".", its parent, "~", and for cd the folders of CDPATH.
// onlyExecutables leaves out the files that cannot be run (the word is the command).
func fileSuggestions(ctx context.Context, env suggestEnv, lc lineContext, showFiles, showFolders, onlyExecutables bool) []rawItem {
	showFolders = showFolders || showFiles
	if !showFolders {
		return nil
	}
	word := lc.cur.text
	slash := strings.LastIndexByte(word, '/')
	folderText := ""
	if slash >= 0 {
		folderText = word[:slash+1]
	}
	// the text as it was typed, for the labels: the prefix up to the last slash keeps its own escapes and quotes
	raw := lc.line[lc.cur.start:lc.cursor]
	rawFolder := ""
	if i := strings.LastIndexByte(raw, '/'); i >= 0 {
		rawFolder = raw[:i+1]
	}
	var dir string
	kindOfPath := "relative"
	switch {
	case strings.HasPrefix(folderText, "~"):
		kindOfPath = "tilde"
		if env.home == "" {
			return nil
		}
		dir = filepath.Join(env.home, folderText[1:])
	case strings.HasPrefix(folderText, "/"):
		kindOfPath = "absolute"
		dir = folderText
	default:
		if env.cwd == "" {
			return nil
		}
		dir = filepath.Join(env.cwd, folderText)
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	friendly := func(p string, folder bool) string {
		p = filepath.Clean(p)
		if folder && !strings.HasSuffix(p, "/") {
			p += "/"
		}
		return p
	}
	prefix := func(label string) string {
		if kindOfPath == "relative" && !strings.HasPrefix(rawFolder, "./") && !strings.HasPrefix(rawFolder, "../") {
			if strings.HasPrefix(label, "/") {
				return "." + label
			}
			return "./" + label
		}
		return label
	}
	var out []rawItem
	add := func(label string, kind itemKind, detail string) {
		out = append(out, rawItem{label: label, kind: kind, detail: detail, provider: providerFiles})
	}
	// the folder itself comes first: an exact match highlights its detail
	switch kindOfPath {
	case "relative":
		label := "."
		if rawFolder != "" {
			label = prefix(rawFolder)
		}
		add(label, kindFolder, friendly(dir, true))
	default:
		add(rawFolder, kindFolder, friendly(dir, true))
	}
	for i, de := range des {
		if i >= maxDirEntries || ctx.Err() != nil {
			break
		}
		full := filepath.Join(dir, de.Name())
		isLink := de.Type()&os.ModeSymlink != 0
		isDir, isFile := de.IsDir(), de.Type().IsRegular()
		if isLink {
			if st, err := os.Stat(full); err == nil {
				isDir, isFile = st.IsDir(), st.Mode().IsRegular()
			}
		}
		var kind itemKind
		switch {
		case isDir:
			kind = kindFolder
			if isLink {
				kind = kindSymlinkFolder
			}
		case isFile && showFiles:
			if onlyExecutables && !isExecutable(de, full) {
				continue
			}
			kind = kindFile
			if isLink {
				kind = kindSymlinkFile
			}
		default:
			continue
		}
		name := escapeName(de.Name(), lc.quote)
		if isDir {
			name += "/"
		}
		detail := friendly(full, isDir)
		if isLink {
			if target, err := filepath.EvalSymlinks(full); err == nil && target != full {
				detail += " -> " + friendly(target, isDir)
			}
		}
		add(prefix(rawFolder+name), kind, detail)
	}
	// $CDPATH, for cd only
	if kindOfPath == "relative" && !lc.command && len(lc.words) == 1 && lc.words[0].text == "cd" && (env.cdPath == "relative" || env.cdPath == "absolute") {
		for _, entry := range filepath.SplitList(env.get("CDPATH")) {
			if entry == "" {
				continue
			}
			base := entry
			if !filepath.IsAbs(base) {
				base = filepath.Join(env.cwd, base)
			}
			children, err := os.ReadDir(base)
			if err != nil {
				continue
			}
			for i, c := range children {
				if i >= maxDirEntries || ctx.Err() != nil {
					break
				}
				full := filepath.Join(base, c.Name())
				st, err := os.Stat(full)
				if err != nil || !st.IsDir() {
					continue
				}
				if env.cdPath == "relative" {
					add(escapeName(c.Name(), lc.quote)+"/", kindFolder, "CDPATH "+friendly(full, true))
				} else {
					add(friendly(full, true), kindFolder, "CDPATH")
				}
			}
		}
	}
	// the parent folder is at the bottom: it is not as useful as the others
	if kindOfPath == "relative" {
		parent := "../"
		if rawFolder != "" {
			parent = prefix(rawFolder + "../")
		}
		add(parent, kindFolder, friendly(filepath.Join(dir, ".."), true))
		if !strings.Contains(word, "/") && env.home != "" {
			add("~", kindFolder, friendly(env.home, true))
		}
	}
	return out
}

// escapeName escapes a file name for the shell unless the word is inside quotes.
func escapeName(name string, quote byte) string {
	if quote != 0 {
		return name
	}
	return escapeLabel(name)
}
