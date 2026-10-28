package terminal

import (
	"bytes"
	"context"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/pty"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// pieceLog records the pieces of output a terminal publishes.
type pieceLog struct {
	mu     sync.Mutex
	pieces []outputEvent
	inputs []inputEvent
	sizes  [][2]int
}

func (p *pieceLog) observer(interval time.Duration) *observer {
	return &observer{
		output: func(seq uint64, data string) {
			p.mu.Lock()
			p.pieces = append(p.pieces, outputEvent{Seq: seq, Data: data})
			p.mu.Unlock()
		},
		input: func(data, by string, hidden bool) {
			p.mu.Lock()
			p.inputs = append(p.inputs, inputEvent{Data: data, By: by, Hidden: hidden})
			p.mu.Unlock()
		},
		resized:  func(cols, rows int) { p.mu.Lock(); p.sizes = append(p.sizes, [2]int{cols, rows}); p.mu.Unlock() },
		interval: interval,
	}
}

func (p *pieceLog) all() []outputEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]outputEvent(nil), p.pieces...)
}

// fakeTerminal is a terminal over a fake program that writes what a test gives it.
func fakeTerminal(t *testing.T, rows, cols int, obs *observer) (*Terminal, *fakeProcess) {
	t.Helper()
	f := newFakeProcess()
	term, err := newTerminal(t.Context(), terminalOpts{id: 1, profile: profile{name: "fake", path: "fake"}, scrollback: 200, rows: rows, cols: cols,
		start: func(context.Context, pty.Options) (process, error) { return f, nil }, obs: obs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Kill(); term.Wait() })
	return term, f
}

// randomOutput makes the chunks a program might write: text in several scripts, ill-formed UTF-8, line feeds, colors, cursor
// motion, erasing, scroll regions, the alternate screen, titles and links. A chunk never ends in the middle of an escape
// sequence (the snapshot, like Serialize, does not carry a half-read sequence), but it may end in the middle of a character.
func randomOutput(rng *rand.Rand, chunks int) [][]byte {
	pieces := []func() string{
		func() string { return "plain text " + strconv.Itoa(rng.Intn(1000)) },
		func() string { return "日本語の文字 " },
		func() string { return "emoji 😀👍🏽 é " },
		func() string { return "\r\n" },
		func() string {
			return "\x1b[" + strconv.Itoa(30+rng.Intn(8)) + ";" + strconv.Itoa(40+rng.Intn(8)) + "m"
		},
		func() string { return "\x1b[1;4m bold underline \x1b[0m" },
		func() string { return "\x1b[38;2;" + strconv.Itoa(rng.Intn(256)) + ";10;20m truecolor \x1b[39m" },
		func() string {
			return "\x1b[" + strconv.Itoa(1+rng.Intn(10)) + ";" + strconv.Itoa(1+rng.Intn(40)) + "H"
		},
		func() string { return "\x1b[2K" },
		func() string { return "\x1b[" + strconv.Itoa(1+rng.Intn(5)) + "A" },
		func() string { return "\x1b[3;12r" },
		func() string { return "\x1b[r" },
		func() string { return "\x1b[?1049h" },
		func() string { return "\x1b[?1049l" },
		func() string { return "\x1b[?25l" },
		func() string { return "\x1b[?25h" },
		func() string { return "\x1b]0;title " + strconv.Itoa(rng.Intn(100)) + "\x07" },
		func() string {
			return "\x1b]8;;http://example.com/" + strconv.Itoa(rng.Intn(9)) + "\x1b\\link\x1b]8;;\x1b\\"
		},
		func() string { return "\xff\xfe" },
		func() string { return "\xe2\x82" }, // a cut euro sign
		func() string { return "\xf0\x9f" },
		func() string { return "\xc0\x80 \xed\xa0\x80 " },
		func() string { return "tab\there\b\b" },
		func() string { return "\x1b[1S\x1b[1T" },
	}
	out := make([][]byte, 0, chunks)
	for range chunks {
		var b bytes.Buffer
		for range 1 + rng.Intn(6) {
			b.WriteString(pieces[rng.Intn(len(pieces))]())
		}
		out = append(out, b.Bytes())
	}
	return out
}

// split cuts a chunk in the middle of a character now and then, as reads of a pseudo-terminal do.
func split(rng *rand.Rand, chunk []byte) [][]byte {
	if len(chunk) < 4 || rng.Intn(4) != 0 {
		return [][]byte{chunk}
	}
	// only a plain-text chunk may be cut anywhere; one with an escape sequence keeps its sequences whole, so cut at the end of
	// the last character
	cut := len(chunk) - 1 - rng.Intn(3)
	if bytes.IndexByte(chunk, 0x1b) >= 0 || cut <= 0 {
		return [][]byte{chunk}
	}
	return [][]byte{chunk[:cut], chunk[cut:]}
}

// follower is a terminal that follows another through its snapshot and its pieces of output.
type follower struct {
	em  *emulator.Emulator
	seq uint64
	// gaps counts the new snapshots it had to ask for.
	gaps int
}

func (f *follower) fromSnapshot(term *Terminal) {
	data, cols, rows, seq := term.Snapshot()
	f.em = emulator.New(rows, cols, emulator.Options{Scrollback: 200})
	_, _ = f.em.Write(data)
	f.seq = seq
}

// apply feeds a piece; a piece that is not the next one is a gap, and the follower reads a snapshot again (a piece it has seen is
// ignored).
func (f *follower) apply(term *Terminal, e outputEvent) {
	switch {
	case e.Seq <= f.seq:
	case e.Seq == f.seq+1:
		_, _ = f.em.Write([]byte(e.Data))
		f.seq = e.Seq
	default:
		f.gaps++
		f.fromSnapshot(term)
	}
}

// sameScreen compares two terminals by what they show: the size, the cursor, the modes that decide what is drawn, and the text,
// the style and the hyperlink of every cell of the scrollback and the screen. It returns "" when they are the same, else where
// they differ. The flags that only say a row is the continuation of another (the soft wrap) are not compared.
func sameScreen(a, b *emulator.Emulator) string {
	ar, ac := a.Size()
	br, bc := b.Size()
	if ar != br || ac != bc {
		return "size " + strconv.Itoa(ac) + "x" + strconv.Itoa(ar) + " against " + strconv.Itoa(bc) + "x" + strconv.Itoa(br)
	}
	if a.LineCount() != b.LineCount() {
		return "lines " + strconv.Itoa(a.LineCount()) + " against " + strconv.Itoa(b.LineCount())
	}
	var fa, fb emulator.Frame
	a.ReadLines(0, a.LineCount(), &fa)
	b.ReadLines(0, b.LineCount(), &fb)
	if fa.Cursor.X != fb.Cursor.X || fa.Cursor.Y != fb.Cursor.Y || fa.Cursor.Visible != fb.Cursor.Visible || fa.Modes.AltScreen != fb.Modes.AltScreen {
		return "cursor " + strconv.Itoa(fa.Cursor.X) + "," + strconv.Itoa(fa.Cursor.Y) + " against " + strconv.Itoa(fb.Cursor.X) + "," + strconv.Itoa(fb.Cursor.Y)
	}
	for y := 0; y < fa.Rows; y++ {
		for x := 0; x < fa.Cols; x++ {
			ca, cb := fa.Cell(y, x), fb.Cell(y, x)
			sa, sb := fa.StyleOf(ca), fb.StyleOf(cb)
			sa.Link, sb.Link = 0, 0
			la, lb := fa.LinkOf(fa.StyleOf(ca)), fb.LinkOf(fb.StyleOf(cb))
			if fa.Text(y, x) != fb.Text(y, x) || sa != sb || la != lb {
				return "cell " + strconv.Itoa(x) + "," + strconv.Itoa(y) + ": " + strconv.Quote(fa.Text(y, x)) + " against " + strconv.Quote(fb.Text(y, x))
			}
		}
	}
	return ""
}

func TestSnapshotAndPiecesGiveTheHostsCells(t *testing.T) {
	tests := []struct {
		name string
		// loseEvery drops every n-th piece on its way to the follower (0 keeps them all)
		loseEvery int
	}{
		{"no piece lost", 0},
		{"a gap in the pieces", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for seed := int64(1); seed <= 40; seed++ {
				rng := rand.New(rand.NewSource(seed))
				log := &pieceLog{}
				term, proc := fakeTerminal(t, 12, 40, log.observer(time.Millisecond))
				chunks := randomOutput(rng, 150)
				var fol follower
				fol.fromSnapshot(term) // the terminal is empty: the first snapshot is the empty screen
				done := make(chan struct{})
				go func() {
					defer close(done)
					for _, c := range chunks {
						for _, part := range split(rng, c) {
							proc.out <- part
						}
						if rng.Intn(10) == 0 {
							time.Sleep(time.Duration(rng.Intn(3)) * time.Millisecond)
						}
					}
					proc.out <- []byte("\x1b]0;end\x07")
				}()
				// the follower reads a snapshot in the middle of the output and follows from there
				mid := false
				seen := 0
				deadline := time.Now().Add(20 * time.Second)
				for term.Meta().title != "end" {
					if time.Now().After(deadline) {
						t.Fatalf("seed %d: the output did not end", seed)
					}
					if !mid && len(log.all()) > 3 {
						mid = true
						fol.fromSnapshot(term)
					}
					time.Sleep(time.Millisecond)
				}
				<-done
				term.flushOutput(false)
				lostAt, pendingLoss := uint64(0), false
				for i, e := range log.all() {
					seen++
					if tt.loseEvery > 0 && i%tt.loseEvery == 1 {
						if e.Seq > fol.seq && !pendingLoss {
							lostAt, pendingLoss = e.Seq, true
						}
						continue
					}
					gaps := fol.gaps
					fol.apply(term, e)
					if pendingLoss && e.Seq > lostAt {
						if fol.gaps != gaps+1 {
							t.Errorf("seed %d: the piece %d was lost and the follower noticed no gap at the piece %d", seed, lostAt, e.Seq)
						}
						pendingLoss = false
					}
				}
				if pendingLoss {
					fol.fromSnapshot(term) // the last pieces were the ones that were lost: the follower is told by the next piece or the reconnect
				}
				if diff := sameScreen(term.Emulator(), fol.em); diff != "" {
					t.Fatalf("seed %d: the follower's screen differs from the host's after %d pieces (%d gaps): %s", seed, seen, fol.gaps, diff)
				}
			}
		})
	}
}

func TestPiecesAreInOrderValidTextAndNotLargerThanTheLimit(t *testing.T) {
	log := &pieceLog{}
	term, proc := fakeTerminal(t, 24, 80, log.observer(time.Hour))
	// 1 MiB of text in chunks of 10 KiB: a piece is published when it reaches 64 KiB, however long the timer is
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 640)
	for range 100 {
		proc.out <- chunk
	}
	proc.out <- []byte("\x1b]0;end\x07")
	deadline := time.Now().Add(20 * time.Second)
	for term.Meta().title != "end" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	term.flushOutput(false)
	var total int
	for i, e := range log.all() {
		if e.Seq != uint64(i+1) {
			t.Fatalf("the piece %d has seq %d", i, e.Seq)
		}
		if !utf8.ValidString(e.Data) {
			t.Errorf("the piece %d is not valid UTF-8", e.Seq)
		}
		if len(e.Data) > outputMaxBytes+len(chunk) {
			t.Errorf("the piece %d is %d bytes, over %d", e.Seq, len(e.Data), outputMaxBytes+len(chunk))
		}
		total += len(e.Data)
	}
	if want := 100*len(chunk) + len("\x1b]0;end\x07"); total != want {
		t.Errorf("the pieces hold %d bytes of the %d written", total, want)
	}
	if n, least := len(log.all()), total/(outputMaxBytes+len(chunk)); n < least {
		t.Errorf("%d bytes came in %d pieces; pieces of at most %d bytes make at least %d", total, n, outputMaxBytes+len(chunk), least)
	}
}

func TestThePieceWaitsForTheTimerAndASnapshotPublishesIt(t *testing.T) {
	log := &pieceLog{}
	term, proc := fakeTerminal(t, 24, 80, log.observer(60*time.Millisecond))
	proc.out <- []byte("hello")
	waitText(t, term, "hello")
	if got := log.all(); len(got) != 0 {
		t.Fatalf("a piece was published before the interval: %+v", got)
	}
	data, cols, rows, seq := term.Snapshot()
	if seq != 1 || cols != 80 || rows != 24 || !bytes.Contains(data, []byte("hello")) {
		t.Errorf("snapshot: seq %d, %dx%d, %q", seq, cols, rows, data)
	}
	if got := log.all(); len(got) != 1 || got[0].Data != "hello" || got[0].Seq != 1 {
		t.Errorf("the snapshot did not publish the piece that waited: %+v", got)
	}
	// the timer comes later and finds nothing to publish
	time.Sleep(120 * time.Millisecond)
	if got := log.all(); len(got) != 1 {
		t.Errorf("a piece was published twice: %+v", got)
	}
	proc.out <- []byte(" again")
	deadline := time.Now().Add(5 * time.Second)
	for len(log.all()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := log.all(); len(got) != 2 || got[1].Data != " again" || got[1].Seq != 2 {
		t.Errorf("the second piece: %+v", got)
	}
}

func TestACharacterCutBetweenReadsIsOneCharacterInThePieces(t *testing.T) {
	log := &pieceLog{}
	term, proc := fakeTerminal(t, 24, 80, log.observer(time.Millisecond))
	euro := []byte("€") // E2 82 AC
	proc.out <- append([]byte("a"), euro[:2]...)
	time.Sleep(30 * time.Millisecond)
	proc.out <- append(append([]byte(nil), euro[2:]...), "b\xe2\x82"...)
	time.Sleep(30 * time.Millisecond)
	proc.out <- []byte("c")
	waitText(t, term, "c")
	time.Sleep(30 * time.Millisecond)
	var got string
	for _, e := range log.all() {
		got += e.Data
	}
	if want := "a€b�c"; got != want {
		t.Errorf("the pieces hold %q, want %q", got, want)
	}
}

func TestTheSizeIsPublishedInOrderWithTheOutput(t *testing.T) {
	log := &pieceLog{}
	term, proc := fakeTerminal(t, 10, 40, log.observer(time.Hour))
	proc.out <- []byte("before")
	waitText(t, term, "before")
	term.Resize(60, 20)
	log.mu.Lock()
	sizes := append([][2]int(nil), log.sizes...)
	log.mu.Unlock()
	if len(sizes) != 1 || sizes[0] != [2]int{60, 20} {
		t.Errorf("sizes = %v", sizes)
	}
	if got := log.all(); len(got) != 1 || got[0].Data != "before" {
		t.Errorf("the output before the resize was not published before the size: %+v", got)
	}
	term.Resize(60, 20) // no change, no event
	log.mu.Lock()
	n := len(log.sizes)
	log.mu.Unlock()
	if n != 1 {
		t.Errorf("a resize to the same size published a size: %d", n)
	}
}

func TestTypedInputIsPublishedWithWhoTypedItAndNotEchoedSecrets(t *testing.T) {
	log := &pieceLog{}
	term, proc := fakeTerminal(t, 24, 80, log.observer(time.Hour))
	term.Type([]byte("echo hi\r"), ByUser)
	term.Send([]byte("a reply of the emulator"))
	term.Type([]byte("\xff caf\xc3"), "relay:alice")
	term.Type(nil, ByUser)
	log.mu.Lock()
	inputs := append([]inputEvent(nil), log.inputs...)
	log.mu.Unlock()
	if len(inputs) != 2 || inputs[0].Data != "echo hi\r" || inputs[0].By != ByUser || inputs[1].By != "relay:alice" || inputs[1].Data != "� caf�" {
		t.Errorf("inputs = %+v", inputs)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !bytes.Contains([]byte(proc.input()), []byte("a reply of the emulator")) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := proc.input(); got != "echo hi\ra reply of the emulator\xff caf\xc3" {
		t.Errorf("the program got %q", got)
	}
}

// TestWhatIsTypedIsHiddenOnlyAtAPasswordPrompt holds decision 185: the data of an input event is left out when the terminal
// does not echo and is canonical (how getpass, sudo, ssh and read -s read a password), or when the mode cannot be read. A line
// editor or a full-screen program turns ICANON off and echoes by itself, so its input is published.
func TestWhatIsTypedIsHiddenOnlyAtAPasswordPrompt(t *testing.T) {
	tests := []struct {
		name        string
		echo, canon bool
		err         error
		wantHidden  bool
	}{
		{"echo on, canonical (cat)", true, true, nil, false},
		{"echo off, canonical (a password prompt)", false, true, nil, true},
		{"echo off, raw (a line editor)", false, false, nil, false},
		{"echo on, raw", true, false, nil, false},
		{"the mode cannot be read", true, true, errors.New("TestIoctlFailed", "the terminal was closed"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := &lineMode{fakeProcess: newFakeProcess(), echo: tt.echo, canon: tt.canon, err: tt.err}
			log := &pieceLog{}
			term, err := newTerminal(t.Context(), terminalOpts{id: 2, profile: profile{name: "fake", path: "fake"}, scrollback: 10, obs: log.observer(time.Hour),
				start: func(context.Context, pty.Options) (process, error) { return proc, nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { term.Kill(); term.Wait() })
			term.Type([]byte("s3cret\r"), "relay:alice")
			log.mu.Lock()
			defer log.mu.Unlock()
			if len(log.inputs) != 1 {
				t.Fatalf("inputs = %+v", log.inputs)
			}
			in := log.inputs[0]
			wantData := "s3cret\r"
			if tt.wantHidden {
				wantData = ""
			}
			if in.Hidden != tt.wantHidden || in.Data != wantData || in.By != "relay:alice" {
				t.Errorf("input = %+v, want hidden %v and data %q", in, tt.wantHidden, wantData)
			}
		})
	}
}

// lineMode is a program whose terminal has the given mode, or cannot be asked.
type lineMode struct {
	*fakeProcess
	echo, canon bool
	err         error
}

func (l *lineMode) LineMode() (bool, bool, error) { return l.echo, l.canon, l.err }

func TestASnapshotKeepsToTheSizeLimitByLeavingTheOldestLinesOut(t *testing.T) {
	f := newFakeProcess()
	log := &pieceLog{}
	term, err := newTerminal(t.Context(), terminalOpts{id: 1, profile: profile{name: "fake", path: "fake"}, scrollback: 200, rows: 24, cols: 80,
		start: func(context.Context, pty.Options) (process, error) { return f, nil }, obs: log.observer(time.Hour), snapshotLimit: 4000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Kill(); term.Wait() })
	for i := 0; i < 150; i++ {
		f.out <- []byte("line number " + strconv.Itoa(i) + "\r\n")
	}
	f.out <- []byte("\x1b]0;end\x07")
	deadline := time.Now().Add(10 * time.Second)
	for term.Meta().title != "end" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	data, _, _, _ := term.Snapshot()
	if len(data) > 4000 {
		t.Errorf("the snapshot is %d bytes, over the limit of 4000", len(data))
	}
	if !bytes.Contains(data, []byte("line number 149")) || bytes.Contains(data, []byte("line number 0\r")) {
		t.Errorf("the snapshot should end with the newest line and have lost the oldest:\n%q", data)
	}
	// the screen and the newest scrollback lines are what it holds
	em := emulator.New(24, 80, emulator.Options{Scrollback: 200})
	_, _ = em.Write(data)
	if got := em.Text(0, em.LineCount()-1); !bytes.Contains([]byte(got), []byte("line number 149")) {
		t.Errorf("the cut snapshot does not show the last line: %q", got)
	}
}

// BenchmarkFeed measures what the stream of pieces adds to the terminal's own goroutine for each chunk the program wrote
// (SPEC §14 "Terminal"): the same colored text of 64 KiB chunks written to the emulator with no observer, and with one that
// publishes (to nothing).
func BenchmarkFeed(b *testing.B) {
	var chunk []byte
	for i := 0; len(chunk) < 64<<10; i++ {
		chunk = append(chunk, "\x1b[3"+strconv.Itoa(1+i%6)+"mthe quick brown fox jumps over the lazy dog 0123456789\x1b[0m\r\n"...)
	}
	for _, tc := range []struct {
		name string
		obs  *observer
	}{
		{"no observer", nil},
		{"observer", &observer{output: func(uint64, string) {}}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			f := newFakeProcess()
			term, err := newTerminal(b.Context(), terminalOpts{id: 1, profile: profile{name: "fake", path: "fake"}, scrollback: 1000, rows: 45, cols: 160,
				start: func(context.Context, pty.Options) (process, error) { return f, nil }, obs: tc.obs})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { term.Kill(); term.Wait() })
			b.SetBytes(int64(len(chunk)))
			b.ResetTimer()
			for range b.N {
				term.feed(chunk)
			}
		})
	}
}
