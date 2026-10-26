package agents

import (
	"sort"
	"strings"
)

// maxCompletions is how many commands or files the list under the input shows.
const maxCompletions = 8

// completionItem is one choice of the completion list.
type completionItem struct {
	// Label is what the row shows ("/compact", "main.go"); Detail follows it, dim.
	Label, Detail string
	// Insert replaces the typed part of the input when the item is accepted.
	Insert string
}

// completion is the list under the input: the commands after "/" and the files after "@".
type completion struct {
	items []completionItem
	// sel is the row that Enter accepts.
	sel int
	// start and end are the bytes of the input that an accepted item replaces.
	start, end int
}

// inputChanged follows the input: it works out the completion list for the text before the caret.
func (t *sessionTab) inputChanged() {
	text := t.input.Value()
	caret := len(text)
	if _, end, ok := t.input.Selection(); ok && end <= len(text) {
		caret = end
	}
	t.comp = t.completionFor(text[:caret])
	t.comp.end = caret
	t.tab.Refresh()
}

// completionFor works out the list for the text before the caret. A "/" at the start of the input, with no space after it,
// lists the commands of the program; an "@" that starts a word lists files of the folder.
func (t *sessionTab) completionFor(before string) completion {
	if rest, ok := strings.CutPrefix(before, "/"); ok && !strings.ContainsAny(rest, " \t\n") {
		return completion{items: t.commandItems(rest), start: 0}
	}
	at := strings.LastIndex(before, "@")
	if at < 0 || (at > 0 && !strings.ContainsAny(before[at-1:at], " \t\n")) {
		return completion{}
	}
	query := before[at+1:]
	if strings.ContainsAny(query, " \t\n") {
		return completion{}
	}
	return completion{items: t.fileItems(query), start: at}
}

// commandItems lists the commands whose name starts with the typed text, then those that contain it.
func (t *sessionTab) commandItems(typed string) []completionItem {
	typed = strings.ToLower(typed)
	var starts, contains []completionItem
	for _, c := range t.commands {
		name := strings.ToLower(c.Name)
		item := completionItem{Label: "/" + c.Name, Detail: strings.TrimSpace(c.Hint + " " + c.Description), Insert: "/" + c.Name + " "}
		switch {
		case strings.HasPrefix(name, typed):
			starts = append(starts, item)
		case strings.Contains(name, typed):
			contains = append(contains, item)
		}
	}
	return limit(append(starts, contains...))
}

// fileItems lists files of the folder for the typed text: the best matches by name, then by path.
func (t *sessionTab) fileItems(typed string) []completionItem {
	list, err := t.ext.api.Workspace().Files()
	if err != nil {
		return nil
	}
	typed = strings.ToLower(typed)
	type match struct {
		path  string
		score int
	}
	var matches []match
	for _, f := range list.Files {
		lower := strings.ToLower(f)
		base := lower[strings.LastIndex(lower, "/")+1:]
		score := -1
		switch {
		case typed == "":
			score = 0
		case strings.HasPrefix(base, typed):
			score = 0
		case strings.HasPrefix(lower, typed):
			score = 1
		case strings.Contains(base, typed):
			score = 2
		case strings.Contains(lower, typed):
			score = 3
		}
		if score >= 0 {
			matches = append(matches, match{f, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return len(matches[i].path) < len(matches[j].path)
	})
	items := make([]completionItem, 0, maxCompletions)
	for _, m := range matches {
		name := m.path[strings.LastIndex(m.path, "/")+1:]
		detail := ""
		if name != m.path {
			detail = strings.TrimSuffix(m.path, name)
		}
		items = append(items, completionItem{Label: name, Detail: detail, Insert: "@" + m.path + " "})
		if len(items) == maxCompletions {
			break
		}
	}
	return items
}

// limit cuts a list to the rows the completion shows.
func limit(items []completionItem) []completionItem {
	if len(items) > maxCompletions {
		return items[:maxCompletions]
	}
	return items
}

// acceptCompletion replaces the typed part of the input with the chosen item.
func (t *sessionTab) acceptCompletion() {
	c := t.comp
	if c.sel < 0 || c.sel >= len(c.items) {
		return
	}
	text := t.input.Value()
	if c.start > len(text) || c.end > len(text) || c.start > c.end {
		t.comp = completion{}
		return
	}
	insert := c.items[c.sel].Insert
	t.comp = completion{}
	t.input.SetValue(text[:c.start] + insert + text[c.end:])
	pos := c.start + len(insert)
	t.input.Select(pos, pos)
}
