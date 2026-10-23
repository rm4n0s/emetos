package indent

import "github.com/rm4n0s/emetos/internal/core/document"

// docLines reads the text of a document.
type docLines struct{ doc *document.Document }

func (d docLines) LineCount() int { return d.doc.Snapshot().LineCount() }

func (d docLines) LineContent(line int) string {
	text, err := d.doc.Snapshot().LineContent(line)
	if err != nil {
		return ""
	}
	return text
}

// AttachLevels makes the Levels of a document, which follow its changes until the function it
// returns is called.
func AttachLevels(doc *document.Document, tabSize int) (*Levels, func()) {
	lv := NewLevels(docLines{doc}, tabSize)
	stop := doc.Subscribe(func(ev document.ChangeEvent) {
		if ev.Cause == document.CauseSetText {
			lv.Reset()
			return
		}
		changes := make([]LineChange, len(ev.Changes))
		for i, c := range ev.Changes {
			changes[i] = LineChange{From: c.OldRange.Start.Line, To: c.OldRange.End.Line, Count: c.NewRange.End.Line - c.NewRange.Start.Line + 1}
		}
		lv.Apply(changes)
	})
	return lv, stop
}

// TextLines adapts a document to a LineSource.
func TextLines(doc *document.Document) LineSource { return docLines{doc} }
