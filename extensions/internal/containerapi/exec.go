package containerapi

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/rm4n0s/errors"
)

// DefaultMaxExecOutput is the most bytes of output, both streams together, that Exec collects.
const DefaultMaxExecOutput = 4 << 20

// ExecOptions say what Exec runs.
type ExecOptions struct {
	// Cmd is the command and its arguments.
	Cmd []string
	// Env is added to the container's environment, as "NAME=value".
	Env []string
	// WorkingDir and User override the container's.
	WorkingDir, User string
	// MaxOutput is the most bytes of output collected; 0 is DefaultMaxExecOutput. More output is dropped and Truncated is set.
	MaxOutput int
	// Wait is how long to wait for the exit status after the output has ended; 0 is five seconds.
	Wait time.Duration
}

// ExecResult is what Exec collected.
type ExecResult struct {
	// Stdout and Stderr are the output.
	Stdout, Stderr string
	// ExitCode is the exit status; -1 when the command had not ended (Running).
	ExitCode int
	// Running reports a command that was still running when Exec stopped waiting for it.
	Running bool
	// Truncated reports that output beyond MaxOutput was dropped.
	Truncated bool
}

// Exec runs a command in a running container without a terminal and collects its output (create, start, read the
// multiplexed stream, inspect for the exit status). It does not feed the command's input.
func (c *Client) Exec(ctx context.Context, id string, opts ExecOptions) (ExecResult, error) {
	create := map[string]any{"AttachStdout": true, "AttachStderr": true, "Tty": false, "Cmd": opts.Cmd}
	if len(opts.Env) > 0 {
		create["Env"] = opts.Env
	}
	if opts.WorkingDir != "" {
		create["WorkingDir"] = opts.WorkingDir
	}
	if opts.User != "" {
		create["User"] = opts.User
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/exec", body: create}, &created); err != nil {
		return ExecResult{}, err
	}
	max := opts.MaxOutput
	if max <= 0 {
		max = DefaultMaxExecOutput
	}
	var stdout, stderr bytes.Buffer
	var res ExecResult
	streamCtx, stop := context.WithCancel(ctx)
	defer stop()
	resp, err := c.do(streamCtx, request{method: http.MethodPost, path: "/exec/" + url.PathEscape(created.ID) + "/start", body: map[string]any{"Detach": false, "Tty": false}})
	if err != nil {
		return ExecResult{}, err
	}
	err = Demux(resp.Body, func(f Frame) error {
		room := max - stdout.Len() - stderr.Len()
		data := f.Data
		if len(data) > room {
			data = data[:room]
			res.Truncated = true
		}
		if f.Stream == StreamStderr {
			stderr.Write(data)
		} else {
			stdout.Write(data)
		}
		if res.Truncated {
			stop() // no more output is wanted: closing the stream ends the read
		}
		return nil
	})
	_ = resp.Body.Close()
	if err != nil && !res.Truncated {
		return ExecResult{}, err
	}
	res.Stdout, res.Stderr = stdout.String(), stderr.String()
	wait := opts.Wait
	if wait <= 0 {
		wait = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		var st struct {
			Running  bool `json:"Running"`
			ExitCode int  `json:"ExitCode"`
		}
		if err := c.call(ctx, request{method: http.MethodGet, path: "/exec/" + url.PathEscape(created.ID) + "/json"}, &st); err != nil {
			if ctx.Err() != nil {
				// the wait was cancelled while it asked the engine, which is a cancelled wait, not a failed request
				return ExecResult{}, errors.NewErr("ContainerAPIExecCancelled", ctx.Err()).SetMetadata("id", id)
			}
			return ExecResult{}, err
		}
		if !st.Running {
			res.ExitCode = st.ExitCode
			return res, nil
		}
		if time.Now().After(deadline) {
			res.ExitCode, res.Running = -1, true
			return res, nil
		}
		select {
		case <-ctx.Done():
			return ExecResult{}, errors.NewErr("ContainerAPIExecCancelled", ctx.Err()).SetMetadata("id", id)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
