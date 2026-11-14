package epub

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	_ "image/gif"  // the images of books
	_ "image/jpeg" // the images of books
	_ "image/png"  // the images of books
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/epub/book"
)

// The tab of the reader (SPEC §10.37 "EPUB"): the book is opened and each chapter parsed off the UI thread, with its images decoded;
// one chapter is shown at a time. The reading position (the chapter and the first block in view) is saved with the tab and in the
// extension's global state by the book's path, so that opening the book again goes back there.

// position is a place in a book: the spine item and the top block in view.
type position struct {
	Chapter int `json:"chapter"`
	Block   int `json:"block"`
}

// positionKey is the key of a book's position in the global state.
func positionKey(path string) string { return "position:" + path }

// fileStamp is what tells that a file changed: its size and the time it was written.
type fileStamp struct {
	size int64
	mod  time.Time
}

// stampOf is the stamp of the file at path; false when it cannot be read.
func stampOf(path string) (fileStamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, false
	}
	return fileStamp{size: info.Size(), mod: info.ModTime()}, true
}

// reader is one tab of the reader. Its fields belong to the UI thread.
type reader struct {
	x      *Extension
	file   extension.FileEditorFile
	tab    extension.FileEditorTab
	ctx    context.Context
	cancel context.CancelFunc
	// gen counts the reads of the book and of its chapters.
	gen     int
	loading bool
	book    *book.Book
	problem error
	deleted bool
	stamp   fileStamp
	pos     position
	// chapter is the chapter shown and its images; target is the block to scroll to once it is laid out (-1 for none).
	chapter    *book.Chapter
	chapterErr error
	images     map[string]*gui.Image
	target     int
	// contentsShown tells that the Contents panel is shown; tree holds its entries.
	contentsShown bool
	tree          *ui.TreeModel
	// note is the body of a note whose link was clicked, shown under the text until it is closed.
	note   []book.Block
	view   *readerViewState
	closed bool
}

// open is the Open of the reader.
func (x *Extension) open(file extension.FileEditorFile, tab extension.FileEditorTab) (extension.FileEditorContent, error) {
	r := &reader{x: x, file: file, tab: tab, target: -1, tree: ui.NewTreeModel(), images: map[string]*gui.Image{}}
	state := file.State()
	if len(state) == 0 {
		state, _ = x.api.GlobalState().Get(positionKey(file.Path()))
	}
	if len(state) > 0 {
		if err := json.Unmarshal(state, &r.pos); err != nil {
			x.api.Log().Debug("the position in a book cannot be read", "path", file.Path(), "err", err)
			r.pos = position{}
		}
	}
	r.target = r.pos.Block
	r.ctx, r.cancel = context.WithCancel(x.ctx)
	x.readers = append(x.readers, r)
	file.OnChange(r.changed)
	r.load()
	return extension.FileEditorContent{Build: r.build, SaveState: r.saveState, OnClose: r.close}, nil
}

// load opens the book on a goroutine of its own, and then the chapter of the position.
func (r *reader) load() {
	r.gen++
	gen, path, ctx, openBook := r.gen, r.file.Path(), r.ctx, r.x.openBook
	r.stamp, _ = stampOf(path)
	r.loading = true
	r.tab.SetBusy(true)
	go func() {
		b, err := openBook(ctx, path)
		r.x.api.Post(func() { r.loaded(gen, b, err) })
	}()
}

// loaded takes the opened book.
func (r *reader) loaded(gen int, b *book.Book, err error) {
	if r.closed || gen != r.gen {
		if b != nil {
			r.closeBook(b)
		}
		return
	}
	r.loading = false
	r.tab.SetBusy(false)
	if r.book != nil {
		r.closeBook(r.book)
	}
	r.book, r.problem, r.deleted = b, err, false
	if err != nil {
		r.refresh()
		return
	}
	var keys []string
	if err := r.tree.SetItems(tocItems(b.TOC, "", &keys)); err != nil {
		r.x.api.Log().Debug("the contents of a book cannot be shown", "err", err)
	}
	r.tree.SetExpandedKeys(keys)
	r.pos.Chapter = min(max(r.pos.Chapter, 0), len(b.Spine)-1)
	r.loadChapter(r.pos.Chapter, "", r.target)
}

// closeBook closes a book that is no longer read.
func (r *reader) closeBook(b *book.Book) {
	if err := b.Close(); err != nil {
		r.x.api.Log().Debug("a book cannot be closed", "path", r.file.Path(), "err", err)
	}
}

// loadChapter parses a chapter and decodes its images on a goroutine of its own; then it is shown at the anchor, or the block.
func (r *reader) loadChapter(i int, anchor string, block int) {
	r.gen++
	gen, b := r.gen, r.book
	r.loading = true
	r.tab.SetBusy(true)
	go func() {
		ch, err := b.Chapter(i)
		images := map[string]*gui.Image{}
		if err == nil {
			for _, p := range imagePaths(ch.Blocks, nil) {
				if img, ok := decodeImage(b, p); ok {
					images[p] = img
				}
			}
		}
		r.x.api.Post(func() { r.chapterLoaded(gen, i, ch, images, anchor, block, err) })
	}()
}

// chapterLoaded shows a parsed chapter.
func (r *reader) chapterLoaded(gen, i int, ch *book.Chapter, images map[string]*gui.Image, anchor string, block int, err error) {
	if r.closed || gen != r.gen {
		return
	}
	r.loading = false
	r.tab.SetBusy(false)
	r.pos, r.chapter, r.chapterErr, r.images, r.note = position{Chapter: i}, ch, err, images, nil
	r.target = max(block, 0)
	if ch != nil && anchor != "" {
		if at, ok := ch.Anchors[anchor]; ok {
			r.target = at
		}
	}
	r.pos.Block = r.target
	r.remember()
	r.refresh()
}

// imagePaths are the paths of the images of blocks and of the blocks in them, in order.
func imagePaths(blocks []book.Block, out []string) []string {
	for _, b := range blocks {
		if b.Kind == book.Image {
			out = append(out, b.Image.Path)
		}
		out = imagePaths(b.Children, out)
		for _, item := range b.Items {
			out = imagePaths(item, out)
		}
	}
	return out
}

// decodeImage reads and decodes an image of the book (PNG, JPEG, GIF); false when it cannot be shown (its text is shown then).
func decodeImage(b *book.Book, p string) (*gui.Image, bool) {
	data, err := b.Resource(p)
	if err != nil {
		return nil, false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	pic, err := ui.NewPicture(img)
	if err != nil {
		return nil, false
	}
	return pic, true
}

// goToChapter shows a chapter (from 0) at an anchor ("" for its top); a chapter beyond the first or the last does nothing.
func (r *reader) goToChapter(i int, anchor string) {
	if r.book == nil || i < 0 || i >= len(r.book.Spine) {
		return
	}
	if r.chapter != nil && i == r.pos.Chapter {
		r.target = 0
		if at, ok := r.chapter.Anchors[anchor]; ok {
			r.target = at
		}
		r.note = nil
		r.refresh()
		return
	}
	r.loadChapter(i, anchor, 0)
}

// openLink follows a link: a page of the web opens with the desktop; a link inside the book goes to its chapter and anchor, and a link
// to a note of the chapter shows the note.
func (r *reader) openLink(link string, external bool) {
	if external {
		if err := r.x.api.Workspace().OpenURI(r.x.ctx, link); err != nil {
			r.x.api.Log().Warn("a link of a book cannot be opened", "uri", link, "err", err)
		}
		return
	}
	if r.book == nil {
		return
	}
	p, anchor, _ := strings.Cut(link, "#")
	if r.chapter != nil && p == r.chapter.Path {
		if note, ok := r.chapter.Notes[anchor]; ok {
			r.note = note
			r.refresh()
			return
		}
	}
	for i, s := range r.book.Spine {
		if s.Path == p {
			r.goToChapter(i, anchor)
			return
		}
	}
}

// toggleContents shows or hides the Contents panel.
func (r *reader) toggleContents() {
	r.contentsShown = !r.contentsShown
	r.refresh()
}

// contentsRow is the data of a row of the contents.
type contentsRow struct {
	path, anchor string
}

// tocItems are the rows of entries of the contents; keys gets the keys of the rows that have children.
func tocItems(entries []book.TOCEntry, parent string, keys *[]string) []ui.TreeItem {
	out := make([]ui.TreeItem, 0, len(entries))
	for i, e := range entries {
		key := parent + "/" + strconv.Itoa(i)
		item := ui.TreeItem{Key: key, Data: contentsRow{path: e.Path, anchor: e.Anchor}, Label: e.Title}
		if len(e.Children) > 0 {
			item.Children = tocItems(e.Children, key, keys)
			*keys = append(*keys, key)
		}
		out = append(out, item)
	}
	return out
}

// openEntry goes to an entry of the contents.
func (r *reader) openEntry(node ui.TreeNode) {
	if row, ok := node.Data.(contentsRow); ok {
		link := row.path
		if row.anchor != "" {
			link += "#" + row.anchor
		}
		r.openLink(link, false)
	}
}

// setTopBlock is the first block in view, from the view: the position follows the reading.
func (r *reader) setTopBlock(i int) {
	if r.target < 0 {
		r.pos.Block = i
	}
}

// remember saves the position in the global state, by the book's path.
func (r *reader) remember() {
	if err := r.x.api.GlobalState().Set(positionKey(r.file.Path()), r.pos); err != nil {
		r.x.api.Log().Debug("the position in a book cannot be remembered", "path", r.file.Path(), "err", err)
	}
}

// changed is the file changed or deleted on disk; a change that leaves the size and the time as they were reads nothing.
func (r *reader) changed(c extension.FileEditorChange) {
	if c.Deleted {
		r.gen++
		r.loading, r.deleted = false, true
		r.tab.SetBusy(false)
		r.refresh()
		return
	}
	if s, ok := stampOf(r.file.Path()); ok && s == r.stamp && !r.deleted && !r.loading {
		return
	}
	r.target = r.pos.Block
	r.load()
}

// saveState is the SaveState of the tab; the position is remembered by the book's path too.
func (r *reader) saveState() (any, error) {
	if r.book != nil {
		r.remember()
	}
	return r.pos, nil
}

// close is the OnClose of the tab.
func (r *reader) close() {
	if r.book != nil {
		r.remember()
		r.closeBook(r.book)
	}
	r.closed = true
	r.cancel()
	for i, o := range r.x.readers {
		if o == r {
			r.x.readers = append(r.x.readers[:i], r.x.readers[i+1:]...)
			break
		}
	}
}

// refresh builds the view again.
func (r *reader) refresh() {
	if r.view != nil {
		r.view.changed()
	}
}

// problemText is what a book that cannot be shown says.
func problemText(err error) string {
	e, ok := errors.FromError(err)
	if !ok {
		return "This book cannot be opened: " + err.Error()
	}
	switch e.Tag {
	case "EPUBTooManyEntries", "EPUBEntryTooLarge", "EPUBTooLarge", "EPUBZipBomb", "EPUBProtected", "EPUBNoContainer", "EPUBPathInvalid":
		return e.Message
	case "EPUBNotZip":
		return "This file is not an EPUB book."
	}
	return "This book cannot be opened: " + strings.TrimSuffix(e.Message, ".") + "."
}
