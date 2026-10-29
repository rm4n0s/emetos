package containerui

import (
	"context"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// TestCommandLines holds the command lines of the CLI that the workflows run (SPEC §10.20): build, push, run interactive, the shell,
// and Compose with its files, project, environment file, profiles and services.
func TestCommandLines(t *testing.T) {
	compose := []string{"docker", "compose"}
	tests := []struct {
		name string
		got  []string
		want string
	}{
		{"build", buildArgv("docker", "sub/Dockerfile", "app:latest", "sub"), "docker build --pull --rm -f sub/Dockerfile -t app:latest sub"},
		{"push", pushArgv("docker", "ghcr.io/me/app:1.0"), "docker push ghcr.io/me/app:1.0"},
		{"run interactive with ports", runInteractiveArgv("docker", []string{"80/tcp", "53/udp", "9000"}, "app:1"), "docker run --rm -it -p 80:80/tcp -p 53:53/udp -p 9000:9000 app:1"},
		{"run interactive with none", runInteractiveArgv("podman", nil, "app:1"), "podman run --rm -it app:1"},
		{"shell", attachShellArgv("docker", "abc123", "bash"), "docker exec -it abc123 bash"},
		{"up", composeArgv(compose, composeTarget{files: []string{"compose.yaml"}}, upVerb(true, true, nil)...), "docker compose -f compose.yaml up -d --build"},
		{"up in the foreground without a build", composeArgv(compose, composeTarget{files: []string{"a.yml", "b.yml"}}, upVerb(false, false, nil)...), "docker compose -f a.yml -f b.yml up"},
		{"up of services", composeArgv(compose, composeTarget{files: []string{"c.yaml"}}, upVerb(true, true, []string{"web", "db"})...), "docker compose -f c.yaml up -d --build web db"},
		{"up of profiles", composeArgv(compose, composeTarget{files: []string{"c.yaml"}, profiles: []string{"debug", "tools"}}, upVerb(true, true, nil)...),
			"docker compose --profile debug --profile tools -f c.yaml up -d --build"},
		{"down", composeArgv(compose, composeTarget{files: []string{"c.yaml"}}, downVerb(nil)...), "docker compose -f c.yaml down"},
		{"down of services", composeArgv(compose, composeTarget{files: []string{"c.yaml"}}, downVerb([]string{"web"})...), "docker compose -f c.yaml down web"},
		{"logs of a project", composeArgv([]string{"docker-compose"}, composeTarget{files: []string{"/w/c.yaml"}, project: "shop", envFile: "/w/.env"}, logsVerb()...),
			"docker-compose -f /w/c.yaml -p shop --env-file /w/.env logs --tail 1000 -f"},
		{"the services of a file", composeArgv(compose, composeTarget{files: []string{"c.yaml"}}, configVerb("services")...), "docker compose -f c.yaml config --services"},
		{"the profiles of a file", composeArgv(compose, composeTarget{files: []string{"c.yaml"}}, configVerb("profiles")...), "docker compose -f c.yaml config --profiles"},
	}
	for _, tc := range tests {
		if got := strings.Join(tc.got, " "); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	// the base slice of the caller is never changed by the command line
	base := make([]string, 2, 10)
	copy(base, compose)
	_ = composeArgv(base, composeTarget{files: []string{"x"}}, "up")
	if base[:3][2] != "" {
		t.Error("composeArgv wrote into the slice it was given")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"docker", "docker"}, {"a/b-c_d.e:f@g=h+i,j%k^l", "a/b-c_d.e:f@g=h+i,j%k^l"}, {"", "''"}, {"has space", "'has space'"},
		{"it's", `'it'\''s'`}, {"$HOME", "'$HOME'"}, {"a;b", "'a;b'"}, {"*", "'*'"}, {"line\nbreak", "'line\nbreak'"}, {"é", "'é'"},
	}
	for _, tc := range tests {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := shellJoin([]string{"docker", "exec", "-it", "my box", "sh"}); got != "docker exec -it 'my box' sh" {
		t.Errorf("shellJoin = %q", got)
	}
}

func TestFileNames(t *testing.T) {
	dockerfiles := map[string]bool{
		"Dockerfile": true, "dockerfile": true, "Containerfile": true, "Dockerfile.dev": true, "app/Dockerfile.prod": true, "api.dockerfile": true,
		"web.Containerfile": true, "Dockerfiles": false, "dockerfile.go": true, "main.go": false, "docker-compose.yml": false, "README.md": false,
	}
	for name, want := range dockerfiles {
		if got := isDockerfile(name); got != want {
			t.Errorf("isDockerfile(%q) = %v", name, got)
		}
	}
	composes := map[string]bool{
		"compose.yaml": true, "compose.yml": true, "docker-compose.yml": true, "docker-compose.yaml": true, "compose.override.yml": true,
		"compose.prod.yaml": true, "my-docker-compose.dev.yml": true, "sub/docker-compose.yml": true, "Compose.YAML": true,
		"compose.json": false, "docker.yml": false, "config.yaml": false, "compose": false, "composer.yml": false,
	}
	for name, want := range composes {
		if got := isComposeFile(name); got != want {
			t.Errorf("isComposeFile(%q) = %v", name, got)
		}
	}
}

func TestImageNamesAndTags(t *testing.T) {
	tests := []struct{ folder, dockerfile, want string }{
		{"/w/My-App", "/w/My-App/Dockerfile", "myapp:latest"},
		{"/w/proj", "/w/proj/services/Api_Svc/Dockerfile", "apisvc:latest"},
		{"/w/---", "/w/---/Dockerfile", "image:latest"},
	}
	for _, tc := range tests {
		if got := defaultTag(tc.folder, tc.dockerfile); got != tc.want {
			t.Errorf("defaultTag(%q, %q) = %q, want %q", tc.folder, tc.dockerfile, got, tc.want)
		}
	}
	refs := map[string]bool{"app": true, "app:latest": true, "ghcr.io/me/app:1.0": true, "localhost:5000/app@sha256:abc": true, "My_App:V1": true,
		"": false, " ": false, "-rm": false, "has space": false, "a;b": false, ":x": false, "app\n": false}
	for ref, ok := range refs {
		if got := checkImageRef(ref) == ""; got != ok {
			t.Errorf("checkImageRef(%q) accepts = %v", ref, got)
		}
	}
	splits := []struct{ ref, repo, tag string }{
		{"app", "app", "latest"}, {"app:2", "app", "2"}, {"localhost:5000/app", "localhost:5000/app", "latest"},
		{"localhost:5000/app:2", "localhost:5000/app", "2"}, {"app@sha256:ab", "app", ""},
	}
	for _, tc := range splits {
		if repo, tag := splitRef(tc.ref); repo != tc.repo || tag != tc.tag {
			t.Errorf("splitRef(%q) = %q, %q", tc.ref, repo, tag)
		}
	}
}

func TestRelOrAbs(t *testing.T) {
	tests := []struct{ folder, p, want string }{
		{"/w", "/w/a/Dockerfile", "a/Dockerfile"}, {"/w", "/w", "."}, {"/w", "/other/x", "/other/x"}, {"/w", "/w2/x", "/w2/x"}, {"", "/x", "/x"},
	}
	for _, tc := range tests {
		if got := relOrAbs(tc.folder, tc.p); got != tc.want {
			t.Errorf("relOrAbs(%q, %q) = %q, want %q", tc.folder, tc.p, got, tc.want)
		}
	}
}

// TestRunProcess runs real programs: the output by line with the stream it came on, the exit status, a program that cannot be started
// (the error has the tag and a route), and a program that is stopped with its group when the context ends.
func TestRunProcess(t *testing.T) {
	type line struct {
		stderr bool
		text   string
	}
	run := func(ctx context.Context, argv ...string) (int, []line, error) {
		var got []line
		code, err := runProcess(ctx, cliJob{argv: argv, env: []string{"PATH=/usr/bin:/bin"}}, func(stderr bool, text string) { got = append(got, line{stderr, text}) })
		return code, got, err
	}
	code, got, err := run(context.Background(), "/bin/sh", "-c", "echo one; echo two >&2; printf 'no end'; exit 3")
	if err != nil || code != 3 {
		t.Fatalf("status %d, err %v", code, err)
	}
	has := func(l line) bool {
		for _, g := range got {
			if g == l {
				return true
			}
		}
		return false
	}
	if !has(line{false, "one"}) || !has(line{true, "two"}) || !has(line{false, "no end"}) {
		t.Errorf("lines %+v", got)
	}

	_, _, err = run(context.Background(), "/nonexistent/program")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("runProcess.ContainerUICLIFailed") {
		t.Errorf("a program that cannot be started: %v", err)
	}
	if _, _, err = run(context.Background()); err == nil {
		t.Error("no program to run is no error")
	} else if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ContainerUICLIFailed" {
		t.Errorf("no program: %v", err)
	}

	// the group is stopped: the child of the shell does not outlive the context
	pidFile := t.TempDir() + "/pid"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = run(ctx, "/bin/sh", "-c", "sleep 60 & echo $! > "+pidFile+"; wait")
	}()
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the program did not start its child")
	}
	cancel()
	<-done
	gone := false
	for i := 0; i < 200 && !gone; i++ {
		gone = syscall.Kill(pid, 0) == syscall.ESRCH
		time.Sleep(10 * time.Millisecond)
	}
	if !gone {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Error("the child of the program outlived the context")
	}
}

// TestRunJobsStopsAtTheFirstFailure runs two programs; the second is not started after a status other than 0.
func TestRunJobsStopsAtTheFirstFailure(t *testing.T) {
	env := []string{"PATH=/usr/bin:/bin"}
	sh := func(script string) cliJob { return cliJob{argv: []string{"/bin/sh", "-c", script}, env: env} }
	var lines []string
	collect := func(_ bool, l string) { lines = append(lines, l) }
	res := runJobs(context.Background(), []cliJob{sh("echo a; exit 2"), sh("echo b")}, collect)
	if res.ok || res.exit != 2 || res.canceled || !reflect.DeepEqual(lines, []string{"a"}) || !reflect.DeepEqual(res.tail, []string{"a"}) {
		t.Errorf("%+v %v", res, lines)
	}
	lines = nil
	res = runJobs(context.Background(), []cliJob{sh("echo a"), sh("echo b")}, collect)
	if !res.ok || !reflect.DeepEqual(lines, []string{"a", "b"}) {
		t.Errorf("%+v %v", res, lines)
	}
	// the tail is bounded
	res = runJobs(context.Background(), []cliJob{sh("i=0; while [ $i -lt 5000 ]; do echo $i; i=$((i+1)); done")}, func(bool, string) {})
	if len(res.tail) > tailLines || res.tail[len(res.tail)-1] != "4999" {
		t.Errorf("the tail has %d lines, the last %q", len(res.tail), res.tail[len(res.tail)-1])
	}
}
