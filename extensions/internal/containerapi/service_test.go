package containerapi_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// fakePodmanDir makes a folder with a program named "podman" that runs the fake podman in the given mode, and returns the
// folder (for PATH), the file the fake writes its pids to, and the environment the service should get.
func fakePodmanDir(t *testing.T, mode string) (dir, info string, env []string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte("#!/bin/sh\nexec \""+self+"\" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	info = filepath.Join(dir, "info")
	return dir, info, []string{envFakePodman + "=" + mode, envFakePodmanInfo + "=" + info}
}

// shortSocketDir makes a folder with a short path for the socket.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func readInfo(t *testing.T, path string) (pid, pgid, ppid int) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(string(b))
	if len(f) != 3 {
		t.Fatalf("info file = %q", b)
	}
	pid, _ = strconv.Atoi(f[0])
	pgid, _ = strconv.Atoi(f[1])
	ppid, _ = strconv.Atoi(f[2])
	return
}

// alive reports whether a process exists.
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestStartServiceRunsPodmanInItsOwnGroupAndStopsIt(t *testing.T) {
	dir, info, env := fakePodmanDir(t, "serve")
	sockDir := shortSocketDir(t)
	svc, err := containerapi.StartService(context.Background(), containerapi.ServiceOptions{
		Getenv: func(k string) string {
			if k == "PATH" {
				return dir
			}
			return ""
		},
		Env: env, SocketDir: sockDir, PID: 4242,
	})
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			svc.Stop()
		}
	}()
	if want := filepath.Join(sockDir, "podman-4242.sock"); svc.Socket != want {
		t.Errorf("socket = %s, want %s", svc.Socket, want)
	}
	pid, pgid, ppid := readInfo(t, info)
	if pgid != pid {
		t.Errorf("the service runs in group %d, want a group of its own (its pid %d)", pgid, pid)
	}
	if ppid != os.Getpid() {
		t.Errorf("the service's parent is %d, want this process %d", ppid, os.Getpid())
	}
	own, _ := syscall.Getpgid(0)
	if pgid == own {
		t.Error("the service shares the process group of the test")
	}
	// it answers as an engine
	c := containerapi.New(containerapi.KindPodman, containerapi.UnixEndpoint(svc.Socket), containerapi.Options{})
	defer c.Close()
	if _, err := c.Negotiate(context.Background()); err != nil {
		t.Fatalf("the service does not answer: %v", err)
	}
	svc.Stop()
	stopped = true
	svc.Stop() // twice is harmless
	select {
	case <-svc.Done():
	default:
		t.Error("Done is not closed after Stop")
	}
	if alive(pid) {
		// the process is gone once Wait has reaped it; give the kernel a moment
		time.Sleep(100 * time.Millisecond)
		if alive(pid) {
			t.Errorf("process %d still runs after Stop", pid)
		}
	}
	if _, err := os.Stat(svc.Socket); !os.IsNotExist(err) {
		t.Errorf("the socket is still there after Stop: %v", err)
	}
}

func TestStopKillsAServiceThatIgnoresSIGTERM(t *testing.T) {
	dir, info, env := fakePodmanDir(t, "stubborn")
	svc, err := containerapi.StartService(context.Background(), containerapi.ServiceOptions{
		Getenv: func(k string) string {
			if k == "PATH" {
				return dir
			}
			return ""
		},
		Env: env, SocketDir: shortSocketDir(t), Grace: 150 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	pid, _, _ := readInfo(t, info)
	start := time.Now()
	svc.Stop()
	if d := time.Since(start); d < 100*time.Millisecond || d > 5*time.Second {
		t.Errorf("Stop took %v, want about the grace time of 150 ms", d)
	}
	time.Sleep(50 * time.Millisecond)
	if alive(pid) {
		t.Errorf("process %d survived SIGKILL", pid)
	}
}

func TestStartServiceFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T) containerapi.ServiceOptions
		ctx   func() (context.Context, context.CancelFunc)
		route string
		text  string
	}{
		{
			name: "no podman on PATH",
			setup: func(t *testing.T) containerapi.ServiceOptions {
				return containerapi.ServiceOptions{Getenv: func(string) string { return t.TempDir() }, SocketDir: shortSocketDir(t)}
			},
			route: "StartService.ContainerAPIPodmanNotFound",
		},
		{
			name: "podman that ends at once says why",
			setup: func(t *testing.T) containerapi.ServiceOptions {
				dir, _, env := fakePodmanDir(t, "exit")
				return containerapi.ServiceOptions{Getenv: func(string) string { return dir }, Env: env, SocketDir: shortSocketDir(t)}
			},
			route: "StartService.ContainerAPIPodmanStartFailed",
			text:  "boom",
		},
		{
			name: "podman that never opens its socket",
			setup: func(t *testing.T) containerapi.ServiceOptions {
				dir, _, env := fakePodmanDir(t, "hang")
				return containerapi.ServiceOptions{Getenv: func(string) string { return dir }, Env: env, SocketDir: shortSocketDir(t), ReadyTimeout: 300 * time.Millisecond, Grace: 100 * time.Millisecond}
			},
			route: "StartService.ContainerAPIPodmanTimeout",
		},
		{
			name: "the context ends while waiting",
			setup: func(t *testing.T) containerapi.ServiceOptions {
				dir, _, env := fakePodmanDir(t, "hang")
				return containerapi.ServiceOptions{Getenv: func(string) string { return dir }, Env: env, SocketDir: shortSocketDir(t), Grace: 100 * time.Millisecond}
			},
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 200*time.Millisecond)
			},
			route: "StartService.ContainerAPIPodmanStartFailed",
		},
		{
			name: "the socket folder cannot be made",
			setup: func(t *testing.T) containerapi.ServiceOptions {
				dir, _, env := fakePodmanDir(t, "serve")
				file := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(file, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return containerapi.ServiceOptions{Getenv: func(string) string { return dir }, Env: env, SocketDir: filepath.Join(file, "sub")}
			},
			route: "StartService.ContainerAPIPodmanStartFailed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.Background(), context.CancelFunc(func() {})
			if tc.ctx != nil {
				ctx, cancel = tc.ctx()
			}
			defer cancel()
			svc, err := containerapi.StartService(ctx, tc.setup(t))
			if svc != nil {
				svc.Stop()
				t.Fatal("StartService succeeded")
			}
			appErr := routeOf(t, err, tc.route)
			if tc.text != "" && !strings.Contains(appErr.Message, tc.text) {
				t.Errorf("message = %q, want it to hold %q (the program's own error output)", appErr.Message, tc.text)
			}
		})
	}
}

func TestStartServiceTakesTheSocketFolderFromXDGRuntimeDir(t *testing.T) {
	dir, _, env := fakePodmanDir(t, "serve")
	rt := shortSocketDir(t)
	svc, err := containerapi.StartService(context.Background(), containerapi.ServiceOptions{
		Getenv: func(k string) string {
			switch k {
			case "PATH":
				return dir
			case "XDG_RUNTIME_DIR":
				return rt
			}
			return ""
		},
		Env: env,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	if want := filepath.Join(rt, "emetos", "podman-"+strconv.Itoa(os.Getpid())+".sock"); svc.Socket != want {
		t.Errorf("socket = %s, want %s", svc.Socket, want)
	}
	if fi, err := os.Stat(filepath.Dir(svc.Socket)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("socket folder: %v, %v; want mode 0700", fi, err)
	}
}
