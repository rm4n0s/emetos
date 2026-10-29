// Package secrettest is what the tests of the keyring run against: a private D-Bus session bus (a dbus-daemon the test starts
// with a configuration of its own, in a temporary folder) and a fake Secret Service on it. Nothing here touches the bus or the
// keyring of the user: the daemon listens on a socket in a temporary folder, and the fake service keeps its secrets in memory.
// It is for tests only (SPEC §10.21 "Connections", step 19.1).
package secrettest

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// Bus is a private session bus.
type Bus struct {
	// Address is the address of the bus ("unix:path=...").
	Address string
	cmd     *exec.Cmd
}

// busConfig is the configuration of the daemon: the session bus type, one socket, the EXTERNAL authentication, and every client
// may do everything (the bus is private to the test).
const busConfig = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:path=%s</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`

// StartBus starts a dbus-daemon on a socket of its own and stops it when the test ends. The test is skipped when dbus-daemon
// is not installed. The daemon runs under timeout(1), in a process group of its own, so that it cannot outlive the test run.
func StartBus(t testing.TB) *Bus {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	timeout, err := exec.LookPath("timeout")
	if err != nil {
		t.Skip("timeout is not installed")
	}
	dir, err := os.MkdirTemp("", "emetos-dbus-")
	if err != nil {
		t.Fatal(err)
	}
	if len(dir) > 60 { // a socket path is limited to about 100 bytes
		short, err := os.MkdirTemp("/tmp", "emetos-dbus-")
		if err != nil {
			t.Fatal(err)
		}
		_ = os.RemoveAll(dir)
		dir = short
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("removing %s: %v", dir, err)
		}
	})
	sock := filepath.Join(dir, "bus")
	conf := filepath.Join(dir, "bus.conf")
	if err := os.WriteFile(conf, []byte(strings.Replace(busConfig, "%s", sock, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(timeout, "-k", "2", "300", daemon, "--config-file="+conf, "--nofork", "--nopidfile", "--print-address=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting dbus-daemon: %v", err)
	}
	b := &Bus{cmd: cmd}
	t.Cleanup(b.stop)
	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(out).ReadString('\n')
		line <- strings.TrimSpace(s)
	}()
	select {
	case addr := <-line:
		if addr == "" {
			t.Fatal("dbus-daemon printed no address")
		}
		b.Address = addr
	case <-time.After(10 * time.Second):
		t.Fatal("dbus-daemon did not print its address in 10 seconds")
	}
	return b
}

// stop ends the daemon and its process group, and waits for it.
func (b *Bus) stop() {
	if b.cmd == nil || b.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGTERM) // the group is gone or going; an error says only that it already is
	done := make(chan struct{})
	go func() { _ = b.cmd.Wait(); close(done) }() // the exit status of a killed daemon is not interesting
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

// Dial connects to the bus, as the function SecretService.Dial wants.
func (b *Bus) Dial(ctx context.Context) (*dbus.Conn, error) {
	return dbus.Connect(b.Address, dbus.WithContext(ctx))
}
