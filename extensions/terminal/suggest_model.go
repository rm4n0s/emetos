package terminal

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The model of the suggestions (SPEC §10.25 "Terminal IntelliSense"): the items filtered by the word that is being typed, with the
// characters that matched, in VS Code's order.
//
// Ported from VS Code: src/vs/workbench/services/suggest/browser/simpleCompletionModel.ts and
// src/vs/workbench/contrib/terminalContrib/suggest/browser/terminalCompletionModel.ts, terminalCompletionItem.ts

// sugItem is an item that matched the typed word.
type sugItem struct {
	rawItem
	score     int
	positions []int
	// labelLow is the label in lower case; stem and ext the lower-case name of a file without and with its extension.
	labelLow, stem, ext string
	// penalty is 1 for a label made of punctuation or a file that starts with an underscore (VS Code's punctuationPenalty).
	penalty int
}

var punctuationOnly = regexp.MustCompile(`^[\[\]{}().,;:!?/\\\-_@#~*%^=$]+$`)

// filterItems keeps the items the word (the text typed since the item's replace start) matches, with their scores, and sorts them.
// The matcher is reused for all the items.
func filterItems(m *ui.SuggestMatcher, items []rawItem, lc lineContext) []sugItem {
	word := lc.line[min(lc.cur.start, lc.cursor):lc.cursor]
	if lc.quote != 0 {
		word = lc.cur.text
	}
	cmdWord := lc.line[min(lc.cmdStart, lc.cursor):lc.cursor]
	out := make([]sugItem, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		key := it.label + "\x00" + it.insert
		if seen[key] {
			continue
		}
		seen[key] = true
		si := sugItem{rawItem: it, labelLow: strings.ToLower(it.label)}
		if it.replaceCommand {
			// a line of the history: it starts with what was typed
			if !strings.HasPrefix(it.label, cmdWord) {
				continue
			}
			for i := range []rune(cmdWord) {
				si.positions = append(si.positions, i)
			}
		} else {
			score, pos, ok := m.Match(word, it.label)
			if !ok {
				continue
			}
			si.score, si.positions = score, pos
		}
		si.stem = si.labelLow
		if it.kind == kindFile || it.kind == kindSymlinkFile {
			base := path.Base(si.labelLow)
			if i := strings.LastIndexByte(base, '.'); i > 0 {
				si.stem, si.ext = strings.TrimSuffix(si.labelLow, base[i:]), base[i+1:]
			}
		}
		if base := path.Base(strings.TrimSuffix(si.stem, "/")); strings.HasPrefix(base, "_") || punctuationOnly.MatchString(si.stem) {
			si.penalty = 1
		}
		out = append(out, si)
	}
	isArg := strings.Contains(lc.line[min(lc.cmdStart, lc.cursor):lc.cursor], " ")
	git := regexp.MustCompile(`^\s*git\b`).MatchString(lc.line[min(lc.cmdStart, lc.cursor):lc.cursor])
	sort.SliceStable(out, func(i, j int) bool { return compareItems(out[i], out[j], isArg, git) < 0 })
	return out
}

// kindRank orders the kinds that differ: commands and aliases first, then arguments, then the rest, then files and folders, then
// the history.
func kindRank(k itemKind) int {
	switch {
	case k == kindMethod || k == kindAlias:
		return 0
	case k == kindArgument:
		return 1
	case k == kindHistory:
		return 4
	case k.isResource():
		return 3
	}
	return 2
}

// fileExtScores boost the extensions of programs when a file is the command (VS Code's table for Linux).
var fileExtScores = map[string]float64{
	"ps1": 0.05, "bat": -0.05, "cmd": -0.05, "exe": -0.05, "sh": 0.05, "bash": 0.05, "zsh": 0.05, "fish": 0.05,
	"csh": 0.04, "ksh": 0.04, "py": 0.05, "pl": 0.05,
}

// compareItems orders two items: negative when a comes first.
func compareItems(a, b sugItem, isArg, git bool) int {
	if a.score != b.score {
		if a.score > b.score {
			return -1
		}
		return 1
	}
	if a.penalty != b.penalty {
		return a.penalty - b.penalty
	}
	if !isArg && a.kind == kindFile && b.kind == kindFile {
		if a.stem != b.stem {
			return compareAlpha(a.stem, b.stem)
		}
		if d := fileExtScores[b.ext] - fileExtScores[a.ext]; d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
		if len(a.ext) != len(b.ext) {
			return len(a.ext) - len(b.ext)
		}
	}
	if git && a.kind == kindArgument && b.kind == kindArgument {
		am, bm := a.label == "main" || a.label == "master", b.label == "main" || b.label == "master"
		if am && !bm {
			return -1
		}
		if bm && !am {
			return 1
		}
	}
	if a.kind == kindFolder && b.kind == kindFolder {
		pa, pb := strings.TrimSuffix(a.labelLow, "/"), strings.TrimSuffix(b.labelLow, "/")
		if d := strings.Count(pa, "/") - strings.Count(pb, "/"); d != 0 {
			return d
		}
		if strings.HasPrefix(pb, pa) && pa != pb {
			return -1
		}
		if strings.HasPrefix(pa, pb) && pa != pb {
			return 1
		}
	}
	if a.kind != b.kind {
		if d := kindRank(a.kind) - kindRank(b.kind); d != 0 {
			return d
		}
	}
	return compareAlpha(a.labelLow, b.labelLow)
}

// compareAlpha compares two strings ignoring case and punctuation (localeCompare with ignorePunctuation), so that dot files
// are mixed in and not all at the top.
func compareAlpha(a, b string) int {
	ka, kb := alphaKey(a), alphaKey(b)
	if c := strings.Compare(ka, kb); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func alphaKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
