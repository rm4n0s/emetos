package terminal

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
)

// The completion specs (SPEC §10.25 "Terminal IntelliSense"): the subcommands, options and arguments of commands such as git,
// npm and ls, as VS Code's terminal-suggest extension has them (MIT, from withfig/autocomplete), converted to JSON by
// scripts/specs2json from scripts/update-assets.sh. A spec's generator, which would run a shell command, is not part of the data
// (D117). This file reads the JSON and walks a command line through a spec.

//go:embed suggestdata/specs suggestdata/builtins
var suggestData embed.FS

// specNames is one name or several; the JSON has a string or an array.
type specNames []string

// UnmarshalJSON reads a string or an array of strings.
func (n *specNames) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*n = specNames{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err // the caller wraps it with the tag of the file
	}
	*n = many
	return nil
}

// specNode is a command or a subcommand of a spec.
type specNode struct {
	Name        specNames   `json:"name"`
	Description string      `json:"description"`
	Subcommands []*specNode `json:"subcommands"`
	Options     []*specOpt  `json:"options"`
	Args        []*specArg  `json:"args"`
	Hidden      bool        `json:"hidden"`
	Deprecated  bool        `json:"deprecated"`
}

// specOpt is an option of a command.
type specOpt struct {
	Name              specNames  `json:"name"`
	Description       string     `json:"description"`
	Args              []*specArg `json:"args"`
	Persistent        bool       `json:"persistent"`
	Repeatable        bool       `json:"repeatable"`
	RequiresSeparator bool       `json:"requiresSeparator"`
	Hidden            bool       `json:"hidden"`
	Deprecated        bool       `json:"deprecated"`
}

// specArg is an argument of a command or an option.
type specArg struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Optional    bool            `json:"optional"`
	Variadic    bool            `json:"variadic"`
	Command     bool            `json:"command"`
	Templates   []string        `json:"templates"`
	Suggestions []specSuggested `json:"suggestions"`
}

// specSuggested is a value an argument may take.
type specSuggested struct {
	Name        specNames `json:"name"`
	Description string    `json:"description"`
	Hidden      bool      `json:"hidden"`
}

// specStore loads the specs when they are asked for and keeps them. It belongs to the extension of a window tab, and is used by
// the goroutines that compute suggestions, so it has a lock.
type specStore struct {
	// data is the folder of the data: specs/<command>.json and builtins/<shell>.json.
	data   fs.FS
	mu     sync.Mutex
	loaded map[string]*specNode
}

func newSpecStore(data fs.FS) *specStore {
	return &specStore{data: data, loaded: map[string]*specNode{}}
}

// bundledData is the data that is part of the program, rooted at its folder.
func bundledData() fs.FS {
	sub, err := fs.Sub(suggestData, "suggestdata")
	if err != nil {
		return suggestData // not reached: the folder is embedded
	}
	return sub
}

// get is the spec of a command, nil when there is none. A spec that cannot be read is logged by the caller through the error.
func (s *specStore) get(command string) (*specNode, error) {
	command = path.Base(command)
	if command == "" || strings.Contains(command, "\\") || command == "." || command == ".." {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.loaded[command]; ok {
		return n, nil
	}
	data, err := fs.ReadFile(s.data, "specs/"+command+".json")
	if err != nil {
		s.loaded[command] = nil // there is no spec of that name
		return nil, nil
	}
	var n specNode
	if err := json.Unmarshal(data, &n); err != nil {
		s.loaded[command] = nil
		return nil, errors.NewErr("TerminalSpecInvalid", err).SetMetadata("command", command)
	}
	s.loaded[command] = &n
	return &n, nil
}

// builtinCmd is a command a shell has in itself.
type builtinCmd struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Args        string `json:"args"`
}

// builtinsOf reads the builtins of a shell ("bash", "zsh" or "fish"); none for another shell.
func builtinsOf(fsys fs.FS, shell string) ([]builtinCmd, error) {
	switch shell {
	case "bash", "zsh", "fish":
	default:
		return nil, nil
	}
	data, err := fs.ReadFile(fsys, "builtins/"+shell+".json")
	if err != nil {
		return nil, errors.NewErr("TerminalBuiltinsMissing", err).SetMetadata("shell", shell)
	}
	var list []builtinCmd
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, errors.NewErr("TerminalBuiltinsInvalid", err).SetMetadata("shell", shell)
	}
	return list, nil
}

// ---- walking a command line through a spec ----

// specResult is what a spec offers at the cursor.
type specResult struct {
	items []rawItem
	// showFiles and showFolders say that the files and the folders of the shell's folder are to be offered too; hasArg that the
	// cursor is where an argument of the spec goes; wantCommand that the argument is itself a command (sudo, xargs).
	showFiles, showFolders, hasArg, wantCommand bool
}

// walkSpec walks the words of a command line (after the command's own name) through the spec and returns what can be typed at
// the cursor, where cur is the word being typed.
func walkSpec(spec *specNode, words []string, cur string) specResult {
	w := specWalker{node: spec, passed: map[string]bool{}}
	for _, word := range words {
		w.feed(word)
	}
	return w.suggest(cur)
}

// specWalker is the position in a spec after some words.
type specWalker struct {
	node       *specNode
	persistent []*specOpt
	// passed are the names of the options already on the line.
	passed map[string]bool
	// argIdx is how many arguments of the node were given; entered that one was.
	argIdx  int
	entered bool
	// pending is an option waiting for its arguments, and pendingIdx the next of them.
	pending    *specOpt
	pendingIdx int
	// endOpts says that "--" was typed: what follows are arguments.
	endOpts bool
}

// optionNamed finds the option of the node or an ancestor that has the name.
func (w *specWalker) optionNamed(name string) *specOpt {
	for _, o := range w.node.Options {
		for _, n := range o.Name {
			if n == name {
				return o
			}
		}
	}
	for _, o := range w.persistent {
		for _, n := range o.Name {
			if n == name {
				return o
			}
		}
	}
	return nil
}

// requiredArgs is the number of arguments of an option that must follow it.
func requiredArgs(o *specOpt) int {
	n := 0
	for _, a := range o.Args {
		if !a.Optional {
			n++
		}
	}
	return n
}

// feed takes one finished word.
func (w *specWalker) feed(word string) {
	if w.pending != nil {
		w.pendingIdx++
		if w.pendingIdx >= requiredArgs(w.pending) {
			w.pending = nil
		}
		return
	}
	if !w.endOpts {
		if word == "--" {
			w.endOpts = true
			return
		}
		if strings.HasPrefix(word, "-") && word != "-" {
			w.option(word)
			return
		}
	}
	if !w.entered {
		if sub := w.subcommandNamed(word); sub != nil {
			for _, o := range w.node.Options {
				if o.Persistent {
					w.persistent = append(w.persistent, o)
				}
			}
			w.node, w.argIdx, w.passed = sub, 0, map[string]bool{}
			return
		}
	}
	w.argIdx++
	w.entered = true
}

// option takes a word that starts with a dash.
func (w *specWalker) option(word string) {
	name, _, hasValue := strings.Cut(word, "=")
	if o := w.optionNamed(name); o != nil {
		w.mark(o)
		if !hasValue && requiredArgs(o) > 0 {
			w.pending, w.pendingIdx = o, 0
		}
		return
	}
	// several short options together: -abc
	if !strings.HasPrefix(word, "--") && len(word) > 2 {
		var last *specOpt
		for _, r := range word[1:] {
			o := w.optionNamed("-" + string(r))
			if o == nil {
				return // not a group of options
			}
			w.mark(o)
			last = o
		}
		if last != nil && requiredArgs(last) > 0 {
			w.pending, w.pendingIdx = last, 0
		}
	}
}

func (w *specWalker) mark(o *specOpt) {
	if o.Repeatable {
		return
	}
	for _, n := range o.Name {
		w.passed[n] = true
	}
}

func (w *specWalker) subcommandNamed(word string) *specNode {
	for _, s := range w.node.Subcommands {
		for _, n := range s.Name {
			if n == word {
				return s
			}
		}
	}
	return nil
}

// currentArg is the argument of the node the cursor is at, nil when there is none.
func (w *specWalker) currentArg() *specArg {
	args := w.node.Args
	if len(args) == 0 {
		return nil
	}
	if w.argIdx < len(args) {
		return args[w.argIdx]
	}
	if last := args[len(args)-1]; last.Variadic {
		return last
	}
	return nil
}

// suggest lists what can be typed in place of cur.
func (w *specWalker) suggest(cur string) specResult {
	var r specResult
	if w.pending != nil {
		// the value of an option
		if w.pendingIdx < len(w.pending.Args) {
			w.addArg(&r, w.pending.Args[w.pendingIdx], kindOptionValue)
		}
		return r
	}
	if !w.endOpts && strings.HasPrefix(cur, "-") {
		w.addOptions(&r)
		return r
	}
	if arg := w.currentArg(); arg != nil {
		w.addArg(&r, arg, kindArgument)
	}
	if !w.entered && !w.endOpts {
		for _, s := range w.node.Subcommands {
			if s.Hidden || s.Deprecated {
				continue
			}
			for _, n := range s.Name {
				r.items = append(r.items, rawItem{label: n, kind: kindMethod, detail: s.Description})
			}
		}
	}
	if len(r.items) == 0 && !r.showFiles && !r.showFolders && !w.endOpts {
		w.addOptions(&r)
	}
	return r
}

// addOptions adds the options of the node and of its ancestors that are persistent, less the ones already on the line.
func (w *specWalker) addOptions(r *specResult) {
	add := func(o *specOpt) {
		if o.Hidden || o.Deprecated {
			return
		}
		for _, n := range o.Name {
			if w.passed[n] {
				return
			}
		}
		var detail string
		if len(o.Args) > 0 {
			var parts []string
			for _, a := range o.Args {
				if a.Name == "" {
					parts = nil
					break
				}
				p := "<" + a.Name + ">"
				if a.Optional {
					p = "[" + p + "]"
				}
				parts = append(parts, p)
			}
			if len(parts) > 0 {
				detail = " " + strings.Join(parts, " ")
			}
		}
		for _, n := range o.Name {
			r.items = append(r.items, rawItem{label: n, kind: kindFlag, detail: o.Description, labelDetail: detail})
		}
	}
	for _, o := range w.node.Options {
		add(o)
	}
	for _, o := range w.persistent {
		add(o)
	}
}

// addArg adds the values an argument suggests and what its templates ask for.
func (w *specWalker) addArg(r *specResult, a *specArg, kind itemKind) {
	if a.Command {
		r.wantCommand = true
	}
	for _, s := range a.Suggestions {
		if s.Hidden {
			continue
		}
		for _, n := range s.Name {
			if n == "" {
				continue
			}
			r.items = append(r.items, rawItem{label: n, kind: kind, detail: s.Description})
		}
	}
	for _, t := range a.Templates {
		switch t {
		case "filepaths":
			r.showFiles, r.showFolders = true, true
		case "folders":
			r.showFolders = true
		}
	}
	if len(a.Templates) > 0 || len(a.Suggestions) > 0 || a.Command {
		r.hasArg = true
	}
}
