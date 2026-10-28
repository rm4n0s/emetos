package emulator

// Callbacks are what the program asks of the terminal besides drawing. Every field is optional. They are called on the
// goroutine that called Write (or Resize, Reset), after the terminal is unlocked, in the order the program caused them; a
// callback may call the Emulator again.
type Callbacks struct {
	// Reply receives the bytes the terminal answers to the program: device status and attributes, mode and color reports
	// (ctlseqs: "CSI Ps n", "CSI Ps c", "CSI Ps $ p", "OSC Ps ; ?"). One call per Write at most; the bytes go to the
	// program's input as if typed.
	Reply func(p []byte)
	// Title receives the window title (OSC 0 and 2), IconName the icon name (OSC 0 and 1).
	Title    func(title string)
	IconName func(name string)
	// Bell is called for BEL (ECMA-48 8.3.3).
	Bell func()
	// Cwd receives the working folder the shell reports (OSC 7: file://host/path), already decoded.
	Cwd func(host, path string)
	// PaletteChanged is called when the program changed the palette or the default colors (OSC 4, 10, 11, 12, 104, 110,
	// 111, 112); read them from the next Snapshot.
	PaletteChanged func()
	// OSC receives the OSC strings the emulator does not handle itself (the shell integration of step 17d.5: 133, 633,
	// 1337; the notifications 9 and 777; and any other). It says where in the buffer the cursor was when the string ended.
	OSC func(ev OSCEvent)
	// Clipboard receives OSC 52 requests (ctlseqs: OSC 52). The emulator does not act on them itself; the host decides
	// (VS Code ignores them by default).
	Clipboard func(selection string, data string)
}

// OSCEvent is an OSC string the emulator passes on.
type OSCEvent struct {
	// Code is the number before the first ';'; Data is the rest of the string.
	Code int
	Data string
	// Line is the absolute number of the row the cursor was on (it counts the lines since the terminal began and does not
	// change when the scrollback drops lines; a resize that reflows the buffer breaks it) and Col its column.
	Line int64
	Col  int
	// Alt says that the alternate screen was active.
	Alt bool
}

type eventKind uint8

const (
	evTitle eventKind = iota + 1
	evIcon
	evBell
	evCwd
	evPalette
	evOSC
	evClipboard
)

type event struct {
	kind eventKind
	s, t string
	osc  OSCEvent
}

// deliver calls the callbacks for the output of a Write, outside the lock.
func (e *Emulator) deliver(reply []byte, events []event) {
	if len(reply) > 0 && e.cb.Reply != nil {
		e.cb.Reply(reply)
	}
	for _, ev := range events {
		switch ev.kind {
		case evTitle:
			if e.cb.Title != nil {
				e.cb.Title(ev.s)
			}
		case evIcon:
			if e.cb.IconName != nil {
				e.cb.IconName(ev.s)
			}
		case evBell:
			if e.cb.Bell != nil {
				e.cb.Bell()
			}
		case evCwd:
			if e.cb.Cwd != nil {
				e.cb.Cwd(ev.s, ev.t)
			}
		case evPalette:
			if e.cb.PaletteChanged != nil {
				e.cb.PaletteChanged()
			}
		case evOSC:
			if e.cb.OSC != nil {
				e.cb.OSC(ev.osc)
			}
		case evClipboard:
			if e.cb.Clipboard != nil {
				e.cb.Clipboard(ev.s, ev.t)
			}
		}
	}
}
