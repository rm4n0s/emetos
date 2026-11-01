package java

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The folders jdtls works in and the start of the server (SPEC §10.34 "Java"): the workspace (-data) and the configuration it writes
// (-configuration) are in Emetos's cache folder, one pair for each project root, so the folder that is opened and the folder of the
// server stay as they were.

// workspacePaths are the folders of one project root in the cache.
type workspacePaths struct {
	// dir holds the other two; Clean Java Language Server Workspace deletes it.
	dir string
	// data is the workspace of the server (-data) and config its writable configuration (-configuration).
	data, config string
}

// cacheRoot is the folder under which the pack keeps its workspaces: Emetos's cache folder ($XDG_CACHE_HOME/emetos, else
// ~/.cache/emetos), then jdtls.
func cacheRoot(env langserver.Env) string {
	base := env.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(env.Home, ".cache")
	}
	return filepath.Join(base, "emetos", "jdtls")
}

// workspaceFor names the folders of a project root: its name and a short hash of its path, so that two folders of one name do not share
// a workspace.
func workspaceFor(env langserver.Env, root string) workspacePaths {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	name := filepath.Base(root)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "root"
	}
	dir := filepath.Join(cacheRoot(env), strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, name)+"-"+hex.EncodeToString(sum[:])[:12])
	return workspacePaths{dir: dir, data: filepath.Join(dir, "workspace"), config: filepath.Join(dir, "config")}
}

// prepareConfiguration copies the configuration of the system (config.ini) of the jdtls folder into the writable configuration folder,
// when it is not there or differs: the server writes its state next to it, and the folder of the server may be read-only.
func prepareConfiguration(in install, w workspacePaths) error {
	want, err := os.ReadFile(filepath.Join(in.config, "config.ini"))
	if err != nil {
		return errors.NewErr("JavaConfigurationReadFailed", err).SetMetadata("path", in.config)
	}
	target := filepath.Join(w.config, "config.ini")
	if have, err := os.ReadFile(target); err == nil && bytes.Equal(have, want) {
		return nil
	}
	if err := os.MkdirAll(w.config, 0o700); err != nil {
		return errors.NewErr("JavaConfigurationFolderFailed", err).SetMetadata("path", w.config)
	}
	if err := os.MkdirAll(w.data, 0o700); err != nil {
		return errors.NewErr("JavaWorkspaceFolderFailed", err).SetMetadata("path", w.data)
	}
	if err := os.WriteFile(target, want, 0o600); err != nil {
		return errors.NewErr("JavaConfigurationWriteFailed", err).SetMetadata("path", target)
	}
	return nil
}

// fileURI is the file: URI of a path.
func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// extendedCapabilities are the capabilities of the client that jdtls asks for in initializationOptions: only what Emetos does. The
// server shows its progress as language/progressReport, asks its questions as language/actionableNotification, and its class files are
// documents (java/classFileContents).
var extendedCapabilities = map[string]any{
	"classFileContentsSupport":        true,
	"progressReportProvider":          true,
	"actionableNotificationSupported": true,
}

// customize is Config.CustomizeContext: it finds java, prepares the folders of the workspace and builds the command line and the
// options of the server from the settings. It runs on the launcher's goroutine.
func (j *java) customize(ctx context.Context, l *langserver.Launch, env langserver.Env) error {
	in, why := checkInstall(l.Found.Path)
	if why != "" {
		return errors.New("ServerPathInvalid", why, "path", l.Found.Path)
	}
	l.Version = jdtlsVersion(in)
	prog, err := resolveJava(ctx, env)
	if err != nil {
		return err
	}
	s := env.Settings
	w := workspaceFor(env, l.Root)
	if j.cleanNext.Swap(false) {
		if err := removeWorkspace(env, w); err != nil {
			return err
		}
	}
	if err := prepareConfiguration(in, w); err != nil {
		return err
	}
	vmargs := defaultVMArgs
	if v, ok := s.Value(VMArgsSetting); ok {
		if str, isString := v.(string); isString {
			vmargs = str
		}
	}
	l.Command = prog.path
	l.Args = commandLine(in, prog, splitArgs(vmargs), w.config, w.data)
	options := map[string]any{
		"bundles":                    []any{},
		"workspaceFolders":           []string{fileURI(l.Root)},
		"settings":                   map[string]any{"java": javaSettings(s)},
		"extendedClientCapabilities": extendedCapabilities,
	}
	j.mu.Lock()
	j.workspace = w
	trigger := j.trigger
	j.mu.Unlock()
	// the Java file in front, when it is in the root: for a folder with no build file it tells the server where the sources start
	if path, ok := strings.CutPrefix(trigger, "file://"); ok && strings.HasPrefix(path, strings.TrimSuffix(l.Root, "/")+"/") {
		options["triggerFiles"] = []string{trigger}
	}
	l.InitializationOptions = options
	j.api.Log().Debug("jdtls is started", "java", prog.path, "javaSource", prog.source, "jdtls", in.dir, "data", w.data)
	return nil
}

// removeWorkspace deletes the folders of a project root, and only those: the folder must be directly below the cache folder of the pack.
func removeWorkspace(env langserver.Env, w workspacePaths) error {
	if w.dir == "" || filepath.Dir(w.dir) != cacheRoot(env) {
		return errors.New("JavaWorkspaceUnsafe", "the workspace folder is not in the cache folder of the pack, and is not deleted", "path", w.dir)
	}
	if err := os.RemoveAll(w.dir); err != nil {
		return errors.NewErr("JavaWorkspaceRemoveFailed", err).SetMetadata("path", w.dir)
	}
	return nil
}
