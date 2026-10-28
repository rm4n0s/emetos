package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// profile is a way to start a terminal: a program, its arguments and environment (SPEC §10.25 "The process").
type profile struct {
	// name is the name in the lists and the dropdown ("bash"); path the program as found (absolute).
	name string
	path string
	args []string
	env  map[string]*string
	icon string
}

// environ is the source of the environment of Emetos and the lookup of programs, replaced by the tests so that nothing of the
// real environment decides a result.
type environ struct {
	// vars is the environment of Emetos as NAME=value lines.
	vars []string
	// lookPath finds a program on the PATH of vars (exec.LookPath's rules); it is os/exec's for the real thing.
	lookPath func(file string) (string, error)
	// home is the home folder of the user.
	home string
}

// osEnviron is the environment of this process.
func osEnviron() environ {
	home, _ := os.UserHomeDir()
	return environ{vars: os.Environ(), lookPath: exec.LookPath, home: home}
}

// get returns a variable of the environment.
func (e environ) get(name string) string {
	for i := len(e.vars) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(e.vars[i], name+"="); ok {
			return v
		}
	}
	return ""
}

// profilesOf reads terminal.integrated.profiles.linux: the profiles whose program is found, in the order of their names.
func profilesOf(s extension.Settings, env environ) []profile {
	raw, err := extension.SettingValue[map[string]profileSetting](s, settingProfiles)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(raw))
	for n := range raw {
		names = append(names, n)
	}
	sort.Strings(names)
	// VS Code lists the profiles in the order of its defaults: bash, zsh, fish, tmux, pwsh, then the user's
	order := []string{"bash", "zsh", "fish", "tmux", "pwsh"}
	slices.SortStableFunc(names, func(a, b string) int {
		ia, ib := slices.Index(order, a), slices.Index(order, b)
		if ia < 0 {
			ia = len(order)
		}
		if ib < 0 {
			ib = len(order)
		}
		return ia - ib
	})
	var out []profile
	for _, n := range names {
		ps := raw[n]
		path := firstFound(env, ps.Path)
		if path == "" {
			continue
		}
		p := profile{name: n, path: path, args: stringList(ps.Args), icon: ps.Icon, env: envMap(ps.Env)}
		out = append(out, p)
	}
	return out
}

// firstFound returns the first program of a path setting (a string or a list of strings) that exists.
func firstFound(env environ, path any) string {
	for _, p := range stringList(path) {
		p = expandHome(p, env.home)
		if found, err := env.lookPath(p); err == nil {
			if abs, err := filepath.Abs(found); err == nil {
				return abs
			}
			return found
		}
	}
	return ""
}

// stringList reads a setting that is a string or a list of strings.
func stringList(v any) []string {
	switch v := v.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

// envMap reads an environment setting: a string is a value, null deletes the variable.
func envMap(v any) map[string]*string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]*string, len(m))
	for k, e := range m {
		switch e := e.(type) {
		case string:
			s := e
			out[k] = &s
		case nil:
			out[k] = nil
		}
	}
	return out
}

// expandHome replaces a leading ~ with the home folder.
func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return p
}

// defaultProfileOf is the profile a new terminal uses: the one terminal.integrated.defaultProfile.linux names when it is among
// the found profiles; else the user's shell ($SHELL), else /bin/sh (SPEC §10.25 "The process").
func defaultProfileOf(s extension.Settings, env environ, profiles []profile) profile {
	if name, err := extension.SettingValue[string](s, settingDefaultProfile); err == nil && name != "" {
		for _, p := range profiles {
			if p.name == name {
				return p
			}
		}
	}
	shell := env.get("SHELL")
	if shell != "" {
		if path := firstFound(env, shell); path != "" {
			// a profile that runs the same program keeps its arguments
			for _, p := range profiles {
				if p.path == path {
					return p
				}
			}
			return profile{name: filepath.Base(path), path: path}
		}
	}
	path := firstFound(env, "/bin/sh")
	if path == "" {
		path = "/bin/sh"
	}
	return profile{name: "sh", path: path}
}

// buildEnv is the environment of a terminal: the environment of Emetos, the variables of terminal.integrated.env.linux and of
// the profile (null deletes one), then the ones that make the program know its terminal: TERM, COLORTERM, TERM_PROGRAM and
// TERM_PROGRAM_VERSION; LANG is set to a UTF-8 locale when terminal.integrated.detectLocale says so.
func buildEnv(base []string, settingEnv, profileEnv map[string]*string, detectLocale, version string) []string {
	order := make([]string, 0, len(base)+8)
	vals := make(map[string]string, len(base)+8)
	set := func(k, v string) {
		if _, ok := vals[k]; !ok {
			order = append(order, k)
		}
		vals[k] = v
	}
	del := func(k string) {
		if _, ok := vals[k]; ok {
			delete(vals, k)
			order = slices.DeleteFunc(order, func(o string) bool { return o == k })
		}
	}
	for _, kv := range base {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			set(k, v)
		}
	}
	for _, m := range []map[string]*string{settingEnv, profileEnv} {
		for k, v := range m {
			if v == nil {
				del(k)
			} else {
				set(k, *v)
			}
		}
	}
	switch detectLocale {
	case "off":
	case "on":
		set("LANG", "en_US.UTF-8")
	default:
		if l := vals["LANG"]; l == "" || !strings.HasSuffix(strings.ToUpper(strings.ReplaceAll(l, "-", "")), ".UTF8") {
			set("LANG", "en_US.UTF-8")
		}
	}
	set("TERM", "xterm-256color")
	set("COLORTERM", "truecolor")
	set("TERM_PROGRAM", "emetos")
	set("TERM_PROGRAM_VERSION", version)
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+vals[k])
	}
	return out
}

// startDir is the folder a terminal starts in: terminal.integrated.cwd (with ${workspaceFolder} replaced) when it is set,
// else the folder of the workspace, else the home folder (SPEC §10.25 "The process").
func startDir(cwdSetting, folder, home string) string {
	if c := strings.TrimSpace(cwdSetting); c != "" {
		c = strings.ReplaceAll(c, "${workspaceFolder}", folder)
		c = strings.ReplaceAll(c, "${workspaceRoot}", folder)
		c = expandHome(c, home)
		if !filepath.IsAbs(c) && folder != "" {
			c = filepath.Join(folder, c)
		}
		return c
	}
	if folder != "" {
		return folder
	}
	if home != "" {
		return home
	}
	return "/"
}
