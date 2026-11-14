// Package epub is the extension EPUB Reader (SPEC §10.37 "EPUB", D55): the file editor of EPUB books, which reflows their chapters in
// a column with the editor's colors, with the contents, the font size, the chapters and the reading position remembered. Books are
// read by extensions/epub/book; nothing is installed.
package epub

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/epub/book"
)

// ID is the id of the extension, and ReaderID the id of its file editor.
const (
	ID       = "emetos.epub"
	ReaderID = "emetos.epub.reader"
)

// The settings of the reader.
const (
	SettingFontFamily = "epub.fontFamily"
	SettingFontSize   = "epub.fontSize"
)

// The bounds and the default of the font size.
const (
	MinFontSize     = 10
	MaxFontSize     = 32
	DefaultFontSize = 16
)

// The commands of the reader.
const (
	CommandToggleContents  = "emetos.epub.toggleContents"
	CommandNextChapter     = "emetos.epub.nextChapter"
	CommandPreviousChapter = "emetos.epub.previousChapter"
	CommandFontSmaller     = "emetos.epub.fontSmaller"
	CommandFontLarger      = "emetos.epub.fontLarger"
)

// whenReader is the when-clause of the commands: the reader is the active editor.
const whenReader = "activeCustomEditorId == '" + ReaderID + "'"

// Extension is the EPUB Reader in one scope. Its fields belong to the UI thread.
type Extension struct {
	api     extension.API
	ctx     context.Context
	readers []*reader
	// openBook opens a book; the tests may give another.
	openBook func(ctx context.Context, path string) (*book.Book, error)
}

// New is the Factory of the extension.
func New() extension.Extension { return &Extension{openBook: book.Open} }

// Manifest implements extension.Extension.
func (x *Extension) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "EPUB Reader", Version: "1.0.0",
		Description: "Reads EPUB books, reflowed in a column with the editor's colors.",
		Contributes: extension.Contributes{
			FileEditors: []extension.FileEditorSpec{
				{ID: ReaderID, Name: "EPUB Reader", Icon: "book", Globs: []string{"*.epub"}, Priority: extension.FileEditorDefault},
			},
			Settings: []extension.SettingSpec{
				{Key: SettingFontFamily, Type: "string", Default: "", Description: "The font of the text of books. When empty, the font of the user interface."},
				{Key: SettingFontSize, Type: "number", Default: DefaultFontSize, Minimum: ptr(float64(MinFontSize)), Maximum: ptr(float64(MaxFontSize)),
					Description: "The size of the text of books, in pixels."},
			},
			Commands: []extension.CommandSpec{
				{ID: CommandToggleContents, Title: "Toggle Contents", Category: "EPUB", When: whenReader},
				{ID: CommandNextChapter, Title: "Next Chapter", Category: "EPUB", When: whenReader},
				{ID: CommandPreviousChapter, Title: "Previous Chapter", Category: "EPUB", When: whenReader},
				{ID: CommandFontSmaller, Title: "Smaller Text", Category: "EPUB", When: whenReader},
				{ID: CommandFontLarger, Title: "Larger Text", Category: "EPUB", When: whenReader},
			},
		},
	}
}

// Activate implements extension.Extension.
func (x *Extension) Activate(ctx context.Context, api extension.API) error {
	x.api, x.ctx = api, ctx
	if err := api.FileEditors().Provide(ReaderID, extension.FileEditorProvider{Open: x.open}); err != nil {
		return err
	}
	for id, act := range map[string]func(*reader){
		CommandToggleContents:  (*reader).toggleContents,
		CommandNextChapter:     func(r *reader) { r.goToChapter(r.pos.Chapter+1, "") },
		CommandPreviousChapter: func(r *reader) { r.goToChapter(r.pos.Chapter-1, "") },
		CommandFontSmaller:     func(r *reader) { r.x.changeFontSize(-1) },
		CommandFontLarger:      func(r *reader) { r.x.changeFontSize(1) },
	} {
		if err := api.Commands().Handle(id, func(context.Context, any) error {
			if r := x.active(); r != nil {
				act(r)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// active is the reader that is the active editor of the window, or nil.
func (x *Extension) active() *reader {
	for _, r := range x.readers {
		if r.tab.Active() {
			return r
		}
	}
	return nil
}

// fontSize is the size of the text, within its bounds.
func (x *Extension) fontSize() float64 {
	size, err := extension.SettingValue[float64](x.api.Settings(), SettingFontSize)
	if err != nil || size == 0 {
		return DefaultFontSize
	}
	return min(max(size, MinFontSize), MaxFontSize)
}

// fontFamily is the font of the text; "" for the font of the user interface.
func (x *Extension) fontFamily() string {
	f, _ := extension.SettingValue[string](x.api.Settings(), SettingFontFamily)
	return f
}

// changeFontSize is A− and A+: they write epub.fontSize, and every reader reflows.
func (x *Extension) changeFontSize(step float64) {
	size := min(max(x.fontSize()+step, MinFontSize), MaxFontSize)
	if size == x.fontSize() {
		return
	}
	if err := x.api.Settings().Update(SettingFontSize, size); err != nil {
		x.api.Log().Warn("the size of the text of books cannot be saved", "err", err)
		return
	}
	for _, r := range x.readers {
		r.refresh()
	}
}

// ptr is a pointer to a value.
func ptr[T any](v T) *T { return &v }
