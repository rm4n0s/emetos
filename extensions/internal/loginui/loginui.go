package loginui

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// Prompter shows the questions of the login and waits for the answers. The tab calls Build in its own Build and puts the result
// over its content; Refresh is called, on the UI thread, when a question appears or goes. It is safe to use from the goroutine
// of the login: Passphrase and ConfirmHost post to the UI thread and block until the user answers, the context ends or Close is
// called.
type Prompter struct {
	post    func(func())
	refresh func()
	words   Wording

	mu      sync.Mutex
	current *question
	closed  bool
}

// Wording is how the dialogs name the product they ask for, so that the same dialogs serve any feature that logs in this way (Remote Work).
type Wording struct {
	// Product is the name of the feature the other computer set up ("Remote Work"), and Where the place on that computer
	// that shows its host key fingerprint ("the Remote Work page").
	Product, Where string
}

// RemoteWorkWording is the wording of a Prompter that has none set.
var RemoteWorkWording = Wording{Product: "Remote Work", Where: "the Remote Work page"}

// SetWording sets how the dialogs name the product; a Prompter says Remote Work until then. Call it before the first question.
func (p *Prompter) SetWording(w Wording) {
	p.mu.Lock()
	p.words = w
	p.mu.Unlock()
}

// wording is the wording in force.
func (p *Prompter) wording() Wording {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.words.Product == "" {
		return RemoteWorkWording
	}
	return p.words
}

// question is the dialog that is showing.
type question struct {
	build func(bc widgets.BuildContext, done func()) widgets.Widget
	// cancel answers the question as the user closing it does.
	cancel func()
	// closeFn is what releases what the dialog holds (the input of a passphrase).
	closeFn func()
}

// NewPrompter makes a Prompter. post runs a function on the UI thread (the Post of the extension API) and refresh builds the
// tab again (EditorTab.Refresh or WindowTab.Refresh).
func NewPrompter(post func(func()), refresh func()) *Prompter {
	return &Prompter{post: post, refresh: refresh}
}

// Build is the dialog that is showing, or nil when there is none; the tab stacks it over its content.
func (p *Prompter) Build(bc widgets.BuildContext) widgets.Widget {
	p.mu.Lock()
	q := p.current
	p.mu.Unlock()
	if q == nil {
		return nil
	}
	colors := ui.Theme(bc)
	card := q.build(bc, func() { p.finish(q) })
	return widgets.NewColoredBox(colors.QuickInputBackground.WithAlpha(0xB0),
		widgets.NewCenter(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(520.0)}),
			widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewColoredBox(colors.EditorBackground,
				widgets.NewPadding(painting.NewEdgeInsetsAll(18), card))))))
}

// Showing tells whether a question is on screen.
func (p *Prompter) Showing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current != nil
}

// Close cancels the question on screen, if any, and refuses the ones that come after: the tab calls it when it closes.
func (p *Prompter) Close() {
	p.mu.Lock()
	p.closed = true
	q := p.current
	p.mu.Unlock()
	if q != nil {
		q.cancel()
		p.finish(q)
	}
}

// finish takes the question away.
func (p *Prompter) finish(q *question) {
	p.mu.Lock()
	if p.current != q {
		p.mu.Unlock()
		return
	}
	p.current = nil
	p.mu.Unlock()
	if q.closeFn != nil {
		q.closeFn()
	}
	p.refresh()
}

// show puts a question on screen from the UI thread; false when the prompter was closed.
func (p *Prompter) show(q *question) bool {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	prev := p.current
	p.current = q
	p.mu.Unlock()
	if prev != nil {
		prev.cancel()
		if prev.closeFn != nil {
			prev.closeFn()
		}
	}
	p.refresh()
	return true
}

// ask shows the question and waits for the user's answer, which the dialog gives through the function it was made with (a
// dialog may answer more than once: the first answer counts). The wait ends with the context, which takes the question away.
func (p *Prompter) ask(ctx context.Context, build func(reply func(bool)) *question) (bool, error) {
	answer := make(chan bool, 1)
	reply := func(v bool) {
		select {
		case answer <- v:
		default:
		}
	}
	var shown atomic.Pointer[question]
	p.post(func() {
		q := build(reply)
		shown.Store(q)
		if !p.show(q) {
			reply(false)
		}
	})
	select {
	case ok := <-answer:
		// the question goes once it is answered
		p.post(func() {
			if q := shown.Load(); q != nil {
				p.finish(q)
			}
		})
		return ok, nil
	case <-ctx.Done():
		p.post(func() {
			if q := shown.Load(); q != nil {
				q.cancel()
				p.finish(q)
			}
		})
		return false, errors.NewErr("LoginUICancelled", ctx.Err())
	}
}

// ---- the passphrase ----

// MaxPassphrase is the longest passphrase the box takes.
const MaxPassphrase = 1024

// Passphrase returns the PassphraseFunc for the key file: it shows the question "Passphrase for <file>", with the note that the
// last one was wrong from the second attempt on, and gives the bytes the user typed. The box is cleared as soon as it is
// answered; nothing is stored or logged.
func (p *Prompter) Passphrase(keyFile string) sshlogin.PassphraseFunc {
	return func(attempt int) ([]byte, bool) {
		var result []byte
		var got bool
		ok, err := p.ask(context.Background(), func(reply func(bool)) *question {
			box := ui.NewInputBoxController("")
			return &question{
				closeFn: box.Dispose,
				cancel:  func() { reply(false) },
				build: func(bc widgets.BuildContext, done func()) widgets.Widget {
					submit := func() {
						result, got = []byte(box.Value()), true
						box.SetValue("")
						reply(true)
					}
					return passphraseCard(bc, keyFile, attempt, box, submit, func() { reply(false) })
				},
			}
		})
		if err != nil || !ok || !got {
			return nil, false
		}
		return result, true
	}
}

// passphraseCard is the dialog of the passphrase.
func passphraseCard(bc widgets.BuildContext, keyFile string, attempt int, box *ui.InputBoxController, submit, cancel func()) widgets.Widget {
	colors := ui.Theme(bc)
	children := []widgets.Widget{
		title(bc, colors, "Passphrase for the key"),
		gap(8),
		text(bc, colors, keyFile, false),
		gap(12),
	}
	if attempt > 1 {
		children = append(children, widgets.NewText("That passphrase is wrong. Try again.", widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.ErrorForeground)}), gap(8))
	}
	children = append(children,
		ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: box, Obscure: true, Autofocus: true, Placeholder: "Passphrase", OnSubmit: func(string) { submit() }}),
		gap(14),
		buttons(ui.NewButton("Cancel", ui.ButtonOpts{Secondary: true, OnPressed: cancel}), ui.NewButton("OK", ui.ButtonOpts{OnPressed: submit})))
	return column(children)
}

// ---- a host that is met for the first time ----

// ConfirmHost returns the ConfirmHostFunc: it shows the fingerprint of the host and asks whether to trust it, and the answer is
// true only when the user presses Trust. The question says to compare the fingerprint with the one the other computer shows (on the
// Remote Work page, or as the wording says).
func (p *Prompter) ConfirmHost() sshlogin.ConfirmHostFunc {
	return func(ctx context.Context, host sshlogin.FirstUse) (bool, error) {
		words := p.wording()
		return p.ask(ctx, func(reply func(bool)) *question {
			return &question{
				cancel: func() { reply(false) },
				build: func(bc widgets.BuildContext, done func()) widgets.Widget {
					return FirstUseCardWith(bc, host, words, func() { reply(true) }, func() { reply(false) })
				},
			}
		})
	}
}

// FirstUseCard is the dialog of a host that is met for the first time: its address and fingerprint, and the instruction to
// compare it with the one on the page of the other computer.
func FirstUseCard(bc widgets.BuildContext, host sshlogin.FirstUse, trust, cancel func()) widgets.Widget {
	return FirstUseCardWith(bc, host, RemoteWorkWording, trust, cancel)
}

// FirstUseCardWith is FirstUseCard in the wording of a product.
func FirstUseCardWith(bc widgets.BuildContext, host sshlogin.FirstUse, words Wording, trust, cancel func()) widgets.Widget {
	colors := ui.Theme(bc)
	return column([]widgets.Widget{
		title(bc, colors, "Trust this computer?"),
		gap(8),
		text(bc, colors, "This is the first time you connect to "+host.Address+". Open "+words.Where+" on that computer and compare its host key fingerprint with this one. Trust it only if they are the same: a different one means somebody else may be answering.", false),
		gap(12),
		text(bc, colors, host.Type, false),
		mono(bc, colors, host.Fingerprint),
		gap(14),
		buttons(ui.NewButton("Cancel", ui.ButtonOpts{Secondary: true, OnPressed: cancel}), ui.NewButton("Trust", ui.ButtonOpts{OnPressed: trust})),
	})
}

// ---- a host key that changed ----

// ChangedHost is what the warning shows: the address, the fingerprint that was trusted and the one the host presented now.
type ChangedHost struct {
	// Address, Pinned and Presented are the host:port, the fingerprint that was pinned and the fingerprint presented.
	Address, Pinned, Presented string
}

// ChangedFromError reads the warning from the error of sshlogin.Dial that refuses a changed key (SSHLoginHostKeyChanged); false
// for any other error.
func ChangedFromError(err error) (ChangedHost, bool) {
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "SSHLoginHostKeyChanged" {
		return ChangedHost{}, false
	}
	str := func(k string) string { s, _ := appErr.Metadata[k].(string); return s }
	return ChangedHost{Address: str("address"), Pinned: str("pinned"), Presented: str("presented")}, true
}

// ChangedKeyCard is the warning that the key of a host changed: the connection was refused and nothing was sent. The only ways on
// are to close it, or to forget the pinned key (so that the next connection is a first use again, compared by the user).
func ChangedKeyCard(bc widgets.BuildContext, host ChangedHost, forget, closeIt func()) widgets.Widget {
	return ChangedKeyCardWith(bc, host, RemoteWorkWording, forget, closeIt)
}

// ChangedKeyCardWith is ChangedKeyCard in the wording of a product.
func ChangedKeyCardWith(bc widgets.BuildContext, host ChangedHost, words Wording, forget, closeIt func()) widgets.Widget {
	colors := ui.Theme(bc)
	return column([]widgets.Widget{
		widgets.NewRow([]widgets.Widget{ui.Icon("warning", 18, colors.NotificationsWarningIconForeground), gap2(8), title(bc, colors, "The key of this computer changed")}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		gap(8),
		text(bc, colors, "The connection to "+host.Address+" was refused and nothing was sent. Somebody may be answering in place of that computer, or "+words.Product+" was set up again there.", false),
		gap(12),
		text(bc, colors, "Trusted before", false), mono(bc, colors, host.Pinned),
		gap(6),
		text(bc, colors, "Shown now", false), mono(bc, colors, host.Presented),
		gap(10),
		text(bc, colors, "If you know why it changed, forget the old key and connect again: you will be asked to compare the new fingerprint.", true),
		gap(14),
		buttons(ui.NewButton("Forget the Old Key", ui.ButtonOpts{Secondary: true, OnPressed: forget}), ui.NewButton("Close", ui.ButtonOpts{OnPressed: closeIt})),
	})
}

// ShowChanged shows the warning for the host of an SSHLoginHostKeyChanged error; forget is called when the user chooses to forget
// the old key (it calls HostStore.Forget). It does nothing for another error, and returns whether it showed anything. Call it on
// the UI thread.
func (p *Prompter) ShowChanged(err error, forget func(ChangedHost)) bool {
	host, ok := ChangedFromError(err)
	if !ok {
		return false
	}
	words := p.wording()
	q := &question{
		cancel: func() {},
		build: func(bc widgets.BuildContext, done func()) widgets.Widget {
			return ChangedKeyCardWith(bc, host, words, func() { forget(host); done() }, done)
		},
	}
	p.show(q)
	return true
}

// ---- small pieces ----

func column(children []widgets.Widget) widgets.Widget {
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
}

func gap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

func gap2(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

func title(bc widgets.BuildContext, colors ui.Colors, s string) widgets.Widget {
	return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 16, colors.Foreground).CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600})})
}

func text(bc widgets.BuildContext, colors ui.Colors, s string, dim bool) widgets.Widget {
	c := colors.Foreground
	if dim {
		c = colors.DescriptionForeground
	}
	return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 13, c)})
}

func mono(bc widgets.BuildContext, colors ui.Colors, s string) widgets.Widget {
	family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if family == "" {
		family = "monospace"
	}
	return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: family})})
}

func buttons(buttons ...widgets.Widget) widgets.Widget {
	row := []widgets.Widget{widgets.NewSpacer()}
	for i, b := range buttons {
		if i > 0 {
			row = append(row, gap2(8))
		}
		row = append(row, b)
	}
	return widgets.NewRow(row, widgets.RowOpts{})
}
