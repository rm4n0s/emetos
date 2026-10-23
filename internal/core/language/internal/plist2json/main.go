// Command plist2json converts a TextMate grammar in the XML property list format to the JSON
// format that the grammar loader reads (SPEC §8.1). scripts/update-assets.sh runs it on the
// grammars of the reference extensions that are plists.
//
// Usage: plist2json <input.tmLanguage> <output.json>
package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/plist"
)

func main() {
	if len(os.Args) != 3 {
		_, _ = os.Stderr.WriteString("usage: plist2json <input.tmLanguage> <output.json>\n")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("conversion failed", "err", err)
		os.Exit(1)
	}
}

// run converts the plist in the file in to indented JSON, with sorted keys, in the file out. The
// error tags are PlistReadFailed, PlistEncodeFailed and PlistWriteFailed, and those of
// plist.Parse.
func run(in, out string) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return errors.NewErr("PlistReadFailed", err).SetMetadata("path", in)
	}
	root, err := plist.Parse(data)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	if err := enc.Encode(root); err != nil {
		return errors.NewErr("PlistEncodeFailed", err)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return errors.NewErr("PlistWriteFailed", err).SetMetadata("path", out)
	}
	return nil
}
