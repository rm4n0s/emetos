package python

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The settings of the Testing provider (SPEC §10.32 "The providers", Python): VS Code Python's python.testing.* names.
const (
	// PytestEnabledSetting and UnittestEnabledSetting choose the framework; with neither set the pack uses pytest when the interpreter has
	// it and unittest otherwise.
	PytestEnabledSetting   = "python.testing.pytestEnabled"
	UnittestEnabledSetting = "python.testing.unittestEnabled"
	// PytestArgsSetting and UnittestArgsSetting are the arguments of the frameworks.
	PytestArgsSetting   = "python.testing.pytestArgs"
	UnittestArgsSetting = "python.testing.unittestArgs"
	// TestCwdSetting is the folder the tests run in (the folder of the workspace when empty).
	TestCwdSetting = "python.testing.cwd"
	// AutoDiscoverSetting and AutoDiscoverPatternSetting make the tests be found again when a file is saved.
	AutoDiscoverSetting        = "python.testing.autoTestDiscoverOnSaveEnabled"
	AutoDiscoverPatternSetting = "python.testing.autoTestDiscoverOnSavePattern"
)

// testSettingSpecs are the settings of the provider.
func testSettingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{Key: PytestEnabledSetting, Type: "boolean", Default: false, Category: categoryPython, Scope: "resource",
			Description: "Run the tests with pytest. When neither pytest nor unittest is enabled, Emetos uses pytest if the interpreter has it, else unittest."},
		{Key: UnittestEnabledSetting, Type: "boolean", Default: false, Category: categoryPython, Scope: "resource",
			Description: "Run the tests with unittest. pytest wins when both are enabled."},
		{Key: PytestArgsSetting, Type: "array", Default: list(), Category: categoryPython, Scope: "resource",
			Description: "The arguments of pytest (`-k slow`, `-m smoke`, `tests`). The folders and files in them are left out of a run of chosen tests."},
		{Key: UnittestArgsSetting, Type: "array", Default: list("-v", "-s", ".", "-p", "*test*.py"), Category: categoryPython, Scope: "resource",
			Description: "The arguments of unittest discovery: `-s` the folder to start in, `-p` the pattern of the test files, `-t` the top-level folder."},
		{Key: TestCwdSetting, Type: "string", Default: "", Category: categoryPython, Scope: "resource",
			Description: "The folder the tests run in: absolute, or relative to the workspace folder. When empty, the workspace folder."},
		{Key: AutoDiscoverSetting, Type: "boolean", Default: true, Category: categoryPython, Scope: "resource",
			Description: "Find the tests again when a Python file is saved."},
		{Key: AutoDiscoverPatternSetting, Type: "string", Default: "**/*.py", Category: categoryPython, Scope: "resource",
			Description: "The files whose saving finds the tests again, as a glob pattern relative to the workspace folder."},
	}
}

// testConfig is what the provider reads from the settings for one discovery or run.
type testConfig struct {
	pytestOn, unittestOn bool
	pytestArgs           []string
	unittestArgs         []string
	cwd                  string
}

// readTestConfig reads the settings; cwd is made absolute against the folder of the scope (${workspaceFolder} is replaced).
func (p *python) readTestConfig() testConfig {
	s := p.api.Settings()
	var c testConfig
	c.pytestOn, _ = extension.SettingValue[bool](s, PytestEnabledSetting)
	c.unittestOn, _ = extension.SettingValue[bool](s, UnittestEnabledSetting)
	c.pytestArgs, _ = extension.SettingValue[[]string](s, PytestArgsSetting)
	c.unittestArgs, _ = extension.SettingValue[[]string](s, UnittestArgsSetting)
	folder := p.api.Scope().Folder
	cwd, _ := extension.SettingValue[string](s, TestCwdSetting)
	cwd = strings.TrimSpace(strings.ReplaceAll(cwd, "${workspaceFolder}", folder))
	switch {
	case cwd == "":
		cwd = folder
	case !filepath.IsAbs(cwd):
		cwd = filepath.Join(folder, cwd)
	}
	c.cwd = filepath.Clean(cwd)
	return c
}

// unittestDiscovery is the start folder, the pattern and the top-level folder of unittest's discovery in the arguments (-s, -p and -t, in
// the separate, joined and long forms); the others (-v) are not used by the helper.
func unittestDiscovery(args []string) (start, pattern, top string) {
	start, pattern = ".", "test*.py"
	take := func(i int, short, long string) (string, int, bool) {
		a := args[i]
		switch {
		case a == short || a == long:
			if i+1 < len(args) {
				return args[i+1], i + 1, true
			}
			return "", i, false
		case strings.HasPrefix(a, long+"="):
			return a[len(long)+1:], i, true
		case strings.HasPrefix(a, short) && len(a) > len(short) && !strings.HasPrefix(a, "--"):
			return a[len(short):], i, true
		}
		return "", i, false
	}
	for i := 0; i < len(args); i++ {
		if v, j, ok := take(i, "-s", "--start-directory"); ok {
			start, i = v, j
		} else if v, j, ok := take(i, "-p", "--pattern"); ok {
			pattern, i = v, j
		} else if v, j, ok := take(i, "-t", "--top-level-directory"); ok {
			top, i = v, j
		}
	}
	return start, pattern, top
}

// pytestValueOptions are the options of pytest whose value is the next argument: the argument after one is never a test path.
var pytestValueOptions = map[string]bool{
	"-c": true, "--rootdir": true, "--confcutdir": true, "--basetemp": true, "-o": true, "--override-ini": true, "--ignore": true,
	"--ignore-glob": true, "--deselect": true, "--junitxml": true, "--log-file": true, "-k": true, "-m": true, "-p": true, "--tb": true,
	"-W": true, "--durations": true, "--maxfail": true, "--import-mode": true, "-r": true, "--color": true,
}

// withoutTestPaths drops the arguments that name an existing file or folder (with an optional ::node part) from pytest's arguments: they
// say what to run, and a run of chosen tests names those itself.
func withoutTestPaths(args []string, exists func(path string) bool) []string {
	var out []string
	skip := false
	for _, a := range args {
		switch {
		case skip:
			skip = false
			out = append(out, a)
		case strings.HasPrefix(a, "-"):
			out = append(out, a)
			skip = pytestValueOptions[a]
		default:
			path, _, _ := strings.Cut(a, "::")
			if exists(path) {
				continue
			}
			out = append(out, a)
		}
	}
	return out
}
