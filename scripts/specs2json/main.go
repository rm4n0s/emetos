// Command specs2json converts the data of the completion specs of VS Code's terminal-suggest extension (TypeScript, MIT, from
// withfig/autocomplete) to the JSON that extensions/terminal embeds (SPEC §10.25 "Terminal IntelliSense"). Only data is
// converted: the generators of a spec (code that runs a shell command) are left out and never run, and nothing of Node is used.
// scripts/update-assets.sh runs it.
//
// Usage:
//
//	specs2json specs <out folder> <spec.ts>...        one <name>.json for each spec file
//	specs2json builtins <cache.ts> <out.json>         the builtins of zsh or fish from VS Code's description caches
//	specs2json bash <out.json>                        the builtins of the bash on this machine, with `help -d`
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("conversion failed", "err", err)
		os.Exit(1)
	}
}

// run executes the command line (without the program name). The error tags are Specs2jsonUsage, Specs2jsonReadFailed,
// Specs2jsonNoSpec, Specs2jsonEncodeFailed, Specs2jsonWriteFailed and Specs2jsonBashFailed.
func run(args []string) error {
	switch {
	case len(args) >= 3 && args[0] == "specs":
		for _, f := range args[2:] {
			if err := convertSpec(f, args[1]); err != nil {
				return err
			}
		}
		return nil
	case len(args) == 3 && args[0] == "builtins":
		return convertBuiltins(args[1], args[2])
	case len(args) == 2 && args[0] == "bash":
		return bashBuiltins(args[1])
	}
	return errors.New("Specs2jsonUsage", "usage: specs2json specs <out folder> <spec.ts>... | builtins <cache.ts> <out.json> | bash <out.json>")
}

// convertSpec converts one spec file to <out>/<file name without .ts>.json.
func convertSpec(path, out string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.NewErr("Specs2jsonReadFailed", err).SetMetadata("path", path)
	}
	spec, err := convertSource(string(data))
	if err != nil {
		return err
	}
	enc, err := encode(spec)
	if err != nil {
		return errors.NewErr("Specs2jsonEncodeFailed", err).SetMetadata("path", path)
	}
	name := strings.TrimSuffix(filepath.Base(path), ".ts")
	return writeFile(filepath.Join(out, name+".json"), append(enc, '\n'))
}

// convertSource converts the text of a spec file.
func convertSource(src string) (*Spec, error) {
	p := parseFile(src)
	var spec *Spec
	if p.hasDefault {
		spec = specOf(p.defaultExport)
	}
	if spec == nil {
		return nil, errors.New("Specs2jsonNoSpec", "the file has no spec as its default export")
	}
	return spec, nil
}

// builtin is a command a shell has in itself.
type builtin struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Args        string `json:"args,omitempty"`
}

// convertBuiltins reads the description cache of zsh or fish (an object of objects with shortDescription and args).
func convertBuiltins(in, out string) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return errors.NewErr("Specs2jsonReadFailed", err).SetMetadata("path", in)
	}
	p := parseFile(string(data))
	var list []builtin
	for _, v := range p.vars {
		o, ok := v.(*object)
		if !ok {
			continue
		}
		for _, k := range o.keys {
			e, ok := o.vals[k].(*object)
			if !ok {
				continue
			}
			args, _ := e.vals["args"].(string)
			list = append(list, builtin{Name: k, Description: stringOf(e, "shortDescription"), Args: firstLine(args)})
		}
	}
	return writeBuiltins(out, list)
}

// firstLine is the first line of a text.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// writeBuiltins writes the list sorted by name.
func writeBuiltins(out string, list []builtin) error {
	slices.SortFunc(list, func(a, b builtin) int { return strings.Compare(a.Name, b.Name) })
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(list); err != nil {
		return errors.NewErr("Specs2jsonEncodeFailed", err).SetMetadata("path", out)
	}
	return writeFile(out, buf.Bytes())
}

// bashBuiltins lists the builtins of bash with `compgen -b` and describes each with `help -d`, run in an empty environment.
func bashBuiltins(out string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	run := func(script string) (string, error) {
		cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", script)
		cmd.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
		b, err := cmd.Output()
		return string(b), err
	}
	names, err := run("compgen -b")
	if err != nil {
		return errors.NewErr("Specs2jsonBashFailed", err)
	}
	var list []builtin
	for _, n := range strings.Fields(names) {
		d, err := run("help -d -- " + n)
		if err != nil {
			list = append(list, builtin{Name: n})
			continue
		}
		// "cd - Change the shell working directory."
		_, desc, _ := strings.Cut(strings.TrimSpace(d), " - ")
		list = append(list, builtin{Name: n, Description: strings.TrimSpace(desc)})
	}
	return writeBuiltins(out, list)
}

// writeFile writes a file, making its folder.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.NewErr("Specs2jsonWriteFailed", err).SetMetadata("path", path)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return errors.NewErr("Specs2jsonWriteFailed", err).SetMetadata("path", path)
	}
	return nil
}
