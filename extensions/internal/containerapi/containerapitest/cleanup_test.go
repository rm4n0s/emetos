package containerapitest_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// The tests of scripts/container-test-cleanup.sh run the script against a fake CLI: this very test binary, started with
// EMETOS_FAKE_CLI set, which understands the few docker and podman commands the script uses, and carries them out through
// the engine client on a fake engine. It logs every command line, so that a test can check that the script asked only for
// labelled things and ran nothing else.

const (
	envFakeCLI = "EMETOS_FAKE_CLI"
	envSock    = "EMETOS_FAKE_CLI_SOCK"
	envKind    = "EMETOS_FAKE_CLI_KIND"
	envLog     = "EMETOS_FAKE_CLI_LOG"
	envFail    = "EMETOS_FAKE_CLI_FAIL"
)

func TestMain(m *testing.M) {
	if os.Getenv(envFakeCLI) != "" {
		os.Exit(fakeCLI(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeCLI is the fake docker and podman program. It returns the exit status.
func fakeCLI(args []string) int {
	if logPath := os.Getenv(envLog); logPath != "" {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(f, strings.Join(args, " "))
			_ = f.Close()
		}
	}
	if fail := os.Getenv(envFail); fail != "" && len(args) > 0 && strings.Contains(strings.Join(args, " "), fail) {
		fmt.Fprintln(os.Stderr, "fake cli: failing as asked")
		return 1
	}
	kind := containerapi.Kind(os.Getenv(envKind))
	client := containerapi.New(kind, containerapi.UnixEndpoint(os.Getenv(envSock)), containerapi.Options{})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// words that are not flags, and the filter
	var words, rest []string
	filters := containerapi.Filters{}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--filter" && i+1 < len(args):
			k, v, _ := strings.Cut(args[i+1], "=")
			filters[k] = append(filters[k], v)
			i++
		case strings.HasPrefix(a, "-"):
			rest = append(rest, a)
		default:
			words = append(words, a)
		}
	}
	has := func(flag string) bool {
		for _, r := range rest {
			if r == flag {
				return true
			}
		}
		return false
	}
	print := func(lines ...string) {
		for _, l := range lines {
			fmt.Println(l)
		}
	}
	die := func(err error) int {
		fmt.Fprintln(os.Stderr, "fake cli:", err)
		return 1
	}
	if len(words) == 0 {
		return 99
	}
	switch {
	case words[0] == "ps":
		list, err := client.ListContainers(ctx, containerapi.ListContainersOptions{All: has("--all") || has("-a"), Filters: filters})
		if err != nil {
			return die(err)
		}
		for _, c := range list {
			print(c.ID[:12])
		}
	case words[0] == "rm":
		for _, id := range words[1:] {
			if err := client.RemoveContainer(ctx, id, containerapi.RemoveOptions{Force: has("--force"), Volumes: has("--volumes")}); err != nil {
				return die(err)
			}
		}
	case len(words) >= 2 && words[0] == "network" && words[1] == "ls":
		list, err := client.ListNetworks(ctx, filters)
		if err != nil {
			return die(err)
		}
		for _, n := range list {
			print(n.Name)
		}
	case len(words) >= 2 && words[0] == "network" && words[1] == "rm":
		for _, id := range words[2:] {
			if err := client.RemoveNetwork(ctx, id); err != nil {
				return die(err)
			}
		}
	case len(words) >= 2 && words[0] == "volume" && words[1] == "ls":
		list, err := client.ListVolumes(ctx, filters)
		if err != nil {
			return die(err)
		}
		for _, v := range list {
			print(v.Name)
		}
	case len(words) >= 2 && words[0] == "volume" && words[1] == "rm":
		for _, name := range words[2:] {
			if err := client.RemoveVolume(ctx, name, has("--force")); err != nil {
				return die(err)
			}
		}
	case len(words) >= 2 && words[0] == "pod" && words[1] == "ps":
		list, err := client.ListPods(ctx, filters)
		if err != nil {
			return die(err)
		}
		for _, p := range list {
			print(p.ID[:12])
		}
	case len(words) >= 2 && words[0] == "pod" && words[1] == "rm":
		for _, id := range words[2:] {
			if err := client.RemovePod(ctx, id, has("--force")); err != nil {
				return die(err)
			}
		}
	default:
		fmt.Fprintln(os.Stderr, "fake cli: unexpected command:", strings.Join(args, " "))
		return 99
	}
	return 0
}

// cleanupSetup is a fake engine with labelled and unlabelled things, and the fake CLI that talks to it.
type cleanupSetup struct {
	fake *containerapitest.Fake
	cli  string
	log  string
	env  []string
}

func newCleanupSetup(t *testing.T, kind containerapi.Kind) *cleanupSetup {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := containerapitest.NewFake(t, kind, containerapitest.FakeOptions{})
	dir := t.TempDir()
	cli := filepath.Join(dir, "fakecli")
	script := "#!/bin/sh\nexec \"" + self + "\" \"$@\"\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "commands.log")
	return &cleanupSetup{fake: fake, cli: cli, log: logPath, env: []string{
		envFakeCLI + "=1", envSock + "=" + fake.Socket(), envKind + "=" + string(kind), envLog + "=" + logPath,
	}}
}

// run runs the cleanup script with the fake CLI as $DOCKER or $PODMAN.
func (s *cleanupSetup) run(t *testing.T, kind containerapi.Kind, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	program := "DOCKER"
	if kind == containerapi.KindPodman {
		program = "PODMAN"
	}
	other := "PODMAN"
	if kind == containerapi.KindPodman {
		other = "DOCKER"
	}
	cmd := exec.Command(filepath.Join(root, "scripts", "container-test-cleanup.sh"), args...)
	cmd.Dir = root
	cmd.Env = append(append(append(os.Environ(), s.env...), extraEnv...), program+"="+s.cli, other+"=/nonexistent/"+strings.ToLower(other))
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok {
		return string(out), exit.ExitCode()
	}
	if err != nil {
		t.Fatalf("running the script: %v", err)
	}
	return string(out), 0
}

// names lists what is in the fake, as "container name", for a comparison.
func (s *cleanupSetup) names(t *testing.T, kind containerapi.Kind) []string {
	t.Helper()
	c := s.fake.Client()
	defer c.Close()
	ctx := context.Background()
	var out []string
	cs, err := c.ListContainers(ctx, containerapi.ListContainersOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range cs {
		out = append(out, "container "+x.Name())
	}
	ns, err := c.ListNetworks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ns {
		out = append(out, "network "+x.Name)
	}
	vs, err := c.ListVolumes(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range vs {
		out = append(out, "volume "+x.Name)
	}
	if kind == containerapi.KindPodman {
		ps, err := c.ListPods(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range ps {
			out = append(out, "pod "+x.Name)
		}
	}
	sort.Strings(out)
	return out
}

// populate puts labelled and unlabelled things into the fake. The unlabelled ones stand for the user's own.
func (s *cleanupSetup) populate(t *testing.T, kind containerapi.Kind) {
	t.Helper()
	label := map[string]string{containerapitest.LabelKey: containerapitest.LabelValue}
	s.fake.AddContainer("emetos-test-aaa", "busybox", label, "running")
	s.fake.AddContainer("emetos-test-bbb", "busybox", label, "exited")
	s.fake.AddContainer("my-database", "postgres", nil, "running")
	s.fake.AddContainer("my-stopped-one", "alpine", map[string]string{"owner": "me"}, "exited")
	c := s.fake.Client()
	defer c.Close()
	ctx := context.Background()
	mustNil := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := c.CreateNetwork(ctx, containerapi.NetworkSpec{Name: "emetos-test-net", Labels: label})
	mustNil(err)
	_, err = c.CreateNetwork(ctx, containerapi.NetworkSpec{Name: "my-network"})
	mustNil(err)
	_, err = c.CreateVolume(ctx, containerapi.VolumeSpec{Name: "emetos-test-vol", Labels: label})
	mustNil(err)
	_, err = c.CreateVolume(ctx, containerapi.VolumeSpec{Name: "my-data"})
	mustNil(err)
	if kind == containerapi.KindPodman {
		_, err = c.CreatePod(ctx, containerapi.PodSpec{Name: "emetos-test-pod", Labels: label})
		mustNil(err)
		_, err = c.CreatePod(ctx, containerapi.PodSpec{Name: "my-pod"})
		mustNil(err)
	}
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestCleanupScriptRemovesOnlyLabelledThings(t *testing.T) {
	for _, kind := range []containerapi.Kind{containerapi.KindDocker, containerapi.KindPodman} {
		t.Run(string(kind), func(t *testing.T) {
			s := newCleanupSetup(t, kind)
			s.populate(t, kind)
			out, status := s.run(t, kind, nil, "--engine", string(kind))
			if status != 0 {
				t.Fatalf("the script exited with %d:\n%s", status, out)
			}
			want := []string{"container my-database", "container my-stopped-one", "network bridge", "network my-network", "volume my-data"}
			if kind == containerapi.KindPodman {
				want = append(want, "pod my-pod")
			}
			sort.Strings(want)
			if got := s.names(t, kind); strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("left in the engine:\n got %v\nwant %v\n(script output:\n%s)", got, want, out)
			}
			// every list asked for the label, and nothing but ps, rm, network, volume and pod commands ran
			for _, line := range readLog(t, s.log) {
				fields := strings.Fields(line)
				isList := strings.Contains(line, "--quiet") && strings.Contains(line, "--filter label="+"dev.emetos.test=1")
				isRemove := fields[0] == "rm" || (len(fields) > 1 && fields[1] == "rm")
				if !isList && !isRemove {
					t.Errorf("an unexpected command ran: %q", line)
				}
				for _, bad := range []string{"prune", "system", "stop", "kill"} {
					if strings.Contains(line, bad) {
						t.Errorf("the script ran %q", line)
					}
				}
			}
		})
	}
}

func TestCleanupScriptDryRunRemovesNothing(t *testing.T) {
	s := newCleanupSetup(t, containerapi.KindDocker)
	s.populate(t, containerapi.KindDocker)
	before := s.names(t, containerapi.KindDocker)
	out, status := s.run(t, containerapi.KindDocker, nil, "--engine", "docker", "--dry-run")
	if status != 0 {
		t.Fatalf("exit %d:\n%s", status, out)
	}
	if after := s.names(t, containerapi.KindDocker); strings.Join(after, "|") != strings.Join(before, "|") {
		t.Errorf("a dry run changed the engine:\n before %v\n after %v", before, after)
	}
	for _, line := range readLog(t, s.log) {
		if f := strings.Fields(line); f[0] == "rm" || (len(f) > 1 && f[1] == "rm") {
			t.Errorf("a dry run ran %q", line)
		}
	}
	if !strings.Contains(out, "would remove 2 containers") {
		t.Errorf("the dry run does not say what it would remove:\n%s", out)
	}
}

func TestCleanupScriptStopsWhenAListFails(t *testing.T) {
	s := newCleanupSetup(t, containerapi.KindDocker)
	s.populate(t, containerapi.KindDocker)
	before := s.names(t, containerapi.KindDocker)
	// the list of networks fails: the containers (done before) are gone, the script stops there, so the networks and volumes stay, and the status says it failed
	out, status := s.run(t, containerapi.KindDocker, []string{envFail + "=network ls"}, "--engine", "docker")
	if status == 0 {
		t.Errorf("exit status 0 although a list failed:\n%s", out)
	}
	after := s.names(t, containerapi.KindDocker)
	contains := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	for _, mustStay := range []string{"container my-database", "container my-stopped-one", "network my-network", "volume my-data", "network emetos-test-net", "volume emetos-test-vol"} {
		if !contains(after, mustStay) {
			t.Errorf("%q is gone after the failed run (before: %v, after: %v)", mustStay, before, after)
		}
	}
}

func TestCleanupScriptSkipsAMissingCLIAndRefusesBadArguments(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	run := func(env []string, args ...string) (string, int) {
		cmd := exec.Command(filepath.Join(root, "scripts", "container-test-cleanup.sh"), args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if exit, ok := err.(*exec.ExitError); ok {
			return string(out), exit.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
	out, status := run([]string{"DOCKER=/nonexistent/docker", "PODMAN=/nonexistent/podman"})
	if status != 0 || !strings.Contains(out, "not installed") {
		t.Errorf("without any CLI: exit %d, output %q", status, out)
	}
	for _, args := range [][]string{{"--engine"}, {"--engine", "rkt"}, {"--bogus"}} {
		if _, status := run(nil, args...); status != 64 {
			t.Errorf("%v: exit %d, want 64", args, status)
		}
	}
}
