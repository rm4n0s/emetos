//go:build emetos_display

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extensions/relay"
	"github.com/rm4n0s/emetos/extensions/remotework"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Real-input tests of hosting a workspace from one place (SPEC §10.29, step 17e.4): two Emetos processes in one compositor, the host
// starting from a fresh state, and everything the user does with the Sharing status item, the guide, the key box and the menu done
// with the keys and the pointer of the compositor. The hosts listen on 127.0.0.1 only (the dev bind variables of the two extensions,
// read by debug builds only), and the keys are made by the test: the user's own are never read.

// hostingInterfaces is the list of network interfaces the host shows in its guide (the dev knob of the interfaces): addresses of the
// documentation ranges of RFC 5737 and a Docker bridge, so that no screenshot of the proofs holds an address of the machine the
// test ran on. The guide lists the default route's address first and leaves the bridge and the interface that is down out.
const hostingInterfaces = `[{"name":"lo","up":true,"loopback":true,"addrs":["127.0.0.1/8"]},` +
	`{"name":"enp3s0","up":true,"defaultRoute":true,"addrs":["192.0.2.10/24"]},` +
	`{"name":"wlan0","up":true,"addrs":["198.51.100.7/24"]},` +
	`{"name":"docker0","up":true,"addrs":["172.17.0.5/16"]},` +
	`{"name":"virbr0","up":true,"addrs":["192.168.122.5/24"]}]`

// hostingPair is a host that has never hosted, and a client that has the host keys of both extensions pinned and a key of its own.
type hostingPair struct {
	host, client *display.App
	rwPort       int
	psPort       int
	keyLine      string // the public key of the client, as an authorized_keys line
	keyFile      string // its private key
	stateDir     string // the state folder of the host
}

// startHostingPair starts the host on a project with a fresh state: no key listed, nothing hosted, and the two host keys made already
// (a first connection then compares nothing: the client has them pinned, as the tests of 15.x and 16.x do).
func startHostingPair(t *testing.T) *hostingPair {
	t.Helper()
	p := &hostingPair{rwPort: freeLoopbackPort(t), psPort: freeLoopbackPort(t)}
	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshClient, err := ssh.NewPublicKey(clientPub)
	if err != nil {
		t.Fatal(err)
	}
	p.keyLine = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshClient))) + " alice@laptop"
	p.keyFile = filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(p.keyFile, []byte(privateKeyText(t, clientPriv, "alice@laptop")), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	hostKey := func() (ssh.PublicKey, string) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			t.Fatal(err)
		}
		return sshPub, privateKeyText(t, priv, "emetos host key")
	}
	rwKey, rwPEM := hostKey()
	psKey, psPEM := hostKey()
	pinned := func(port int, key ssh.PublicKey) string {
		return jsonText(t, map[string]any{"version": 1, "hosts": map[string]any{
			net.JoinHostPort("127.0.0.1", fmt.Sprint(port)): map[string]string{
				"type": key.Type(), "fingerprint": ssh.FingerprintSHA256(key),
				"key": base64.StdEncoding.EncodeToString(key.Marshal()), "firstSeen": now,
			},
		}})
	}
	p.host = display.Start(t, display.Options{
		Args: []string{"."}, Project: map[string]string{"app.go": "package main\n"}, Config: terminalConfig,
		Env: []string{remotework.DevBindEnv + "=127.0.0.1", relay.DevBindEnv + "=127.0.0.1", remotework.DevInterfacesEnv + "=" + hostingInterfaces},
		State: map[string]string{
			"remote-work-host-key": rwPEM, "relay-host-key": psPEM,
			"remote-work.json": jsonText(t, map[string]any{"version": 1, "port": p.rwPort, "idleMinutes": 30, "workspaces": []string{}, "keys": []any{}}),
			"relay.json":    jsonText(t, map[string]any{"version": 1, "listening": false, "port": p.psPort, "workspaces": []string{}, "keys": []any{}}),
		},
	})
	p.stateDir = filepath.Join(filepath.Dir(p.host.Project), "state", "emetos")
	p.client = display.Start(t, display.Options{
		Args: []string{"."}, Project: map[string]string{"local.go": "package main\n"}, Config: terminalConfig,
		Env:   []string{remotework.DevInterfacesEnv + "=" + hostingInterfaces}, // the client's own Be a Host section shows addresses too
		State: map[string]string{"remote-work-hosts.json": pinned(p.rwPort, rwKey), "relay-hosts.json": pinned(p.psPort, psKey)},
	})
	return p
}

// statusLeft is the leftmost text of the status bar (its bottom 30 pixels) that is not the Problems counter's: the Sharing item sits
// before the Problems item.
func statusLeft(a *display.App) (app.DevText, bool) {
	var best app.DevText
	found := false
	for _, tx := range a.Texts() {
		if tx.Y < 770 || tx.X > 400 {
			continue
		}
		if !found || tx.X < best.X {
			best, found = tx, true
		}
	}
	return best, found
}

// itemText is what the Sharing item says now: the texts of the left end of the status bar up to the Problems item (the icon is a text
// of its own, and the word "Hosting" another).
func itemText(t *testing.T, a *display.App) string {
	t.Helper()
	var parts []app.DevText
	for _, tx := range a.Texts() {
		if tx.Y >= 770 && tx.X < 125 {
			parts = append(parts, tx)
		}
	}
	if len(parts) == 0 {
		t.Fatalf("no status bar on screen: %s", a.Dump())
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].X < parts[j].X })
	var texts []string
	for _, tx := range parts {
		texts = append(texts, tx.Text)
	}
	return strings.Join(texts, " ")
}

// waitContaining waits until a text on screen has part in it.
func waitContaining(a *display.App, part string) app.DevText {
	var found app.DevText
	a.Eventually("a text with "+fmt.Sprintf("%q", part)+" on screen", func() (bool, string) {
		tx, ok := textContaining(a, part)
		found = tx
		return ok, a.Dump()
	})
	return found
}

// clickContaining clicks the first text on screen that has part in it.
func clickContaining(t *testing.T, a *display.App, part string) {
	t.Helper()
	tx, ok := textContaining(a, part)
	if !ok {
		t.Fatalf("no text with %q on screen: %s", part, a.Dump())
	}
	a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
}

// openMenu clicks the Sharing item of the host and waits for its menu.
func openMenu(t *testing.T, a *display.App) {
	t.Helper()
	a.Focus()
	tx, ok := statusLeft(a)
	if !ok {
		t.Fatalf("no Sharing item: %s", a.Dump())
	}
	a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
	waitContaining(a, "Settings...")
}

// hostingShot saves a screenshot of the window for the proofs.
func hostingShot(t *testing.T, a *display.App, name string) {
	t.Helper()
	a.Focus() // two windows share the screen: the one to look at is brought to the front
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), name))
}

// readState reads a JSON state file of the host into a map.
func (p *hostingPair) readState(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(p.stateDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// listensOn tells whether something accepts connections on the loopback port.
func listensOn(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// addKeyWithTheKeyboard pastes the key of the client in the box of the page or the tab that is open and presses Add.
func (p *hostingPair) addKeyWithTheKeyboard(t *testing.T) {
	t.Helper()
	// the box may be below the fold of the Relay tab: the wheel brings it up, as it does for a user
	const placeholder = "ssh-ed25519 AAAA... comment"
	for range 12 {
		tx, ok := p.host.Text(placeholder)
		if ok && tx.Y < 700 {
			break
		}
		p.host.Scroll(800, 400, 0, 3) // the page of Remote Work
		p.host.Scroll(300, 400, 0, 3) // the left column of the Relay tab
		p.host.Settle()
	}
	p.host.ClickText(placeholder)
	p.host.Type(p.keyLine)
	p.host.ClickText("Add")
	waitContaining(p.host, "Added alice@laptop")
}

func TestRealInputHostWithRemoteWorkFromTheStatusItem(t *testing.T) {
	p := startHostingPair(t)
	host, client := p.host, p.client
	host.Focus()
	host.Settle()

	// a workspace that nobody hosts shows the icon alone
	if got := itemText(t, host); strings.Contains(got, "Hosting") {
		t.Fatalf("the item of a workspace nobody hosts says %q", got)
	}
	openMenu(t, host)
	for _, want := range []string{"Host with Remote Work", "Host with Relay", "Remote Work Settings...", "Relay Settings...",
		"Allowed computers see this window and control it", "Allowed computers watch this workspace's events and, if you allow it, send commands"} {
		if _, ok := textContaining(host, want); !ok {
			t.Errorf("the menu does not show %q: %s", want, host.Dump())
		}
	}
	if _, ok := textContaining(host, "Stop Hosting"); ok {
		t.Error("the menu of a workspace nobody hosts offers Stop Hosting")
	}
	hostingShot(t, host, "17e.4-menu-not-hosted.png")

	// step 1: Host with no key opens the guide instead of a switch that does nothing
	clickContaining(t, host, "Host with Remote Work")
	waitContaining(host, "How to host this workspace")
	for _, n := range []string{"1", "2", "3"} {
		if _, ok := host.Text(n); !ok {
			t.Errorf("the number %s of the guide is not shown before any step is done", n)
		}
	}
	waitContaining(host, "On the other computer, connect to:")
	hostingShot(t, host, "17e.4-guide.png")

	// the user pastes the key of the other computer: step 1 gets its check mark
	p.addKeyWithTheKeyboard(t)
	host.Eventually("the check mark of step 1", func() (bool, string) { _, ok := host.Text("1"); return !ok, host.Dump() })
	if _, ok := host.Text("2"); !ok {
		t.Error("step 2 is marked done before hosting is on")
	}
	if got := itemText(t, host); strings.Contains(got, "Hosting") {
		t.Errorf("the item says %q before hosting is on", got)
	}

	// step 2: the switch turns hosting on, and the item says so; the plain line names the address
	host.ClickText("Host this workspace")
	waitContaining(host, "On. Computers with an allowed key can connect to")
	host.Eventually("the item to say Hosting", func() (bool, string) { got := itemText(t, host); return strings.Contains(got, "Hosting"), got })
	if got := itemText(t, host); strings.Contains(got, "·") {
		t.Errorf("the item counts computers with nobody connected: %q", got)
	}
	if _, ok := host.Text("Stop Hosting"); !ok {
		t.Error("the page has no Stop Hosting button while it is on")
	}
	if cfg := p.readState(t, "remote-work.json"); len(cfg["workspaces"].([]any)) != 1 {
		t.Errorf("remote-work.json = %v", cfg)
	}
	if !listensOn(p.rwPort) {
		t.Error("nothing listens on the port of Remote Work")
	}
	waitContaining(host, "This workspace is shared with Remote Work (nobody connected)")

	// step 3: the other computer connects with its key
	client.Focus()
	client.Dev("dev.openWindowTab", map[string]any{"kind": remotework.TabKindID})
	client.Settle()
	client.Dev(remotework.DevConnectCommand, map[string]any{"host": "127.0.0.1", "port": p.rwPort, "keyFile": p.keyFile})
	host.Focus()
	host.Eventually("the item to count the computer", func() (bool, string) {
		got := itemText(t, host)
		return strings.Contains(got, "Hosting · 1"), got
	})
	waitContaining(host, "This workspace is shared with Remote Work (1 computer connected)")

	// the menu while hosted: stop, disconnect, settings
	openMenu(t, host)
	for _, want := range []string{"Stop Hosting with Remote Work", "Disconnect alice@laptop", "Host with Relay", "Remote Work Settings..."} {
		if _, ok := textContaining(host, want); !ok {
			t.Errorf("the menu while hosted does not show %q: %s", want, host.Dump())
		}
	}
	hostingShot(t, host, "17e.4-hosting.png")

	// Stop Hosting from the item ends the session of the client with the message
	clickContaining(t, host, "Stop Hosting with Remote Work")
	host.Eventually("the item to be the icon alone", func() (bool, string) {
		got := itemText(t, host)
		return !strings.Contains(got, "Hosting"), got
	})
	host.WaitNoText("This workspace is shared with Remote Work (nobody connected)")
	client.WaitText("The host stopped sharing this workspace.")
	hostingShot(t, client, "17e.4-client-stopped.png")
	if cfg := p.readState(t, "remote-work.json"); len(cfg["workspaces"].([]any)) != 0 {
		t.Errorf("remote-work.json still lists the workspace: %v", cfg)
	}
	host.Eventually("the listener to close", func() (bool, string) { return !listensOn(p.rwPort), "still listening" })
	// the key stays listed: hosting again needs one click
	if cfg := p.readState(t, "remote-work.json"); len(cfg["keys"].([]any)) != 1 {
		t.Errorf("the key was dropped: %v", cfg)
	}
}

func TestRealInputHostWithRelayFromTheStatusItem(t *testing.T) {
	p := startHostingPair(t)
	host, client := p.host, p.client
	host.Focus()
	host.Settle()

	// Host with Relay, with no key listed, opens the Relay tab at its guide
	openMenu(t, host)
	clickContaining(t, host, "Host with Relay")
	waitContaining(host, "How to host a workspace")
	waitContaining(host, "Host the shared workspaces")
	hostingShot(t, host, "17e.4-relay-guide.png")
	p.addKeyWithTheKeyboard(t)
	host.Eventually("the check mark of step 1", func() (bool, string) { _, ok := host.Text("1"); return !ok, host.Dump() })

	// back in the workspace, the same entry now turns hosting on: it shares the workspace and listens
	clickTextWhere(t, host, "project", inRect(0, 0, 400, 36))
	host.Eventually("the workspace tab", func() (bool, string) { _, ok := statusLeft(host); return ok, host.Dump() })
	openMenu(t, host)
	clickContaining(t, host, "Host with Relay")
	host.Eventually("the item to say Hosting", func() (bool, string) { got := itemText(t, host); return strings.Contains(got, "Hosting"), got })
	cfg := p.readState(t, "relay.json")
	if cfg["listening"] != true || len(cfg["workspaces"].([]any)) != 1 {
		t.Errorf("relay.json = %v", cfg)
	}
	if !listensOn(p.psPort) {
		t.Error("nothing listens on the port of Relay")
	}

	// the other computer watches the workspace
	client.Focus()
	client.Dev("dev.openWindowTab", map[string]any{"kind": relay.TabKindID})
	client.Settle()
	client.Dev(relay.DevAddHostCommand, map[string]any{"host": "127.0.0.1", "port": p.psPort, "name": "Office laptop", "keyFile": p.keyFile})
	client.Dev(relay.DevConnectCommand, map[string]any{"host": "Office laptop"})
	client.WaitText("Connected, 1 workspace, watch only")
	host.Focus()
	host.Eventually("the item to count the computer", func() (bool, string) {
		got := itemText(t, host)
		return strings.Contains(got, "Hosting · 1"), got
	})
	openMenu(t, host)
	if _, ok := textContaining(host, "Stop Hosting with Relay"); !ok {
		t.Errorf("the menu does not offer Stop Hosting with Relay: %s", host.Dump())
	}
	hostingShot(t, host, "17e.4-relay-hosting.png")

	// Disconnect <key> ends that computer's session and leaves hosting on
	clickContaining(t, host, "Disconnect alice@laptop")
	client.Eventually("the end of the session", func() (bool, string) {
		txt := client.Dump()
		return strings.Contains(txt, "Disconnected by"), txt
	})
	host.Eventually("the item to be Hosting with nobody", func() (bool, string) {
		got := itemText(t, host)
		return strings.Contains(got, "Hosting") && !strings.Contains(got, "·"), got
	})

	// it connects again, and Stop Hosting ends its session with the message and turns listening off
	client.Dev(relay.DevConnectCommand, map[string]any{"host": "Office laptop"})
	client.WaitText("Connected, 1 workspace, watch only")
	host.Focus()
	host.Eventually("the item to count the computer again", func() (bool, string) {
		return strings.Contains(itemText(t, host), "Hosting · 1"), itemText(t, host)
	})
	openMenu(t, host)
	clickContaining(t, host, "Stop Hosting with Relay")
	host.Eventually("the item to be the icon alone", func() (bool, string) {
		got := itemText(t, host)
		return !strings.Contains(got, "Hosting"), got
	})
	client.Eventually("the message", func() (bool, string) {
		txt := client.Dump()
		return strings.Contains(txt, "The host stopped sharing this workspace."), txt
	})
	hostingShot(t, client, "17e.4-relay-client-stopped.png")
	cfg = p.readState(t, "relay.json")
	if cfg["listening"] != false || len(cfg["workspaces"].([]any)) != 0 {
		t.Errorf("relay.json after Stop Hosting = %v", cfg)
	}
	host.Eventually("the listener to close", func() (bool, string) { return !listensOn(p.psPort), "still listening" })
}
