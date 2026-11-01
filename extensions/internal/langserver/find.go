package langserver

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// Env is what the search for a program and the hooks of a Config know of the machine and the tab. Tests make their own.
type Env struct {
	// Folder is the folder of the tab, Home the home folder of the user and Path the value of PATH.
	Folder, Home, Path string
	// Environ reads a variable of Emetos's environment for ${env:NAME}; os.Getenv when nil.
	Environ func(name string) string
	// Settings reads the settings of the tab.
	Settings *Settings
}

// DefaultEnv is the Env of this machine for a tab: the folder, the home folder, PATH, and the settings.
func DefaultEnv(folder string, s *Settings) Env {
	home, _ := os.UserHomeDir()
	return Env{Folder: folder, Home: home, Path: os.Getenv("PATH"), Settings: s}
}

// getenv reads a variable.
func (e Env) getenv(name string) string {
	if e.Environ != nil {
		return e.Environ(name)
	}
	return os.Getenv(name)
}

// Where a program was found.
const (
	// FromSetting: the path setting of the pack. FromBefore and FromAfter: a folder searched before and after PATH. FromPath: PATH.
	FromSetting = "setting"
	FromBefore  = "before"
	FromPath    = "PATH"
	FromAfter   = "after"
)

// Found is a program that was found.
type Found struct {
	// Path is the absolute path, Source where it was found (FromSetting, FromBefore, FromPath, FromAfter) and Dir the folder it was
	// found in, for the places searched.
	Path, Source string
}

// Expand replaces ~, ${userHome}, ${workspaceFolder} and ${env:NAME} in a path template; a relative result is relative to the folder
// of the tab.
func (e Env) Expand(tmpl string) string {
	s := tmpl
	if s == "~" || strings.HasPrefix(s, "~/") {
		s = e.Home + s[1:]
	}
	s = strings.ReplaceAll(s, "${userHome}", e.Home)
	s = strings.ReplaceAll(s, "${workspaceFolder}", e.Folder)
	for {
		i := strings.Index(s, "${env:")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "}")
		if j < 0 {
			break
		}
		s = s[:i] + e.getenv(s[i+len("${env:"):i+j]) + s[i+j+1:]
	}
	return s
}

// absolute makes a path from a template: expanded, and joined to the folder when it is relative.
func (e Env) absolute(tmpl string) string {
	p := e.Expand(tmpl)
	if p != "" && !filepath.IsAbs(p) && e.Folder != "" {
		p = filepath.Join(e.Folder, p)
	}
	return filepath.Clean(p)
}

// IsExecutable tells whether the path is a file (a link to one counts) that someone may run.
func IsExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// FindExecutable looks for the file name in the folders, in order, and returns the first executable one.
func FindExecutable(name string, dirs []string) (string, bool) {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if IsExecutable(p) {
			return p, true
		}
	}
	return "", false
}

// Find looks for the program: the setting of its path, then the folders of Dirs and Before, PATH, and the folders of After and Dirs
// (Program.Find replaces all this). A setting that is set is the only place looked at: a wrong one fails with ServerPathInvalid, so
// that the user is told, instead of running another program than the one named. A program that is nowhere fails with
// ServerNotFound, which lists the places looked at.
func Find(p Program, env Env) (Found, error) {
	if p.Find != nil {
		return p.Find(env)
	}
	before, after := p.Before, p.After
	var dynBefore, dynAfter []string
	if p.Dirs != nil {
		dynBefore, dynAfter = p.Dirs(env)
	}
	var beforeDirs, afterDirs []string
	for _, d := range append(append([]string{}, dynBefore...), before...) {
		beforeDirs = append(beforeDirs, env.absolute(d))
	}
	for _, d := range append(append([]string{}, after...), dynAfter...) {
		afterDirs = append(afterDirs, env.absolute(d))
	}
	var pathDirs []string
	for _, d := range filepath.SplitList(env.Path) {
		if d != "" {
			pathDirs = append(pathDirs, d)
		}
	}

	if p.PathSetting != "" && env.Settings != nil {
		if set := env.Settings.String(p.PathSetting); set != "" {
			return findSet(p, set, env, append(append(append([]string{}, beforeDirs...), pathDirs...), afterDirs...))
		}
	}
	name := p.exe()
	if path, ok := FindExecutable(name, beforeDirs); ok {
		return Found{Path: path, Source: FromBefore}, nil
	}
	if path, ok := FindExecutable(name, pathDirs); ok {
		return Found{Path: path, Source: FromPath}, nil
	}
	if path, ok := FindExecutable(name, afterDirs); ok {
		return Found{Path: path, Source: FromAfter}, nil
	}
	searched := append(append(append([]string{}, beforeDirs...), pathDirs...), afterDirs...)
	return Found{}, errors.New("ServerNotFound", p.Name+" was not found", "program", p.Name, "file", name, "searched", strings.Join(searched, string(os.PathListSeparator)))
}

// findSet resolves the path the user set. A value with a slash is a path; one without is a file name looked for in the places.
func findSet(p Program, set string, env Env, dirs []string) (Found, error) {
	expanded := env.Expand(set)
	if !strings.Contains(expanded, "/") {
		if path, ok := FindExecutable(expanded, dirs); ok {
			return Found{Path: path, Source: FromSetting}, nil
		}
		return Found{}, errors.New("ServerPathInvalid", p.PathSetting+" is set to "+set+", which is not a program in PATH", "setting", p.PathSetting, "value", set)
	}
	path := env.absolute(set)
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return Found{}, errors.New("ServerPathInvalid", p.PathSetting+" is set to "+set+", which does not exist", "setting", p.PathSetting, "value", set, "path", path)
	case info.IsDir():
		return Found{}, errors.New("ServerPathInvalid", p.PathSetting+" is set to "+set+", which is a folder", "setting", p.PathSetting, "value", set, "path", path)
	case !IsExecutable(path):
		return Found{}, errors.New("ServerPathInvalid", p.PathSetting+" is set to "+set+", which cannot be run (it is not executable)", "setting", p.PathSetting, "value", set, "path", path)
	}
	return Found{Path: path, Source: FromSetting}, nil
}

// CheckPath tells why the text is not the path of a program a user may set, or "" when it is. An empty text is valid (it clears the
// setting).
func CheckPath(env Env, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	expanded := env.Expand(text)
	if !strings.Contains(expanded, "/") {
		return "" // a name looked for in PATH: it may not exist yet
	}
	path := env.absolute(text)
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return path + " does not exist"
	case info.IsDir():
		return path + " is a folder"
	case !IsExecutable(path):
		return path + " is not executable"
	}
	return ""
}
