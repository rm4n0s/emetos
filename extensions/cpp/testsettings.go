package cpp

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The settings of the Testing provider (SPEC §10.32 "The providers", C and C++; "As built (step 26.9)").
const (
	// BuildDirectorySetting is the folder CMake configured the project into, where ctest runs: absolute, or relative to the workspace
	// folder; ${workspaceFolder} stands for the folder. It is CMake Tools' cmake.buildDirectory with Emetos's own name (decision 1700).
	BuildDirectorySetting = "emetos.cpp.ctest.buildDirectory"
	// SuiteDelimiterSetting splits the name of a test into the groups of the tree, like CMake Tools' cmake.ctest.testSuiteDelimiter: with
	// "." the test Calc.Adds is the test Adds in the group Calc.
	SuiteDelimiterSetting = "emetos.cpp.ctest.testSuiteDelimiter"
)

// defaultBuildDirectory is the default of BuildDirectorySetting.
const defaultBuildDirectory = "build"

// testSettingSpecs are the settings of the provider.
func testSettingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{Key: BuildDirectorySetting, Type: "string", Default: defaultBuildDirectory, Category: settingsCategory, Scope: "resource",
			Description: "The folder CMake configured the project into, where the tests are found and run with ctest: absolute, or relative to " +
				"the workspace folder; ${workspaceFolder} stands for the folder. The project must be configured (cmake -S . -B build) and built."},
		{Key: SuiteDelimiterSetting, Type: "string", Default: "", Category: settingsCategory, Scope: "resource",
			Description: "A text that splits the names of the tests into groups in the Testing view: with \".\", the test Calc.Adds is shown as " +
				"Adds in the group Calc. Empty for a flat list."},
	}
}

// buildDirectory is the absolute build folder the settings name.
func (c *cpp) buildDirectory() string {
	dir, _ := extension.SettingValue[string](c.api.Settings(), BuildDirectorySetting)
	return resolveBuildDirectory(dir, c.api.Scope().Folder)
}

// resolveBuildDirectory makes the setting's text an absolute path against the workspace folder; "" is the default.
func resolveBuildDirectory(dir, folder string) string {
	dir = strings.TrimSpace(strings.ReplaceAll(dir, "${workspaceFolder}", folder))
	if dir == "" {
		dir = defaultBuildDirectory
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(folder, dir)
	}
	return filepath.Clean(dir)
}

// suiteDelimiter is the text that splits the names of the tests.
func (c *cpp) suiteDelimiter() string {
	d, _ := extension.SettingValue[string](c.api.Settings(), SuiteDelimiterSetting)
	return d
}
