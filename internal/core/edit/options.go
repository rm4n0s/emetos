package edit

// Options is everything the cursor and editing functions read besides the text
// (SPEC §7.2). Later steps add fields; none of the functions take the settings
// one by one.
type Options struct {
	// TabSize is editor.tabSize.
	TabSize int
	// InsertSpaces is editor.insertSpaces.
	InsertSpaces bool
	// WordSeparators is the classifier of editor.wordSeparators.
	WordSeparators *WordClassifier
	// MultiCursorMergeOverlapping is editor.multiCursorMergeOverlapping: cursors
	// that overlap or touch become one.
	MultiCursorMergeOverlapping bool
}

// DefaultOptions are VS Code's defaults.
func DefaultOptions() Options {
	return Options{
		TabSize:                     4,
		InsertSpaces:                true,
		WordSeparators:              NewWordClassifier(DefaultWordSeparators),
		MultiCursorMergeOverlapping: true,
	}
}

// tabSize is the tab size to use: a size below 1 counts as 1.
func (o Options) tabSize() int { return tabSizeOf(o.TabSize) }

// words is the word classifier, the default one when none is set.
func (o Options) words() *WordClassifier {
	if o.WordSeparators == nil {
		return NewWordClassifier(DefaultWordSeparators)
	}
	return o.WordSeparators
}
