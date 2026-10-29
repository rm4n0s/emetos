//go:build emetos_display

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The real-input test of the Redis console (SPEC §10.23, step 21.2): a real Emetos in the compositor, connected to a throwaway
// Redis in a container of the user's Docker (CLAUDE.md "Testing notes": named emetos-test-<random>, labelled dev.emetos.test=1,
// published on 127.0.0.1 only, a random password that reaches Emetos through the environment variable the connection names, removed
// when the test ends). The test is skipped when Docker does not answer.

const redisProofImage = "redis:8.2.2"

// startRedisContainer starts the server and returns its name, its address and its password.
func startRedisContainer(t *testing.T) (name, addr, password string) {
	t.Helper()
	if err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Run(); err != nil {
		t.Skip("the Docker engine does not answer: ", err)
	}
	random := func(n int) string {
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(b)
	}
	name, password = "emetos-test-"+random(6), random(12)
	out, err := exec.Command("docker", "run", "-d", "--name", name, "--label", "dev.emetos.test=1", "-p", "127.0.0.1::6379",
		redisProofImage, "redis-server", "--requirepass", password).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput(); err != nil {
			t.Errorf("docker rm -f %s: %v\n%s", name, err, out)
		}
	})
	out, err = exec.Command("docker", "port", name, "6379/tcp").CombinedOutput()
	if err != nil {
		t.Fatalf("docker port: %v\n%s", err, out)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, "127.0.0.1:") {
			addr = line
		}
	}
	if addr == "" {
		t.Fatalf("no address on 127.0.0.1 in %q", out)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := redisCLI(name, password, "", "PING"); err == nil {
			return name, addr, password
		}
		if time.Now().After(deadline) {
			t.Fatal("the server did not start")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// redisCLI runs redis-cli in the container with the password in the environment (never on a command line) and the commands given.
func redisCLI(name, password, stdin string, args ...string) (string, error) {
	cmd := exec.Command("docker", append([]string{"exec", "-i", "-e", "REDISCLI_AUTH=" + password, name, "redis-cli"}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func waitTextContaining(t *testing.T, a *display.App, sub string) {
	t.Helper()
	a.Eventually("a text with "+sub, func() (bool, string) {
		_, ok := textContaining(a, sub)
		return ok, a.Dump()
	})
}

func TestDesktopRedisConsoleAndKeyEditor(t *testing.T) {
	name, addr, password := startRedisContainer(t)
	seed := "HSET user:42:profile name Ada mail ada@example.com role admin\n" +
		"SET greeting \"hello world\"\nRPUSH queue:jobs build test deploy\nSADD user:42:tags go redis ui\nSET user:7:name Bob\n" +
		"ZADD scores 7.5 carol 9.25 alice\nSET config:app '{\"debug\":true}' EX 3600\n"
	if out, err := redisCLI(name, password, seed); err != nil {
		t.Fatalf("seeding: %v\n%s", err, out)
	}
	host, port, _ := strings.Cut(addr, ":")
	portN, _ := strconv.Atoi(port)
	const connID = "proofredis00000000000000"
	file, err := json.Marshal(map[string]any{"version": 1, "connections": []map[string]any{{
		"id": connID, "name": "cache", "engine": "redis", "scope": "user", "host": host, "port": portN,
		"passwordEnv": "${env:EMETOS_TEST_REDIS_PASSWORD}", "options": map[string]string{"separator": ":"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	a := display.Start(t, display.Options{
		Args:    []string{".", "session.redis"},
		Project: map[string]string{"session.redis": "# Commands of a .redis file: Ctrl+Enter runs the line of the cursor\nPING\nHGETALL user:42:profile\nSMEMBERS user:42:tags\nLRANGE queue:jobs 0 -1\n"},
		State:   map[string]string{"db-connections.json": string(file)},
		Env:     []string{"EMETOS_TEST_REDIS_PASSWORD=" + password},
	})

	// connect in the Databases view and walk the keys
	a.Dev("workbench.view.extension.emetos.databases.container", nil)
	a.WaitText("cache")
	a.Dev("emetos.databases.connect", connID)
	a.Eventually("the connection to be open", func() (bool, string) {
		_, ok := a.Text("db0")
		if !ok {
			a.ClickText("cache")
			a.Press("right")
		}
		_, ok = a.Text("db0")
		return ok, a.Dump()
	})
	a.ClickText("db0")
	a.Press("right")
	a.WaitText("user")
	a.WaitText("greeting")

	// a hash in the key editor
	a.Dev("emetos.redis.openKey", map[string]any{"connection": connID, "db": 0, "key": "user:42:profile"})
	waitTextContaining(t, a, "ada@example.com")

	// the commands of a .redis file, run with Ctrl+Enter from the editor
	a.ClickText("session.redis")
	a.Dev("workbench.action.focusActiveEditorGroup", nil)
	a.Press("ctrl+home", "down", "down") // the line of HGETALL
	a.Press("ctrl+enter")
	waitTextContaining(t, a, `"name" => "Ada"`)
	a.Dev("workbench.action.focusActiveEditorGroup", nil)
	a.Press("down")
	a.Press("ctrl+enter")
	waitTextContaining(t, a, `1~ "go"`)

	// a command typed in the console, with the quoting of redis-cli
	in := a.Inputs()
	bottom := in[0]
	for _, i := range in {
		if i.Y > bottom.Y {
			bottom = i
		}
	}
	a.ClickInput(bottom)
	a.Type(`SET note "two words" EX 60`)
	a.Press("enter")
	waitTextContaining(t, a, "OK")
	if out, err := redisCLI(name, password, "", "GET", "note"); err != nil || strings.TrimSpace(out) != "two words" {
		t.Errorf("the server has %q (%v)", out, err)
	}

	// the key editor again, beside the explorer and the console
	a.ClickText("profile")
	waitTextContaining(t, a, "ada@example.com")
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "21.2-redis-console.png"))
}
