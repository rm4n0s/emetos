package terminal

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// The quick fixes of a failed command (SPEC §10.25 "Shell integration"): VS Code's built-in ones. When a command ends, its
// command line, exit status and the last lines of its output are matched against a few patterns; each match offers actions: run
// another command line, open an address, or free a port. They are shown as a light bulb on the command's decoration, and in
// the menu of the decoration.
//
// Ported from VS Code: src/vs/workbench/contrib/terminalContrib/quickFix/browser/terminalQuickFixBuiltinActions.ts and the
// matching of ITerminalCommand.getOutputMatch (src/vs/platform/terminal/common/capabilities/commandDetection/terminalCommand.ts).
// The regular expressions are Go's (RE2); VS Code's use only what RE2 has, with named groups written (?P<name>...).

// The patterns of the built-in fixes (VS Code's names).
var (
	gitCommandLineRegex         = regexp.MustCompile(`git`)
	gitFastForwardPullOutputRe  = regexp.MustCompile(`and can be fast-forwarded`)
	gitPushCommandLineRegex     = regexp.MustCompile(`git\s+push`)
	gitTwoDashesRegex           = regexp.MustCompile("error: did you mean `--(.+)` \\(with two dashes\\)\\?")
	gitSimilarOutputRegex       = regexp.MustCompile(`(?:(most similar commands? (is|are)))`)
	freePortOutputRegex         = regexp.MustCompile(`(?:address already in use (?:0\.0\.0\.0|127\.0\.0\.1|localhost|::):|Unable to bind [^ ]*:|can't listen on port |listen EADDRINUSE [^ ]*:)(?P<portNumber>\d{4,5})`)
	gitPushOutputRegex          = regexp.MustCompile(`git push --set-upstream origin (?P<branchName>[^\s]+)`)
	gitCreatePrOutputRegex      = regexp.MustCompile(`remote:\s*(?P<link>https://github\.com/.+/.+/pull/new/.+)`)
	pwshGeneralErrorOutputRegex = regexp.MustCompile(`Suggestion \[General\]:`)
	pwshUnixNotFoundOutputRegex = regexp.MustCompile(`Suggestion \[cmd-not-found\]:`)
	gitSimilarSubstitution      = regexp.MustCompile(`git\s+[^\s]+`)
	pwshMostSimilarRegex        = regexp.MustCompile(`The most similar commands are: (?P<values>.+)\.`)
	pwshInstallInsteadRegex     = regexp.MustCompile(`You also have .+ installed, you can run '(?P<command>.+)' instead\.`)
	pwshCanBeInstalledWithRegex = regexp.MustCompile(`Command '.+' not found, but can be installed with:`)
	anyCommandLine              = regexp.MustCompile(`.+`)
)

// fixKind is what a quick fix does.
type fixKind int

// The kinds of quick fix.
const (
	// fixCommand runs a command line in the terminal (VS Code's TerminalCommand).
	fixCommand fixKind = iota
	// fixOpen opens an address.
	fixOpen
	// fixPort kills the process that holds a port and runs the command again.
	fixPort
)

// quickFix is one action offered for a command.
type quickFix struct {
	id   string
	kind fixKind
	// label is what the menu shows ("Run: git status", "Open: https://...", "Free port 3000").
	label string
	// command is the command line to run (fixCommand), execute whether it is run with Enter or only typed; uri the address
	// (fixOpen); port the port (fixPort).
	command string
	execute bool
	uri     string
	port    string
}

// outputMatcher says which lines of the output to match: anchor "bottom" counts from the last line, "top" from the first,
// offset lines away, and at most length lines are read.
type outputMatcher struct {
	re     *regexp.Regexp
	anchor string
	offset int
	length int
}

// outputMatch is a match: the groups of the regular expression (index 0 is the whole match) with the names of the named ones,
// and the lines that were read.
type outputMatch struct {
	groups      []string
	names       map[string]string
	outputLines []string
}

// matchResult is what a fix gets.
type matchResult struct {
	commandLine string
	output      *outputMatch
}

// fixOption is a built-in fix: when it applies and what it offers.
type fixOption struct {
	id string
	// commandRe selects the command lines; output the lines of the output that must match; exit is "success" or "error".
	commandRe *regexp.Regexp
	output    outputMatcher
	exit      string
	fixes     func(m matchResult) []quickFix
}

// outputMatchOf reads the lines of an output as the matcher says. lines are the logical lines of the output, the last one the
// line the next prompt is on (VS Code reads up to the line where the command ended, that line included).
//
// Ported from VS Code: ITerminalCommand.getOutputMatch (terminalCommand.ts)
func outputMatchOf(lines []string, m outputMatcher) *outputMatch {
	toCheck := m.length
	if toCheck <= 0 {
		toCheck = 1
	}
	var read []string
	var match []string
	if m.anchor == "bottom" {
		for i := len(lines) - 1 - m.offset; i >= 0; i-- {
			read = append([]string{lines[i]}, read...)
			if match == nil {
				match = m.re.FindStringSubmatch(read[0])
			}
			if len(read) >= toCheck {
				break
			}
		}
	} else {
		for i := m.offset; i < len(lines)-1; i++ {
			read = append(read, lines[i])
			if match == nil {
				match = m.re.FindStringSubmatch(read[len(read)-1])
			}
			if len(read) >= toCheck {
				break
			}
		}
	}
	if match == nil {
		return nil
	}
	// the last matching line among those read gives the groups (VS Code matches while reading and keeps the first match)
	names := map[string]string{}
	for i, n := range m.re.SubexpNames() {
		if n != "" && i < len(match) {
			names[n] = match[i]
		}
	}
	return &outputMatch{groups: match, names: names, outputLines: read}
}

// builtinFixes are the fixes of VS Code's terminal, in the order it registers them (terminal.quickFix.contribution.ts).
func builtinFixes() []fixOption {
	runCommand := func(id, command string) quickFix {
		return quickFix{id: id, kind: fixCommand, label: "Run: " + command, command: command, execute: true}
	}
	return []fixOption{
		{
			id: "Git Two Dashes", commandRe: gitCommandLineRegex, exit: "error",
			output: outputMatcher{re: gitTwoDashesRegex, anchor: "bottom", offset: 0, length: 2},
			fixes: func(m matchResult) []quickFix {
				if len(m.output.groups) < 2 || m.output.groups[1] == "" {
					return nil
				}
				arg := m.output.groups[1]
				return []quickFix{runCommand("Git Two Dashes", strings.Replace(m.commandLine, " -"+arg, " --"+arg, 1))}
			},
		},
		{
			id: "Git Fast Forward Pull", commandRe: gitCommandLineRegex, exit: "success",
			output: outputMatcher{re: gitFastForwardPullOutputRe, anchor: "bottom", offset: 0, length: 8},
			fixes: func(matchResult) []quickFix {
				return []quickFix{runCommand("Git Fast Forward Pull", "git pull")}
			},
		},
		{
			id: "Free Port", commandRe: anyCommandLine, exit: "error",
			output: outputMatcher{re: freePortOutputRegex, anchor: "bottom", offset: 0, length: 30},
			fixes: func(m matchResult) []quickFix {
				port := m.output.names["portNumber"]
				if port == "" {
					return nil
				}
				return []quickFix{{id: "Free Port", kind: fixPort, label: "Free port " + port, port: port, command: m.commandLine}}
			},
		},
		{
			id: "Git Similar", commandRe: gitCommandLineRegex, exit: "error",
			output: outputMatcher{re: gitSimilarOutputRegex, anchor: "bottom", offset: 0, length: 10},
			fixes: func(m matchResult) []quickFix {
				whole := m.output.groups[0]
				start := 0
				for i, l := range m.output.outputLines {
					if strings.Contains(l, whole) {
						start = i + 1
						break
					}
				}
				var out []quickFix
				for _, l := range m.output.outputLines[start:] {
					fixed := strings.TrimSpace(l)
					if fixed == "" {
						continue
					}
					// only the first "git word" is replaced
					replaced := m.commandLine
					if loc := gitSimilarSubstitution.FindStringIndex(m.commandLine); loc != nil {
						replaced = m.commandLine[:loc[0]] + "git " + fixed + m.commandLine[loc[1]:]
					}
					out = append(out, runCommand("Git Similar", replaced))
				}
				return out
			},
		},
		{
			id: "Git Push Set Upstream", commandRe: gitPushCommandLineRegex, exit: "error",
			output: outputMatcher{re: gitPushOutputRegex, anchor: "bottom", offset: 0, length: 8},
			fixes: func(m matchResult) []quickFix {
				branch := m.output.names["branchName"]
				if branch == "" {
					return nil
				}
				return []quickFix{runCommand("Git Push Set Upstream", "git push --set-upstream origin "+branch)}
			},
		},
		{
			id: "Git Create Pr", commandRe: gitPushCommandLineRegex, exit: "success",
			output: outputMatcher{re: gitCreatePrOutputRegex, anchor: "bottom", offset: 4, length: 12},
			fixes: func(m matchResult) []quickFix {
				link := strings.TrimRight(m.output.names["link"], " \t\r\n")
				if link == "" {
					return nil
				}
				if _, err := url.Parse(link); err != nil {
					return nil
				}
				return []quickFix{{id: "Git Create Pr", kind: fixOpen, label: "Open: " + link, uri: link}}
			},
		},
		{
			id: "Unix Command Not Found", commandRe: anyCommandLine, exit: "error",
			output: outputMatcher{re: pwshUnixNotFoundOutputRegex, anchor: "bottom", offset: 0, length: 10},
			fixes: func(m matchResult) []quickFix {
				// the lines from "Suggestion [cmd-not-found]:" on, up to the first empty one
				lines := m.output.outputLines
				i := 0
				for ; i < len(lines); i++ {
					if pwshUnixNotFoundOutputRegex.MatchString(lines[i]) {
						break
					}
				}
				var out []quickFix
				inSuggestions := false
				for ; i < len(lines); i++ {
					line := strings.TrimSpace(lines[i])
					if line == "" {
						break
					}
					if g := pwshInstallInsteadRegex.FindStringSubmatch(line); g != nil {
						out = append(out, quickFix{id: "Pwsh Unix Command Not Found Error", kind: fixCommand, label: "Run: " + g[1], command: g[1], execute: false})
						inSuggestions = false
						continue
					}
					if pwshCanBeInstalledWithRegex.MatchString(line) {
						inSuggestions = true
						continue
					}
					if inSuggestions {
						out = append(out, quickFix{id: "Pwsh Unix Command Not Found Error", kind: fixCommand, label: "Run: " + line, command: line})
					}
				}
				return out
			},
		},
		{
			id: "Pwsh General Error", commandRe: anyCommandLine, exit: "error",
			output: outputMatcher{re: pwshGeneralErrorOutputRegex, anchor: "bottom", offset: 0, length: 10},
			fixes: func(m matchResult) []quickFix {
				lines := m.output.outputLines
				for i, l := range lines {
					if !pwshGeneralErrorOutputRegex.MatchString(l) || i+1 >= len(lines) {
						continue
					}
					g := pwshMostSimilarRegex.FindStringSubmatch(lines[i+1])
					if g == nil {
						return nil
					}
					var out []quickFix
					for _, s := range strings.Split(g[1], ", ") {
						out = append(out, quickFix{id: "Pwsh General Error", kind: fixCommand, label: "Run: " + s, command: s})
					}
					return out
				}
				return nil
			},
		},
	}
}

// quickFixesFor decides the fixes of a finished command: the options whose exit result, command line and output match, in
// order, without two fixes that run the same line or open the same address. lines are the logical lines of the output.
//
// Ported from VS Code: getQuickFixesForCommand (quickFixAddon.ts)
func quickFixesFor(options []fixOption, commandLine string, exit int, lines []string) []quickFix {
	var out []quickFix
	seen := map[string]bool{}
	for _, o := range options {
		if o.exit == "success" && exit != 0 || o.exit == "error" && exit == 0 {
			continue
		}
		if !o.commandRe.MatchString(commandLine) {
			continue
		}
		m := outputMatchOf(lines, o.output)
		if m == nil {
			continue
		}
		for _, f := range o.fixes(matchResult{commandLine: commandLine, output: m}) {
			key := strconv.Itoa(int(f.kind)) + "\x00" + f.command + "\x00" + f.uri + "\x00" + f.port
			if f.kind != fixPort && seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

// ---- freeing a port ----

// portHolders are the processes that listen on a TCP port of this computer, found in /proc (no lsof): the sockets in
// /proc/net/tcp and tcp6 that listen on the port, and the processes that hold them open. Only the processes of the user can be
// read, which are the ones a fix may kill.
func portHolders(proc string, port int) []int {
	inodes := map[string]bool{}
	for _, f := range []string{"net/tcp", "net/tcp6"} {
		data, err := os.ReadFile(filepath.Join(proc, f))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if i == 0 || len(fields) < 10 || fields[3] != "0A" { // 0A is LISTEN
				continue
			}
			_, hexPort, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			if p, err := strconv.ParseUint(hexPort, 16, 32); err == nil && int(p) == port {
				inodes[fields[9]] = true
			}
		}
	}
	if len(inodes) == 0 {
		return nil
	}
	entries, err := os.ReadDir(proc)
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(proc, e.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(proc, e.Name(), "fd", fd.Name()))
			if err != nil {
				continue
			}
			if rest, ok := strings.CutPrefix(link, "socket:["); ok && inodes[strings.TrimSuffix(rest, "]")] {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

// killPort ends the processes that hold a port (SIGTERM) and returns how many it signalled.
func killPort(proc string, port int, kill func(pid int, sig syscall.Signal) error) int {
	n := 0
	for _, pid := range portHolders(proc, port) {
		if err := kill(pid, syscall.SIGTERM); err == nil {
			n++
		}
	}
	return n
}
