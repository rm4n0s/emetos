package java

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// Eclipse JDT LS for the Java pack (SPEC §10.34 "Java"). jdtls is no program but a folder: a launcher jar of Equinox, a configuration
// folder for the system and the bundles of the server. Emetos runs java on that jar the way the server's own script (bin/jdtls) and
// vscode-java do, with a workspace of its own in the cache folder, so nothing is written to the folder that is opened or to the folder of
// the server.

// The ids the pack uses.
const (
	// ID is the id of the extension, and ServerID the id of its language server in the manifest.
	ID       = "emetos.java"
	ServerID = "jdtls"
)

// minJava is the oldest Java that runs jdtls.
const minJava = 21

// rootMarkers are the files that mark a Java project: the server is started in the nearest folder that holds one.
var rootMarkers = []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", ".project"}

// afterDirs are the folders searched after PATH for a jdtls folder: where the install command of the pack puts it, and where the
// distributions put it.
var afterDirs = []string{"~/.local/share/jdtls", "/usr/share/java/jdtls", "/usr/share/jdtls", "/usr/lib/jdtls", "/opt/jdtls"}

// serverConfig describes jdtls for extensions/internal/langserver.
func (j *java) serverConfig() *langserver.Config {
	return &langserver.Config{
		ID: ServerID, Name: "Java", Languages: []string{"java"}, Category: "Java",
		Purpose: "diagnostics, completion, hover, go to definition (into the JDK and the libraries), rename, organize imports and code actions for Java, with Maven and Gradle projects",
		Programs: []langserver.Program{{
			Name: "jdtls", PathSetting: PathSetting, Find: findJDTLS, CheckPath: checkJDTLSPath,
			PathDescription: "The folder of Eclipse JDT LS (the folder that has plugins/org.eclipse.equinox.launcher_*.jar and config_linux), or its bin/jdtls. When empty, jdtls is looked for in PATH " +
				"and in ~/.local/share/jdtls, /usr/share/java/jdtls, /usr/share/jdtls, /usr/lib/jdtls and /opt/jdtls. Only the user's settings are read.",
			// jdtls has no --version; the number is in the name of its core jar (customize)
			Install: langserver.Install{
				Docs: "https://github.com/eclipse-jdtls/eclipse.jdt.ls#installation",
				Commands: []langserver.InstallCommand{
					{Family: langserver.FamilyArch, Command: "sudo pacman -S jdtls"},
					{Family: langserver.FamilyAny, Command: "mkdir -p ~/.local/share/jdtls && curl -fsSL https://download.eclipse.org/jdtls/snapshots/jdt-language-server-latest.tar.gz | tar -xz -C ~/.local/share/jdtls"},
				},
			},
		}},
		RootMarkers:      rootMarkers,
		CustomizeContext: j.customize,
		TraceKey:         TraceSetting,
		WatchedFiles:     []string{"**/pom.xml", "**/build.gradle", "**/build.gradle.kts", "**/settings.gradle", "**/settings.gradle.kts", "**/.classpath", "**/.project"},
		SettingsQuery:    "java",
		Menu: []langserver.MenuEntry{
			{Label: "$(sync) Update Project Configuration", Description: "reads the build files again", Run: func() { j.updateProjectConfiguration(nil) }},
			{Label: "$(trash) Clean Java Language Server Workspace", Description: "deletes the index and starts again", Run: j.cleanWorkspace},
		},
	}
}

// ---- finding jdtls ----

// install is a jdtls folder.
type install struct {
	// dir is the folder, launcher the Equinox launcher jar and config the configuration folder of the system.
	dir, launcher, config string
}

// configFolder is the name of the folder of jdtls that holds the configuration for this system.
func configFolder() string {
	if runtime.GOARCH == "arm64" {
		return "config_linux_arm"
	}
	return "config_linux"
}

// checkInstall tells whether the folder is a jdtls folder, and why not when it is not.
func checkInstall(dir string) (install, string) {
	info, err := os.Stat(dir)
	switch {
	case err != nil:
		return install{}, dir + " does not exist"
	case !info.IsDir():
		return install{}, dir + " is not a folder"
	}
	jars, _ := filepath.Glob(filepath.Join(dir, "plugins", "org.eclipse.equinox.launcher_*.jar"))
	if len(jars) == 0 {
		if _, err := os.Stat(filepath.Join(dir, "plugins", "org.eclipse.equinox.launcher.jar")); err == nil {
			jars = []string{filepath.Join(dir, "plugins", "org.eclipse.equinox.launcher.jar")}
		}
	}
	if len(jars) == 0 {
		return install{}, dir + " is not a jdtls folder: it has no plugins/org.eclipse.equinox.launcher_*.jar"
	}
	sort.Strings(jars)
	config := filepath.Join(dir, configFolder())
	if info, err := os.Stat(filepath.Join(config, "config.ini")); err != nil || info.IsDir() {
		return install{}, dir + " is not a jdtls folder: it has no " + configFolder() + "/config.ini"
	}
	return install{dir: dir, launcher: jars[len(jars)-1], config: config}, ""
}

// folderOf is the jdtls folder a path stands for: the folder itself, or, for the program bin/jdtls (a link to it, too), the folder that
// holds its bin folder.
func folderOf(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return path
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	return filepath.Dir(filepath.Dir(real))
}

// findJDTLS is Program.Find: the folder of the path setting, else a jdtls program in PATH (the folder beside its bin folder), else the
// usual folders.
func findJDTLS(env langserver.Env) (langserver.Found, error) {
	if set := env.Settings.String(PathSetting); set != "" {
		path := env.Abs(set)
		in, why := checkInstall(folderOf(path))
		if why != "" {
			return langserver.Found{}, errors.New("ServerPathInvalid", PathSetting+" is set to "+set+": "+why, "setting", PathSetting, "value", set, "path", path)
		}
		return langserver.Found{Path: in.dir, Source: langserver.FromSetting}, nil
	}
	var searched []string
	var notUsable string
	pathDirs := filepath.SplitList(env.Path)
	if program, ok := langserver.FindExecutable("jdtls", pathDirs); ok {
		dir := folderOf(program)
		_, why := checkInstall(dir)
		if why == "" {
			return langserver.Found{Path: dir, Source: langserver.FromPath}, nil
		}
		notUsable = "jdtls was found at " + program + ", but it does not stand beside a jdtls folder (" + why + "); set " + PathSetting + " to the folder"
	}
	searched = append(searched, pathDirs...)
	for _, d := range afterDirs {
		dir := env.Abs(d)
		searched = append(searched, dir)
		if _, why := checkInstall(dir); why == "" {
			return langserver.Found{Path: dir, Source: langserver.FromAfter}, nil
		}
	}
	msg := "jdtls was not found"
	if notUsable != "" {
		msg = notUsable
	}
	return langserver.Found{}, errors.New("ServerNotFound", msg, "program", "jdtls", "file", "jdtls", "searched", strings.Join(searched, string(os.PathListSeparator)))
}

// checkJDTLSPath is Program.CheckPath: what Set Path... says about the text typed.
func checkJDTLSPath(env langserver.Env, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if _, why := checkInstall(folderOf(env.Abs(text))); why != "" {
		return why
	}
	return ""
}

// jdtlsVersion is the version of the server, from the name of its core jar (org.eclipse.jdt.ls.core_1.63.0.202610191322.jar); "" when it
// cannot be read.
func jdtlsVersion(in install) string {
	jars, _ := filepath.Glob(filepath.Join(in.dir, "plugins", "org.eclipse.jdt.ls.core_*.jar"))
	if len(jars) == 0 {
		return ""
	}
	m := regexp.MustCompile(`core_(\d+\.\d+\.\d+)`).FindStringSubmatch(filepath.Base(jars[0]))
	if m == nil {
		return ""
	}
	return m[1]
}

// ---- finding java ----

// javaVersionPattern finds the major version in what `java -version` prints: openjdk version "25.0.4.1" 2026-08-18, "21" 2023-09-19
// or "1.8.0_292" (Java 8).
var javaVersionPattern = regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`)

// parseJavaMajor is VersionCheck.Parse for java -version: the major version number.
func parseJavaMajor(output string) (string, bool) {
	m := javaVersionPattern.FindStringSubmatch(output)
	if m == nil {
		return "", false
	}
	if m[1] == "1" && m[2] != "" {
		return m[2], true // 1.8.0 is Java 8
	}
	return m[1], true
}

// javaProgram is the java that runs the server and its major version.
type javaProgram struct {
	path  string
	major int
	// source says where it came from, for the Output channel: the setting, JAVA_HOME or PATH.
	source string
}

// resolveJava finds the java program (java.jdt.ls.java.home, JAVA_HOME, then PATH) and checks that it is Java 21 or newer. It fails with
// JavaHomeInvalid, JavaNotFound and JavaTooOld, and with ServerVersionFailed or ServerVersionUnreadable when `java -version` says nothing
// that can be read.
func resolveJava(ctx context.Context, env langserver.Env) (javaProgram, error) {
	var prog javaProgram
	if set := env.Settings.String(JavaHomeSetting); set != "" {
		path := filepath.Join(env.Abs(set), "bin", "java")
		if !langserver.IsExecutable(path) {
			return prog, errors.New("JavaHomeInvalid", JavaHomeSetting+" is set to "+set+", which has no bin/java that can be run", "setting", JavaHomeSetting, "value", set, "path", path)
		}
		prog = javaProgram{path: path, source: JavaHomeSetting}
	} else if home := env.Getenv("JAVA_HOME"); home != "" && langserver.IsExecutable(filepath.Join(home, "bin", "java")) {
		prog = javaProgram{path: filepath.Join(home, "bin", "java"), source: "JAVA_HOME"}
	} else if path, ok := langserver.FindExecutable("java", filepath.SplitList(env.Path)); ok {
		prog = javaProgram{path: path, source: "PATH"}
	} else {
		return prog, errors.New("JavaNotFound", "java was not found: jdtls needs Java 21 or newer. Install a JDK, or set "+JavaHomeSetting+" or JAVA_HOME", "path", env.Path)
	}
	version, err := langserver.QueryVersion(ctx, prog.path, langserver.VersionCheck{Args: []string{"-version"}, Parse: parseJavaMajor}, env.Folder, nil)
	if err != nil {
		return prog, err
	}
	prog.major, _ = strconv.Atoi(version)
	if prog.major < minJava {
		return prog, errors.New("JavaTooOld", "Java "+version+" ("+prog.path+") is too old: jdtls needs Java "+strconv.Itoa(minJava)+" or newer. Set "+JavaHomeSetting+" to a newer JDK",
			"java", prog.path, "version", version, "min", minJava)
	}
	return prog, nil
}

// ---- the command line ----

// splitArgs splits the text of java.jdt.ls.vmargs into arguments at spaces; single or double quotes keep a space in an argument, and a
// backslash makes the next character an ordinary one.
func splitArgs(text string) []string {
	var out []string
	var cur strings.Builder
	have := false
	var quote rune
	escaped := false
	for _, r := range text {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, have = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, have = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// hasPrefixArg tells whether an argument starts with the prefix.
func hasPrefixArg(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

// commandLine is the arguments of java for the server (bin/jdtls.py of the server and vscode-java's javaServerStarter.ts do the same).
func commandLine(in install, java javaProgram, vmargs []string, configuration, data string) []string {
	args := []string{
		"-Declipse.application=org.eclipse.jdt.ls.core.id1",
		"-Dosgi.bundles.defaultStartLevel=4",
		"-Declipse.product=org.eclipse.jdt.ls.core.product",
		"-DDetectVMInstallationsJob.disabled=true",
		"-Dfile.encoding=UTF-8",
	}
	if java.major >= 24 {
		args = append(args, "-Djdk.xml.maxGeneralEntitySizeLimit=0", "-Djdk.xml.totalEntitySizeLimit=0")
	}
	args = append(args,
		"--add-modules=ALL-SYSTEM",
		"--add-opens", "java.base/java.util=ALL-UNNAMED",
		"--add-opens", "java.base/java.lang=ALL-UNNAMED",
		"--add-opens", "java.base/sun.nio.fs=ALL-UNNAMED",
	)
	args = append(args, vmargs...)
	// the JVM writes its own log lines to stdout, which is the channel of the protocol
	if !hasPrefixArg(vmargs, "-Xlog:") {
		args = append(args, "-Xlog:disable")
	}
	return append(args, "-jar", in.launcher, "-configuration", configuration, "-data", data)
}
