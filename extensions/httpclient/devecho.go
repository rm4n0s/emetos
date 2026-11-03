package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// maxEchoBody is the most of a request body the dev echo server reads.
const maxEchoBody = 1 << 20

// cmdDevEcho starts the echo server on 127.0.0.1, sets the variable baseUrl of the active environment to its address, and tells the
// user the address. A second call starts another server.
func (e *Extension) cmdDevEcho(_ context.Context, args any) error {
	if err := e.ctx.Err(); err != nil {
		return errors.NewErr("HttpDevEchoListenFailed", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(e.ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return errors.NewErr("HttpDevEchoListenFailed", err)
	}
	srv := &http.Server{Handler: echoHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			e.api.Log().Warn("the echo server stopped", "err", serr)
		}
	}()
	go func() {
		<-e.ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if serr := srv.Shutdown(sctx); serr != nil {
			e.api.Log().Warn("the echo server did not stop in time", "err", serr)
		}
	}()
	addr := "http://" + ln.Addr().String()
	name := argString(args, "variable")
	if name == "" {
		name = "baseUrl"
	}
	env := e.activeEnvironment()
	if env == nil {
		e.api.Notifications().Info("Echo server on " + addr + ". Choose an environment to have {{" + name + "}} point at it.")
		return nil
	}
	if err := e.values.Set(e.ctx, variables.EnvironmentOwner(env.Key), name, addr, false); err != nil {
		return err
	}
	e.api.Notifications().Info("Echo server on " + addr + ": {{" + name + "}} of the environment " + env.Name + " points at it.")
	return nil
}

// echoHandler answers a request with a JSON description of it. The path /status/<n> answers with that status.
func echoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxEchoBody))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		headers := map[string]string{}
		names := make([]string, 0, len(r.Header))
		for k := range r.Header {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			headers[k] = strings.Join(r.Header[k], ", ")
		}
		out := map[string]any{"method": r.Method, "path": r.URL.Path, "query": r.URL.Query(), "headers": headers}
		if len(body) > 0 {
			var parsed any
			if json.Unmarshal(body, &parsed) == nil {
				out["json"] = parsed
			} else {
				out["body"] = string(body)
			}
		}
		status := http.StatusOK
		if rest, ok := strings.CutPrefix(r.URL.Path, "/status/"); ok {
			if n, err := strconv.Atoi(rest); err == nil && n >= 200 && n < 600 {
				status = n
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return
		}
	})
}
