package folding

import (
	"context"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The kinds of region (VS Code's FoldingRangeKind).
const (
	// KindComment is a region of comment lines.
	KindComment = "comment"
	// KindImports is a region of import lines.
	KindImports = "imports"
	// KindRegion is a region between region markers.
	KindRegion = "region"
)

// Provider finds the folding regions of a text. Compute runs on a snapshot, not on the goroutine
// of the document, and stops with an error when ctx is cancelled.
type Provider interface {
	Compute(ctx context.Context, text Text, tabSize int) (*Regions, error)
}

// IndentProvider is the provider that finds the regions by indentation (VS Code's
// IndentRangeProvider): the one every language has.
type IndentProvider struct {
	// OffSide is the language's folding.offSide.
	OffSide bool
	// Markers are the patterns of the lines that start and end a region, or nil.
	Markers *Markers
	// Limit is the most regions; DefaultLimit when 0.
	Limit Limit
}

// Compute implements Provider.
func (p IndentProvider) Compute(ctx context.Context, text Text, tabSize int) (*Regions, error) {
	return computeRanges(ctx, text, tabSize, p.OffSide, p.Markers, p.Limit)
}

// snapshotText reads a buffer snapshot as a Text.
type snapshotText struct{ snap buffer.Snapshot }

func (s snapshotText) LineCount() int { return s.snap.LineCount() }

func (s snapshotText) LineContent(line int) string {
	text, _ := s.snap.LineContent(line)
	return text
}

// SnapshotText makes a Text of a snapshot.
func SnapshotText(s buffer.Snapshot) Text { return snapshotText{s} }

// UpdaterOptions tune an Updater.
type UpdaterOptions struct {
	// Delay is how long the document must be quiet before the regions are computed again; 200 ms
	// when 0.
	Delay time.Duration
}

// DefaultDelay is the default of UpdaterOptions.Delay.
const DefaultDelay = 200 * time.Millisecond

// Updater computes the regions of a document in the background: after the document has been quiet
// for the delay, a goroutine runs the provider on a snapshot, and the result goes to a function
// (which hands it to the goroutine of the editor, where Model.Update runs). A change of the
// document cancels the computation under way. A document in large file mode is never folded.
type Updater struct {
	doc      *document.Document
	provider Provider
	delay    time.Duration
	apply    func(*Regions)

	mu      sync.Mutex
	gen     uint64
	timer   *time.Timer
	cancel  context.CancelFunc
	stopped bool
	unsub   func()
}

// Attach starts an Updater that follows a document. apply is called, on a goroutine of the
// updater, with the regions of every computation that finished before the document changed again;
// it is called once at the start too. The returned function stops it. For a document in large file
// mode nothing is computed.
func Attach(doc *document.Document, provider Provider, opts UpdaterOptions, apply func(*Regions)) (stop func()) {
	if doc.LargeFile() {
		return func() {}
	}
	u := &Updater{doc: doc, provider: provider, delay: opts.Delay, apply: apply}
	if u.delay <= 0 {
		u.delay = DefaultDelay
	}
	u.unsub = doc.Subscribe(func(document.ChangeEvent) { u.schedule(u.delay) })
	u.schedule(0)
	return u.stop
}

func (u *Updater) schedule(after time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopped {
		return
	}
	u.gen++
	gen := u.gen
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
	if u.timer != nil {
		u.timer.Stop()
	}
	// The snapshot is taken now, on the goroutine of the document.
	snap := u.doc.Snapshot()
	tabSize, _ := u.doc.Indentation()
	u.timer = time.AfterFunc(after, func() { u.run(gen, snap, tabSize) })
}

func (u *Updater) run(gen uint64, snap buffer.Snapshot, tabSize int) {
	u.mu.Lock()
	if u.stopped || u.gen != gen {
		u.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.mu.Unlock()
	regions, err := u.provider.Compute(ctx, SnapshotText(snap), tabSize)
	cancel()
	if err != nil {
		return
	}
	u.mu.Lock()
	current := !u.stopped && u.gen == gen
	u.mu.Unlock()
	if current {
		u.apply(regions)
	}
}

func (u *Updater) stop() {
	u.mu.Lock()
	u.stopped = true
	if u.cancel != nil {
		u.cancel()
	}
	if u.timer != nil {
		u.timer.Stop()
	}
	u.mu.Unlock()
	u.unsub()
}
