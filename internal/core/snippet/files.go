package snippet

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// bodyInsights is what Emetos finds out about the body of a snippet the first time it is asked.
//
// Ported from VS Code: src/vs/workbench/contrib/snippets/browser/snippetsFile.ts (SnippetBodyInsights)
type bodyInsights struct {
	codeSnippet   string
	isBogus       bool
	isTrivial     bool
	usesClipboard bool
	usesSelection bool
}

func newBodyInsights(body string) *bodyInsights {
	in := &bodyInsights{codeSnippet: body}
	snippet := NewParser().Parse(body)

	placeholderMax := 0
	for _, p := range snippet.Placeholders() {
		placeholderMax = max(placeholderMax, p.Index)
	}
	// trivial: no placeholder, or the only one is the final tab stop at the very end
	if len(snippet.Placeholders()) == 0 {
		in.isTrivial = true
	} else if placeholderMax == 0 {
		children := snippet.Children()
		if len(children) > 0 {
			last, ok := children[len(children)-1].(*Placeholder)
			in.isTrivial = ok && last.IsFinalTabstop()
		}
	}

	indexes := map[string]int{}
	stack := slices.Clone(snippet.Children())
	for len(stack) > 0 {
		marker := stack[0]
		stack = stack[1:]
		v, ok := marker.(*Variable)
		if !ok {
			stack = append(stack, marker.Children()...)
			continue
		}
		if len(v.Children()) == 0 && !IsKnownVariable(v.Name) {
			// a variable without a default that is not one of ours is a placeholder: ${foo} is ${N:foo}
			index, seen := indexes[v.Name]
			if !seen {
				placeholderMax++
				index = placeholderMax
				indexes[v.Name] = index
			}
			synthetic := NewPlaceholder(index)
			synthetic.AppendChild(NewText(v.Name))
			snippet.Replace(v, []Marker{synthetic})
			in.isBogus = true
		}
		switch v.Name {
		case "CLIPBOARD":
			in.usesClipboard = true
		case "SELECTION", "TM_SELECTED_TEXT":
			in.usesSelection = true
		}
	}
	if in.isBogus {
		in.codeSnippet = snippet.ToTextmateString()
	}
	return in
}

// Snippet is one snippet of a snippet file.
//
// Ported from VS Code: src/vs/workbench/contrib/snippets/browser/snippetsFile.ts (Snippet)
type Snippet struct {
	// IsFileTemplate marks a snippet that is a template for a new file.
	IsFileTemplate bool
	// Scopes are the language ids the snippet is for, empty for every language.
	Scopes []string
	// Name is the key of the snippet in its file.
	Name string
	// Prefix is what the user types to get the snippet; empty when it has none. A snippet whose
	// file gives several prefixes is several Snippets.
	Prefix string
	// PrefixLow is Prefix in lower case.
	PrefixLow string
	// Description tells what the snippet is for.
	Description string
	// Body is the snippet text, the lines of the file joined by "\n".
	Body string
	// Source tells where the snippet is from, for the picker ("User Snippet").
	Source string
	// Identifier is the file name and the snippet name, which tell this snippet from the others.
	Identifier string
	// Include and Exclude are globs on the file name (or the path when they hold a "/") that limit
	// the files the snippet is for.
	Include, Exclude []string

	once     sync.Once
	insights *bodyInsights
}

func (s *Snippet) body() *bodyInsights {
	s.once.Do(func() { s.insights = newBodyInsights(s.Body) })
	return s.insights
}

// CodeSnippet is the text to insert: the body, with the variables that are not variables turned
// into placeholders (VS Code turns `${foo}` into `${1:foo}`).
func (s *Snippet) CodeSnippet() string { return s.body().codeSnippet }

// IsBogus tells whether the body uses a name in the place of a placeholder that is not a variable.
func (s *Snippet) IsBogus() bool { return s.body().isBogus }

// IsTrivial tells whether the body has no tab stop, or only a final one at its end.
func (s *Snippet) IsTrivial() bool { return s.body().isTrivial }

// NeedsClipboard tells whether the body uses the CLIPBOARD variable, so the caller has to read
// the clipboard before inserting.
func (s *Snippet) NeedsClipboard() bool { return s.body().usesClipboard }

// UsesSelection tells whether the body uses SELECTION or TM_SELECTED_TEXT.
func (s *Snippet) UsesSelection() bool { return s.body().usesSelection }

// IsFileIncluded tells whether the include and exclude globs let a file path have the snippet.
func (s *Snippet) IsFileIncluded(path string) bool {
	name := filepath.Base(path)
	target := func(pattern string) string {
		if strings.Contains(pattern, "/") {
			return path
		}
		return name
	}
	for _, pattern := range s.Exclude {
		if pattern != "" && glob.MatchPattern(pattern, target(pattern), glob.Options{IgnoreCase: true}) {
			return false
		}
	}
	if s.Include != nil {
		for _, pattern := range s.Include {
			if pattern != "" && glob.MatchPattern(pattern, target(pattern), glob.Options{IgnoreCase: true}) {
				return true
			}
		}
		return false
	}
	return true
}

// File is a snippet file: `<language>.json` for one language, or `*.code-snippets` for any
// language (each snippet then names its `scope`).
//
// Ported from VS Code: src/vs/workbench/contrib/snippets/browser/snippetsFile.ts (SnippetFile)
type File struct {
	// Path is where the file is.
	Path string
	// IsGlobal is true for a `.code-snippets` file.
	IsGlobal bool
	// Snippets are the snippets of the file, in the order of the file.
	Snippets []*Snippet
}

// ParseFile reads the text of a snippet file. Like VS Code, it is tolerant: comments and trailing
// commas are allowed, and what is not a snippet is left out. Text that is not an object has no
// snippets.
func ParseFile(path, text string) *File {
	f := &File{Path: path, IsGlobal: filepath.Ext(path) == ".code-snippets"}
	root, _ := jsonc.ParseTree(text, jsonc.ParseOptions{})
	if root == nil || root.Type != jsonc.ObjectNode {
		return f
	}
	for _, prop := range root.Children {
		name, value := propertyOf(prop)
		if value == nil || value.Type != jsonc.ObjectNode {
			continue
		}
		if hasBody(value) {
			f.addSnippet(name, value)
			continue
		}
		// a group of snippets, as the scope of a `.code-snippets` file
		for _, inner := range value.Children {
			innerName, innerValue := propertyOf(inner)
			if innerValue != nil && innerValue.Type == jsonc.ObjectNode {
				f.addSnippet(innerName, innerValue)
			}
		}
	}
	return f
}

func propertyOf(prop *jsonc.Node) (string, *jsonc.Node) {
	if prop.Type != jsonc.PropertyNode || len(prop.Children) < 2 {
		return "", nil
	}
	name, _ := prop.Children[0].Value.(string)
	return name, prop.Children[1]
}

func member(obj *jsonc.Node, key string) *jsonc.Node {
	for _, prop := range obj.Children {
		if name, value := propertyOf(prop); name == key {
			return value
		}
	}
	return nil
}

// hasBody mirrors VS Code's isJsonSerializedSnippet: an object whose "body" is not falsy.
func hasBody(obj *jsonc.Node) bool {
	body := member(obj, "body")
	if body == nil {
		return false
	}
	switch body.Type {
	case jsonc.StringNode:
		return body.Value != ""
	case jsonc.NullNode:
		return false
	case jsonc.BooleanNode:
		return body.Value == true
	case jsonc.NumberNode:
		return body.Value != float64(0)
	}
	return true
}

// strings reads a string, or an array of strings (other elements are left out), and reports
// whether the node was one of them.
func stringsOf(n *jsonc.Node) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	switch n.Type {
	case jsonc.StringNode:
		s, _ := n.Value.(string)
		return []string{s}, true
	case jsonc.ArrayNode:
		out := []string{}
		for _, c := range n.Children {
			if c.Type == jsonc.StringNode {
				s, _ := c.Value.(string)
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func (f *File) addSnippet(name string, obj *jsonc.Node) {
	bodyNode := member(obj, "body")
	body, ok := stringsOf(bodyNode)
	if !ok {
		return
	}
	// a body that is a string is one text; an array is lines
	text := strings.Join(body, "\n")

	prefixes := []string{""}
	if p, ok := stringsOf(member(obj, "prefix")); ok {
		prefixes = p // an empty array gives no snippet, as in VS Code
	}
	description := ""
	if d, ok := stringsOf(member(obj, "description")); ok {
		description = strings.Join(d, "\n")
	}
	var scopes []string
	if scope := member(obj, "scope"); scope != nil && scope.Type == jsonc.StringNode {
		s, _ := scope.Value.(string)
		for part := range strings.SplitSeq(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				scopes = append(scopes, part)
			}
		}
	}
	include, _ := stringsOf(member(obj, "include"))
	exclude, _ := stringsOf(member(obj, "exclude"))
	isTemplate := false
	if n := member(obj, "isFileTemplate"); n != nil {
		isTemplate = n.Value == true
	}

	source := "User Snippet"
	if f.IsGlobal {
		source = "Global User Snippet"
	}
	for _, prefix := range prefixes {
		f.Snippets = append(f.Snippets, &Snippet{
			IsFileTemplate: isTemplate,
			Scopes:         scopes,
			Name:           name,
			Prefix:         prefix,
			PrefixLow:      strings.ToLower(prefix),
			Description:    description,
			Body:           text,
			Source:         source,
			Identifier:     filepath.Base(f.Path) + "/" + name,
			Include:        include,
			Exclude:        exclude,
		})
	}
}

// Select adds to bucket the snippets of the file that are for a language. A language file is for
// its own language only (the scope of a snippet in it is ignored); a global file is for the
// snippets with no scope and those whose scope is the language, or the language's parent: the
// scope `source.js` also matches `source`.
func (f *File) Select(languageID string, bucket []*Snippet) []*Snippet {
	if !f.IsGlobal {
		if languageID+".json" == filepath.Base(f.Path) {
			bucket = append(bucket, f.Snippets...)
		}
		return bucket
	}
	for selector, first := languageID, true; ; first = false {
		for _, s := range f.Snippets {
			// a snippet with no scope is taken once (VS Code takes it again for every parent scope)
			if (first && len(s.Scopes) == 0) || slices.Contains(s.Scopes, selector) {
				bucket = append(bucket, s)
			}
		}
		i := strings.LastIndex(selector, ".")
		if i < 0 {
			return bucket
		}
		selector = selector[:i]
	}
}

// Service holds the snippet files of a folder and finds the snippets for a language and a file.
// Reload reads the folder again, which the file watcher of the workspace calls when it changes.
// It is safe to use from several goroutines.
//
// Ported from VS Code: src/vs/workbench/contrib/snippets/browser/snippetsService.ts
type Service struct {
	mu       sync.RWMutex
	dir      string
	files    []*File
	disabled map[string]bool
}

// NewService makes a service for the snippet files in a folder; it has none until Reload.
func NewService(dir string) *Service {
	return &Service{dir: dir, disabled: map[string]bool{}}
}

// Dir is the folder the snippet files are in.
func (s *Service) Dir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dir
}

// SetDir makes the service read another folder, that of another profile (SPEC §10.12), and reads it: the
// snippets of the old folder are gone, and the next query answers from the new one. The errors are those of
// Reload; the snippets of the old folder are dropped even when the new one cannot be read.
func (s *Service) SetDir(ctx context.Context, dir string) error {
	s.mu.Lock()
	s.dir = dir
	s.files = nil
	s.mu.Unlock()
	return s.Reload(ctx)
}

// SetDisabled sets the identifiers of the snippets that are not offered.
func (s *Service) SetDisabled(identifiers ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = map[string]bool{}
	for _, id := range identifiers {
		s.disabled[id] = true
	}
}

// Reload reads the `*.json` and `*.code-snippets` files of the folder. A folder that does not
// exist has no snippets. A file that cannot be read is left out, and the first such failure is
// returned after the others are loaded.
func (s *Service) Reload(ctx context.Context) error {
	dir := s.Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			s.set(dir, nil)
			return nil
		}
		s.set(dir, nil)
		return errors.NewErr("SnippetDirReadFailed", err).SetMetadata("dir", dir)
	}
	var files []*File
	var first error
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.IsDir() || (ext != ".json" && ext != ".code-snippets") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		read, err := fileio.Read(ctx, path, fileio.ReadOptions{})
		if err != nil {
			if first == nil {
				first = errors.NewErr("SnippetFileReadFailed", err).SetMetadata("path", path)
			}
			continue
		}
		files = append(files, ParseFile(path, read.Text))
	}
	s.set(dir, files)
	return first
}

// set keeps the files read from dir, unless the service reads another folder by now (SetDir ran while the files
// were read).
func (s *Service) set(dir string, files []*File) {
	s.mu.Lock()
	if s.dir == dir {
		s.files = files
	}
	s.mu.Unlock()
}

// Query narrows the snippets that Snippets returns.
type Query struct {
	// LanguageID limits the snippets to those for a language; empty means all of them.
	LanguageID string
	// Path limits the snippets to those whose include and exclude globs let this file have them;
	// empty applies no limit.
	Path string
	// IncludeNoPrefix keeps the snippets that have no prefix.
	IncludeNoPrefix bool
	// IncludeDisabled keeps the snippets that are disabled.
	IncludeDisabled bool
	// FileTemplates, when not nil, keeps only the snippets that are (true) or are not (false)
	// file templates.
	FileTemplates *bool
}

// Snippets are the snippets that answer a query, sorted by the file kind and name as VS Code
// does (Emetos does not sort by recent use yet).
func (s *Service) Snippets(q Query) []*Snippet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var picked []*Snippet
	for _, f := range s.files {
		if q.LanguageID != "" {
			picked = f.Select(q.LanguageID, picked)
		} else {
			picked = append(picked, f.Snippets...)
		}
	}
	var out []*Snippet
	for _, sn := range picked {
		switch {
		case sn.Prefix == "" && !q.IncludeNoPrefix:
		case s.disabled[sn.Identifier] && !q.IncludeDisabled:
		case q.FileTemplates != nil && *q.FileTemplates != sn.IsFileTemplate:
		case q.Path != "" && !sn.IsFileIncluded(q.Path):
		default:
			out = append(out, sn)
		}
	}
	slices.SortStableFunc(out, func(a, b *Snippet) int {
		if c := strings.Compare(a.Source, b.Source); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// NonWhitespacePrefix is the text before a column of a line back to the last white space: what
// the user has typed of a prefix. It looks at no more than 100 characters, and is empty when the
// word is longer. The column is a byte offset in the line.
//
// Ported from VS Code: src/vs/workbench/contrib/snippets/browser/snippetsService.ts (getNonWhitespacePrefix)
func NonWhitespacePrefix(line string, column int) string {
	const maxPrefixLength = 100
	line = line[:min(column, len(line))]
	runes := []rune(line)
	minIndex := max(0, len(runes)-maxPrefixLength)
	for i := len(runes) - 1; i >= minIndex; i-- {
		switch runes[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f', 0xA0, 0x2028, 0x2029, 0xFEFF:
			return string(runes[i+1:])
		}
	}
	if minIndex == 0 {
		return line
	}
	return ""
}

// TabCompletions are the snippets that the Tab key would insert at a place: those whose prefix
// the text before the cursor ends with (for an empty selection), or equals the selected text (a
// selection on one line of at most 100 characters). Which snippets are offered is the caller's
// choice (Service.Snippets for the language at the cursor).
//
// Ported from VS Code: src/vs/workbench/contrib/snippets/browser/tabCompletion.ts (_updateSnippets)
func TabCompletions(snippets []*Snippet, lineBeforeCursor, selectedText string, hasSelection, multiline bool) []*Snippet {
	var out []*Snippet
	if !hasSelection {
		prefix := NonWhitespacePrefix(lineBeforeCursor, len(lineBeforeCursor))
		if prefix == "" {
			return nil
		}
		for _, s := range snippets {
			if strings.HasSuffix(prefix, s.Prefix) {
				out = append(out, s)
			}
		}
		return out
	}
	if multiline || len([]rune(selectedText)) > 100 || selectedText == "" {
		return nil
	}
	for _, s := range snippets {
		if selectedText == s.Prefix {
			out = append(out, s)
		}
	}
	return out
}
