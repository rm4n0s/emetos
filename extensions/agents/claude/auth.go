package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// AuthInfo is what `claude auth status` says. LoggedIn comes from the exit status of the program (0 when logged in, 1 when
// not); the other fields come from its JSON output and are empty when the output is not JSON. The adapter reads no file of
// the program's configuration and no token: this is all it knows about the login.
type AuthInfo struct {
	// LoggedIn is true when the program exited with status 0.
	LoggedIn bool
	// Method is "claude.ai", "oauth_token", "api_key", "api_key_helper", "third_party" or "none".
	Method string
	// Provider is the API provider the program reports ("firstParty" and others).
	Provider string
	// Email, Organization and Subscription describe the account, when the program says.
	Email, Organization, Subscription string
}

// authJSON is the JSON that `claude auth status` prints.
type authJSON struct {
	AuthMethod       string `json:"authMethod"`
	APIProvider      string `json:"apiProvider"`
	Email            string `json:"email"`
	OrgName          string `json:"orgName"`
	SubscriptionType string `json:"subscriptionType"`
}

// maxAuthOutput bounds what is read from `claude auth status`.
const maxAuthOutput = 1 << 20

// AuthStatus runs `claude auth status` and tells whether the user is logged in. A program that exits with status 1 is not an
// error: it is AuthInfo{LoggedIn: false}. It fails with the tags ProgramNotFound (see Find), ProcessStartFailed, and
// AuthStatusFailed (any other exit status, a program killed by a signal, or the time limit of 20 seconds or ctx ending).
// The program runs in Dir, in a process group of its own that is killed when it ends, with the environment of Emetos
// unchanged.
func (a *Adapter) AuthStatus(ctx context.Context) (AuthInfo, error) {
	path, err := a.Find()
	if err != nil {
		return AuthInfo{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, orDefault(a.authTimeout, defaultAuthTimeout))
	defer cancel()
	cmd := exec.Command(path, "auth", "status")
	cmd.Dir = a.Dir
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out limitedBuffer
	out.limit = maxAuthOutput
	cmd.Stdout = &out
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return AuthInfo{}, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path)
	}
	pgid := cmd.Process.Pid
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		case <-done:
		}
	}()
	werr := cmd.Wait()
	close(done)
	_ = syscall.Kill(-pgid, syscall.SIGKILL) // whatever it left behind
	if ctx.Err() != nil {
		return AuthInfo{}, errors.NewErr("AuthStatusFailed", ctx.Err()).SetMetadata("program", path)
	}
	code := cmd.ProcessState.ExitCode()
	if werr != nil && code != 1 {
		return AuthInfo{}, errors.NewErr("AuthStatusFailed", werr).SetMetadata("program", path, "exit", code)
	}
	info := AuthInfo{LoggedIn: code == 0}
	var j authJSON
	if json.Unmarshal(bytes.TrimSpace(out.buf.Bytes()), &j) == nil {
		info.Method, info.Provider = j.AuthMethod, j.APIProvider
		info.Email, info.Organization, info.Subscription = j.Email, j.OrgName, j.SubscriptionType
	}
	return info, nil
}

// limitedBuffer keeps the first limit bytes written to it and drops the rest.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
