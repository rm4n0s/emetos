package settings

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/settingsTreeModels.ts
// (SettingsTreeSettingElement: the value type, the value shown, the modified mark, where else a setting is
// modified) and src/vs/workbench/services/preferences/common/preferencesValidation.ts (createValidator)

// RowKind is how the settings editor edits a setting.
type RowKind int

const (
	// KindBool is a check box.
	KindBool RowKind = iota + 1
	// KindNumber is a box for a number.
	KindNumber
	// KindInteger is a box for a whole number.
	KindInteger
	// KindString is a box for a text.
	KindString
	// KindEnum is a list of the values the setting allows.
	KindEnum
	// KindComplex is a value the editor does not edit itself (an array, an object, a value of several
	// types): it links to the JSON file.
	KindComplex
)

// Choice is a value of an enum setting.
type Choice struct {
	// Value is the JSON value to write.
	Value any
	// Label is what the list shows: the value as text.
	Label string
	// Description is the text of the choice (EnumDescriptions), or empty.
	Description string
}

// Row is one setting as the settings editor shows it for a target (a tab): the texts, how to edit it, the value it
// shows and whether the file of the tab sets it.
type Row struct {
	// Key is the setting's key.
	Key string
	// Category and Label are the title of the setting in two parts ("Editor", "Font Size"); the category
	// is empty where the group of the setting says it already.
	Category, Label string
	// Description is the Markdown text of the setting.
	Description string
	// Kind is how the setting is edited.
	Kind RowKind
	// Value is the value shown: the value in the file of the target when it sets the setting, else the default.
	Value any
	// Default is the default of the setting.
	Default any
	// Modified is true when the file of the target sets the setting (VS Code's "isConfigured"): the row
	// gets the modified mark and "Reset Setting".
	Modified bool
	// AlsoModifiedIn names the other files that set the setting ("User", "Workspace").
	AlsoModifiedIn []string
	// Choices are the values of an enum.
	Choices []Choice
	// Minimum and Maximum bound a number.
	Minimum, Maximum *float64
	// Deprecated is the message of a deprecated setting.
	Deprecated string
	// Invalid is a message when the value in the file is not allowed by the setting (the editor shows the default).
	Invalid string

	setting Setting
}

// Title is "Category: Label".
func (r Row) Title() string { return Title(r.Category, r.Label) }

// Setting is the registered setting of the row.
func (r Row) Setting() Setting { return r.setting }

// UIModel is what the settings editor shows (SPEC §11.1): the groups of the registered settings, the rows of a
// target and the edits. It holds no state of its own besides the groups, which it builds again when something is
// registered.
type UIModel struct {
	store *Store
	toc   []TOCEntry

	mu      sync.Mutex
	version int
	built   bool
	tree    []*Category
}

// NewUIModel makes the UI model of the settings editor over a store, with the table of contents of VS Code
// (DefaultTOC).
func NewUIModel(store *Store) *UIModel {
	return &UIModel{store: store, toc: DefaultTOC()}
}

// Store is the store the model edits.
func (m *UIModel) Store() *Store { return m.store }

// Categories are the groups of the settings a target shows, in order: for the workspace only the settings a
// workspace may set (VS Code hides the application and machine settings there). Empty groups are not in it.
// match, when not nil, keeps the settings it accepts (the search).
func (m *UIModel) Categories(target Target, match func(Row) bool) []*Category {
	m.mu.Lock()
	if !m.built || m.version != m.store.reg.Version() {
		m.tree = BuildCategories(m.toc, m.store.reg.All())
		m.version, m.built = m.store.reg.Version(), true
	}
	tree := m.tree
	m.mu.Unlock()
	var filter func(c *Category) *Category
	filter = func(c *Category) *Category {
		out := &Category{ID: c.ID, Label: c.Label, Level: c.Level}
		for _, key := range c.Keys {
			s, ok := m.store.reg.Lookup(key)
			if !ok || !visibleIn(s, target) {
				continue
			}
			if match != nil {
				row, ok := m.rowIn(s, target, c.ID)
				if !ok || !match(row) {
					continue
				}
			}
			out.Keys = append(out.Keys, key)
		}
		for _, ch := range c.Children {
			if f := filter(ch); f != nil {
				out.Children = append(out.Children, f)
			}
		}
		if len(out.Keys) == 0 && len(out.Children) == 0 {
			return nil
		}
		return out
	}
	var out []*Category
	for _, c := range tree {
		if f := filter(c); f != nil {
			out = append(out, f)
		}
	}
	return out
}

// visibleIn tells whether the settings of a target list the setting.
func visibleIn(s Setting, target Target) bool {
	return target != TargetWorkspace || s.Scope.inWorkspace()
}

// Row describes a setting for a target, in the group the setting is shown in (the group says which part of
// the title is left out; "" is none). It reports false for a key that is not registered.
func (m *UIModel) Row(key string, target Target, groupID string) (Row, bool) {
	s, ok := m.store.reg.Lookup(key)
	if !ok {
		return Row{}, false
	}
	return m.rowIn(s, target, groupID)
}

func (m *UIModel) rowIn(s Setting, target Target, groupID string) (Row, bool) {
	category, label := KeyToDisplayFormat(s.Key, groupID)
	in := m.store.Inspect(s.Key, "")
	r := Row{
		Key: s.Key, Category: category, Label: label, Description: s.Description, Kind: kindOf(s),
		Default: in.Default.Value, Minimum: s.Minimum, Maximum: s.Maximum, Deprecated: s.Deprecated, setting: s,
	}
	if !in.Default.Ok {
		r.Default = s.Default
	}
	r.Value = r.Default
	own, other := in.User, in.Workspace
	otherName := "Workspace"
	if target == TargetWorkspace {
		own, other = in.Workspace, in.User
		otherName = "User"
	}
	if own.Ok {
		r.Modified = true
		r.Value = own.Value
		if msg := s.validate(own.Value); msg != "" {
			// a value the setting does not allow is shown as the default, with the reason
			r.Invalid, r.Value = msg, r.Default
		}
	}
	if other.Ok && (target != TargetUser || !in.WorkspaceIgnored) {
		r.AlsoModifiedIn = append(r.AlsoModifiedIn, otherName)
	}
	if r.Kind == KindEnum {
		for i, v := range s.Enum {
			c := Choice{Value: v, Label: choiceLabel(v)}
			if i < len(s.EnumDescriptions) {
				c.Description = s.EnumDescriptions[i]
			}
			r.Choices = append(r.Choices, c)
		}
	}
	return r, true
}

func choiceLabel(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// kindOf is how a setting is edited, in VS Code's order: an enum of renderable types is a list, then strings,
// numbers and booleans, and the rest links to the JSON.
func kindOf(s Setting) RowKind {
	if len(s.Types) == 1 {
		t := s.Types[0]
		if len(s.Enum) > 0 && (t == String || t == Boolean || t == Integer || t == Number) {
			return KindEnum
		}
		switch t {
		case String:
			return KindString
		case Integer:
			return KindInteger
		case Number:
			return KindNumber
		case Boolean:
			return KindBool
		}
		return KindComplex
	}
	if len(s.Enum) > 0 && allIn(s.Types, String, Boolean, Integer, Number, Null) {
		return KindEnum
	}
	return KindComplex
}

func allIn(types []Type, allowed ...Type) bool {
	for _, t := range types {
		found := false
		for _, a := range allowed {
			if t == a {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Parse turns the text of an input box into the value to write, or says why it is not allowed (the message VS Code
// shows under the box). Only the kinds of the input boxes (numbers and strings) parse text.
func (r Row) Parse(text string) (value any, message string) {
	switch r.Kind {
	case KindNumber, KindInteger:
		trimmed := strings.TrimSpace(text)
		f, err := strconv.ParseFloat(trimmed, 64)
		if trimmed == "" || err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, "Value must be a number."
		}
		if msg := r.checkNumber(f); msg != "" {
			return nil, msg
		}
		return f, ""
	case KindString:
		return text, ""
	}
	return nil, "This setting is edited in settings.json."
}

// checkNumber is the message about a number that the setting does not allow.
func (r Row) checkNumber(f float64) string {
	if r.Maximum != nil && f > *r.Maximum {
		return "Value must be less than or equal to " + strconv.FormatFloat(*r.Maximum, 'g', -1, 64) + "."
	}
	if r.Minimum != nil && f < *r.Minimum {
		return "Value must be greater than or equal to " + strconv.FormatFloat(*r.Minimum, 'g', -1, 64) + "."
	}
	if r.Kind == KindInteger && f != math.Trunc(f) {
		return "Value must be an integer."
	}
	return ""
}

// ValueText is the value of a number or a string as the text of its input box.
func (r Row) ValueText() string {
	switch v := r.Value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// ChoiceIndex is the index of the choice that is the value of an enum row, or -1.
func (r Row) ChoiceIndex() int {
	for i, c := range r.Choices {
		if reflect.DeepEqual(c.Value, r.Value) {
			return i
		}
	}
	return -1
}

// Checked is the value of a boolean row.
func (r Row) Checked() bool {
	b, _ := r.Value.(bool)
	return b
}

// SetValue writes a value of a setting in the file of the target. It fails with the tags of Store.Update.
func (m *UIModel) SetValue(ctx context.Context, key string, target Target, value any) error {
	return m.store.Update(ctx, key, value, target)
}

// SetText parses the text of an input box and writes the value; a text the setting does not allow
// fails with the tag SettingInputInvalid, whose message is the one to show, and writes nothing.
func (m *UIModel) SetText(ctx context.Context, key string, target Target, text string) error {
	row, ok := m.Row(key, target, "")
	if !ok {
		return errors.New("SettingUnregistered", "the setting is not registered", "key", key)
	}
	value, msg := row.Parse(text)
	if msg != "" {
		return errors.New("SettingInputInvalid", msg, "key", key)
	}
	return m.store.Update(ctx, key, value, target)
}

// Reset removes the setting from the file of the target ("Reset Setting"): the layers below show again.
func (m *UIModel) Reset(ctx context.Context, key string, target Target) error {
	return m.store.Remove(ctx, key, target)
}

// Span is a piece of a description: plain text, a code span or a link.
type Span struct {
	Text string
	// Code marks text between backticks.
	Code bool
	// Link, when not empty, is the target of a Markdown link; Text is its label.
	Link string
}

var (
	linkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]*)[^)]*\)`)
	// refRe is a reference to another setting, written between hashes (`#editor.tabSize#`).
	refRe  = regexp.MustCompile(`#([A-Za-z][A-Za-z0-9_.\-]*)#`)
	codeRe = regexp.MustCompile("`([^`]*)`")
)

// RenderDescription breaks the Markdown of a description into lines of spans: backtick code spans,
// [label](target) links and #setting.key# references (links to "#setting.key") are marked and their syntax
// removed, emphasis marks are dropped. It is the
// part of Markdown the descriptions of the settings use.
func RenderDescription(markdown string) [][]Span {
	var lines [][]Span
	for _, line := range strings.Split(markdown, "\n") {
		lines = append(lines, renderLine(line))
	}
	return lines
}

func renderLine(line string) []Span {
	var out []Span
	add := func(text string, code bool, link string) {
		if text == "" && link == "" {
			return
		}
		out = append(out, Span{Text: text, Code: code, Link: link})
	}
	plain := func(text string) {
		text = strings.ReplaceAll(text, "**", "")
		for {
			ref := refRe.FindStringSubmatchIndex(text)
			if ref == nil {
				break
			}
			add(text[:ref[0]], false, "")
			add(text[ref[2]:ref[3]], false, "#"+text[ref[2]:ref[3]])
			text = text[ref[1]:]
		}
		add(text, false, "")
	}
	rest := line
	for rest != "" {
		code := codeRe.FindStringSubmatchIndex(rest)
		link := linkRe.FindStringSubmatchIndex(rest)
		switch {
		case code == nil && link == nil:
			plain(rest)
			rest = ""
		case link != nil && (code == nil || link[0] < code[0]):
			plain(rest[:link[0]])
			add(rest[link[2]:link[3]], false, rest[link[4]:link[5]])
			rest = rest[link[1]:]
		default:
			plain(rest[:code[0]])
			add(rest[code[2]:code[3]], true, "")
			rest = rest[code[1]:]
		}
	}
	return out
}

// MatchText is the plain-text search of the editor: the words of query (separated by spaces) must all be in the
// key, the title or the description of the row, whatever the case.
func MatchText(r Row, query string) bool {
	hay := strings.ToLower(r.Key + " " + r.Title() + " " + r.Description)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(hay, word) {
			return false
		}
	}
	return true
}
