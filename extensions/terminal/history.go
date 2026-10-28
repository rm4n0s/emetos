package terminal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// The history of the terminal (SPEC §10.25 "Shell integration"): the command lines and the folders of the earlier sessions, kept
// in the extension's state (terminal-history.json in the state folder of Emetos) and shown by Run Recent Command and Go to
// Recent Directory, together with the history file of the shell.
//
// Ported from VS Code: src/vs/workbench/contrib/terminalContrib/history/common/history.ts

// historyFile is the name of the state file; historyMaxBytes the most it is read.
const (
	historyFile     = "terminal-history.json"
	historyMaxBytes = 4 << 20
)

// histCmd is a command line of the history and the kind of shell it ran in.
type histCmd struct {
	Command string `json:"command"`
	Shell   string `json:"shell"`
}

// historyData is the content of the state file: both lists oldest first.
type historyData struct {
	Commands []histCmd `json:"commands"`
	Dirs     []string  `json:"dirs"`
}

// historyStore keeps the command lines and folders, least recently used first, up to a limit (terminal.integrated.shellIntegration.history,
// VS Code's LRU cache). Every change is written to the file; the file is read again before a change, so that the terminals of
// several windows add to one list. It is safe to use from any goroutine.
type historyStore struct {
	mu sync.Mutex
	// dir gives the folder of the state file.
	dir func() (string, error)
	// limit gives the most entries each list keeps.
	limit func() int
	data  historyData
	// loaded says that the file was read.
	loaded bool
}

func newHistoryStore(dir func() (string, error), limit func() int) *historyStore {
	return &historyStore{dir: dir, limit: limit}
}

// path is the state file.
func (h *historyStore) path() (string, error) {
	dir, err := h.dir()
	if err != nil {
		return "", err // the error is statefile's own: its tag and route stay
	}
	return filepath.Join(dir, historyFile), nil
}

// reload reads the file; a file that is not there is an empty history. The caller holds the lock. The errors are statefile's, or
// have the tag TerminalHistoryInvalid.
func (h *historyStore) reload() error {
	p, err := h.path()
	if err != nil {
		return err
	}
	raw, err := statefile.Read(p, historyMaxBytes, statefile.Writable)
	if err != nil {
		if statefile.Missing(err) {
			h.data, h.loaded = historyData{}, true
			return nil
		}
		return err
	}
	var d historyData
	if err := json.Unmarshal(raw, &d); err != nil {
		return errors.NewErr("TerminalHistoryInvalid", err).SetMetadata("path", p)
	}
	h.data, h.loaded = d, true
	return nil
}

// save writes the file. The caller holds the lock.
func (h *historyStore) save() error {
	p, err := h.path()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(h.data)
	if err != nil {
		return errors.NewErr("TerminalHistoryInvalid", err).SetMetadata("path", p)
	}
	return statefile.Write(p, raw)
}

// trim keeps the newest limit entries.
func (h *historyStore) trim() {
	n := max(h.limit(), 0)
	if len(h.data.Commands) > n {
		h.data.Commands = slices.Delete(h.data.Commands, 0, len(h.data.Commands)-n)
	}
	if len(h.data.Dirs) > n {
		h.data.Dirs = slices.Delete(h.data.Dirs, 0, len(h.data.Dirs)-n)
	}
}

// change reads the file, applies f and writes the file again.
func (h *historyStore) change(f func(d *historyData)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.reload(); err != nil {
		h.data, h.loaded = historyData{}, true // a file that cannot be read is replaced by the next change
		f(&h.data)
		h.trim()
		if serr := h.save(); serr != nil {
			return serr
		}
		return err
	}
	f(&h.data)
	h.trim()
	return h.save()
}

// AddCommand records a command line run in a kind of shell; the same line is moved to the newest place. A limit of 0 keeps
// no history.
func (h *historyStore) AddCommand(command, shell string) error {
	if strings.TrimSpace(command) == "" || h.limit() <= 0 {
		return nil
	}
	return h.change(func(d *historyData) {
		d.Commands = slices.DeleteFunc(d.Commands, func(c histCmd) bool { return c.Command == command })
		d.Commands = append(d.Commands, histCmd{Command: command, Shell: shell})
	})
}

// AddDir records a folder; the same folder is moved to the newest place.
func (h *historyStore) AddDir(dir string) error {
	if dir == "" || h.limit() <= 0 {
		return nil
	}
	return h.change(func(d *historyData) {
		d.Dirs = slices.DeleteFunc(d.Dirs, func(x string) bool { return x == dir })
		d.Dirs = append(d.Dirs, dir)
	})
}

// RemoveCommand forgets a command line.
func (h *historyStore) RemoveCommand(command string) error {
	return h.change(func(d *historyData) {
		d.Commands = slices.DeleteFunc(d.Commands, func(c histCmd) bool { return c.Command == command })
	})
}

// RemoveDir forgets a folder.
func (h *historyStore) RemoveDir(dir string) error {
	return h.change(func(d *historyData) {
		d.Dirs = slices.DeleteFunc(d.Dirs, func(x string) bool { return x == dir })
	})
}

// Clear forgets every command line (VS Code's Clear Previous Session History) and keeps the folders.
func (h *historyStore) Clear() error {
	return h.change(func(d *historyData) { d.Commands = nil })
}

// snapshot reads the file again and returns the lists, newest first. The command lines are those of a kind of shell ("" for
// all). An unreadable file is an empty history, with the error.
func (h *historyStore) snapshot(shell string) (commands, dirs []string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	err = h.reload()
	for _, c := range slices.Backward(h.data.Commands) {
		if shell == "" || c.Shell == shell {
			commands = append(commands, c.Command)
		}
	}
	for _, d := range slices.Backward(h.data.Dirs) {
		dirs = append(dirs, d)
	}
	return commands, dirs, err
}

// ---- the history file of the shell ----

// shellHistory is the commands of a shell's own history file, oldest first, and where they came from.
type shellHistory struct {
	// label names the file as the quick pick shows it ("~/.bash_history"), path is the file.
	label, path string
	commands    []string
}

// shellHistoryFile reads and parses the history file of a kind of shell. home is the home folder and dataHome the value of
// XDG_DATA_HOME (fish). A shell with no file, or a file that is not there, gives nil.
//
// Ported from VS Code: fetchBashHistory, fetchZshHistory, fetchPwshHistory and fetchFishHistory (history.ts)
func shellHistoryFile(shell, home, dataHome string) *shellHistory {
	var label, rel, base string
	var parse func(string) []string
	switch shell {
	case shellBash:
		label, base, rel, parse = "~/.bash_history", home, ".bash_history", parseBashHistory
	case shellZsh:
		label, base, rel, parse = "~/.zsh_history", home, ".zsh_history", parseZshHistory
	case shellPwsh:
		label, base, rel, parse = "~/.local/share/powershell/PSReadline/ConsoleHost_history.txt", home, ".local/share/powershell/PSReadline/ConsoleHost_history.txt", parsePwshHistory
	case shellFish:
		if dataHome != "" {
			label, base, rel = "$XDG_DATA_HOME/fish/fish_history", dataHome, "fish/fish_history"
		} else {
			label, base, rel = "~/.local/share/fish/fish_history", home, ".local/share/fish/fish_history"
		}
		parse = parseFishHistory
	default:
		return nil
	}
	if base == "" {
		return nil
	}
	path := filepath.Join(base, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return &shellHistory{label: label, path: path, commands: parse(string(data))}
}

// uniq collects strings in order, once each.
type uniq struct {
	seen map[string]bool
	list []string
}

func (u *uniq) add(s string) {
	if u.seen == nil {
		u.seen = map[string]bool{}
	}
	if !u.seen[s] {
		u.seen[s] = true
		u.list = append(u.list, s)
	}
}

// parseBashHistory reads ~/.bash_history: one command a line, a command that goes on while a quote is open is joined with its
// next lines.
func parseBashHistory(content string) []string {
	var out uniq
	var current *string
	var wrap rune
	for _, line := range strings.Split(content, "\n") {
		if current == nil {
			c := line
			current = &c
		} else {
			*current += "\n" + line
		}
		for _, r := range line {
			switch {
			case wrap != 0:
				if r == wrap {
					wrap = 0
				}
			case r == '\'' || r == '"':
				wrap = r
			}
		}
		if wrap == 0 {
			if len(*current) > 0 {
				out.add(strings.TrimSpace(*current))
			}
			current = nil
		}
	}
	return out.list
}

var (
	zshExtendedStart = regexp.MustCompile(`^:\s\d+:\d+;`)
	zshExtendedSplit = regexp.MustCompile(`:\s\d+:\d+;`)
)

// parseZshHistory reads ~/.zsh_history: the extended format (": 1234567890:0;command") or one command a line, where a line
// that ends with a backslash goes on in the next.
func parseZshHistory(content string) []string {
	var parts []string
	if zshExtendedStart.MatchString(content) {
		parts = zshExtendedSplit.Split(content, -1)
	} else {
		// split at the newlines that no backslash precedes (VS Code: /(?<!\\)\n/)
		start := 0
		for i := 0; i < len(content); i++ {
			if content[i] == '\n' && (i == 0 || content[i-1] != '\\') {
				parts = append(parts, content[start:i])
				start = i + 1
			}
		}
		parts = append(parts, content[start:])
	}
	var out uniq
	for _, p := range parts {
		s := strings.TrimSpace(strings.ReplaceAll(p, "\\\n", "\n"))
		if s != "" {
			out.add(s)
		}
	}
	return out.list
}

// parsePwshHistory reads PSReadLine's ConsoleHost_history.txt: a line that ends with a backtick goes on in the next.
func parsePwshHistory(content string) []string {
	var out uniq
	var current *string
	for _, line := range strings.Split(content, "\n") {
		if current == nil {
			c := line
			current = &c
		} else {
			*current += "\n" + line
		}
		if !strings.HasSuffix(line, "`") {
			if s := strings.TrimSpace(*current); s != "" {
				out.add(s)
			}
			current = nil
			continue
		}
		// an even number of backticks in the line ends the command; an odd number continues it
		if strings.Count(line, "`")%2 == 0 {
			if s := strings.TrimSpace(*current); s != "" {
				out.add(s)
			}
			current = nil
		} else {
			*current = strings.TrimSuffix(*current, "`")
		}
	}
	return out.list
}

var fishNewline = regexp.MustCompile(`(^|[^\\])((?:\\\\)*)(\\n)`)

// parseFishHistory reads fish_history: the "- cmd:" lines of its pseudo-YAML, with \n for a line break and \\ for a backslash.
func parseFishHistory(content string) []string {
	var out uniq
	for _, line := range strings.Split(content, "\n") {
		rest, ok := strings.CutPrefix(line, "- cmd:")
		if !ok {
			continue
		}
		if s := strings.TrimSpace(sanitizeFishHistoryCmd(strings.TrimLeft(rest, " \t"))); s != "" {
			out.add(s)
		}
	}
	return out.list
}

// sanitizeFishHistoryCmd turns the \n of fish's file into line breaks, leaving \\n (a backslash before an n) alone.
func sanitizeFishHistoryCmd(cmd string) string {
	for {
		next := fishNewline.ReplaceAllString(cmd, "${1}${2}\n")
		if next == cmd {
			return cmd
		}
		cmd = next
	}
}
