package gitscm

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/githttp"
)

// CommandDevTestServer is the dev command that makes the proof of the password prompt (SPEC §15.2): it serves a copy of the repository on
// 127.0.0.1 behind Basic authentication and makes it the remote "origin" of the repository, so that a fetch asks for the password. It
// exists in debug builds and in builds with the emetos_dev tag only.
const CommandDevTestServer = "dev.git.testserver"

// registerDev registers the dev commands of the extension in the builds that run dev commands.
func (x *gitscm) registerDev() {
	if !foundation.DebugMode && !devBuild {
		return
	}
	spec := extension.CommandSpec{
		ID: CommandDevTestServer,
		Description: `Serve a copy of the first open repository on 127.0.0.1 with a user name and a password and make it the remote origin. Arguments: ` +
			`{"user": "ada", "password": "secret", "root": "<repository>"}.`,
	}
	if err := x.api.Commands().Register(spec, x.cmdDevTestServer); err != nil {
		x.log.Debug("the dev command of the Git extension is not registered", "err", err)
	}
}

// cmdDevTestServer starts the test server and sets the remote. It runs on the UI thread and returns when the remote is set, so that the
// next step of a script finds it.
func (x *gitscm) cmdDevTestServer(ctx context.Context, args any) error {
	user, password, root := "ada", "secret", ""
	if m, ok := args.(map[string]any); ok {
		if v, ok := m["user"].(string); ok && v != "" {
			user = v
		}
		if v, ok := m["password"].(string); ok && v != "" {
			password = v
		}
		root, _ = m["root"].(string)
	}
	var target *repository
	for _, r := range x.model.order {
		if r.repo != nil && (root == "" || r.root == root) {
			target = r
			break
		}
	}
	if target == nil || x.runner == nil {
		return errors.New("DevGitTestServerNoRepository", "no open repository to serve", "root", root)
	}
	dir, err := os.MkdirTemp("", "emetos-gittest-")
	if err != nil {
		return errors.NewErr("DevGitTestServerFailed", err)
	}
	bare := filepath.Join(dir, "small.git")
	steps := [][]string{
		{"clone", "--bare", "-q", target.root, bare},
		{"config", "--file", filepath.Join(bare, "config"), "http.receivepack", "true"},
	}
	for _, args := range steps {
		if _, err := x.runner.Run(ctx, git.Command{Dir: dir, Args: args, Background: true}); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
	}
	srv, err := githttp.Start(githttp.Options{
		GitPath: x.runner.Path(), Root: dir, User: user, Password: password,
		Env: []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1"},
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	go func() {
		<-x.ctx.Done()
		srv.Close()
		_ = os.RemoveAll(dir)
	}()
	url := "http://" + user + "@" + srv.URL[len("http://"):] + "/small.git"
	set := git.Command{Dir: target.root, Args: []string{"remote", "set-url", "origin", url}, Background: true}
	if _, err := x.runner.Run(ctx, set); err != nil {
		set.Args = []string{"remote", "add", "origin", url}
		if _, err := x.runner.Run(ctx, set); err != nil {
			return err
		}
	}
	x.logLine("dev.git.testserver: " + git.MaskURLs(srv.URL) + " asks for the password of " + user + "; origin of " + target.root + " is the copy of the repository")
	return nil
}
