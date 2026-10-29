package dbconn

import (
	"testing"

	"github.com/rm4n0s/errors"
)

func envOf(m map[string]string) LookupEnv {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestPasswordsResolveInOrder(t *testing.T) {
	kr := &MemoryKeyring{}
	if err := kr.Store(t.Context(), "c1", "Emetos: c", "from-keyring"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		c        Connection
		memory   string
		env      map[string]string
		keyring  Keyring
		want     Resolved
		wantTags string
	}{
		{"nothing", Connection{ID: "c1"}, "", nil, kr, Resolved{}, ""},
		{"the keyring when the connection saves", Connection{ID: "c1", SavePassword: true}, "", nil, kr, Resolved{"from-keyring", SourceKeyring}, ""},
		{"not the keyring when it does not save", Connection{ID: "c1"}, "", nil, kr, Resolved{}, ""},
		{"memory before the keyring", Connection{ID: "c1", SavePassword: true}, "typed", nil, kr, Resolved{"typed", SourceMemory}, ""},
		{"the environment before all", Connection{ID: "c1", SavePassword: true, PasswordEnv: "${env:PW}"}, "typed", map[string]string{"PW": "from-env"}, kr, Resolved{"from-env", SourceEnv}, ""},
		{"an empty variable is an empty password", Connection{ID: "c1", PasswordEnv: "${env:PW}"}, "", map[string]string{"PW": ""}, kr, Resolved{"", SourceEnv}, ""},
		{"a variable that is not set", Connection{ID: "c1", PasswordEnv: "${env:PW}"}, "", nil, kr, Resolved{}, "Passwords.Resolve->ExpandEnv.ConnectionEnvUnset"},
		{"no keyring", Connection{ID: "c1", SavePassword: true}, "", nil, nil, Resolved{}, ""},
		{"a keyring that fails", Connection{ID: "c1", SavePassword: true}, "", nil, &MemoryKeyring{Err: errors.New("KeyringFake", "down")}, Resolved{}, "KeyringFake"},
		{"a secret that is not stored", Connection{ID: "other", SavePassword: true}, "", nil, kr, Resolved{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPasswords(tt.keyring, envOf(tt.env))
			if tt.memory != "" {
				p.Remember(tt.c.ID, tt.memory)
			}
			got, err := p.Resolve(t.Context(), tt.c)
			if tt.wantTags != "" {
				wantRoute(t, err, tt.wantTags)
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Resolve = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestPasswordsSaveAndForget(t *testing.T) {
	kr := &MemoryKeyring{}
	p := NewPasswords(kr, envOf(nil))
	c := Connection{ID: "c1", Name: "Prod"}

	// not saved: in memory only, until forgotten
	if got := p.Save(t.Context(), c, "typed"); got.In != SourceMemory || got.KeyringErr != nil {
		t.Fatalf("Save = %+v", got)
	}
	if !p.Remembered("c1") || kr.Len() != 0 {
		t.Error("the password is not in memory only")
	}
	p.Forget("c1")
	if p.Remembered("c1") {
		t.Error("Forget left it")
	}

	// saved: in the keyring, and not in memory
	c.SavePassword = true
	if got := p.Save(t.Context(), c, "kept"); got.In != SourceKeyring {
		t.Fatalf("Save = %+v", got)
	}
	if p.Remembered("c1") || kr.Len() != 1 || kr.Label("c1") != "Emetos: Prod" {
		t.Errorf("remembered %v, keyring holds %d, label %q", p.Remembered("c1"), kr.Len(), kr.Label("c1"))
	}

	// a keyring that fails: the password is held in memory and the caller is told
	bad := NewPasswords(&MemoryKeyring{Err: errors.New("KeyringFake", "locked")}, envOf(nil))
	got := bad.Save(t.Context(), c, "held")
	if got.In != SourceMemory || got.KeyringErr == nil || !bad.Remembered("c1") {
		t.Fatalf("Save = %+v, remembered %v", got, bad.Remembered("c1"))
	}
	res, err := bad.Resolve(t.Context(), c)
	if err != nil || res.Password != "held" || res.Source != SourceMemory {
		t.Errorf("Resolve = %+v, %v", res, err)
	}

	// no keyring at all
	none := NewPasswords(nil, envOf(nil))
	got = none.Save(t.Context(), c, "held")
	wantRoute(t, got.KeyringErr, "Passwords.Save.KeyringMissing")
	if got.In != SourceMemory || !none.Remembered("c1") {
		t.Errorf("Save without a keyring = %+v", got)
	}
	if err := none.Delete(t.Context(), "c1"); err != nil || none.Remembered("c1") {
		t.Errorf("Delete = %v, remembered %v", err, none.Remembered("c1"))
	}

	// Delete clears both
	p.Remember("c1", "again")
	if err := p.Delete(t.Context(), "c1"); err != nil || p.Remembered("c1") || kr.Len() != 0 {
		t.Errorf("Delete = %v, remembered %v, keyring %d", err, p.Remembered("c1"), kr.Len())
	}
	// a keyring that fails still clears the memory and reports
	bad.Remember("c1", "x")
	if err := bad.Delete(t.Context(), "c1"); err == nil || bad.Remembered("c1") {
		t.Errorf("Delete = %v, remembered %v", err, bad.Remembered("c1"))
	}
}

func TestSourceNames(t *testing.T) {
	for s, want := range map[Source]string{SourceNone: "none", SourceEnv: "environment", SourceMemory: "memory", SourceKeyring: "keyring"} {
		if s.String() != want {
			t.Errorf("%d.String() = %q", s, s.String())
		}
	}
}
