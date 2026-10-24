package theme

import (
	_ "embed" // the Seti mapping
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// The file icon themes (SPEC §10.8): VS Code's icon theme format, a font plus a JSON mapping of file
// names, extensions, language ids and folder names to the characters of the font, with a section for
// light themes and one for high contrast themes.
//
// Ported from VS Code: src/vs/workbench/services/themes/browser/fileIconThemeData.ts (the document and
// the rules it makes the CSS from) and src/vs/editor/common/services/getIconClasses.ts (the classes of
// a file)

// seti is the mapping of VS Code's default icon theme.
//
//go:embed iconassets/vs-seti-icon-theme.json
var setiMapping []byte

// DefaultFileIconTheme is the id of the file icon theme that is used when the setting does not choose
// one: VS Code's Seti.
const DefaultFileIconTheme = "vs-seti"

// FileIconKind tells what an icon is for.
type FileIconKind int

const (
	// FileIconFile is a file.
	FileIconFile FileIconKind = iota
	// FileIconFolder is a folder.
	FileIconFolder
	// FileIconRootFolder is the folder of the window.
	FileIconRootFolder
)

// FileIconQuery says which icon to look for.
type FileIconQuery struct {
	// Kind is what the icon is for.
	Kind FileIconKind
	// Name is the name of the file or folder, without its folder.
	Name string
	// Parent is the name of the folder it is in, for the names the theme gives as "folder/name"; it may
	// be empty.
	Parent string
	// LanguageID is the language of a file; empty or "plaintext" is none.
	LanguageID string
	// Expanded says that a folder is open in the tree.
	Expanded bool
	// Type is the type of the color theme: it chooses the light and the high contrast sections.
	Type Type
}

// FileIcon is the icon a theme gives to a file or a folder.
type FileIcon struct {
	// Glyph is the character of the icon font.
	Glyph rune
	// Family is the family of the font the glyph is in (the id of the font in the theme).
	Family string
	// Color is the color of the glyph when HasColor.
	Color Color
	// HasColor says that the theme gives the glyph a color; without one the glyph takes the color of the
	// text.
	HasColor bool
	// Scale is the size of the glyph in the size of the text (1.5 for the "150%" of Seti).
	Scale float64
}

// iconDefinition is an entry of iconDefinitions.
type iconDefinition struct {
	FontCharacter string `json:"fontCharacter"`
	FontColor     string `json:"fontColor"`
	FontSize      string `json:"fontSize"`
	FontID        string `json:"fontId"`
}

// iconFont is an entry of fonts.
type iconFont struct {
	ID   string `json:"id"`
	Size string `json:"size"`
}

// iconAssociations is the part of the document that gives icons to names: the document itself, and its
// light and highContrast sections.
type iconAssociations struct {
	Folder                  string            `json:"folder"`
	FolderExpanded          string            `json:"folderExpanded"`
	RootFolder              string            `json:"rootFolder"`
	RootFolderExpanded      string            `json:"rootFolderExpanded"`
	File                    string            `json:"file"`
	FolderNames             map[string]string `json:"folderNames"`
	FolderNamesExpanded     map[string]string `json:"folderNamesExpanded"`
	RootFolderNames         map[string]string `json:"rootFolderNames"`
	RootFolderNamesExpanded map[string]string `json:"rootFolderNamesExpanded"`
	LanguageIDs             map[string]string `json:"languageIds"`
	FileExtensions          map[string]string `json:"fileExtensions"`
	FileNames               map[string]string `json:"fileNames"`
}

// iconDocument is a file icon theme file.
type iconDocument struct {
	iconAssociations
	IconDefinitions map[string]iconDefinition `json:"iconDefinitions"`
	Fonts           []iconFont                `json:"fonts"`
	Light           *iconAssociations         `json:"light"`
	HighContrast    *iconAssociations         `json:"highContrast"`
}

// FileIconTheme is a file icon theme: the mapping of a theme file, ready to answer IconFor. It never
// changes after it is made, so it is safe for concurrent use.
type FileIconTheme struct {
	id    string
	doc   iconDocument
	fonts map[string]float64
	// base, light and hc are the sections, with their names lowercased.
	base, light, hc *normalized
}

// normalized is an iconAssociations with the keys lowercased and split into the parts the lookup needs.
type normalized struct {
	iconAssociations
	// folderNames, folderNamesExpanded, rootNames and rootNamesExpanded are by name, then by parent
	// ("" for a rule that names no parent).
	folderNames, folderNamesExpanded, rootNames, rootNamesExpanded map[string]map[string]string
	// fileNames is by name, then by parent.
	fileNames map[string]map[string]string
	// extensions is by extension (the part after the first dot of the suffix), then by parent.
	extensions map[string]map[string]string
}

// ParseFileIconTheme reads the file of a file icon theme (VS Code's format). The id is what the
// setting calls the theme. It fails with the tag FileIconThemeInvalid when the text is not a JSON
// object that has iconDefinitions.
func ParseFileIconTheme(id string, data []byte) (*FileIconTheme, error) {
	var doc iconDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.NewErr("FileIconThemeInvalid", err).SetMetadata("theme", id)
	}
	if doc.IconDefinitions == nil {
		return nil, errors.New("FileIconThemeInvalid", "the file icon theme has no iconDefinitions", "theme", id)
	}
	t := &FileIconTheme{id: id, doc: doc, fonts: map[string]float64{}}
	for _, f := range doc.Fonts {
		t.fonts[f.ID] = fontScale(f.Size, 1.5)
	}
	t.base = normalize(doc.iconAssociations)
	if doc.Light != nil {
		t.light = normalize(*doc.Light)
	}
	if doc.HighContrast != nil {
		t.hc = normalize(*doc.HighContrast)
	}
	return t, nil
}

// fontScale reads a font size such as "150%" or "1.5em" as a factor of the size of the text.
func fontScale(size string, fallback float64) float64 {
	size = strings.TrimSpace(size)
	switch {
	case strings.HasSuffix(size, "%"):
		if v, err := strconv.ParseFloat(strings.TrimSuffix(size, "%"), 64); err == nil && v > 0 {
			return v / 100
		}
	case strings.HasSuffix(size, "em"):
		if v, err := strconv.ParseFloat(strings.TrimSuffix(size, "em"), 64); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

// BundledFileIconTheme loads the file icon theme with an id: "vs-seti" is the Seti theme that comes with
// Emetos. Another id fails with the tag FileIconThemeNotFound.
func BundledFileIconTheme(id string) (*FileIconTheme, error) {
	if id != DefaultFileIconTheme {
		return nil, errors.New("FileIconThemeNotFound", "there is no file icon theme with that id", "theme", id)
	}
	return ParseFileIconTheme(id, setiMapping)
}

// ID is the id of the theme.
func (t *FileIconTheme) ID() string { return t.id }

// splitParent splits a key "folder/name" into the folder and the name; a key without a slash has no
// folder (VS Code's handleParentFolder).
func splitParent(key string) (parent, name string) {
	key = strings.ToLower(key)
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

// byNameAndParent groups a map of "folder/name" keys by name and then by folder.
func byNameAndParent(m map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(m))
	for key, def := range m {
		parent, name := splitParent(key)
		if out[name] == nil {
			out[name] = map[string]string{}
		}
		out[name][parent] = def
	}
	return out
}

// normalize lowercases and groups the keys of a section; jsonc has the icon of json when it has none of
// its own.
func normalize(a iconAssociations) *normalized {
	n := &normalized{iconAssociations: a}
	if a.LanguageIDs != nil {
		langs := make(map[string]string, len(a.LanguageIDs)+1)
		for k, v := range a.LanguageIDs {
			langs[k] = v
		}
		if _, ok := langs["jsonc"]; !ok && langs["json"] != "" {
			langs["jsonc"] = langs["json"]
		}
		n.LanguageIDs = langs
	}
	n.folderNames = byNameAndParent(a.FolderNames)
	n.folderNamesExpanded = byNameAndParent(a.FolderNamesExpanded)
	n.rootNames = byNameAndParent(a.RootFolderNames)
	n.rootNamesExpanded = byNameAndParent(a.RootFolderNamesExpanded)
	n.fileNames = byNameAndParent(a.FileNames)
	n.extensions = byNameAndParent(a.FileExtensions)
	return n
}

// candidate is an icon definition that a rule of the theme offers for a name, with the specificity of
// the CSS rule VS Code makes of it: the icon with the highest one wins, as in the browser.
type candidate struct {
	def   string
	score int
	// order breaks a tie: a file name beats an extension, which beats a language, which beats the
	// default (the order the rules are written in the style sheet).
	order int
}

const (
	orderDefault = iota
	orderLanguage
	orderExtension
	orderName
)

// IconFor chooses the icon of a file or folder, and reports false when the theme has none for it.
//
// VS Code makes a CSS rule of every association and lets the specificity of the selectors choose, so
// the choice is: for a file, a file name beats an extension and an extension beats a language
// (an extension with more dots beats one with fewer, and a file name with more dots beats one with
// fewer), and the default file icon is the last; for a folder, a name beats the default, and the
// "expanded" rules beat the plain ones when the folder is open; a section for the type of the color
// theme (light, high contrast) beats the document's own rules of the same kind.
func (t *FileIconTheme) IconFor(q FileIconQuery) (FileIcon, bool) {
	var cands []candidate
	add := func(def string, score, order int) {
		if def != "" {
			cands = append(cands, candidate{def: def, score: score, order: order})
		}
	}
	// the sections that apply, with the extra class the qualifier of a section adds
	type section struct {
		n     *normalized
		bonus int
	}
	sections := []section{{t.base, 0}}
	switch q.Type {
	case TypeLight:
		if t.light != nil {
			sections = append(sections, section{t.light, 1})
		}
	case TypeHCDark, TypeHCLight:
		if t.hc != nil {
			sections = append(sections, section{t.hc, 1})
		}
	}
	name := strings.ToLower(q.Name)
	parent := strings.ToLower(q.Parent)
	for _, s := range sections {
		switch q.Kind {
		case FileIconFile:
			t.fileCandidates(s.n, s.bonus, name, parent, q.LanguageID, add)
		case FileIconFolder:
			folderCandidates(s.n, s.bonus, name, parent, q.Expanded, false, add)
		case FileIconRootFolder:
			folderCandidates(s.n, s.bonus, name, parent, q.Expanded, true, add)
		}
	}
	// the highest specificity wins, a later rule on a tie
	best := -1
	for i, c := range cands {
		if _, ok := t.resolve(c.def); !ok {
			continue // a definition with no character draws nothing
		}
		if best < 0 || c.score > cands[best].score || (c.score == cands[best].score && c.order >= cands[best].order) {
			best = i
		}
	}
	if best < 0 {
		return FileIcon{}, false
	}
	return t.resolve(cands[best].def)
}

// fileCandidates adds the rules of a section that apply to a file. The score of a rule is the number of
// class selectors of its CSS selector: ".show-file-icons" and ".file-icon" in all, one for the language
// or for the name and the extension classes, and one more for the qualifier of a section.
func (t *FileIconTheme) fileCandidates(n *normalized, bonus int, name, parent, languageID string, add func(def string, score, order int)) {
	add(n.File, 2+bonus, orderDefault)
	if languageID != "" && languageID != "plaintext" {
		add(n.LanguageIDs[languageID], 3+bonus, orderLanguage)
	}
	// a rule for an extension needs a dot before it in the name: "a.test.go" has "test.go" and "go"
	for i := 0; i < len(name); i++ {
		if name[i] != '.' || i == len(name)-1 {
			continue
		}
		suffix := name[i+1:]
		if byParent, ok := n.extensions[suffix]; ok {
			segments := strings.Count(suffix, ".") + 1
			addParent(byParent, parent, 3+segments+bonus, orderExtension, add)
		}
	}
	if byParent, ok := n.fileNames[name]; ok {
		segments := strings.Count(name, ".") + 1
		addParent(byParent, parent, 4+segments+bonus, orderName, add)
	}
}

// addParent adds the rule that names no folder, and the one that names the folder the file is in (which
// has one more class).
func addParent(byParent map[string]string, parent string, score, order int, add func(def string, score, order int)) {
	add(byParent[""], score, order)
	if parent != "" {
		add(byParent[parent], score+1, order)
	}
}

// folderCandidates adds the rules of a section that apply to a folder. The "expanded" rules have the
// selector of an open node of the tree in front, which is four classes more.
func folderCandidates(n *normalized, bonus int, name, parent string, expanded, root bool, add func(def string, score, order int)) {
	const open = 4
	plain, plainOpen := n.Folder, n.FolderExpanded
	names, namesOpen := n.folderNames, n.folderNamesExpanded
	if root {
		plain, plainOpen = firstNonEmpty(n.RootFolder, n.Folder), firstNonEmpty(n.RootFolderExpanded, n.FolderExpanded)
		names, namesOpen = n.rootNames, n.rootNamesExpanded
	}
	add(plain, 2+bonus, orderDefault)
	if byParent, ok := names[name]; ok {
		addParent(byParent, parent, 3+bonus, orderName, add)
	}
	if !expanded {
		return
	}
	add(plainOpen, 2+open+bonus, orderDefault)
	if byParent, ok := namesOpen[name]; ok {
		addParent(byParent, parent, 3+open+bonus, orderName, add)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// resolve makes the icon of a definition.
func (t *FileIconTheme) resolve(def string) (FileIcon, bool) {
	d, ok := t.doc.IconDefinitions[def]
	if !ok {
		return FileIcon{}, false
	}
	glyph, ok := parseFontCharacter(d.FontCharacter)
	if !ok {
		return FileIcon{}, false
	}
	icon := FileIcon{Glyph: glyph, Scale: 1.5}
	family := d.FontID
	if family == "" && len(t.doc.Fonts) > 0 {
		family = t.doc.Fonts[0].ID
	}
	icon.Family = family
	if scale, ok := t.fonts[family]; ok {
		icon.Scale = scale
	}
	if d.FontSize != "" {
		icon.Scale = fontScale(d.FontSize, icon.Scale)
	}
	if d.FontColor != "" {
		if c, ok := ParseColor(d.FontColor); ok {
			icon.Color, icon.HasColor = c, true
		}
	}
	return icon, true
}

// parseFontCharacter reads the fontCharacter of a definition: a backslash and hexadecimal digits
// ("\E001"), or the character itself.
func parseFontCharacter(s string) (rune, bool) {
	if s == "" {
		return 0, false
	}
	if strings.HasPrefix(s, "\\") {
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil {
			return 0, false
		}
		return rune(v), true
	}
	for _, r := range s {
		return r, true
	}
	return 0, false
}
