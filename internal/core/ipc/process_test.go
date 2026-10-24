package ipc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

// The two-process tests start copies of the test binary as helper processes (the pattern of os/exec's own tests):
// TestMain sees helperEnv and runs only the ipc code, with no window and no test framework, then exits.
//
//	EMETOS_IPC_HELPER=launch   Join; as the instance serve until EMETOS_IPC_EXPECT requests are done; as a client
//	                           send a request (cwd EMETOS_IPC_ID, files EMETOS_IPC_FILES, wait EMETOS_IPC_WAIT=1)
//	                           and exit with the exit code of the reply.
//
// Helpers print "ROLE server", "ROLE client", "REQ <cwd> <wait>" and "REPLY <type> <code>" lines. A request that
// waits is released by a line on the standard input of the instance; the reply is done with the code
// EMETOS_IPC_CODE.
const (
	helperEnv     = "EMETOS_IPC_HELPER"
	helperTimeout = 60 * time.Second
)

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) != "" {
		os.Exit(helperMain())
	}
	os.Exit(m.Run())
}

func helperMain() int {
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	defer cancel()
	paths, err := storage.ResolvePaths()
	if err != nil {
		fmt.Println("ERROR", err)
		return 90
	}
	loc, err := Locations(paths, os.Getuid())
	if err != nil {
		fmt.Println("ERROR", err)
		return 91
	}
	srv, client, err := Join(ctx, Options{Location: loc})
	if err != nil {
		fmt.Println("ERROR", err)
		return 92
	}
	if srv != nil {
		return helperServe(ctx, srv)
	}
	defer func() { _ = client.Close() }()
	fmt.Println("ROLE client")
	req := Request{Cwd: os.Getenv("EMETOS_IPC_ID"), Open: &OpenArgs{Wait: os.Getenv("EMETOS_IPC_WAIT") == "1"}}
	for _, f := range strings.Fields(os.Getenv("EMETOS_IPC_FILES")) {
		req.Open.Files = append(req.Open.Files, FileTarget{Path: f})
	}
	reply, err := client.Send(ctx, req)
	if err != nil {
		fmt.Println("ERROR", err)
		return 93
	}
	fmt.Println("REPLY", reply.Type, reply.ExitCode)
	return reply.ExitCode
}

func helperServe(ctx context.Context, srv *Server) int {
	expect, _ := strconv.Atoi(os.Getenv("EMETOS_IPC_EXPECT"))
	code, _ := strconv.Atoi(os.Getenv("EMETOS_IPC_CODE"))
	release := make(chan struct{}, 64)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			release <- struct{}{}
		}
	}()
	served := make(chan struct{}, 64)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fmt.Println("ROLE server")
	go func() {
		for range expect {
			select {
			case <-served:
			case <-ctx.Done():
				return
			}
		}
		cancel()
	}()
	if expect == 0 {
		cancel()
	}
	err := srv.Serve(ctx, func(hctx context.Context, req Request) Reply {
		fmt.Println("REQ", req.Cwd, req.Open.Wait, len(req.Open.Files))
		defer func() { served <- struct{}{} }()
		if req.Open.Wait {
			select {
			case <-release:
				return Done(code)
			case <-hctx.Done():
				fmt.Println("CANCELLED", req.Cwd)
				return Failed("cancelled")
			}
		}
		return OK()
	})
	if err != nil {
		fmt.Println("ERROR", err)
		return 94
	}
	fmt.Println("EXIT")
	return 0
}

// helper is a helper process of a test.
type helper struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	done  chan struct{}
	err   error
}

// startHelper starts a helper in its own process group, under the deadline of the test: when the test ends
// or the helper outlives helperTimeout the whole group is killed (the equivalent of timeout -k).
func startHelper(t *testing.T, env ...string) *helper {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	// the race detector otherwise sleeps a second before a process exits
	cmd.Env = append(os.Environ(), append([]string{helperEnv + "=launch", "GORACE=atexit_sleep_ms=0"}, env...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start the helper: %v", err)
	}
	h := &helper{t: t, cmd: cmd, stdin: stdin, lines: make(chan string, 256), done: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			h.lines <- sc.Text()
		}
		h.err = cmd.Wait()
		close(h.lines)
		close(h.done)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-h.done:
		case <-time.After(wait):
			t.Errorf("helper %d did not end", cmd.Process.Pid)
		}
		cancel()
	})
	return h
}

// line waits for the next line of output.
func (h *helper) line() string {
	h.t.Helper()
	select {
	case l, ok := <-h.lines:
		if !ok {
			h.t.Fatal("the helper ended without the line")
		}
		return l
	case <-time.After(wait):
		h.t.Fatal("no line from the helper")
		return ""
	}
}

// want waits for a line that equals text, passing over the others.
func (h *helper) want(text string) {
	h.t.Helper()
	for {
		if l := h.line(); l == text {
			return
		}
	}
}

// all collects the lines up to the end of the helper and returns them with its exit code.
func (h *helper) finish() (lines []string, code int) {
	h.t.Helper()
	for {
		select {
		case l, ok := <-h.lines:
			if !ok {
				<-h.done
				if h.err != nil {
					if ee, isExit := h.err.(*exec.ExitError); isExit {
						return lines, ee.ExitCode()
					}
					h.t.Fatalf("helper: %v", h.err)
				}
				return lines, 0
			}
			lines = append(lines, l)
		case <-time.After(wait):
			h.t.Fatalf("the helper did not end; output so far %q", lines)
		}
	}
}

func TestTwoProcessesHandOver(t *testing.T) {
	storageIsolate(t)
	server := startHelper(t, "EMETOS_IPC_EXPECT=1")
	server.want("ROLE server")
	client := startHelper(t, "EMETOS_IPC_ID=/work/dir", "EMETOS_IPC_FILES=/work/dir/a.go /work/dir/b.go")
	lines, code := client.finish()
	if code != 0 || !slices.Equal(lines, []string{"ROLE client", "REPLY ok 0"}) {
		t.Errorf("client: code %d, lines %q", code, lines)
	}
	lines, code = server.finish()
	if code != 0 || !slices.Equal(lines, []string{"REQ /work/dir false 2", "EXIT"}) {
		t.Errorf("server: code %d, lines %q", code, lines)
	}
}

func TestTwoProcessesWait(t *testing.T) {
	storageIsolate(t)
	server := startHelper(t, "EMETOS_IPC_EXPECT=1", "EMETOS_IPC_CODE=5")
	server.want("ROLE server")
	client := startHelper(t, "EMETOS_IPC_ID=w", "EMETOS_IPC_WAIT=1")
	client.want("ROLE client")
	server.want("REQ w true 0")
	// the client has not been answered while the files are open
	select {
	case l, ok := <-client.lines:
		t.Fatalf("the client went on before the release: %q %v", l, ok)
	case <-time.After(300 * time.Millisecond): // the one place a pause is the test: nothing may happen
	}
	if _, err := io.WriteString(server.stdin, "release\n"); err != nil {
		t.Fatal(err)
	}
	lines, code := client.finish()
	if code != 5 || !slices.Equal(lines, []string{"REPLY done 5"}) {
		t.Errorf("client: code %d, lines %q", code, lines)
	}
	if _, code := server.finish(); code != 0 {
		t.Errorf("server code %d", code)
	}
}

func TestTwoProcessesKilledClientCancelsTheWait(t *testing.T) {
	storageIsolate(t)
	server := startHelper(t, "EMETOS_IPC_EXPECT=1")
	server.want("ROLE server")
	client := startHelper(t, "EMETOS_IPC_ID=k", "EMETOS_IPC_WAIT=1")
	client.want("ROLE client")
	server.want("REQ k true 0")
	if err := syscall.Kill(-client.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	server.want("CANCELLED k")
	if _, code := server.finish(); code != 0 {
		t.Errorf("server code %d", code)
	}
}

func TestTwoProcessesStaleSocketAfterAKilledInstance(t *testing.T) {
	paths := storageIsolate(t)
	first := startHelper(t, "EMETOS_IPC_EXPECT=5")
	first.want("ROLE server")
	loc, err := Locations(paths, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-first.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	first.finish()
	// the killed instance could not remove its socket
	if info, err := os.Lstat(loc.Socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("no stale socket left: %v %v", info, err)
	}
	second := startHelper(t, "EMETOS_IPC_EXPECT=1")
	second.want("ROLE server")
	third := startHelper(t, "EMETOS_IPC_ID=after")
	if lines, code := third.finish(); code != 0 || !slices.Equal(lines, []string{"ROLE client", "REPLY ok 0"}) {
		t.Errorf("client: code %d, lines %q", code, lines)
	}
	second.finish()
	// a clean exit removes the socket
	if _, err := os.Lstat(loc.Socket); !os.IsNotExist(err) {
		t.Errorf("the socket of the instance that ended is still there: %v", err)
	}
}

func TestTwoProcessesRaceOfLaunches(t *testing.T) {
	const launches = 4
	for round := range 3 {
		t.Run("round "+strconv.Itoa(round), func(t *testing.T) {
			storageIsolate(t)
			procs := make([]*helper, launches)
			for i := range procs {
				procs[i] = startHelper(t, "EMETOS_IPC_ID=l"+strconv.Itoa(i), "EMETOS_IPC_EXPECT="+strconv.Itoa(launches-1))
			}
			servers := 0
			var served []string
			for i, p := range procs {
				lines, code := p.finish()
				if code != 0 || len(lines) == 0 {
					t.Errorf("launch %d: code %d, lines %q", i, code, lines)
					continue
				}
				switch lines[0] {
				case "ROLE server":
					servers++
					for _, l := range lines[1:] {
						if rest, ok := strings.CutPrefix(l, "REQ "); ok {
							served = append(served, strings.Fields(rest)[0])
						}
					}
				case "ROLE client":
					if !slices.Equal(lines, []string{"ROLE client", "REPLY ok 0"}) {
						t.Errorf("launch %d: lines %q", i, lines)
					}
				default:
					t.Errorf("launch %d: lines %q", i, lines)
				}
			}
			if servers != 1 {
				t.Fatalf("%d instances, want 1", servers)
			}
			slices.Sort(served)
			if len(served) != launches-1 {
				t.Errorf("the instance served %q, want %d requests", served, launches-1)
			}
			for i := range served {
				if i > 0 && served[i] == served[i-1] {
					t.Errorf("a launch was served twice: %q", served)
				}
			}
		})
	}
}

// storageIsolate points the runtime and the user folders at temporary ones; the helpers inherit the variables.
func storageIsolate(t *testing.T) storage.Paths {
	t.Helper()
	return isolatePaths(t)
}
