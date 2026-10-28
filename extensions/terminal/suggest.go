package terminal

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
)

// Terminal IntelliSense (SPEC §10.25 "Terminal IntelliSense"). This file is the part that computes the suggestions for a command
// line; suggest_input.go reads the line from the terminal, and suggest_pane.go shows the list and takes the keys.

// suggester computes suggestions. It belongs to the extension of a window tab and is used by the goroutines of the requests, so
// what it keeps (the specs, the executables of PATH) has locks.
type suggester struct {
	log   *slog.Logger
	specs *specStore
	path  *pathIndex
}

func newSuggester(log *slog.Logger) *suggester {
	return &suggester{log: log, specs: newSpecStore(bundledData()), path: newPathIndex()}
}

// quickSettings say when the list opens by itself (terminal.integrated.suggest.quickSuggestions).
type quickSettings struct {
	commands, arguments, unknown bool
}

// suggestResult are the suggestions for a line.
type suggestResult struct {
	items []rawItem
	// fallback says that the files were offered only because nothing else was known about the word.
	fallback bool
}

// complete computes the suggestions for the word at the cursor: the commands when it is the first word of a command, else what
// the spec of the command offers, the files and folders its arguments are, and the history. The files that are only a fallback
// (the command is not known and has no spec) are left out of an automatic request unless quick.unknown says so.
func (s *suggester) complete(ctx context.Context, env suggestEnv, lc lineContext, explicit bool, quick quickSettings) suggestResult {
	var res suggestResult
	showFiles, showFolders, onlyExec := false, false, false
	switch {
	case lc.command:
		if env.enabled(providerCommands) {
			res.items = append(res.items, s.commandSuggestions(ctx, env)...)
		}
		showFiles, showFolders, onlyExec = true, true, true
	case lc.redirect || len(lc.words) == 0:
		showFiles, showFolders = true, true
	default:
		var spec specResult
		name := lc.words[0].text
		if env.enabled(providerCommands) && s.hasCommand(ctx, env, filepath.Base(name)) {
			node, err := s.specs.get(name)
			if err != nil {
				s.log.Warn("a completion spec cannot be read", "command", name, "err", err)
			}
			if node != nil {
				texts := make([]string, 0, len(lc.words)-1)
				for _, w := range lc.words[1:] {
					texts = append(texts, w.text)
				}
				spec = walkSpec(node, texts, lc.cur.text)
				for i := range spec.items {
					spec.items[i].provider = providerCommands
				}
			}
		}
		res.items = append(res.items, spec.items...)
		showFiles, showFolders = spec.showFiles, spec.showFolders
		if spec.wantCommand && env.enabled(providerCommands) {
			res.items = append(res.items, s.commandSuggestions(ctx, env)...)
			showFiles, showFolders, onlyExec = true, true, true
		}
		if len(spec.items) == 0 && !showFiles && !showFolders && !spec.hasArg {
			showFiles, showFolders = true, true
			res.fallback = true
		}
	}
	if (showFiles || showFolders) && env.enabled(providerFiles) {
		if !res.fallback || explicit || quick.unknown {
			res.items = append(res.items, fileSuggestions(ctx, env, lc, showFiles, showFolders, onlyExec)...)
		}
	}
	if env.enabled(providerHistory) {
		res.items = append(res.items, historySuggestions(env, lc)...)
	}
	return res
}

// acceptText is what is typed to the shell to accept an item: when the word already typed is the start of the item, the rest
// of it; else Backspaces for what was typed and the whole item. A trailing space follows when addSpace is on and the item is not
// a folder.
func acceptText(it sugItem, lc lineContext, addSpace bool) string {
	insert := it.insert
	if insert == "" {
		insert = it.label
	}
	start := lc.cur.start
	typed := lc.line[min(start, lc.cursor):lc.cursor]
	if it.replaceCommand {
		start = lc.cmdStart
		typed = lc.line[min(start, lc.cursor):lc.cursor]
	}
	var out string
	if lc.quote != 0 && !it.replaceCommand {
		// inside an open quote: what was typed after the quote is the start of the name
		typed = lc.cur.text
	}
	if strings.HasPrefix(insert, typed) {
		out = insert[len(typed):]
	} else {
		out = strings.Repeat("\x7f", len([]rune(typed))) + insert
	}
	if addSpace && !strings.HasSuffix(insert, "/") {
		out += " "
	}
	return out
}
