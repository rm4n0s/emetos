package search

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// inputs are the four boxes of the view, the toggles, and which of them has the keyboard.
type inputs struct {
	searchBox, replaceBox, includeBox, excludeBox *ui.InputBoxController
	history, replaceHistory                       *ui.History

	caseSensitive, wholeWord, regex, preserveCase bool
	replaceOpen, detailsOpen, onlyOpen            bool
	useExcludes                                   bool // "Use Exclude Settings and Ignore Files", on by default

	// focus says where the keyboard is: the view as a whole, each box, and the list of results.
	focus struct{ view, search, replace, include, exclude, list bool }
	// wantFocus is the box that gets the keyboard when the view is built next ("search", "replace" or "list").
	wantFocus string
	// queryInvalid is true while the regular expression does not compile; the message is in status.err.
	queryInvalid bool
	// replacer makes the replacement of a match; nil until it is needed, and after the query or the replace text changes.
	replacer extension.TextReplacer
	// replacerFor is the replace text and flags replacer was made for.
	replacerFor string
}

func (v *view) initInputs() {
	v.searchBox = ui.NewInputBoxController("")
	v.replaceBox = ui.NewInputBoxController("")
	v.includeBox = ui.NewInputBoxController("")
	v.excludeBox = ui.NewInputBoxController("")
	v.history = ui.NewHistory(nil)
	v.replaceHistory = ui.NewHistory(nil)
	v.useExcludes = true
}

// setFlag sets a toggle of the search box and searches again.
func (v *view) setFlag(flag *bool, on bool) {
	*flag = on
	v.replacer = nil
	if v.searchBox.Value() != "" && flag != &v.preserveCase {
		v.queryChanged(true)
	}
	v.updateKeys()
	v.refreshView()
}

// toggleDetails shows or hides the boxes "files to include" and "files to exclude".
func (v *view) toggleDetails() {
	v.detailsOpen = !v.detailsOpen
	v.refreshView()
}

// closeReplace hides the replace box and gives the keyboard back to the search box.
func (v *view) closeReplace() {
	v.replaceOpen = false
	v.replacer = nil
	v.wantFocus = "search"
	v.updateKeys()
	v.refreshView()
}

// toggleSearchOnType flips search.searchOnType in the settings of the user.
func (v *view) toggleSearchOnType() {
	v.api.Notifications().Info("Search on type is " + onOff(v.bool(settingSearchOnType)) + "; change search.searchOnType in the settings to turn it " + onOff(!v.bool(settingSearchOnType)) + ".")
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// focusSearchBox gives the keyboard to the search box, with its text selected.
func (v *view) focusSearchBox() {
	v.wantFocus = "search"
	v.handle.Reveal(true)
	v.refreshView()
}

// focusList gives the keyboard to the list of results.
func (v *view) focusList() {
	v.wantFocus = "list"
	v.handle.Reveal(true)
	v.refreshView()
}

// moveInputFocus moves the keyboard to the next or the previous box (Ctrl+Down, Ctrl+Up): search, replace, include, exclude, the
// boxes that are shown.
func (v *view) moveInputFocus(delta int) {
	boxes := []struct {
		name string
		ctl  *ui.InputBoxController
		on   bool
	}{
		{"search", v.searchBox, true}, {"replace", v.replaceBox, v.replaceOpen},
		{"include", v.includeBox, v.detailsOpen}, {"exclude", v.excludeBox, v.detailsOpen},
	}
	cur := -1
	for i, b := range boxes {
		if b.ctl.HasFocus() {
			cur = i
		}
	}
	for i := cur + delta; i >= 0 && i < len(boxes); i += delta {
		if boxes[i].on {
			boxes[i].ctl.Focus()
			return
		}
	}
	if delta > 0 {
		v.focusList()
	}
}

// ---- Find in Files: its arguments and its seed ----

// findArgs are the arguments of workbench.action.findInFiles and workbench.action.replaceInFiles (VS Code's
// IFindInFilesArgs); a field that is nil was not given.
type findArgs struct {
	Query           *string
	Replace         *string
	Regex           *bool
	CaseSensitive   *bool
	WholeWord       *bool
	PreserveCase    *bool
	FilesToInclude  *string
	FilesToExclude  *string
	OnlyOpenEditors *bool
	TriggerSearch   *bool
}

// parseFindArgs reads the arguments; a value of the wrong type is left out.
func parseFindArgs(args any) findArgs {
	var a findArgs
	m, ok := args.(map[string]any)
	if !ok {
		if raw, isRaw := args.(json.RawMessage); isRaw {
			_ = json.Unmarshal(raw, &m) // an argument that is not an object is no argument
		}
	}
	str := func(k string) *string {
		if s, ok := m[k].(string); ok {
			return &s
		}
		return nil
	}
	boolean := func(k string) *bool {
		if b, ok := m[k].(bool); ok {
			return &b
		}
		return nil
	}
	a.Query, a.Replace = str("query"), str("replace")
	a.Regex, a.CaseSensitive, a.WholeWord, a.PreserveCase = boolean("isRegex"), boolean("isCaseSensitive"), boolean("matchWholeWord"), boolean("preserveCase")
	a.FilesToInclude, a.FilesToExclude = str("filesToInclude"), str("filesToExclude")
	a.OnlyOpenEditors, a.TriggerSearch = boolean("onlyOpenEditors"), boolean("triggerSearch")
	return a
}

// findInFiles is workbench.action.findInFiles and, with replace, workbench.action.replaceInFiles: the view opens with the
// keyboard in the search box, seeded with the arguments, or with the selection of the editor, or with the word at its cursor
// when search.seedWithNearestWord is on.
func (v *view) findInFiles(args any, replace bool) {
	a := parseFindArgs(args)
	if a.Regex != nil {
		v.regex = *a.Regex
	}
	if a.CaseSensitive != nil {
		v.caseSensitive = *a.CaseSensitive
	}
	if a.WholeWord != nil {
		v.wholeWord = *a.WholeWord
	}
	if a.PreserveCase != nil {
		v.preserveCase = *a.PreserveCase
	}
	if a.OnlyOpenEditors != nil {
		v.onlyOpen = *a.OnlyOpenEditors
	}
	if a.FilesToInclude != nil {
		v.includeBox.SetValue(*a.FilesToInclude)
		v.detailsOpen = v.detailsOpen || *a.FilesToInclude != ""
	}
	if a.FilesToExclude != nil {
		v.excludeBox.SetValue(*a.FilesToExclude)
		v.detailsOpen = v.detailsOpen || *a.FilesToExclude != ""
	}
	if replace || a.Replace != nil {
		v.replaceOpen = true
	}
	if a.Replace != nil {
		v.replaceBox.SetValue(*a.Replace)
	}
	seeded := false
	switch {
	case a.Query != nil:
		v.searchBox.SetValue(*a.Query)
		seeded = *a.Query != ""
	default:
		if text := v.seedText(); text != "" {
			v.searchBox.SetValue(text)
			seeded = true
		}
	}
	v.replacer = nil
	v.wantFocus = "search"
	v.handle.Reveal(true)
	trigger := seeded
	if a.TriggerSearch != nil {
		trigger = *a.TriggerSearch && v.searchBox.Value() != ""
	}
	if trigger {
		v.startSearch()
	}
	v.updateKeys()
	v.refreshView()
}

// seedText is the text the search starts with: the selection of the active editor when it is on one line, else the word at the
// cursor when search.seedWithNearestWord is on (VS Code's getSearchString). The editor comes from Documents().Active, so the
// seed is right also when the view was activated after the editor.
func (v *view) seedText() string {
	doc, ok := v.api.Documents().Active()
	if !ok || len(doc.Selections) == 0 {
		return ""
	}
	text, err := v.api.Documents().Read(v.ctx, doc.URI)
	if err != nil {
		return ""
	}
	return seedFrom(text, doc.Selections[0], v.bool(settingSeedNearestWord))
}

// seedFrom is the selected text of a text, or the word at the cursor.
func seedFrom(text string, sel extension.Selection, nearestWord bool) string {
	from, to := sel.Anchor, sel.Active
	if from.Line > to.Line || (from.Line == to.Line && from.Col > to.Col) {
		from, to = to, from
	}
	lines := strings.Split(text, "\n")
	if from.Line >= len(lines) || to.Line != from.Line {
		return ""
	}
	line := lines[from.Line]
	if from.Col < 0 || to.Col > len(line) {
		return ""
	}
	if to.Col > from.Col {
		return line[from.Col:to.Col]
	}
	if !nearestWord {
		return ""
	}
	start, end := from.Col, from.Col
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:start])
		if !isWordRune(r) {
			break
		}
		start -= size
	}
	for end < len(line) {
		r, size := utf8.DecodeRuneInString(line[end:])
		if !isWordRune(r) {
			break
		}
		end += size
	}
	return line[start:end]
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// ---- the replacement ----

// replacement is the replacement for the text of a match.
func (v *view) replacement(matchText string) string {
	key := v.replaceBox.Value()
	if v.replacer == nil || v.replacerFor != key {
		q := v.status.query
		if q.Pattern == "" {
			q = v.query()
		}
		r, err := v.api.Workspace().NewTextReplacer(q, key, v.preserveCase)
		if err != nil {
			var appErr *errors.Error
			if e, ok := errors.FromError(err); ok {
				appErr = e
			}
			v.api.Log().Warn("the replacement cannot be made", "tag", tagOf(appErr), "err", err)
			return key
		}
		v.replacer, v.replacerFor = r, key
	}
	return v.replacer.Replace(matchText)
}

func tagOf(e *errors.Error) string {
	if e == nil {
		return ""
	}
	return e.Tag
}
