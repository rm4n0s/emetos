package main

import (
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

// options are the parsed command line (SPEC §10.11). The paths are as typed: buildOpen makes them absolute and
// tells folders from files.
type options struct {
	help, version bool
	level         slog.Level
	// wait, newWindow and reuseWindow are --wait, --new-window and --reuse-window.
	wait, newWindow, reuseWindow bool
	profile                      string
	// gotos are the values of --goto (file:line[:col]), in order.
	gotos []string
	// diff holds the two values of --diff and merge the four of --merge.
	diff, merge []string
	// paths are the positional arguments: the folder and files to open.
	paths []string
}

// spec describes one option: its names, how many values it takes and what it does with them.
type spec struct {
	short  rune
	long   string
	values int
	apply  func(o *options, v []string) error
	// repeatable options may be given more than once (--goto); the others are an error the second time.
	repeatable bool
}

// optionSpecs are the options of SPEC §10.11. The table is read, never changed.
var optionSpecs = []spec{
	{short: 'h', long: "help", apply: func(o *options, _ []string) error { o.help = true; return nil }, repeatable: true},
	{short: 'v', long: "version", apply: func(o *options, _ []string) error { o.version = true; return nil }, repeatable: true},
	{short: 'n', long: "new-window", apply: func(o *options, _ []string) error { o.newWindow = true; return nil }, repeatable: true},
	{short: 'r', long: "reuse-window", apply: func(o *options, _ []string) error { o.reuseWindow = true; return nil }, repeatable: true},
	{short: 'w', long: "wait", apply: func(o *options, _ []string) error { o.wait = true; return nil }, repeatable: true},
	{short: 'g', long: "goto", values: 1, repeatable: true, apply: func(o *options, v []string) error {
		o.gotos = append(o.gotos, v[0])
		return nil
	}},
	{short: 'd', long: "diff", values: 2, apply: func(o *options, v []string) error { o.diff = v; return nil }},
	{short: 'm', long: "merge", values: 4, apply: func(o *options, v []string) error { o.merge = v; return nil }},
	{long: "profile", values: 1, apply: func(o *options, v []string) error {
		if !storage.ValidProfileName(v[0]) {
			return errors.New("ArgsProfileInvalid", "a profile name has 1 to 64 letters, digits, spaces, - or _", "name", v[0])
		}
		o.profile = v[0]
		return nil
	}},
	{long: "log", values: 1, apply: func(o *options, v []string) error {
		if err := o.level.UnmarshalText([]byte(strings.ToUpper(v[0]))); err != nil {
			return errors.New("LogLevelInvalid", "the log level is not debug, info, warn or error", "level", v[0])
		}
		return nil
	}},
}

// parseArgs parses the command line by hand (SPEC §10.11):
//
//   - long options are --name, --name value and --name=value; short options are -n, -n value and -nvalue, and
//     boolean short options can be clustered (-nw, -wg file.go:3);
//   - an option with several values (--diff, --merge) takes them from the arguments that follow (the first one
//     may be attached with = or to a short option);
//   - a value is taken from the next argument whatever it looks like; "--" ends the options, and everything
//     after it is a path; a lone "-" (standard input) is not supported;
//   - the log level is checked while the options are read.
//
// The error tags are ArgsUnknownOption, ArgsMissingValue, ArgsValueUnexpected, ArgsRepeated, ArgsConflict,
// ArgsStdinUnsupported, ArgsProfileInvalid and LogLevelInvalid. The default level is info.
func parseArgs(args []string) (options, error) {
	o := options{level: slog.LevelInfo}
	seen := map[string]bool{}
	use := func(sp *spec, name string, vals []string) error {
		if !sp.repeatable && seen[sp.long] {
			return errors.New("ArgsRepeated", "the option is given more than once", "option", name)
		}
		seen[sp.long] = true
		return sp.apply(&o, vals)
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			o.paths = append(o.paths, args[i+1:]...)
			i = len(args)
		case a == "-":
			return options{}, errors.New("ArgsStdinUnsupported", "reading a file from the standard input (-) is not supported", "argument", a)
		case strings.HasPrefix(a, "--"):
			name, attached, hasAttached := strings.Cut(a[2:], "=")
			sp := findLong(name)
			if sp == nil {
				return options{}, errors.New("ArgsUnknownOption", "unknown option", "option", "--"+name)
			}
			vals, next, err := takeValues(sp, "--"+name, attached, hasAttached, args, i)
			if err != nil {
				return options{}, err
			}
			i = next
			if err := use(sp, "--"+name, vals); err != nil {
				return options{}, err
			}
		case strings.HasPrefix(a, "-"):
			cluster := a[1:]
			for j, r := range cluster {
				sp := findShort(r)
				if sp == nil {
					return options{}, errors.New("ArgsUnknownOption", "unknown option", "option", "-"+string(r))
				}
				name := "-" + string(r)
				if sp.values == 0 {
					if err := use(sp, name, nil); err != nil {
						return options{}, err
					}
					continue
				}
				// the rest of the argument is the first value; the others come from the arguments that follow
				rest := cluster[j+utf8.RuneLen(r):]
				vals, next, err := takeValues(sp, name, rest, rest != "", args, i)
				if err != nil {
					return options{}, err
				}
				i = next
				if err := use(sp, name, vals); err != nil {
					return options{}, err
				}
				break
			}
		default:
			o.paths = append(o.paths, a)
		}
	}
	if o.newWindow && o.reuseWindow {
		return options{}, errors.New("ArgsConflict", "--new-window and --reuse-window cannot be used together")
	}
	if len(o.diff) > 0 && len(o.merge) > 0 {
		return options{}, errors.New("ArgsConflict", "--diff and --merge cannot be used together")
	}
	return o, nil
}

func findLong(name string) *spec {
	for i := range optionSpecs {
		if optionSpecs[i].long == name {
			return &optionSpecs[i]
		}
	}
	return nil
}

func findShort(r rune) *spec {
	for i := range optionSpecs {
		if optionSpecs[i].short == r {
			return &optionSpecs[i]
		}
	}
	return nil
}

// takeValues collects the values of an option: the attached one (after = or in the rest of a short cluster),
// then the next arguments. next is the index of the last argument used.
func takeValues(sp *spec, name, attached string, hasAttached bool, args []string, i int) (vals []string, next int, err error) {
	if sp.values == 0 {
		if hasAttached {
			return nil, i, errors.New("ArgsValueUnexpected", "the option takes no value", "option", name, "value", attached)
		}
		return nil, i, nil
	}
	if hasAttached {
		vals = append(vals, attached)
	}
	for len(vals) < sp.values {
		i++
		if i >= len(args) {
			return nil, i, errors.New("ArgsMissingValue", "the option needs more values", "option", name, "needs", sp.values, "got", len(vals))
		}
		vals = append(vals, args[i])
	}
	return vals, i, nil
}

// gotoPattern is the suffix of a --goto value: :line or :line:column.
var gotoPattern = regexp.MustCompile(`^(.+?):(\d+)(?::(\d+))?$`)

// parseGoto splits the value of --goto, file[:line[:column]], into the path and the position (counted from 1;
// 0 when the value has none). The tag is ArgsGotoInvalid (an empty path, or a line or column of 0).
func parseGoto(v string) (path string, line, column int, err error) {
	m := gotoPattern.FindStringSubmatch(v)
	if m == nil {
		if v == "" {
			return "", 0, 0, errors.New("ArgsGotoInvalid", "--goto needs a file, file:line or file:line:column", "value", v)
		}
		return v, 0, 0, nil
	}
	line, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		column, _ = strconv.Atoi(m[3])
	}
	if line < 1 || (m[3] != "" && column < 1) {
		return "", 0, 0, errors.New("ArgsGotoInvalid", "the line and the column of --goto count from 1", "value", v)
	}
	return m[1], line, column, nil
}
