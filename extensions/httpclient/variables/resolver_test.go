package variables

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

var bg = context.Background()

func vars(pairs ...string) []format.Variable {
	var out []format.Variable
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, format.Variable{Name: pairs[i], Value: pairs[i+1], Enabled: true})
	}
	return out
}

// every layer defines "x" with its own value, so the winner shows the order.
func fullInput(t *testing.T) Input {
	t.Helper()
	state := newState()
	vals := NewValues("/w", state, &dbconn.MemoryKeyring{})
	if err := vals.Set(bg, EnvironmentOwner("dev"), "x", "env-current", false); err != nil {
		t.Fatal(err)
	}
	if err := vals.Set(bg, CollectionOwner("api"), "x", "coll-current", false); err != nil {
		t.Fatal(err)
	}
	return Input{
		Locals:      map[string]string{"x": "local"},
		Environment: &Scope{Owner: EnvironmentOwner("dev"), Name: "Dev", Variables: vars("x", "env-initial")},
		Folders: []Scope{
			{Name: "Outer", Variables: vars("x", "outer-folder")},
			{Name: "Inner", Variables: vars("x", "inner-folder")},
		},
		Collection: &Scope{Owner: CollectionOwner("api"), Name: "API", Variables: vars("x", "coll-initial")},
		Globals:    []Entry{{Name: "x", Value: "global", Enabled: true}},
		Values:     vals,
	}
}

func TestTheOrderOfResolution(t *testing.T) {
	tests := []struct {
		name   string
		strip  func(in *Input)
		want   string
		source Source
	}{
		{"everything defined: the local value", func(*Input) {}, "local", SourceLocal},
		{"no local: the current value of the environment", func(in *Input) { in.Locals = nil }, "env-current", SourceEnvironmentCurrent},
		{"no current value: the initial one", func(in *Input) {
			in.Locals = nil
			in.Values = NewValues("/w", newState(), nil)
		}, "env-initial", SourceEnvironmentInitial},
		{"no environment: the innermost folder", func(in *Input) { in.Locals, in.Environment = nil, nil }, "inner-folder", SourceFolder},
		{"no inner folder: the outer one", func(in *Input) { in.Locals, in.Environment, in.Folders = nil, nil, in.Folders[:1] }, "outer-folder", SourceFolder},
		{"no folders: the current value of the collection", func(in *Input) { in.Locals, in.Environment, in.Folders = nil, nil, nil }, "coll-current", SourceCollectionCurrent},
		{"no current value: the collection's initial", func(in *Input) {
			in.Locals, in.Environment, in.Folders = nil, nil, nil
			in.Values = nil
		}, "coll-initial", SourceCollectionInitial},
		{"no collection: the global", func(in *Input) { in.Locals, in.Environment, in.Folders, in.Collection = nil, nil, nil, nil }, "global", SourceGlobal},
		{"a disabled global is not there", func(in *Input) {
			in.Locals, in.Environment, in.Folders, in.Collection = nil, nil, nil, nil
			in.Globals[0].Enabled = false
		}, "{{x}}", ""},
		{"a disabled environment variable lets the next one win", func(in *Input) {
			in.Locals = nil
			in.Environment.Variables[0].Enabled = false
			in.Values = nil
		}, "inner-folder", SourceFolder},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := fullInput(t)
			tt.strip(&in)
			r, err := Build(bg, in)
			if err != nil {
				t.Fatal(err)
			}
			if got := r.Resolve("{{x}}").Text; got != tt.want {
				t.Errorf("Resolve = %q, want %q", got, tt.want)
			}
			v, src, ok := r.Lookup("x")
			if tt.source == "" {
				if ok {
					t.Errorf("Lookup found %q", v)
				}
				return
			}
			if !ok || v != tt.want || src != tt.source {
				t.Errorf("Lookup = %q, %q, %v; want %q from %q", v, src, ok, tt.want, tt.source)
			}
		})
	}
}

func TestResolvingText(t *testing.T) {
	in := Input{
		Locals: map[string]string{"id": "7"},
		Collection: &Scope{Variables: vars(
			"base", "http://{{host}}:{{port}}", "host", "localhost", "port", "8080",
			"a", "{{b}}", "b", "{{a}}", "self", "{{self}}", "deep", "{{deep2}}", "deep2", "end",
			"empty", "")},
	}
	r, err := Build(bg, in)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, in, want string
		unresolved     []string
	}{
		{"nothing to do", "plain text", "plain text", nil},
		{"one", "/users/{{id}}", "/users/7", nil},
		{"spaces inside", "/users/{{  id }}", "/users/7", nil},
		{"nested values", "{{base}}/x", "http://localhost:8080/x", nil},
		{"many", "{{id}}-{{id}}-{{host}}", "7-7-localhost", nil},
		{"not found stays", "a {{nope}} b", "a {{nope}} b", []string{"nope"}},
		{"not found once", "{{nope}}{{nope}}", "{{nope}}{{nope}}", []string{"nope"}},
		{"cycle of two", "{{a}}", "{{a}}", []string{"a"}},
		{"cycle of one", "{{self}}", "{{self}}", []string{"self"}},
		{"deep", "{{deep}}", "end", nil},
		{"empty value", "[{{empty}}]", "[]", nil},
		{"empty name", "{{}}", "{{}}", []string{""}},
		{"unclosed", "{{id", "{{id", nil},
		{"closed first", "}}{{id}}", "}}7", nil},
		{"single braces", "{id}", "{id}", nil},
		{"json body", `{"id": {{id}}, "o": {"k": "{{host}}"}}`, `{"id": 7, "o": {"k": "localhost"}}`, nil},
		{"unknown dynamic", "{{$nope}}", "{{$nope}}", []string{"$nope"}},
		{"unicode", "é{{id}}é", "é7é", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Resolve(tt.in)
			if got.Text != tt.want || !reflect.DeepEqual(got.Unresolved, tt.unresolved) {
				t.Errorf("Resolve(%q) = %q %v, want %q %v", tt.in, got.Text, got.Unresolved, tt.want, tt.unresolved)
			}
		})
	}
}

func TestDynamicVariables(t *testing.T) {
	d := Dynamic{Now: fixedNow, Rand: &seq{}, Getenv: func(n string) string { return map[string]string{"HOME": "/h", "TOKEN": "t0k"}[n] }, AllowedProcessEnv: []string{"HOME", "UNSET"}}
	r, err := Build(bg, Input{Dynamic: d})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in, want   string
		unresolved []string
	}{
		{"{{$timestamp}}", "1793701230", nil},
		{"{{$isoTimestamp}}", "2026-11-03T10:20:30.456Z", nil},
		{"{{$guid}}", "01020304-0506-4708-890a-0b0c0d0e0f10", nil},
		{"{{$randomUUID}}", "11121314-1516-4718-991a-1b1c1d1e1f20", nil},
		{"{{$randomInt}}", "", nil}, // checked below
		{"{{$processEnv HOME}}", "/h", nil},
		{"{{ $processEnv   HOME }}", "/h", nil},
		{"{{$processEnv UNSET}}", "", nil},
		{"{{$processEnv TOKEN}}", "{{$processEnv TOKEN}}", []string{"$processEnv TOKEN"}},
		{"{{$processEnv}}", "{{$processEnv}}", []string{"$processEnv"}},
		{"{{$unknown}}", "{{$unknown}}", []string{"$unknown"}},
	}
	for _, tt := range tests {
		got := r.Resolve(tt.in)
		if tt.in == "{{$randomInt}}" {
			continue
		}
		if got.Text != tt.want || !reflect.DeepEqual(got.Unresolved, tt.unresolved) {
			t.Errorf("Resolve(%q) = %q %v, want %q %v", tt.in, got.Text, got.Unresolved, tt.want, tt.unresolved)
		}
	}
	// a real source: a version 4 UUID each time, an integer from 0 to 1000
	real, _ := Build(bg, Input{})
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		g := real.Resolve("{{$guid}}").Text
		if !uuid.MatchString(g) || seen[g] {
			t.Fatalf("guid %q is not a new UUID v4", g)
		}
		seen[g] = true
		n := real.Resolve("{{$randomInt}}").Text
		if v := atoi(t, n); v < 0 || v > 1000 {
			t.Fatalf("randomInt = %s", n)
		}
	}
	if got := real.Resolve("{{$timestamp}}").Text; len(got) != 10 {
		t.Errorf("timestamp = %s", got)
	}
	if got := real.Resolve("{{$isoTimestamp}}").Text; !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`).MatchString(got) {
		t.Errorf("isoTimestamp = %s", got)
	}
	// with nothing allowed the process environment cannot be read
	t.Setenv("EMETOS_HTTP_TEST_ENV", "secret")
	if got := real.Resolve("{{$processEnv EMETOS_HTTP_TEST_ENV}}"); got.Text == "secret" {
		t.Error("a name that is not allowed was read")
	}
	allowed, _ := Build(bg, Input{Dynamic: Dynamic{AllowedProcessEnv: []string{"EMETOS_HTTP_TEST_ENV"}}})
	if got := allowed.Resolve("{{$processEnv EMETOS_HTTP_TEST_ENV}}").Text; got != "secret" {
		t.Errorf("an allowed name = %q", got)
	}
	// a random source that fails leaves the variable unresolved
	broken, _ := Build(bg, Input{Dynamic: Dynamic{Rand: strings.NewReader("")}})
	for _, in := range []string{"{{$guid}}", "{{$randomInt}}"} {
		if got := broken.Resolve(in); got.Text != in {
			t.Errorf("Resolve(%q) with no random bytes = %q", in, got.Text)
		}
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("%q is not a number", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func TestSecretsAreInTheKeyringAndNeverInAFile(t *testing.T) {
	keyring := &dbconn.MemoryKeyring{}
	state := newState()
	vals := NewValues("/work/space", state, keyring)
	owner := EnvironmentOwner("prod")
	if err := vals.Set(bg, owner, "token", "S3CR3T-VALUE", true); err != nil {
		t.Fatal(err)
	}
	if err := vals.Set(bg, owner, "plain", "visible", false); err != nil {
		t.Fatal(err)
	}
	if keyring.Len() != 1 || keyring.Label(SecretKey("/work/space", owner, "token")) == "" {
		t.Errorf("keyring has %d secrets", keyring.Len())
	}
	for k, v := range state.data {
		if strings.Contains(string(v), "S3CR3T") {
			t.Errorf("the state %q holds the secret: %s", k, v)
		}
	}
	// a file written for the environment has no value for the secret
	env := format.Environment{Name: "Prod", Variables: []format.Variable{{Name: "token", Value: "S3CR3T-VALUE", Enabled: true, Secret: true}, {Name: "plain", Value: "visible", Enabled: true}}}
	text, err := format.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "prod.jsonc")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "S3CR3T") || !strings.Contains(text, `"secret": true`) {
		t.Errorf("the file = %s", text)
	}
	// persisting the current values writes the plain one and refuses the secret
	in := Input{Environment: &Scope{Owner: owner, Name: "Prod", Variables: env.Variables}, Values: vals}
	r, err := Build(bg, in)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Resolve("{{token}}/{{plain}}"); got.Text != "S3CR3T-VALUE/visible" {
		t.Errorf("Resolve = %+v", got)
	}
	if _, err := Persist(env.Variables, vals.All(owner)); err != nil {
		t.Errorf("Persist of the plain current value: %v", err)
	}
	_, err = Persist(env.Variables, map[string]string{"token": "x"})
	wantRoute(t, err, "Persist.HttpPersistSecret")
	// the secret of another workspace or environment is another one
	other := NewValues("/other", newState(), keyring)
	if _, ok, _ := other.Secret(bg, owner, "token"); ok {
		t.Error("another workspace sees the secret")
	}
	if _, ok, _ := vals.Secret(bg, EnvironmentOwner("dev"), "token"); ok {
		t.Error("another environment sees the secret")
	}
	// the secret goes with the variable
	if err := vals.DeleteSecrets(bg, owner, "token", "never-existed"); err != nil || keyring.Len() != 0 {
		t.Errorf("DeleteSecrets: %v, %d left", err, keyring.Len())
	}
}

func TestAMissingSecretIsReported(t *testing.T) {
	vals := NewValues("/w", newState(), &dbconn.MemoryKeyring{})
	owner := CollectionOwner("api")
	in := Input{Collection: &Scope{Owner: owner, Variables: []format.Variable{{Name: "key", Enabled: true, Secret: true}, {Name: "ok", Value: "1", Enabled: true}}}, Values: vals}
	r, err := Build(bg, in)
	if err != nil {
		t.Fatal(err)
	}
	got := r.Resolve("{{key}} {{ok}}")
	if got.Text != "{{key}} 1" || !reflect.DeepEqual(got.MissingSecrets, []string{"key"}) || len(got.Unresolved) != 0 {
		t.Errorf("Resolve = %+v", got)
	}
	if _, _, ok := r.Lookup("key"); ok {
		t.Error("Lookup found a secret that is not there")
	}
	// without a keyring at all
	r, err = Build(bg, Input{Collection: in.Collection, Values: NewValues("/w", newState(), nil)})
	if err != nil || len(r.Resolve("{{key}}").MissingSecrets) != 1 {
		t.Errorf("no keyring: %v", err)
	}
	// a keyring that fails: the error comes back and the rest still resolves
	failing := &dbconn.MemoryKeyring{Err: errors.New("KeyringDown", "no service")}
	r, err = Build(bg, Input{Collection: in.Collection, Values: NewValues("/w", newState(), failing)})
	if err == nil || r.Resolve("{{ok}}").Text != "1" {
		t.Errorf("failing keyring: %v", err)
	}
	// ... and the same when the keyring is asked
	if err := vals.Set(bg, owner, "k", "v", true); err != nil {
		t.Fatal(err)
	}
	if err := NewValues("/w", newState(), failing).Set(bg, owner, "k", "v", true); err == nil {
		t.Error("Set into a failing keyring succeeded")
	}
	// no keyring for a secret
	err = NewValues("/w", newState(), nil).Set(bg, owner, "k", "v", true)
	wantRoute(t, err, "Values.Set.HttpSecretsUnavailable")
	if err := NewValues("/w", newState(), nil).Reset(bg, owner, "k", true); err != nil {
		t.Errorf("Reset of a secret with no keyring: %v", err)
	}
	if err := NewValues("/w", newState(), nil).DeleteSecrets(bg, owner, "k"); err != nil {
		t.Error(err)
	}
}

func TestCurrentValues(t *testing.T) {
	state := newState()
	vals := NewValues("/w", state, &dbconn.MemoryKeyring{})
	dev, prod := EnvironmentOwner("dev"), EnvironmentOwner("prod")
	for _, set := range []struct{ owner, name, value string }{{dev, "a", "1"}, {dev, "b", "2"}, {prod, "a", "9"}} {
		if err := vals.Set(bg, set.owner, set.name, set.value, false); err != nil {
			t.Fatal(err)
		}
	}
	if v, ok := vals.Get(dev, "a"); !ok || v != "1" {
		t.Errorf("Get = %q, %v", v, ok)
	}
	if !reflect.DeepEqual(vals.All(dev), map[string]string{"a": "1", "b": "2"}) {
		t.Errorf("All = %v", vals.All(dev))
	}
	// they are in the state of the workspace and survive a new Values over it
	again := NewValues("/w", state, nil)
	if v, _ := again.Get(prod, "a"); v != "9" {
		t.Errorf("after reopening = %q", v)
	}
	if err := vals.Reset(bg, dev, "a", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := vals.Get(dev, "a"); ok {
		t.Error("Reset left the value")
	}
	if err := vals.Reset(bg, dev, "never", false); err != nil {
		t.Error(err)
	}
	if err := vals.ResetOwner(dev); err != nil {
		t.Fatal(err)
	}
	if len(vals.All(dev)) != 0 || len(vals.All(prod)) != 1 {
		t.Errorf("ResetOwner: %v %v", vals.All(dev), vals.All(prod))
	}
	if err := vals.ResetOwner(dev); err != nil {
		t.Error(err)
	}
	// a state that holds nonsense is no values; a state that fails is the error
	state.data[StateKey] = []byte(`"nonsense"`)
	if len(vals.All(prod)) != 0 {
		t.Error("nonsense in the state gave values")
	}
	state.err = errors.New("StateBroken", "no")
	if err := vals.Set(bg, prod, "x", "y", false); err == nil {
		t.Error("Set into a failing state succeeded")
	}
	// persisting
	out, err := Persist(vars("a", "old", "b", "keep"), map[string]string{"a": "new", "zz": "unused"})
	if err != nil || out[0].Value != "new" || out[1].Value != "keep" {
		t.Errorf("Persist = %v, %v", out, err)
	}
}

func TestGlobals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", GlobalsFileName)
	keyring := &dbconn.MemoryKeyring{}
	g := NewGlobals(path, keyring)
	if e, m, err := g.Load(bg); err != nil || e != nil || m != nil {
		t.Fatalf("Load of nothing = %v %v %v", e, m, err)
	}
	entries := []Entry{{Name: "host", Value: "h", Enabled: true}, {Name: "key", Value: "TOP-SECRET", Enabled: true, Secret: true}, {Name: "off", Value: "o"}}
	if err := g.Save(bg, entries); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "TOP-SECRET") {
		t.Errorf("the file has the secret: %s", raw)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	got, missing, err := NewGlobals(path, keyring).Load(bg)
	if err != nil || len(missing) != 0 || !reflect.DeepEqual(got, entries) {
		t.Errorf("Load = %v %v %v", got, missing, err)
	}
	// they resolve below everything else
	r, _ := Build(bg, Input{Globals: got})
	if r.Resolve("{{host}} {{key}} {{off}}").Text != "h TOP-SECRET {{off}}" {
		t.Errorf("Resolve = %q", r.Resolve("{{host}} {{key}} {{off}}").Text)
	}
	// a secret that left the globals leaves the keyring
	if err := g.Save(bg, entries[:1]); err != nil || keyring.Len() != 0 {
		t.Errorf("after removing the secret: %v, %d in the keyring", err, keyring.Len())
	}
	// a secret that the keyring lost is reported; with no keyring at all, too
	if err := g.Save(bg, entries); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Delete(bg, SecretKey("", GlobalOwner, "key")); err != nil {
		t.Fatal(err)
	}
	if _, missing, err := NewGlobals(path, keyring).Load(bg); err != nil || !reflect.DeepEqual(missing, []string{"key"}) {
		t.Errorf("missing = %v, %v", missing, err)
	}
	if _, missing, err := NewGlobals(path, nil).Load(bg); err != nil || len(missing) != 1 {
		t.Errorf("no keyring: %v %v", missing, err)
	}
	failing := &dbconn.MemoryKeyring{Err: errors.New("KeyringDown", "no service")}
	if _, _, err := NewGlobals(path, failing).Load(bg); err == nil {
		t.Error("Load with a failing keyring succeeded")
	}
	if err := NewGlobals(path, failing).Save(bg, entries); err == nil {
		t.Error("Save with a failing keyring succeeded")
	}
}

func TestGlobalsFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, GlobalsFileName)
	tests := []struct {
		name  string
		setup func(t *testing.T)
		do    func() error
		route string
	}{
		{"not JSON", func(t *testing.T) { writeFile(t, path, "{", 0o600) }, func() error { _, _, err := NewGlobals(path, nil).Load(bg); return err }, "Globals.Load.GlobalsFileInvalid"},
		{"another version", func(t *testing.T) { writeFile(t, path, `{"version": 7}`, 0o600) }, func() error { _, _, err := NewGlobals(path, nil).Load(bg); return err }, "Globals.Load.GlobalsFileVersion"},
		{"writable by others", func(t *testing.T) { writeFile(t, path, `{"version": 1}`, 0o666) }, func() error { _, _, err := NewGlobals(path, nil).Load(bg); return err }, "StateFileUnsafeMode"},
		{"a secret with no keyring", func(t *testing.T) {}, func() error {
			return NewGlobals(path, nil).Save(bg, []Entry{{Name: "k", Secret: true}})
		}, "Globals.Save.HttpSecretsUnavailable"},
		{"cannot encode", func(t *testing.T) {}, func() error {
			g := NewGlobals(path, nil)
			g.encode = func(any) ([]byte, error) { return nil, errors.New("Boom", "boom") }
			return g.Save(bg, nil)
		}, "Globals.Save.GlobalsEncodeFailed"},
		{"cannot write", func(t *testing.T) {}, func() error {
			writeFile(t, path, "a file", 0o600)
			return NewGlobals(filepath.Join(path, "x", GlobalsFileName), nil).Save(bg, nil)
		}, "StateFileWriteFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Remove(path)
			tt.setup(t)
			wantRoute(t, tt.do(), tt.route)
		})
	}
}

func writeFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalsPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := GlobalsPath()
	if err != nil || filepath.Base(p) != GlobalsFileName || !strings.HasSuffix(filepath.Dir(p), "emetos") {
		t.Errorf("GlobalsPath = %q, %v", p, err)
	}
}

func TestOwnersAndKeysKeepPlacesApart(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range []string{
		SecretKey("/a", EnvironmentOwner("x"), "n"), SecretKey("/b", EnvironmentOwner("x"), "n"),
		SecretKey("/a", EnvironmentOwner("y"), "n"), SecretKey("/a", CollectionOwner("x"), "n"),
		SecretKey("/a", EnvironmentOwner("x"), "m"), SecretKey("", GlobalOwner, "n"),
	} {
		if keys[k] {
			t.Errorf("key %q is used twice", k)
		}
		keys[k] = true
	}
}
