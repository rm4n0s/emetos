package httpclient

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/checks"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
	"github.com/rm4n0s/emetos/extensions/internal/cachehome"
)

// place is what a request gets from where it lives in the tree: the variables of its environment, folders and collection, the headers
// and the authorization they pass down.
type place struct {
	envKey     string
	env        *variables.Scope
	folders    []variables.Scope
	collection *variables.Scope
	headers    []format.Pair
	// auth is the authorization the folders and the collection give a request that inherits (nil: none given, type "none" included
	// as a value); a request with its own authorization ignores it.
	auth *format.Auth
}

// placeOf collects the scopes of the request file at path (empty: a request that is not saved, which has only the environment).
func (e *Extension) placeOf(path string) place { return e.placeIn(path, e.activeEnvironment()) }

// placeIn is placeOf with the environment env (nil: none) in place of the active one: the runner has an environment of its own.
func (e *Extension) placeIn(path string, env *store.Node) place {
	var p place
	if env != nil && env.Environment != nil {
		p.envKey = env.Key
		p.env = &variables.Scope{Owner: variables.EnvironmentOwner(env.Key), Name: env.Environment.Name, Variables: env.Environment.Variables}
	}
	if e.tree == nil || path == "" {
		return p
	}
	n := e.tree.Find(path)
	if n == nil {
		return p
	}
	var chain []*store.Node // from the innermost group out to the collection
	for g := e.tree.Parent(n); g != nil; g = e.tree.Parent(g) {
		chain = append(chain, g)
	}
	for i := len(chain) - 1; i >= 0; i-- { // the outermost first
		g := chain[i]
		if g.Group == nil {
			continue
		}
		if g.Kind == store.KindCollection {
			p.collection = &variables.Scope{Owner: variables.CollectionOwner(g.Key), Name: g.Name, Variables: g.Group.Variables}
		} else {
			p.folders = append(p.folders, variables.Scope{Name: g.Name, Variables: g.Group.Variables})
		}
		p.headers = append(p.headers, g.Group.Headers...)
	}
	for _, g := range chain { // the innermost first: the nearest authorization wins
		if g.Group != nil && g.Group.Auth != nil && g.Group.Auth.Type != "" && g.Group.Auth.Type != format.AuthInherit {
			a := *g.Group.Auth
			p.auth = &a
			break
		}
	}
	return p
}

// engineConfig is the engine's configuration from the settings of the editor.
func (e *Extension) engineConfig() engine.Config {
	st := e.api.Settings()
	cfg := engine.Config{Jars: e.jars, UserAgent: "Emetos/dev"}
	if n, err := extension.SettingValue[int](st, SettingTimeoutMs); err == nil {
		cfg.TimeoutMs = n
	}
	if v, err := extension.SettingValue[bool](st, SettingVerifyTLS); err == nil {
		cfg.VerifyTLS = &v
	}
	if s, err := extension.SettingValue[string](st, SettingProxy); err == nil {
		cfg.Proxy = s
	}
	if n, err := extension.SettingValue[int](st, SettingMaxResponseMiB); err == nil && n > 0 {
		cfg.MemoryLimit = int64(n) << 20
	}
	if certs, err := extension.SettingValue[[]map[string]any](st, SettingClientCerts); err == nil {
		for _, c := range certs {
			host, _ := c["host"].(string)
			cert, _ := c["certificate"].(string)
			key, _ := c["key"].(string)
			if host != "" && cert != "" && key != "" {
				cfg.ClientCerts = append(cfg.ClientCerts, engine.ClientCert{Host: host, CertFile: cert, KeyFile: key})
			}
		}
	}
	if dir, ok := cachehome.Dir(); ok {
		cfg.CacheDir = filepath.Join(dir, "emetos", "httpclient")
	}
	return cfg
}

// allowedProcessEnv is the setting httpClient.allowedProcessEnv.
func (e *Extension) allowedProcessEnv() []string {
	names, err := extension.SettingValue[[]string](e.api.Settings(), SettingAllowedProcessEnv)
	if err != nil {
		return nil
	}
	return names
}

// sendJob is a request the tab hands to the engine.
type sendJob struct {
	files store.RequestFiles
	place place
	// locals are variables of this send that win over all others (the columns of a row of the runner's data file); nil for none.
	locals map[string]string
}

// sendOutcome is how a send ended.
type sendOutcome struct {
	resp *engine.Response
	// disp is what the response pane shows of it, made off the UI thread.
	disp *respDisplay
	// extracted and checkResults are what the rules and the checks of the request made of the response.
	extracted    []extractOutcome
	checkResults []checks.Result
	err          error
	duration     time.Duration
}

// runSend builds the request from the form (the text of its body as it is in the boxes, not as it is on disk) and sends it with the engine.
// It does I/O (the keyring, the globals, the network) and runs off the UI thread; ctx cancels it.
func (e *Extension) runSend(ctx context.Context, job sendJob) sendOutcome {
	start := time.Now()
	req, texts := bodyTexts(job.files)
	resolver := e.resolverFor(ctx, job.place, job.locals)
	eng := engine.New(e.engineConfig())
	defer eng.Close()
	input := engine.Input{
		Request: req, Headers: job.place.headers, Resolver: resolver, Folder: e.folder, Texts: texts, Environment: job.place.envKey,
	}
	if req.Auth == nil || req.Auth.Type == "" || req.Auth.Type == format.AuthInherit {
		input.Auth = job.place.auth
		if input.Auth == nil {
			input.Auth = &format.Auth{Type: format.AuthNone}
		}
	}
	if auth := firstAuth(input.Auth, req.Auth); auth != nil && auth.Type == format.AuthOAuth2 {
		tok, terr := e.oauthTokenFor(ctx, auth, job.place, resolver)
		if terr != nil {
			return sendOutcome{err: terr, duration: time.Since(start)}
		}
		input.OAuth = &engine.OAuthToken{Value: tok.AccessToken, Scheme: tok.Scheme()}
	}
	resp, err := eng.Send(ctx, input)
	out := sendOutcome{resp: resp, err: err, duration: time.Since(start)}
	if resp != nil {
		out.disp = buildDisplay(resp)
		out.extracted = e.extractAndStore(ctx, job.place, resp, req.Extract)
		out.checkResults = checks.Run(resp, req.Checks)
	}
	return out
}

// sentOf is what the history keeps of a request that was sent.
func sentOf(job sendJob, out sendOutcome, now time.Time) history.Sent {
	s := history.Sent{Time: now, Request: job.files.Request, Body: job.files.Body, Variables: job.files.Variables, Duration: out.duration}
	if out.resp != nil {
		s.Status = out.resp.StatusCode
		s.StatusText = strings.TrimSpace(strings.TrimPrefix(out.resp.Status, strconv.Itoa(out.resp.StatusCode)))
		if out.resp.Body != nil {
			s.Size = out.resp.Body.Size()
		}
	}
	if out.err != nil {
		s.Err = out.err.Error()
	}
	return s
}

// durationText is a time in milliseconds, or in seconds from one second on.
func durationText(d time.Duration) string {
	if d >= time.Second {
		return strconv.FormatFloat(d.Seconds(), 'f', 2, 64) + " s"
	}
	return strconv.FormatInt(d.Milliseconds(), 10) + " ms"
}

// bodyTexts gives the request the names of its body files (a name is made when it has none) and the texts the engine uses for them in
// place of reading files: what is in the boxes of the form.
func bodyTexts(files store.RequestFiles) (format.Request, map[string]string) {
	req := files.Request
	if req.Body == nil || format.BodyExtension(req.Body.Type) == "" {
		return req, nil
	}
	b := *req.Body
	req.Body = &b
	if b.File == "" {
		b.File = format.BodyFileName("request", b.Type)
	}
	texts := map[string]string{b.File: files.Body}
	if b.Type == format.BodyGraphQL {
		if b.Variables == "" {
			b.Variables = format.VariablesFileName("request")
		}
		texts[b.Variables] = files.Variables
	}
	req.Body = &b
	return req, texts
}

// resolverFor reads the global variables and the secrets of the keyring and makes the resolver for a request at place, with locals winning
// over every other variable. A part that cannot be read is logged and left out: the request then says which variables have no value.
// It does I/O and runs off the UI thread.
func (e *Extension) resolverFor(ctx context.Context, p place, locals map[string]string) *variables.Resolver {
	in := variables.Input{
		Locals: locals, Environment: p.env, Folders: p.folders, Collection: p.collection, Values: e.values,
		Dynamic: variables.Dynamic{AllowedProcessEnv: e.allowedProcessEnv()},
	}
	if path, perr := variables.GlobalsPath(); perr == nil {
		entries, _, lerr := variables.NewGlobals(path, e.opts.Keyring).Load(ctx)
		if lerr != nil {
			e.api.Log().Warn("the global variables cannot be read", "err", lerr)
		}
		in.Globals = entries
	}
	resolver, rerr := variables.Build(ctx, in)
	if rerr != nil {
		e.api.Log().Warn("some secret variables cannot be read from the keyring", "err", rerr)
	}
	return resolver
}
