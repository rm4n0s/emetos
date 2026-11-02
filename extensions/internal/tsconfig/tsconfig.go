// Package tsconfig reads the little the debuggers of JavaScript and TypeScript need from a tsconfig.json: where the compiler puts its
// output. A tsconfig.json is JSON with comments and trailing commas; its "extends" is not followed.
package tsconfig

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxSize bounds what is read of a tsconfig.json.
const maxSize = 4 << 20

// Strip removes the comments and the trailing commas of a tsconfig.json, which is JSON with both.
func Strip(data []byte) []byte {
	var out bytes.Buffer
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case inString:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(data) {
				i++
				out.WriteByte(data[i])
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && (data[i] != '*' || data[i+1] != '/') {
				i++
			}
			i++
		default:
			out.WriteByte(c)
		}
	}
	return trailingComma.ReplaceAll(out.Bytes(), []byte("$1"))
}

// trailingComma finds a comma before a closing bracket.
var trailingComma = regexp.MustCompile(`,(\s*[}\]])`)

// OutDir is the compilerOptions.outDir of the tsconfig.json at the path, "" when the file is not there, is not read, or has none. A
// relative outDir is as the file wrote it.
func OutDir(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read only
	data, err := io.ReadAll(io.LimitReader(f, maxSize))
	if err != nil {
		return ""
	}
	var cfg struct {
		CompilerOptions struct {
			OutDir string `json:"outDir"`
		} `json:"compilerOptions"`
	}
	if json.Unmarshal(Strip(data), &cfg) != nil {
		return ""
	}
	return cfg.CompilerOptions.OutDir
}

// Nearest finds the tsconfig.json that governs a file: the first one in its folder or a folder above it, up to the folder stop (which
// is searched too) or, when the file is not below stop, up to the root of the file system or five levels, whichever comes first.
func Nearest(file, stop string) (string, bool) {
	dir := filepath.Dir(file)
	inside := stop != "" && (dir == stop || strings.HasPrefix(dir, stop+string(filepath.Separator)))
	for level := 0; ; level++ {
		p := filepath.Join(dir, "tsconfig.json")
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p, true
		}
		parent := filepath.Dir(dir)
		switch {
		case parent == dir, inside && dir == stop, !inside && level >= 5:
			return "", false
		}
		dir = parent
	}
}
