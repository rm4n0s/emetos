package java

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The finding of jdtls and java and the command line, as tables.

// env makes the Env of a test: the settings are the map's.
func env(folder, home, path string, set map[string]any) langserver.Env {
	return langserver.Env{Folder: folder, Home: home, Path: path, Environ: func(string) string { return "" },
		Settings: langserver.NewSettings(func(key string) (any, bool) { v, ok := set[key]; return v, ok })}
}

func TestFindJDTLS(t *testing.T) {
	tests := []struct {
		name string
		// layout makes the folders under base and returns the setting, the PATH and the settings of the test.
		layout func(t *testing.T, base string) (set map[string]any, path string)
		want   func(base string) langserver.Found
		route  string
		msg    string
	}{
		{"the setting names the folder", func(t *testing.T, base string) (map[string]any, string) {
			fakeJDTLS(t, filepath.Join(base, "tools", "jdtls"), "1.63.0.1")
			return map[string]any{PathSetting: filepath.Join(base, "tools", "jdtls")}, ""
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "tools", "jdtls"), Source: langserver.FromSetting}
		}, "", ""},
		{"the setting names bin/jdtls", func(t *testing.T, base string) (map[string]any, string) {
			fakeJDTLS(t, filepath.Join(base, "tools", "jdtls"), "1.63.0.1")
			return map[string]any{PathSetting: filepath.Join(base, "tools", "jdtls", "bin", "jdtls")}, ""
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "tools", "jdtls"), Source: langserver.FromSetting}
		}, "", ""},
		{"the setting is relative to the folder of the tab", func(t *testing.T, base string) (map[string]any, string) {
			fakeJDTLS(t, filepath.Join(base, "work", "tools", "jdtls"), "1.63.0.1")
			return map[string]any{PathSetting: "tools/jdtls"}, ""
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "work", "tools", "jdtls"), Source: langserver.FromSetting}
		}, "", ""},
		{"the setting names a folder that is not there", func(t *testing.T, base string) (map[string]any, string) {
			return map[string]any{PathSetting: filepath.Join(base, "nothing")}, ""
		}, nil, "findJDTLS.ServerPathInvalid", "does not exist"},
		{"the setting names a folder that is not jdtls", func(t *testing.T, base string) (map[string]any, string) {
			writeFile(t, base, "other/plugins/readme.txt", "x", 0o644)
			return map[string]any{PathSetting: filepath.Join(base, "other")}, ""
		}, nil, "findJDTLS.ServerPathInvalid", "has no plugins/org.eclipse.equinox.launcher_*.jar"},
		{"the setting names a folder with no configuration", func(t *testing.T, base string) (map[string]any, string) {
			writeFile(t, base, "other/plugins/org.eclipse.equinox.launcher_1.jar", "x", 0o644)
			return map[string]any{PathSetting: filepath.Join(base, "other")}, ""
		}, nil, "findJDTLS.ServerPathInvalid", "config.ini"},
		{"the setting wins over PATH", func(t *testing.T, base string) (map[string]any, string) {
			fakeJDTLS(t, filepath.Join(base, "a"), "1.63.0.1")
			fakeJDTLS(t, filepath.Join(base, "b"), "1.63.0.1")
			return map[string]any{PathSetting: filepath.Join(base, "a")}, filepath.Join(base, "b", "bin")
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "a"), Source: langserver.FromSetting}
		}, "", ""},
		{"jdtls in PATH is a link into its folder", func(t *testing.T, base string) (map[string]any, string) {
			fakeJDTLS(t, filepath.Join(base, "opt", "jdtls"), "1.63.0.1")
			if err := os.MkdirAll(filepath.Join(base, "bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(base, "opt", "jdtls", "bin", "jdtls"), filepath.Join(base, "bin", "jdtls")); err != nil {
				t.Fatal(err)
			}
			return nil, filepath.Join(base, "bin")
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "opt", "jdtls"), Source: langserver.FromPath}
		}, "", ""},
		{"jdtls in PATH is a script of a distribution", func(t *testing.T, base string) (map[string]any, string) {
			writeFile(t, base, "bin/jdtls", "#!/bin/sh\n", 0o755)
			return nil, filepath.Join(base, "bin")
		}, nil, "findJDTLS.ServerNotFound", "does not stand beside a jdtls folder"},
		{"the folder the install command makes", func(t *testing.T, base string) (map[string]any, string) {
			fakeJDTLS(t, filepath.Join(base, "home", ".local", "share", "jdtls"), "1.63.0.1")
			return nil, filepath.Join(base, "bin")
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "home", ".local", "share", "jdtls"), Source: langserver.FromAfter}
		}, "", ""},
		{"nothing", func(t *testing.T, base string) (map[string]any, string) {
			return nil, filepath.Join(base, "bin")
		}, nil, "findJDTLS.ServerNotFound", "jdtls was not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			set, path := tt.layout(t, base)
			got, err := findJDTLS(env(filepath.Join(base, "work"), filepath.Join(base, "home"), path, set))
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				if !strings.Contains(err.Error(), tt.msg) {
					t.Errorf("message %q lacks %q", err.Error(), tt.msg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(base); got != want {
				t.Errorf("found %+v, want %+v", got, want)
			}
		})
	}
}

func TestNotFoundListsThePlacesSearched(t *testing.T) {
	base := t.TempDir()
	_, err := findJDTLS(env(base, filepath.Join(base, "home"), "/p1:/p2", nil))
	wantRoute(t, err, "findJDTLS.ServerNotFound")
	appErr, _ := errors.FromError(err)
	searched, _ := appErr.Metadata["searched"].(string)
	for _, want := range []string{"/p1", "/p2", filepath.Join(base, "home", ".local", "share", "jdtls"), "/usr/share/java/jdtls", "/opt/jdtls"} {
		if !strings.Contains(searched, want) {
			t.Errorf("searched = %q lacks %q", searched, want)
		}
	}
}

func TestCheckPathForSetPath(t *testing.T) {
	base := t.TempDir()
	fakeJDTLS(t, filepath.Join(base, "jdtls"), "1.63.0.1")
	e := env(base, base, "", nil)
	tests := []struct {
		text, contains string
	}{
		{"", ""},
		{"  ", ""},
		{filepath.Join(base, "jdtls"), ""},
		{filepath.Join(base, "jdtls", "bin", "jdtls"), ""},
		{filepath.Join(base, "none"), "does not exist"},
		{base, "no plugins/org.eclipse.equinox.launcher_*.jar"},
	}
	for _, tt := range tests {
		got := checkJDTLSPath(e, tt.text)
		if (tt.contains == "") != (got == "") || !strings.Contains(got, tt.contains) {
			t.Errorf("checkJDTLSPath(%q) = %q, want it to contain %q", tt.text, got, tt.contains)
		}
	}
}

func TestJDTLSVersionComesFromTheNameOfTheCoreJar(t *testing.T) {
	base := t.TempDir()
	fakeJDTLS(t, base, "1.63.0.202610191322")
	in, why := checkInstall(base)
	if why != "" {
		t.Fatal(why)
	}
	if got := jdtlsVersion(in); got != "1.63.0" {
		t.Errorf("version = %q", got)
	}
	if err := os.Remove(filepath.Join(base, "plugins", "org.eclipse.jdt.ls.core_1.63.0.202610191322.jar")); err != nil {
		t.Fatal(err)
	}
	if got := jdtlsVersion(in); got != "" {
		t.Errorf("version without the jar = %q", got)
	}
}

func TestTheNewestLauncherIsUsedAndThePlatformOnesAreNot(t *testing.T) {
	base := t.TempDir()
	fakeJDTLS(t, base, "1.63.0.1")
	writeFile(t, base, "plugins/org.eclipse.equinox.launcher_1.9.0.v1.jar", "x", 0o644)
	in, why := checkInstall(base)
	if why != "" {
		t.Fatal(why)
	}
	if filepath.Base(in.launcher) != "org.eclipse.equinox.launcher_1.9.0.v1.jar" {
		t.Errorf("launcher = %s", in.launcher)
	}
}

func TestParseJavaMajor(t *testing.T) {
	tests := []struct {
		out  string
		want string
		ok   bool
	}{
		{`openjdk version "25.0.4.1" 2026-08-18`, "25", true},
		{`openjdk version "21" 2023-09-19`, "21", true},
		{`openjdk version "21.0.3" 2024-04-16 LTS`, "21", true},
		{`java version "1.8.0_292"`, "8", true},
		{`openjdk version "17-ea" 2021-09-14`, "17", true},
		{`Picked up JAVA_TOOL_OPTIONS: -Xmx1g` + "\n" + `openjdk version "22.0.1"`, "22", true},
		{`no version here`, "", false},
		{``, "", false},
	}
	for _, tt := range tests {
		got, ok := parseJavaMajor(tt.out)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseJavaMajor(%q) = %q, %v, want %q, %v", tt.out, got, ok, tt.want, tt.ok)
		}
	}
}

func TestResolveJava(t *testing.T) {
	tests := []struct {
		name string
		// layout makes the programs and returns the settings, PATH and JAVA_HOME.
		layout func(t *testing.T, base string) (set map[string]any, path, javaHome string)
		want   func(base string) string
		major  int
		route  string
		msg    string
	}{
		{"java in PATH", func(t *testing.T, base string) (map[string]any, string, string) {
			fakeJava(t, filepath.Join(base, "bin"), "25.0.4.1")
			return nil, filepath.Join(base, "bin"), ""
		}, func(base string) string { return filepath.Join(base, "bin", "java") }, 25, "", ""},
		{"JAVA_HOME wins over PATH", func(t *testing.T, base string) (map[string]any, string, string) {
			fakeJava(t, filepath.Join(base, "bin"), "17.0.1")
			fakeJava(t, filepath.Join(base, "jdk", "bin"), "21.0.3")
			return nil, filepath.Join(base, "bin"), filepath.Join(base, "jdk")
		}, func(base string) string { return filepath.Join(base, "jdk", "bin", "java") }, 21, "", ""},
		{"the setting wins over JAVA_HOME", func(t *testing.T, base string) (map[string]any, string, string) {
			fakeJava(t, filepath.Join(base, "jdk17", "bin"), "17.0.1")
			fakeJava(t, filepath.Join(base, "jdk21", "bin"), "21")
			return map[string]any{JavaHomeSetting: filepath.Join(base, "jdk21")}, "", filepath.Join(base, "jdk17")
		}, func(base string) string { return filepath.Join(base, "jdk21", "bin", "java") }, 21, "", ""},
		{"a JAVA_HOME with no java is left", func(t *testing.T, base string) (map[string]any, string, string) {
			fakeJava(t, filepath.Join(base, "bin"), "23.0.1")
			return nil, filepath.Join(base, "bin"), filepath.Join(base, "empty")
		}, func(base string) string { return filepath.Join(base, "bin", "java") }, 23, "", ""},
		{"the setting names a folder with no java", func(t *testing.T, base string) (map[string]any, string, string) {
			return map[string]any{JavaHomeSetting: filepath.Join(base, "nothing")}, "", ""
		}, nil, 0, "resolveJava.JavaHomeInvalid", "bin/java"},
		{"no java at all", func(t *testing.T, base string) (map[string]any, string, string) {
			return nil, filepath.Join(base, "bin"), ""
		}, nil, 0, "resolveJava.JavaNotFound", "Java 21 or newer"},
		{"Java 17", func(t *testing.T, base string) (map[string]any, string, string) {
			fakeJava(t, filepath.Join(base, "bin"), "17.0.9")
			return nil, filepath.Join(base, "bin"), ""
		}, nil, 0, "resolveJava.JavaTooOld", "Java 17"},
		{"Java 8", func(t *testing.T, base string) (map[string]any, string, string) {
			fakeJava(t, filepath.Join(base, "bin"), "1.8.0_292")
			return nil, filepath.Join(base, "bin"), ""
		}, nil, 0, "resolveJava.JavaTooOld", "Java 8"},
		{"a java that prints no version", func(t *testing.T, base string) (map[string]any, string, string) {
			writeFile(t, base, "bin/java", "#!/bin/sh\necho hello\n", 0o755)
			return nil, filepath.Join(base, "bin"), ""
		}, nil, 0, "QueryVersion.ServerVersionUnreadable", "printed no version"},
		{"a java that cannot be run", func(t *testing.T, base string) (map[string]any, string, string) {
			writeFile(t, base, "bin/java", "#!/bin/sh\necho 'cannot start' >&2\nexit 3\n", 0o755)
			return nil, filepath.Join(base, "bin"), ""
		}, nil, 0, "QueryVersion.ServerVersionFailed", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			set, path, javaHome := tt.layout(t, base)
			if err := os.MkdirAll(filepath.Join(base, "work"), 0o755); err != nil {
				t.Fatal(err)
			}
			e := env(filepath.Join(base, "work"), filepath.Join(base, "home"), path, set)
			e.Environ = func(name string) string {
				if name == "JAVA_HOME" {
					return javaHome
				}
				return ""
			}
			got, err := resolveJava(context.Background(), e)
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				if !strings.Contains(err.Error(), tt.msg) {
					t.Errorf("message %q lacks %q", err.Error(), tt.msg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.path != tt.want(base) || got.major != tt.major {
				t.Errorf("java = %+v, want %s %d", got, tt.want(base), tt.major)
			}
		})
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"-Xmx1G", []string{"-Xmx1G"}},
		{"-Xmx1G  -Xms100m\t-Xlog:disable\n", []string{"-Xmx1G", "-Xms100m", "-Xlog:disable"}},
		{`-Dname="two words" -Dother=x`, []string{"-Dname=two words", "-Dother=x"}},
		{`-Dname='two words'`, []string{"-Dname=two words"}},
		{`-Dpath=a\ b`, []string{"-Dpath=a b"}},
		{`"" x`, []string{"", "x"}},
		{`-Dq="it's"`, []string{"-Dq=it's"}},
		{`'back\slash'`, []string{`back\slash`}},
	}
	for _, tt := range tests {
		if got := splitArgs(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitArgs(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTheCommandLine(t *testing.T) {
	in := install{dir: "/opt/jdtls", launcher: "/opt/jdtls/plugins/org.eclipse.equinox.launcher_1.8.0.jar", config: "/opt/jdtls/config_linux"}
	tests := []struct {
		name   string
		major  int
		vmargs []string
		has    []string
		not    []string
	}{
		{"Java 21 with the default options", 21, splitArgs(defaultVMArgs),
			[]string{"-Declipse.application=org.eclipse.jdt.ls.core.id1", "-Declipse.product=org.eclipse.jdt.ls.core.product", "-Xmx1G", "-Xlog:disable", "--add-modules=ALL-SYSTEM"},
			[]string{"-Djdk.xml.maxGeneralEntitySizeLimit=0"}},
		{"Java 25 limits nothing of XML", 25, splitArgs(defaultVMArgs), []string{"-Djdk.xml.maxGeneralEntitySizeLimit=0", "-Djdk.xml.totalEntitySizeLimit=0"}, nil},
		{"Java 24 too", 24, nil, []string{"-Djdk.xml.totalEntitySizeLimit=0"}, nil},
		{"options without -Xlog get it", 21, []string{"-Xmx2G"}, []string{"-Xmx2G", "-Xlog:disable"}, nil},
		{"options with -Xlog keep theirs", 21, []string{"-Xlog:all=warning"}, []string{"-Xlog:all=warning"}, []string{"-Xlog:disable"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := commandLine(in, javaProgram{major: tt.major}, tt.vmargs, "/cache/config", "/cache/workspace")
			joined := " " + strings.Join(args, " ") + " "
			for _, want := range tt.has {
				if !strings.Contains(joined, " "+want+" ") {
					t.Errorf("the arguments lack %q: %v", want, args)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(joined, " "+not+" ") {
					t.Errorf("the arguments have %q: %v", not, args)
				}
			}
			// the end is the launcher, the configuration and the workspace, in that order
			tail := args[len(args)-6:]
			want := []string{"-jar", in.launcher, "-configuration", "/cache/config", "-data", "/cache/workspace"}
			if !reflect.DeepEqual(tail, want) {
				t.Errorf("tail = %v, want %v", tail, want)
			}
		})
	}
}

// withCache makes the Env read XDG_CACHE_HOME from the folder.
func withCache(e langserver.Env, dir string) langserver.Env {
	e.Environ = func(name string) string {
		if name == "XDG_CACHE_HOME" {
			return dir
		}
		return ""
	}
	return e
}
