package redis

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The limits of what a console keeps (SPEC §14: nothing grows without a bound).
const (
	// maxConsoleEntries is how many entries (a command or a reply is one each) a console keeps, and maxConsoleBytes how many bytes of
	// text; the oldest entries go first.
	maxConsoleEntries = 1000
	maxConsoleBytes   = 2 << 20
	// maxHistory is how many commands the history of the input remembers.
	maxHistory = 200
)

// entryKind says what an entry of the console is.
type entryKind uint8

const (
	// entryCommand is a command that was run, with its prompt.
	entryCommand entryKind = iota
	// entryReply is a reply of the server.
	entryReply
	// entryError is an error: a reply of the server, or a refusal of the console.
	entryError
	// entryNote is something the console says (a command that waits for the user, a cancel).
	entryNote
)

// entry is one thing the console shows: a command or its reply. The text may have several lines.
type entry struct {
	kind entryKind
	text string
}

// console is the console of one connection (SPEC §10.23 "The console"): the entries it shows, the database its commands go to, the history
// of the input and the commands that are running. Its fields belong to the UI thread.
type console struct {
	x    *Extension
	conn string
	// db is the database the commands go to; SELECT changes it.
	db int

	entries []entry
	size    int
	// version grows with every change of the entries, so that the view knows to build again.
	version int

	hist  *ui.History
	input *ui.InputBoxController
	// scrollCtl is the controller of the output list, made when the list is first built.
	scrollCtl widgets.ScrollController

	// run is the commands that are queued or in flight, nil when the console is idle.
	run *consoleRun
	// gen numbers the runs, so that the answer of one that was replaced is dropped.
	gen int
}

// consoleRun is a batch of command lines being run one after the other.
type consoleRun struct {
	gen   int
	queue []string
	// cancel ends the command in flight; waiting says that the next command waits for the user's answer to a confirmation.
	cancel  context.CancelFunc
	waiting bool
	// shown is the command that waits, for the note of the console.
	shown string
}

// consoleFor returns the console of a connection, made on first use. The database starts at the one the connection selects.
func (x *Extension) consoleFor(conn string) *console {
	if c, ok := x.consoles[conn]; ok {
		return c
	}
	c := &console{x: x, conn: conn, hist: ui.NewHistory(nil, maxHistory), input: ui.NewInputBoxController("")}
	if s, err := x.drv.session(conn); err == nil {
		c.db = s.dbIndex
	}
	x.consoles[conn] = c
	return c
}

// add appends an entry and drops the oldest ones past the limits.
func (c *console) add(kind entryKind, text string) {
	c.entries = append(c.entries, entry{kind: kind, text: text})
	c.size += len(text)
	for len(c.entries) > 1 && (len(c.entries) > maxConsoleEntries || c.size > maxConsoleBytes) {
		c.size -= len(c.entries[0].text)
		c.entries = c.entries[1:]
	}
	c.version++
}

// clear forgets the entries.
func (c *console) clear() {
	c.entries, c.size = nil, 0
	c.version++
	c.x.refreshConsole()
}

// prompt is what the console shows before a command.
func (c *console) prompt() string { return "db" + strconv.Itoa(c.db) + "> " }

// readOnly tells whether the connection of the console refuses changes.
func (c *console) readOnly() bool {
	s, err := c.x.drv.session(c.conn)
	return err == nil && s.conn.ReadOnly
}

// busy tells whether a command is in flight or waits for a confirmation.
func (c *console) busy() bool { return c.run != nil }

// inFlight tells whether a command is being run by the server.
func (c *console) inFlight() bool { return c.run != nil && c.run.cancel != nil }

// submit runs the command line of the input box.
func (c *console) submit(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if c.run != nil && !c.run.waiting {
		c.add(entryNote, "A command is still running. Cancel it first.")
		c.x.refreshConsole()
		return
	}
	c.input.SetValue("")
	c.runLines([]string{line})
}

// runLines runs command lines one after the other: blank lines and lines that start with # are skipped. A command that waits for a
// confirmation stops the batch until the user answers; a line that cannot be read or a connection that fails stops it too.
func (c *console) runLines(lines []string) {
	if c.run != nil && !c.run.waiting {
		c.add(entryNote, "A command is still running. Cancel it first.")
		c.x.refreshConsole()
		return
	}
	if c.run != nil {
		c.add(entryNote, "(not run: "+c.run.shown+")")
	}
	var queue []string
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			queue = append(queue, l)
		}
	}
	c.gen++
	c.run = &consoleRun{gen: c.gen, queue: queue}
	c.step()
}

// finish ends the run.
func (c *console) finish() {
	c.run = nil
	c.x.refreshConsole()
}

// step takes the next command of the run.
func (c *console) step() {
	r := c.run
	if r == nil {
		return
	}
	for len(r.queue) > 0 {
		line := r.queue[0]
		r.queue = r.queue[1:]
		args, err := SplitArgs(line)
		if err != nil {
			c.add(entryCommand, c.prompt()+line)
			c.add(entryError, "(error) "+err.Error())
			r.queue = nil
			break
		}
		if len(args) == 0 {
			continue
		}
		c.add(entryCommand, c.prompt()+line)
		if why := Unsupported(args); why != "" {
			c.add(entryError, "(error) "+why)
			continue
		}
		// a read-only connection refuses the command whatever the user says: asking would only get a "yes" that is refused
		if why := Dangerous(args); why != "" && c.x.confirmDangerous() && (!c.readOnly() || !RefusedByBuiltinList(args)) {
			c.ask(r, args, why)
			return
		}
		c.exec(r, args)
		return
	}
	c.finish()
}

// ask puts a dangerous command to the user: it runs only when the user presses Run in the notification (SPEC §10.23 "Safety").
func (c *console) ask(r *consoleRun, args []string, why string) {
	name, _ := commandWords(args)
	r.waiting = true
	r.shown = strings.Join(args, " ")
	c.add(entryNote, "Waiting for your answer: "+why)
	gen := r.gen
	c.x.api.Notifications().Warn(why+" Run "+name+"?",
		extension.NotificationAction{Label: "Run", Run: func() { c.confirmed(gen, args) }})
	c.x.refreshConsole()
}

// confirmed runs the command the user agreed to, when it is still the one the console waits for.
func (c *console) confirmed(gen int, args []string) {
	r := c.run
	if r == nil || r.gen != gen || !r.waiting {
		c.x.api.Notifications().Info("That command is no longer waiting.")
		return
	}
	r.waiting = false
	c.exec(r, args)
}

// cancel ends the command in flight, or drops the commands that wait.
func (c *console) cancel() {
	r := c.run
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		return
	}
	c.add(entryNote, "(not run: "+r.shown+")")
	c.run = nil
	c.x.refreshConsole()
}

// exec sends one command on a goroutine and takes the next when the answer comes.
func (c *console) exec(r *consoleRun, args []string) {
	x := c.x
	ctx, cancel := context.WithCancel(x.ctx)
	r.cancel = cancel
	conn, db, gen := c.conn, c.db, r.gen
	x.refreshConsole()
	go func() {
		start := time.Now()
		type result struct {
			rep   Reply
			err   error
			newDB int
		}
		ch := make(chan result, 1)
		go func() {
			res := result{newDB: -1}
			d, _, derr := x.drv.database(conn, db)
			switch {
			case derr != nil:
				res.err = derr
			case isSelect(args):
				res.newDB, res.rep, res.err = selectDatabase(ctx, x.drv, conn, args)
			default:
				res.rep, res.err = d.Do(ctx, args)
			}
			ch <- res
		}()
		var res result
		select {
		case res = <-ch:
		case <-ctx.Done():
			// Cancel: do not wait for a client that cannot be interrupted (it ends by itself, and its answer is dropped)
			res = result{newDB: -1, err: ctx.Err()}
		}
		cancelled := ctx.Err() != nil
		cancel()
		ms := time.Since(start).Milliseconds()
		x.api.Post(func() { c.done(gen, res.rep, res.err, cancelled, res.newDB, ms) })
	}()
}

// done shows the answer of a command and goes on with the run.
func (c *console) done(gen int, rep Reply, err error, cancelled bool, newDB int, ms int64) {
	r := c.run
	if r == nil || r.gen != gen {
		return
	}
	r.cancel = nil
	ok := err == nil && !rep.IsError()
	stop := false
	switch {
	case cancelled:
		c.add(entryNote, "(cancelled)")
		ok, stop = false, true
	case err != nil:
		c.add(entryError, "(error) "+errorText(err))
		if appErr, isTagged := errors.FromError(err); !isTagged || appErr.Tag != "RedisReadOnly" {
			stop = true
		}
	case rep.IsError():
		c.add(entryError, strings.Join(FormatReply(rep), "\n"))
	default:
		if newDB >= 0 {
			c.db = newDB
		}
		c.add(entryReply, strings.Join(FormatReply(rep), "\n"))
	}
	c.x.publishCommandFinished(c.conn, ok, ms)
	if stop {
		r.queue = nil
	}
	c.step()
}

// errorText is the message of an error for the console: the message of the tagged error, else its text.
func errorText(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		if cmd, ok := appErr.Metadata["command"].(string); ok && appErr.Tag == "RedisReadOnly" {
			return appErr.Message + ": " + cmd + " is not sent"
		}
		return appErr.Message
	}
	return err.Error()
}

// isSelect tells whether the command is SELECT.
func isSelect(args []string) bool {
	name, _ := commandWords(args)
	return name == "SELECT"
}

// selectDatabase checks that the database can be used by sending PING on it, as go-redis selects the database when it opens a
// connection and the server refuses an index out of range. It returns the number of the database to use from now on.
func selectDatabase(ctx context.Context, d *Driver, conn string, args []string) (int, Reply, error) {
	if len(args) != 2 {
		return -1, Reply{Kind: ReplyError, Str: "ERR wrong number of arguments for 'select' command"}, nil
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 0 {
		return -1, Reply{Kind: ReplyError, Str: "ERR value is not an integer or out of range"}, nil
	}
	db, _, err := d.database(conn, n)
	if err != nil {
		return -1, Reply{}, err
	}
	rep, err := db.Do(ctx, []string{"PING"})
	if err != nil {
		return -1, Reply{}, err
	}
	if rep.IsError() {
		return -1, rep, nil
	}
	return n, Reply{Kind: ReplyStatus, Str: "OK"}, nil
}
