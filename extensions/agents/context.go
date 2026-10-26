package agents

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// lastEditor is the text editor the user looked at last: the file and its selection. The extension follows the events of the
// editor, so that "Add Selection to Agent" and the active file of a message mean the file the user was in, also while a session
// tab has the focus.
type lastEditor struct {
	// uri is the URI of the file; empty before any editor was shown.
	uri string
	// sel is the primary selection, and hasSel says that it is known.
	sel    extension.Selection
	hasSel bool
}

// editorEvent follows the active editor and its selection.
func (a *agents) editorEvent(ev extension.Event) {
	switch ev.Topic {
	case extension.TopicEditorActiveChanged:
		var ae extension.ActiveEditor
		if ev.Decode(&ae) != nil || ae.URI == "" {
			return // no text editor in front: the last one stays the one to refer to
		}
		if ae.URI != a.editor.uri {
			a.editor = lastEditor{uri: ae.URI}
		}
	case extension.TopicEditorSelectionChanged:
		var es extension.EditorSelection
		if ev.Decode(&es) != nil || es.URI == "" || len(es.Selections) == 0 {
			return
		}
		a.editor = lastEditor{uri: es.URI, sel: es.Selections[0], hasSel: true}
	}
}

// fileOfURI gives the path of a file: URI.
func fileOfURI(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return "", false
	}
	return filepath.FromSlash(u.Path), true
}

// reference names a file for the agent: its path relative to the folder of the workspace (the absolute path of a file outside
// it), with forward slashes. inside says that it is in the folder.
func (a *agents) reference(uri string) (name string, inside bool, ok bool) {
	abs, ok := fileOfURI(uri)
	if !ok {
		return "", false, false
	}
	folder := a.api.Scope().Folder
	if folder != "" {
		if rel, err := filepath.Rel(folder, abs); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel), true, true
		}
	}
	return filepath.ToSlash(abs), false, true
}

// lineRange is the part of a reference after the path: #L12-30, or #L12 for one line or a cursor. The lines count from 1; a
// selection that ends at the start of a line does not include that line.
func lineRange(sel extension.Selection) string {
	start, end := sel.Anchor, sel.Active
	if end.Line < start.Line || (end.Line == start.Line && end.Col < start.Col) {
		start, end = end, start
	}
	first, last := start.Line+1, end.Line+1
	if end.Col == 0 && end.Line > start.Line {
		last = end.Line
	}
	if last <= first {
		return "#L" + strconv.Itoa(first)
	}
	return "#L" + strconv.Itoa(first) + "-" + strconv.Itoa(last)
}

// addSelection is the command "Add Selection to Agent": it puts @path#Lstart-end for the selection of the last text editor
// in the input of the session in front, and shows it. With no session open it opens one.
func (a *agents) addSelection(ctx context.Context, _ any) error {
	e := a.editor
	if e.uri == "" {
		a.api.Notifications().Info("Open a file in an editor first: Add Selection to Agent refers to the selection of the active editor.")
		return nil
	}
	name, inside, ok := a.reference(e.uri)
	if !ok {
		a.api.Notifications().Info("The active editor is not a file, so it cannot be added to the agent.")
		return nil
	}
	if inside && a.denied(ctx, name) {
		a.api.Notifications().Warn(name + " matches a read deny rule of the project, so it is not added to the agent.")
		return nil
	}
	ref := "@" + name
	if e.hasSel {
		ref += lineRange(e.sel)
	}
	t := a.target()
	if t == nil {
		tab, err := a.openNew(0)
		if err != nil {
			return err
		}
		for _, x := range a.tabs {
			if x.tab == tab {
				t = x
			}
		}
		if t == nil {
			return nil
		}
	}
	t.insert(ref)
	t.tab.Reveal(true)
	t.input.Focus()
	t.changed()
	return nil
}

// target is the session that gets a reference: the one whose input had the focus last, else the newest tab; nil with none.
func (a *agents) target() *sessionTab {
	if a.active != nil && !a.active.closed {
		return a.active
	}
	if n := len(a.tabs); n > 0 {
		return a.tabs[n-1]
	}
	return nil
}

// insert puts text at the caret of the input (in place of the selection), with a space before it unless it follows one, and a
// space after.
func (t *sessionTab) insert(text string) {
	v := t.input.Value()
	start, end, ok := t.input.Selection()
	if !ok || start > len(v) || end > len(v) {
		start, end = len(v), len(v)
	}
	if start > 0 && v[start-1] != ' ' && v[start-1] != '\n' {
		text = " " + text
	}
	text += " "
	t.input.SetValue(v[:start] + text + v[end:])
	t.input.Select(start+len(text), start+len(text))
}

// activeFileNote is what the setting agents.attachActiveFile adds to a message: the path of the file open in the editor, as a
// reference. It is empty when the setting is off, when there is no such file inside the folder, when the message names it
// already, or when a read deny rule excludes it.
func (a *agents) activeFileNote(ctx context.Context, message string) string {
	if !a.attachActiveFile() || a.editor.uri == "" {
		return ""
	}
	name, inside, ok := a.reference(a.editor.uri)
	if !ok || !inside || strings.Contains(message, "@"+name) || a.denied(ctx, name) {
		return ""
	}
	return "\n\n(The file open in the editor: @" + name + ")"
}

// builtinDeny are the files that are never attached or added, besides what the project's deny rules say: secrets and keys.
var builtinDeny = []string{
	".env", ".env.*", "*.pem", "*.key", "*.p12", "*.pfx", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", ".netrc", ".npmrc", ".pypirc",
	".git-credentials", ".aws/credentials",
}

// denied tells whether the file (a path relative to the folder) matches a read deny rule: the built-in list, or a Read(...)
// entry of permissions.deny in .claude/settings.json or .claude/settings.local.json of the folder. The settings of the user's
// own configuration folder are not read.
func (a *agents) denied(ctx context.Context, rel string) bool {
	patterns := append([]string(nil), builtinDeny...)
	for _, file := range []string{".claude/settings.json", ".claude/settings.local.json"} {
		text, err := a.api.Workspace().ReadFile(ctx, file)
		if err != nil {
			continue // no such file, or not readable: it has no rules
		}
		patterns = append(patterns, readDenyRules(text)...)
	}
	for _, p := range patterns {
		if matchGlob(p, rel) {
			return true
		}
	}
	return false
}

// readDenyRules returns the patterns of the Read(...) entries of permissions.deny in the text of a settings file.
func readDenyRules(text string) []string {
	var s struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if json.Unmarshal([]byte(text), &s) != nil {
		return nil
	}
	var out []string
	for _, rule := range s.Permissions.Deny {
		inner, ok := strings.CutPrefix(rule, "Read(")
		if !ok || !strings.HasSuffix(inner, ")") {
			continue
		}
		inner = strings.TrimSuffix(inner, ")")
		if strings.HasPrefix(inner, "//") || strings.HasPrefix(inner, "~") {
			continue // an absolute path or one in the home folder: not a file of the folder
		}
		inner = strings.TrimPrefix(strings.TrimPrefix(inner, "./"), "/")
		if inner != "" {
			out = append(out, inner)
		}
	}
	return out
}

// matchGlob matches a path relative to the folder with a pattern in the style of the ignore files: * stays inside a name, **
// crosses folders, ? is one character; a pattern with no slash matches the name of a file at any depth; a pattern also
// matches everything below a folder it names.
func matchGlob(pattern, rel string) bool {
	if pattern == "" || rel == "" {
		return false
	}
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("(?:/.*)?$")
	re, err := regexp.Compile(sb.String())
	return err == nil && re.MatchString(path.Clean(rel))
}
