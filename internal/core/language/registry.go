package language

import (
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/errors"
)

// Ported from VS Code: reference/vscode/src/vs/editor/common/services/languagesRegistry.ts and
// reference/vscode/src/vs/workbench/services/textMate/common/TMGrammarFactory.ts (MIT)

// PlainTextID is the id of the language of a file that no language claims.
const PlainTextID = "plaintext"

// plainTextCode is the number of the plain text language in the grammars: 0 is no language.
const plainTextCode = 1

// Language is a language as the registry knows it, merged from every contribution to its id.
type Language struct {
	// ID is the language id, such as go.
	ID string
	// Name is the display name: the first alias, or the id.
	Name string
	// Aliases are the names that find the language, ID included.
	Aliases []string
	// Extensions are the file name endings of the language.
	Extensions []string
	// Filenames are the exact file names of the language.
	Filenames []string
	// FilenamePatterns are the file name globs of the language.
	FilenamePatterns []string
	// MIMETypes are the MIME types; the first is the main one.
	MIMETypes []string
}

// resource is a file of an extension.
type resource struct {
	fsys fs.FS
	path string
}

// read reads the file.
func (r resource) read() ([]byte, error) { return fs.ReadFile(r.fsys, r.path) }

// grammarDef is a grammar contributed by an extension.
type grammarDef struct {
	GrammarContribution
	file resource
}

// Registry holds the languages and grammars of the extensions it was given, and tells the
// language of a file (SPEC §8.1). It implements textmate.Registry. It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	languages map[string]*Language
	order     []string
	config    map[string][]resource
	codes     map[string]int
	byCode    []string
	names     map[string]string // lower-case alias → id
	mimes     map[string]string

	platform []association
	user     []association
	// all is every association in the order of registration, for the first line.
	all []association

	grammars    map[string]*grammarDef
	langScope   map[string]string
	injections  map[string][]string
	injectedEmb map[string][]map[string]string

	parsed   map[string]*textmate.RawGrammar
	configs  map[string]*Configuration
	problems []error
	// configDefaults are the settings that extensions give to languages, by language id.
	configDefaults map[string]map[string]any

	// sources are the extensions that were added, in order: a registry is made again from them when one is removed.
	sources []*source
	// version counts the changes of the set of languages and grammars (Version).
	version uint64
}

// source is an extension that was added to a registry.
type source struct {
	// key names the extension for RemoveExtension; empty for one that cannot be removed.
	key      string
	fsys     fs.FS
	manifest *Manifest
}

// NewRegistry makes a registry that has the plain text language.
func NewRegistry() *Registry {
	r := &Registry{}
	r.reset()
	return r
}

// reset empties the registry down to the plain text language. The numbers of the languages, the associations of the
// user and the list of the extensions that were added are kept. The lock is held, or the registry is not shared yet.
func (r *Registry) reset() {
	r.languages, r.config, r.names, r.mimes = map[string]*Language{}, map[string][]resource{}, map[string]string{}, map[string]string{}
	r.order, r.platform, r.all, r.problems = nil, nil, nil, nil
	r.grammars, r.langScope, r.injections = map[string]*grammarDef{}, map[string]string{}, map[string][]string{}
	r.injectedEmb, r.parsed, r.configs = map[string][]map[string]string{}, map[string]*textmate.RawGrammar{}, map[string]*Configuration{}
	r.configDefaults = nil
	if r.codes == nil {
		r.codes, r.byCode = map[string]int{PlainTextID: plainTextCode}, []string{"", PlainTextID}
	}
	r.addLanguage(LanguageContribution{
		ID: PlainTextID, Extensions: []string{".txt"}, Aliases: []string{"Plain Text", "text"}, MIMETypes: []string{"text/plain"},
	}, resource{})
}

// AddExtension adds the languages and grammars that the package.json at the root of fsys
// contributes. The error has the tag ExtensionUnreadable when there is no readable package.json
// and ManifestInvalid when it is not JSON; contributions that are not valid are left out and
// listed in Problems. The extension stays for the life of the registry; AddNamedExtension adds
// one that can be taken away.
func (r *Registry) AddExtension(fsys fs.FS) error {
	data, err := fs.ReadFile(fsys, "package.json")
	if err != nil {
		return errors.NewErr("ExtensionUnreadable", err).SetMetadata("file", "package.json")
	}
	m, err := ParseManifest(data)
	if err != nil {
		return err
	}
	r.addSource("", fsys, m)
	return nil
}

// AddNamedExtension is AddExtension for an extension that RemoveExtension can take away again (a language pack of
// Emetos, SPEC §10.15). It returns what the package.json contributes, so that the caller can tell the languages and
// the settings defaults that came with it. The error has the tags of AddExtension and ExtensionKeyTaken when an
// extension with the key is in the registry, ExtensionKeyEmpty when the key is empty.
func (r *Registry) AddNamedExtension(key string, fsys fs.FS) (*Manifest, error) {
	if key == "" {
		return nil, errors.New("ExtensionKeyEmpty", "an extension that can be removed needs a key")
	}
	data, err := fs.ReadFile(fsys, "package.json")
	if err != nil {
		return nil, errors.NewErr("ExtensionUnreadable", err).SetMetadata("file", "package.json")
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	if !r.addSource(key, fsys, m) {
		return nil, errors.New("ExtensionKeyTaken", "an extension with this key is in the registry", "key", key)
	}
	return m, nil
}

// RemoveExtension takes away the extension added with the key, with its languages, grammars, associations and
// configurations, and reports whether it was there. A language that remains in no extension is no longer registered,
// but its number stays reserved for it, so tokens already made and grammars that name the number stay valid, and an
// extension that comes back gets the same number.
func (r *Registry) RemoveExtension(key string) bool {
	if key == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i := slices.IndexFunc(r.sources, func(s *source) bool { return s.key == key })
	if i < 0 {
		return false
	}
	r.sources = slices.Delete(r.sources, i, i+1)
	r.reset()
	for _, s := range r.sources {
		r.apply(s.fsys, s.manifest)
	}
	r.version++
	return true
}

// Version counts the changes of the set of languages and grammars: it grows whenever an extension is added or
// removed. A cache of what the registry made (the grammars) is valid while it is the same.
func (r *Registry) Version() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// addSource adds an extension whose manifest was read. It reports false, and adds nothing, when an extension has the key.
func (r *Registry) addSource(key string, fsys fs.FS, m *Manifest) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key != "" && slices.ContainsFunc(r.sources, func(s *source) bool { return s.key == key }) {
		return false
	}
	r.sources = append(r.sources, &source{key: key, fsys: fsys, manifest: m})
	r.apply(fsys, m)
	r.version++
	return true
}

// apply adds the contributions of a manifest. The lock is held.
func (r *Registry) apply(fsys fs.FS, m *Manifest) {
	r.problems = append(r.problems, m.Problems...)
	for id, values := range m.ConfigurationDefaults {
		if r.configDefaults == nil {
			r.configDefaults = map[string]map[string]any{}
		}
		if r.configDefaults[id] == nil {
			r.configDefaults[id] = map[string]any{}
		}
		for k, v := range values {
			r.configDefaults[id][k] = v
		}
	}
	for _, l := range m.Languages {
		r.addLanguage(l, resource{fsys: fsys, path: cleanContributionPath(l.Configuration)})
	}
	for _, g := range m.Grammars {
		r.addGrammar(g, resource{fsys: fsys, path: cleanContributionPath(g.Path)})
	}
}

// ConfigurationDefaults gives the settings that the extensions contribute to languages
// (contributes.configurationDefaults), by language id: "[go]": {"editor.insertSpaces": false}
// is {"go": {"editor.insertSpaces": false}}. The maps are copies.
func (r *Registry) ConfigurationDefaults() map[string]map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]map[string]any, len(r.configDefaults))
	for id, values := range r.configDefaults {
		cp := make(map[string]any, len(values))
		for k, v := range values {
			cp[k] = v
		}
		out[id] = cp
	}
	return out
}

// cleanContributionPath turns the relative path of a contribution, such as ./syntaxes/a.json,
// into a path of an fs.FS.
func cleanContributionPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(strings.TrimPrefix(p, "./"))
}

// addLanguage merges a language contribution (_mergeLanguage of VS Code). The lock is held.
func (r *Registry) addLanguage(c LanguageContribution, config resource) {
	l, ok := r.languages[c.ID]
	if !ok {
		l = &Language{ID: c.ID}
		r.languages[c.ID] = l
		r.order = append(r.order, c.ID)
		if _, has := r.codes[c.ID]; !has {
			r.codes[c.ID] = len(r.byCode)
			r.byCode = append(r.byCode, c.ID)
		}
	}
	primaryMIME := ""
	if len(c.MIMETypes) > 0 {
		l.MIMETypes = append(l.MIMETypes, c.MIMETypes...)
		primaryMIME = c.MIMETypes[0]
	}
	if primaryMIME == "" {
		primaryMIME = "text/x-" + c.ID
		l.MIMETypes = append(l.MIMETypes, primaryMIME)
	}
	if c.Configuration != "" {
		l.Extensions = append(append([]string(nil), c.Extensions...), l.Extensions...)
	} else {
		l.Extensions = append(l.Extensions, c.Extensions...)
	}
	for _, ext := range c.Extensions {
		r.platform = r.register(r.platform, association{id: c.ID, mime: primaryMIME, extension: ext})
	}
	for _, name := range c.Filenames {
		r.platform = r.register(r.platform, association{id: c.ID, mime: primaryMIME, filename: name})
		l.Filenames = append(l.Filenames, name)
	}
	for _, pattern := range c.FilenamePatterns {
		r.platform = r.register(r.platform, newPatternAssociation(c.ID, primaryMIME, pattern))
		l.FilenamePatterns = append(l.FilenamePatterns, pattern)
	}
	if c.FirstLine != "" {
		if a, err := newFirstLineAssociation(c.ID, primaryMIME, c.FirstLine); err != nil {
			r.problems = append(r.problems, err)
		} else if a != nil {
			r.platform = r.register(r.platform, *a)
		}
	}
	l.Aliases = append(l.Aliases, c.ID)
	var aliases []string
	for _, a := range c.Aliases {
		if a != "" {
			aliases = append(aliases, a)
		}
	}
	l.Aliases = append(l.Aliases, aliases...)
	// The first alias is the display name, unless the contribution has aliases that are all
	// empty or none, in which case an earlier name stays.
	switch {
	case len(c.Aliases) > 0 && c.Aliases[0] == "":
	case len(c.Aliases) > 0:
		l.Name = c.Aliases[0]
	case l.Name == "":
		l.Name = c.ID
	}
	if config.fsys != nil && config.path != "" {
		r.config[c.ID] = append(r.config[c.ID], config)
	}
	// The lookups by name and MIME type are made again from the languages.
	for _, id := range r.order {
		lang := r.languages[id]
		for _, alias := range lang.Aliases {
			r.names[strings.ToLower(alias)] = id
		}
		for _, mime := range lang.MIMETypes {
			r.mimes[mime] = id
		}
	}
}

// register adds an association to the list and to the list of all of them.
func (r *Registry) register(list []association, a association) []association {
	r.all = append(r.all, a)
	return append(list, a)
}

// addGrammar adds a grammar contribution. The lock is held.
func (r *Registry) addGrammar(c GrammarContribution, file resource) {
	r.grammars[c.ScopeName] = &grammarDef{GrammarContribution: c, file: file}
	delete(r.parsed, c.ScopeName)
	for _, scope := range c.InjectTo {
		r.injections[scope] = append(r.injections[scope], c.ScopeName)
		if len(c.EmbeddedLanguages) > 0 {
			r.injectedEmb[scope] = append(r.injectedEmb[scope], c.EmbeddedLanguages)
		}
	}
	if c.Language != "" {
		r.langScope[c.Language] = c.ScopeName
	}
}

// IsRegistered tells whether a language has the id.
func (r *Registry) IsRegistered(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.languages[id]
	return ok
}

// Language gives the language with the id.
func (r *Registry) Language(id string) (Language, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.languages[id]
	if !ok {
		return Language{}, false
	}
	return *l, true
}

// Languages gives every language, in the order they were registered.
func (r *Registry) Languages() []Language {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Language, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, *r.languages[id])
	}
	return out
}

// ByName gives the id of the language that has the name as an alias or as its id, ignoring
// case.
func (r *Registry) ByName(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.names[strings.ToLower(name)]
	return id, ok
}

// ByMIMEType gives the id of the language with the MIME type.
func (r *Registry) ByMIMEType(mime string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.mimes[mime]
	return id, ok
}

// Code is the number of a language in the tokens of the grammars; 0 for an id that is not
// registered.
func (r *Registry) Code(id string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.codes[id]
}

// IDOfCode is the id of the language with the number, or "" for 0 and for a number that is not
// one.
func (r *Registry) IDOfCode(code int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if code < 0 || code >= len(r.byCode) {
		return ""
	}
	return r.byCode[code]
}

// Problems are the contributions, grammars and configurations that could not be used so far.
func (r *Registry) Problems() []error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]error(nil), r.problems...)
}

// addProblem notes an error once.
func (r *Registry) addProblem(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems = append(r.problems, err)
}

// Configuration gives the language configuration of the language: its configuration files merged
// in the order they were contributed. A language without any has an empty configuration. What
// could not be read is listed in Problems.
func (r *Registry) Configuration(id string) *Configuration {
	r.mu.RLock()
	c, ok := r.configs[id]
	files := r.config[id]
	r.mu.RUnlock()
	if ok {
		return c
	}
	merged := &Configuration{}
	var problems []error
	for _, f := range files {
		data, err := f.read()
		if err != nil {
			problems = append(problems, errors.NewErr("ConfigurationFileUnreadable", err).SetMetadata("language", id, "file", f.path))
			continue
		}
		c, errs := ParseConfiguration(id, data)
		problems = append(problems, errs...)
		merged = merged.Merge(c)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.configs[id]; ok {
		return c
	}
	r.configs[id] = merged
	r.problems = append(r.problems, problems...)
	return merged
}

// Grammar implements textmate.Registry: the grammar with the scope name, read from its file the
// first time. A grammar that cannot be read is nil, and the reason is in Problems.
func (r *Registry) Grammar(scopeName string) *textmate.RawGrammar {
	r.mu.RLock()
	g, ok := r.parsed[scopeName]
	def := r.grammars[scopeName]
	r.mu.RUnlock()
	if ok {
		return g
	}
	if def == nil {
		return nil
	}
	data, err := def.file.read()
	var raw *textmate.RawGrammar
	if err != nil {
		r.addProblem(errors.NewErr("GrammarFileUnreadable", err).SetMetadata("scope", scopeName, "file", def.file.path))
	} else if raw, err = textmate.ParseGrammar(data); err != nil {
		r.addProblem(errors.New("GrammarFileInvalid", "a grammar file is not a valid JSON grammar", "scope", scopeName, "file", def.file.path, "cause", err.Error()))
		raw = nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.parsed[scopeName]; ok {
		return g
	}
	r.parsed[scopeName] = raw
	return raw
}

// Injections implements textmate.Registry: the scopes of the grammars that are injected into
// the grammar with the scope name, or into a grammar whose scope is the start of it (a grammar
// injected into source.js is injected into source.js.embedded too).
func (r *Registry) Injections(scopeName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	parts := strings.Split(scopeName, ".")
	for i := 1; i <= len(parts); i++ {
		out = append(out, r.injections[strings.Join(parts[:i], ".")]...)
	}
	return out
}

// NewGrammar makes the tokenizer of a language: the grammar contributed for it, with the
// embedded languages and token types of its contribution (and of the grammars injected into
// it). cache, when it is not nil, compiles the patterns. The error has the tag
// GrammarNotRegistered when no grammar is for the language and GrammarNotFound when the grammar
// cannot be read.
func (r *Registry) NewGrammar(languageID string, cache *regex.Cache) (*textmate.Grammar, error) {
	return r.NewGrammarWith(languageID, GrammarSettings{Cache: cache})
}

// GrammarSettings are what NewGrammarWith adds to a grammar.
type GrammarSettings struct {
	// Cache compiles the patterns; nil compiles them without a shared cache.
	Cache *regex.Cache
	// MaxLineBytes is editor.maxTokenizationLineLength: a line of this many bytes or more is one
	// token; zero is the default of the tokenizer (20,000).
	MaxLineBytes int
	// ScanTimeout is the longest a search of a pattern may take; zero is regex.ScanTimeout.
	ScanTimeout time.Duration
}

// HasGrammar tells whether a grammar is registered for the language.
func (r *Registry) HasGrammar(languageID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	scope, ok := r.langScope[languageID]
	return ok && r.grammars[scope] != nil
}

// NewGrammarWith is NewGrammar with settings.
func (r *Registry) NewGrammarWith(languageID string, settings GrammarSettings) (*textmate.Grammar, error) {
	cache := settings.Cache
	r.mu.RLock()
	scope, ok := r.langScope[languageID]
	def := r.grammars[scope]
	embedded := map[string]string{}
	if ok && def != nil {
		for k, v := range def.EmbeddedLanguages {
			embedded[k] = v
		}
		for _, injected := range r.injectedEmb[scope] {
			for k, v := range injected {
				embedded[k] = v
			}
		}
	}
	r.mu.RUnlock()
	if !ok || def == nil {
		return nil, errors.New("GrammarNotRegistered", "no grammar is registered for this language", "language", languageID)
	}
	opts := textmate.GrammarOptions{Cache: cache, MaxLineBytes: settings.MaxLineBytes, ScanTimeout: settings.ScanTimeout, InitialLanguage: r.Code(languageID), EmbeddedLanguages: map[string]int{}, TokenTypes: map[string]textmate.TokenType{}}
	for scope, lang := range embedded {
		if r.IsRegistered(lang) {
			opts.EmbeddedLanguages[scope] = r.Code(lang)
		}
	}
	for selector, kind := range def.TokenTypes {
		switch kind {
		case "string":
			opts.TokenTypes[selector] = textmate.TokenString
		case "other":
			opts.TokenTypes[selector] = textmate.TokenOther
		case "comment":
			opts.TokenTypes[selector] = textmate.TokenComment
		case "regex":
			opts.TokenTypes[selector] = textmate.TokenRegEx
		}
	}
	return textmate.NewGrammar(scope, r, opts)
}
