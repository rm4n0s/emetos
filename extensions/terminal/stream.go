package terminal

import (
	"time"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The output of a terminal as events (SPEC §10.25 "Events and requests"): the text the program wrote is cut into pieces of at
// most outputMaxBytes, one at most every outputInterval, each with the number of the piece (seq, from 1), and a snapshot
// of the screen says which piece it includes, so that whoever follows a terminal reads the snapshot and then the pieces after
// it, and asks for a new snapshot when a piece is missing.
const (
	// outputInterval is how long the output of a terminal is gathered before it is published.
	outputInterval = 50 * time.Millisecond
	// outputMaxBytes is the most a piece holds; a piece that gets as big as this is published at once.
	outputMaxBytes = 64 << 10
	// maxSnapshotBytes is the most the serialized screen of a snapshot may be; a terminal with more scrollback than fits has the
	// oldest lines left out.
	maxSnapshotBytes = 3 << 20
	// maxPendingInput is the most a client may have queued for a program that does not read its terminal.
	maxPendingInput = 4 << 20
	// inputPieceBytes is the most one input event holds.
	inputPieceBytes = 64 << 10
)

// observer is told, from any goroutine, what happens in a terminal. Its functions must not block. A nil function is not called.
type observer struct {
	// output gets a piece of the output (valid UTF-8, its seq) and input what was typed with who typed it (ByUser, a client or an
	// extension); hidden says that the terminal did not echo it (a password prompt), so that the text is left out.
	output func(seq uint64, data string)
	input  func(data string, by string, hidden bool)
	// resized gets the new size, title the title a program set.
	resized func(cols, rows int)
	title   func(title string)
	// interval is the time the output is gathered; 0 is outputInterval.
	interval time.Duration
}

// ByUser is the `by` of the input the person at the keyboard typed.
const ByUser = "user"

// lineModer is a process that can tell the line mode of its terminal (*pty.Process).
type lineModer interface {
	LineMode() (echo, canonical bool, err error)
}

// secretInput tells whether what is typed now is a secret: the terminal is in canonical mode and does not echo, which is how
// getpass, sudo, ssh, passwd and read -s read a password (decision 185). A line editor or a full-screen program turns ICANON
// off and draws what it wants itself, which the output events carry, so its input is published. When the mode cannot be read
// the input counts as secret.
func secretInput(p any) bool {
	m, ok := p.(lineModer)
	if !ok {
		return false
	}
	echo, canonical, err := m.LineMode()
	return err != nil || (!echo && canonical)
}

// feed gives the output of the program to the emulator and, when the terminal has an observer, to the stream of pieces: both
// under outMu, so that a snapshot taken under it is the screen after exactly the pieces published so far.
func (t *Terminal) feed(p []byte) {
	if t.obs == nil || t.obs.output == nil {
		if _, err := t.em.Write(p); err != nil {
			t.log.Debug("the emulator refused output", "err", err)
		}
		return
	}
	t.outMu.Lock()
	defer t.outMu.Unlock()
	if _, err := t.em.Write(p); err != nil {
		t.log.Debug("the emulator refused output", "err", err)
	}
	t.collectLocked(p)
}

// collectLocked adds the output to the piece that waits: as valid text (the emulator replaces what is ill-formed by U+FFFD,
// so does the piece), and a sequence that p ends in the middle of waits for the next chunk.
func (t *Terminal) collectLocked(p []byte) {
	if len(t.outTail) > 0 {
		p = append(t.outTail, p...)
	}
	out, held := emulator.AppendValidUTF8(t.outBuf, p, false)
	t.outBuf = out
	t.outTail = append(t.outTail[:0:0], p[len(p)-held:]...)
	switch {
	case len(t.outBuf) >= outputMaxBytes:
		t.publishLocked()
	case len(t.outBuf) > 0 && t.outTimer == nil:
		d := t.obs.interval
		if d <= 0 {
			d = outputInterval
		}
		t.outTimer = time.AfterFunc(d, func() { t.flushOutput(false) })
	}
}

// publishLocked publishes the piece that waits, if there is one. outMu is held, which keeps the pieces in order.
func (t *Terminal) publishLocked() {
	if t.outTimer != nil {
		t.outTimer.Stop()
		t.outTimer = nil
	}
	if len(t.outBuf) == 0 {
		return
	}
	t.outSeq++
	data := string(t.outBuf)
	t.outBuf = t.outBuf[:0]
	t.obs.output(t.outSeq, data)
}

// flushOutput publishes the piece that waits; final also takes the incomplete sequence the output ended in (the program is gone).
func (t *Terminal) flushOutput(final bool) {
	if t.obs == nil || t.obs.output == nil {
		return
	}
	t.outMu.Lock()
	defer t.outMu.Unlock()
	if final && len(t.outTail) > 0 {
		t.outBuf, _ = emulator.AppendValidUTF8(t.outBuf, t.outTail, true)
		t.outTail = nil
	}
	t.publishLocked()
}

// resizeEmulator resizes the emulator and tells the observer; the piece that waits is published first, so that the events keep
// the order the emulator saw.
func (t *Terminal) resizeEmulator(rows, cols int) {
	if t.obs == nil || t.obs.resized == nil || t.obs.output == nil {
		t.em.Resize(rows, cols)
		if t.obs != nil && t.obs.resized != nil {
			t.obs.resized(cols, rows)
		}
		return
	}
	t.outMu.Lock()
	defer t.outMu.Unlock()
	t.publishLocked()
	t.em.Resize(rows, cols)
	t.obs.resized(cols, rows)
}

// Snapshot is the screen and the scrollback of the terminal serialized as escape sequences (emulator.Serialize), with the size
// and the number of the last piece of output it includes. It publishes the piece that waits first, so that the snapshot and the
// events agree: a follower feeds the data to an emulator of that size and goes on with the pieces after seq. The oldest lines of
// the scrollback are left out when the whole would be larger than maxSnapshotBytes.
func (t *Terminal) Snapshot() (data []byte, cols, rows int, seq uint64) {
	if t.obs != nil && t.obs.output != nil {
		t.outMu.Lock()
		defer t.outMu.Unlock()
		t.publishLocked()
		seq = t.outSeq
	}
	data = t.em.Serialize(-1)
	for lines := t.em.LineCount(); len(data) > t.snapMax && lines > 0; {
		lines /= 2
		data = t.em.Serialize(lines)
	}
	rows, cols = t.em.Size()
	return data, cols, rows, seq
}

// Type sends what was typed to the program, as Send does, and tells the observer who typed it. The bytes of a reply of the
// emulator, of focus and of the mouse go through Send and are no input.
func (t *Terminal) Type(p []byte, by string) {
	if len(p) == 0 {
		return
	}
	t.Send(p)
	if t.obs == nil || t.obs.input == nil {
		return
	}
	hidden := secretInput(t.proc)
	for len(p) > 0 {
		n := min(len(p), inputPieceBytes)
		piece, held := emulator.AppendValidUTF8(nil, p[:n], n == len(p))
		if hidden {
			piece = nil
		}
		t.obs.input(string(piece), by, hidden)
		p = p[n-held:]
	}
}

// pendingInput is the number of bytes queued for the program and not written yet.
func (t *Terminal) pendingInput() int {
	t.inMu.Lock()
	defer t.inMu.Unlock()
	return len(t.inBuf)
}
