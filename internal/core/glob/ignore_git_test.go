//go:build emetos_git

package glob

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

// notGit lists the cases git cannot run: two ignore files of one folder (.gitignore and
// .ignore) are one file for git.
var notGit = map[string]bool{"later file of the same folder overrides": true}

// TestIgnoreAgainstGit asks a real git about every query of TestIgnore and requires the
// same answer. It needs git, so it carries the emetos_git build tag.
func TestIgnoreAgainstGit(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	for _, tc := range ignoreCases {
		t.Run(tc.name, func(t *testing.T) {
			if notGit[tc.name] {
				t.Skip("git cannot run this case")
			}
			for _, q := range tc.queries {
				root := t.TempDir()
				git := func(args ...string) error {
					cmd := exec.Command(gitPath, append([]string{"-C", root}, args...)...)
					cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "HOME="+root)
					return cmd.Run()
				}
				if err := git("init", "-q"); err != nil {
					t.Fatal(err)
				}
				if tc.opts.IgnoreCase {
					if err := git("config", "core.ignorecase", "true"); err != nil {
						t.Fatal(err)
					}
				}
				for _, file := range tc.files {
					dir := filepath.Join(root, file.dir)
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(file.text), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				target := filepath.Join(root, filepath.FromSlash(q.path))
				if q.dir {
					err = os.MkdirAll(target, 0o755)
				} else if err = os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
					err = os.WriteFile(target, nil, 0o644)
				}
				if err != nil {
					t.Fatal(err)
				}
				gitErr := git("check-ignore", "-q", "--no-index", "--", q.path)
				var exit *exec.ExitError
				gitIgnored := gitErr == nil
				if gitErr != nil && (!errors.As(gitErr, &exit) || exit.ExitCode() != 1) {
					t.Fatalf("git check-ignore %q: %v", q.path, gitErr)
				}
				if gitIgnored != q.want {
					t.Errorf("git says ignored(%q, dir=%v) = %v, the test table says %v", q.path, q.dir, gitIgnored, q.want)
				}
			}
		})
	}
}
