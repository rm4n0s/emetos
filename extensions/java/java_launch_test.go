package java

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

func TestTheManifestDeclaresTheServerTheCommandsTheKeysAndTheSettings(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != ID || m.Name != "Java" {
		t.Errorf("manifest = %s %q", m.ID, m.Name)
	}
	if len(m.Contributes.LanguageServers) != 1 {
		t.Fatalf("servers = %+v", m.Contributes.LanguageServers)
	}
	sv := m.Contributes.LanguageServers[0]
	if sv.ID != ServerID || !sv.OwnUI || sv.TraceKey != TraceSetting || !slices.Equal(sv.Languages, []string{"java"}) {
		t.Errorf("server = %+v", sv)
	}
	settings := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		settings[s.Key] = s
	}
	for _, key := range []string{PathSetting, JavaHomeSetting, VMArgsSetting} {
		if settings[key].Scope != "machine" {
			t.Errorf("%s is read from %q settings, want the user's only", key, settings[key].Scope)
		}
	}
	if settings[VMArgsSetting].Default != defaultVMArgs {
		t.Errorf("vmargs default = %v", settings[VMArgsSetting].Default)
	}
	for _, key := range []string{"java.configuration.maven.userSettings", "java.configuration.maven.globalSettings", "java.import.gradle.home", "java.import.gradle.java.home",
		"java.import.gradle.user.home", "java.import.gradle.arguments", "java.import.gradle.jvmArguments", "java.configuration.runtimes"} {
		if settings[key].Scope != "machine" {
			t.Errorf("%s is read from %q settings, want the user's only (it names a program or a file outside the folder)", key, settings[key].Scope)
		}
	}
	if s := settings["java.import.maven.offline.enabled"]; s.Scope == "machine" || s.Type != "boolean" {
		t.Errorf("a setting of the folder = %+v", s)
	}
	keys := map[string]bool{}
	for _, c := range m.Contributes.Keybindings {
		keys[c.Command] = true
	}
	if !keys[UpdateProjectConfigurationCommand] || !keys[OrganizeImportsCommand] || len(m.Contributes.Keybindings) != 2 {
		t.Errorf("keybindings = %+v", m.Contributes.Keybindings)
	}
	if got := m.Events.Subscribes; !slices.Contains(got, extension.TopicEditorActiveChanged) {
		t.Errorf("subscribes = %v", got)
	}
}

func TestEverySettingHasAKnownTypeADefaultOfThatTypeAndADescription(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range New().Manifest().Contributes.Settings {
		if seen[s.Key] {
			t.Errorf("%s is declared twice", s.Key)
		}
		seen[s.Key] = true
		if !strings.HasPrefix(s.Key, "java.") || s.Description == "" || (s.Category != "Java" && s.Category != "") {
			t.Errorf("setting %+v lacks its description or category", s)
		}
		ok := false
		switch s.Type {
		case "string":
			_, ok = s.Default.(string)
		case "boolean":
			_, ok = s.Default.(bool)
		case "integer", "number":
			_, ok = s.Default.(float64)
		case "array":
			_, ok = s.Default.([]any)
		}
		if !ok {
			t.Errorf("%s: the default %v (%T) is no %s", s.Key, s.Default, s.Default, s.Type)
		}
		for _, e := range s.Enum {
			if s.Type == "string" {
				if _, isString := e.(string); !isString {
					t.Errorf("%s: enum member %v", s.Key, e)
				}
			}
		}
		if len(s.Enum) > 0 && s.Type == "string" && !slices.Contains(s.Enum, s.Default) {
			t.Errorf("%s: the default %v is not in its enum", s.Key, s.Default)
		}
	}
	// the path setting is declared by the server's config
	if !seen[PathSetting] {
		t.Errorf("%s is not declared", PathSetting)
	}
}

func TestAStartRunsJavaOnTheLauncherWithAWorkspaceInTheCache(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "25.0.4.1", files: map[string]string{"pom.xml": "<project/>"}})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	wantJava := filepath.Join(r.bin, "java")
	if l.Program != wantJava {
		t.Errorf("program = %s, want %s", l.Program, wantJava)
	}
	if l.Root != r.folder {
		t.Errorf("root = %s", l.Root)
	}
	args := strings.Join(l.Args, "\n")
	launcher := filepath.Join(r.jdtls, "plugins", "org.eclipse.equinox.launcher_1.8.0.v20260804-1928.jar")
	for _, want := range []string{"-jar\n" + launcher, "-Declipse.application=org.eclipse.jdt.ls.core.id1", "-Xlog:disable", "-Xmx1G", "-Djdk.xml.maxGeneralEntitySizeLimit=0", "--add-modules=ALL-SYSTEM"} {
		if !strings.Contains(args, want) {
			t.Errorf("the arguments lack %q:\n%s", want, args)
		}
	}
	// the workspace and the configuration are in the cache, one folder for the root, and nothing is written to the folder or to jdtls
	cacheDirs, _ := filepath.Glob(filepath.Join(r.cache, "emetos", "jdtls", "work-*"))
	if len(cacheDirs) != 1 {
		t.Fatalf("cache folders = %v", cacheDirs)
	}
	for _, want := range []string{"-configuration\n" + filepath.Join(cacheDirs[0], "config"), "-data\n" + filepath.Join(cacheDirs[0], "workspace")} {
		if !strings.Contains(args, want) {
			t.Errorf("the arguments lack %q:\n%s", want, args)
		}
	}
	ini, err := os.ReadFile(filepath.Join(cacheDirs[0], "config", "config.ini"))
	if err != nil || !strings.Contains(string(ini), "osgi.bundles") {
		t.Errorf("config.ini = %q, %v", ini, err)
	}
	for _, dir := range []string{r.folder, r.jdtls} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.Name() == ".metadata" || e.Name() == "workspace" || e.Name() == "config" {
				t.Errorf("%s holds %s", dir, e.Name())
			}
		}
	}
	// the options of initialize
	opts := launchOptions(t, l)
	folders, _ := opts["workspaceFolders"].([]any)
	if len(folders) != 1 || folders[0] != "file://"+r.folder {
		t.Errorf("workspaceFolders = %v", opts["workspaceFolders"])
	}
	caps, _ := opts["extendedClientCapabilities"].(map[string]any)
	for _, want := range []string{"classFileContentsSupport", "progressReportProvider", "actionableNotificationSupported"} {
		if caps[want] != true {
			t.Errorf("extendedClientCapabilities lacks %s: %v", want, caps)
		}
	}
	if len(caps) != 3 {
		t.Errorf("extendedClientCapabilities = %v: only what Emetos does is declared", caps)
	}
	settings, _ := opts["settings"].(map[string]any)
	if !reflect.DeepEqual(settings, map[string]any{"java": map[string]any{}}) {
		t.Errorf("settings = %v: nothing differs from the defaults", settings)
	}
	// the item names the server and its version
	r.api.Pump()
	if tip := r.item().Tooltip; !strings.Contains(tip, "jdtls 1.63.0: "+r.jdtls) {
		t.Errorf("tooltip = %q", tip)
	}
}

func TestTheRootIsTheNearestFolderWithABuildFile(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21", files: map[string]string{"pom.xml": "<project/>", "src/a.txt": "x"}})
	// the tab is on a subfolder of the project
	sub := filepath.Join(r.folder, "src")
	api := r.api
	_ = api
	got := langserver.ResolveRoot(sub, rootMarkers, r.home)
	if got != r.folder {
		t.Errorf("root = %s, want %s", got, r.folder)
	}
	for _, name := range []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", ".project"} {
		dir := filepath.Join(t.TempDir(), "p")
		writeFile(t, dir, name, "", 0o644)
		writeFile(t, dir, "src/x.java", "", 0o644)
		if got := langserver.ResolveRoot(filepath.Join(dir, "src"), rootMarkers, r.home); got != dir {
			t.Errorf("with %s the root = %s, want %s", name, got, dir)
		}
	}
}

func TestSettingsThatDifferFromTheirDefaultReachTheServerNested(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21", settings: map[string]any{
		"java.import.maven.offline.enabled":    true,
		"java.completion.guessMethodArguments": "off",
		"java.import.gradle.java.home":         "/opt/jdk",
		"java.format.enabled":                  true, // the default: not sent
		"java.import.exclusions":               []any{"**/gen/**"},
		"java.completion.importOrder":          []any{"#", "java"},
		"java.maxConcurrentBuilds":             float64(4),
		"java.project.outputPath":              "  ", // empty after trimming: not set
		"java.cleanup.actions":                 []any{},
	}})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	got := launchOptions(t, l)["settings"]
	want := map[string]any{"java": map[string]any{
		"import": map[string]any{
			"maven":      map[string]any{"offline": map[string]any{"enabled": true}},
			"gradle":     map[string]any{"java": map[string]any{"home": "/opt/jdk"}},
			"exclusions": []any{"**/gen/**"},
		},
		"completion":          map[string]any{"guessMethodArguments": "off", "importOrder": []any{"#", "java"}},
		"maxConcurrentBuilds": float64(4),
		"cleanup":             map[string]any{"actions": []any{}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("settings = %v\nwant     %v", got, want)
	}
}

func TestTheOptionsOfTheJVMComeFromTheSetting(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21", settings: map[string]any{VMArgsSetting: `-Xmx2G -Dname="a b" -Xlog:all=warning`}})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(l.Args, "\n")
	for _, want := range []string{"-Xmx2G", "-Dname=a b", "-Xlog:all=warning"} {
		if !strings.Contains(args, want) {
			t.Errorf("the arguments lack %q", want)
		}
	}
	if strings.Contains(args, "-Xlog:disable") || strings.Contains(args, "-Xmx1G") {
		t.Errorf("the default options are still there:\n%s", args)
	}
}

func TestEveryFailureOfAStartIsSaidAndLeavesTheServerMissing(t *testing.T) {
	tests := []struct {
		name    string
		o       rigOptions
		route   string
		item    string
		message string
	}{
		{"no jdtls", rigOptions{javaVersion: "21", noJDTLS: true}, "findJDTLS.ServerNotFound", "jdtls not found", "jdtls was not found."},
		{"a wrong path", rigOptions{javaVersion: "21", settings: map[string]any{PathSetting: "/nowhere/jdtls"}}, "findJDTLS.ServerPathInvalid", "wrong path", "java.jdt.ls.path is set to /nowhere/jdtls"},
		{"no java", rigOptions{}, "resolveJava.JavaNotFound", "jdtls cannot start", "jdtls cannot be started: java was not found"},
		{"Java 17", rigOptions{javaVersion: "17.0.9"}, "resolveJava.JavaTooOld", "jdtls cannot start", "Java 17"},
		{"a wrong java home", rigOptions{javaVersion: "21", settings: map[string]any{JavaHomeSetting: "/nowhere"}}, "resolveJava.JavaHomeInvalid", "jdtls cannot start", "java.jdt.ls.java.home is set to /nowhere"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.o)
			_, err := r.run()
			wantRoute(t, err, tt.route)
			if text := r.item().Text; !strings.Contains(text, tt.item) {
				t.Errorf("item = %q, want it to say %q", text, tt.item)
			}
			if !strings.Contains(strings.Join(r.notes(), "\n"), tt.message) {
				t.Errorf("notifications = %v, want one with %q", r.notes(), tt.message)
			}
		})
	}
}

func TestTheFailuresOfTheWorkspaceFoldersEachHaveTheirOwnTag(t *testing.T) {
	// each case breaks one step of making the folders of the workspace: the configuration of jdtls, the folder of the configuration, the
	// folder of the workspace, the file written
	tests := []struct {
		name   string
		break_ func(t *testing.T, r *rig, w workspacePaths)
		route  string
	}{
		{"the configuration of jdtls cannot be read", func(t *testing.T, r *rig, w workspacePaths) {
			if err := os.Chmod(filepath.Join(r.jdtls, configFolder(), "config.ini"), 0); err != nil {
				t.Fatal(err)
			}
		}, "prepareConfiguration.JavaConfigurationReadFailed"},
		{"the folder of the configuration cannot be made", func(t *testing.T, r *rig, w workspacePaths) {
			writeFile(t, w.dir, "config", "a file where a folder goes", 0o644)
		}, "prepareConfiguration.JavaConfigurationFolderFailed"},
		{"the folder of the workspace cannot be made", func(t *testing.T, r *rig, w workspacePaths) {
			writeFile(t, w.dir, "workspace", "a file where a folder goes", 0o644)
		}, "prepareConfiguration.JavaWorkspaceFolderFailed"},
		{"the configuration cannot be written", func(t *testing.T, r *rig, w workspacePaths) {
			if err := os.MkdirAll(filepath.Join(w.config, "config.ini"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "prepareConfiguration.JavaConfigurationWriteFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Getuid() == 0 && strings.Contains(tt.name, "read") {
				t.Skip("root reads every file")
			}
			r := newRig(t, rigOptions{javaVersion: "21"})
			w := workspaceFor(withCache(env(r.folder, r.home, "", nil), r.cache), r.folder)
			if err := os.MkdirAll(w.dir, 0o755); err != nil {
				t.Fatal(err)
			}
			tt.break_(t, r, w)
			_, err := r.api.Launch(t.Context(), ServerID)
			wantRoute(t, err, tt.route)
		})
	}
}

func TestOnlyTheFolderOfAProjectBelowTheCacheIsDeleted(t *testing.T) {
	base := t.TempDir()
	e := withCache(env(base, base, "", nil), filepath.Join(base, "cache"))
	for _, dir := range []string{"", base, filepath.Join(base, "cache"), filepath.Join(base, "cache", "emetos", "jdtls", "a", "b"), filepath.Join(base, "elsewhere", "x")} {
		err := removeWorkspace(e, workspacePaths{dir: dir})
		wantRoute(t, err, "removeWorkspace.JavaWorkspaceUnsafe")
	}
	// a folder that cannot be emptied
	dir := filepath.Join(base, "cache", "emetos", "jdtls", "p-1")
	writeFile(t, dir, "locked/file", "x", 0o644)
	if err := os.Chmod(filepath.Join(dir, "locked"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "locked"), 0o755) })
	if os.Getuid() != 0 {
		wantRoute(t, removeWorkspace(e, workspacePaths{dir: dir}), "removeWorkspace.JavaWorkspaceRemoveFailed")
	}
	if err := os.Chmod(filepath.Join(dir, "locked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeWorkspace(e, workspacePaths{dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the folder is still there: %v", err)
	}
}

func TestTheWorkspaceOfARootHasItsOwnFolder(t *testing.T) {
	e := env("/w", "/home/u", "", nil)
	a, b := workspaceFor(e, "/w/a/app"), workspaceFor(e, "/w/b/app")
	if a.dir == b.dir {
		t.Errorf("two roots named app share %s", a.dir)
	}
	if a != workspaceFor(e, "/w/a/app") {
		t.Error("the same root gave another folder")
	}
	if !strings.HasPrefix(a.dir, filepath.Join("/home/u", ".cache", "emetos", "jdtls", "app-")) {
		t.Errorf("dir = %s", a.dir)
	}
	e.Environ = func(name string) string {
		if name == "XDG_CACHE_HOME" {
			return "/var/cache/x"
		}
		return ""
	}
	if got := workspaceFor(e, "/w/a/app"); !strings.HasPrefix(got.dir, "/var/cache/x/emetos/jdtls/app-") {
		t.Errorf("dir with XDG_CACHE_HOME = %s", got.dir)
	}
	if root := workspaceFor(e, "/"); !strings.HasPrefix(filepath.Base(root.dir), "root-") {
		t.Errorf("dir of / = %s", root.dir)
	}
}

func TestAChangeOfASettingTheStartReadRestartsTheServer(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.api.SetSetting("java.format.enabled", true) // the same as its default: it was read, so it is watched, but nothing differs
	r.settle()
	if got := r.api.ServerRestarts(ServerID); got != 0 {
		t.Errorf("restarts = %d after a setting that did not change", got)
	}
	r.api.SetSetting("java.import.maven.offline.enabled", true)
	r.settle()
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d after java.import.maven.offline.enabled", got)
	}
	// the options of the JVM, the JDK and the path of the server are read too
	_, _ = r.run()
	r.api.SetSetting(JavaHomeSetting, "/x")
	r.settle()
	if got := r.api.ServerRestarts(ServerID); got != 2 {
		t.Errorf("restarts = %d after %s", got, JavaHomeSetting)
	}
}

func TestInstallHelp(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21", noJDTLS: true})
	if _, err := r.run(); err == nil {
		t.Fatal("jdtls was found")
	}
	prog := r.ext.serverConfig().Programs[0]
	arch, ok := prog.Install.CommandFor(langserver.FamilyArch)
	if !ok || arch.Command != "sudo pacman -S jdtls" {
		t.Errorf("Arch = %+v", arch)
	}
	for _, family := range []string{langserver.FamilyFedora, langserver.FamilyDebian, ""} {
		c, ok := prog.Install.CommandFor(family)
		if !ok || !strings.Contains(c.Command, "download.eclipse.org/jdtls") || !strings.Contains(c.Command, "~/.local/share/jdtls") {
			t.Errorf("%q = %+v", family, c)
		}
	}
	// the folder the command makes is a place jdtls is looked for in
	found := false
	for _, d := range afterDirs {
		found = found || d == "~/.local/share/jdtls"
	}
	if !found {
		t.Error("the folder of the install command is not searched")
	}
	_ = errors.New
}

func TestTheJavaFileInFrontIsATriggerFileOfTheStart(t *testing.T) {
	tests := []struct {
		name   string
		events []extension.ActiveEditor
		want   string
	}{
		{"none was in front", nil, ""},
		{"a Java file of the folder", []extension.ActiveEditor{{URI: "file://@/src/A.java", LanguageID: "java"}}, "file://@/src/A.java"},
		{"the last one", []extension.ActiveEditor{{URI: "file://@/A.java", LanguageID: "java"}, {URI: "file://@/B.java", LanguageID: "java"}}, "file://@/B.java"},
		{"another language is not one", []extension.ActiveEditor{{URI: "file://@/a.txt", LanguageID: "plaintext"}}, ""},
		{"it stays when another language comes after", []extension.ActiveEditor{{URI: "file://@/A.java", LanguageID: "java"}, {URI: "file://@/a.txt", LanguageID: "plaintext"}}, "file://@/A.java"},
		{"a file outside the folder", []extension.ActiveEditor{{URI: "file:///elsewhere/A.java", LanguageID: "java"}}, ""},
		{"a class file is no file", []extension.ActiveEditor{{URI: "emetos-virtual:/jdt/contents/x/Y.class", LanguageID: "java"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{javaVersion: "21"})
			for _, ev := range tt.events {
				ev.URI = strings.ReplaceAll(ev.URI, "@", r.folder)
				r.api.Emit(extension.TopicEditorActiveChanged, ev)
			}
			r.settle()
			l, err := r.run()
			if err != nil {
				t.Fatal(err)
			}
			got, _ := launchOptions(t, l)["triggerFiles"].([]any)
			want := strings.ReplaceAll(tt.want, "@", r.folder)
			switch {
			case want == "" && got != nil:
				t.Errorf("triggerFiles = %v", got)
			case want != "" && (len(got) != 1 || got[0] != want):
				t.Errorf("triggerFiles = %v, want [%s]", got, want)
			}
		})
	}
}

func TestTheMavenAndGradleProjectsOfTestdataAreRoots(t *testing.T) {
	for name, marker := range map[string]string{"maven": "pom.xml", "gradle": "build.gradle"} {
		t.Run(name, func(t *testing.T) {
			project := filepath.Join(t.TempDir(), name)
			src := filepath.Join("..", "..", "testdata", "java", name)
			err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(src, path)
				text, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				writeFile(t, project, rel, string(text), 0o644)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(project, marker)); err != nil {
				t.Fatal(err)
			}
			deep := filepath.Join(project, "src", "main", "java", "dev", "emetos", "app")
			if got := langserver.ResolveRoot(deep, rootMarkers, t.TempDir()); got != project {
				t.Errorf("root of %s = %s, want %s", deep, got, project)
			}
			if got := langserver.FindProjects(filepath.Dir(project), rootMarkers, langserver.ProjectOptions{}); !slices.Equal(got, []string{project}) {
				t.Errorf("projects = %v", got)
			}
		})
	}
}
