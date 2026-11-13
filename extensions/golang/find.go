package golang

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// How the extension finds its programs, as plain functions of the settings, so that a running activation (the methods of golang)
// and the checks of the requirements (requirements.go, SPEC §10.36) find the same program the same way.

// goSettings reads the settings the finders need: the value and whether there is one.
type goSettings func(key string) (any, bool)

// settingsOf is the goSettings of an activation.
func settingsOf(s extension.Settings) goSettings {
	return func(key string) (any, bool) {
		v, err := s.Get(key)
		return v, err == nil
	}
}

// str is a string setting, trimmed; "" when it is not one.
func (s goSettings) str(key string) string {
	if s == nil {
		return ""
	}
	v, _ := s(key)
	str, _ := v.(string)
	return strings.TrimSpace(str)
}

// object is an object setting, nil when it is not one.
func (s goSettings) object(key string) map[string]any {
	if s == nil {
		return nil
	}
	v, _ := s(key)
	m, _ := v.(map[string]any)
	return m
}

// alternateTool is the program go.alternateTools names for the tool, or "".
func (s goSettings) alternateTool(name string) string {
	v, _ := s.object(AlternateToolsSetting)[name].(string)
	return strings.TrimSpace(v)
}

// lintTool is the value of go.lintTool, its default when it is not set.
func (s goSettings) lintTool() string {
	if t := s.str(LintToolSetting); t != "" {
		return t
	}
	return defaultLintTool
}

// toolsEnv reads a variable of go.toolsEnvVars, else of Emetos's environment.
func (s goSettings) toolsEnv(name string) string {
	if v, ok := s.object(ToolsEnvVarsSetting)[name].(string); ok {
		return v
	}
	return os.Getenv(name)
}

// findGo finds the go program: go.alternateTools.go, else go in PATH.
func findGo(s goSettings, folder string) (string, error) {
	name := s.alternateTool("go")
	if name == "" {
		name = "go"
	}
	return lookProgram(name, folder)
}

// findGopls finds gopls the way the host starts it (SPEC §9.12): go.goplsPath, else gopls in PATH.
func findGopls(s goSettings, folder string) (string, error) {
	name := s.str(GoplsPathSetting)
	if name == "" {
		name = gopls
	}
	return lookProgram(name, folder)
}

// toolBinDirs are the folders the Go tools are installed to: the bin folder of go.toolsGopath, GOBIN, and the bin folder of each
// GOPATH (the home folder's go when none is set). The variables are those of go.toolsEnvVars, else of Emetos.
func toolBinDirs(s goSettings) []string {
	var dirs []string
	if gp := s.str(ToolsGopathSetting); gp != "" {
		dirs = append(dirs, filepath.Join(gp, "bin"))
	}
	if bin := s.toolsEnv("GOBIN"); bin != "" {
		dirs = append(dirs, bin)
	}
	if gopath := s.toolsEnv("GOPATH"); gopath != "" {
		for _, p := range filepath.SplitList(gopath) {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	return dirs
}

// inToolDirs finds an executable file of the name in the folders of toolBinDirs, "" when none has it.
func inToolDirs(s goSettings, name string) string {
	for _, dir := range toolBinDirs(s) {
		path := filepath.Join(dir, name)
		if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return path
		}
	}
	return ""
}

// findDlv finds dlv: go.alternateTools.dlv (it must exist), else dlv in PATH, else the bin folder of go.toolsGopath, GOBIN or
// the GOPATH (go.toolsEnvVars first), the places "Go: Install/Update Tools" installs it to.
func findDlv(s goSettings, folder string) (string, error) {
	if name := s.alternateTool("dlv"); name != "" {
		path, err := lookProgram(name, folder)
		if err != nil {
			return "", errors.New("GoDlvNotFound", "go.alternateTools names a dlv that cannot be run: "+describe(err), "program", name)
		}
		return path, nil
	}
	if path, err := lookProgram("dlv", folder); err == nil {
		return path, nil
	}
	if path := inToolDirs(s, "dlv"); path != "" {
		return path, nil
	}
	return "", errors.New("GoDlvNotFound", "dlv was not found in go.alternateTools, in PATH or in the bin folder of GOBIN or GOPATH")
}

// lintToolName is the program a lint tool is looked for as: go.alternateTools of the tool, or of its program, else the program.
func lintToolName(s goSettings, tool string) string {
	if name := s.alternateTool(tool); name != "" {
		return name
	}
	if name := s.alternateTool(lintProgram(tool)); name != "" {
		return name
	}
	return lintProgram(tool)
}
